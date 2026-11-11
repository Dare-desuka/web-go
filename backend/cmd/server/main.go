// Command server boots the Go API: config, SQLite, migrations, HTTP routes.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"web-go/backend/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, os.Getenv); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}
