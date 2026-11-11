// Package upload owns the video upload pipeline: validation, splitting,
// part-tracked upload with retry/resume, and verification.
// It depends on storage.Storage, so Telegram specifics never leak in here.
package upload

import (
	"errors"
	"fmt"

	"web-go/backend/internal/database"
)

// Split divides totalSize bytes into [start,end) parts of at most
// maxPartSize bytes (PRD 28.1-28.2). Pure function, int64 only.
func Split(totalSize, maxPartSize int64) ([]database.PlanPart, error) {
	if totalSize <= 0 {
		return nil, errors.New("empty file")
	}
	if maxPartSize <= 0 {
		return nil, errors.New("invalid max part size")
	}
	n := (totalSize + maxPartSize - 1) / maxPartSize
	plan := make([]database.PlanPart, 0, n)
	for i := int64(0); i < n; i++ {
		start := i * maxPartSize
		end := start + maxPartSize
		if end > totalSize {
			end = totalSize
		}
		plan = append(plan, database.PlanPart{
			PartNumber:  i + 1,
			Size:        end - start,
			StartOffset: start,
			EndOffset:   end,
		})
	}
	return plan, nil
}

// Validate gates a movie's bytes before any part row is created.
// Size is the primary check (PRD 28.3); mime must be a video/* sniff result,
// never a bare file extension.
func Validate(totalSize int64, mime string, maxMovieSize int64) error {
	if totalSize <= 0 {
		return errors.New("empty file")
	}
	if totalSize > maxMovieSize {
		return fmt.Errorf("file size %d exceeds limit %d", totalSize, maxMovieSize)
	}
	if mime == "" {
		return errors.New("missing mime type")
	}
	for i := 0; i < len(mime); i++ {
		if mime[i] == ';' {
			mime = mime[:i]
			break
		}
	}
	if len(mime) < 6 || mime[:6] != "video/" {
		return fmt.Errorf("not a video mime type: %q", mime)
	}
	return nil
}
