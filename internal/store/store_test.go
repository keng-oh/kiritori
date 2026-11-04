package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("DATABASE_URL must be set in CI")
		}
		t.Skip("DATABASE_URL not set")
	}
	if err := Migrate(context.Background(), url); err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestNewID(t *testing.T) {
	id, err := newID()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != idLength {
		t.Fatalf("len = %d, want %d", len(id), idLength)
	}
}

func TestCreateAndGetClip(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	c, err := s.CreateClip(ctx, NewClip{SourceID: "dQw4w9WgXcQ", Title: "test", StartSec: 10, EndSec: 40})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.pool.Exec(ctx, `DELETE FROM clips WHERE id = $1`, c.ID) })

	got, err := s.GetClip(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Platform != "youtube" || got.SourceID != "dQw4w9WgXcQ" || got.StartSec != 10 || got.EndSec != 40 || got.UserID != nil {
		t.Fatalf("unexpected clip: %+v", got)
	}

	if _, err := s.GetClip(ctx, "nonexistent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCreateClipRejectsLongRange(t *testing.T) {
	s := openTest(t)
	_, err := s.CreateClip(context.Background(), NewClip{SourceID: "x", Title: "t", StartSec: 0, EndSec: 61})
	if err == nil {
		t.Fatal("expected check constraint error")
	}
}

func TestUserAndSession(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	sub := "test-sub-" + time.Now().Format("150405.000000000")

	u1, err := s.UpsertUserByGoogleSub(ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, u1.ID) })
	if !strings.HasPrefix(u1.DisplayName, "ユーザー") {
		t.Fatalf("display name = %q", u1.DisplayName)
	}

	// 表示名を変えたユーザーは、再ログインしても名前が保たれる
	if _, err := s.pool.Exec(ctx, `UPDATE users SET display_name = '変更後' WHERE id = $1::uuid`, u1.ID); err != nil {
		t.Fatal(err)
	}
	u2, err := s.UpsertUserByGoogleSub(ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	if u2.ID != u1.ID || u2.DisplayName != "変更後" {
		t.Fatalf("second upsert = %+v, first = %+v", u2, u1)
	}

	hash := sha256.Sum256([]byte("token-" + sub))
	if err := s.CreateSession(ctx, hash[:], u1.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSessionUser(ctx, hash[:])
	if err != nil || got.ID != u1.ID {
		t.Fatalf("GetSessionUser = %+v, %v", got, err)
	}
	var stored []byte
	if err := s.pool.QueryRow(ctx, `SELECT id_hash FROM sessions WHERE user_id = $1::uuid`, u1.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, hash[:]) {
		t.Fatal("stored id_hash differs from the hash passed in")
	}

	if err := s.DeleteSession(ctx, hash[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSessionUser(ctx, hash[:]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete err = %v, want ErrNotFound", err)
	}
}

func TestExpiredSession(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	u, err := s.UpsertUserByGoogleSub(ctx, "test-expired-"+time.Now().Format("150405.000000000"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, u.ID) })

	hash := sha256.Sum256([]byte("expired-" + u.ID))
	if err := s.CreateSession(ctx, hash[:], u.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSessionUser(ctx, hash[:]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
