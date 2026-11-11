package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
)

// Session is a server-side admin login. The cookie holds only ID.
type Session struct {
	ID        string
	UserID    int64
	ExpiresAt int64
	CreatedAt int64
}

// NewSessionID returns a 256-bit random hex ID.
func NewSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CreateSession persists a login valid until expiresAt (Unix ms).
func CreateSession(ctx context.Context, db DBTX, userID, expiresAt int64) (Session, error) {
	id, err := NewSessionID()
	if err != nil {
		return Session{}, err
	}
	now := NowMillis()
	_, err = db.ExecContext(ctx, `INSERT INTO sessions (id, user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?)`, id, userID, expiresAt, now)
	if err != nil {
		return Session{}, err
	}
	return Session{ID: id, UserID: userID, ExpiresAt: expiresAt, CreatedAt: now}, nil
}

// GetSession loads a session only if it has not expired; expired rows are
// removed and reported as ErrNotFound so callers just see "logged out".
func GetSession(ctx context.Context, db DBTX, id string, now int64) (Session, error) {
	var s Session
	err := db.QueryRowContext(ctx, `SELECT id, user_id, expires_at, created_at
		FROM sessions WHERE id = ?`, id).
		Scan(&s.ID, &s.UserID, &s.ExpiresAt, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	if now >= s.ExpiresAt {
		_, _ = db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
		return Session{}, ErrNotFound
	}
	return s, nil
}

// DeleteSession logs out one session; missing IDs are not an error.
func DeleteSession(ctx context.Context, db DBTX, id string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// DeleteExpiredSessions purges stale rows; run opportunistically on login.
func DeleteExpiredSessions(ctx context.Context, db DBTX, now int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now)
	return err
}
