package store

import (
	"context"
	"errors"
	"os"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
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
