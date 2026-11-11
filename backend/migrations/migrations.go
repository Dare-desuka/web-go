// Package migrations embeds SQL migration files for database.Migrate.
package migrations

import "embed"

// FS holds backend/migrations/*.sql (0001_init.sql baseline).
//
//go:embed *.sql
var FS embed.FS
