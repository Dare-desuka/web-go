package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"web-go/backend/internal/auth"
	"web-go/backend/internal/database"
)

func openAuthMux(t *testing.T) (*http.ServeMux, *sql.DB) {
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
	return NewMux(db, Options{LoginLimiter: newLoginLimiter(100, time.Minute)}), db
}

func doLogin(t *testing.T, mux *http.ServeMux, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/admin/login", strings.NewReader(body))
	r.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func sessionCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == SessionCookie {
			return c
		}
	}
	return nil
}

func TestLoginSuccessAndMe(t *testing.T) {
	mux, _ := openAuthMux(t)
	w := doLogin(t, mux, `{"username":"admin","password":"s3cret"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	c := sessionCookie(w)
	if c == nil {
		t.Fatal("no session cookie set")
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Value == "" {
		t.Fatalf("weak cookie: %+v", c)
	}

	r := httptest.NewRequest("GET", "/api/admin/me", nil)
	r.AddCookie(c)
	mw := httptest.NewRecorder()
	mux.ServeHTTP(mw, r)
	if mw.Code != http.StatusOK {
		t.Fatalf("me status = %d", mw.Code)
	}
	var me map[string]any
	if err := json.NewDecoder(mw.Body).Decode(&me); err != nil {
		t.Fatal(err)
	}
	if me["username"] != "admin" {
		t.Fatalf("me = %v", me)
	}
	// Password hash must never leak.
	if strings.Contains(mw.Body.String(), "argon2") {
		t.Fatal("hash leaked in /me response")
	}
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	mux, db := openAuthMux(t)
	for _, body := range []string{
		`{"username":"admin","password":"wrong"}`,
		`{"username":"ghost","password":"whatever"}`,
		`{"username":"","password":""}`,
		`not-json`,
	} {
		w := doLogin(t, mux, body)
		if w.Code != http.StatusUnauthorized && w.Code != http.StatusBadRequest {
			t.Fatalf("%q -> %d", body, w.Code)
		}
		if sessionCookie(w) != nil {
			t.Fatalf("%q set a session cookie", body)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("sessions created by failed logins")
	}
}

func TestMeRequiresAuth(t *testing.T) {
	mux, _ := openAuthMux(t)
	for _, c := range []*http.Cookie{nil, {Name: SessionCookie, Value: "garbage"}} {
		r := httptest.NewRequest("GET", "/api/admin/me", nil)
		if c != nil {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "null" {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	mux, _ := openAuthMux(t)
	w := doLogin(t, mux, `{"username":"admin","password":"s3cret"}`)
	c := sessionCookie(w)
	if c == nil {
		t.Fatal("no session cookie")
	}

	r := httptest.NewRequest("POST", "/api/admin/logout", nil)
	r.AddCookie(c)
	lw := httptest.NewRecorder()
	mux.ServeHTTP(lw, r)
	if lw.Code != http.StatusOK {
		t.Fatalf("logout status = %d", lw.Code)
	}

	r = httptest.NewRequest("GET", "/api/admin/me", nil)
	r.AddCookie(c)
	mw := httptest.NewRecorder()
	mux.ServeHTTP(mw, r)
	if mw.Code != http.StatusOK || strings.TrimSpace(mw.Body.String()) != "null" {
		t.Fatalf("session valid after logout: %d %s", mw.Code, mw.Body.String())
	}
}

func TestExpiredSessionRejected(t *testing.T) {
	mux, db := openAuthMux(t)
	u, _ := database.GetUserByUsername(context.Background(), db, "admin")
	s, err := database.CreateSession(context.Background(), db, u.ID, database.NowMillis()-1)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/admin/me", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: s.ID})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "null" {
		t.Fatalf("expired session status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestLoginRateLimited(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	mux := NewMux(db, Options{LoginLimiter: newLoginLimiter(2, time.Minute)})

	for i := 0; i < 2; i++ {
		r := httptest.NewRequest("POST", "/api/admin/login", strings.NewReader(`{"username":"nobody","password":"x"}`))
		r.RemoteAddr = "10.9.9.9:1"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d", i, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/api/admin/login", strings.NewReader(`{"username":"nobody","password":"x"}`))
	r.RemoteAddr = "10.9.9.9:1"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("throttled attempt = %d, want 429", w.Code)
	}
}
