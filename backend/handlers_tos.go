package main

import (
	"net/http"
)

// currentToSVersion is the version of the Terms of Service the app
// currently requires. Bump this any time the ToS text materially changes;
// every user with a lower tos_accepted_version gets re-prompted on next
// login.
const currentToSVersion = 1

// handleToSStatus returns whether the caller needs to (re-)accept the ToS.
// The frontend reads this from /api/auth/me which already calls into the
// users row; this endpoint exists for explicit polling and the accept page.
//
// GET /api/account/tos-status
func handleToSStatus(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil {
			respondError(w, "auth required", http.StatusUnauthorized)
			return
		}
		var v int
		db.QueryRow(`SELECT COALESCE(tos_accepted_version,0) FROM users WHERE email=?`, c.Email).Scan(&v)
		respond(w, map[string]any{
			"acceptedVersion": v,
			"currentVersion":  currentToSVersion,
			"mustAccept":      v < currentToSVersion,
		})
	}
}

// handleToSAccept records the caller's acceptance of the current ToS
// version. Idempotent — re-accepting just refreshes the timestamp.
//
// POST /api/account/accept-tos
func handleToSAccept(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil {
			respondError(w, "auth required", http.StatusUnauthorized)
			return
		}
		if _, err := db.Exec(
			`UPDATE users SET tos_accepted_version=?, tos_accepted_at=NOW() WHERE email=?`,
			currentToSVersion, c.Email,
		); err != nil {
			respondError(w, "could not record acceptance", 500)
			return
		}
		logAudit(db, c.Email, "tos_accepted", "user", c.Email, "")
		respond(w, map[string]any{
			"acceptedVersion": currentToSVersion,
			"mustAccept":      false,
		})
	}
}
