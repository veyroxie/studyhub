package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// handleProfile lets any authenticated user view and update their own
// profile (name, phone). Admins can also use this.
//
// GET  /api/auth/profile → returns current user info
// PUT  /api/auth/profile → updates name/phone on user + family rows
func handleProfile(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil {
			respondError(w, "not authenticated", 401)
			return
		}
		switch r.Method {
		case http.MethodGet:
			tw, twArgs := scopeTenant(c, "")
			var phone string
			phoneArgs := append([]any{c.Email}, twArgs...)
			db.QueryRow(`SELECT COALESCE(phone,'') FROM families WHERE contact=? AND deleted_at IS NULL`+tw+` LIMIT 1`, phoneArgs...).Scan(&phone)
			var mfaEnabled, notifyReminders, notifyAnnouncements, notifyReceipts bool
			db.QueryRow(`SELECT COALESCE(mfa_enabled,false),
			                    COALESCE(notify_invoice_reminders,true),
			                    COALESCE(notify_announcements,true),
			                    COALESCE(notify_payment_receipts,true)
			             FROM users WHERE email=?`, c.Email).Scan(&mfaEnabled, &notifyReminders, &notifyAnnouncements, &notifyReceipts)
			respond(w, map[string]any{
				"name":                  c.Name,
				"email":                 c.Email,
				"role":                  c.Role,
				"phone":                 phone,
				"mfaEnabled":            mfaEnabled,
				"notifyInvoiceReminders": notifyReminders,
				"notifyAnnouncements":    notifyAnnouncements,
				"notifyPaymentReceipts":  notifyReceipts,
			})
		case http.MethodPut:
			var body struct {
				Name                   string `json:"name"`
				Phone                  string `json:"phone"`
				NotifyInvoiceReminders *bool  `json:"notifyInvoiceReminders"`
				NotifyAnnouncements    *bool  `json:"notifyAnnouncements"`
				NotifyPaymentReceipts  *bool  `json:"notifyPaymentReceipts"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			if body.Name == "" {
				respondError(w, "name is required", 400)
				return
			}
			tw, twArgs := scopeTenant(c, "")
			// users.email is globally unique so the user-row update is one row.
			// families and students use contact=? which can collide across
			// tenants — scope those to the caller's tenant or a profile edit
			// in tenant A will leak into tenant B for the same parent email.
			if _, err := db.Exec(`UPDATE users SET name=? WHERE email=?`, body.Name, c.Email); err != nil {
				respondError(w, "could not update profile", 500)
				return
			}
			famArgs := append([]any{body.Name, body.Phone, c.Email}, twArgs...)
			if _, err := db.Exec(`UPDATE families SET parent_name=?, phone=? WHERE contact=? AND deleted_at IS NULL`+tw, famArgs...); err != nil {
				respondError(w, "could not update profile", 500)
				return
			}
			stuArgs := append([]any{body.Name, body.Phone, c.Email}, twArgs...)
			if _, err := db.Exec(`UPDATE students SET parent_name=?, phone=? WHERE contact=? AND deleted_at IS NULL`+tw, stuArgs...); err != nil {
				respondError(w, "could not update profile", 500)
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

			logAudit(db, c.Email, "profile_updated", "user", c.Email, body.Name)

			// Reissue the JWT so the new name is reflected in the cookie
			// immediately (no need to re-login). Preserve the original
			// session lifetime by reading the existing claim's expiry —
			// otherwise a "Remember me" session would silently shrink to
			// the short default on every profile update.
			remaining := time.Until(c.ExpiresAt.Time)
			if remaining <= 0 {
				remaining = tokenExpiryShort
			}
			newToken, terr := makeToken(c.UserID, c.TenantID, c.Email, c.Role, body.Name, remaining)
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
				if remaining > tokenExpiryShort {
					cookie.Expires = time.Now().Add(remaining)
				}
				http.SetCookie(w, cookie)
			}

			respond(w, map[string]string{"message": "Profile updated."})
		}
	}
}

// handleChangePassword lets an authenticated user change their own password.
// Requires the current password for verification (prevents session-hijack
// password changes).
//
// POST /api/auth/change-password  body: {currentPassword, newPassword}
func handleChangePassword(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil {
			respondError(w, "not authenticated", 401)
			return
		}
		var body struct {
			CurrentPassword string `json:"currentPassword"`
			NewPassword     string `json:"newPassword"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		if ok, msg := validatePassword(body.NewPassword); !ok {
			respondError(w, msg, 400)
			return
		}

		// Verify current password
		var hash string
		if err := db.QueryRow(`SELECT password_hash FROM users WHERE email=?`, c.Email).Scan(&hash); err != nil {
			respondError(w, "user not found", 404)
			return
		}
		if err := verifyPassword(hash, body.CurrentPassword); err != nil {
			respondError(w, "current password is incorrect", 403)
			return
		}

		newHash, err := hashPassword(body.NewPassword)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		if _, err := db.Exec(`UPDATE users SET password_hash=? WHERE email=?`, newHash, c.Email); err != nil {
			respondError(w, "could not update password", 500)
			return
		}

		logAudit(db, c.Email, "password_changed", "user", c.Email, "")

		respond(w, map[string]string{"message": "Password changed."})
	}
}
