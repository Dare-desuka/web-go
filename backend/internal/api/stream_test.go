package api

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"web-go/backend/internal/database"
	"web-go/backend/internal/storage"
)

// openStreamMux seeds a READY movie (2500 bytes, 3 parts) backed by memory.
func openStreamMux(t *testing.T) (*http.ServeMux, *sql.DB, *storage.Memory, int64, []byte) {
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
	data := make([]byte, 2500)
	for i := range data {
		data[i] = byte(i % 251)
	}
	movieID, err := database.CreateMovie(ctx, db, database.Movie{
		Title: "Stream", Slug: "stream", VideoSize: int64(len(data)),
		VideoMime: "video/mp4", Status: database.StatusREADY,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := storage.NewMemory()
	sizes := []int64{1000, 1000, 500}
	plan := make([]database.PlanPart, len(sizes))
	fileIDs := make([]string, len(sizes))
	var offset int64
	for i, size := range sizes {
		f, err := store.Upload(ctx, bytes.NewReader(data[offset:offset+size]),
			storage.Metadata{Name: "p", Size: size, Mime: "video/mp4"})
		if err != nil {
			t.Fatal(err)
		}
		plan[i] = database.PlanPart{PartNumber: int64(i + 1), Size: size, StartOffset: offset, EndOffset: offset + size}
		fileIDs[i] = f.ID
		offset += size
	}
	now := database.NowMillis()
	if err := database.WithTx(ctx, db, func(tx database.DBTX) error {
		return database.CreateParts(ctx, tx, movieID, plan, now)
	}); err != nil {
		t.Fatal(err)
	}
	parts, err := database.ListParts(ctx, db, movieID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range parts {
		if err := database.MarkPartUploading(ctx, db, parts[i].ID); err != nil {
			t.Fatal(err)
		}
		if err := database.MarkPartComplete(ctx, db, parts[i].ID, 0, fileIDs[i]); err != nil {
			t.Fatal(err)
		}
	}
	mux := NewMux(db, Options{Store: store})
	return mux, db, store, movieID, data
}

func getStream(t *testing.T, mux *http.ServeMux, id string, rangeHeader string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/stream/movies/"+id, nil)
	if rangeHeader != "" {
		r.Header.Set("Range", rangeHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestStreamFull(t *testing.T) {
	mux, _, _, movieID, data := openStreamMux(t)
	w := getStream(t, mux, strconv.FormatInt(movieID, 10), "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if w.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("headers = %v", w.Header())
	}
	if w.Header().Get("Content-Length") != "2500" {
		t.Fatalf("content-length = %q", w.Header().Get("Content-Length"))
	}
	if w.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("content-type = %q", w.Header().Get("Content-Type"))
	}
	if !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatal("body mismatch")
	}
}

func TestStreamRangeWithinPart(t *testing.T) {
	mux, _, _, movieID, data := openStreamMux(t)
	w := getStream(t, mux, strconv.FormatInt(movieID, 10), "bytes=100-199")
	if w.Code != http.StatusPartialContent {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("Content-Range"); got != "bytes 100-199/2500" {
		t.Fatalf("content-range = %q", got)
	}
	if !bytes.Equal(w.Body.Bytes(), data[100:200]) {
		t.Fatal("body mismatch")
	}
}

func TestStreamRangeCrossPart(t *testing.T) {
	mux, _, _, movieID, data := openStreamMux(t)
	w := getStream(t, mux, strconv.FormatInt(movieID, 10), "bytes=900-2100")
	if w.Code != http.StatusPartialContent {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("Content-Range"); got != "bytes 900-2100/2500" {
		t.Fatalf("content-range = %q", got)
	}
	if !bytes.Equal(w.Body.Bytes(), data[900:2101]) {
		t.Fatal("cross-part body mismatch")
	}
}

func TestStreamSuffixAndOpenEnd(t *testing.T) {
	mux, _, _, movieID, data := openStreamMux(t)
	w := getStream(t, mux, strconv.FormatInt(movieID, 10), "bytes=-10")
	if w.Code != http.StatusPartialContent || !bytes.Equal(w.Body.Bytes(), data[2490:]) {
		t.Fatalf("suffix: %d %d bytes", w.Code, w.Body.Len())
	}
	w = getStream(t, mux, strconv.FormatInt(movieID, 10), "bytes=2400-")
	if w.Code != http.StatusPartialContent || !bytes.Equal(w.Body.Bytes(), data[2400:]) {
		t.Fatalf("open end: %d %d bytes", w.Code, w.Body.Len())
	}
}

func TestStreamNotFound(t *testing.T) {
	mux, db, _, _, _ := openStreamMux(t)
	ctx := context.Background()
	draftID, err := database.CreateMovie(ctx, db, database.Movie{
		Title: "Draft", Slug: "draft", VideoSize: 100,
		VideoMime: "video/mp4", Status: database.StatusDRAFT,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"999999", strconv.FormatInt(draftID, 10), "abc", "0", "-5"} {
		if w := getStream(t, mux, id, "bytes=0-10"); w.Code != http.StatusNotFound {
			t.Fatalf("id %s -> %d", id, w.Code)
		}
	}
}

func TestStreamBadRange(t *testing.T) {
	mux, _, _, movieID, _ := openStreamMux(t)
	for _, h := range []string{"bytes=2500-", "bytes=5000-6000", "bytes=200-100", "items=0-10", "bytes=0-1,2-3"} {
		w := getStream(t, mux, strconv.FormatInt(movieID, 10), h)
		if w.Code != http.StatusRequestedRangeNotSatisfiable {
			t.Fatalf("%q -> %d", h, w.Code)
		}
		if got := w.Header().Get("Content-Range"); got != "bytes */2500" {
			t.Fatalf("%q content-range = %q", h, got)
		}
	}
}

func TestStreamMissingPartBytesAbortsMidBody(t *testing.T) {
	mux, db, _, movieID, data := openStreamMux(t)
	parts, err := database.ListParts(context.Background(), db, movieID)
	if err != nil {
		t.Fatal(err)
	}
	gone := "gone"
	if _, err := db.ExecContext(context.Background(),
		`UPDATE movie_parts SET telegram_file_id = ? WHERE id = ?`, gone, parts[1].ID); err != nil {
		t.Fatal(err)
	}
	w := getStream(t, mux, strconv.FormatInt(movieID, 10), "bytes=900-1100")
	if w.Code != http.StatusPartialContent {
		t.Fatalf("status = %d", w.Code)
	}
	if !bytes.Equal(w.Body.Bytes(), data[900:1000]) {
		t.Fatalf("got %d bytes", w.Body.Len())
	}
}

func TestStreamNilStore(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "nil.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	id, err := database.CreateMovie(context.Background(), db, database.Movie{
		Title: "N", Slug: "n", VideoSize: 10, VideoMime: "video/mp4", Status: database.StatusREADY,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.WithTx(context.Background(), db, func(tx database.DBTX) error {
		return database.CreateParts(context.Background(), tx, id,
			[]database.PlanPart{{PartNumber: 1, Size: 10, StartOffset: 0, EndOffset: 10}}, database.NowMillis())
	}); err != nil {
		t.Fatal(err)
	}
	nilMux := NewMux(db, Options{})
	w := getStream(t, nilMux, strconv.FormatInt(id, 10), "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestStreamBodyReadable(t *testing.T) {
	mux, _, _, movieID, _ := openStreamMux(t)
	w := getStream(t, mux, strconv.FormatInt(movieID, 10), "bytes=0-99")
	body, err := io.ReadAll(w.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 100 {
		t.Fatalf("len = %d", len(body))
	}
}

func TestStreamDownloadAttachment(t *testing.T) {
	mux, _, _, movieID, data := openStreamMux(t)
	w := getStream(t, mux, strconv.FormatInt(movieID, 10), "")
	if w.Code != http.StatusOK || w.Header().Get("Content-Disposition") != "" {
		t.Fatalf("inline: %d %q", w.Code, w.Header().Get("Content-Disposition"))
	}
	r := httptest.NewRequest("GET", "/api/stream/movies/"+strconv.FormatInt(movieID, 10)+"?download=1", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("Content-Disposition"); got != `attachment; filename="stream.mp4"` {
		t.Fatalf("content-disposition = %q", got)
	}
	if !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatal("download bytes differ from stream")
	}
	r = httptest.NewRequest("GET", "/api/stream/movies/"+strconv.FormatInt(movieID, 10)+"?download=1", nil)
	r.Header.Set("Range", "bytes=100-199")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusPartialContent || !bytes.Equal(w.Body.Bytes(), data[100:200]) {
		t.Fatalf("range download: %d %d bytes", w.Code, w.Body.Len())
	}
	if r.Header.Get("Range") == "" {
		t.Fatal("attachment lost on range download")
	}
}
