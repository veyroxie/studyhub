package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
)

// ── Payroll ───────────────────────────────────────────────────────────────────

func listPayroll(db *store.DB, c *core.Claims) []models.Payroll {
	// Payroll is admin/superadmin-only. Teachers previously saw every
	// staff member's salary because only parents were filtered out.
	if !core.IsAdminRole(c) {
		return []models.Payroll{}
	}
	tw, twArgs := store.ScopeTenant(c, "")
	rows, err := db.Query(`SELECT id,staff_id,month,base_salary,bonus,deductions,total,status,paid_on,COALESCE(manually_edited,false) FROM payroll WHERE 1=1`+tw+` ORDER BY month DESC`, twArgs...)
	if err != nil {
		core.Logger.Error("list query failed", "err", err, "type", "Payroll")
		return []models.Payroll{}
	}
	defer rows.Close()
	out := []models.Payroll{}
	for rows.Next() {
		var p models.Payroll
		var paidOn sql.NullString
		if err := rows.Scan(&p.ID, &p.StaffID, &p.Month, &p.BaseSalary, &p.Bonus, &p.Deductions, &p.Total, &p.Status, &paidOn, &p.ManuallyEdited); err != nil {
			continue
		}
		if paidOn.Valid {
			p.PaidOn = &paidOn.String
		}
		out = append(out, p)
	}
	return out
}

// HandlePayrollUpdate lets admin hand-correct a payroll row: base salary,
// bonus, deductions and status. Total is recomputed server-side (never
// trusted from the client) and the row is flagged manually_edited so the
// cron's stale-row refresh never overwrites an admin correction.
func HandlePayrollUpdate(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		var p models.Payroll
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			core.RespondError(w, "bad body", http.StatusBadRequest)
			return
		}
		if p.BaseSalary < 0 || p.Bonus < 0 || p.Deductions < 0 {
			core.RespondError(w, "amounts cannot be negative", http.StatusBadRequest)
			return
		}
		if p.Status != "Pending" && p.Status != "Paid" {
			core.RespondError(w, "status must be Pending or Paid", http.StatusBadRequest)
			return
		}
		id := chi.URLParam(r, "id")
		p.Total = math.Round((p.BaseSalary+p.Bonus-p.Deductions)*100) / 100

		var paidOn any
		if p.Status == "Paid" {
			paidOn = core.Today()
		}
		tw, twArgs := store.ScopeTenant(c, "")
		args := append([]any{p.BaseSalary, p.Bonus, p.Deductions, p.Total, p.Status, paidOn, id}, twArgs...)
		res, err := db.Exec(`UPDATE payroll SET base_salary=?, bonus=?, deductions=?, total=?, status=?, paid_on=?, manually_edited=TRUE WHERE id=?`+tw, args...)
		if err != nil {
			core.RespondError(w, "could not update payroll", http.StatusInternalServerError)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			core.RespondError(w, "payroll row not found", http.StatusNotFound)
			return
		}
		p.ID = id
		p.ManuallyEdited = true
		core.LogAudit(db, store.TenantID(c), c.Email, "payroll_edited", "payroll", id, fmt.Sprintf("total RM%.2f (%s)", p.Total, p.Status))
		core.Respond(w, p)
	}
}
