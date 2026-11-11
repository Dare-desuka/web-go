package api

import (
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"web-go/backend/internal/database"
	"web-go/backend/internal/storage"
)

// Subtitle file serving caps at 5 MiB: tracks are kilobytes, anything
// bigger is a planted file, not a subtitle.
const maxSubtitleBytes = 5 << 20

// readyOr404 loads a READY movie or writes 404. Admin sessions also see
// DRAFTs, so admins can check subtitles before publishing. Subtitles of
// incomplete uploads stay invisible to everyone else.
func readyOr404(db *sql.DB, w http.ResponseWriter, r *http.Request) (database.Movie, bool) {
	movieID, ok := pathID(r, "movieID")
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
		return database.Movie{}, false
	}
	return resolveMovie(db, w, r, movieID)
}

// handleListSubtitles serves GET /api/subtitles/{movieID}.
// Only languages that exist are listed; file paths never leave the server.
func handleListSubtitles(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m, ok := readyOr404(db, w, r)
		if !ok {
			return
		}
		subs, err := database.ListSubtitles(r.Context(), db, m.ID)
		if err != nil {
			slog.Error("subtitle list failed", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		if subs == nil {
			subs = []database.Subtitle{}
		}
		writeJSON(w, http.StatusOK, subs)
	}
}

// handleServeSubtitle serves GET /api/subtitles/{movieID}/{id} as WebVTT.
// The file path / Telegram file_id always comes from the database row scoped
// to the movie, so request input can never traverse the filesystem.
// dataDir pins relative file_path rows back into the service tree so the
// suite binary can run from any cwd.
func handleServeSubtitle(db *sql.DB, store storage.Storage, dataDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m, ok := readyOr404(db, w, r)
		if !ok {
			return
		}
		subID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || subID <= 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "subtitle not found"})
			return
		}
		sub, err := database.GetSubtitleByID(r.Context(), db, subID)
		if err == nil && sub.MovieID != m.ID {
			err = database.ErrNotFound
		}
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "subtitle not found"})
				return
			}
			slog.Error("subtitle lookup failed", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		// Subtitle tracks are fetched cross-origin by <track>; allow any origin
		// so the browser can actually load and show them (anonymous, no creds).
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if store != nil && sub.TelegramFileID != nil {
			rc, err := store.DownloadRange(r.Context(), storage.File{
				ID: *sub.TelegramFileID, MessageID: deref64(sub.TelegramMessageID),
			}, 0, maxSubtitleBytes+1)
			if err != nil {
				slog.Error("subtitle download from storage failed", "movie", m.ID, "err", err)
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "subtitle not found"})
				return
			}
			defer rc.Close()
			if _, err := io.Copy(w, io.LimitReader(rc, maxSubtitleBytes+1)); err != nil {
				slog.Error("subtitle serve failed", "err", err)
			}
			return
		}
		// Local disk fallback (older rows / dev without Telegram creds).
		fp := subtitleDiskPath(dataDir, sub.FilePath)
		f, err := os.Open(fp)
		if err != nil {
			slog.Error("subtitle file missing", "movie", m.ID, "path", fp, "err", err)
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "subtitle not found"})
			return
		}
		defer f.Close()
		if _, err := io.Copy(w, io.LimitReader(f, maxSubtitleBytes+1)); err != nil {
			slog.Error("subtitle serve failed", "err", err)
		}
	}
}

// subtitleDiskPath resolves a possibly-relative subtitle file_path against
// dataDir/subtitles. Absolute paths are returned unchanged.
func subtitleDiskPath(dataDir, filePath string) string {
	if filepath.IsAbs(filePath) {
		return filePath
	}
	return filepath.Join(dataDir, "subtitles", filepath.Base(filePath))
}
