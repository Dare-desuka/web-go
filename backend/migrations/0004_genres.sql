-- 0004_genres.sql: multi genre per movie (csv, max 5).
ALTER TABLE movies ADD COLUMN genres TEXT NOT NULL DEFAULT '';
