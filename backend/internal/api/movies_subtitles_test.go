package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"web-go/backend/internal/auth"
	"web-go/backend/internal/database"
)

const testVTT = "WEBVTT\n\n00:00.000 --> 00:01.000\nHalo\n"

func seedDetailMovie(t *testing.T, db *sql.DB, status string) int64 {
	t.Helper()
	id, err := database.CreateMovie(context.Background(), db, database.Movie{
		Title: "Detail", Slug: "detail-" + status, VideoSize: 100,
		VideoMime: "video/mp4", Status: status,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestGetMovie(t *testing.T) {
	mux, db := openTestMux(t)
	cookie := loginAsAdmin(t, mux, db)
	readyID := seedDetailMovie(t, db, database.StatusREADY)
	draftID := seedDetailMovie(t, db, database.StatusDRAFT)

	r := httptest.NewRequest("GET", "/api/movies/"+strconv.FormatInt(readyID, 10), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var m database.Movie
	if err := json.NewDecoder(w.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	if m.ID != readyID || m.Title != "Detail" {
		t.Fatalf("%+v", m)
	}

	for _, id := range []string{"999999", strconv.FormatInt(draftID, 10), "abc", "0"} {
		r := httptest.NewRequest("GET", "/api/movies/"+id, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("id %s -> %d", id, w.Code)
		}
	}

	// Admins see DRAFT details.
	r = httptest.NewRequest("GET", "/api/movies/"+strconv.FormatInt(draftID, 10), nil)
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: cookie})
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("admin draft -> %d", w.Code)
	}

}

func loginAsAdmin(t *testing.T, mux *http.ServeMux, db *sql.DB) string {
	t.Helper()
	hash, err := auth.Hash("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateUser(context.Background(), db, "admin", hash, database.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/admin/login",
		strings.NewReader(`{"username":"admin","password":"s3cret"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	for _, c := range w.Result().Cookies() {
		if c.Name == SessionCookie {
			return c.Value
		}
	}
	t.Fatal("no session cookie")
	return ""
}

func TestSubtitlesAPI(t *testing.T) {
	mux, db := openTestMux(t)
	ctx := context.Background()
	cookie := loginAsAdmin(t, mux, db)
	readyID := seedDetailMovie(t, db, database.StatusREADY)
	draftID := seedDetailMovie(t, db, database.StatusDRAFT)

	// Empty list is [] not null.
	r := httptest.NewRequest("GET", "/api/subtitles/"+strconv.FormatInt(readyID, 10), nil)
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: cookie})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Body.String() != "[]\n" {
		t.Fatalf("empty: %d %q", w.Code, w.Body.String())
	}

	vttPath := filepath.Join(t.TempDir(), "id.vtt")
	if err := os.WriteFile(vttPath, []byte(testVTT), 0o644); err != nil {
		t.Fatal(err)
	}
	subID, err := database.AddSubtitle(ctx, db, database.Subtitle{
		MovieID: readyID, Language: "id", Label: "Indonesia", FilePath: vttPath,
	})
	if err != nil {
		t.Fatal(err)
	}

	r = httptest.NewRequest("GET", "/api/subtitles/"+strconv.FormatInt(readyID, 10), nil)
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: cookie})
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	var subs []database.Subtitle
	if err := json.NewDecoder(w.Body).Decode(&subs); err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0].Language != "id" {
		t.Fatalf("%+v", subs)
	}
	if strings.Contains(w.Body.String(), vttPath) {
		t.Fatal("file path leaked to client")
	}

	// Serve the .vtt bytes.
	r = httptest.NewRequest("GET", "/api/subtitles/"+strconv.FormatInt(readyID, 10)+"/"+strconv.FormatInt(subID, 10), nil)
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: cookie})
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("serve status = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/vtt; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}
	if w.Body.String() != testVTT {
		t.Fatalf("body = %q", w.Body.String())
	}

	// Missing subtitle, missing movie, bad ids.
	for _, url := range []string{
		"/api/subtitles/" + strconv.FormatInt(readyID, 10) + "/999999",
		"/api/subtitles/999999",
		"/api/subtitles/" + strconv.FormatInt(draftID, 10) + "/1",
		"/api/subtitles/abc",
	} {
		r := httptest.NewRequest("GET", url, nil)
		r.AddCookie(&http.Cookie{Name: SessionCookie, Value: cookie})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s -> %d", url, w.Code)
		}
	}

	// Admins see DRAFT lists too (empty here, not 404).
	r = httptest.NewRequest("GET", "/api/subtitles/"+strconv.FormatInt(draftID, 10), nil)
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: cookie})
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Body.String() != "[]\n" {
		t.Fatalf("draft list: %d %q", w.Code, w.Body.String())
	}

	// DB row exists but file deleted from disk.
	if err := os.Remove(vttPath); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("GET", "/api/subtitles/"+strconv.FormatInt(readyID, 10)+"/"+strconv.FormatInt(subID, 10), nil)
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: cookie})
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("deleted file -> %d", w.Code)
	}
}
