package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/store"
)

// HandleCompleteSetup lets someone holding an admin-issued temporary password
// choose their own email and password. It is the ONLY thing such a session can
// do (auth.RequireSetupComplete), so the temporary credential buys exactly one
// action: replacing itself.
//
// The staff row's email moves in the same transaction, for the same reason the
// admin endpoint does it: a teacher's classes resolve through
// `staff WHERE email = <signed-in email>`, so changing the login alone lands
// her in an empty timetable with nothing logged.
//
// POST /api/auth/complete-setup  {"email": "...", "password": "..."}
func HandleCompleteSetup(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if c == nil || c.UserID <= 0 {
			core.RespondError(w, "sign in first", http.StatusUnauthorized)
			return
		}
		if !auth.MustCompleteSetup(db, c) {
			core.RespondError(w, "this account is already set up — change your password from your profile", http.StatusConflict)
			return
		}
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			core.RespondError(w, "bad request body", http.StatusBadRequest)
			return
		}
		body.Email = strings.ToLower(strings.TrimSpace(body.Email))
		if body.Password == "" {
			core.RespondError(w, "choose a password", http.StatusBadRequest)
			return
		}
		if ok, why := auth.ValidatePassword(body.Password); !ok {
			core.RespondError(w, why, http.StatusBadRequest)
			return
		}
		if body.Email != "" && !auth.ValidateEmail(body.Email) {
			core.RespondError(w, "that is not a valid email address", http.StatusBadRequest)
			return
		}

		currentEmail := c.Email
		newEmail := body.Email
		if newEmail == "" {
			newEmail = currentEmail
		}
		hash, err := auth.HashPassword(body.Password)
		if err != nil {
			core.RespondError(w, "could not save your details", 500)
			return
		}

		tx, err := db.BeginTx(r.Context())
		if err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		defer tx.Rollback()

		if newEmail != currentEmail {
			if _, err := tx.Exec(`UPDATE users SET email=? WHERE id=?`, newEmail, c.UserID); err != nil {
				if isDuplicate(err) {
					core.RespondError(w, "another account already uses that email", http.StatusConflict)
					return
				}
				core.LogFromReq(r).Error("complete-setup: email update failed", "err", err, "user_id", c.UserID)
				core.RespondError(w, "could not save your details", 500)
				return
			}
			if _, err := tx.Exec(`UPDATE staff SET email=? WHERE email=? AND deleted_at IS NULL`, newEmail, currentEmail); err != nil {
				core.LogFromReq(r).Error("complete-setup: staff email update failed", "err", err, "user_id", c.UserID)
				core.RespondError(w, "could not save your details", 500)
				return
			}
		}
		// sessions_invalid_before is stamped so the temporary-password session
		// dies with the password it was issued for. The user signs in again with
		// what they just chose, which also proves they can.
		if _, err := tx.Exec(`UPDATE users SET password_hash=?, must_change_credentials=FALSE,
			sessions_invalid_before=NOW(), failed_login_count=0, locked_until=NULL WHERE id=?`, hash, c.UserID); err != nil {
			core.LogFromReq(r).Error("complete-setup: password update failed", "err", err, "user_id", c.UserID)
			core.RespondError(w, "could not save your details", 500)
			return
		}
		if err := tx.Commit(); err != nil {
			core.RespondError(w, "could not save your details", 500)
			return
		}

		auth.InvalidateUserStatusCache(c.UserID)
		store.RevokeRefreshFamilyByUser(db, c.UserID, "credentials chosen at first sign-in")
		core.LogAudit(db, store.TenantID(c), newEmail, "credentials_self_set", "user", strconv.Itoa(c.UserID),
			"email="+boolWord(newEmail != currentEmail)+" password=changed")
		core.Respond(w, map[string]string{"email": newEmail, "status": "ok"})
	}
}
