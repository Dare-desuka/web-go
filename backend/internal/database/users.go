package database

import (
	"context"
	"database/sql"
)

// User is an admin account. PasswordHash is never serialized to JSON.
type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"`
	CreatedAt    int64  `json:"created_at"`
}

// Roles known to the panel. Admin API requires RoleAdmin (enforced by
// the api layer, not here).
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

// CreateUser inserts an account. Hashing happens in the auth layer,
// this package only persists the resulting hash string.
func CreateUser(ctx context.Context, db DBTX, username, hash, role string) (int64, error) {
	if role == "" {
		role = "admin"
	}
	res, err := db.ExecContext(ctx, `INSERT INTO users
		(username, password_hash, role, created_at)
		VALUES (?, ?, ?, ?)`, username, hash, role, NowMillis())
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrAlreadyExists
		}
		return 0, err
	}
	return res.LastInsertId()
}

// GetUserByUsername loads credentials for login verification.
func GetUserByUsername(ctx context.Context, db DBTX, username string) (User, error) {
	var u User
	err := db.QueryRowContext(ctx, `SELECT id, username, password_hash, role, created_at
		FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return User{}, ErrNotFound
	}
	return u, err
}

// ListUsers returns all accounts ordered by id (hashes never serialized).
func ListUsers(ctx context.Context, db DBTX) ([]User, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, username, password_hash, role, created_at
		FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if out == nil {
		out = []User{}
	}
	return out, rows.Err()
}

// UpdateUserRole changes role; ErrNotFound for unknown ids.
func UpdateUserRole(ctx context.Context, db DBTX, id int64, role string) error {
	res, err := db.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, role, id)
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

// DeleteUser removes an account; sessions cascade via FK.
func DeleteUser(ctx context.Context, db DBTX, id int64) error {
	res, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
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

// GetUserByID loads a session owner.
func GetUserByID(ctx context.Context, db DBTX, id int64) (User, error) {
	var u User
	err := db.QueryRowContext(ctx, `SELECT id, username, password_hash, role, created_at
		FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return User{}, ErrNotFound
	}
	return u, err
}
