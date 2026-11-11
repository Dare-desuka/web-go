package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"bytes"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"web-go/backend/internal/storage"

	"web-go/backend/internal/database"
)

const (
	maxSubtitleUpload = 5 << 20
	maxPosterUpload   = 10 << 20
)

var langRe = regexp.MustCompile(`^[a-z]{2,8}$`)

var posterExtByMime = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

func mediaDir(opts Options) string {
	if opts.DataDir != "" {
		return opts.DataDir
	}
	return "data"
}

// handleAdminListSubtitles lists tracks for any movie status (admin use).
func (a *adminAPI) handleAdminListSubtitles(w http.ResponseWriter, r *http.Request) {
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
	subs, err := database.ListSubtitles(r.Context(), a.db, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if subs == nil {
		subs = []database.Subtitle{}
	}
	writeJSON(w, http.StatusOK, subs)
}

// handleUploadSubtitle stores a WebVTT track: POST /api/admin/movies/{id}/subtitles
// with multipart fields file (.vtt), language (e.g. "id"), label ("Indonesia").
func (a *adminAPI) handleUploadSubtitle(w http.ResponseWriter, r *http.Request) {
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
	r.Body = http.MaxBytesReader(w, r.Body, maxSubtitleUpload+1<<20)
	language := strings.ToLower(strings.TrimSpace(r.FormValue("language")))
	label := strings.TrimSpace(r.FormValue("label"))
	if !langRe.MatchString(language) || label == "" || len(label) > 64 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "language (2-8 lowercase letters) and label required"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing subtitle file"})
		return
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".vtt" && ext != ".srt" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "only .vtt and .srt files accepted"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSubtitleUpload+1))
	if err != nil || int64(len(data)) > maxSubtitleUpload {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "subtitle too large"})
		return
	}
	// ponytail: accept .srt by converting to WebVTT on the fly (ffmpeg not needed for this tiny file).
	if ext == ".srt" {
		data = srtToVTT(data)
	}
	if !strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff")), "WEBVTT") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "not a WebVTT file"})
		return
	}
	// ponytail: multiple tracks per language allowed (Indonesia, Indonesia2, ...).
	// Colliding labels take the next free number; existing rows are never renamed.
	// Filenames carry millis so two same-language tracks never share a path.
	subs, err := database.ListSubtitles(r.Context(), a.db, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	taken := map[string]bool{}
	for _, s := range subs {
		taken[s.Label] = true
	}
	if taken[label] {
		base := strings.TrimRight(label, "0123456789")
		if base == "" {
			base = label
		}
		for n := 2; ; n++ {
			candidate := base + strconv.Itoa(n)
			if !taken[candidate] {
				label = candidate
				break
			}
		}
	}
	// Upload to Telegram storage when configured, else fall back to local disk.
	var subFilePath string
	var tfID *string
	var tmID *int64
	dir := filepath.Join(mediaDir(a.opts), "subtitles")
	stem := strconv.FormatInt(id, 10) + "_" + language + "_" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	name := filepath.Base(filepath.Join(dir, stem+".vtt"))
	if a.opts.Store != nil {
		f, err := a.opts.Store.Upload(r.Context(), bytes.NewReader(data), storage.Metadata{
			Name: name,
			Size: int64(len(data)),
			Mime: "text/vtt; charset=utf-8",
		})
		if err != nil {
			slog.Error("subtitle upload to storage failed", "movie", id, "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		tfID = &f.ID
		if f.MessageID != 0 {
			tmID = &f.MessageID
		}
	} else {
		// ponytail: local disk fallback for dev/tests without Telegram creds.
		if err := os.MkdirAll(dir, 0o755); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		path := filepath.Join(dir, stem+".vtt")
		for n := 2; ; n++ {
			if _, err := os.Stat(path); os.IsNotExist(err) {
				break
			}
			path = filepath.Join(dir, stem+"-"+strconv.Itoa(n)+".vtt")
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		subFilePath = path
	}
	subID, err := database.AddSubtitle(r.Context(), a.db, database.Subtitle{
		MovieID:          id,
		Language:         language,
		Label:            label,
		FilePath:         subFilePath,
		TelegramFileID:   tfID,
		TelegramMessageID: tmID,
	})
	if err != nil {
		_ = os.Remove(subFilePath)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	sub, _ := database.GetSubtitleByID(r.Context(), a.db, subID)
	writeJSON(w, http.StatusCreated, sub)
}

// srtToVTT converts SRT bytes to WebVTT (WEBVTT header + comma→dot timestamps).
func srtToVTT(data []byte) []byte {
	s := strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff"))
	if strings.HasPrefix(s, "WEBVTT") {
		return data
	}
	re := regexp.MustCompile(`(\d{2}:\d{2}:\d{2}),(\d{3})`)
	s = re.ReplaceAllString(s, `$1.$2`)
	re2 := regexp.MustCompile(`(\d{2}:\d{2}),(\d{3})`)
	s = re2.ReplaceAllString(s, `$1.$2`)
	return []byte("WEBVTT\n\n" + strings.TrimSpace(s) + "\n")
}

// handleDeleteSubtitle removes a track row + file: DELETE /api/admin/subtitles/{subID}.
func (a *adminAPI) handleDeleteSubtitle(w http.ResponseWriter, r *http.Request) {
	subID, err := strconv.ParseInt(r.PathValue("subID"), 10, 64)
	if err != nil || subID <= 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "subtitle not found"})
		return
	}
	sub, err := database.GetSubtitleByID(r.Context(), a.db, subID)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "subtitle not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if err := database.DeleteSubtitle(r.Context(), a.db, sub.ID, sub.MovieID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	// Delete subtitle blob: Telegram first, else local disk fallback.
	if sub.TelegramFileID != nil && a.opts.Store != nil {
		_ = a.opts.Store.Delete(r.Context(), storage.File{
			ID: *sub.TelegramFileID, MessageID: deref64(sub.TelegramMessageID),
		})
	} else {
		_ = os.Remove(subtitleDiskPath(mediaDir(a.opts), sub.FilePath))
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleUploadPoster stores poster/backdrop images and points the movie URL
// at the public serve endpoint.
func (a *adminAPI) handleUploadPoster(kind string) http.HandlerFunc {
	if kind != "poster" && kind != "backdrop" {
		panic("bad poster kind")
	}
	return func(w http.ResponseWriter, r *http.Request) {
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
		r.Body = http.MaxBytesReader(w, r.Body, maxPosterUpload+1<<20)
		file, _, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing image file"})
			return
		}
		defer file.Close()
		head := make([]byte, 512)
		n, _ := io.ReadFull(file, head)
		mime := http.DetectContentType(head[:n])
		ext, ok := posterExtByMime[mime]
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "only JPEG/PNG/WebP images accepted"})
			return
		}
		dir := filepath.Join(mediaDir(a.opts), "posters")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		// Remove previous kind file regardless of extension.
		if old, _ := filepath.Glob(filepath.Join(dir, strconv.FormatInt(id, 10)+"_"+kind+".*")); len(old) > 0 {
			for _, p := range old {
				_ = os.Remove(p)
			}
		}
		path := filepath.Join(dir, strconv.FormatInt(id, 10)+"_"+kind+ext)
		out, err := os.Create(path)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		_, _ = out.Write(head[:n])
		size, err := io.CopyN(out, file, maxPosterUpload)
		out.Close()
		if err != nil && !errors.Is(err, io.EOF) {
			_ = os.Remove(path)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot read upload"})
			return
		}
		if size+int64(n) > maxPosterUpload {
			_ = os.Remove(path)
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "image too large"})
			return
		}
		url := "/api/posters/" + strconv.FormatInt(id, 10) + "/" + kind
		if kind == "poster" {
			m.PosterURL = &url
		} else {
			m.BackdropURL = &url
		}
		if err := database.UpdateMovieMeta(r.Context(), a.db, id, m); err != nil {
			_ = os.Remove(path)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		m, _ = database.GetMovieByID(r.Context(), a.db, id)
		writeJSON(w, http.StatusOK, m)
	}
}

// textSubCodecs extract straight to WebVTT without OCR. Image-based
// tracks (hdmv_pgs_subtitle, dvd_subtitle, …) are skipped, never OCR'd.
var textSubCodecs = map[string]bool{
	"subrip": true, "srt": true, "ass": true, "ssa": true,
	"mov_text": true, "webvtt": true, "microdvd": true, "mpl2": true,
}

// subLangNames maps common ISO-639-2 tags to code + label.
var subLangNames = map[string][2]string{
	"eng": {"en", "English"},
	"ind": {"id", "Indonesia"},
	"jpn": {"ja", "Japanese"},
	"zho": {"zh", "Chinese"},
	"chi": {"zh", "Chinese"},
	"msa": {"ms", "Malay"},
	"ara": {"ar", "Arabic"},
	"spa": {"es", "Spanish"},
	"fra": {"fr", "French"},
	"deu": {"de", "German"},
	"por": {"pt", "Portuguese"},
	"rus": {"ru", "Russian"},
	"kor": {"ko", "Korean"},
	"tha": {"th", "Thai"},
	"vie": {"vi", "Vietnamese"},
}

// maxAutoSubs caps extraction per movie (pathological files).
const maxAutoSubs = 16

type subTrack struct {
	sIndex int // subtitle-relative index for -map 0:s:N
	codec  string
	lang   string
	label  string
}

// probeSubTracks lists text subtitle streams via ffprobe (JSON shape is
// stable; the csv writer reorders columns).
func probeSubTracks(ctx context.Context, input string) ([]subTrack, error) {
	tctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	out, err := exec.CommandContext(tctx, "ffprobe",
		"-v", "error",
		"-show_entries", "stream=index,codec_type,codec_name:stream_tags=language",
		"-of", "json", input).Output()
	if err != nil {
		return nil, err
	}
	var probed struct {
		Streams []struct {
			Index     int               `json:"index"`
			CodecType string            `json:"codec_type"`
			CodecName string            `json:"codec_name"`
			Tags      map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &probed); err != nil {
		return nil, err
	}
	var tracks []subTrack
	sN := 0
	for _, st := range probed.Streams {
		if st.CodecType != "subtitle" {
			continue
		}
		codec := strings.ToLower(st.CodecName)
		cur := sN
		sN++
		if !textSubCodecs[codec] {
			continue
		}
		lang, label := "", ""
		if tag := strings.ToLower(st.Tags["language"]); tag != "" {
			if m, ok := subLangNames[tag]; ok {
				lang, label = m[0], m[1]
			} else {
				lang, label = tag, tag
			}
		}
		if lang == "" || !langRe.MatchString(lang) {
			continue
		}
		if label == "" {
			label = lang
		}
		tracks = append(tracks, subTrack{sIndex: cur, codec: codec, lang: lang, label: label})
		if len(tracks) >= maxAutoSubs {
			break
		}
	}
	return tracks, nil
}

// ExtractSubtitles pulls embedded text subtitle tracks into .vtt files +
// DB rows, skipping languages that already exist (manual uploads win).
// Best-effort: per-track failures are logged and skipped. Returns how many
// tracks were stored.
func ExtractSubtitles(ctx context.Context, db *sql.DB, opts Options, movieID int64, videoPath string) (int, error) {
	tracks, err := probeSubTracks(ctx, videoPath)
	if err != nil {
		return 0, err
	}
	if len(tracks) == 0 {
		return 0, nil
	}
	dir := filepath.Join(mediaDir(opts), "subtitles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	stored := 0
	for _, tr := range tracks {
		dup, err := database.SubtitleExists(ctx, db, movieID, tr.lang)
		if err == nil && dup {
			slog.Info("subtitle auto-skip exists", "movie", movieID, "lang", tr.lang)
			continue
		}
		path := filepath.Join(dir, strconv.FormatInt(movieID, 10)+"_"+tr.lang+".vtt")
		tctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err = exec.CommandContext(tctx, "ffmpeg", "-y", "-v", "error",
			"-i", videoPath, "-map", "0:s:"+strconv.Itoa(tr.sIndex),
			"-c:s", "webvtt", path).Run()
		cancel()
		if err != nil {
			_ = os.Remove(path)
			slog.Info("subtitle auto-extract failed", "movie", movieID, "lang", tr.lang, "err", err)
			continue
		}
		if st, serr := os.Stat(path); serr != nil || st.Size() == 0 {
			_ = os.Remove(path)
			continue
		}
		if _, err := database.AddSubtitle(ctx, db, database.Subtitle{
			MovieID: movieID, Language: tr.lang, Label: tr.label, FilePath: path,
		}); err != nil {
			_ = os.Remove(path)
			slog.Info("subtitle auto-store failed", "movie", movieID, "lang", tr.lang, "err", err)
			continue
		}
		slog.Info("subtitle auto-stored", "movie", movieID, "lang", tr.lang)
		stored++
	}
	return stored, nil
}
func thumbPath(opts Options, movieID int64) string {
	return filepath.Join(mediaDir(opts), "posters", strconv.FormatInt(movieID, 10)+"_thumb.jpg")
}

func thumbFileName(movieID int64) string {
	return strconv.FormatInt(movieID, 10) + "_thumb.jpg"
}

// thumbSeeks are tried in order; short videos fall through to earlier frames.
var thumbSeeks = []string{"10", "1", "0"}

// GenerateThumbnail extracts one 640px-wide JPEG frame via ffmpeg.
// Best-effort by design: callers log failures and continue without it.
// input is a local file path (pipeline) or http(s) URL (backfill).
func GenerateThumbnail(ctx context.Context, opts Options, movieID int64, input string) error {
	dir := filepath.Join(mediaDir(opts), "posters")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out := thumbPath(opts, movieID)
	for _, seek := range thumbSeeks {
		tctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		cmd := exec.CommandContext(tctx, "ffmpeg", "-y", "-v", "error",
			"-ss", seek, "-i", input,
			"-frames:v", "1", "-q:v", "4", "-vf", "scale=640:-1", out)
		err := cmd.Run()
		cancel()
		if err == nil {
			if st, serr := os.Stat(out); serr == nil && st.Size() > 0 {
				slog.Info("thumbnail generated", "movie", movieID, "seek", seek)
				return nil
			}
		}
		slog.Info("thumbnail seek failed, trying earlier", "movie", movieID, "seek", seek)
	}
	_ = os.Remove(out)
	return errors.New("ffmpeg could not extract a frame")
}

// handleServeThumb serves GET /api/thumbs/{movieID}.
// Prefers Telegram (survives disk loss) with a disk fallback for older rows.
// Admins also see DRAFT artwork.
func handleServeThumb(db *sql.DB, opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		movieID, err := strconv.ParseInt(r.PathValue("movieID"), 10, 64)
		if err != nil || movieID <= 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		m, ok := resolveMovie(db, w, r, movieID)
		if !ok {
			return
		}
		if m.ThumbTelegramFileID != nil && m.ThumbTelegramMessageID != nil && opts.Store != nil {
			rc, err := opts.Store.DownloadRange(r.Context(),
				storage.File{ID: *m.ThumbTelegramFileID, MessageID: *m.ThumbTelegramMessageID},
				0, 10<<20)
			if err == nil {
				defer rc.Close()
				w.Header().Set("Content-Type", "image/jpeg")
				w.Header().Set("Cache-Control", "public, max-age=86400")
				_, _ = io.Copy(w, rc)
				return
			}
			slog.Info("thumb telegram miss, trying disk", "movie", movieID, "err", err)
		}
		f, err := os.Open(thumbPath(opts, movieID))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = io.Copy(w, f)
	}
}

// handleServePoster serves GET /api/posters/{movieID}/{kind}.
// Admins also see DRAFT artwork.
func handleServePoster(db *sql.DB, opts Options) http.HandlerFunc {
	cts := map[string]string{".jpg": "image/jpeg", ".png": "image/png", ".webp": "image/webp"}
	return func(w http.ResponseWriter, r *http.Request) {
		movieID, err := strconv.ParseInt(r.PathValue("movieID"), 10, 64)
		kind := r.PathValue("kind")
		if err != nil || movieID <= 0 || (kind != "poster" && kind != "backdrop") {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if _, ok := resolveMovie(db, w, r, movieID); !ok {
			return
		}
		matches, _ := filepath.Glob(filepath.Join(mediaDir(opts), "posters",
			strconv.FormatInt(movieID, 10)+"_"+kind+".*"))
		if len(matches) == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		f, err := os.Open(matches[0])
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		defer f.Close()
		if ct, ok := cts[strings.ToLower(filepath.Ext(matches[0]))]; ok {
			w.Header().Set("Content-Type", ct)
		}
		_, _ = io.Copy(w, f)
	}
}

// handlePublish verifies parts then marks READY (republish after unpublish).
func (a *adminAPI) handlePublish(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
		return
	}
	if err := database.PublishMovie(r.Context(), a.db, id); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
			return
		}
		writeJSON(w, http.StatusConflict, map[string]string{"error": "movie cannot be published: parts incomplete"})
		return
	}
	m, _ := database.GetMovieByID(r.Context(), a.db, id)
	writeJSON(w, http.StatusOK, m)
}

// handleUnpublish hides a READY movie (READY -> DRAFT).
func (a *adminAPI) handleUnpublish(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
		return
	}
	if err := database.SetMovieStatus(r.Context(), a.db, id, database.StatusDRAFT); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "movie not found"})
			return
		}
		writeJSON(w, http.StatusConflict, map[string]string{"error": "only ready movies can be unpublished"})
		return
	}
	m, _ := database.GetMovieByID(r.Context(), a.db, id)
	writeJSON(w, http.StatusOK, m)
}

// removeMovieFiles deletes subtitle/poster files owned by a movie.
// Best-effort: DB delete proceeds regardless.
func removeMovieFiles(ctx context.Context, db *sql.DB, opts Options, movieID int64) {
	subs, err := database.ListSubtitles(ctx, db, movieID)
	if err == nil {
		for _, s := range subs {
			if s.TelegramFileID != nil && opts.Store != nil {
				_ = opts.Store.Delete(ctx, storage.File{
					ID: *s.TelegramFileID, MessageID: deref64(s.TelegramMessageID),
				})
			} else {
				_ = os.Remove(subtitleDiskPath(mediaDir(opts), s.FilePath))
			}
		}
	}
	for _, kind := range []string{"poster", "backdrop"} {
		matches, _ := filepath.Glob(filepath.Join(mediaDir(opts), "posters",
			strconv.FormatInt(movieID, 10)+"_"+kind+".*"))
		for _, p := range matches {
			_ = os.Remove(p)
		}
	}
	// Thumb: Telegram ref removed via its file_id; disk fallback file (older
	// rows) is the conventional thumbPath.
	if m, err := database.GetMovieByID(ctx, db, movieID); err == nil {
		if m.ThumbTelegramFileID != nil && opts.Store != nil {
			_ = opts.Store.Delete(ctx, storage.File{
				ID: *m.ThumbTelegramFileID, MessageID: deref64(m.ThumbTelegramMessageID),
			})
		}
	}
	_ = os.Remove(thumbPath(opts, movieID))
	_ = os.Remove(tmpPath(opts.TmpDir, movieID))
}

func deref64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
