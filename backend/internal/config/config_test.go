package config

import (
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("SERVER_HOST", "")
	t.Setenv("SERVER_PORT", "")
	t.Setenv("DATABASE_PATH", "")
	t.Setenv("MAX_PART_SIZE", "")
	t.Setenv("MAX_MOVIE_SIZE", "")

	c := Load()
	if c.ServerHost != "127.0.0.1" {
		t.Fatalf("ServerHost = %q, want 127.0.0.1", c.ServerHost)
	}
	if c.ServerPort != "8080" {
		t.Fatalf("ServerPort = %q, want 8080", c.ServerPort)
	}
	if c.DatabasePath != "data/app.db" {
		t.Fatalf("DatabasePath = %q, want data/app.db", c.DatabasePath)
	}
	if c.MaxPartSize != 943718400 {
		t.Fatalf("MaxPartSize = %d, want 943718400", c.MaxPartSize)
	}
	if c.MaxMovieSize != 21474836480 {
		t.Fatalf("MaxMovieSize = %d, want 21474836480", c.MaxMovieSize)
	}
	if c.Addr() != "127.0.0.1:8080" {
		t.Fatalf("Addr = %q", c.Addr())
	}
}

func TestLoadInvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("MAX_PART_SIZE", "not-a-number")
	t.Setenv("MAX_MOVIE_SIZE", "-5")

	c := Load()
	if c.MaxPartSize != DefaultMaxPartSize {
		t.Fatalf("MaxPartSize = %d, want default %d", c.MaxPartSize, DefaultMaxPartSize)
	}
	if c.MaxMovieSize != DefaultMaxMovieSize {
		t.Fatalf("MaxMovieSize = %d, want default %d", c.MaxMovieSize, DefaultMaxMovieSize)
	}
}
