package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

// sessionCreditUnits is THE size of a missed session's credit: the class as scheduled
// on that date (0047), at 1 credit = 15 minutes. Every grant path uses it, so the same
// missed lesson earns the same amount however it was recorded.
func sessionCreditUnits(db *store.DB, tid int, classID, date string) int {
	var day, start, end string
	if err := db.QueryRow(`SELECT COALESCE(day,''), COALESCE(time,''), COALESCE(end_time,'') FROM classes WHERE id=? AND tenant_id=?`, classID, tid).
		Scan(&day, &start, &end); err != nil && !errors.Is(err, sql.ErrNoRows) {
		core.Logger.Error("session credit class lookup failed", "err", err, "class_id", classID)
	}
	versions, err := store.ClassScheduleVersions(db, tid, classID)
	if err != nil {
		core.Logger.Error("session credit schedule lookup failed", "err", err, "class_id", classID)
	}
	on := store.ScheduleOn(versions, store.ScheduleVersion{Day: day, Time: start, EndTime: end}, date)
	return creditsForDuration(on.Time, on.EndTime)
}

// attendedSession reports whether the student was marked present for that class and date.
func attendedSession(q interface {
	QueryRow(string, ...any) *sql.Row
}, tid int, studentID, classID, date string) (bool, error) {
	var n int
	err := q.QueryRow(`SELECT COUNT(*) FROM attendance WHERE tenant_id=? AND person_type='student' AND person_id=? AND class_id=? AND date=? AND COALESCE(status,'Present')=?`,
		tid, studentID, classID, date, attendanceStatusPresent).Scan(&n)
	return n > 0, err
}

// grantStaffSessionCredit is a teacher's or admin's "Absent + credit" for one session.
// A parent's report for that session decides what is allowed: told late or already
// declined, only an admin may still credit it (an override); pending, the credit
// approves it. A credit that already exists is not granted twice.
func grantStaffSessionCredit(ctx context.Context, db *store.DB, c *core.Claims, g store.SessionCredit) (string, bool, error) {
	tx, err := db.BeginTx(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	var reportID, status string
	err = tx.QueryRow(`SELECT id, status FROM absence_reports WHERE tenant_id=? AND student_id=? AND class_id=? AND session_date=? AND deleted_at IS NULL FOR UPDATE`,
		g.TenantID, g.StudentID, g.ClassID, g.Date).Scan(&reportID, &status)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("absence report lookup: %w", err)
	}
	if (status == models.AbsenceLate || status == models.AbsenceDeclined) && !core.IsAdminRole(c) {
		return "", false, userError(fmt.Sprintf("the parent's report for this class was %s, so no make-up credit; ask an admin if it should be given anyway", absenceStatusWords[status]))
	}
	id, granted, err := store.GrantSessionCredit(tx, g)
	if err != nil {
		return "", false, err
	}
	if status == models.AbsencePending {
		if _, err := tx.Exec(`UPDATE absence_reports SET status=?, decided_by=?, decided_at=now(), credit_id=? WHERE id=?`,
			models.AbsenceApproved, c.Email, id, reportID); err != nil {
			return "", false, fmt.Errorf("approve report from attendance: %w", err)
		}
	}
	return id, granted, tx.Commit()
}

var absenceStatusWords = map[string]string{
	models.AbsenceLate:     "less than 3 hours before class",
	models.AbsenceDeclined: "declined",
}
