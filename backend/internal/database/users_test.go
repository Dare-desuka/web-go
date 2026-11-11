package database

import (
	"errors"
	"testing"
)

func TestUsers(t *testing.T) {
	db, ctx := openTestDB(t)

	id, err := CreateUser(ctx, db, "admin", "argon2id-hash", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateUser(ctx, db, "admin", "other", "admin"); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("dup username err = %v", err)
	}

	u, err := GetUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != id || u.PasswordHash != "argon2id-hash" || u.Role != "admin" {
		t.Fatalf("got %+v", u)
	}
	if _, err := GetUserByID(ctx, db, id); err != nil {
		t.Fatal(err)
	}
	if _, err := GetUserByUsername(ctx, db, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing user err = %v", err)
	}
}
