// Command upload-subtitles uploads every local .vtt file in data/subtitles/
// to Telegram storage and updates the subtitles table rows to reference the
// Telegram file_id/message_id instead of the local path.
//
// Usage:
//   cd web-go
//   go run ./backend/cmd/upload-subtitles
package main

import (
	"bufio"
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"web-go/backend/internal/config"
	"web-go/backend/internal/database"
	"web-go/backend/internal/storage"
	"web-go/backend/internal/telegram"
)

func main() {
	slog.Info("upload-subtitles: starting")

	envFile := parseEnv(".env")
	getenv := makeLookup(envFile)

	cfg := config.LoadFrom(getenv)

	dbPath := getenv("DATABASE_PATH")
	if dbPath == "" {
		dbPath = "data/app.db"
	}
	if !filepath.IsAbs(dbPath) {
		dbPath = filepath.Join(".", dbPath)
	}

	ctx := context.Background()

	db, err := database.Open(dbPath)
	if err != nil {
		slog.Error("open db", "path", dbPath, "err", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := database.Migrate(ctx, db); err != nil {
		slog.Error("migrate db", "err", err)
		os.Exit(1)
	}

	store, err := telegramStore(cfg)
	if err != nil {
		slog.Error("telegram store", "err", err)
		os.Exit(1)
	}
	if store == nil {
		slog.Error("telegram not configured; set TELEGRAM_API_ID / HASH / SESSION / CHANNEL_ID")
		os.Exit(1)
	}

	dir := filepath.Join(".", "data", "subtitles")
	matches, err := filepath.Glob(filepath.Join(dir, "*.vtt"))
	if err != nil {
		slog.Error("glob subtitles", "dir", dir, "err", err)
		os.Exit(1)
	}
	slog.Info("found local subtitles", "count", len(matches))

	uploaded := 0
	skipped := 0
	for _, p := range matches {
		name := filepath.Base(p)

		var already int
		err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM subtitles WHERE file_path = ? AND telegram_file_id IS NOT NULL
		`, p).Scan(&already)
		if err != nil {
			slog.Error("check row", "path", p, "err", err)
			continue
		}
		if already > 0 {
			slog.Info("already on telegram", "file", name)
			skipped++
			continue
		}

		data, err := os.ReadFile(p)
		if err != nil {
			slog.Error("read file", "path", p, "err", err)
			continue
		}

		f, err := store.Upload(ctx, bytes.NewReader(data), storage.Metadata{
			Name: name,
			Size: int64(len(data)),
			Mime: "text/vtt; charset=utf-8",
		})
		if err != nil {
			slog.Error("upload to telegram", "file", name, "err", err)
			continue
		}

		var msgID *int64
		if f.MessageID != 0 {
			msgID = &f.MessageID
		}

		res, err := db.ExecContext(ctx, `
			UPDATE subtitles
			SET telegram_file_id = ?, telegram_message_id = ?
			WHERE file_path = ? AND telegram_file_id IS NULL
		`, f.ID, msgID, p)
		if err != nil {
			slog.Error("update db", "file", name, "err", err)
			continue
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			slog.Warn("no row updated (maybe already migrated)", "file", name)
		}
		uploaded++
		slog.Info("uploaded", "file", name, "file_id", f.ID, "msg_id", f.MessageID, "rows", n)
		// Throttle to avoid Telegram FLOOD_WAIT.
		time.Sleep(500 * time.Millisecond)
	}

	slog.Info("done", "uploaded", uploaded, "skipped", skipped, "total", len(matches))
}

func parseEnv(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		slog.Warn("open .env", "path", path, "err", err)
		return out
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if key != "" {
			out[key] = val
		}
	}
	if err := sc.Err(); err != nil {
		slog.Warn("read .env", "path", path, "err", err)
	}
	return out
}

func makeLookup(env map[string]string) func(string) string {
	return func(key string) string {
		if v, ok := env[key]; ok && v != "" {
			return v
		}
		return os.Getenv(key)
	}
}

func telegramStore(cfg config.Config) (storage.Storage, error) {
	tcfg, err := telegram.ParseConfig(
		cfg.TelegramAPIID, cfg.TelegramAPIHash,
		cfg.TelegramSession, cfg.TelegramChannelID,
	)
	if err != nil {
		return nil, err
	}
	client := telegram.NewClient(tcfg)
	go func() {
		if err := client.Run(context.Background()); err != nil {
			slog.Error("telegram client stopped", "err", err)
		}
	}()
	return telegram.NewStorage(client), nil
}
