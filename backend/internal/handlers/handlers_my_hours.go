package handlers

import (
	"net/http"

	"studyhub/internal/core"
	"studyhub/internal/jobs"
	"studyhub/internal/store"
)

// HandleMyHours shows a teacher their own hours this month and their pay once payroll
// has run. Staff and payroll pages are admin-only, so this was invisible to them.
// Only ever the caller's own staff row: it is looked up by their email, never by an id.
//
// GET /api/me/hours?month=YYYY-MM
func HandleMyHours(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if c == nil || c.Role != "teacher" {
			core.RespondError(w, "teachers only", http.StatusForbidden)
			return
		}
		month := r.URL.Query().Get("month")
		if month == "" {
			month = core.Today()[:7]
		}
		if !monthPattern.MatchString(month) {
			core.RespondError(w, "month must be YYYY-MM", http.StatusBadRequest)
			return
		}
		tid := store.TenantID(c)
		var staffID string
		if err := db.QueryRow(`SELECT id FROM staff WHERE email=? AND tenant_id=? AND deleted_at IS NULL LIMIT 1`, c.Email, tid).Scan(&staffID); err != nil {
			core.RespondError(w, "no staff record for this account", http.StatusNotFound)
			return
		}
		out := map[string]any{"month": month, "hours": jobs.TeacherHoursInMonth(db, staffID, month)}
		var total float64
		var status string
		if db.QueryRow(`SELECT COALESCE(total,0), COALESCE(status,'Pending') FROM payroll WHERE staff_id=? AND month=? AND tenant_id=?`,
			staffID, month, tid).Scan(&total, &status) == nil {
			out["pay"] = map[string]any{"total": total, "status": status}
		}
		core.Respond(w, out)
	}
}
