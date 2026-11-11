// Package app boots the web-go server. It is the only non-internal seam, so
// the suite binary can run web-go beside music-go and media-go in one
// process; everything it needs stays internal.
package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"web-go/backend/internal/api"
	"web-go/backend/internal/config"
	"web-go/backend/internal/database"
	"web-go/backend/internal/storage"
	"web-go/backend/internal/telegram"
)

// Run serves the API until ctx is cancelled, then shuts down gracefully.
// getenv replaces the process environment so a merged binary can hand
// web-go its own DATABASE_PATH and TELEGRAM_* values.
func Run(ctx context.Context, getenv func(string) string) error {
	cfg := config.LoadFrom(getenv)

	dataDir := filepath.Dir(cfg.DatabasePath)
	// ponytail: tmp lives on the HDD (TMP_DIR), not next to data/ — a 20 GiB
	// upload must not fill the box's internal storage. The HDD is slow but
	// big; the eMMC is fast but small.
	tmpDir := cfg.TmpDir
	if tmpDir == "" {
		tmpDir = filepath.Join(filepath.Dir(dataDir), "tmp")
	}
	if dir := filepath.Dir(cfg.DatabasePath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return err
	}
	// ponytail: force multipart spool (http.ParseMultipartForm) onto the HDD
	// too — a 1.3 GiB upload spills to os.TempDir() which defaults to /tmp
	// on the eMMC, filling it. TMPDIR wins for Go's os.TempDir().
	os.Setenv("TMPDIR", tmpDir)
	os.Setenv("TMP", tmpDir)

	db, err := database.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()

	migrateCtx, cancelMigrate := context.WithTimeout(ctx, 30*time.Second)
	defer cancelMigrate()
	if err := database.Migrate(migrateCtx, db); err != nil {
		return err
	}

	store := telegramStore(cfg)
	sweepStaleTemps(db, tmpDir)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// ponytail: janitor — the upload temp is a full-size movie on the HDD.
	// Once the movie is READY (bytes already on Telegram) the file is dead
	// weight; anything still UPLOADING is left alone so a live upload is
	// never pulled out from under itself.
	go cleanupOldTemps(ctx, db, tmpDir, time.Hour)

	apiHandler := api.WithCORS(api.NewMux(db, api.Options{
		CookieSecure: cfg.CookieSecure,
		Store:        store,
		TmpDir:       tmpDir,
		DataDir:      dataDir,
		MaxMovieSize: cfg.MaxMovieSize,
		MaxPartSize:  cfg.MaxPartSize,
	}), api.SplitOrigins(cfg.FrontendURL))

	handler := apiHandler
	// Frontend served from this same process: the Next.js export costs one
	// static dir and no Node, which is what makes the STB viable. Empty
	// FRONTEND_DIR keeps the standalone split (Next on its own port).
	if dir := getenv("FRONTEND_DIR"); dir != "" {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			slog.Info("serving frontend export", "dir", dir)
			handler = staticHandler(dir, apiHandler)
		} else {
			slog.Warn("FRONTEND_DIR is not a directory, API only", "dir", dir)
		}
	}

	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: handler,
		// ponytail: no Read/WriteTimeout — a multi-GB upload or a long
		// stream legitimately outlives any fixed window. Header timeout
		// stops slowloris; MaxBytesReader bounds uploads; handlers
		// respect request cancel.
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Addr())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	slog.Info("shutting down")
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return <-serveErr
}

// sweepStaleTemps removes leftover upload temps for movies already READY
// (crash between finalize and cleanup). Temps of unfinished movies stay:
// they are the resume source for retry.
func sweepStaleTemps(db *sql.DB, tmpDir string) {
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, e := range entries {
		var movieID int64
		if _, err := fmt.Sscanf(e.Name(), "upload-%d.bin", &movieID); err != nil {
			continue
		}
		m, err := database.GetMovieByID(ctx, db, movieID)
		if err != nil {
			continue
		}
		if m.Status == database.StatusREADY {
			_ = os.Remove(filepath.Join(tmpDir, e.Name()))
			slog.Info("swept stale upload temp", "movie", movieID)
		}
	}
}

// cleanupOldTemps deletes upload temps older than maxAge. A temp holds the
// full movie on disk; once the parts are on Telegram the file is dead
// weight. Movies still UPLOADING/PROCESSING are kept (a live upload must
// not lose its source), and movies in ERROR keep their temp for retry —
// only READY movies and temps whose movie row is gone get deleted.
func cleanupOldTemps(ctx context.Context, db *sql.DB, tmpDir string, maxAge time.Duration) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		cleanupOldTempsOnce(ctx, db, tmpDir, maxAge)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func cleanupOldTempsOnce(ctx context.Context, db *sql.DB, tmpDir string, maxAge time.Duration) {
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		var movieID int64
		if _, err := fmt.Sscanf(e.Name(), "upload-%d.bin", &movieID); err != nil {
			continue
		}
		fp := filepath.Join(tmpDir, e.Name())
		st, err := os.Stat(fp)
		if err != nil || !st.Mode().IsRegular() || !st.ModTime().Before(cutoff) {
			continue
		}
		m, err := database.GetMovieByID(ctx, db, movieID)
		if err == nil {
			switch m.Status {
			case database.StatusUPLOADING, database.StatusPROCESSING:
				continue // upload in flight — keep the source
			case database.StatusERROR:
				continue // retry source — keep until the movie is retried
			}
		}
		if err := os.Remove(fp); err != nil {
			slog.Error("temp cleanup failed", "file", e.Name(), "err", err)
			continue
		}
		slog.Info("temp cleaned", "movie", movieID, "file", e.Name())
	}
}

// telegramStore connects the MTProto storage channel when credentials are
// configured, else returns nil and the stream endpoint answers 503.
// Config values come from the passed env lookup (never logged).
func telegramStore(cfg config.Config) storage.Storage {
	tcfg, err := telegram.ParseConfig(
		cfg.TelegramAPIID, cfg.TelegramAPIHash,
		cfg.TelegramSession, cfg.TelegramChannelID,
	)
	if err != nil {
		slog.Warn("telegram storage disabled")
		return nil
	}
	client := telegram.NewClient(tcfg)
	go func() {
		if err := client.Run(context.Background()); err != nil {
			slog.Error("telegram client stopped", "err", err)
		}
	}()
	return telegram.NewStorage(client)
}
