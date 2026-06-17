package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/mailer"
	"studyhub/internal/models"
	"studyhub/internal/store"
	"time"
)

// ── Password Reset (public endpoint) ──────────────────────────────────────────

// handleForgotPassword issues a single-use password reset token and emails it
// to the user. The response is intentionally identical whether the email
// exists or not, to prevent attackers from probing for valid accounts.
func HandleForgotPassword(db *store.DB) http.HandlerFunc {
	genericResponse := map[string]string{
		"message": "If an account with that email exists, a password reset link has been sent.",
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		req.Email = strings.ToLower(strings.TrimSpace(req.Email))
		if !auth.ValidateEmail(req.Email) {
			core.RespondError(w, "invalid email", 400)
			return
		}

		// Look up user — if not found, return the same response anyway.
		var userID int64
		var name sql.NullString
		err := db.QueryRow(`SELECT id, name FROM users WHERE email=?`, req.Email).Scan(&userID, &name)
		if err != nil {
			core.Respond(w, genericResponse)
			return
		}

		// Invalidate any older outstanding reset tokens for this email — only the
		// most recent link should ever be valid.
		store.InvalidateOldTokens(db, req.Email, store.TokenPurposeResetPassword)

		token, err := store.CreateEmailToken(db, req.Email, store.TokenPurposeResetPassword, &userID, nil, store.ResetTokenTTL)
		l := core.LogFromReq(r).With("email", req.Email, "user_id", userID)
		if err != nil {
			// Don't leak the failure to the client — log and return generic.
			l.Error("forgot-password token create failed", "err", err)
			core.Respond(w, genericResponse)
			return
		}

		resetURL := mailer.AppURL() + "/reset.html?token=" + token
		body := mailer.RenderResetPasswordEmail(models.NullStr(name), resetURL)
		if err := core.SendEmail(req.Email, "Reset your Study Hub password", body); err != nil {
			l.Error("forgot-password mail send failed", "err", err)
			// Still return generic success — don't expose mail errors to client.
		} else {
			l.Info("password reset email sent")
		}

		core.LogAudit(db, req.Email, "password_reset_requested", "user", fmt.Sprintf("%d", userID), "reset email queued")

		core.Respond(w, genericResponse)
	}
}

// handleSetPassword consumes a set_password token (issued when admin approves
// a teacher application or when an admin manually creates a user later) and
// writes the user's first password. On success it activates the account,
// issues an auth cookie, and returns user info so the frontend can redirect
// to the dashboard already logged in.
//
// POST /api/set-password  body: {token, newPassword}
func HandleSetPassword(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Token       string `json:"token"`
			NewPassword string `json:"newPassword"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		if req.Token == "" {
			core.RespondError(w, "token required", 400)
			return
		}
		if ok, msg := auth.ValidatePassword(req.NewPassword); !ok {
			core.RespondError(w, msg, 400)
			return
		}

		t, err := store.ConsumeEmailToken(db, req.Token, store.TokenPurposeSetPassword)
		if err != nil {
			core.RespondError(w, "this link is invalid or has expired — please ask an admin to resend it", 400)
			return
		}
		if !t.UserID.Valid {
			core.RespondError(w, "token has no associated account", 400)
			return
		}

		hash, err := auth.HashPasswordBytes(req.NewPassword)
		if err != nil {
			core.RespondError(w, "server error", 500)
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
			core.RespondError(w, "could not activate account", 500)
			return
		}

		// Password reset issues a default (short) session — the user can
		// opt into a remembered session via the login form on a future visit.
		jwtTok, err := auth.MakeToken(int(t.UserID.Int64), tenantID, email, role, name, auth.TokenExpiryShort)
		if err != nil {
			core.RespondError(w, "could not sign token", 500)
			return
		}
		secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
		http.SetCookie(w, &http.Cookie{
			Name:     "sh_token",
			Value:    jwtTok,
			Path:     "/",
			Expires:  time.Now().Add(auth.TokenExpiryShort),
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		})

		core.LogAudit(db, t.Email, "password_set", "user", fmt.Sprintf("%d", t.UserID.Int64), "")

		core.Respond(w, map[string]any{
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
func HandleResetPassword(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Token       string `json:"token"`
			NewPassword string `json:"newPassword"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		if req.Token == "" {
			core.RespondError(w, "token required", 400)
			return
		}
		if ok, msg := auth.ValidatePassword(req.NewPassword); !ok {
			core.RespondError(w, msg, 400)
			return
		}

		t, err := store.ConsumeEmailToken(db, req.Token, store.TokenPurposeResetPassword)
		if err != nil {
			core.RespondError(w, "this reset link is invalid or has expired — please request a new one", 400)
			return
		}
		if !t.UserID.Valid {
			core.RespondError(w, "token has no associated account", 400)
			return
		}

		hash, err := auth.HashPasswordBytes(req.NewPassword)
		if err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		// Resetting the password also clears any pending lockout — otherwise
		// a user who was locked out and reset their password would still hit
		// the "account locked" wall on their next login.
		if _, err := db.Exec(`UPDATE users SET password_hash=?, failed_login_count=0, locked_until=NULL WHERE id=?`, string(hash), t.UserID.Int64); err != nil {
			core.RespondError(w, "could not update password", 500)
			return
		}
		core.LogAudit(db, t.Email, "password_reset_completed", "user", fmt.Sprintf("%d", t.UserID.Int64), "")

		core.Respond(w, map[string]string{"message": "Password updated. You can now log in with your new password."})
	}
}
