package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// ── User management (admin) ───────────────────────────────────────────────────

type userCreateReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
	Name     string `json:"name"`
}

func handleUsers(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			c := claimsFrom(r)
			tw, twArgs := scopeTenant(c, "")
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
			respond(w, out)
		case http.MethodPost:
			var req userCreateReq
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			req.Email = strings.ToLower(strings.TrimSpace(req.Email))
			if !validateEmail(req.Email) {
				respondError(w, "invalid email format", http.StatusBadRequest)
				return
			}
			if ok, msg := validatePassword(req.Password); !ok {
				respondError(w, msg, http.StatusBadRequest)
				return
			}
			if req.Role == "" {
				req.Role = "parent"
			}
			hash, err := hashPassword(req.Password)
			if err != nil {
				respondError(w, "server error", 500)
				return
			}
			c := claimsFrom(r)
			tid := tenantID(c)
			_, err = db.Exec(`INSERT INTO users(tenant_id,email,password_hash,role,name) VALUES(?,?,?,?,?)`, tid, req.Email, hash, req.Role, req.Name)
			if err != nil {
				if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "duplicate key") {
					respondError(w, "email already exists", 409)
					return
				}
				respondError(w, "server error", 500)
				return
			}
			// Create a family record for parent accounts so enrollment
			// requests and student linkage work from day one.
			if req.Role == "parent" {
				famID := generateID("FAM")
				familyName := req.Name + " Family"
				if req.Name == "" {
					familyName = req.Email
				}
				db.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name,referral_code) VALUES(?,?,?,?,?,?,?)`,
					famID, tid, familyName, req.Email, "", req.Name, newReferralCode())
			}
			w.WriteHeader(http.StatusCreated)
			respond(w, map[string]string{"email": req.Email, "role": req.Role})
		}
	}
}

// handleUserVerify lets an admin manually activate a pending_verification account.
// Useful when the verification email didn't arrive (spam, misconfigured mailer, etc.)
// or in dev/testing scenarios.
//
// POST /api/users/{id}/verify
func handleUserVerify(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		c := claimsFrom(r)

		var email, status string
		if err := db.QueryRow(`SELECT email, COALESCE(status,'active') FROM users WHERE id=?`, id).Scan(&email, &status); err != nil {
			respondError(w, "user not found", 404)
			return
		}
		if status != "pending_verification" {
			respondError(w, "user is already verified", 400)
			return
		}

		if _, err := db.Exec(`UPDATE users SET status='active', email_verified_at=NOW() WHERE id=?`, id); err != nil {
			respondError(w, "could not verify user", 500)
			return
		}

		logAudit(db, c.Email, "user_manually_verified", "user", id, "by admin: "+c.Email)

		respond(w, map[string]string{"message": "Account activated", "email": email})
	}
}

// handleUserResendVerification lets an admin re-send the verification email
// for a pending_verification account. Invalidates any previous tokens first.
//
// POST /api/users/{id}/resend-verification
func handleUserResendVerification(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		c := claimsFrom(r)

		var userID int64
		var email, name, status string
		if err := db.QueryRow(`SELECT id, email, name, COALESCE(status,'active') FROM users WHERE id=?`, id).Scan(&userID, &email, &name, &status); err != nil {
			respondError(w, "user not found", 404)
			return
		}
		if status != "pending_verification" {
			respondError(w, "user is already verified — no email needed", 400)
			return
		}

		invalidateOldTokens(db, email, tokenPurposeVerifyParent)

		token, err := createEmailToken(db, email, tokenPurposeVerifyParent, &userID, nil, verifyTokenTTL)
		if err != nil {
			respondError(w, "could not create verification token", 500)
			return
		}

		verifyURL := appURL() + "/verify.html?token=" + token
		if err := mailer.Send(email, "Verify your Study Hub account", renderVerifyParentEmail(name, verifyURL)); err != nil {
			logFromReq(r).Error("admin resend verification failed", "err", err, "email", email)
			respondError(w, "token created but email send failed — check server logs", 500)
			return
		}

		logAudit(db, c.Email, "verification_resent_by_admin", "user", id, email)

		respond(w, map[string]string{"message": "Verification email sent to " + email})
	}
}

func handleUserDelete(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		tw, twArgs := scopeTenant(claimsFrom(r), "")
		args := append([]any{id}, twArgs...)
		if _, err := db.Exec(`DELETE FROM users WHERE id=?`+tw, args...); err != nil {
			respondError(w, "could not delete user", 500)
			return
		}
		if c := claimsFrom(r); c != nil {
			logAudit(db, c.Email, "user_deleted", "user", id, "hard deleted")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
