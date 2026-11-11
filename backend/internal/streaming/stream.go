package streaming

import (
	"context"
	"errors"
	"io"
	"net/http"

	"web-go/backend/internal/database"
	"web-go/backend/internal/storage"
)

// streamChunk bounds one DownloadRange call so a single segment never
// buffers more than 1 MiB, whatever the storage backend does internally.
const streamChunk = 1 << 20

// Stream copies segments to w as one continuous body, part after part.
// The browser sees a single video; part boundaries are invisible.
// Callers set headers + status before invoking; on mid-body error the
// only option is aborting, so errors are returned for logging.
func Stream(ctx context.Context, w io.Writer, store storage.Storage, parts []database.Part, segments []Segment) error {
	byNumber := make(map[int64]database.Part, len(parts))
	for _, p := range parts {
		byNumber[p.PartNumber] = p
	}
	flusher, _ := w.(http.Flusher)
	for _, sg := range segments {
		p, ok := byNumber[sg.PartNumber]
		if !ok || p.TelegramFileID == nil {
			return errMissingPart
		}
		loc := storage.File{ID: *p.TelegramFileID}
		if p.TelegramMessageID != nil {
			loc.MessageID = *p.TelegramMessageID
		}
		remaining, off := sg.Length, sg.LocalOffset
		for remaining > 0 {
			if err := ctx.Err(); err != nil {
				return err // viewer went away; stop pulling from Telegram
			}
			want := min(remaining, streamChunk)
			rc, err := store.DownloadRange(ctx, loc, off, want)
			if err != nil {
				return err
			}
			_, err = io.CopyN(w, rc, want)
			rc.Close()
			if err != nil {
				return err
			}
			off += want
			remaining -= want
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
	return nil
}

// errMissingPart reports part-availability failures (handler maps to 503).
var errMissingPart = errors.New("part unavailable")

// IsMissingPart reports part-availability failures (handler maps to 503).
func IsMissingPart(err error) bool {
	return errors.Is(err, errMissingPart)
}
