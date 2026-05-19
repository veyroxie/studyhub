package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ── Password Reset (public endpoint) ──────────────────────────────────────────

// handleForgotPassword issues a single-use password reset token and emails it
// to the user. The response is intentionally identical whether the email
// exists or not, to prevent attackers from probing for valid accounts.
func handleForgotPassword(db *DB) http.HandlerFunc {
	genericResponse := map[string]string{
		"message": "If an account with that email exists, a password reset link has been sent.",
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		req.Email = strings.ToLower(strings.TrimSpace(req.Email))
		if !validateEmail(req.Email) {
			respondError(w, "invalid email", 400)
			return
		}

		// Look up user — if not found, return the same response anyway.
		var userID int64
		var name sql.NullString
		err := db.QueryRow(`SELECT id, name FROM users WHERE email=?`, req.Email).Scan(&userID, &name)
		if err != nil {
			respond(w, genericResponse)
			return
		}

		// Invalidate any older outstanding reset tokens for this email — only the
		// most recent link should ever be valid.
		invalidateOldTokens(db, req.Email, tokenPurposeResetPassword)

		token, err := createEmailToken(db, req.Email, tokenPurposeResetPassword, &userID, nil, resetTokenTTL)
		l := logFromReq(r).With("email", req.Email, "user_id", userID)
		if err != nil {
			// Don't leak the failure to the client — log and return generic.
			l.Error("forgot-password token create failed", "err", err)
			respond(w, genericResponse)
			return
		}

		resetURL := appURL() + "/reset.html?token=" + token
		body := renderResetPasswordEmail(nullStr(name), resetURL)
		if err := mailer.Send(req.Email, "Reset your Study Hub password", body); err != nil {
			l.Error("forgot-password mail send failed", "err", err)
			// Still return generic success — don't expose mail errors to client.
		} else {
			l.Info("password reset email sent")
		}

		logAudit(db, req.Email, "password_reset_requested", "user", fmt.Sprintf("%d", userID), "reset email queued")

		respond(w, genericResponse)
	}
}

// handleSetPassword consumes a set_password token (issued when admin approves
// a teacher application or when an admin manually creates a user later) and
// writes the user's first password. On success it activates the account,
// issues an auth cookie, and returns user info so the frontend can redirect
// to the dashboard already logged in.
//
// POST /api/set-password  body: {token, newPassword}
func handleSetPassword(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Token       string `json:"token"`
			NewPassword string `json:"newPassword"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		if req.Token == "" {
			respondError(w, "token required", 400)
			return
		}
		if ok, msg := validatePassword(req.NewPassword); !ok {
			respondError(w, msg, 400)
			return
		}

		t, err := consumeEmailToken(db, req.Token, tokenPurposeSetPassword)
		if err != nil {
			respondError(w, "this link is invalid or has expired — please ask an admin to resend it", 400)
			return
		}
		if !t.UserID.Valid {
			respondError(w, "token has no associated account", 400)
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}

		// Activate the user and write the new password hash in a single
		// statement so we can fetch the role/name/email/tenant_id in the
		// same round trip — needed below to issue the JWT.
		var (
			role     string
			name     string
			email    string
			tenantID int
		)
		if err := db.QueryRow(
			`UPDATE users SET password_hash=?, status='active', email_verified_at=NOW() WHERE id=? RETURNING role, name, email, tenant_id`,
			string(hash), t.UserID.Int64,
		).Scan(&role, &name, &email, &tenantID); err != nil {
			respondError(w, "could not activate account", 500)
			return
		}

		// Password reset issues a default (short) session — the user can
		// opt into a remembered session via the login form on a future visit.
		jwtTok, err := makeToken(int(t.UserID.Int64), tenantID, email, role, name, tokenExpiryShort)
		if err != nil {
			respondError(w, "could not sign token", 500)
			return
		}
		secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
		http.SetCookie(w, &http.Cookie{
			Name:     "sh_token",
			Value:    jwtTok,
			Path:     "/",
			Expires:  time.Now().Add(tokenExpiryShort),
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		})

		logAudit(db, t.Email, "password_set", "user", fmt.Sprintf("%d", t.UserID.Int64), "")

		respond(w, map[string]any{
			"message":    "Password set. You're now signed in.",
			"role":       role,
			"name":       name,
			"email":      email,
			"redirectTo": "/",
		})
	}
}

// handleResetPassword consumes a reset token and writes a new password hash.
// POST /api/reset-password  body: {token, newPassword}
func handleResetPassword(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Token       string `json:"token"`
			NewPassword string `json:"newPassword"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		if req.Token == "" {
			respondError(w, "token required", 400)
			return
		}
		if ok, msg := validatePassword(req.NewPassword); !ok {
			respondError(w, msg, 400)
			return
		}

		t, err := consumeEmailToken(db, req.Token, tokenPurposeResetPassword)
		if err != nil {
			respondError(w, "this reset link is invalid or has expired — please request a new one", 400)
			return
		}
		if !t.UserID.Valid {
			respondError(w, "token has no associated account", 400)
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		if _, err := db.Exec(`UPDATE users SET password_hash=? WHERE id=?`, string(hash), t.UserID.Int64); err != nil {
			respondError(w, "could not update password", 500)
			return
		}
		logAudit(db, t.Email, "password_reset_completed", "user", fmt.Sprintf("%d", t.UserID.Int64), "")

		respond(w, map[string]string{"message": "Password updated. You can now log in with your new password."})
	}
}
