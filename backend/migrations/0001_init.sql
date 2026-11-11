-- 0001_init.sql: MVP baseline schema.
-- Conventions (PRD section 28):
--   * all timestamps are Unix INTEGER in milliseconds
--   * movie_parts uses [start_offset, end_offset) with end exclusive
--   * sizes/offsets are INTEGER (int64) bytes, never float

CREATE TABLE IF NOT EXISTS movies (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    title            TEXT NOT NULL,
    slug             TEXT NOT NULL UNIQUE,
    description      TEXT NOT NULL DEFAULT '',
    year             INTEGER NULL,
    duration_seconds INTEGER NULL,
    poster_url       TEXT NULL,
    backdrop_url     TEXT NULL,
    video_size       INTEGER NOT NULL,
    video_mime       TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'DRAFT',
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS movie_parts (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id            INTEGER NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    part_number         INTEGER NOT NULL,
    size                INTEGER NOT NULL,
    start_offset        INTEGER NOT NULL,
    end_offset          INTEGER NOT NULL,
    telegram_message_id INTEGER NULL,
    telegram_file_id    TEXT NULL,
    status              TEXT NOT NULL DEFAULT 'WAITING',
    retry_count         INTEGER NOT NULL DEFAULT 0,
    last_error          TEXT NULL,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    UNIQUE (movie_id, part_number)
);

CREATE TABLE IF NOT EXISTS subtitles (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id   INTEGER NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    language   TEXT NOT NULL,
    label      TEXT NOT NULL,
    file_path  TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE (movie_id, language)
);

-- MVP anonymous progress is client-side localStorage (PRD 28.5).
-- This table is reserved for post-MVP authenticated progress.
CREATE TABLE IF NOT EXISTS watch_progress (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id         INTEGER NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    user_id          INTEGER NULL REFERENCES users(id) ON DELETE CASCADE,
    position_seconds INTEGER NOT NULL DEFAULT 0,
    updated_at       INTEGER NOT NULL,
    UNIQUE (movie_id, user_id)
);

CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'admin',
    created_at    INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_movies_status ON movies(status);
CREATE INDEX IF NOT EXISTS idx_movies_year ON movies(year);
CREATE INDEX IF NOT EXISTS idx_movie_parts_movie ON movie_parts(movie_id, part_number);
CREATE INDEX IF NOT EXISTS idx_movie_parts_status ON movie_parts(movie_id, status);
CREATE INDEX IF NOT EXISTS idx_subtitles_movie ON subtitles(movie_id);
CREATE INDEX IF NOT EXISTS idx_watch_progress_movie ON watch_progress(movie_id);
