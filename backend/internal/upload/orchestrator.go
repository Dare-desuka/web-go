package upload

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"time"

	"web-go/backend/internal/database"
	"web-go/backend/internal/storage"
)

// Options tunes the upload loop. Zero values take production defaults.
type Options struct {
	// MaxRetries bounds per-part attempts: total tries = 1 + MaxRetries.
	MaxRetries int
	// BackoffBase scales exponential wait between tries: base * 2^attempt.
	BackoffBase time.Duration
	// Sleep waits between retries; inject a fake in tests.
	Sleep func(time.Duration)
	// Progress reports in-flight bytes per part (throttled, in-process only).
	Progress ProgressFunc
}

// ProgressFunc observes uploaded bytes of one part.
type ProgressFunc func(partNumber, uploaded, total int64)

func (o Options) withDefaults() Options {
	if o.MaxRetries <= 0 {
		o.MaxRetries = 5
	}
	if o.BackoffBase <= 0 {
		o.BackoffBase = time.Second
	}
	if o.Sleep == nil {
		o.Sleep = time.Sleep
	}
	return o
}

// Prepare validates a movie, computes its split plan and persists WAITING
// part rows while moving the movie to UPLOADING, atomically.
// Re-running on an already-prepared movie is an error; resume via UploadParts.
func Prepare(ctx context.Context, db *sql.DB, movieID, maxMovieSize, maxPartSize int64) ([]database.PlanPart, error) {
	m, err := database.GetMovieByID(ctx, db, movieID)
	if err != nil {
		return nil, err
	}
	if err := Validate(m.VideoSize, m.VideoMime, maxMovieSize); err != nil {
		return nil, err
	}
	plan, err := Split(m.VideoSize, maxPartSize)
	if err != nil {
		return nil, err
	}
	now := database.NowMillis()
	err = database.WithTx(ctx, db, func(tx database.DBTX) error {
		existing, err := database.ListParts(ctx, tx, movieID)
		if err != nil {
			return err
		}
		if len(existing) > 0 {
			return fmt.Errorf("movie %d already has %d parts", movieID, len(existing))
		}
		if err := database.SetMovieStatus(ctx, tx, movieID, database.StatusUPLOADING); err != nil {
			return err
		}
		return database.CreateParts(ctx, tx, movieID, plan, now)
	})
	if err != nil {
		return nil, err
	}
	slog.Info("upload prepared", "movie", movieID, "parts", len(plan), "size", m.VideoSize)
	return plan, nil
}

// UploadParts uploads every WAITING/FAILED part by streaming its
// [start,end) segment from src (never the whole movie in RAM), then moves
// the movie to PROCESSING (all COMPLETE) or ERROR (anything left).
// Safe to re-run after a crash: COMPLETE parts are skipped.
func UploadParts(ctx context.Context, db *sql.DB, store storage.Storage, movieID int64, src io.ReaderAt, opts Options) error {
	opts = opts.withDefaults()
	m, err := database.GetMovieByID(ctx, db, movieID)
	if err != nil {
		return err
	}
	pending, err := database.PendingParts(ctx, db, movieID)
	if err != nil {
		return err
	}
	for _, p := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := uploadOne(ctx, db, store, m, p, src, opts); err != nil {
			slog.Error("upload part failed", "movie", movieID, "part", p.PartNumber, "err", err)
		}
	}
	pending, err = database.PendingParts(ctx, db, movieID)
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		if err := database.SetMovieStatus(ctx, db, movieID, database.StatusERROR); err != nil {
			return err
		}
		return fmt.Errorf("movie %d: %d parts pending", movieID, len(pending))
	}
	// Re-runs with nothing pending are a no-op success (already PROCESSING).
	if m.Status != database.StatusPROCESSING {
		return database.SetMovieStatus(ctx, db, movieID, database.StatusPROCESSING)
	}
	return nil
}

// Finalize verifies every part is COMPLETE and reconstructs the movie size
// exactly, then marks READY. Anything else marks ERROR.
func Finalize(ctx context.Context, db *sql.DB, movieID int64) error {
	m, err := database.GetMovieByID(ctx, db, movieID)
	if err != nil {
		return err
	}
	parts, err := database.ListParts(ctx, db, movieID)
	if err != nil {
		return err
	}
	fail := func(why error) error {
		if err := database.SetMovieStatus(ctx, db, movieID, database.StatusERROR); err != nil {
			return err
		}
		return why
	}
	for _, p := range parts {
		if p.Status != database.PartComplete {
			return fail(fmt.Errorf("part %d is %s, want COMPLETE", p.PartNumber, p.Status))
		}
	}
	if err := database.VerifyParts(parts, m.VideoSize); err != nil {
		return fail(err)
	}
	if err := database.SetMovieStatus(ctx, db, movieID, database.StatusREADY); err != nil {
		return err
	}
	slog.Info("upload finalized", "movie", movieID, "parts", len(parts))
	return nil
}

// RetryMovie re-queues FAILED parts of an ERROR movie and runs the full
// upload + verify cycle. COMPLETE parts are never re-uploaded.
func RetryMovie(ctx context.Context, db *sql.DB, store storage.Storage, movieID int64, src io.ReaderAt, opts Options) error {
	m, err := database.GetMovieByID(ctx, db, movieID)
	if err != nil {
		return err
	}
	if m.Status == database.StatusERROR {
		if err := database.SetMovieStatus(ctx, db, movieID, database.StatusUPLOADING); err != nil {
			return err
		}
	} else if m.Status != database.StatusUPLOADING {
		return fmt.Errorf("cannot retry movie in status %s", m.Status)
	}
	pending, err := database.PendingParts(ctx, db, movieID)
	if err != nil {
		return err
	}
	for _, p := range pending {
		if p.Status == database.PartFailed {
			if err := database.ResetPartForRetry(ctx, db, p.ID); err != nil {
				return err
			}
		}
	}
	if err := UploadParts(ctx, db, store, movieID, src, opts); err != nil {
		// Fall through to Finalize so the movie lands on ERROR, never
		// stuck in UPLOADING where no endpoint can recover it.
		slog.Info("retry upload incomplete", "movie", movieID, "err", err)
	}
	return Finalize(ctx, db, movieID)
}

// uploadOne streams one part segment with bounded retries + backoff.
func uploadOne(ctx context.Context, db *sql.DB, store storage.Storage, m database.Movie, p database.Part, src io.ReaderAt, opts Options) error {
	if err := database.MarkPartUploading(ctx, db, p.ID); err != nil {
		return err
	}
	name := fmt.Sprintf("%s-part-%d", m.Slug, p.PartNumber)
	var lastErr error
	for attempt := 0; attempt <= opts.MaxRetries; attempt++ {
		if attempt > 0 {
			opts.Sleep(opts.BackoffBase * (1 << (attempt - 1)))
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		section := io.NewSectionReader(src, p.StartOffset, p.Size)
		pr := &progressReader{r: section, part: p.PartNumber, total: p.Size, cb: opts.Progress}
		file, err := store.Upload(ctx, pr, storage.Metadata{Name: name, Size: p.Size, Mime: m.VideoMime})
		if err == nil {
			slog.Info("upload part completed", "movie", m.ID, "part", p.PartNumber)
			return database.MarkPartComplete(ctx, db, p.ID, file.MessageID, file.ID)
		}
		lastErr = err
		slog.Info("upload part attempt failed", "movie", m.ID, "part", p.PartNumber, "attempt", attempt+1)
	}
	// ponytail: error text persisted is provider-safe (no credentials in Storage errors by contract).
	_ = database.MarkPartFailed(ctx, db, p.ID, lastErr.Error())
	return lastErr
}

// progressReader counts streamed bytes and reports throttled progress.
type progressReader struct {
	r            io.Reader
	part         int64
	total        int64
	uploaded     int64
	lastReported int64
	cb           ProgressFunc
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.uploaded += int64(n)
	if p.cb != nil && (p.uploaded-p.lastReported >= 262144 || p.uploaded == p.total) {
		p.lastReported = p.uploaded
		p.cb(p.part, p.uploaded, p.total)
	}
	return n, err
}
