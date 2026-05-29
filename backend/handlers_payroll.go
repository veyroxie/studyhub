package main

import (
	"database/sql"
)

// ── Payroll ───────────────────────────────────────────────────────────────────

func listPayroll(db *DB, c *Claims) []Payroll {
	// Payroll is admin/superadmin-only. Teachers previously saw every
	// staff member's salary because only parents were filtered out.
	if !isAdminRole(c) {
		return []Payroll{}
	}
	tw, twArgs := scopeTenant(c, "")
	rows, err := db.Query(`SELECT id,staff_id,month,base_salary,bonus,deductions,total,status,paid_on FROM payroll WHERE 1=1`+tw+` ORDER BY month DESC`, twArgs...)
	if err != nil {
		return []Payroll{}
	}
	defer rows.Close()
	out := []Payroll{}
	for rows.Next() {
		var p Payroll
		var paidOn sql.NullString
		if err := rows.Scan(&p.ID, &p.StaffID, &p.Month, &p.BaseSalary, &p.Bonus, &p.Deductions, &p.Total, &p.Status, &paidOn); err != nil {
			continue
		}
		if paidOn.Valid {
			p.PaidOn = &paidOn.String
		}
		out = append(out, p)
	}
	return out
}
