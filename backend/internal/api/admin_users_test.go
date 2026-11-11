package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func register(t *testing.T, f *adminFixture, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/auth/register",
		strings.NewReader(`{"username":`+strconv.Quote(username)+`,"password":`+strconv.Quote(password)+`}`))
	r.RemoteAddr = "10.9.9.9:1"
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	return w
}

func TestRegisterHappyPath(t *testing.T) {
	f := openAdminMux(t, 1<<20, 1000)
	w := register(t, f, "NewUser", "longenoughpassword")
	if w.Code != http.StatusCreated {
		t.Fatalf("register -> %d %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["role"] != "user" {
		t.Fatalf("role = %v, want user (client must not escalate)", body["role"])
	}
	if len(w.Result().Cookies()) == 0 {
		t.Fatal("no session cookie (auto-login expected)")
	}
}

func TestRegisterValidation(t *testing.T) {
	f := openAdminMux(t, 1<<20, 1000)
	for _, tc := range []struct{ user, pass string }{
		{"ab", "longenoughpassword"},        // too short
		{"bad name!", "longenoughpassword"}, // charset
		{"fine-name", "short"},              // password
		{"", ""},
	} {
		if w := register(t, f, tc.user, tc.pass); w.Code != http.StatusBadRequest {
			t.Errorf("register(%q) -> %d, want 400", tc.user, w.Code)
		}
	}
	if w := register(t, f, "admin", "longenoughpassword"); w.Code != http.StatusConflict {
		t.Errorf("duplicate -> %d, want 409", w.Code)
	}
}

func TestNonAdminForbidden(t *testing.T) {
	f := openAdminMux(t, 1<<20, 1000)
	w := register(t, f, "plain", "longenoughpassword")
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == SessionCookie {
			cookie = c
		}
	}
	r := httptest.NewRequest("GET", "/api/admin/movies", nil)
	r.AddCookie(cookie)
	resp := httptest.NewRecorder()
	f.mux.ServeHTTP(resp, r)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("user hitting admin API -> %d, want 403", resp.Code)
	}
}

func userAdmin(t *testing.T, f *adminFixture, method, url, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, url, nil)
	} else {
		r = httptest.NewRequest(method, url, strings.NewReader(body))
	}
	r.AddCookie(f.cookie)
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	return w
}

func TestAdminUserCRUD(t *testing.T) {
	f := openAdminMux(t, 1<<20, 1000)

	w := userAdmin(t, f, "POST", "/api/admin/users", `{"username":"Crew","password":"longenoughpassword","role":"user"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create -> %d %s", w.Code, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	id := int64(created["id"].(float64))

	w = userAdmin(t, f, "GET", "/api/admin/users", "")
	var list []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 2 {
		t.Fatalf("users = %d, want admin+crew", len(list))
	}

	// Promote.
	w = userAdmin(t, f, "PATCH", "/api/admin/users/"+strconv.FormatInt(id, 10), `{"role":"admin"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("promote -> %d %s", w.Code, w.Body.String())
	}
	// Bad role rejected.
	w = userAdmin(t, f, "PATCH", "/api/admin/users/"+strconv.FormatInt(id, 10), `{"role":"root"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad role -> %d, want 400", w.Code)
	}
	// Self-demote forbidden (admin id is 1 in fixture).
	w = userAdmin(t, f, "PATCH", "/api/admin/users/1", `{"role":"user"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("self-demote -> %d, want 403", w.Code)
	}
	// Self-delete forbidden.
	w = userAdmin(t, f, "DELETE", "/api/admin/users/1", "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("self-delete -> %d, want 403", w.Code)
	}
	// Delete crew.
	w = userAdmin(t, f, "DELETE", "/api/admin/users/"+strconv.FormatInt(id, 10), "")
	if w.Code != http.StatusOK {
		t.Fatalf("delete -> %d %s", w.Code, w.Body.String())
	}
	// Unknown id.
	w = userAdmin(t, f, "DELETE", "/api/admin/users/999", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing -> %d, want 404", w.Code)
	}
}
