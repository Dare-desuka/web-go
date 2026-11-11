// Package config loads server configuration from environment variables.
// Single source of locked constants lives in PRD.md section 28:
// MAX_PART_SIZE is 900 MiB = 943718400 bytes, MAX_MOVIE_SIZE is 20 GiB.
package config

import (
	"os"
	"strconv"
)

const (
	// DefaultMaxPartSize is 900 MiB in bytes. ponytail: one const, no env-per-part tuning.
	DefaultMaxPartSize int64 = 943718400
	// DefaultMaxMovieSize is 20 GiB in bytes (MVP global upload limit).
	DefaultMaxMovieSize int64 = 21474836480
)

type Config struct {
	ServerHost string
	ServerPort string

	DatabasePath string

	// TmpDir holds in-flight upload parts (video files waiting to be cut
	// and shipped to Telegram). Lives on the HDD, not the box's internal
	// storage: a 20 GiB movie must not fill the eMMC.
	TmpDir string

	MaxPartSize  int64
	MaxMovieSize int64

	TelegramAPIID     string
	TelegramAPIHash   string
	TelegramSession   string
	TelegramChannelID string

	// FrontendURL lists browser origins allowed by CORS (comma-separated).
	FrontendURL string

	// CookieSecure enables Secure on the admin session cookie (production).
	CookieSecure bool
}

func getEnv(getenv func(string) string, key, def string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return def
}

func getInt64Env(getenv func(string) string, key string, def int64) int64 {
	v := getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func getBoolEnv(getenv func(string) string, key string) bool {
	v := getenv(key)
	return v == "1" || v == "true" || v == "TRUE"
}

// Load reads configuration from the process environment, applying MVP
// defaults.
func Load() Config { return LoadFrom(os.Getenv) }

// LoadFrom reads configuration through getenv instead of the process
// environment. The suite binary runs several services in one process, and
// web-go/music-go both claim DATABASE_PATH and the TELEGRAM_* names — a
// per-service lookup keeps them from overwriting each other. ponytail:
// Load() stays as the os.Getenv shorthand every standalone cmd still uses.
func LoadFrom(getenv func(string) string) Config {
	return Config{
		ServerHost:   getEnv(getenv, "SERVER_HOST", "127.0.0.1"),
		ServerPort:   getEnv(getenv, "SERVER_PORT", "8080"),
		DatabasePath: getEnv(getenv, "DATABASE_PATH", "data/app.db"),
		TmpDir:       getEnv(getenv, "TMP_DIR", "/mnt/hdd/videos/tmp"),
		CookieSecure: getBoolEnv(getenv, "COOKIE_SECURE"),

		MaxPartSize:  getInt64Env(getenv, "MAX_PART_SIZE", DefaultMaxPartSize),
		MaxMovieSize: getInt64Env(getenv, "MAX_MOVIE_SIZE", DefaultMaxMovieSize),

		TelegramAPIID:     getenv("TELEGRAM_API_ID"),
		TelegramAPIHash:   getenv("TELEGRAM_API_HASH"),
		TelegramSession:   getenv("TELEGRAM_SESSION"),
		TelegramChannelID: getenv("TELEGRAM_CHANNEL_ID"),

		FrontendURL: getEnv(getenv, "FRONTEND_URL", "http://127.0.0.1:3003"),
	}
}

// Addr returns host:port for http.Server.
func (c Config) Addr() string {
	return c.ServerHost + ":" + c.ServerPort
}
