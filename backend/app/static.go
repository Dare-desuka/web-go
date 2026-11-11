package app

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// staticHandler serves the Next.js export in frontend/out next to the API, so
// the STB needs no Node process for the site.
//
// It handles one wrinkle the export cannot: every dynamic route is
// prerendered as a single sentinel file, because its page is a client
// component that resolves its own parameter from the URL. /movies/5 and
// /movies/9 therefore share out/movies/_.html.
func staticHandler(root string, api http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}

		rel := exportPath(r)
		if !exists(filepath.Join(root, filepath.FromSlash(rel))) {
			if fallback := sentinelPath(rel); fallback != rel &&
				exists(filepath.Join(root, filepath.FromSlash(fallback))) {
				rel = fallback
			}
		}

		if !exists(filepath.Join(root, filepath.FromSlash(rel))) {
			notFound(w, r, root)
			return
		}

		// Build hashes change with the bundle, so those files can be cached
		// hard; the HTML shells must not be, or a deploy strands browsers.
		if strings.HasPrefix(rel, "_next/static/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}

		// ServeContent rather than http.FileServer: FileServer redirects
		// /index.html to ./ and normalises directories, which breaks the
		// export's own file names.
		file, err := os.Open(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			notFound(w, r, root)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || info.IsDir() {
			notFound(w, r, root)
			return
		}
		http.ServeContent(w, r, filepath.Base(rel), info.ModTime(), file)
	})
}

// exportPath maps a request URL onto a file inside the export directory.
// path.Clean collapses "..", so a request can never climb out of root.
func exportPath(r *http.Request) string {
	rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if rel == "" {
		return "index.html"
	}
	// Client-side navigation asks for the prerendered RSC payload instead of
	// a document. The URL may already name the segment file (e.g.
	// home/__next._tree.txt?_rsc=...); only append .txt when there is no
	// extension yet, otherwise every segment request doubled the suffix and
	// 404'd.
	if _, ok := r.URL.Query()["_rsc"]; ok && path.Ext(rel) == "" {
		return rel + ".txt"
	}
	if path.Ext(rel) == "" {
		return rel + ".html"
	}
	return rel
}

// sentinelPath rewrites the parameter of a dynamic route to the sentinel the
// export was built with, leaving the rest of the path alone so segment
// payloads like movies/5/__next.movies.$d$id.__PAGE__.txt still resolve.
func sentinelPath(rel string) string {
	ext := path.Ext(rel)
	base := strings.TrimSuffix(rel, ext)
	segs := strings.Split(base, "/")

	switch {
	case len(segs) == 1 && isPageNumber(segs[0]):
		segs[0] = "2"
	case len(segs) > 2 && segs[0] == "admin" && segs[1] == "movies":
		segs[2] = "_"
	case len(segs) > 1 && (segs[0] == "movies" || segs[0] == "series"):
		segs[1] = "_"
	}
	return strings.Join(segs, "/") + ext
}

// isPageNumber matches the paginated home route, which only exists for n >= 2.
func isPageNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	n, err := strconv.Atoi(s)
	return err == nil && n >= 2
}

func notFound(w http.ResponseWriter, r *http.Request, root string) {
	file, err := os.Open(filepath.Join(root, "404.html"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusNotFound)
	http.ServeContent(w, r, "404.html", info.ModTime(), file)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
