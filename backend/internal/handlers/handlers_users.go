package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/mailer"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
)

// ── User management (admin) ───────────────────────────────────────────────────

type userCreateReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
	Name     string `json:"name"`
}

func HandleUsers(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			c := core.ClaimsFrom(r)
			tw, twArgs := store.ScopeTenant(c, "")
			rows, _ := db.Query(`SELECT id,email,role,name,COALESCE(status,'active') FROM users WHERE 1=1`+tw+` ORDER BY role,name`, twArgs...)
			defer rows.Close()
			type userRow struct {
				ID     int    `json:"id"`
				Email  string `json:"email"`
				Role   string `json:"role"`
				Name   string `json:"name"`
				Status string `json:"status"`
			}
			out := []userRow{}
			for rows.Next() {
				var u userRow
				if err := rows.Scan(&u.ID, &u.Email, &u.Role, &u.Name, &u.Status); err != nil {
					continue
				}
				out = append(out, u)
			}
			core.Respond(w, out)
		case http.MethodPost:
			var req userCreateReq
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				core.RespondError(w, "bad body", 400)
				return
			}
			req.Email = strings.ToLower(strings.TrimSpace(req.Email))
			if !auth.ValidateEmail(req.Email) {
				core.RespondError(w, "invalid email format", http.StatusBadRequest)
				return
			}
			if ok, msg := auth.ValidatePassword(req.Password); !ok {
				core.RespondError(w, msg, http.StatusBadRequest)
				return
			}
			if req.Role == "" {
				req.Role = "parent"
			}
			// Only these three roles are creatable here. Reject anything else —
			// notably "superadmin" — so an admin cannot self-provision a
			// higher-privilege account through this endpoint.
			if req.Role != "parent" && req.Role != "teacher" && req.Role != "admin" {
				core.RespondError(w, "invalid role", 400)
				return
			}
			hash, err := auth.HashPassword(req.Password)
			if err != nil {
				core.RespondError(w, "server error", 500)
				return
			}
			c := core.ClaimsFrom(r)
			tid := store.TenantID(c)
			_, err = db.Exec(`INSERT INTO users(tenant_id,email,password_hash,role,name) VALUES(?,?,?,?,?)`, tid, req.Email, hash, req.Role, req.Name)
			if err != nil {
				if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "duplicate key") {
					core.RespondError(w, "email already exists", 409)
					return
				}
				core.RespondError(w, "server error", 500)
				return
			}
			// Create a family record for parent accounts so enrollment
			// requests and student linkage work from day one.
			if req.Role == "parent" {
				famID := core.GenerateID("FAM")
				familyName := req.Name + " Family"
				if req.Name == "" {
					familyName = req.Email
				}
				db.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name,referral_code) VALUES(?,?,?,?,?,?,?)`,
					famID, tid, familyName, req.Email, "", req.Name, core.NewReferralCode())
			}
			w.WriteHeader(http.StatusCreated)
			core.Respond(w, map[string]string{"email": req.Email, "role": req.Role})
		}
	}
}

// handleUserVerify lets an admin manually activate a pending_verification account.
// Useful when the verification email didn't arrive (spam, misconfigured mailer, etc.)
// or in dev/testing scenarios.
//
// POST /api/users/{id}/verify
func HandleUserVerify(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		c := core.ClaimsFrom(r)
		tw, twArgs := store.ScopeTenant(c, "")

		var email, status string
		selArgs := append([]any{id}, twArgs...)
		if err := db.QueryRow(`SELECT email, COALESCE(status,'active') FROM users WHERE id=?`+tw, selArgs...).Scan(&email, &status); err != nil {
			core.RespondError(w, "user not found", 404)
			return
		}
		if status != "pending_verification" {
			core.RespondError(w, "user is already verified", 400)
			return
		}

		updArgs := append([]any{id}, twArgs...)
		if _, err := db.Exec(`UPDATE users SET status='active', email_verified_at=NOW() WHERE id=?`+tw, updArgs...); err != nil {
			core.RespondError(w, "could not verify user", 500)
			return
		}

		core.LogAudit(db, c.Email, "user_manually_verified", "user", id, "by admin: "+c.Email)

		core.Respond(w, map[string]string{"message": "Account activated", "email": email})
	}
}

// handleUserResendVerification lets an admin re-send the verification email
// for a pending_verification account. Invalidates any previous tokens first.
//
// POST /api/users/{id}/resend-verification
func HandleUserResendVerification(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		c := core.ClaimsFrom(r)
		tw, twArgs := store.ScopeTenant(c, "")

		var userID int64
		var email, name, status string
		selArgs := append([]any{id}, twArgs...)
		if err := db.QueryRow(`SELECT id, email, name, COALESCE(status,'active') FROM users WHERE id=?`+tw, selArgs...).Scan(&userID, &email, &name, &status); err != nil {
			core.RespondError(w, "user not found", 404)
			return
		}
		if status != "pending_verification" {
			core.RespondError(w, "user is already verified — no email needed", 400)
			return
		}

		store.InvalidateOldTokens(db, email, store.TokenPurposeVerifyParent)

		token, err := store.CreateEmailToken(db, email, store.TokenPurposeVerifyParent, &userID, nil, store.VerifyTokenTTL)
		if err != nil {
			core.RespondError(w, "could not create verification token", 500)
			return
		}

		verifyURL := mailer.AppURL() + "/verify.html?token=" + token
		if err := core.SendEmail(email, "Verify your Study Hub account", mailer.RenderVerifyParentEmail(name, verifyURL)); err != nil {
			core.LogFromReq(r).Error("admin resend verification failed", "err", err, "email", email)
			core.RespondError(w, "token created but email send failed — check server logs", 500)
			return
		}

		core.LogAudit(db, c.Email, "verification_resent_by_admin", "user", id, email)

		core.Respond(w, map[string]string{"message": "Verification email sent to " + email})
	}
}

func HandleUserDelete(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(core.ClaimsFrom(r), "")
		args := append([]any{id}, twArgs...)
		if _, err := db.Exec(`DELETE FROM users WHERE id=?`+tw, args...); err != nil {
			core.RespondError(w, "could not delete user", 500)
			return
		}
		if c := core.ClaimsFrom(r); c != nil {
			core.LogAudit(db, c.Email, "user_deleted", "user", id, "hard deleted")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
