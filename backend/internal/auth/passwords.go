package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Password hashing strategy:
//
//   - New hashes use Argon2id (OWASP-recommended since 2015). PHC string
//     format so the parameters travel with the hash and we can bump cost
//     later without breaking older hashes.
//   - Existing bcrypt hashes ($2a$ / $2b$ / $2y$ prefix) are still verified —
//     we detect the algorithm by prefix.
//   - On a successful verify of a non-Argon2id hash, the caller should
//     rehash to Argon2id and overwrite the DB row. The login path
//     (auth.go) does this automatically.
//
// OWASP-recommended parameters for interactive auth (2024): memory 19 MiB,
// time 2, parallelism 1. We pick slightly heftier defaults (64 MiB, t=1)
// since this is a SaaS not a phone, and login is infrequent enough that
// 30-50ms per check is fine.
const (
	argonMemory      uint32 = 64 * 1024 // 64 MiB
	argonTime        uint32 = 1
	argonParallelism uint8  = 4
	argonSaltLen     uint32 = 16
	argonKeyLen      uint32 = 32
)

// hashPasswordArgon2id returns a PHC-formatted argon2id hash.
func hashPasswordArgon2id(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonParallelism, argonKeyLen)
	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory, argonTime, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
	return encoded, nil
}

// verifyArgon2id checks password against a PHC-encoded argon2id hash.
// Returns nil on match.
func verifyArgon2id(password, encoded string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return errors.New("not an argon2id hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return err
	}
	if version != argon2.Version {
		return errors.New("argon2 version mismatch")
	}
	var memory uint32
	var time uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &parallelism); err != nil {
		return err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return err
	}
	got := argon2.IDKey([]byte(password), salt, time, memory, parallelism, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return errors.New("argon2id: mismatch")
	}
	return nil
}

// isArgon2idHash reports whether the encoded string is an argon2id PHC hash.
func isArgon2idHash(encoded string) bool {
	return strings.HasPrefix(encoded, "$argon2id$")
}

// isBcryptHash reports whether the encoded string is a bcrypt hash.
func isBcryptHash(encoded string) bool {
	return strings.HasPrefix(encoded, "$2a$") ||
		strings.HasPrefix(encoded, "$2b$") ||
		strings.HasPrefix(encoded, "$2y$")
}

// verifyPassword checks a password against any supported hash format.
// Returns nil on match.
func VerifyPassword(hash, password string) error {
	if isArgon2idHash(hash) {
		return verifyArgon2id(password, hash)
	}
	if isBcryptHash(hash) {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	}
	return errors.New("unknown password hash format")
}

// hashShouldRehash returns true when the hash uses an outdated algorithm
// (bcrypt) and the caller should rehash the password on this login to
// upgrade to Argon2id.
func hashShouldRehash(hash string) bool {
	return !isArgon2idHash(hash)
}

// hashPasswordBytes is a []byte-returning convenience wrapper for callers
// (mostly handlers_password.go) that previously consumed bcrypt's []byte
// return type. Keeps the migration diff small.
func HashPasswordBytes(password string) ([]byte, error) {
	s, err := hashPasswordArgon2id(password)
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}
