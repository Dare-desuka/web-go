package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"web-go/backend/internal/database"
)

func openTestMux(t *testing.T) (*http.ServeMux, *sql.DB) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	return NewMux(db, Options{}), db
}

func TestHealth(t *testing.T) {
	mux, _ := openTestMux(t)
	r := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %v", body)
	}
}

func TestListMoviesEmpty(t *testing.T) {
	mux, _ := openTestMux(t)
	r := httptest.NewRequest("GET", "/api/movies", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	// Must be [] not null so the frontend can iterate without guards.
	if got := w.Body.String(); got != "[]\n" {
		t.Fatalf("body = %q, want empty JSON array", got)
	}
}

func TestListMoviesOnlyReady(t *testing.T) {
	mux, db := openTestMux(t)
	now := database.NowMillis()
	_, err := db.ExecContext(context.Background(), `INSERT INTO movies
		(title, slug, description, video_size, video_mime, status, created_at, updated_at)
		VALUES ('Shown', 'shown', '', 10, 'video/mp4', 'READY', ?, ?),
		       ('Hidden', 'hidden', '', 20, 'video/mp4', 'UPLOADING', ?, ?)`,
		now, now, now, now)
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest("GET", "/api/movies", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	var movies []database.Movie
	if err := json.NewDecoder(w.Body).Decode(&movies); err != nil {
		t.Fatal(err)
	}
	if len(movies) != 1 || movies[0].Slug != "shown" {
		t.Fatalf("got %+v, want only shown", movies)
	}
}
