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

// ── Email verification (public endpoint) ─────────────────────────────────────

// handleVerifyEmail consumes a verification token. The behavior depends on
// the token's purpose:
//
//   - verify_parent: activates the existing user, issues an auth cookie,
//     responds with redirectTo=/ so the frontend lands on the dashboard.
//   - verify_teacher: just marks the registration as email-verified so admin
//     can see "verified" in the queue. NO user account is created here —
//     that happens at admin approval time. Response has no cookie and a
//     "thanks, in review" message instead of a redirect.
//
// GET /api/verify-email?token=xxx
func HandleVerifyEmail(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			core.RespondError(w, "missing token", 400)
			return
		}

		t, err := store.ConsumeEmailTokenAny(db, token, store.TokenPurposeVerifyParent, store.TokenPurposeVerifyTeacher)
		if err != nil {
			core.RespondError(w, "this verification link is invalid or has expired — please request a new one", 400)
			return
		}

		// Always update the linked registration row's verified-at timestamp,
		// regardless of token purpose. This drives the admin "verified" badge.
		if t.RegistrationID.Valid {
			db.Exec(`UPDATE registrations SET email_verified_at=NOW() WHERE id=?`, t.RegistrationID.String)
		}

		// ── Teacher branch: confirm only, no login ─────────────────────────
		if t.Purpose == store.TokenPurposeVerifyTeacher {
			core.LogAudit(db, store.TenantOfRegistration(db, t.RegistrationID.String), t.Email, "teacher_email_verified", "registration", nullStrFromPtr(&t.RegistrationID), "")
			core.Respond(w, map[string]any{
				"type":       "teacher",
				"message":    "Email confirmed. Your application is now in our review queue — we'll be in touch within 3-5 business days.",
				"redirectTo": nil,
			})

			return
		}

		// ── Parent branch: activate user + auto-login ──────────────────────
		if !t.UserID.Valid {
			core.RespondError(w, "token has no associated account", 400)
			return
		}
		var (
			role     string
			name     string
			email    string
			tenantID int
		)
		if err := db.QueryRow(
			`UPDATE users SET status='active', email_verified_at=NOW() WHERE id=? RETURNING role, name, email, tenant_id`,
			t.UserID.Int64,
		).Scan(&role, &name, &email, &tenantID); err != nil {
			core.RespondError(w, "could not activate account", 500)
			return
		}

		// Email verification activates the account and issues a default
		// (short) session. The user can opt into a remembered session via the
		// login form on a future visit.
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

		core.LogAudit(db, store.TenantOfUser(db, t.UserID.Int64), email, "email_verified", "user", fmt.Sprintf("%d", t.UserID.Int64), "")

		core.Respond(w, map[string]any{
			"type":       "parent",
			"message":    "Email verified. You're now signed in.",
			"role":       role,
			"name":       name,
			"email":      email,
			"redirectTo": "/",
		})

	}
}

// nullStrFromPtr converts a *sql.NullString to a plain string for audit logs.
// If the pointer is nil or invalid, returns an empty string.
func nullStrFromPtr(s *sql.NullString) string {
	if s == nil || !s.Valid {
		return ""
	}
	return s.String
}

// handleResendVerification re-issues a fresh verification email for either
// a pending parent account OR a pending teacher application. Like
// forgot-password, it returns the same generic response in every case to
// prevent enumeration.
//
// Resolution order:
//  1. If a `users` row exists with status=pending_verification, re-send the
//     parent verification email.
//  2. Else if a `registrations` row exists with type=teacher and the
//     registration's email_verified_at is still NULL, re-send the teacher
//     confirmation email.
//  3. Else: return generic "if it exists, we sent it" response.
//
// POST /api/resend-verification  body: {email}
func HandleResendVerification(db *store.DB) http.HandlerFunc {
	generic := map[string]string{
		"message": "If a pending account or application exists for that email, a fresh confirmation link has been sent.",
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

		// ── Branch 1: pending user (parent OR teacher) ─────────────────────
		// Teachers reach this branch when they were approved by admin (which
		// creates the users row in pending_verification) but never clicked
		// the set-password link. They need a SetPassword token + the
		// teacher-welcome template, not the parent verify flow.
		var userID int64
		var userName sql.NullString
		var userStatus, userRole string
		err := db.QueryRow(`SELECT id, name, COALESCE(status,'active'), COALESCE(role,'parent') FROM users WHERE email=?`, req.Email).Scan(&userID, &userName, &userStatus, &userRole)
		if err == nil && userStatus == "pending_verification" {
			l := core.LogFromReq(r).With("email", req.Email, "user_id", userID, "role", userRole)

			if userRole == "teacher" {
				store.InvalidateOldTokens(db, req.Email, store.TokenPurposeSetPassword)
				token, terr := store.CreateEmailToken(db, req.Email, store.TokenPurposeSetPassword, &userID, nil, store.SetPasswordTokenTTL)
				if terr != nil {
					l.Error("resend-verification token create failed", "err", terr)
					core.Respond(w, generic)
					return
				}
				setURL := mailer.AppURL() + "/set-password.html?token=" + token
				if err := core.SendEmail(req.Email, "Welcome to The Study Hub — set your password", mailer.RenderTeacherWelcomeEmail(models.NullStr(userName), setURL)); err != nil {
					l.Error("resend-verification mail send failed", "err", err)
				} else {
					l.Info("teacher set-password link resent")
				}
				core.LogAudit(db, store.TenantOfUser(db, int64(userID)), req.Email, "verification_resent", "user", fmt.Sprintf("%d", userID), "teacher")
				core.Respond(w, generic)
				return
			}

			var regID sql.NullString
			db.QueryRow(`SELECT id FROM registrations WHERE email=? AND status='pending' AND COALESCE(type,'student')!='teacher' ORDER BY submitted_on DESC LIMIT 1`, req.Email).Scan(&regID)

			store.InvalidateOldTokens(db, req.Email, store.TokenPurposeVerifyParent)

			var regPtr *string
			if regID.Valid {
				regPtr = &regID.String
			}
			token, terr := store.CreateEmailToken(db, req.Email, store.TokenPurposeVerifyParent, &userID, regPtr, store.VerifyTokenTTL)
			if terr != nil {
				l.Error("resend-verification token create failed", "err", terr)
				core.Respond(w, generic)
				return
			}
			verifyURL := mailer.AppURL() + "/verify.html?token=" + token
			if err := core.SendEmail(req.Email, "Verify your Study Hub account", mailer.RenderVerifyParentEmail(models.NullStr(userName), verifyURL)); err != nil {
				l.Error("resend-verification mail send failed", "err", err)
			} else {
				l.Info("verification resent")
			}
			core.LogAudit(db, store.TenantOfUser(db, int64(userID)), req.Email, "verification_resent", "user", fmt.Sprintf("%d", userID), "parent")
			core.Respond(w, generic)
			return
		}

		// ── Branch 2: unverified teacher application ───────────────────────
		var regID, regName string
		var verifiedAt sql.NullTime
		err = db.QueryRow(`SELECT id, parent_name, email_verified_at FROM registrations
		                   WHERE email=? AND status='pending' AND type='teacher'
		                   ORDER BY submitted_on DESC LIMIT 1`, req.Email).Scan(&regID, &regName, &verifiedAt)
		if err == nil && !verifiedAt.Valid {
			store.InvalidateOldTokens(db, req.Email, store.TokenPurposeVerifyTeacher)
			token, terr := store.CreateEmailToken(db, req.Email, store.TokenPurposeVerifyTeacher, nil, &regID, store.VerifyTokenTTL)
			l := core.LogFromReq(r).With("email", req.Email, "registration_id", regID, "flow", "teacher")
			if terr != nil {
				l.Error("resend-verification token create failed", "err", terr)
				core.Respond(w, generic)
				return
			}
			verifyURL := mailer.AppURL() + "/verify.html?token=" + token
			if err := core.SendEmail(req.Email, "Confirm your Study Hub teacher application", mailer.RenderVerifyTeacherEmail(regName, verifyURL)); err != nil {
				l.Error("resend-verification mail send failed", "err", err)
			} else {
				l.Info("teacher verification resent")
			}
			core.LogAudit(db, store.TenantOfRegistration(db, regID), req.Email, "verification_resent", "registration", regID, "teacher")
			core.Respond(w, generic)
			return
		}

		// ── Branch 3: nothing pending — generic response ───────────────────
		core.Respond(w, generic)
	}
}
