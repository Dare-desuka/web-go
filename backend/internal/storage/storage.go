// Package storage abstracts video blob storage behind one interface.
// Handlers and pipeline code depend on Storage, never on Telegram.
// First implementation: TelegramStorage (phase F5). This package also holds
// Memory, a test/dev fake. Nothing here reads credentials or the network.
package storage

import (
	"context"
	"io"
)

// Metadata describes a blob being uploaded.
type Metadata struct {
	Name string
	Size int64
	Mime string
}

// File is an opaque provider locator. ID persists to
// movie_parts.telegram_file_id; MessageID (when non-zero) persists to
// movie_parts.telegram_message_id. Memory ignores MessageID.
type File struct {
	ID        string
	MessageID int64
}

type Storage interface {
	Upload(ctx context.Context, r io.Reader, meta Metadata) (File, error)
	DownloadRange(ctx context.Context, f File, offset, length int64) (io.ReadCloser, error)
	Delete(ctx context.Context, f File) error
}
