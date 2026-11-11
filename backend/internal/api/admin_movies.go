package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"web-go/backend/internal/database"
	"web-go/backend/internal/storage"
	"web-go/backend/internal/upload"
)

// slugify turns a title into a URL-safe slug.
func slugify(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteRune('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// partDTO exposes part progress without Telegram locators (never leave server).
type partDTO struct {
	PartNumber  int64  `json:"part_number"`
	Size        int64  `json:"size"`
	StartOffset int64  `json:"start_offset"`
	EndOffset   int64  `json:"end_offset"`
	Status      string `json:"status"`
	RetryCount  int64  `json:"retry_count"`
	LastError   string `json:"last_error,omitempty"`
}

func toPartDTO(p database.Part) partDTO {
	d := partDTO{
		PartNumber: p.PartNumber, Size: p.Size,
		StartOffset: p.StartOffset, EndOffset: p.EndOffset,
		Status: p.Status, RetryCount: p.RetryCount,
	}
	if p.LastError != nil {
		d.LastError = *p.LastError
	}
	return d
}

// uploadJob tracks one movie's background pipeline for status polling.
type uploadJob struct {
	mu           sync.Mutex
	running      bool
	currentPart  int64
	currentBytes int64
	currentTotal int64
	lastErr      string
}

func (j *uploadJob) start() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.running {
		return false
	}
	j.running = true
	j.lastErr = ""
	return true
}

func (j *uploadJob) progress(part, uploaded, total int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.currentPart, j.currentBytes, j.currentTotal = part, uploaded, total
}

func (j *uploadJob) finish(errMsg string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.running = false
	j.lastErr = errMsg
}

func (j *uploadJob) snapshot() (running bool, part, b, total int64, errMsg string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running, j.currentPart, j.currentBytes, j.currentTotal, j.lastErr
}

type jobRegistry struct {
	mu   sync.Mutex
	jobs map[int64]*uploadJob
}

func (r *jobRegistry) get(id int64) *uploadJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.jobs == nil {
		r.jobs = map[int64]*uploadJob{}
	}
	j, ok := r.jobs[id]
	if !ok {
		j = &uploadJob{}
		r.jobs[id] = j
	}
	return j
}

// adminAPI bundles admin handler dependencies.
type adminAPI struct {
	db   *sql.DB
	opts Options
	jobs jobRegistry
}

func tmpPath(tmpDir string, movieID int64) string {
	return filepath.Join(tmpDir, fmt.Sprintf("upload-%d.bin", movieID))
}

// mimeFor prefers sniffed video/*, else maps trusted container extensions.
// http.DetectContentType does not know .mkv (reports octet-stream).
func mimeFor(filename, sniffed string) string {
	if strings.HasPrefix(sniffed, "video/") {
		return sniffed
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".mkv":
		return "video/x-matroska"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	default:
		return sniffed
	}
}

type createMovieRequest struct {
	Title       string  `json:"title"`
	Slug        string  `json:"slug"`
	Description string  `json:"description"`
	Year        *int64  `json:"year"`
	PosterURL   *string `json:"poster_url"`
	BackdropURL *string `json:"backdrop_url"`
	Genres      *string `json:"genres"`
}

var allowedGenres = map[string]bool{
	"comedy": true, "drama": true, "romance": true, "fantasy": true, "crime": true, "horror": true,
}

func normalizeGenres(s string) (string, bool) {
	if strings.TrimSpace(s) == "" {
		return "", true
	}
	parts := strings.Split(s, ",")
	seen := map[string]bool{}
	var out []string
	for _, p := range parts {
		g := strings.ToLower(strings.TrimSpace(p))
		if g == "" {
			continue
		}
		if !allowedGenres[g] {
			return "", false
		}
		if seen[g] {
			continue
		}
		seen[g] = true
		out = append(out, g)
		if len(out) > 5 {
			return "", false
		}
	}
	return strings.Join(out, ","), true
}

func (a *adminAPI) handleList(w http.ResponseWriter, r *http.Request) {
	movies, err := database.ListAll(r.Context(), a.db)
	if err != nil {
		slog.Error("admin list failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if movies == nil {
		movies = []database.Movie{}
	}
	writeJSON(w, http.StatusOK, movies)
}

func (a *adminAPI) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createMovieRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		// One-click create from the dashboard: fill a placeholder, the
		// admin renames it on the detail page. Suffix keeps slug unique.
		req.Title = "Film Baru"
	}
	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = slugify(req.Title)
		if req.Title == "Film Baru" {
			slug += "-" + strconv.FormatInt(database.NowMillis(), 36)
		}
	}
	if slug == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unusable title for slug"})
		return
	}
	genres := ""
	if req.Genres != nil {
		var ok bool
		if genres, ok = normalizeGenres(*req.Genres); !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid genres"})
			return
		}
	}
	m := database.Movie{
		Title: strings.TrimSpace(req.Title), Slug: slug,
		Description: req.Description, Year: req.Year,
		PosterURL: req.PosterURL, BackdropURL: req.BackdropURL,
		Genres: genres,
	}
	id, err := database.CreateMovie(r.Context(), a.db, m)
	if err != nil {
		if errors.Is(err, database.ErrAlreadyExists) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "slug already exists"})
			return
		}
		slog.Error("admin create failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	m, err = database.GetMovieByID(r.Context(), a.db, id)
	if err != nil {
		slog.Error("admin create reread failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (a *adminAPI) handleGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
		return
	}
	m, err := database.GetMovieByID(r.Context(), a.db, id)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
			return
		}
		slog.Error("admin get failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (a *adminAPI) handlePatch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
		return
	}
	var req createMovieRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	m, err := database.GetMovieByID(r.Context(), a.db, id)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if req.Title != "" {
		m.Title = strings.TrimSpace(req.Title)
	}
	if req.Slug != "" {
		m.Slug = strings.TrimSpace(req.Slug)
	}
	// MVP: description always overwritten (no null-vs-empty distinction).
	m.Description = req.Description
	if req.Year != nil {
		m.Year = req.Year
	}
	if req.PosterURL != nil {
		m.PosterURL = req.PosterURL
	}
	if req.BackdropURL != nil {
		m.BackdropURL = req.BackdropURL
	}
	if req.Genres != nil {
		var ok bool
		if m.Genres, ok = normalizeGenres(*req.Genres); !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid genres"})
			return
		}
	}
	// Description: empty string clears; absence indistinguishable in MVP (note).
	if err := database.UpdateMovieMeta(r.Context(), a.db, id, m); err != nil {
		if errors.Is(err, database.ErrAlreadyExists) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "slug already exists"})
			return
		}
		if errors.Is(err, database.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	m, _ = database.GetMovieByID(r.Context(), a.db, id)
	writeJSON(w, http.StatusOK, m)
}

func (a *adminAPI) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
		return
	}
	if _, err := database.GetMovieByID(r.Context(), a.db, id); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	// Best-effort Telegram cleanup; DB delete proceeds regardless (logged).
	if a.opts.Store != nil {
		parts, err := database.ListParts(r.Context(), a.db, id)
		if err == nil {
			for _, p := range parts {
				if p.Status != database.PartComplete || p.TelegramFileID == nil {
					continue
				}
				loc := storage.File{ID: *p.TelegramFileID}
				if p.TelegramMessageID != nil {
					loc.MessageID = *p.TelegramMessageID
				}
				if err := a.opts.Store.Delete(r.Context(), loc); err != nil {
					slog.Error("telegram delete failed", "movie", id, "part", p.PartNumber, "err", err)
				}
			}
		}
	}
	_ = os.Remove(tmpPath(a.opts.TmpDir, id))
	removeMovieFiles(r.Context(), a.db, a.opts, id)
	if err := database.DeleteMovie(r.Context(), a.db, id); err != nil {
		slog.Error("admin delete failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleUpload receives the video file and runs the pipeline in background.
// Temp file stays until READY so part-level retry never needs re-upload.
func (a *adminAPI) handleUpload(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
		return
	}
	m, err := database.GetMovieByID(r.Context(), a.db, id)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if m.Status == database.StatusREADY {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "movie already ready; delete first to replace"})
		return
	}
	if a.opts.Store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "storage unavailable"})
		return
	}
	job := a.jobs.get(id)
	if !job.start() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "upload already running"})
		return
	}
	fail := func(code int, msg string) {
		job.finish(msg)
		writeJSON(w, code, map[string]string{"error": msg})
	}

	r.Body = http.MaxBytesReader(w, r.Body, a.opts.MaxMovieSize+16<<20)
	file, header, err := r.FormFile("video")
	if err != nil {
		// Log the real cause server-side: the client message stays generic,
		// but "unexpected EOF" (dropped connection) vs "no such file"
		// (empty field) need very different follow-ups.
		slog.Warn("upload form parse failed", "movie", id, "err", err)
		if strings.Contains(err.Error(), "too large") {
			fail(http.StatusRequestEntityTooLarge, "file too large")
		} else {
			fail(http.StatusBadRequest, "missing video file")
		}
		return
	}
	defer file.Close()
	// Drop Go's multipart spool files (full body copy on disk): without this
	// every rejected upload leaks gigabytes in TMPDIR.
	defer r.MultipartForm.RemoveAll()

	if err := os.MkdirAll(a.opts.TmpDir, 0o755); err != nil {
		fail(http.StatusInternalServerError, "internal error")
		return
	}
	path := tmpPath(a.opts.TmpDir, id)
	tmp, err := os.Create(path)
	if err != nil {
		fail(http.StatusInternalServerError, "internal error")
		return
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	mime := mimeFor(header.Filename, http.DetectContentType(head[:n]))
	size, err := io.CopyN(tmp, io.MultiReader(bytes.NewReader(head[:n]), file), a.opts.MaxMovieSize+1)
	tmp.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		_ = os.Remove(path)
		fail(http.StatusBadRequest, "cannot read upload")
		return
	}
	if size > a.opts.MaxMovieSize {
		_ = os.Remove(path)
		fail(http.StatusRequestEntityTooLarge, "file too large")
		return
	}
	if err := upload.Validate(size, mime, a.opts.MaxMovieSize); err != nil {
		_ = os.Remove(path)
		fail(http.StatusBadRequest, "invalid video file")
		return
	}
	if err := database.UpdateMovieSource(r.Context(), a.db, id, size, mime); err != nil {
		_ = os.Remove(path)
		fail(http.StatusInternalServerError, "internal error")
		return
	}

	go a.runPipeline(id, path, mime)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (a *adminAPI) runPipeline(movieID int64, path, _ string) {
	job := a.jobs.get(movieID)
	ctx := context.Background()
	finishErr := ""
	defer func() {
		// Temp survives ERROR for retry; only READY cleans up.
		if m, err := database.GetMovieByID(ctx, a.db, movieID); err == nil && m.Status == database.StatusREADY {
			_ = os.Remove(path)
		}
		job.finish(finishErr)
	}()

	src, err := os.Open(path)
	if err != nil {
		finishErr = "lost upload file"
		slog.Error("pipeline open failed", "movie", movieID, "err", err)
		return
	}
	defer src.Close()

	opts := upload.Options{
		Progress:    job.progress,
		MaxRetries:  a.opts.UploadRetries,
		BackoffBase: a.opts.UploadBackoff,
	}
	if _, err := upload.Prepare(ctx, a.db, movieID, a.opts.MaxMovieSize, a.opts.MaxPartSize); err != nil {
		finishErr = "prepare failed"
		slog.Error("pipeline prepare failed", "movie", movieID, "err", err)
		return
	}
	if err := upload.UploadParts(ctx, a.db, a.opts.Store, movieID, src, opts); err != nil {
		slog.Error("pipeline upload incomplete", "movie", movieID, "err", err)
	}
	// Auto thumbnail from the local temp (still present; cleanup only
	// removes it after READY). Best-effort and placed BEFORE Finalize so a
	// slow ffmpeg never delays the READY state or temp cleanup.
	// Generated once, then stored to Telegram (survives disk loss) with a
	// disk fallback for older rows.
	if err := GenerateThumbnail(ctx, a.opts, movieID, path); err != nil {
		slog.Warn("pipeline thumbnail skipped", "movie", movieID, "err", err)
	} else {
		func() {
			f, err := os.Open(thumbPath(a.opts, movieID))
			if err != nil {
				return
			}
			defer f.Close()
			st, err := f.Stat()
			if err != nil || st.Size() == 0 || st.Size() > 10<<20 {
				return
			}
			file, err := a.opts.Store.Upload(ctx, f, storage.Metadata{
				Name: thumbFileName(movieID), Size: st.Size(), Mime: "image/jpeg",
			})
			if err != nil {
				slog.Warn("thumb telegram store failed", "movie", movieID, "err", err)
				return
			}
			if err := database.SetThumbTelegram(ctx, a.db, movieID, file.ID, file.MessageID); err != nil {
				slog.Warn("thumb telegram save failed", "movie", movieID, "err", err)
			} else {
				slog.Info("thumb telegram stored", "movie", movieID)
			}
		}()
	}
	// Same deal for embedded text subtitles: extract to .vtt unless the
	// language already exists (manual uploads win).
	if n, err := ExtractSubtitles(ctx, a.db, a.opts, movieID, path); err != nil {
		slog.Warn("pipeline subtitles skipped", "movie", movieID, "err", err)
	} else if n > 0 {
		slog.Info("pipeline subtitles stored", "movie", movieID, "count", n)
	}
	if err := upload.Finalize(ctx, a.db, movieID); err != nil {
		finishErr = "finalize failed"
		slog.Error("pipeline finalize failed", "movie", movieID, "err", err)
		return
	}
	slog.Info("pipeline done", "movie", movieID)
}

// handleRetry re-runs a failed pipeline from stored temp + COMPLETE parts.
func (a *adminAPI) handleRetry(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
		return
	}
	m, err := database.GetMovieByID(r.Context(), a.db, id)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if m.Status != database.StatusERROR {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "nothing to retry"})
		return
	}
	if a.opts.Store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "storage unavailable"})
		return
	}
	path := tmpPath(a.opts.TmpDir, id)
	if _, err := os.Stat(path); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload file gone; upload again"})
		return
	}
	job := a.jobs.get(id)
	if !job.start() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "upload already running"})
		return
	}
	go a.runRetry(id, path)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "retrying"})
}

func (a *adminAPI) runRetry(movieID int64, path string) {
	job := a.jobs.get(movieID)
	ctx := context.Background()
	finishErr := ""
	defer func() {
		if m, err := database.GetMovieByID(ctx, a.db, movieID); err == nil && m.Status == database.StatusREADY {
			_ = os.Remove(path)
		}
		job.finish(finishErr)
	}()
	src, err := os.Open(path)
	if err != nil {
		finishErr = "lost upload file"
		return
	}
	defer src.Close()
	if err := upload.RetryMovie(ctx, a.db, a.opts.Store, movieID, src, upload.Options{
		Progress:    job.progress,
		MaxRetries:  a.opts.UploadRetries,
		BackoffBase: a.opts.UploadBackoff,
	}); err != nil {
		finishErr = "retry failed"
		slog.Error("pipeline retry failed", "movie", movieID, "err", err)
	}
}

// handleGenerateThumb (re)generates the auto thumbnail for a READY movie and
// stores it to Telegram so it survives disk loss. One at a time, admin only:
// ffmpeg on an A53 box is ~100% CPU for a few seconds and parallel runs OOM.
func (a *adminAPI) handleGenerateThumb(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
		return
	}
	m, err := database.GetMovieByID(r.Context(), a.db, id)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if m.Status != database.StatusREADY {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "movie must be READY"})
		return
	}
	if a.opts.Store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "storage unavailable"})
		return
	}
	// ffmpeg fetches the stream URL itself (Go → Telegram). Only the first
	// few seconds are needed for a frame, so even on a slow link this is
	// bounded.
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	streamURL := fmt.Sprintf("%s://%s/api/stream/movies/%d", scheme, r.Host, id)
	if err := GenerateThumbnail(r.Context(), a.opts, id, streamURL); err != nil {
		slog.Warn("thumbnail generation failed", "movie", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "thumbnail generation failed: " + err.Error()})
		return
	}
	f, err := os.Open(thumbPath(a.opts, id))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "thumbnail file missing after generation"})
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() == 0 || st.Size() > 10<<20 {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "generated thumbnail invalid"})
		return
	}
	file, err := a.opts.Store.Upload(r.Context(), f, storage.Metadata{
		Name: thumbFileName(id), Size: st.Size(), Mime: "image/jpeg",
	})
	if err != nil {
		slog.Warn("thumb telegram store failed", "movie", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "telegram upload failed: " + err.Error()})
		return
	}
	if err := database.SetThumbTelegram(r.Context(), a.db, id, file.ID, file.MessageID); err != nil {
		slog.Warn("thumb telegram save failed", "movie", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database update failed"})
		return
	}
	slog.Info("thumbnail regenerated", "movie", id)
	writeJSON(w, http.StatusOK, map[string]string{"thumb_url": fmt.Sprintf("/api/thumbs/%d", id)})
}

// handleUploadStatus reports movie + per-part state + live job progress.
// Telegram locators are never included (see partDTO).
func (a *adminAPI) handleUploadStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
		return
	}
	ctx := r.Context()
	m, err := database.GetMovieByID(ctx, a.db, id)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	parts, err := database.ListParts(ctx, a.db, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	dtos := make([]partDTO, 0, len(parts))
	var done int64
	for _, p := range parts {
		dtos = append(dtos, toPartDTO(p))
		if p.Status == database.PartComplete {
			done += p.Size
		}
	}
	running, curPart, curBytes, curTotal, jobErr := a.jobs.get(id).snapshot()
	uploaded := done
	if running {
		uploaded += curBytes
	}
	var percent int64
	if m.VideoSize > 0 {
		percent = uploaded * 100 / m.VideoSize
		if percent > 100 {
			percent = 100
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"movie": m,
		"parts": dtos,
		"job": map[string]any{
			"running": running, "current_part": curPart,
			"current_bytes": curBytes, "current_total": curTotal, "error": jobErr,
		},
		"overall": map[string]any{
			"uploaded_bytes": uploaded, "total_bytes": m.VideoSize, "percent": percent,
		},
	})
}
