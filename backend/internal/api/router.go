// Package api wires HTTP routes to repositories and services.
// No Telegram logic here: handlers depend on storage/streaming layers (later phases).
package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"web-go/backend/internal/database"
)

// Route registration lives in admin_auth.go (NewMux).

// pathID parses a positive integer route parameter.
func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

// resolveMovie loads a READY movie, falling back to any status when the
// request carries a valid admin session. Lets admins see and play
// not-yet-published films; everyone else still gets 404 for them.
func resolveMovie(db *sql.DB, w http.ResponseWriter, r *http.Request, id int64) (database.Movie, bool) {
	m, err := database.GetReadyMovieByID(r.Context(), db, id)
	if err == nil {
		return m, true
	}
	if !errors.Is(err, database.ErrNotFound) {
		slog.Error("movie lookup failed", "movie", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return database.Movie{}, false
	}
	if !isAdminSession(r, db) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
		return database.Movie{}, false
	}
	m, err = database.GetMovieByID(r.Context(), db, id)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
			return database.Movie{}, false
		}
		slog.Error("movie lookup failed", "movie", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return database.Movie{}, false
	}
	return m, true
}

// isAdminSession reports whether the request carries a valid admin session.
// Duplicates RequireAdmin's check without writing a response, for handlers
// that stay public but reveal more to admins.
func isAdminSession(r *http.Request, db *sql.DB) bool {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return false
	}
	s, err := database.GetSession(r.Context(), db, c.Value, database.NowMillis())
	if err != nil {
		return false
	}
	u, err := database.GetUserByID(r.Context(), db, s.UserID)
	if err != nil {
		return false
	}
	return u.Role == database.RoleAdmin
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleListMovies(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Admins see every status (hidden films appear on their home);
		// everyone else sees READY only.
		list := database.ListReady
		if isAdminSession(r, db) {
			list = database.ListAll
		}
		movies, err := list(r.Context(), db)
		if err != nil {
			slog.Error("list movies failed", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		writeJSON(w, http.StatusOK, movies)
	}
}
