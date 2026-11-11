// Package auth owns admin credential verification and session lifetime.
// Password hashes never leave this package as plaintext; sessions are
// random IDs persisted server-side (PRD 28.6), never JWT.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// SessionTTL is the admin login lifetime: 24 hours (PRD 28.6).
const SessionTTL = 24 * time.Hour

const (
	argonTime    = 1
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

var (
	ErrEmptyPassword = errors.New("empty password")
	ErrInvalidHash   = errors.New("invalid hash format")
)

// Hash returns a PHC-style Argon2id string: $argon2id$v=19$m=..,t=..,p=..$salt$hash.
func Hash(password string) (string, error) {
	if password == "" {
		return "", ErrEmptyPassword
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding.EncodeToString
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		b64(salt), b64(hash)), nil
}

// Verify checks password against a Hash-produced string in constant time.
func Verify(encodedHash, password string) error {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return ErrInvalidHash
	}
	var version int
	var mem uint32
	var t uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return ErrInvalidHash
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &threads); err != nil {
		return ErrInvalidHash
	}
	if version != argon2.Version {
		return ErrInvalidHash
	}
	b64 := base64.RawStdEncoding.DecodeString
	salt, err := b64(parts[4])
	if err != nil {
		return ErrInvalidHash
	}
	want, err := b64(parts[5])
	if err != nil {
		return ErrInvalidHash
	}
	got := argon2.IDKey([]byte(password), salt, t, mem, threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return errors.New("invalid credentials")
	}
	return nil
}
