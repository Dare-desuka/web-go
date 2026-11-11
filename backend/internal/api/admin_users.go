package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"web-go/backend/internal/auth"
	"web-go/backend/internal/database"
)

// jsonDecode parses a small JSON body with a size bound.
func jsonDecode(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<16)).Decode(v)
}

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,32}$`)

// normalizeUsername trims + lowercases so "Orcaella" and "orcaella" are one
// account. Applied on register, login and admin-create alike.
func normalizeUsername(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func validUsername(s string) bool { return usernameRe.MatchString(s) }

func validPassword(s string) bool { return len(s) >= 8 && len(s) <= 256 }

func validRole(s string) bool { return s == database.RoleUser || s == database.RoleAdmin }

func userJSON(u database.User) map[string]any {
	return map[string]any{"id": u.ID, "username": u.Username, "role": u.Role}
}

func setSessionCookie(w http.ResponseWriter, opts Options, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   opts.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(auth.SessionTTL.Seconds()),
	})
}

// handleRegister creates a public account (always role "user"; any client
// role field is ignored) and logs it in immediately. Abused like login:
// same per-IP limiter, same generic failures.
func handleRegister(db *sql.DB, opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !opts.LoginLimiter.allow(ip) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, try later"})
			return
		}
		var req loginRequest
		if err := jsonDecode(r, &req); err != nil {
			opts.LoginLimiter.record(ip)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		username := normalizeUsername(req.Username)
		if !validUsername(username) || !validPassword(req.Password) {
			opts.LoginLimiter.record(ip)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username 3-32 [a-z 0-9 _ . -], password min 8 chars"})
			return
		}
		hash, err := auth.Hash(req.Password)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		id, err := database.CreateUser(r.Context(), db, username, hash, database.RoleUser)
		if err != nil {
			opts.LoginLimiter.record(ip)
			if errors.Is(err, database.ErrAlreadyExists) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "username taken"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		_ = database.DeleteExpiredSessions(r.Context(), db, database.NowMillis())
		s, err := database.CreateSession(r.Context(), db, id, database.NowMillis()+auth.SessionTTL.Milliseconds())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		opts.LoginLimiter.reset(ip)
		setSessionCookie(w, opts, s.ID)
		writeJSON(w, http.StatusCreated, map[string]any{"id": id, "username": username, "role": database.RoleUser})
	}
}

// handleListUsers lists accounts (hashes never serialize).
func handleListUsers(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := database.ListUsers(r.Context(), db)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		out := make([]map[string]any, 0, len(users))
		for _, u := range users {
			out = append(out, userJSON(u))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// handleCreateUser lets an admin create any role directly.
func handleCreateUser(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		if err := jsonDecode(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		username := normalizeUsername(req.Username)
		if !validUsername(username) || !validPassword(req.Password) || !validRole(req.Role) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad username, password or role"})
			return
		}
		hash, err := auth.Hash(req.Password)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		id, err := database.CreateUser(r.Context(), db, username, hash, req.Role)
		if err != nil {
			if errors.Is(err, database.ErrAlreadyExists) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "username taken"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		u, _ := database.GetUserByID(r.Context(), db, id)
		writeJSON(w, http.StatusCreated, userJSON(u))
	}
}

// handleSetUserRole promotes/demotes. Self-change is forbidden (anti lockout).
func handleSetUserRole(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}
		var req struct {
			Role string `json:"role"`
		}
		if err := jsonDecode(r, &req); err != nil || !validRole(req.Role) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be user or admin"})
			return
		}
		if cur, ok := CurrentUser(r); ok && cur.ID == id {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cannot change own role"})
			return
		}
		if err := database.UpdateUserRole(r.Context(), db, id, req.Role); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		u, _ := database.GetUserByID(r.Context(), db, id)
		writeJSON(w, http.StatusOK, userJSON(u))
	}
}

// handleDeleteUser removes an account (sessions cascade). No self-delete.
func handleDeleteUser(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}
		if cur, ok := CurrentUser(r); ok && cur.ID == id {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cannot delete own account"})
			return
		}
		if err := database.DeleteUser(r.Context(), db, id); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}
