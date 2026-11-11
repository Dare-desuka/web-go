package api

import (
	"database/sql"
	"net/http"
	"strconv"
)

// handleGetMovie serves GET /api/movies/{id}; admins also see DRAFTs.
func handleGetMovie(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
			return
		}
		m, ok := resolveMovie(db, w, r, id)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, m)
	}
}
