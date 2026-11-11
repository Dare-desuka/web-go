package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"web-go/backend/internal/auth"
	"web-go/backend/internal/database"
	"web-go/backend/internal/storage"
)

// SessionCookie is the only auth state the browser holds (PRD 28.6).
const SessionCookie = "admin_session"

// Options tunes handler behavior without a config dependency.
type Options struct {
	// CookieSecure enables Secure on the session cookie (production).
	CookieSecure bool
	// LoginLimiter caps failed logins; nil disables (tests use their own).
	LoginLimiter *loginLimiter
	// Store serves video bytes. Nil until production wiring (F6+):
	// the stream endpoint answers 503 when unset.
	Store storage.Storage
	// TmpDir holds in-flight uploads until READY.
	TmpDir string
	// MaxMovieSize bounds one upload (bytes); MaxPartSize splits it.
	MaxMovieSize int64
	MaxPartSize  int64
	// UploadRetries bounds per-part attempts (0 = pipeline default);
	// UploadBackoff scales retry waits (0 = pipeline default).
	UploadRetries int
	UploadBackoff time.Duration
	// DataDir stores subtitles/posters (default "data").
	DataDir string
}

type ctxKey struct{}

// CurrentUser returns the admin authenticated by RequireAdmin.
func CurrentUser(r *http.Request) (database.User, bool) {
	u, ok := r.Context().Value(ctxKey{}).(database.User)
	return u, ok
}

// NewMux registers all routes. db is the migrated SQLite pool.
func NewMux(db *sql.DB, opts Options) *http.ServeMux {
	if opts.LoginLimiter == nil {
		opts.LoginLimiter = newLoginLimiter(10, 5*time.Minute)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /api/movies", handleListMovies(db))
	mux.HandleFunc("GET /api/movies/{id}", handleGetMovie(db))
	mux.HandleFunc("GET /api/subtitles/{movieID}", handleListSubtitles(db))
	mux.HandleFunc("GET /api/subtitles/{movieID}/{id}", handleServeSubtitle(db, opts.Store, opts.DataDir))
	mux.HandleFunc("GET /api/stream/movies/{id}", handleStream(db, opts.Store))
	mux.HandleFunc("POST /api/admin/login", handleLogin(db, opts))
	mux.HandleFunc("POST /api/admin/logout", handleLogout(db))
	mux.HandleFunc("POST /api/auth/register", handleRegister(db, opts))
	mux.HandleFunc("GET /api/admin/me", handleMeSelf(db))

	admin := &adminAPI{db: db, opts: opts}
	mux.HandleFunc("GET /api/admin/movies", RequireAdmin(db, admin.handleList))
	mux.HandleFunc("POST /api/admin/movies", RequireAdmin(db, admin.handleCreate))
	mux.HandleFunc("GET /api/admin/movies/{id}", RequireAdmin(db, admin.handleGet))
	mux.HandleFunc("PATCH /api/admin/movies/{id}", RequireAdmin(db, admin.handlePatch))
	mux.HandleFunc("DELETE /api/admin/movies/{id}", RequireAdmin(db, admin.handleDelete))
	mux.HandleFunc("POST /api/admin/movies/{id}/upload", RequireAdmin(db, admin.handleUpload))
	mux.HandleFunc("GET /api/admin/movies/{id}/upload", RequireAdmin(db, admin.handleUploadStatus))
	mux.HandleFunc("POST /api/admin/movies/{id}/retry", RequireAdmin(db, admin.handleRetry))
	mux.HandleFunc("POST /api/admin/movies/{id}/thumbnail", RequireAdmin(db, admin.handleGenerateThumb))
	mux.HandleFunc("POST /api/admin/movies/{id}/subtitles", RequireAdmin(db, admin.handleUploadSubtitle))
	mux.HandleFunc("GET /api/admin/movies/{id}/subtitles", RequireAdmin(db, admin.handleAdminListSubtitles))
	mux.HandleFunc("DELETE /api/admin/subtitles/{subID}", RequireAdmin(db, admin.handleDeleteSubtitle))
	mux.HandleFunc("POST /api/admin/movies/{id}/poster", RequireAdmin(db, admin.handleUploadPoster("poster")))
	mux.HandleFunc("POST /api/admin/movies/{id}/backdrop", RequireAdmin(db, admin.handleUploadPoster("backdrop")))
	mux.HandleFunc("GET /api/posters/{movieID}/{kind}", handleServePoster(db, opts))
	mux.HandleFunc("GET /api/thumbs/{movieID}", handleServeThumb(db, opts))
	mux.HandleFunc("POST /api/admin/movies/{id}/publish", RequireAdmin(db, admin.handlePublish))
	mux.HandleFunc("POST /api/admin/movies/{id}/unpublish", RequireAdmin(db, admin.handleUnpublish))

	mux.HandleFunc("GET /api/admin/users", RequireAdmin(db, handleListUsers(db)))
	mux.HandleFunc("POST /api/admin/users", RequireAdmin(db, handleCreateUser(db)))
	mux.HandleFunc("PATCH /api/admin/users/{id}", RequireAdmin(db, handleSetUserRole(db)))
	mux.HandleFunc("DELETE /api/admin/users/{id}", RequireAdmin(db, handleDeleteUser(db)))
	return mux
}

// RequireAdmin gates admin handlers: valid unexpired session cookie required,
// plus role "admin". Logged-in non-admins get 403 (PRD 28.5a).
// (/me is the exception: it answers 200 null, see handleMeSelf.)
func RequireAdmin(db *sql.DB, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := sessionUser(r, db)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if u.Role != database.RoleAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}
}

// sessionUser resolves the request's session cookie to its user.
// ok=false covers missing/invalid/expired sessions and unknown users.
func sessionUser(r *http.Request, db *sql.DB) (database.User, bool) {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return database.User{}, false
	}
	s, err := database.GetSession(r.Context(), db, c.Value, database.NowMillis())
	if err != nil {
		return database.User{}, false
	}
	u, err := database.GetUserByID(r.Context(), db, s.UserID)
	if err != nil {
		return database.User{}, false
	}
	return u, true
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func handleLogin(db *sql.DB, opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !opts.LoginLimiter.allow(ip) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, try later"})
			return
		}
		var req loginRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		req.Username = normalizeUsername(req.Username)
		if req.Username == "" || req.Password == "" {
			opts.LoginLimiter.record(ip)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			return
		}

		u, err := database.GetUserByUsername(r.Context(), db, req.Username)
		if err != nil {
			// Same response as bad password; dummy verify keeps timing flat.
			_ = auth.Verify(dummyHash(), req.Password)
			opts.LoginLimiter.record(ip)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			return
		}
		if err := auth.Verify(u.PasswordHash, req.Password); err != nil {
			slog.Info("admin login failed", "username", req.Username)
			opts.LoginLimiter.record(ip)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			return
		}

		_ = database.DeleteExpiredSessions(r.Context(), db, database.NowMillis())
		expiresAt := database.NowMillis() + auth.SessionTTL.Milliseconds()
		s, err := database.CreateSession(r.Context(), db, u.ID, expiresAt)
		if err != nil {
			slog.Error("create session failed", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		opts.LoginLimiter.reset(ip)
		http.SetCookie(w, &http.Cookie{
			Name:     SessionCookie,
			Value:    s.ID,
			Path:     "/",
			HttpOnly: true,
			Secure:   opts.CookieSecure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int(auth.SessionTTL.Seconds()),
		})
		writeJSON(w, http.StatusOK, map[string]any{"id": u.ID, "username": u.Username, "role": u.Role})
	}
}

// dummyHash returns a valid-format hash so unknown-user timing matches a
// real verification. Computed once per call site use; cost ≈ one login.
var dummyHashOnce = func() string {
	h, _ := auth.Hash("dummy-password-for-timing-parity")
	return h
}()

func dummyHash() string { return dummyHashOnce }

func handleLogout(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(SessionCookie); err == nil && c.Value != "" {
			_ = database.DeleteSession(r.Context(), db, c.Value)
		}
		http.SetCookie(w, &http.Cookie{
			Name:     SessionCookie,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// handleMeSelf answers the session check the frontend runs on every page.
// Admins get their user JSON; everyone else (anonymous, expired, non-admin)
// gets 200 null — same meaning as 401 to the frontend's me() helper, but
// browsers don't log it as a console error.
// ponytail: null body, not {}, so r.ok-then-parse still yields null.
func handleMeSelf(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if u, ok := sessionUser(r, db); ok && u.Role == database.RoleAdmin {
			writeJSON(w, http.StatusOK, map[string]any{"id": u.ID, "username": u.Username, "role": u.Role})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("null"))
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// loginLimiter caps failed logins per IP in a sliding window.
type loginLimiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	failures map[string][]time.Time
}

func newLoginLimiter(max int, window time.Duration) *loginLimiter {
	return &loginLimiter{max: max, window: window, failures: map[string][]time.Time{}}
}

func (l *loginLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-l.window)
	kept := l.failures[ip][:0]
	for _, t := range l.failures[ip] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.failures[ip] = kept
	return len(kept) < l.max
}

func (l *loginLimiter) record(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures[ip] = append(l.failures[ip], time.Now())
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, ip)
}
