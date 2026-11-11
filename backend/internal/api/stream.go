package api

import (
	"database/sql"
	"log/slog"
	"net/http"
	"strconv"

	"web-go/backend/internal/database"
	"web-go/backend/internal/storage"
	"web-go/backend/internal/streaming"
)

// downloadName builds a safe attachment filename from slug + mime.
// Slug is already URL-safe (admin slugify); ext comes from a fixed map.
func downloadName(m database.Movie) string {
	ext := "bin"
	switch {
	case m.VideoMime == "video/x-matroska":
		ext = "mkv"
	case m.VideoMime == "video/webm":
		ext = "webm"
	case m.VideoMime == "video/x-msvideo":
		ext = "avi"
	case m.VideoMime == "video/quicktime":
		ext = "mov"
	case m.VideoMime == "video/mp4":
		ext = "mp4"
	}
	name := m.Slug
	if name == "" {
		name = "movie-" + strconv.FormatInt(m.ID, 10)
	}
	return strconv.Quote(name + "." + ext)
}

// handleStream serves GET /api/stream/movies/{id} as one continuous video.
// Only READY movies are reachable; part existence stays an internal detail.
//
//	200 no Range header (full body)
//	206 valid Range (single continuous body, possibly cross-part)
//	404 unknown id, bad id, or movie not READY (indistinguishable on purpose)
//	416 unsatisfiable Range (+ Content-Range: bytes */size)
//	503 storage unwired or part not COMPLETE (backend failures mid-body
//	abort the stream instead: headers are already sent, browsers resume
//	via Range)
//	500  database failure
//
// ?download=1 adds Content-Disposition: attachment for file download;
// the bytes and Range behavior are identical.
func handleStream(db *sql.DB, store storage.Storage) http.HandlerFunc {
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
		if m.VideoSize <= 0 {
			slog.Error("stream movie has no size", "movie", id)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		parts, err := database.ListParts(r.Context(), db, id)
		if err != nil {
			slog.Error("stream parts lookup failed", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}

		start, length, partial, err := streaming.ParseRange(r.Header.Get("Range"), m.VideoSize)
		if err != nil {
			w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(m.VideoSize, 10))
			writeJSON(w, http.StatusRequestedRangeNotSatisfiable, map[string]string{"error": "range not satisfiable"})
			return
		}
		segments, err := streaming.MapRange(start, start+length-1, parts)
		if err != nil {
			slog.Error("stream range unmapped", "movie", id, "err", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "storage unavailable"})
			return
		}
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "storage unavailable"})
			return
		}
		for _, sg := range segments {
			var p database.Part
			for _, part := range parts {
				if part.PartNumber == sg.PartNumber {
					p = part
					break
				}
			}
			if p.Status != database.PartComplete || p.TelegramFileID == nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "storage unavailable"})
				return
			}
		}

		w.Header().Set("Content-Type", m.VideoMime)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		// ?download=1 turns the same stream into a file download.
		// Range keeps working, so download managers can resume.
		if r.URL.Query().Get("download") != "" {
			w.Header().Set("Content-Disposition", "attachment; filename="+downloadName(m))
		}
		if partial {
			w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+
				strconv.FormatInt(start+length-1, 10)+"/"+strconv.FormatInt(m.VideoSize, 10))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		if err := streaming.Stream(r.Context(), w, store, parts, segments); err != nil {
			// Headers already sent; only logging remains possible.
			slog.Error("stream aborted", "movie", id, "err", err)
		} else {
			slog.Info("stream served", "movie", id, "bytes", length, "partial", partial)
		}
	}
}
