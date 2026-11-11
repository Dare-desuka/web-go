package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"web-go/backend/internal/auth"
	"web-go/backend/internal/database"
	"web-go/backend/internal/storage"
)

type adminFixture struct {
	mux     *http.ServeMux
	db      *sql.DB
	store   *storage.Memory
	tmp     string
	dataDir string
	cookie  *http.Cookie
}

func openAdminMux(t *testing.T, maxMovie, maxPart int64) *adminFixture {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.Hash("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateUser(context.Background(), db, "admin", hash, "admin"); err != nil {
		t.Fatal(err)
	}
	store := storage.NewMemory()
	tmp := t.TempDir()
	dataDir := t.TempDir()
	mux := NewMux(db, Options{
		Store: store, TmpDir: tmp, DataDir: dataDir,
		MaxMovieSize: maxMovie, MaxPartSize: maxPart,
		UploadRetries: 1, UploadBackoff: time.Millisecond,
		LoginLimiter: newLoginLimiter(100, time.Minute),
	})
	// Log in.
	r := httptest.NewRequest("POST", "/api/admin/login", strings.NewReader(`{"username":"admin","password":"s3cret"}`))
	r.RemoteAddr = "10.0.0.1:1"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d", w.Code)
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == SessionCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie")
	}
	return &adminFixture{mux: mux, db: db, store: store, tmp: tmp, dataDir: dataDir, cookie: cookie}
}

func (f *adminFixture) do(t *testing.T, method, url string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, url, body)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	r.AddCookie(f.cookie)
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	return w
}

func (f *adminFixture) createMovie(t *testing.T, title string) int64 {
	t.Helper()
	w := f.do(t, "POST", "/api/admin/movies", strings.NewReader(`{"title":"`+title+`"}`), "application/json")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var m database.Movie
	if err := json.NewDecoder(w.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m.ID
}

// mp4Body returns size bytes sniffed as video/mp4 (ftyp at offset 4).
func mp4Body(size int) []byte {
	b := make([]byte, size)
	copy(b, []byte{0, 0, 0, 0, 'f', 't', 'y', 'p', 'm', 'p', '4', '2'})
	for i := 12; i < size; i++ {
		b[i] = byte(i % 251)
	}
	return b
}

func uploadBody(t *testing.T, filename string, data []byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("video", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func (f *adminFixture) waitStatus(t *testing.T, id int64, want string) map[string]any {
	t.Helper()
	url := "/api/admin/movies/" + strconv.FormatInt(id, 10) + "/upload"
	deadline := time.Now().Add(30 * time.Second)
	for {
		w := f.do(t, "GET", url, nil, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status: %d", w.Code)
		}
		var st map[string]any
		raw, _ := io.ReadAll(w.Body)
		if err := json.Unmarshal(raw, &st); err != nil {
			t.Fatal(err)
		}
		got := st["movie"].(map[string]any)["status"].(string)
		if got == want || time.Now().After(deadline) {
			if got != want {
				t.Fatalf("status stuck at %s: %s", got, string(raw))
			}
			return st
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// postRetry polls POST retry until 202: the status row can show ERROR a
// moment before the background job clears its running flag.
func (f *adminFixture) postRetry(t *testing.T, id int64) {
	t.Helper()
	url := "/api/admin/movies/" + strconv.FormatInt(id, 10) + "/retry"
	deadline := time.Now().Add(10 * time.Second)
	for {
		w := f.do(t, "POST", url, nil, "")
		if w.Code == http.StatusAccepted {
			return
		}
		if w.Code != http.StatusConflict || time.Now().After(deadline) {
			t.Fatalf("retry -> %d %s", w.Code, w.Body.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// waitJobIdle polls until the background pipeline for id is done, then
// returns its status. Acting on a terminal status (ERROR/READY) is only
// sound once the owning job finished: right after 202 the row can still
// show the previous run's terminal state before the new goroutine starts.
func (f *adminFixture) waitJobIdle(t *testing.T, id int64) string {
	t.Helper()
	url := "/api/admin/movies/" + strconv.FormatInt(id, 10) + "/upload"
	deadline := time.Now().Add(30 * time.Second)
	for {
		w := f.do(t, "GET", url, nil, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status: %d", w.Code)
		}
		var st struct {
			Job struct {
				Running bool `json:"running"`
			} `json:"job"`
			Movie struct {
				Status string `json:"status"`
			} `json:"movie"`
		}
		raw, _ := io.ReadAll(w.Body)
		if err := json.Unmarshal(raw, &st); err != nil {
			t.Fatal(err)
		}
		if !st.Job.Running || time.Now().After(deadline) {
			if st.Job.Running {
				t.Fatalf("job stuck running: %s", string(raw))
			}
			return st.Movie.Status
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestAdminGating(t *testing.T) {
	f := openAdminMux(t, 1<<20, 1000)
	id := f.createMovie(t, "Gated")
	urls := []struct{ method, url string }{
		{"GET", "/api/admin/movies"},
		{"POST", "/api/admin/movies"},
		{"GET", "/api/admin/movies/" + strconv.FormatInt(id, 10)},
		{"PATCH", "/api/admin/movies/" + strconv.FormatInt(id, 10)},
		{"DELETE", "/api/admin/movies/" + strconv.FormatInt(id, 10)},
		{"POST", "/api/admin/movies/" + strconv.FormatInt(id, 10) + "/upload"},
		{"GET", "/api/admin/movies/" + strconv.FormatInt(id, 10) + "/upload"},
		{"POST", "/api/admin/movies/" + strconv.FormatInt(id, 10) + "/retry"},
	}
	for _, u := range urls {
		r := httptest.NewRequest(u.method, u.url, nil)
		w := httptest.NewRecorder()
		f.mux.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s -> %d", u.method, u.url, w.Code)
		}
	}
}

func TestAdminCRUD(t *testing.T) {
	f := openAdminMux(t, 1<<20, 1000)

	w := f.do(t, "POST", "/api/admin/movies", strings.NewReader(`{}`), "application/json")
	if w.Code != http.StatusCreated {
		t.Fatalf("blank create -> %d %s", w.Code, w.Body.String())
	}
	var blank database.Movie
	_ = json.NewDecoder(w.Body).Decode(&blank)
	if blank.Title == "" || blank.Slug == "" {
		t.Fatalf("placeholder missing: %+v", blank)
	}
	w = f.do(t, "POST", "/api/admin/movies",
		strings.NewReader(`{"title":"My Film!","description":"d","year":2024}`), "application/json")
	if w.Code != http.StatusCreated {
		t.Fatalf("create -> %d %s", w.Code, w.Body.String())
	}
	var m database.Movie
	_ = json.NewDecoder(w.Body).Decode(&m)
	if m.Slug != "my-film" || m.Status != "DRAFT" || m.Year == nil || *m.Year != 2024 {
		t.Fatalf("%+v", m)
	}
	idStr := strconv.FormatInt(m.ID, 10)

	w = f.do(t, "POST", "/api/admin/movies", strings.NewReader(`{"title":"My Film?"}`), "application/json")
	if w.Code != http.StatusConflict {
		t.Fatalf("dup slug -> %d", w.Code)
	}

	w = f.do(t, "GET", "/api/admin/movies", nil, "")
	var list []database.Movie
	_ = json.NewDecoder(w.Body).Decode(&list)
	if len(list) != 2 {
		t.Fatalf("%+v", list)
	}

	w = f.do(t, "PATCH", "/api/admin/movies/"+idStr,
		strings.NewReader(`{"title":"New Title","poster_url":"http://x/p.jpg"}`), "application/json")
	_ = json.NewDecoder(w.Body).Decode(&m)
	if m.Title != "New Title" || m.PosterURL == nil {
		t.Fatalf("%+v", m)
	}

	w = f.do(t, "DELETE", "/api/admin/movies/"+idStr, nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("delete -> %d", w.Code)
	}
	if w := f.do(t, "GET", "/api/admin/movies/"+idStr, nil, ""); w.Code != http.StatusNotFound {
		t.Fatalf("get deleted -> %d", w.Code)
	}
}

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"My Film!": "my-film", "  A  B  ": "a-b", "Film 2: X": "film-2-x", "---": "",
	} {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q", in, got)
		}
	}
}

func TestUploadHappyPath(t *testing.T) {
	f := openAdminMux(t, 1<<20, 1000)
	id := f.createMovie(t, "Upload Me")
	data := mp4Body(2500)

	body, ctype := uploadBody(t, "film.mp4", data)
	w := f.do(t, "POST", "/api/admin/movies/"+strconv.FormatInt(id, 10)+"/upload", body, ctype)
	if w.Code != http.StatusAccepted {
		t.Fatalf("upload -> %d %s", w.Code, w.Body.String())
	}
	st := f.waitStatus(t, id, "READY")

	// No Telegram locators or internals in the status payload.
	raw, _ := json.Marshal(st)
	for _, leak := range []string{"telegram", "message_id", "file_id"} {
		if strings.Contains(strings.ToLower(string(raw)), leak) {
			t.Fatalf("status leaks %q", leak)
		}
	}
	overall := st["overall"].(map[string]any)
	if overall["percent"].(float64) != 100 {
		t.Fatalf("%v", overall)
	}
	parts := st["parts"].([]any)
	if len(parts) != 3 {
		t.Fatalf("%d parts", len(parts))
	}
	// Temp cleaned after READY.
	if _, err := os.Stat(fmt.Sprintf("%s/upload-%d.bin", f.tmp, id)); !os.IsNotExist(err) {
		t.Fatal("temp not cleaned")
	}
	// Each part uploaded exactly once under its own name.
	if len(f.store.UploadCallsByName) != 3 {
		t.Fatalf("upload calls: %v", f.store.UploadCallsByName)
	}
	for _, n := range []string{"upload-me-part-1", "upload-me-part-2", "upload-me-part-3"} {
		if f.store.UploadCallsByName[n] != 1 {
			t.Fatalf("upload calls: %v", f.store.UploadCallsByName)
		}
	}
}

func TestUploadRejects(t *testing.T) {
	f := openAdminMux(t, 500, 1000) // tiny limit
	id := f.createMovie(t, "Rejects")

	// Missing file field.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("note", "x")
	_ = mw.Close()
	w := f.do(t, "POST", "/api/admin/movies/"+strconv.FormatInt(id, 10)+"/upload", &buf, mw.FormDataContentType())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("no file -> %d", w.Code)
	}

	// Oversize.
	body, ctype := uploadBody(t, "big.mp4", mp4Body(600))
	w = f.do(t, "POST", "/api/admin/movies/"+strconv.FormatInt(id, 10)+"/upload", body, ctype)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize -> %d", w.Code)
	}

	// Non-video.
	body, ctype = uploadBody(t, "note.txt", []byte("hello world, not a video at all...."))
	w = f.do(t, "POST", "/api/admin/movies/"+strconv.FormatInt(id, 10)+"/upload", body, ctype)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("text file -> %d", w.Code)
	}

	// MKV sniffed as octet-stream is accepted via extension mapping.
	mkv := make([]byte, 300)
	copy(mkv, []byte("random-bytes-not-video-sniffable.............."))
	body, ctype = uploadBody(t, "film.mkv", mkv)
	w = f.do(t, "POST", "/api/admin/movies/"+strconv.FormatInt(id, 10)+"/upload", body, ctype)
	if w.Code != http.StatusAccepted {
		t.Fatalf("mkv -> %d %s", w.Code, w.Body.String())
	}
	f.waitStatus(t, id, "READY")

	// Re-upload over READY rejected.
	body, ctype = uploadBody(t, "film.mp4", mp4Body(100))
	w = f.do(t, "POST", "/api/admin/movies/"+strconv.FormatInt(id, 10)+"/upload", body, ctype)
	if w.Code != http.StatusConflict {
		t.Fatalf("re-upload -> %d", w.Code)
	}
}

func TestUploadRetryFlow(t *testing.T) {
	f := openAdminMux(t, 1<<20, 1000)
	id := f.createMovie(t, "Flaky")
	idStr := strconv.FormatInt(id, 10)
	f.store.FailNextUploads = 100000 // everything fails

	body, ctype := uploadBody(t, "film.mp4", mp4Body(2500))
	if w := f.do(t, "POST", "/api/admin/movies/"+idStr+"/upload", body, ctype); w.Code != http.StatusAccepted {
		t.Fatalf("upload -> %d", w.Code)
	}
	f.waitStatus(t, id, "ERROR")

	// Retry while broken stays ERROR-ward; heal then retry to READY.
	// Each act waits for the owning job to finish first (see waitJobIdle).
	f.postRetry(t, id)
	if got := f.waitJobIdle(t, id); got != "ERROR" {
		t.Fatalf("status = %s, want ERROR", got)
	}
	f.store.FailNextUploads = 0
	f.postRetry(t, id)
	if got := f.waitJobIdle(t, id); got != "READY" {
		t.Fatalf("status = %s, want READY", got)
	}

	// Nothing left to retry.
	if w := f.do(t, "POST", "/api/admin/movies/"+idStr+"/retry", nil, ""); w.Code != http.StatusConflict {
		t.Fatalf("retry READY -> %d", w.Code)
	}
}

func TestDeleteCleansUp(t *testing.T) {
	f := openAdminMux(t, 1<<20, 1000)
	id := f.createMovie(t, "Delete Me")
	body, ctype := uploadBody(t, "film.mp4", mp4Body(1500))
	if w := f.do(t, "POST", "/api/admin/movies/"+strconv.FormatInt(id, 10)+"/upload", body, ctype); w.Code != http.StatusAccepted {
		t.Fatalf("upload -> %d", w.Code)
	}
	f.waitStatus(t, id, "READY")

	if w := f.do(t, "DELETE", "/api/admin/movies/"+strconv.FormatInt(id, 10), nil, ""); w.Code != http.StatusOK {
		t.Fatalf("delete -> %d", w.Code)
	}
	// Memory blobs gone (store.Delete called per part).
	if len(f.store.Get("mem-1"))+len(f.store.Get("mem-2")) != 0 {
		t.Fatal("blobs survived delete")
	}
	// Public catalog hides it.
	r := httptest.NewRequest("GET", "/api/movies", nil)
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "[]") {
		t.Fatalf("catalog: %s", w.Body.String())
	}
}
