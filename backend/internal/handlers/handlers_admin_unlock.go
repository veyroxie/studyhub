package handlers

import (
	"net/http"
	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
)

// handleAdminUnlockUser clears the lockout state for any user in the
// caller's tenant. Operational must-have: a teacher or parent who fat-
// fingered their password five times in a row is otherwise stuck for
// 15 minutes (the natural lockout window), and there's no other lever
// short of editing the DB.
//
// POST /api/users/{id}/unlock — admin-only.
func HandleAdminUnlockUser(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		id := chi.URLParam(r, "id")
		// Scope to caller's tenant — superadmins (tenantID=0) can unlock
		// any user, regular admins only their own tenant.
		var email string
		if c.TenantID == 0 {
			db.QueryRow(`SELECT email FROM users WHERE id=?`, id).Scan(&email)
		} else {
			db.QueryRow(`SELECT email FROM users WHERE id=? AND tenant_id=?`, id, c.TenantID).Scan(&email)
		}
		if email == "" {
			core.RespondError(w, "user not found", http.StatusNotFound)
			return
		}
		if _, err := db.Exec(
			`UPDATE users SET failed_login_count=0, locked_until=NULL WHERE id=?`,
			id,
		); err != nil {
			core.RespondError(w, "could not unlock", 500)
			return
		}
		auth.InvalidateUserStatusCache(parseInt(id))
		core.LogAudit(db, store.TenantID(c), c.Email, "user_unlocked", "user", id, email)
		core.Respond(w, map[string]string{"status": "unlocked", "email": email})
	}
}

func parseInt(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}
