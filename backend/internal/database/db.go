// Package database owns SQLite access: open, pragmas, migrations.
// Handlers must use repositories here, never raw SQL in HTTP handlers.
package database

import (
	"context"
	"database/sql"
	"time"

	"web-go/backend/migrations"

	_ "modernc.org/sqlite"
)

// Open creates the SQLite pool with MVP-safe pragmas.
// Single writer: MaxOpenConns(1) avoids SQLITE_BUSY under concurrent streams.
// foreign_keys rides in the DSN (not just Exec below): pragmas are
// per-connection, and the pool opens fresh conns after idle timeout —
// without this, ON DELETE CASCADE silently stops working and orphans pile up.
func Open(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?cache=shared&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxIdleTime(time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA foreign_keys=ON;",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// DBTX abstracts *sql.DB and *sql.Tx so repositories work inside or
// outside a transaction.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// WithTx runs fn inside a transaction, rolling back on error.
func WithTx(ctx context.Context, db *sql.DB, fn func(DBTX) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// migrationFiles applies in order. New migrations append to this list;
// files stay idempotent (CREATE TABLE/INDEX IF NOT EXISTS) for old checkouts.
var migrationFiles = []string{"0001_init.sql", "0002_sessions.sql", "0003_thumb_telegram.sql", "0004_genres.sql", "0005_subtitle_variants.sql", "0006_subtitle_telegram.sql"}

// Migrate applies all schema files in order (idempotent).
func Migrate(ctx context.Context, db *sql.DB) error {
	for _, name := range migrationFiles {
		schema, err := migrations.FS.ReadFile(name)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, string(schema)); err != nil {
			// 0003 ALTER ADD COLUMN fails on second run (duplicate column);
			// treat as success to keep Migrate idempotent.
			if isDuplicateColumn(err) {
				continue
			}
			return err
		}
	}
	return nil
}

func isDuplicateColumn(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "duplicate column name") || contains(msg, "already exists")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// NowMillis returns Unix milliseconds for created_at/updated_at columns.
func NowMillis() int64 {
	return time.Now().UnixMilli()
}
