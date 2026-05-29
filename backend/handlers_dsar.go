package main

import (
	"encoding/json"
	"net/http"
)

// handleDSARExport returns a JSON dump of every row the authenticated caller
// can claim ownership of. Required by Malaysian PDPA / equivalent privacy
// regulations: the data subject has the right to receive a portable copy of
// their personal data.
//
// Scope:
//   - Parent: the user row, family row, all their children, all their
//             children's invoices/attendance/feedback/progress reports/
//             replacement credits/self-study sessions, referrals issued
//             or received.
//   - Teacher: the user row + their staff row + payroll history. (Feedback
//             they authored is excluded — that belongs to the student.)
//   - Admin: same as their role (admin's "personal" data is just the user
//             row); admins wanting a full tenant dump should use the CSV
//             export endpoints instead.
//
// Response is a single JSON document with one top-level key per dataset.
// Designed for self-serve download; deletion is a separate endpoint
// (handleFamilyPDPADelete).
//
// GET /api/account/export-my-data
func handleDSARExport(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil {
			respondError(w, "auth required", http.StatusUnauthorized)
			return
		}
		out := map[string]any{
			"exportedAt": today(),
			"email":      c.Email,
			"role":       c.Role,
		}

		// User row (everyone has one)
		var userRow struct {
			ID              int    `json:"id"`
			Name            string `json:"name"`
			Status          string `json:"status"`
			EmailVerifiedAt string `json:"emailVerifiedAt"`
			CreatedAt       string `json:"createdAt"`
			MFAEnabled      bool   `json:"mfaEnabled"`
		}
		db.QueryRow(
			`SELECT id, COALESCE(name,''), COALESCE(status,''), COALESCE(email_verified_at::text,''), COALESCE(created_at::text,''), COALESCE(mfa_enabled,false) FROM users WHERE email=?`,
			c.Email,
		).Scan(&userRow.ID, &userRow.Name, &userRow.Status, &userRow.EmailVerifiedAt, &userRow.CreatedAt, &userRow.MFAEnabled)
		out["user"] = userRow

		switch c.Role {
		case "parent":
			out["family"] = dsarParentFamily(db, c)
			out["students"] = dsarParentStudents(db, c)
			out["invoices"] = listInvoices(db, c)
			out["attendance"] = listAttendance(db, c)
			out["feedback"] = listFeedback(db, c) // already parent-scoped + stripped
			out["progressReports"] = dsarParentProgressReports(db, c)
			out["replacementCredits"] = listReplacementCredits(db, c)
			out["referrals"] = listReferralRewards(db, c)
		case "teacher":
			out["staff"] = dsarTeacherSelf(db, c)
			out["payroll"] = dsarTeacherPayroll(db, c)
		case "admin", "superadmin":
			// Admin's personal data ≈ user row. For tenant-wide dumps see
			// the CSV exporters on the billing / students pages.
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="studyhub-export.json"`)
		json.NewEncoder(w).Encode(out)
	}
}

func dsarParentFamily(db *DB, c *Claims) any {
	tw, twArgs := scopeTenant(c, "")
	args := append([]any{c.Email}, twArgs...)
	rows, err := db.Query(
		`SELECT id, name, contact, COALESCE(phone,''), COALESCE(parent_name,''), COALESCE(address,''), COALESCE(notes,''), COALESCE(referral_code,'') FROM families WHERE contact=?`+tw+` AND deleted_at IS NULL`,
		args...,
	)
	if err != nil {
		return []any{}
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, contact, phone, parentName, address, notes, code string
		if err := rows.Scan(&id, &name, &contact, &phone, &parentName, &address, &notes, &code); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "name": name, "contact": contact, "phone": phone,
			"parentName": parentName, "address": address, "notes": notes,
			"referralCode": code,
		})
	}
	return out
}

func dsarParentStudents(db *DB, c *Claims) any {
	return listStudents(db, c)
}

func dsarParentProgressReports(db *DB, c *Claims) any {
	// Parents see only published reports for their children — same scope
	// as the snapshot. Wrapped here so DSAR doesn't accidentally surface
	// drafts.
	stuIDs := parentStudentIDs(db, c)
	if len(stuIDs) == 0 {
		return []any{}
	}
	tw, twArgs := scopeTenant(c, "")
	placeholders := ""
	args := []any{}
	for id := range stuIDs {
		if placeholders != "" {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	args = append(args, twArgs...)
	q := `SELECT id, student_id, term, COALESCE(subject,''), COALESCE(grade,''), COALESCE(strengths,''), COALESCE(areas_to_improve,''), COALESCE(teacher_comment,''), COALESCE(next_term_focus,'') FROM progress_reports WHERE student_id IN (` + placeholders + `) AND COALESCE(published,false)=true AND deleted_at IS NULL` + tw
	rows, err := db.Query(q, args...)
	if err != nil {
		return []any{}
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, stuID, term, subject, grade, str, aim, comment, focus string
		if err := rows.Scan(&id, &stuID, &term, &subject, &grade, &str, &aim, &comment, &focus); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "studentId": stuID, "term": term, "subject": subject, "grade": grade,
			"strengths": str, "areasToImprove": aim, "teacherComment": comment, "nextTermFocus": focus,
		})
	}
	return out
}

func dsarTeacherSelf(db *DB, c *Claims) any {
	tw, twArgs := scopeTenant(c, "")
	args := append([]any{c.Email}, twArgs...)
	var id, name, fullName, role, phone, joinDate, status string
	var spec, nric string
	if err := db.QueryRow(
		`SELECT id, name, full_name, COALESCE(role,''), COALESCE(phone,''), COALESCE(join_date,''), COALESCE(status,''), COALESCE(specialization,''), COALESCE(nric,'') FROM staff WHERE email=?`+tw+` AND deleted_at IS NULL`,
		args...,
	).Scan(&id, &name, &fullName, &role, &phone, &joinDate, &status, &spec, &nric); err != nil {
		return nil
	}
	return map[string]any{
		"id": id, "name": name, "fullName": fullName, "role": role,
		"phone": phone, "joinDate": joinDate, "status": status,
		"specialization": spec, "nric": nric,
	}
}

func dsarTeacherPayroll(db *DB, c *Claims) any {
	tw, twArgs := scopeTenant(c, "")
	args := append([]any{c.Email}, twArgs...)
	var staffID string
	db.QueryRow(`SELECT id FROM staff WHERE email=?`+tw+` AND deleted_at IS NULL`, args...).Scan(&staffID)
	if staffID == "" {
		return []any{}
	}
	prArgs := append([]any{staffID}, twArgs...)
	rows, err := db.Query(
		`SELECT id, month, base_salary, bonus, deductions, total, status, COALESCE(paid_on,'') FROM payroll WHERE staff_id=?`+tw+` ORDER BY month DESC`,
		prArgs...,
	)
	if err != nil {
		return []any{}
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, month, statusV, paidOn string
		var base, bonus, ded, total float64
		if err := rows.Scan(&id, &month, &base, &bonus, &ded, &total, &statusV, &paidOn); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "month": month, "baseSalary": base, "bonus": bonus,
			"deductions": ded, "total": total, "status": statusV, "paidOn": paidOn,
		})
	}
	return out
}
