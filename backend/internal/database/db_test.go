package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func openTestDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	// Migrate must be idempotent.
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second migrate failed: %v", err)
	}
	return db, ctx
}

func TestMigrateCreatesTables(t *testing.T) {
	db, ctx := openTestDB(t)
	for _, table := range []string{"movies", "movie_parts", "subtitles", "watch_progress", "users"} {
		var name string
		err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("table %s missing: %v", table, err)
		}
	}
}

func TestListReadyFiltersStatus(t *testing.T) {
	db, ctx := openTestDB(t)
	now := NowMillis()
	_, err := db.ExecContext(ctx, `INSERT INTO movies
		(title, slug, description, video_size, video_mime, status, created_at, updated_at)
		VALUES ('Ready Film', 'ready-film', '', 1000, 'video/mp4', 'READY', ?, ?),
		       ('Draft Film', 'draft-film', '', 2000, 'video/mp4', 'DRAFT', ?, ?)`,
		now, now, now, now)
	if err != nil {
		t.Fatal(err)
	}

	movies, err := ListReady(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(movies) != 1 || movies[0].Slug != "ready-film" {
		t.Fatalf("got %+v, want only ready-film", movies)
	}
}

func TestPartsCascadeOnMovieDelete(t *testing.T) {
	db, ctx := openTestDB(t)
	now := NowMillis()
	res, err := db.ExecContext(ctx, `INSERT INTO movies
		(title, slug, description, video_size, video_mime, status, created_at, updated_at)
		VALUES ('Cascade', 'cascade', '', 943718400, 'video/mp4', 'UPLOADING', ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	movieID, _ := res.LastInsertId()
	_, err = db.ExecContext(ctx, `INSERT INTO movie_parts
		(movie_id, part_number, size, start_offset, end_offset, status, created_at, updated_at)
		VALUES (?, 1, 943718400, 0, 943718400, 'COMPLETE', ?, ?)`, movieID, now, now)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM movies WHERE id = ?`, movieID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM movie_parts WHERE movie_id = ?`, movieID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("parts remaining after movie delete: %d", n)
	}
}
