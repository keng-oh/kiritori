-- +goose Up
CREATE TABLE users (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    google_sub  text NOT NULL UNIQUE,
    display_name text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id_hash     bytea PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

CREATE TABLE clips (
    id             text PRIMARY KEY,
    platform       text NOT NULL DEFAULT 'youtube',
    source_id      text NOT NULL,
    title          text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 100),
    source_title   text,
    start_sec      integer NOT NULL CHECK (start_sec >= 0),
    end_sec        integer NOT NULL,
    user_id        uuid REFERENCES users(id) ON DELETE SET NULL,
    view_count     bigint NOT NULL DEFAULT 0,
    last_viewed_at timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    deleted_at     timestamptz,
    CHECK (end_sec - start_sec BETWEEN 1 AND 60)
);
-- 同じ動画のクリップ検索用
CREATE INDEX clips_source_idx ON clips (platform, source_id);
-- 一覧に載るのはログインユーザーのクリップのみ
CREATE INDEX clips_listed_created_idx ON clips (created_at DESC)
    WHERE user_id IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX clips_listed_views_idx ON clips (view_count DESC)
    WHERE user_id IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX clips_user_idx ON clips (user_id, created_at DESC)
    WHERE user_id IS NOT NULL AND deleted_at IS NULL;
-- 匿名クリップの期限切れ削除用
CREATE INDEX clips_anon_expiry_idx ON clips ((COALESCE(last_viewed_at, created_at)))
    WHERE user_id IS NULL AND deleted_at IS NULL;

-- ホット集計用（日別の再生数）
CREATE TABLE clip_view_daily (
    clip_id text NOT NULL REFERENCES clips(id) ON DELETE CASCADE,
    day     date NOT NULL,
    views   integer NOT NULL DEFAULT 0,
    PRIMARY KEY (clip_id, day)
);
CREATE INDEX clip_view_daily_day_idx ON clip_view_daily (day);

-- 重複カウント防止（古い行は定期的に削除）
CREATE TABLE clip_view_dedup (
    clip_id      text NOT NULL REFERENCES clips(id) ON DELETE CASCADE,
    visitor_hash bytea NOT NULL,
    day          date NOT NULL,
    PRIMARY KEY (clip_id, visitor_hash, day)
);
CREATE INDEX clip_view_dedup_day_idx ON clip_view_dedup (day);

-- +goose Down
DROP TABLE clip_view_dedup;
DROP TABLE clip_view_daily;
DROP TABLE clips;
DROP TABLE sessions;
DROP TABLE users;
