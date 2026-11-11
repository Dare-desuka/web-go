package database

import (
	"context"
	"database/sql"
	"fmt"
)

// Part statuses: each part uploads independently (AGENTS section 16).
const (
	PartWaiting   = "WAITING"
	PartUploading = "UPLOADING"
	PartComplete  = "COMPLETE"
	PartFailed    = "FAILED"
)

// Part tracks one Telegram chunk of a virtual movie.
// Offsets follow [start_offset, end_offset) with end exclusive (PRD 28.2).
type Part struct {
	ID                int64
	MovieID           int64
	PartNumber        int64
	Size              int64
	StartOffset       int64
	EndOffset         int64
	TelegramMessageID *int64
	TelegramFileID    *string
	Status            string
	RetryCount        int64
	LastError         *string
	CreatedAt         int64
	UpdatedAt         int64
}

// PlanPart is splitter output: geometry without storage state.
type PlanPart struct {
	PartNumber  int64
	Size        int64
	StartOffset int64
	EndOffset   int64
}

const partColumns = `id, movie_id, part_number, size, start_offset, end_offset,
	telegram_message_id, telegram_file_id, status, retry_count, last_error,
	created_at, updated_at`

func scanPart(s rowScanner) (Part, error) {
	var p Part
	var msgID sql.NullInt64
	var fileID, lastErr sql.NullString
	err := s.Scan(&p.ID, &p.MovieID, &p.PartNumber, &p.Size, &p.StartOffset,
		&p.EndOffset, &msgID, &fileID, &p.Status, &p.RetryCount, &lastErr,
		&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return Part{}, err
	}
	if msgID.Valid {
		p.TelegramMessageID = &msgID.Int64
	}
	if fileID.Valid {
		p.TelegramFileID = &fileID.String
	}
	if lastErr.Valid {
		p.LastError = &lastErr.String
	}
	return p, nil
}

// CreateParts inserts a full split plan as WAITING rows.
// Call inside WithTx together with the movie status change.
func CreateParts(ctx context.Context, db DBTX, movieID int64, plan []PlanPart, now int64) error {
	for _, pp := range plan {
		_, err := db.ExecContext(ctx, `INSERT INTO movie_parts
			(movie_id, part_number, size, start_offset, end_offset, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			movieID, pp.PartNumber, pp.Size, pp.StartOffset, pp.EndOffset,
			PartWaiting, now, now)
		if err != nil {
			return err
		}
	}
	return nil
}

// ListParts returns all parts of a movie ordered by part_number.
func ListParts(ctx context.Context, db DBTX, movieID int64) ([]Part, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+partColumns+`
		FROM movie_parts WHERE movie_id = ? ORDER BY part_number`, movieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	parts := []Part{}
	for rows.Next() {
		p, err := scanPart(rows)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	return parts, rows.Err()
}

// PendingParts returns WAITING or FAILED parts in order: the resume set.
// COMPLETE parts are never re-uploaded.
func PendingParts(ctx context.Context, db DBTX, movieID int64) ([]Part, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+partColumns+`
		FROM movie_parts WHERE movie_id = ? AND status IN (?, ?)
		ORDER BY part_number`, movieID, PartWaiting, PartFailed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	parts := []Part{}
	for rows.Next() {
		p, err := scanPart(rows)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	return parts, rows.Err()
}

func touchPart(ctx context.Context, db DBTX, id int64, set string, args ...any) error {
	args = append(args, NowMillis(), id)
	res, err := db.ExecContext(ctx, `UPDATE movie_parts SET `+set+`, updated_at = ? WHERE id = ?`, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkPartUploading flags a part as in-flight.
func MarkPartUploading(ctx context.Context, db DBTX, partID int64) error {
	return touchPart(ctx, db, partID, `status = ?`, PartUploading)
}

// MarkPartComplete records Telegram locators and closes the part.
func MarkPartComplete(ctx context.Context, db DBTX, partID, messageID int64, fileID string) error {
	return touchPart(ctx, db, partID,
		`status = ?, telegram_message_id = ?, telegram_file_id = ?, last_error = NULL`,
		PartComplete, messageID, fileID)
}

// MarkPartFailed records a safe error message and bumps retry_count.
func MarkPartFailed(ctx context.Context, db DBTX, partID int64, errMsg string) error {
	res, err := db.ExecContext(ctx, `UPDATE movie_parts
		SET status = ?, last_error = ?, retry_count = retry_count + 1, updated_at = ?
		WHERE id = ?`, PartFailed, errMsg, NowMillis(), partID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ResetPartForRetry re-queues a FAILED (or WAITING) part without touching retry_count.
func ResetPartForRetry(ctx context.Context, db DBTX, partID int64) error {
	return touchPart(ctx, db, partID, `status = ?, last_error = NULL`, PartWaiting)
}

// VerifyParts checks a part list reconstructs videoSize exactly:
// contiguous 1..N numbering, [start,end) continuity, size == end-start,
// and total == videoSize. Pure function, no I/O.
func VerifyParts(parts []Part, videoSize int64) error {
	if len(parts) == 0 {
		return fmt.Errorf("no parts")
	}
	var total int64
	for i, p := range parts {
		wantNumber := int64(i + 1)
		if p.PartNumber != wantNumber {
			return fmt.Errorf("part %d: number %d, want %d", i, p.PartNumber, wantNumber)
		}
		if p.Size != p.EndOffset-p.StartOffset {
			return fmt.Errorf("part %d: size %d != end-start %d",
				p.PartNumber, p.Size, p.EndOffset-p.StartOffset)
		}
		if i == 0 {
			if p.StartOffset != 0 {
				return fmt.Errorf("part 1 starts at %d, want 0", p.StartOffset)
			}
		} else if p.StartOffset != parts[i-1].EndOffset {
			return fmt.Errorf("part %d starts at %d, want %d (end of part %d)",
				p.PartNumber, p.StartOffset, parts[i-1].EndOffset, parts[i-1].PartNumber)
		}
		total += p.Size
	}
	if total != videoSize {
		return fmt.Errorf("parts total %d != video size %d", total, videoSize)
	}
	if parts[len(parts)-1].EndOffset != videoSize {
		return fmt.Errorf("last end %d != video size %d", parts[len(parts)-1].EndOffset, videoSize)
	}
	return nil
}
