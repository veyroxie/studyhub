package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"studyhub/internal/core"
	"time"
)

// Refresh token cookie lifetime. Independent of the access-token TTL —
// the refresh token can outlive a single JWT and is used to mint fresh
// JWTs without re-prompting for credentials.
const (
	refreshTokenTTL   = 7 * 24 * time.Hour
	refreshCookieName = "sh_refresh"
	refreshCookiePath = "/api/auth"
)

// issueRefreshToken mints a fresh refresh token, persists its hash, and
// writes the cookie. Returns the cleartext (only the cookie carries it;
// the DB stores SHA-256). Used by login + by the rotation endpoint.
//
// family is empty for the first token of a new session (a new family is
// generated); subsequent rotations pass the same family so reuse detection
// can trace a stolen cookie back to all its siblings.
func IssueRefreshToken(db *DB, w http.ResponseWriter, r *http.Request, userID, tenantID int, family string) error {
	cleartext, err := GenerateToken()
	if err != nil {
		return err
	}
	if family == "" {
		family, err = GenerateToken()
		if err != nil {
			return err
		}
	}
	sum := sha256.Sum256([]byte(cleartext))
	hash := hex.EncodeToString(sum[:])
	if _, err := db.Exec(
		`INSERT INTO refresh_tokens(token_hash, token_family, user_id, tenant_id, expires_at, user_agent, ip) VALUES(?,?,?,?,?,?,?)`,
		hash, family, userID, tenantID, time.Now().Add(refreshTokenTTL),
		r.UserAgent(), clientIP(r),
	); err != nil {
		return err
	}
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    cleartext,
		Path:     refreshCookiePath,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode, // Strict — never sent cross-site
		Expires:  time.Now().Add(refreshTokenTTL),
	})
	return nil
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return v
	}
	return r.RemoteAddr
}

// handleRefresh exchanges a valid refresh token for a new access JWT + a
// rotated refresh token. Implements reuse detection: if the presented
// token was already used, the entire family is revoked and the request
// fails — the assumption being that the cookie was stolen.
//
// POST /api/auth/refresh
func HandleRefresh(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(refreshCookieName)
		if err != nil || cookie.Value == "" {
			core.RespondError(w, "missing refresh token", http.StatusUnauthorized)
			return
		}
		sum := sha256.Sum256([]byte(cookie.Value))
		hash := hex.EncodeToString(sum[:])

		// Look up the token by hash. Need usage state + family for reuse
		// detection.
		var (
			family    string
			userID    int
			tenantID  int
			expiresAt time.Time
			usedAt    *time.Time
			revokedAt *time.Time
		)
		var usedRaw, revokedRaw any
		if err := db.QueryRow(
			`SELECT token_family, user_id, tenant_id, expires_at, used_at, revoked_at FROM refresh_tokens WHERE token_hash=?`,
			hash,
		).Scan(&family, &userID, &tenantID, &expiresAt, &usedRaw, &revokedRaw); err != nil {
			core.RespondError(w, "invalid refresh token", http.StatusUnauthorized)
			return
		}
		if t, ok := usedRaw.(time.Time); ok {
			usedAt = &t
		}
		if t, ok := revokedRaw.(time.Time); ok {
			revokedAt = &t
		}

		// Reuse detection: already-used or revoked token is a strong
		// indicator of theft. Burn down the entire family.
		if usedAt != nil || revokedAt != nil {
			db.Exec(`UPDATE refresh_tokens SET revoked_at=NOW() WHERE token_family=? AND revoked_at IS NULL`, family)
			core.LogAudit(db, "system", "refresh_token_reused", "user", "", "family="+family+" — possible theft, family revoked")
			core.RespondError(w, "session terminated — please sign in again", http.StatusUnauthorized)
			return
		}
		if time.Now().After(expiresAt) {
			core.RespondError(w, "refresh token expired", http.StatusUnauthorized)
			return
		}

		// Mark old token used + look up user details for the new JWT.
		var email, role, name string
		var status string
		if err := db.QueryRow(
			`SELECT email, COALESCE(role,''), COALESCE(name,''), COALESCE(status,'active') FROM users WHERE id=?`,
			userID,
		).Scan(&email, &role, &name, &status); err != nil {
			core.RespondError(w, "user no longer exists", http.StatusUnauthorized)
			return
		}
		if status != "active" {
			db.Exec(`UPDATE refresh_tokens SET revoked_at=NOW() WHERE token_family=? AND revoked_at IS NULL`, family)
			core.RespondError(w, "account is "+status, http.StatusForbidden)
			return
		}
		if _, err := db.Exec(`UPDATE refresh_tokens SET used_at=NOW() WHERE token_hash=?`, hash); err != nil {
			core.RespondError(w, "could not rotate token", http.StatusInternalServerError)
			return
		}

		// Issue new pair: rotated refresh cookie + fresh access JWT cookie.
		if err := IssueRefreshToken(db, w, r, userID, tenantID, family); err != nil {
			core.RespondError(w, "could not issue refresh token", http.StatusInternalServerError)
			return
		}
		// Access TTL: keep parity with the original login choice. We can't
		// know if the original was a "Remember Me" without looking — use
		// the shorter default to be conservative; the user can always log
		// in fresh if they want a 30-day session again.
		core.IssueAuthCookie(w, r, userID, tenantID, email, role, name, false)
		core.Respond(w, map[string]string{"status": "rotated"})
	}
}

// revokeRefreshFamily marks every refresh token for a user as revoked.
// Called from password change and admin suspension so a still-cached
// refresh token can't mint new access tokens.
func RevokeRefreshFamilyByUser(db *DB, userID int, reason string) {
	if userID == 0 {
		return
	}
	_, err := db.Exec(`UPDATE refresh_tokens SET revoked_at=NOW() WHERE user_id=? AND revoked_at IS NULL`, userID)
	if err != nil {
		core.Logger.Error("revoke refresh family failed", "err", err, "user_id", userID)
		return
	}
	core.LogAudit(db, "system", "refresh_family_revoked", "user", "", reason)
}

// purgeExpiredRefreshTokens runs alongside the email-token cleanup.
func PurgeExpiredRefreshTokens(db *DB) error {
	if _, err := db.Exec(`DELETE FROM refresh_tokens WHERE expires_at < NOW() OR revoked_at < NOW() - INTERVAL '7 days'`); err != nil {
		return errors.New("purge refresh tokens: " + err.Error())
	}
	return nil
}
