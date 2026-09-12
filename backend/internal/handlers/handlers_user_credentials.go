package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/store"
)

// HandleUserCredentials lets an admin set a user's sign-in email and password,
// moving the matching staff row's email in the same transaction.
//
// This capability was missing entirely. /api/set-password and
// /api/reset-password both need an emailed token, and outbound mail is
// deliberately restricted (ADR-015), so there was no way to give a teacher
// working credentials short of deleting and recreating the account.
//
// The staff row moves WITH the login because a teacher's classes resolve
// through `staff WHERE email = <the signed-in email>`. Change one without the
// other and the teacher signs in successfully to an empty timetable: no error,
// nothing logged, just no classes. Doing both in one transaction is what makes
// that state unreachable rather than merely unlikely.
//
// PUT /api/users/{id}/credentials  {"email": "...", "password": "..."}
func HandleUserCredentials(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		id := chi.URLParam(r, "id")
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			core.RespondError(w, "bad request body", http.StatusBadRequest)
			return
		}
		body.Email = strings.ToLower(strings.TrimSpace(body.Email))
		if body.Email == "" && body.Password == "" {
			core.RespondError(w, "give an email, a password, or both", http.StatusBadRequest)
			return
		}
		if body.Email != "" && !auth.ValidateEmail(body.Email) {
			core.RespondError(w, "that is not a valid email address", http.StatusBadRequest)
			return
		}
		if body.Password != "" {
			if ok, why := auth.ValidatePassword(body.Password); !ok {
				core.RespondError(w, why, http.StatusBadRequest)
				return
			}
		}

		tw, twArgs := store.ScopeTenant(c, "")
		var currentEmail, role string
		selArgs := append([]any{id}, twArgs...)
		if err := db.QueryRow(`SELECT email, COALESCE(role,'') FROM users WHERE id=?`+tw, selArgs...).
			Scan(&currentEmail, &role); err != nil {
			core.RespondError(w, "user not found", http.StatusNotFound)
			return
		}
		// A superadmin is not reachable from here: an admin must not be able to
		// take over the account above their own, which is the same reason
		// HandleUsers refuses to create one.
		if role == "superadmin" {
			core.RespondError(w, "a superadmin's credentials cannot be changed from here", http.StatusForbidden)
			return
		}
		newEmail := body.Email
		if newEmail == "" {
			newEmail = currentEmail
		}

		tx, err := db.BeginTx(r.Context())
		if err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		defer tx.Rollback()

		if body.Email != "" && newEmail != currentEmail {
			emailArgs := append([]any{newEmail, id}, twArgs...)
			if _, err := tx.Exec(`UPDATE users SET email=? WHERE id=?`+tw, emailArgs...); err != nil {
				if isDuplicate(err) {
					core.RespondError(w, "another account already uses that email", http.StatusConflict)
					return
				}
				core.LogFromReq(r).Error("credentials: email update failed", "err", err, "user_id", id)
				core.RespondError(w, "could not update the account", 500)
				return
			}
			staffArgs := append([]any{newEmail, currentEmail}, twArgs...)
			if _, err := tx.Exec(`UPDATE staff SET email=? WHERE email=?`+tw+` AND deleted_at IS NULL`, staffArgs...); err != nil {
				core.LogFromReq(r).Error("credentials: staff email update failed", "err", err, "user_id", id)
				core.RespondError(w, "could not update the account", 500)
				return
			}
		}

		if body.Password != "" {
			hash, err := auth.HashPassword(body.Password)
			if err != nil {
				core.RespondError(w, "could not update the account", 500)
				return
			}
			pwArgs := append([]any{hash, id}, twArgs...)
			if _, err := tx.Exec(`UPDATE users SET password_hash=?, failed_login_count=0, locked_until=NULL WHERE id=?`+tw, pwArgs...); err != nil {
				core.LogFromReq(r).Error("credentials: password update failed", "err", err, "user_id", id)
				core.RespondError(w, "could not update the account", 500)
				return
			}
		}
		if err := tx.Commit(); err != nil {
			core.RespondError(w, "could not update the account", 500)
			return
		}

		// Every existing session dies. A password change that leaves old
		// sessions alive does not lock anyone out of anything.
		if body.Password != "" {
			var uid int
			db.QueryRow(`SELECT id FROM users WHERE id=?`+tw, selArgs...).Scan(&uid)
			store.RevokeRefreshFamilyByUser(db, uid, "credentials changed by admin")
		}
		// The detail records WHAT changed, never the password itself.
		core.LogAudit(db, store.TenantID(c), c.Email, "user_credentials_changed", "user", id,
			"email="+boolWord(body.Email != "" && newEmail != currentEmail)+" password="+boolWord(body.Password != ""))
		core.Respond(w, map[string]string{"id": id, "email": newEmail})
	}
}

func boolWord(b bool) string {
	if b {
		return "changed"
	}
	return "unchanged"
}
