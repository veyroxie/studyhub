package main

import (
	"database/sql"
	"encoding/json"
	"time"
)

// Snapshot-specific bounded list queries. The "_Recent" variants apply
// time/status filters that the dashboard actually renders, leaving the
// per-endpoint paginated GETs to serve deep history.
//
// Windows are picked from the surface that consumes them:
//   - attendance: 90 days  — dashboard "recent absences", weekly bars
//   - feedback:   90 days  — parent timeline
//   - invoices:   unpaid OR last 24 months — billing table + history
//   - self study: 90 days
//   - announcements: 6 months OR not-archived
//
// Each function mirrors its unbounded sibling exactly except for the
// WHERE clause. Tenant + parent scope is preserved.

const (
	snapshotRecencyDays    = 90
	snapshotInvoicesMonths = 24
	snapshotAnnounceMonths = 6
)

func recentDate(days int) string {
	return time.Now().AddDate(0, 0, -days).Format("2006-01-02")
}

func recentDateMonths(months int) string {
	return time.Now().AddDate(0, -months, 0).Format("2006-01-02")
}

// ── Attendance ──────────────────────────────────────────────────────────────

func listAttendanceRecent(db *DB, c *Claims) []Attendance {
	cutoff := recentDate(snapshotRecencyDays)
	var rows *sql.Rows
	var err error
	tid := tenantID(c)
	if c != nil && c.Role == "parent" {
		rows, err = db.Query(`SELECT a.id,a.person_id,a.person_type,a.date,a.class_id,a.check_in,a.check_out,a.status FROM attendance a JOIN students s ON s.id=a.person_id WHERE a.person_type='student' AND s.contact=? AND s.tenant_id=? AND a.tenant_id=? AND s.deleted_at IS NULL AND a.date >= ? ORDER BY a.date DESC`, c.Email, tid, tid, cutoff)
	} else {
		tw, twArgs := scopeTenant(c, "")
		args := append(append([]any{}, twArgs...), cutoff)
		rows, err = db.Query(`SELECT id,person_id,person_type,date,class_id,check_in,check_out,status FROM attendance WHERE 1=1`+tw+` AND date >= ? ORDER BY date DESC`, args...)
	}
	if err != nil {
		return []Attendance{}
	}
	defer rows.Close()
	out := []Attendance{}
	for rows.Next() {
		var a Attendance
		var classID, checkIn, checkOut sql.NullString
		if err := rows.Scan(&a.ID, &a.PersonID, &a.PersonType, &a.Date, &classID, &checkIn, &checkOut, &a.Status); err != nil {
			continue
		}
		if classID.Valid {
			a.ClassID = &classID.String
		}
		if checkIn.Valid {
			a.CheckIn = &checkIn.String
		}
		if checkOut.Valid {
			a.CheckOut = &checkOut.String
		}
		out = append(out, a)
	}
	return out
}

// ── Feedback ────────────────────────────────────────────────────────────────

func listFeedbackRecent(db *DB, c *Claims) []Feedback {
	cutoff := recentDate(snapshotRecencyDays)
	tw, twArgs := scopeTenant(c, "")
	args := append(append([]any{}, twArgs...), cutoff)
	rows, err := db.Query(`SELECT id,class_id,date,teacher_id,topic,mood,notes,student_notes FROM feedback WHERE deleted_at IS NULL`+tw+` AND date >= ? ORDER BY date DESC`, args...)
	if err != nil {
		return []Feedback{}
	}
	defer rows.Close()
	out := []Feedback{}
	for rows.Next() {
		var f Feedback
		var sn string
		if err := rows.Scan(&f.ID, &f.ClassID, &f.Date, &f.TeacherID, &f.Topic, &f.Mood, &f.Notes, &sn); err != nil {
			continue
		}
		if sn != "" {
			json.Unmarshal([]byte(sn), &f.StudentNotes)
		}
		if f.StudentNotes == nil {
			f.StudentNotes = []StudentNote{}
		}
		out = append(out, f)
	}
	return out
}

// ── Invoices ────────────────────────────────────────────────────────────────

func listInvoicesRecent(db *DB, c *Claims) []Invoice {
	cutoff := recentDateMonths(snapshotInvoicesMonths)
	tid := tenantID(c)
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		rows, err = db.Query(`SELECT i.id,i.student_id,i.description,i.type,i.amount,i.due_date,i.status,i.created_on,i.paid_on,COALESCE(i.payment_proof,''),COALESCE(i.payment_method,''),COALESCE(i.discount_pct,0),COALESCE(i.submitted_by_parent,false),COALESCE(i.sibling_ids,''),COALESCE(i.sibling_discount,0),COALESCE(i.referral_credit,0),COALESCE(i.reference_no,''),COALESCE(i.early_bird_cutoff,''),COALESCE(i.early_bird_discount,0) FROM invoices i JOIN students s ON s.id=i.student_id WHERE s.contact=? AND s.tenant_id=? AND i.tenant_id=? AND i.deleted_at IS NULL AND (i.status<>'Paid' OR i.created_on >= ?) ORDER BY i.created_on DESC`, c.Email, tid, tid, cutoff)
	} else {
		tw, twArgs := scopeTenant(c, "")
		args := append(append([]any{}, twArgs...), cutoff)
		rows, err = db.Query(`SELECT id,student_id,description,type,amount,due_date,status,created_on,paid_on,COALESCE(payment_proof,''),COALESCE(payment_method,''),COALESCE(discount_pct,0),COALESCE(submitted_by_parent,false),COALESCE(sibling_ids,''),COALESCE(sibling_discount,0),COALESCE(referral_credit,0),COALESCE(reference_no,''),COALESCE(early_bird_cutoff,''),COALESCE(early_bird_discount,0) FROM invoices WHERE deleted_at IS NULL`+tw+` AND (status<>'Paid' OR created_on >= ?) ORDER BY created_on DESC`, args...)
	}
	if err != nil {
		return []Invoice{}
	}
	defer rows.Close()
	out := []Invoice{}
	for rows.Next() {
		var inv Invoice
		var paidOn sql.NullString
		if err := rows.Scan(&inv.ID, &inv.StudentID, &inv.Description, &inv.Type, &inv.Amount, &inv.DueDate, &inv.Status, &inv.CreatedOn, &paidOn, &inv.PaymentProof, &inv.PaymentMethod, &inv.DiscountPct, &inv.SubmittedByParent, &inv.SiblingIds, &inv.SiblingDiscount, &inv.ReferralCredit, &inv.ReferenceNo, &inv.EarlyBirdCutoff, &inv.EarlyBirdDiscount); err != nil {
			continue
		}
		if paidOn.Valid {
			inv.PaidOn = &paidOn.String
		}
		out = append(out, inv)
	}
	return out
}

// ── Self-study ──────────────────────────────────────────────────────────────

func listSelfStudyRecent(db *DB, c *Claims) []SelfStudySession {
	cutoff := recentDate(snapshotRecencyDays)
	tw, twArgs := scopeTenant(c, "")
	args := append(append([]any{}, twArgs...), cutoff)
	rows, err := db.Query(`SELECT id,student_id,date,start_time,end_time,duration_min,notes FROM self_study_sessions WHERE deleted_at IS NULL`+tw+` AND date >= ? ORDER BY date DESC`, args...)
	if err != nil {
		return []SelfStudySession{}
	}
	defer rows.Close()
	out := []SelfStudySession{}
	for rows.Next() {
		var s SelfStudySession
		if err := rows.Scan(&s.ID, &s.StudentID, &s.Date, &s.StartTime, &s.EndTime, &s.DurationMin, &s.Notes); err != nil {
			continue
		}
		out = append(out, s)
	}
	return out
}

// ── Announcements ───────────────────────────────────────────────────────────

func listAnnouncementsRecent(db *DB, c *Claims) []Announcement {
	cutoff := recentDateMonths(snapshotAnnounceMonths)
	tw, twArgs := scopeTenant(c, "")
	vw, vwArgs := announceVisibilityClause(c)
	args := append(append(append([]any{}, twArgs...), vwArgs...), cutoff)
	rows, err := db.Query(`SELECT id,title,message,audience,type,created_on,created_by,status,archive_on FROM announcements WHERE 1=1`+tw+vw+` AND (created_on >= ? OR COALESCE(status,'')<>'archived') ORDER BY created_on DESC`, args...)
	if err != nil {
		return []Announcement{}
	}
	defer rows.Close()
	out := []Announcement{}
	for rows.Next() {
		var a Announcement
		var status, archiveOn sql.NullString
		if err := rows.Scan(&a.ID, &a.Title, &a.Message, &a.Audience, &a.Type, &a.CreatedOn, &a.CreatedBy, &status, &archiveOn); err != nil {
			continue
		}
		a.Status = nullStr(status)
		if a.Status == "" {
			a.Status = "published"
		}
		a.ArchiveOn = nullStr(archiveOn)
		out = append(out, a)
	}
	if c != nil && c.Role == "parent" {
		out = parentAnnouncementFilter(out, parentClassIDs(db, c))
	}
	return out
}
