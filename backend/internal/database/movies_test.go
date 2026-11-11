package database

import (
	"errors"
	"testing"
)

func TestMovieCRUD(t *testing.T) {
	db, ctx := openTestDB(t)
	year := int64(2024)

	id, err := CreateMovie(ctx, db, Movie{
		Title: "CRUD Film", Slug: "crud-film", Description: "d",
		Year: &year, VideoSize: 1000, VideoMime: "video/mp4",
	})
	if err != nil {
		t.Fatal(err)
	}

	m, err := GetMovieByID(ctx, db, id)
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != StatusDRAFT || m.Year == nil || *m.Year != 2024 {
		t.Fatalf("got %+v", m)
	}

	if _, err := CreateMovie(ctx, db, Movie{Title: "Dup", Slug: "crud-film", VideoSize: 1, VideoMime: "video/mp4"}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("dup slug err = %v, want ErrAlreadyExists", err)
	}

	poster := "http://x/p.jpg"
	if err := UpdateMovieMeta(ctx, db, id, Movie{
		Title: "CRUD Film 2", Slug: "crud-film-2", Description: "d2", PosterURL: &poster,
	}); err != nil {
		t.Fatal(err)
	}
	m, _ = GetMovieByID(ctx, db, id)
	if m.Title != "CRUD Film 2" || m.PosterURL == nil || *m.PosterURL != poster {
		t.Fatalf("after update: %+v", m)
	}

	if err := UpdateMovieMeta(ctx, db, 999999, Movie{Title: "x", Slug: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing err = %v", err)
	}
	if err := DeleteMovie(ctx, db, id); err != nil {
		t.Fatal(err)
	}
	if _, err := GetMovieByID(ctx, db, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted err = %v", err)
	}
	if err := DeleteMovie(ctx, db, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing err = %v", err)
	}
}

func TestMovieStatusTransitions(t *testing.T) {
	db, ctx := openTestDB(t)
	id, err := CreateMovie(ctx, db, Movie{Title: "S", Slug: "s", VideoSize: 1, VideoMime: "video/mp4"})
	if err != nil {
		t.Fatal(err)
	}

	// Illegal jump skips the upload pipeline.
	if err := SetMovieStatus(ctx, db, id, StatusREADY); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("DRAFT->READY err = %v, want ErrInvalidTransition", err)
	}
	for _, s := range []string{StatusUPLOADING, StatusPROCESSING, StatusREADY} {
		if err := SetMovieStatus(ctx, db, id, s); err != nil {
			t.Fatalf("->%s failed: %v", s, err)
		}
	}
	// READY movies cannot go back to work states.
	if err := SetMovieStatus(ctx, db, id, StatusUPLOADING); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("READY->UPLOADING err = %v", err)
	}
	if err := SetMovieStatus(ctx, db, id, StatusDELETING); err != nil {
		t.Fatal(err)
	}
	if err := SetMovieStatus(ctx, db, 999999, StatusDELETING); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing movie err = %v", err)
	}
}

func TestMovieErrorRetryTransition(t *testing.T) {
	db, ctx := openTestDB(t)
	id, err := CreateMovie(ctx, db, Movie{Title: "R", Slug: "r", VideoSize: 1, VideoMime: "video/mp4"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{StatusUPLOADING, StatusERROR, StatusUPLOADING, StatusPROCESSING, StatusREADY} {
		if err := SetMovieStatus(ctx, db, id, s); err != nil {
			t.Fatalf("->%s failed: %v", s, err)
		}
	}
}

func TestGetReadyMovieHidesNonReady(t *testing.T) {
	db, ctx := openTestDB(t)
	id, _ := CreateMovie(ctx, db, Movie{Title: "H", Slug: "h", VideoSize: 1, VideoMime: "video/mp4"})
	if _, err := GetReadyMovieByID(ctx, db, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DRAFT visible err = %v", err)
	}
	if err := SetMovieStatus(ctx, db, id, StatusUPLOADING); err != nil {
		t.Fatal(err)
	}
	if _, err := GetReadyMovieByID(ctx, db, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UPLOADING visible err = %v", err)
	}
}

func TestWithTxRollback(t *testing.T) {
	db, ctx := openTestDB(t)
	err := WithTx(ctx, db, func(tx DBTX) error {
		if _, err := CreateMovie(ctx, tx, Movie{Title: "Tx", Slug: "tx", VideoSize: 1, VideoMime: "video/mp4"}); err != nil {
			return err
		}
		return ErrInvalidTransition // force rollback
	})
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM movies WHERE slug = 'tx'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rolled-back row persisted")
	}
}
