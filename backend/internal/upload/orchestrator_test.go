package upload

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"web-go/backend/internal/database"
	"web-go/backend/internal/storage"
)

const (
	testMaxMovie = int64(21474836480)
	testMaxPart  = int64(1000)
)

func openUploadDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedUploadMovie(t *testing.T, db *sql.DB, slug string, size int64) int64 {
	t.Helper()
	id, err := database.CreateMovie(context.Background(), db, database.Movie{
		Title: slug, Slug: slug, VideoSize: size, VideoMime: "video/mp4",
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// deterministicBytes returns size bytes with a verifiable pattern.
func deterministicBytes(size int64) []byte {
	b := make([]byte, size)
	for i := range b {
		b[i] = byte((i * 31) % 251)
	}
	return b
}

func fastOpts() Options {
	return Options{MaxRetries: 2, BackoffBase: time.Second, Sleep: func(time.Duration) {}}
}

func TestHappyPathPrepareUploadFinalize(t *testing.T) {
	db := openUploadDB(t)
	ctx := context.Background()
	data := deterministicBytes(2500) // 3 parts: 1000/1000/500
	movieID := seedUploadMovie(t, db, "happy", int64(len(data)))

	plan, err := Prepare(ctx, db, movieID, testMaxMovie, testMaxPart)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 3 {
		t.Fatalf("parts = %d, want 3", len(plan))
	}
	if m, _ := database.GetMovieByID(ctx, db, movieID); m.Status != database.StatusUPLOADING {
		t.Fatalf("status = %s", m.Status)
	}

	var progressCalls int
	opts := fastOpts()
	opts.Progress = func(part, uploaded, total int64) { progressCalls++ }
	if err := UploadParts(ctx, db, storage.NewMemory(), movieID, bytes.NewReader(data), opts); err != nil {
		t.Fatal(err)
	}
	// Re-run must be a no-op success (idempotent resume with nothing pending).
	store := storage.NewMemory()
	_ = store
	if err := UploadParts(ctx, db, storage.NewMemory(), movieID, bytes.NewReader(data), fastOpts()); err != nil {
		t.Fatalf("re-run: %v", err)
	}

	if err := Finalize(ctx, db, movieID); err != nil {
		t.Fatal(err)
	}
	if m, _ := database.GetMovieByID(ctx, db, movieID); m.Status != database.StatusREADY {
		t.Fatalf("status = %s, want READY", m.Status)
	}
	if progressCalls == 0 {
		t.Fatal("no progress reported")
	}
}

func TestUploadedBytesMatchSource(t *testing.T) {
	db := openUploadDB(t)
	ctx := context.Background()
	data := deterministicBytes(2500)
	movieID := seedUploadMovie(t, db, "exact", int64(len(data)))
	store := storage.NewMemory()

	if _, err := Prepare(ctx, db, movieID, testMaxMovie, testMaxPart); err != nil {
		t.Fatal(err)
	}
	if err := UploadParts(ctx, db, store, movieID, bytes.NewReader(data), fastOpts()); err != nil {
		t.Fatal(err)
	}
	parts, err := database.ListParts(ctx, db, movieID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range parts {
		if p.TelegramFileID == nil {
			t.Fatalf("part %d has no file id", p.PartNumber)
		}
		got := store.Get(*p.TelegramFileID)
		want := data[p.StartOffset:p.EndOffset]
		if !bytes.Equal(got, want) {
			t.Fatalf("part %d bytes differ", p.PartNumber)
		}
	}
}

func TestFailureRetryAndResume(t *testing.T) {
	db := openUploadDB(t)
	ctx := context.Background()
	data := deterministicBytes(2500)
	movieID := seedUploadMovie(t, db, "flaky", int64(len(data)))
	if _, err := Prepare(ctx, db, movieID, testMaxMovie, testMaxPart); err != nil {
		t.Fatal(err)
	}

	store := storage.NewMemory()
	store.FailNextUploads = 100 // every attempt fails
	var sleeps []time.Duration
	opts := fastOpts()
	opts.Sleep = func(d time.Duration) { sleeps = append(sleeps, d) }

	if err := UploadParts(ctx, db, store, movieID, bytes.NewReader(data), opts); err == nil {
		t.Fatal("expected upload error")
	}
	// Exponential backoff observed without actually waiting: [1s 2s] per part × 3 parts.
	wantSleeps := []time.Duration{time.Second, 2 * time.Second, time.Second, 2 * time.Second, time.Second, 2 * time.Second}
	if len(sleeps) != len(wantSleeps) {
		t.Fatalf("sleeps = %v", sleeps)
	}
	for i := range wantSleeps {
		if sleeps[i] != wantSleeps[i] {
			t.Fatalf("sleeps = %v", sleeps)
		}
	}
	parts, _ := database.ListParts(ctx, db, movieID)
	// 3 attempts (1 + MaxRetries 2), one failed round recorded.
	if parts[0].Status != database.PartFailed || parts[0].RetryCount != 1 {
		t.Fatalf("part1 = %+v, want FAILED retry_count=1", parts[0])
	}
	if m, _ := database.GetMovieByID(ctx, db, movieID); m.Status != database.StatusERROR {
		t.Fatalf("status = %s, want ERROR", m.Status)
	}

	// Heal the backend, retry: part 1 re-uploads, others proceed normally.
	store.FailNextUploads = 0
	callsBefore := store.UploadCalls
	if err := RetryMovie(ctx, db, store, movieID, bytes.NewReader(data), fastOpts()); err != nil {
		t.Fatal(err)
	}
	if m, _ := database.GetMovieByID(ctx, db, movieID); m.Status != database.StatusREADY {
		t.Fatalf("status = %s, want READY", m.Status)
	}
	parts, _ = database.ListParts(ctx, db, movieID)
	for _, p := range parts {
		if p.Status != database.PartComplete {
			t.Fatalf("part %d = %s", p.PartNumber, p.Status)
		}
	}
	// 1 failed attempt batch (3 tries on part 1) + retry run (3 uploads).
	if store.UploadCalls-callsBefore != 3 {
		t.Fatalf("retry uploads = %d, want 3", store.UploadCalls-callsBefore)
	}
}

func TestCrashResumeSkipsComplete(t *testing.T) {
	db := openUploadDB(t)
	ctx := context.Background()
	data := deterministicBytes(2500)
	movieID := seedUploadMovie(t, db, "crash", int64(len(data)))
	if _, err := Prepare(ctx, db, movieID, testMaxMovie, testMaxPart); err != nil {
		t.Fatal(err)
	}
	store := storage.NewMemory()

	// Simulate crash after part 1: complete it directly, then run UploadParts.
	parts, _ := database.ListParts(ctx, db, movieID)
	if err := database.MarkPartUploading(ctx, db, parts[0].ID); err != nil {
		t.Fatal(err)
	}
	f, err := store.Upload(ctx, bytes.NewReader(data[:1000]),
		storage.Metadata{Name: "crash-part-1", Size: 1000, Mime: "video/mp4"})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MarkPartComplete(ctx, db, parts[0].ID, 0, f.ID); err != nil {
		t.Fatal(err)
	}

	callsBefore := store.UploadCalls
	if err := UploadParts(ctx, db, store, movieID, bytes.NewReader(data), fastOpts()); err != nil {
		t.Fatal(err)
	}
	// Only parts 2 and 3 uploaded; part 1 skipped.
	if store.UploadCalls-callsBefore != 2 {
		t.Fatalf("uploads = %d, want 2", store.UploadCalls-callsBefore)
	}
	if err := Finalize(ctx, db, movieID); err != nil {
		t.Fatal(err)
	}
}

func TestFinalizeRejectsIncomplete(t *testing.T) {
	db := openUploadDB(t)
	ctx := context.Background()
	movieID := seedUploadMovie(t, db, "incomplete", 2000)
	if _, err := Prepare(ctx, db, movieID, testMaxMovie, testMaxPart); err != nil {
		t.Fatal(err)
	}
	if err := Finalize(ctx, db, movieID); err == nil {
		t.Fatal("finalize accepted incomplete upload")
	}
	if m, _ := database.GetMovieByID(ctx, db, movieID); m.Status != database.StatusERROR {
		t.Fatalf("status = %s, want ERROR", m.Status)
	}
}

func TestPrepareValidates(t *testing.T) {
	db := openUploadDB(t)
	ctx := context.Background()
	if _, err := Prepare(ctx, db, 999999, testMaxMovie, testMaxPart); !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("missing movie err = %v", err)
	}
	tooBig := seedUploadMovie(t, db, "big", testMaxMovie+1)
	if _, err := Prepare(ctx, db, tooBig, testMaxMovie, testMaxPart); err == nil {
		t.Fatal("oversize accepted")
	}
	movieID := seedUploadMovie(t, db, "twice", 100)
	if _, err := Prepare(ctx, db, movieID, testMaxMovie, testMaxPart); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(ctx, db, movieID, testMaxMovie, testMaxPart); err == nil {
		t.Fatal("double prepare accepted")
	}
}
