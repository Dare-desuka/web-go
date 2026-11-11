package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSPreflight(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler must not run on preflight")
	})
	h := WithCORS(inner, SplitOrigins("http://127.0.0.1:3003, http://localhost:3003"))

	r := httptest.NewRequest(http.MethodOptions, "/api/admin/login", nil)
	r.Header.Set("Origin", "http://localhost:3003")
	r.Header.Set("Access-Control-Request-Method", "POST")
	r.Header.Set("Access-Control-Request-Headers", "content-type")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	res := w.Result()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", res.StatusCode)
	}
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "http://localhost:3003" {
		t.Fatalf("allow-origin = %q", got)
	}
	if got := res.Header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow-credentials = %q", got)
	}
}

func TestCORSPassthrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h := WithCORS(inner, SplitOrigins("http://127.0.0.1:3003"))

	for _, tc := range []struct {
		name   string
		origin string
		wantAO string
	}{
		{"allowed", "http://127.0.0.1:3003", "http://127.0.0.1:3003"},
		{"denied", "http://evil.example", ""},
		{"no origin", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/admin/login", nil)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			res := w.Result()
			if res.StatusCode != http.StatusTeapot {
				t.Fatalf("status = %d, want passthrough", res.StatusCode)
			}
			if got := res.Header.Get("Access-Control-Allow-Origin"); got != tc.wantAO {
				t.Fatalf("allow-origin = %q, want %q", got, tc.wantAO)
			}
		})
	}
}
