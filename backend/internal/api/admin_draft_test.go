package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"web-go/backend/internal/database"
	"web-go/backend/internal/storage"
)

// Playback is admin-gated: anonymous gets 401, role user gets 403, admin
// streams any status (DRAFT included). Catalog/detail stay public READY-only.
func TestAdminSeesDrafts(t *testing.T) {
	f := openAdminMux(t, 1<<20, 1000)
	ctx := context.Background()
	data := make([]byte, 2500)
	for i := range data {
		data[i] = byte(i % 251)
	}
	movieID, err := database.CreateMovie(ctx, f.db, database.Movie{
		Title: "Hidden Draft", Slug: "hidden-draft", VideoSize: int64(len(data)),
		VideoMime: "video/mp4", Status: database.StatusDRAFT,
	})
	if err != nil {
		t.Fatal(err)
	}
	sizes := []int64{1000, 1000, 500}
	plan := make([]database.PlanPart, len(sizes))
	fileIDs := make([]string, len(sizes))
	var offset int64
	for i, size := range sizes {
		fil, err := f.store.Upload(ctx, bytes.NewReader(data[offset:offset+size]),
			storage.Metadata{Name: "p", Size: size, Mime: "video/mp4"})
		if err != nil {
			t.Fatal(err)
		}
		plan[i] = database.PlanPart{PartNumber: int64(i + 1), Size: size, StartOffset: offset, EndOffset: offset + size}
		fileIDs[i] = fil.ID
		offset += size
	}
	if err := database.WithTx(ctx, f.db, func(tx database.DBTX) error {
		return database.CreateParts(ctx, tx, movieID, plan, database.NowMillis())
	}); err != nil {
		t.Fatal(err)
	}
	parts, err := database.ListParts(ctx, f.db, movieID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range parts {
		if err := database.MarkPartUploading(ctx, f.db, parts[i].ID); err != nil {
			t.Fatal(err)
		}
		if err := database.MarkPartComplete(ctx, f.db, parts[i].ID, 0, fileIDs[i]); err != nil {
			t.Fatal(err)
		}
	}
	id := strconv.FormatInt(movieID, 10)

	anon := func(url string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", url, nil)
		w := httptest.NewRecorder()
		f.mux.ServeHTTP(w, r)
		return w
	}
	if w := anon("/api/movies/" + id); w.Code != http.StatusNotFound {
		t.Fatalf("anon detail -> %d, want 404", w.Code)
	}
	if w := anon("/api/stream/movies/" + id); w.Code != http.StatusNotFound {
		t.Fatalf("anon stream -> %d, want 404", w.Code)
	}

	authed := func(url string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", url, nil)
		r.AddCookie(f.cookie)
		w := httptest.NewRecorder()
		f.mux.ServeHTTP(w, r)
		return w
	}
	if w := authed("/api/movies/" + id); w.Code != http.StatusOK {
		t.Fatalf("admin detail -> %d", w.Code)
	}
	r := httptest.NewRequest("GET", "/api/stream/movies/"+id, nil)
	r.AddCookie(f.cookie)
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("admin draft stream -> %d", w.Code)
	}
	got, _ := io.ReadAll(w.Body)
	if !bytes.Equal(got, data) {
		t.Fatal("admin draft stream bytes mismatch")
	}

	// Admin catalog includes non-READY too (hidden films on admin home).
	r = httptest.NewRequest("GET", "/api/movies", nil)
	r.AddCookie(f.cookie)
	w = httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte("Hidden Draft")) {
		t.Fatalf("admin catalog -> %d %s", w.Code, w.Body.String())
	}
}
