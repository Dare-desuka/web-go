package api

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"web-go/backend/internal/database"
)

func subBody(t *testing.T, filename, content, lang, label string) (body *bytes.Buffer, ctype string) {
	t.Helper()
	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	_ = mw.WriteField("language", lang)
	_ = mw.WriteField("label", label)
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf, mw.FormDataContentType()
}

func TestSubtitleUploadDelete(t *testing.T) {
	f := openAdminMux(t, 1<<20, 1000)
	id := f.createMovie(t, "Sub Film")
	url := "/api/admin/movies/" + strconv.FormatInt(id, 10) + "/subtitles"

	body, ctype := subBody(t, "en.vtt", "WEBVTT\n\n00:00.000 --> 00:01.000\nHi\n", "en", "English")
	w := f.do(t, "POST", url, body, ctype)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload -> %d %s", w.Code, w.Body.String())
	}

	// Same language twice: second keeps a free label (English2), no conflict.
	body, ctype = subBody(t, "en2.vtt", "WEBVTT\n", "en", "English")
	w2 := f.do(t, "POST", url, body, ctype)
	if w2.Code != http.StatusCreated {
		t.Fatalf("second en -> %d %s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "English2") {
		t.Fatalf("second en not renumbered: %s", w2.Body.String())
	}
	// Third copy skips the taken number.
	body, ctype = subBody(t, "en3.vtt", "WEBVTT\n", "en", "English")
	w3 := f.do(t, "POST", url, body, ctype)
	if w3.Code != http.StatusCreated || !strings.Contains(w3.Body.String(), "English3") {
		t.Fatalf("third en -> %d %s", w3.Code, w3.Body.String())
	}
	// Not WebVTT.
	body, ctype = subBody(t, "id.vtt", "hello, not subtitles", "id", "Indonesia")
	if w := f.do(t, "POST", url, body, ctype); w.Code != http.StatusBadRequest {
		t.Fatalf("garbage -> %d", w.Code)
	}
	// Bad language + wrong extension.
	body, ctype = subBody(t, "x.vtt", "WEBVTT\n", "ENGLISH!", "X")
	if w := f.do(t, "POST", url, body, ctype); w.Code != http.StatusBadRequest {
		t.Fatalf("bad lang -> %d", w.Code)
	}
	// .srt accepted and auto-converted to WebVTT
	body, ctype = subBody(t, "fr.srt", "1\n00:00:00,000 --> 00:00:01,000\nBonjour\n", "fr", "French")
	if w := f.do(t, "POST", url, body, ctype); w.Code != http.StatusCreated {
		t.Fatalf("srt -> %d %s", w.Code, w.Body.String())
	}

	subs, err := database.ListSubtitles(context.Background(), f.db, id)
	if err != nil || len(subs) != 4 {
		t.Fatalf("%v %+v", err, subs)
	}
	seen := map[string]bool{}
	for _, s := range subs {
		if s.TelegramFileID != nil {
			// Stored on Telegram storage: file_path should be empty.
			if s.FilePath != "" {
				t.Fatalf("telegram subtitle should have empty file_path: %s", s.FilePath)
			}
		} else {
			// Local disk fallback.
			if _, err := os.Stat(s.FilePath); err != nil {
				t.Fatalf("file not stored: %v", err)
			}
			if !strings.HasSuffix(s.FilePath, ".vtt") {
				t.Fatalf("stored path not .vtt: %s", s.FilePath)
			}
		}
		key := s.Label
		if s.TelegramFileID != nil {
			key = *s.TelegramFileID
		}
		if seen[key] {
			t.Fatalf("shared subtitle key: %s", key)
		}
		seen[key] = true
	}

	w = f.do(t, "DELETE", "/api/admin/subtitles/"+strconv.FormatInt(subs[0].ID, 10), nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("delete -> %d", w.Code)
	}
	if subs[0].TelegramFileID != nil {
		// Verify subtitle row is gone from DB.
		if _, err := database.GetSubtitleByID(context.Background(), f.db, subs[0].ID); err == nil {
			t.Fatal("subtitle row survived delete")
		}
	} else {
		if _, err := os.Stat(subs[0].FilePath); !os.IsNotExist(err) {
			t.Fatal("subtitle file survived delete")
		}
	}
	if w := f.do(t, "DELETE", "/api/admin/subtitles/999999", nil, ""); w.Code != http.StatusNotFound {
		t.Fatalf("delete missing -> %d", w.Code)
	}
}

// png1px is a minimal valid PNG (DetectContentType sniffs image/png).
var png1px = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0}

func imgBody(t *testing.T, filename string, data []byte) (body *bytes.Buffer, ctype string) {
	t.Helper()
	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf, mw.FormDataContentType()
}

func forceReady(t *testing.T, f *adminFixture, id int64) {
	t.Helper()
	// Test-only shortcut: poster serving is READY-gated, and reaching READY
	// via pipeline is covered elsewhere.
	if _, err := f.db.Exec(`UPDATE movies SET status = 'READY' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
}

func TestPosterUploadServe(t *testing.T) {
	f := openAdminMux(t, 1<<20, 1000)
	id := f.createMovie(t, "Poster Film")
	idStr := strconv.FormatInt(id, 10)

	body, ctype := imgBody(t, "p.png", png1px)
	w := f.do(t, "POST", "/api/admin/movies/"+idStr+"/poster", body, ctype)
	if w.Code != http.StatusOK {
		t.Fatalf("poster -> %d %s", w.Code, w.Body.String())
	}
	m, _ := database.GetMovieByID(context.Background(), f.db, id)
	if m.PosterURL == nil || *m.PosterURL != "/api/posters/"+idStr+"/poster" {
		t.Fatalf("%+v", m)
	}

	// Non-image rejected.
	body, ctype = imgBody(t, "p.png", []byte("definitely not an image at all...."))
	if w := f.do(t, "POST", "/api/admin/movies/"+idStr+"/backdrop", body, ctype); w.Code != http.StatusBadRequest {
		t.Fatalf("text -> %d", w.Code)
	}

	// Serve gated on READY.
	r := httptest.NewRequest("GET", "/api/posters/"+idStr+"/poster", nil)
	w2 := httptest.NewRecorder()
	f.mux.ServeHTTP(w2, r)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("draft poster -> %d", w2.Code)
	}
	forceReady(t, f, id)
	r = httptest.NewRequest("GET", "/api/posters/"+idStr+"/poster", nil)
	r.AddCookie(f.cookie)
	r.AddCookie(f.cookie)
	w2 = httptest.NewRecorder()
	f.mux.ServeHTTP(w2, r)
	if w2.Code != http.StatusOK {
		t.Fatalf("serve -> %d", w2.Code)
	}
	if ct := w2.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("ct = %q", ct)
	}
	if w2.Body.Len() == 0 {
		t.Fatal("empty body")
	}
	r = httptest.NewRequest("GET", "/api/posters/"+idStr+"/avatar", nil)
	w2 = httptest.NewRecorder()
	f.mux.ServeHTTP(w2, r)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("bad kind -> %d", w2.Code)
	}
}

func TestPublishUnpublish(t *testing.T) {
	f := openAdminMux(t, 1<<20, 1000)
	id := f.createMovie(t, "Pub Film")
	idStr := strconv.FormatInt(id, 10)
	ctx := context.Background()

	// No parts: cannot publish.
	if w := f.do(t, "POST", "/api/admin/movies/"+idStr+"/publish", nil, ""); w.Code != http.StatusConflict {
		t.Fatalf("publish empty -> %d", w.Code)
	}

	// Complete parts -> publish works.
	now := database.NowMillis()
	if err := database.WithTx(ctx, f.db, func(tx database.DBTX) error {
		return database.CreateParts(ctx, tx, id, []database.PlanPart{
			{PartNumber: 1, Size: 500, StartOffset: 0, EndOffset: 500},
			{PartNumber: 2, Size: 500, StartOffset: 500, EndOffset: 1000},
		}, now)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE movies SET video_size = 1000 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	parts, _ := database.ListParts(ctx, f.db, id)
	for _, p := range parts {
		_ = database.MarkPartUploading(ctx, f.db, p.ID)
		if err := database.MarkPartComplete(ctx, f.db, p.ID, 1, "f"); err != nil {
			t.Fatal(err)
		}
	}
	if w := f.do(t, "POST", "/api/admin/movies/"+idStr+"/publish", nil, ""); w.Code != http.StatusOK {
		t.Fatalf("publish -> %d %s", w.Code, w.Body.String())
	}
	// Visible publicly now.
	r := httptest.NewRequest("GET", "/api/movies/"+idStr, nil)
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("public detail -> %d", w.Code)
	}

	// Unpublish hides it again.
	if w := f.do(t, "POST", "/api/admin/movies/"+idStr+"/unpublish", nil, ""); w.Code != http.StatusOK {
		t.Fatalf("unpublish -> %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/api/movies/"+idStr, nil)
	w = httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("still public -> %d", w.Code)
	}
	// Unpublish twice fails; publish again works.
	if w := f.do(t, "POST", "/api/admin/movies/"+idStr+"/unpublish", nil, ""); w.Code != http.StatusConflict {
		t.Fatalf("unpublish x2 -> %d", w.Code)
	}
	if w := f.do(t, "POST", "/api/admin/movies/"+idStr+"/publish", nil, ""); w.Code != http.StatusOK {
		t.Fatalf("republish -> %d", w.Code)
	}
}

func TestDeleteRemovesMediaFiles(t *testing.T) {
	f := openAdminMux(t, 1<<20, 1000)
	id := f.createMovie(t, "Media Grave")
	idStr := strconv.FormatInt(id, 10)

	body, ctype := subBody(t, "en.vtt", "WEBVTT\n", "en", "English")
	if w := f.do(t, "POST", "/api/admin/movies/"+idStr+"/subtitles", body, ctype); w.Code != http.StatusCreated {
		t.Fatalf("sub -> %d", w.Code)
	}
	body, ctype = imgBody(t, "p.png", png1px)
	if w := f.do(t, "POST", "/api/admin/movies/"+idStr+"/poster", body, ctype); w.Code != http.StatusOK {
		t.Fatalf("poster -> %d", w.Code)
	}
	subs, _ := database.ListSubtitles(context.Background(), f.db, id)
	subPath := subs[0].FilePath

	if w := f.do(t, "DELETE", "/api/admin/movies/"+idStr, nil, ""); w.Code != http.StatusOK {
		t.Fatalf("delete -> %d", w.Code)
	}
	if _, err := os.Stat(subPath); !os.IsNotExist(err) {
		t.Fatal("subtitle file survived movie delete")
	}
	// No poster files remain for this movie under DataDir.
	left, _ := filepath.Glob(filepath.Join(f.dataDir, "posters", idStr+"_poster.*"))
	if len(left) != 0 {
		t.Fatalf("poster files survived: %v", left)
	}
}

func TestExtractSubtitles(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg missing")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe missing")
	}
	dir := t.TempDir()
	mkv := filepath.Join(dir, "two-subs.mkv")
	enSRT := "1\n00:00:00,000 --> 00:00:01,000\nHello\n"
	idSRT := "1\n00:00:00,000 --> 00:00:01,000\nHalo\n"
	if err := os.WriteFile(filepath.Join(dir, "en.srt"), []byte(enSRT), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "id.srt"), []byte(idSRT), 0o644); err != nil {
		t.Fatal(err)
	}
	mk := exec.Command("ffmpeg", "-y", "-v", "error",
		"-f", "lavfi", "-i", "color=c=black:s=320x240:d=1",
		"-i", filepath.Join(dir, "en.srt"), "-i", filepath.Join(dir, "id.srt"),
		"-map", "0:v", "-map", "1", "-map", "2",
		"-c:v", "mpeg4", "-c:s", "srt",
		"-metadata:s:s:0", "language=eng", "-metadata:s:s:1", "language=ind",
		mkv)
	if out, err := mk.CombinedOutput(); err != nil {
		t.Fatalf("synth mkv: %v %s", err, out)
	}

	db, err := database.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	movieID, err := database.CreateMovie(ctx, db, database.Movie{
		Title: "Sub", Slug: "sub", VideoSize: 10, VideoMime: "video/x-matroska",
		Status: database.StatusDRAFT,
	})
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{DataDir: filepath.Join(dir, "data")}
	n, err := ExtractSubtitles(ctx, db, opts, movieID, mkv)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("extracted = %d, want 2", n)
	}
	subs, err := database.ListSubtitles(ctx, db, movieID)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 2 {
		t.Fatalf("rows = %d, want 2", len(subs))
	}
	for _, s := range subs {
		raw, err := os.ReadFile(s.FilePath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(raw), "WEBVTT") {
			t.Fatalf("%s: not webvtt", s.Language)
		}
	}
	// Second run stores nothing new (manual/existing wins).
	if n, err := ExtractSubtitles(ctx, db, opts, movieID, mkv); err != nil || n != 0 {
		t.Fatalf("rerun = %d, %v; want 0, nil", n, err)
	}
	// Garbage input errors.
	garbage := filepath.Join(dir, "garbage.bin")
	if err := os.WriteFile(garbage, []byte{0, 1, 2, 3, 4, 5, 6, 7}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtractSubtitles(ctx, db, opts, movieID, garbage); err == nil {
		t.Fatal("want error for non-video input")
	}
}
