-- 0005: allow multiple subtitle tracks per language (Indonesia, Indonesia2, ...).
-- SQLite cannot drop the UNIQUE(movie_id, language) constraint in place, so
-- rebuild the table. Idempotent: every run ends with only `subtitles` existing.
CREATE TABLE IF NOT EXISTS subtitles_new (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id   INTEGER NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    language   TEXT NOT NULL,
    label      TEXT NOT NULL,
    file_path  TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
INSERT INTO subtitles_new (id, movie_id, language, label, file_path, created_at)
SELECT id, movie_id, language, label, file_path, created_at FROM subtitles
WHERE NOT EXISTS (SELECT 1 FROM subtitles_new WHERE subtitles_new.id = subtitles.id);
DROP TABLE IF EXISTS subtitles;
ALTER TABLE subtitles_new RENAME TO subtitles;
CREATE INDEX IF NOT EXISTS idx_subtitles_movie ON subtitles(movie_id);
