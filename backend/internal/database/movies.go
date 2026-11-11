package database

import (
	"context"
	"database/sql"
	"fmt"
)

// Movie statuses (PRD section 13). Only READY is publicly visible.
const (
	StatusDRAFT      = "DRAFT"
	StatusUPLOADING  = "UPLOADING"
	StatusPROCESSING = "PROCESSING"
	StatusREADY      = "READY"
	StatusERROR      = "ERROR"
	StatusDELETING   = "DELETING"
)

// allowedTransitions gates SetMovieStatus. ERROR can go back to UPLOADING
// (retry); anything can move to DELETING (admin delete flow).
var allowedTransitions = map[string][]string{
	StatusDRAFT:      {StatusUPLOADING, StatusDELETING},
	StatusUPLOADING:  {StatusPROCESSING, StatusERROR, StatusDELETING},
	StatusPROCESSING: {StatusREADY, StatusERROR, StatusDELETING},
	StatusREADY:      {StatusDRAFT, StatusDELETING},
	StatusERROR:      {StatusUPLOADING, StatusDELETING},
	StatusDELETING:   {},
}

// ValidateTransition reports whether from -> to is a legal status change.
func ValidateTransition(from, to string) error {
	for _, next := range allowedTransitions[from] {
		if next == to {
			return nil
		}
	}
	return ErrInvalidTransition
}

// Movie is the public-safe movie row. Timestamps are Unix milliseconds.
// Thumb Telegram locators are internal (never serialized); the public thumb URL
// is the convention path GET /api/thumbs/{id}.
type Movie struct {
	ID              int64   `json:"id"`
	Title           string  `json:"title"`
	Slug            string  `json:"slug"`
	Description     string  `json:"description"`
	Year            *int64  `json:"year,omitempty"`
	DurationSeconds *int64  `json:"duration_seconds,omitempty"`
	PosterURL       *string `json:"poster_url,omitempty"`
	BackdropURL     *string `json:"backdrop_url,omitempty"`
	Genres          string  `json:"genres,omitempty"`
	VideoSize       int64   `json:"video_size"`
	VideoMime       string  `json:"video_mime"`
	Status          string  `json:"status"`
	CreatedAt       int64   `json:"created_at"`
	UpdatedAt       int64   `json:"updated_at"`
	// SubtitleCount is query-time only (admin ListAll); omitempty keeps public JSON unchanged.
	SubtitleCount int `json:"subtitle_count,omitempty"`

	ThumbTelegramFileID    *string `json:"-"`
	ThumbTelegramMessageID *int64  `json:"-"`
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMovie(s rowScanner) (Movie, error) {
	var m Movie
	var year, dur sql.NullInt64
	var poster, backdrop, genres sql.NullString
	var thumbFile sql.NullString
	var thumbMsg sql.NullInt64
	err := s.Scan(&m.ID, &m.Title, &m.Slug, &m.Description, &year,
		&dur, &poster, &backdrop, &genres, &m.VideoSize, &m.VideoMime,
		&m.Status, &m.CreatedAt, &m.UpdatedAt, &thumbFile, &thumbMsg)
	if err != nil {
		return Movie{}, err
	}
	if year.Valid {
		m.Year = &year.Int64
	}
	if dur.Valid {
		m.DurationSeconds = &dur.Int64
	}
	if poster.Valid {
		m.PosterURL = &poster.String
	}
	if backdrop.Valid {
		m.BackdropURL = &backdrop.String
	}
	if genres.Valid {
		m.Genres = genres.String
	}
	if thumbFile.Valid {
		m.ThumbTelegramFileID = &thumbFile.String
	}
	if thumbMsg.Valid {
		m.ThumbTelegramMessageID = &thumbMsg.Int64
	}
	return m, nil
}

const movieColumns = `id, title, slug, description, year, duration_seconds,
	poster_url, backdrop_url, genres, video_size, video_mime, status, created_at, updated_at,
	thumb_telegram_file_id, thumb_telegram_message_id`

func nullableInt64(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

func nullableString(v *string) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *v, Valid: true}
}

// CreateMovie inserts a movie row, defaulting empty status to DRAFT.
// ID, CreatedAt and UpdatedAt are filled when zero.
func CreateMovie(ctx context.Context, db DBTX, m Movie) (int64, error) {
	if m.Status == "" {
		m.Status = StatusDRAFT
	}
	now := NowMillis()
	if m.CreatedAt == 0 {
		m.CreatedAt = now
	}
	if m.UpdatedAt == 0 {
		m.UpdatedAt = now
	}
	res, err := db.ExecContext(ctx, `INSERT INTO movies
		(title, slug, description, year, duration_seconds, poster_url, backdrop_url,
		 genres, video_size, video_mime, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Title, m.Slug, m.Description, nullableInt64(m.Year),
		nullableInt64(m.DurationSeconds), nullableString(m.PosterURL),
		nullableString(m.BackdropURL), m.Genres, m.VideoSize, m.VideoMime,
		m.Status, m.CreatedAt, m.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrAlreadyExists
		}
		return 0, err
	}
	return res.LastInsertId()
}

// GetMovieByID returns a movie of any status (admin use).
func GetMovieByID(ctx context.Context, db DBTX, id int64) (Movie, error) {
	m, err := scanMovie(db.QueryRowContext(ctx, `SELECT `+movieColumns+`
		FROM movies WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return Movie{}, ErrNotFound
	}
	return m, err
}

// GetReadyMovieByID returns a movie only if it is READY (public use).
func GetReadyMovieByID(ctx context.Context, db DBTX, id int64) (Movie, error) {
	m, err := scanMovie(db.QueryRowContext(ctx, `SELECT `+movieColumns+`
		FROM movies WHERE id = ? AND status = ?`, id, StatusREADY))
	if err == sql.ErrNoRows {
		return Movie{}, ErrNotFound
	}
	return m, err
}

// ListAll returns every movie newest-first for the admin dashboard.
func ListAll(ctx context.Context, db DBTX) ([]Movie, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+movieColumns+`
		FROM movies ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	movies := []Movie{}
	for rows.Next() {
		m, err := scanMovie(rows)
		if err != nil {
			return nil, err
		}
		movies = append(movies, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// ponytail: one GROUP BY beats N+1 counts; add index on subtitles(movie_id) if slow
	crows, err := db.QueryContext(ctx, `SELECT movie_id, COUNT(*) FROM subtitles GROUP BY movie_id`)
	if err != nil {
		return nil, err
	}
	defer crows.Close()
	counts := map[int64]int{}
	for crows.Next() {
		var mid int64
		var n int
		if err := crows.Scan(&mid, &n); err != nil {
			return nil, err
		}
		counts[mid] = n
	}
	if err := crows.Err(); err != nil {
		return nil, err
	}
	for i := range movies {
		movies[i].SubtitleCount = counts[movies[i].ID]
	}
	return movies, nil
}

// ListReady returns READY movies ordered by newest first.
func ListReady(ctx context.Context, db DBTX) ([]Movie, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+movieColumns+`
		FROM movies WHERE status = ? ORDER BY id DESC`, StatusREADY)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	movies := []Movie{}
	for rows.Next() {
		m, err := scanMovie(rows)
		if err != nil {
			return nil, err
		}
		movies = append(movies, m)
	}
	return movies, rows.Err()
}

// UpdateMovieMeta edits catalog fields (not size, mime or status).
func UpdateMovieMeta(ctx context.Context, db DBTX, id int64, m Movie) error {
	res, err := db.ExecContext(ctx, `UPDATE movies SET
		title = ?, slug = ?, description = ?, year = ?,
		poster_url = ?, backdrop_url = ?, genres = ?, updated_at = ?
		WHERE id = ?`,
		m.Title, m.Slug, m.Description, nullableInt64(m.Year),
		nullableString(m.PosterURL), nullableString(m.BackdropURL),
		m.Genres, NowMillis(), id)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
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

// UpdateMovieSource records the uploaded video's size and mime.
// Used by the upload endpoint after receiving the file.
func UpdateMovieSource(ctx context.Context, db DBTX, id, size int64, mime string) error {
	res, err := db.ExecContext(ctx, `UPDATE movies SET
		video_size = ?, video_mime = ?, updated_at = ? WHERE id = ?`,
		size, mime, NowMillis(), id)
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

// SetMovieStatus transitions a movie after validating the state machine.
func SetMovieStatus(ctx context.Context, db DBTX, id int64, to string) error {
	var from string
	err := db.QueryRowContext(ctx, `SELECT status FROM movies WHERE id = ?`, id).Scan(&from)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := ValidateTransition(from, to); err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `UPDATE movies SET status = ?, updated_at = ? WHERE id = ?`,
		to, NowMillis(), id)
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

// PublishMovie moves a DRAFT movie to READY only after verifying its parts
// reconstruct the declared video size exactly. The only verified path to
// READY outside the upload pipeline (republish after unpublish).
func PublishMovie(ctx context.Context, db DBTX, id int64) error {
	var status string
	var videoSize int64
	err := db.QueryRowContext(ctx, `SELECT status, video_size FROM movies WHERE id = ?`, id).
		Scan(&status, &videoSize)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != StatusDRAFT {
		return ErrInvalidTransition
	}
	parts, err := ListParts(ctx, db, id)
	if err != nil {
		return err
	}
	for _, p := range parts {
		if p.Status != PartComplete {
			return fmt.Errorf("part %d is %s", p.PartNumber, p.Status)
		}
	}
	if err := VerifyParts(parts, videoSize); err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `UPDATE movies SET status = ?, updated_at = ? WHERE id = ?`,
		StatusREADY, NowMillis(), id)
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

// SetThumbTelegram persists the Telegram locator for the auto thumbnail.
func SetThumbTelegram(ctx context.Context, db DBTX, id int64, fileID string, messageID int64) error {
	res, err := db.ExecContext(ctx, `UPDATE movies SET
		thumb_telegram_file_id = ?, thumb_telegram_message_id = ?, updated_at = ?
		WHERE id = ?`, fileID, messageID, NowMillis(), id)
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

// DeleteMovie removes a movie; parts/subtitles/progress cascade via FK.
func DeleteMovie(ctx context.Context, db DBTX, id int64) error {
	res, err := db.ExecContext(ctx, `DELETE FROM movies WHERE id = ?`, id)
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
