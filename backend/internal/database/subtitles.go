package database

import (
	"context"
	"database/sql"
)

// Subtitle is one WebVTT track. Only existing languages are served (PRD 17).
type Subtitle struct {
	ID               int64   `json:"id"`
	MovieID          int64   `json:"movie_id"`
	Language         string  `json:"language"`
	Label            string  `json:"label"`
	FilePath         string  `json:"-"`
	TelegramFileID    *string `json:"-"`
	TelegramMessageID *int64  `json:"-"`
	CreatedAt        int64   `json:"created_at"`
}

// SubtitleExists reports whether a movie already has a track language.
// Kept (not folded into the insert error): the upload handler must refuse
// BEFORE overwriting the existing file on disk.
func SubtitleExists(ctx context.Context, db DBTX, movieID int64, language string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subtitles WHERE movie_id = ? AND language = ?`,
		movieID, language).Scan(&n)
	return n > 0, err
}

// AddSubtitle registers a WebVTT file for a movie language.
func AddSubtitle(ctx context.Context, db DBTX, s Subtitle) (int64, error) {
	if s.CreatedAt == 0 {
		s.CreatedAt = NowMillis()
	}
	res, err := db.ExecContext(ctx, `INSERT INTO subtitles
		(movie_id, language, label, file_path, telegram_file_id, telegram_message_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.MovieID, s.Language, s.Label, s.FilePath,
		nullableString(s.TelegramFileID), nullableInt64(s.TelegramMessageID), s.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrAlreadyExists
		}
		return 0, err
	}
	return res.LastInsertId()
}

// ListSubtitles returns all tracks for a movie ordered by language.
func ListSubtitles(ctx context.Context, db DBTX, movieID int64) ([]Subtitle, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, movie_id, language, label, file_path, telegram_file_id, telegram_message_id, created_at
		FROM subtitles WHERE movie_id = ? ORDER BY language`, movieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	subs := []Subtitle{}
	for rows.Next() {
		var s Subtitle
		var tf sql.NullString
		var tm sql.NullInt64
		if err := rows.Scan(&s.ID, &s.MovieID, &s.Language, &s.Label, &s.FilePath, &tf, &tm, &s.CreatedAt); err != nil {
			return nil, err
		}
		if tf.Valid {
			s.TelegramFileID = &tf.String
		}
		if tm.Valid {
			s.TelegramMessageID = &tm.Int64
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// DeleteSubtitle removes one track of a movie.
func DeleteSubtitle(ctx context.Context, db DBTX, id, movieID int64) error {
	res, err := db.ExecContext(ctx, `DELETE FROM subtitles WHERE id = ? AND movie_id = ?`, id, movieID)
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

// GetSubtitleByID loads a track; callers compare MovieID when scoping matters.
func GetSubtitleByID(ctx context.Context, db DBTX, id int64) (Subtitle, error) {
	var s Subtitle
	var tf sql.NullString
	var tm sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT id, movie_id, language, label, file_path, telegram_file_id, telegram_message_id, created_at
		FROM subtitles WHERE id = ?`, id).
		Scan(&s.ID, &s.MovieID, &s.Language, &s.Label, &s.FilePath, &tf, &tm, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return Subtitle{}, ErrNotFound
	}
	if err != nil {
		return Subtitle{}, err
	}
	if tf.Valid {
		s.TelegramFileID = &tf.String
	}
	if tm.Valid {
		s.TelegramMessageID = &tm.Int64
	}
	return s, nil
}
