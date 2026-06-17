package handlers

import (
	"net/http"
	"studyhub/internal/core"
	"studyhub/internal/store"
)

// handleToSStatus returns whether the caller needs to (re-)accept the ToS.
// The frontend reads this from /api/auth/me which already calls into the
// users row; this endpoint exists for explicit polling and the accept page.
//
// GET /api/account/tos-status
func HandleToSStatus(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if c == nil {
			core.RespondError(w, "auth required", http.StatusUnauthorized)
			return
		}
		var v int
		db.QueryRow(`SELECT COALESCE(tos_accepted_version,0) FROM users WHERE email=?`, c.Email).Scan(&v)
		core.Respond(w, map[string]any{
			"acceptedVersion": v,
			"currentVersion":  core.CurrentToSVersion,
			"mustAccept":      v < core.CurrentToSVersion,
		})

	}
}

// handleToSAccept records the caller's acceptance of the current ToS
// version. Idempotent — re-accepting just refreshes the timestamp.
//
// POST /api/account/accept-tos
func HandleToSAccept(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if c == nil {
			core.RespondError(w, "auth required", http.StatusUnauthorized)
			return
		}
		if _, err := db.Exec(
			`UPDATE users SET tos_accepted_version=?, tos_accepted_at=NOW() WHERE email=?`,
			core.CurrentToSVersion, c.Email,
		); err != nil {
			core.RespondError(w, "could not record acceptance", 500)
			return
		}
		core.LogAudit(db, c.Email, "tos_accepted", "user", c.Email, "")
		core.Respond(w, map[string]any{
			"acceptedVersion": core.CurrentToSVersion,
			"mustAccept":      false,
		})

	}
}
