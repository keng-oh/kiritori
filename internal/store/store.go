package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("store: not found")

const uniqueViolation = "23505"

type Store struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

type Clip struct {
	ID          string
	Platform    string
	SourceID    string
	Title       string
	SourceTitle *string
	StartSec    int
	EndSec      int
	UserID      *string
	ViewCount   int64
	CreatedAt   time.Time
}

type NewClip struct {
	Platform    string
	SourceID    string
	Title       string
	SourceTitle *string
	StartSec    int
	EndSec      int
	UserID      *string
}

const clipColumns = `id, platform, source_id, title, source_title, start_sec, end_sec,
	user_id::text, view_count, created_at`

func scanClip(row pgx.Row) (Clip, error) {
	var c Clip
	err := row.Scan(&c.ID, &c.Platform, &c.SourceID, &c.Title, &c.SourceTitle,
		&c.StartSec, &c.EndSec, &c.UserID, &c.ViewCount, &c.CreatedAt)
	return c, err
}

// CreateClip inserts a clip with a freshly generated ID, retrying on ID collision.
func (s *Store) CreateClip(ctx context.Context, in NewClip) (Clip, error) {
	if in.Platform == "" {
		in.Platform = "youtube"
	}
	for range 5 {
		id, err := newID()
		if err != nil {
			return Clip{}, err
		}
		c, err := scanClip(s.pool.QueryRow(ctx, `
			INSERT INTO clips (id, platform, source_id, title, source_title, start_sec, end_sec, user_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::uuid)
			RETURNING `+clipColumns,
			id, in.Platform, in.SourceID, in.Title, in.SourceTitle, in.StartSec, in.EndSec, in.UserID))
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "clips_pkey" {
			continue
		}
		return c, err
	}
	return Clip{}, errors.New("store: could not generate unique clip id")
}

func (s *Store) GetClip(ctx context.Context, id string) (Clip, error) {
	c, err := scanClip(s.pool.QueryRow(ctx,
		`SELECT `+clipColumns+` FROM clips WHERE id = $1 AND deleted_at IS NULL`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Clip{}, ErrNotFound
	}
	return c, err
}
