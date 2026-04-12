package main

import (
	"database/sql"
)

// ── Payroll ───────────────────────────────────────────────────────────────────

func listPayroll(db *DB, c *Claims) []Payroll {
	if c != nil && c.Role == "parent" {
		return []Payroll{}
	}
	tid := tenantID(c)
	rows, err := db.Query(`SELECT id,staff_id,month,base_salary,bonus,deductions,total,status,paid_on FROM payroll WHERE (tenant_id=? OR ?=0) ORDER BY month DESC`, tid, tid)
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
