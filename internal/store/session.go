package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type User struct {
	ID          string
	DisplayName string
}

// UpsertUserByGoogleSub returns the user for sub, creating one with a
// placeholder display name if needed. An existing user's name is left alone.
func (s *Store) UpsertUserByGoogleSub(ctx context.Context, sub string) (User, error) {
	suffix, err := newID()
	if err != nil {
		return User{}, err
	}
	var u User
	err = s.pool.QueryRow(ctx, `
		INSERT INTO users (google_sub, display_name) VALUES ($1, $2)
		ON CONFLICT (google_sub) DO UPDATE SET google_sub = EXCLUDED.google_sub
		RETURNING id::text, display_name`,
		sub, "ユーザー"+suffix[:6]).Scan(&u.ID, &u.DisplayName)
	return u, err
}

func (s *Store) CreateSession(ctx context.Context, tokenHash []byte, userID string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (id_hash, user_id, expires_at) VALUES ($1, $2::uuid, $3)`,
		tokenHash, userID, expiresAt)
	return err
}

// GetSessionUser returns ErrNotFound for unknown or expired sessions.
func (s *Store) GetSessionUser(ctx context.Context, tokenHash []byte) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT u.id::text, u.display_name
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.id_hash = $1 AND s.expires_at > now()`, tokenHash).Scan(&u.ID, &u.DisplayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id_hash = $1`, tokenHash)
	return err
}
