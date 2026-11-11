package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exportDir builds the subset of a Next.js export these tests exercise. The
// real one comes from frontend/out; the shape that matters is the sentinel
// file per dynamic route.
func exportDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "<html>root</html>")
	write("404.html", "<html>missing</html>")
	write("home.html", "<html>home</html>")
	write("2.html", "<html>page 2</html>")
	write("2.txt", "rsc page 2")
	write("movies/_.html", "<html>movie shell</html>")
	write("movies/_.txt", "rsc movie")
	write("movies/_/__next._full.txt", "rsc movie full")
	write("series/_.html", "<html>series shell</html>")
	write("series.html", "<html>series index</html>")
	write("admin/movies/_.html", "<html>admin movie shell</html>")
	write("_next/static/chunk.js", "console.log(1)")
	return dir
}

func get(t *testing.T, dir, url string) *httptest.ResponseRecorder {
	t.Helper()
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-From-API", "1")
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	staticHandler(dir, api).ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
	return rec
}

func TestStaticServesExport(t *testing.T) {
	dir := exportDir(t)
	for _, tc := range []struct{ url, want string }{
		{"/", "<html>root</html>"},
		{"/home", "<html>home</html>"},
		{"/2", "<html>page 2</html>"},
		{"/series", "<html>series index</html>"},
		{"/movies/5", "<html>movie shell</html>"},
		{"/movies/9", "<html>movie shell</html>"},
		{"/series/log-horizon", "<html>series shell</html>"},
		{"/admin/movies/5", "<html>admin movie shell</html>"},
	} {
		rec := get(t, dir, tc.url)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", tc.url, rec.Code)
		}
		if got := rec.Body.String(); got != tc.want {
			t.Errorf("%s: body %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestStaticServesRSCPayload(t *testing.T) {
	dir := exportDir(t)
	rec := get(t, dir, "/movies/5?_rsc=abc")
	if got := rec.Body.String(); got != "rsc movie" {
		t.Errorf("payload = %q, want %q", got, "rsc movie")
	}
	rec = get(t, dir, "/2?_rsc=abc")
	if got := rec.Body.String(); got != "rsc page 2" {
		t.Errorf("payload = %q, want %q", got, "rsc page 2")
	}
}

func TestStaticPaginationFallsBackToSentinel(t *testing.T) {
	dir := exportDir(t)
	// Only /2 was prerendered; /7 must still resolve, and /1 must not.
	if rec := get(t, dir, "/7"); rec.Body.String() != "<html>page 2</html>" {
		t.Errorf("/7 = %q", rec.Body.String())
	}
	rec := get(t, dir, "/1")
	if rec.Code != http.StatusNotFound {
		t.Errorf("/1 status = %d, want 404", rec.Code)
	}
}

func TestStaticUnknownPathIs404(t *testing.T) {
	dir := exportDir(t)
	rec := get(t, dir, "/nope/deep")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if got := rec.Body.String(); got != "<html>missing</html>" {
		t.Errorf("body = %q, want the 404 shell", got)
	}
}

func TestStaticPassesAPIToBackend(t *testing.T) {
	dir := exportDir(t)
	rec := get(t, dir, "/api/movies")
	if rec.Header().Get("X-From-API") != "1" {
		t.Error("/api must reach the API handler, not the file server")
	}
}

func TestStaticRejectsTraversal(t *testing.T) {
	dir := exportDir(t)
	rec := get(t, dir, "/../secret")
	if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "secret") {
		t.Error("path traversal escaped the export root")
	}
}

func TestStaticCachesBuildAssetsHard(t *testing.T) {
	dir := exportDir(t)
	if cc := get(t, dir, "/_next/static/chunk.js").Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("build asset Cache-Control = %q, want immutable", cc)
	}
	if cc := get(t, dir, "/home").Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("html Cache-Control = %q, want no-cache", cc)
	}
}

func TestSentinelPath(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"5.html", "2.html"},
		{"2.html", "2.html"},
		{"1.html", "1.html"},
		{"movies/5.html", "movies/_.html"},
		{"movies/5.txt", "movies/_.txt"},
		{"movies/5/__next._full.txt", "movies/_/__next._full.txt"},
		{"admin/movies/12.html", "admin/movies/_.html"},
		{"series/log-horizon.html", "series/_.html"},
		{"series.html", "series.html"},
		{"admin.html", "admin.html"},
		{"_next/static/chunk.js", "_next/static/chunk.js"},
	} {
		if got := sentinelPath(tc.in); got != tc.want {
			t.Errorf("sentinelPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
