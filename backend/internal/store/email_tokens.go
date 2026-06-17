package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"studyhub/internal/core"
	"time"
)

// Email token purposes — kept as constants so typos turn into compile errors
// rather than silently-broken flows.
const (
	TokenPurposeVerifyParent  = "verify_parent"
	TokenPurposeVerifyTeacher = "verify_teacher"
	TokenPurposeResetPassword = "reset_password"
	TokenPurposeSetPassword   = "set_password"
)

// Token TTLs. Reset is short because it's the most attack-sensitive flow;
// verification and set-password are 24h since users may not click immediately.
const (
	ResetTokenTTL       = 1 * time.Hour
	VerifyTokenTTL      = 24 * time.Hour
	SetPasswordTokenTTL = 24 * time.Hour
)

// EmailToken is the in-memory representation of a row in email_tokens.
type EmailToken struct {
	Token          string
	Email          string
	Purpose        string
	UserID         sql.NullInt64
	RegistrationID sql.NullString
	ExpiresAt      time.Time
	UsedAt         sql.NullTime
}

// generateToken returns a 32-byte random hex string. Suitable for URLs and
// resistant to brute force at any practical attempt rate.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// createEmailToken inserts a fresh token row and returns it. Either userID or
// registrationID may be nil — verification tokens for new teachers, for example,
// have no user account yet.
func CreateEmailToken(db *DB, email, purpose string, userID *int64, registrationID *string, ttl time.Duration) (string, error) {
	token, err := GenerateToken()
	if err != nil {
		return "", err
	}
	var uid sql.NullInt64
	if userID != nil {
		uid = sql.NullInt64{Int64: *userID, Valid: true}
	}
	var rid sql.NullString
	if registrationID != nil {
		rid = sql.NullString{String: *registrationID, Valid: true}
	}
	expires := time.Now().Add(ttl)
	_, err = db.Exec(
		`INSERT INTO email_tokens(token,email,purpose,user_id,registration_id,expires_at) VALUES(?,?,?,?,?,?)`,
		token, email, purpose, uid, rid, expires,
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

// ErrTokenInvalid is returned for any token that's missing, expired, or already
// used. We collapse all three into one error so callers can't accidentally leak
// "this token was used yesterday" info to the user.
var ErrTokenInvalid = errors.New("token invalid or expired")

// consumeEmailToken validates a token and atomically marks it as used in the
// same UPDATE statement, so racing requests can't double-redeem the same token.
// Returns the loaded token row on success.
func ConsumeEmailToken(db *DB, token, expectedPurpose string) (*EmailToken, error) {
	var t EmailToken
	err := db.QueryRow(
		`UPDATE email_tokens SET used_at=NOW()
		 WHERE token=? AND purpose=? AND used_at IS NULL AND expires_at > NOW()
		 RETURNING token,email,purpose,user_id,registration_id,expires_at,used_at`,
		token, expectedPurpose,
	).Scan(&t.Token, &t.Email, &t.Purpose, &t.UserID, &t.RegistrationID, &t.ExpiresAt, &t.UsedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTokenInvalid
		}
		return nil, err
	}
	return &t, nil
}

// consumeEmailTokenAny is consumeEmailToken but accepts a set of allowed
// purposes. Used by endpoints that handle multiple flows through one URL
// (e.g. /api/verify-email handles both verify_parent and verify_teacher).
// The caller can switch on the returned token's Purpose field.
func ConsumeEmailTokenAny(db *DB, token string, purposes ...string) (*EmailToken, error) {
	if len(purposes) == 0 {
		return nil, ErrTokenInvalid
	}
	// Build "purpose IN (?, ?, ...)" — Postgres needs each placeholder
	// individually since pgx doesn't expand slices.
	placeholders := make([]string, len(purposes))
	args := make([]any, 0, len(purposes)+1)
	args = append(args, token)
	for i, p := range purposes {
		placeholders[i] = "?"
		args = append(args, p)
	}
	query := `UPDATE email_tokens SET used_at=NOW()
	          WHERE token=? AND purpose IN (` + strings.Join(placeholders, ",") + `)
	            AND used_at IS NULL AND expires_at > NOW()
	          RETURNING token,email,purpose,user_id,registration_id,expires_at,used_at`
	var t EmailToken
	err := db.QueryRow(query, args...).Scan(&t.Token, &t.Email, &t.Purpose, &t.UserID, &t.RegistrationID, &t.ExpiresAt, &t.UsedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTokenInvalid
		}
		return nil, err
	}
	return &t, nil
}

// invalidateOldTokens marks any unused tokens for (email, purpose) as used.
// Called before issuing a new token so the most recent link is the only valid
// one — protects against an attacker reusing a leaked link after the user
// requested a new one.
func InvalidateOldTokens(db *DB, email, purpose string) {
	if _, err := db.Exec(`UPDATE email_tokens SET used_at=NOW() WHERE email=? AND purpose=? AND used_at IS NULL`, email, purpose); err != nil {
		core.Logger.Error("failed to invalidate old email tokens", "err", err, "email", email, "purpose", purpose)
	}
}
