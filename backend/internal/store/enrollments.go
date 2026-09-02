package store

import (
	"fmt"
	"time"

	"studyhub/internal/core"
)

// SyncEnrollments reconciles the enrollments join table with a student's
// new class list. Dual-write phase of B6: students.enrolled_classes stays
// the read path; this table records WHEN each enrolment started and ended,
// which session billing needs and the JSON cannot express.
//
// Removed classes are ENDED (ended_on = today), never deleted -- a student
// leaving mid-month must still be billable for the sessions already held.
// Errors are logged, not returned: the JSON write already succeeded and
// unwinding it over shadow-table trouble would hurt more than drift, which
// the 0043 backfill pattern can always repair.
func SyncEnrollments(db *DB, tenantID int, studentID string, newClassIDs []string, actor string) {
	today := time.Now().Format("2006-01-02")
	want := map[string]bool{}
	for _, cid := range newClassIDs {
		want[cid] = true
	}

	rows, err := db.Query(`SELECT class_id FROM enrollments WHERE tenant_id=? AND student_id=? AND ended_on IS NULL`, tenantID, studentID)
	if err != nil {
		core.Logger.Error("enrollment sync read failed", "err", err, "student_id", studentID)
		return
	}
	live := map[string]bool{}
	for rows.Next() {
		var cid string
		if rows.Scan(&cid) == nil {
			live[cid] = true
		}
	}
	rows.Close()

	for cid := range live {
		if !want[cid] {
			if _, err := db.Exec(`UPDATE enrollments SET ended_on=? WHERE tenant_id=? AND student_id=? AND class_id=? AND ended_on IS NULL`,
				today, tenantID, studentID, cid); err != nil {
				core.Logger.Error("enrollment end failed", "err", err, "student_id", studentID, "class_id", cid)
			}
		}
	}
	for cid := range want {
		if !live[cid] {
			if _, err := db.Exec(`INSERT INTO enrollments (id, tenant_id, student_id, class_id, started_on, created_by, created_on)
				VALUES (?,?,?,?,?,?,?)
				ON CONFLICT (tenant_id, student_id, class_id) WHERE ended_on IS NULL DO NOTHING`,
				core.GenerateID("ENR"), tenantID, studentID, cid, today, actor, today); err != nil {
				core.Logger.Error("enrollment insert failed", "err", err, "student_id", studentID, "class_id", cid)
			}
		}
	}
}

// EndAllEnrollments closes every live enrolment for a student -- the
// student-deleted path.
func EndAllEnrollments(db *DB, tenantID int, studentID string) {
	if _, err := db.Exec(`UPDATE enrollments SET ended_on=? WHERE tenant_id=? AND student_id=? AND ended_on IS NULL`,
		time.Now().Format("2006-01-02"), tenantID, studentID); err != nil {
		core.Logger.Error("enrollment end-all failed", "err", err, "student_id", studentID)
	}
}

// EnrollmentWindow is one class a student was enrolled in during a period,
// clipped to that period. From/To are inclusive TEXT dates, so a caller can
// hand them straight to SessionsInPeriod.
type EnrollmentWindow struct {
	ClassID string
	From    string
	To      string
}

// EnrollmentWindowsIn returns the classes a student was enrolled in at any
// point within [from, to], each clipped to the days they were actually
// enrolled. Billing counts sessions inside these windows rather than over the
// whole month, which is what makes a mid-month joiner pay for fewer sessions
// (Ely, 2026-09-01) instead of a full month.
//
// Enrolment is treated as HALF-OPEN, [started_on, ended_on): the day a student
// is removed is not billed. Removal stamps ended_on with that day, so a
// student taken off the list on the 15th is billed through the 14th. The
// alternative (inclusive) would bill a session on a day they had already left.
//
// Replaces reading students.enrolled_classes, which is a bare id list with no
// dates and therefore cannot answer this at all.
func EnrollmentWindowsIn(db *DB, tenantID int, studentID, from, to string) ([]EnrollmentWindow, error) {
	rows, err := db.Query(`SELECT class_id, started_on, COALESCE(ended_on,'')
		FROM enrollments
		WHERE tenant_id=? AND student_id=?
		  AND started_on <= ?
		  AND (ended_on IS NULL OR ended_on > ?)
		ORDER BY class_id, started_on`, tenantID, studentID, to, from)
	if err != nil {
		return nil, fmt.Errorf("enrollment windows: %w", err)
	}
	defer rows.Close()

	var out []EnrollmentWindow
	for rows.Next() {
		var classID, startedOn, endedOn string
		if rows.Scan(&classID, &startedOn, &endedOn) != nil {
			continue
		}
		w := EnrollmentWindow{ClassID: classID, From: from, To: to}
		if startedOn > w.From {
			w.From = startedOn
		}
		// ended_on is exclusive, so the last billable day is the one before it.
		if endedOn != "" {
			if last := dayBefore(endedOn); last < w.To {
				w.To = last
			}
		}
		if w.From <= w.To {
			out = append(out, w)
		}
	}
	return out, rows.Err()
}

func dayBefore(date string) string {
	d, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return date
	}
	return d.AddDate(0, 0, -1).Format("2006-01-02")
}
