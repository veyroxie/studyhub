package store

import (
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
