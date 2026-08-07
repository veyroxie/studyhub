package handlers

import (
	"encoding/json"
	"net/http"
	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/store"
	"time"
)

// handleProfile lets any authenticated user view and update their own
// profile (name, phone). Admins can also use this.
//
// GET  /api/auth/profile → returns current user info
// PUT  /api/auth/profile → updates name/phone on user + family rows
func HandleProfile(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if c == nil {
			core.RespondError(w, "not authenticated", 401)
			return
		}
		switch r.Method {
		case http.MethodGet:
			tw, twArgs := store.ScopeTenant(c, "")
			var phone string
			phoneArgs := append([]any{c.Email}, twArgs...)
			db.QueryRow(`SELECT COALESCE(phone,'') FROM families WHERE contact=? AND deleted_at IS NULL`+tw+` LIMIT 1`, phoneArgs...).Scan(&phone)
			var mfaEnabled, notifyReminders, notifyAnnouncements, notifyReceipts, notifyCheckin bool
			db.QueryRow(`SELECT COALESCE(mfa_enabled,false),
			                    COALESCE(notify_invoice_reminders,true),
			                    COALESCE(notify_announcements,true),
			                    COALESCE(notify_payment_receipts,true),
			                    COALESCE(notify_checkin_email,false)
			             FROM users WHERE email=?`, c.Email).Scan(&mfaEnabled, &notifyReminders, &notifyAnnouncements, &notifyReceipts, &notifyCheckin)
			core.Respond(w, map[string]any{
				"name":                   c.Name,
				"email":                  c.Email,
				"role":                   c.Role,
				"phone":                  phone,
				"mfaEnabled":             mfaEnabled,
				"notifyInvoiceReminders": notifyReminders,
				"notifyAnnouncements":    notifyAnnouncements,
				"notifyPaymentReceipts":  notifyReceipts,
				"notifyCheckinEmail":     notifyCheckin,
			})

		case http.MethodPut:
			var body struct {
				Name                   string `json:"name"`
				Phone                  string `json:"phone"`
				NotifyInvoiceReminders *bool  `json:"notifyInvoiceReminders"`
				NotifyAnnouncements    *bool  `json:"notifyAnnouncements"`
				NotifyPaymentReceipts  *bool  `json:"notifyPaymentReceipts"`
				NotifyCheckinEmail     *bool  `json:"notifyCheckinEmail"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				core.RespondError(w, "bad body", 400)
				return
			}
			if body.Name == "" {
				core.RespondError(w, "name is required", 400)
				return
			}
			tw, twArgs := store.ScopeTenant(c, "")
			// users.email is globally unique so the user-row update is one row.
			// families and students use contact=? which can collide across
			// tenants — scope those to the caller's tenant or a profile edit
			// in tenant A will leak into tenant B for the same parent email.
			if _, err := db.Exec(`UPDATE users SET name=? WHERE email=?`, body.Name, c.Email); err != nil {
				core.RespondError(w, "could not update profile", 500)
				return
			}
			famArgs := append([]any{body.Name, body.Phone, c.Email}, twArgs...)
			if _, err := db.Exec(`UPDATE families SET parent_name=?, phone=? WHERE contact=? AND deleted_at IS NULL`+tw, famArgs...); err != nil {
				core.RespondError(w, "could not update profile", 500)
				return
			}
			stuArgs := append([]any{body.Name, body.Phone, c.Email}, twArgs...)
			if _, err := db.Exec(`UPDATE students SET parent_name=?, phone=? WHERE contact=? AND deleted_at IS NULL`+tw, stuArgs...); err != nil {
				core.RespondError(w, "could not update profile", 500)
				return
			}

			// Notification toggles — optional fields, only update when the
			// client actually sent a value. Pointer-bool lets us distinguish
			// "didn't include the field" from "sent false".
			if body.NotifyInvoiceReminders != nil {
				db.Exec(`UPDATE users SET notify_invoice_reminders=? WHERE email=?`, *body.NotifyInvoiceReminders, c.Email)
			}
			if body.NotifyAnnouncements != nil {
				db.Exec(`UPDATE users SET notify_announcements=? WHERE email=?`, *body.NotifyAnnouncements, c.Email)
			}
			if body.NotifyPaymentReceipts != nil {
				db.Exec(`UPDATE users SET notify_payment_receipts=? WHERE email=?`, *body.NotifyPaymentReceipts, c.Email)
			}
			if body.NotifyCheckinEmail != nil {
				db.Exec(`UPDATE users SET notify_checkin_email=? WHERE email=?`, *body.NotifyCheckinEmail, c.Email)
			}

			core.LogAudit(db, store.TenantID(c), c.Email, "profile_updated", "user", c.Email, body.Name)

			// Reissue the JWT so the new name is reflected in the cookie
			// immediately (no need to re-login). Preserve the original
			// session lifetime by reading the existing claim's expiry —
			// otherwise a "Remember me" session would silently shrink to
			// the short default on every profile update.
			remaining := time.Until(c.ExpiresAt.Time)
			if remaining <= 0 {
				remaining = auth.TokenExpiryShort
			}
			newToken, terr := auth.MakeToken(c.UserID, c.TenantID, c.Email, c.Role, body.Name, remaining)
			if terr == nil {
				secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
				cookie := &http.Cookie{
					Name:     "sh_token",
					Value:    newToken,
					Path:     "/",
					HttpOnly: true,
					Secure:   secure,
					SameSite: http.SameSiteLaxMode,
				}
				// Mirror the original cookie's persistence: if the
				// remaining lifetime is longer than the short default,
				// the user had opted into "Remember me" — keep the cookie
				// persistent. Otherwise leave Expires zero (session cookie).
				if remaining > auth.TokenExpiryShort {
					cookie.Expires = time.Now().Add(remaining)
				}
				http.SetCookie(w, cookie)
			}

			core.Respond(w, map[string]string{"message": "Profile updated."})
		}
	}
}

// handleChangePassword lets an authenticated user change their own password.
// Requires the current password for verification (prevents session-hijack
// password changes).
//
// POST /api/auth/change-password  body: {currentPassword, newPassword}
func HandleChangePassword(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if c == nil {
			core.RespondError(w, "not authenticated", 401)
			return
		}
		var body struct {
			CurrentPassword string `json:"currentPassword"`
			NewPassword     string `json:"newPassword"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		if ok, msg := auth.ValidatePassword(body.NewPassword); !ok {
			core.RespondError(w, msg, 400)
			return
		}

		// Verify current password
		var hash string
		if err := db.QueryRow(`SELECT password_hash FROM users WHERE email=?`, c.Email).Scan(&hash); err != nil {
			core.RespondError(w, "user not found", 404)
			return
		}
		if err := auth.VerifyPassword(hash, body.CurrentPassword); err != nil {
			core.RespondError(w, "current password is incorrect", 403)
			return
		}

		newHash, err := auth.HashPassword(body.NewPassword)
		if err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		if _, err := db.Exec(`UPDATE users SET password_hash=? WHERE email=?`, newHash, c.Email); err != nil {
			core.RespondError(w, "could not update password", 500)
			return
		}

		// Evict any other live sessions: a password change should invalidate
		// refresh tokens (and thus the ability to mint new access JWTs) so a
		// compromise-driven change kicks out the intruder.
		store.RevokeRefreshFamilyByUser(db, c.UserID, "password_changed")
		// Also invalidate already-issued access JWTs (incl. this one) — the
		// user re-authenticates, and any stolen session dies with them.
		if _, err := db.Exec(`UPDATE users SET sessions_invalid_before=NOW() WHERE id=?`, c.UserID); err != nil {
			core.Logger.Error("bump sessions_invalid_before failed", "err", err, "user_id", c.UserID)
		}
		auth.InvalidateUserStatusCache(c.UserID)

		core.LogAudit(db, store.TenantID(c), c.Email, "password_changed", "user", c.Email, "")

		core.Respond(w, map[string]string{"message": "Password changed."})
	}
}
