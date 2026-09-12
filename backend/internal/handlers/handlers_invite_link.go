package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/core"
	"studyhub/internal/mailer"
	"studyhub/internal/store"
)

// HandleUserInviteLink mints a set-password link and RETURNS it to the admin
// instead of emailing it.
//
// The machinery to onboard a parent already existed and was already correct:
// the account is created with a random unusable password and a set-password
// token, and the link is emailed. Only the delivery is dead --
// OUTBOUND_ALLOWLIST is one address (ADR-015), so in production 51 parent
// accounts exist and not one has ever been able to sign in.
//
// Handing the link to the admin fixes that without weakening anything. Nadine
// pastes it into the WhatsApp thread she is already having with a number
// already on file, which for this centre is a stronger check on "is this really
// that child's parent" than an email round-trip. An account only ever arrives
// by invitation, after a child is enrolled, so the admin IS the verification.
//
// No new privilege: an admin can already set this user's password outright via
// PUT /api/users/{id}/credentials. This is the gentler version of a power they
// have, and unlike a password it expires.
//
// POST /api/users/{id}/invite-link
func HandleUserInviteLink(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")

		var email, role string
		var userID int64
		selArgs := append([]any{id}, twArgs...)
		if err := db.QueryRow(`SELECT id, email, COALESCE(role,'') FROM users WHERE id=?`+tw, selArgs...).
			Scan(&userID, &email, &role); err != nil {
			core.RespondError(w, "user not found", http.StatusNotFound)
			return
		}
		if role == "superadmin" {
			core.RespondError(w, "a superadmin cannot be invited from here", http.StatusForbidden)
			return
		}

		// Only one live invitation at a time. Two valid links for one account
		// means the older one still works after the newer has been used, and
		// nobody can say which was handed to whom.
		store.InvalidateOldTokens(db, email, store.TokenPurposeSetPassword)

		token, err := store.CreateEmailToken(db, email, store.TokenPurposeSetPassword, &userID, nil, store.SetPasswordTokenTTL)
		if err != nil {
			core.LogFromReq(r).Error("invite link: token create failed", "err", err, "user_id", id)
			core.RespondError(w, "could not create the invite link", 500)
			return
		}

		// The token itself is never logged. The audit trail records that an
		// invitation was issued and to whom, which is what an auditor needs;
		// the link is a credential and belongs only in the response.
		core.LogAudit(db, store.TenantID(c), c.Email, "invite_link_issued", "user", id, "email="+email)

		core.Respond(w, map[string]any{
			"email":     email,
			"link":      mailer.AppURL() + "/set-password.html?token=" + token,
			"expiresAt": time.Now().Add(store.SetPasswordTokenTTL).Format(time.RFC3339),
			"expiresIn": store.SetPasswordTokenTTL.String(),
		})
	}
}
