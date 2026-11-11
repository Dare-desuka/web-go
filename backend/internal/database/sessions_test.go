package database

import (
	"errors"
	"testing"
)

func TestSessions(t *testing.T) {
	db, ctx := openTestDB(t)
	userID, err := CreateUser(ctx, db, "sess-admin", "hash", "admin")
	if err != nil {
		t.Fatal(err)
	}
	now := NowMillis()

	s, err := CreateSession(ctx, db, userID, now+3600000)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.ID) != 64 {
		t.Fatalf("session id = %q, want 64 hex chars", s.ID)
	}

	got, err := GetSession(ctx, db, s.ID, now)
	if err != nil || got.UserID != userID {
		t.Fatalf("got %+v err %v", got, err)
	}
	if _, err := GetSession(ctx, db, "nonexistent", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing session err = %v", err)
	}

	// Expired sessions read as not-found and are purged.
	expired, _ := CreateSession(ctx, db, userID, now-1)
	if _, err := GetSession(ctx, db, expired.ID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session err = %v", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, expired.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("expired session row not purged")
	}

	if err := DeleteSession(ctx, db, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := GetSession(ctx, db, s.ID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted session err = %v", err)
	}
	// Logout twice is fine.
	if err := DeleteSession(ctx, db, s.ID); err != nil {
		t.Fatal(err)
	}
}

func TestSessionsCascadeOnUserDelete(t *testing.T) {
	db, ctx := openTestDB(t)
	userID, _ := CreateUser(ctx, db, "sess-cascade", "hash", "admin")
	if _, err := CreateSession(ctx, db, userID, NowMillis()+3600000); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("sessions survived user delete")
	}
}
