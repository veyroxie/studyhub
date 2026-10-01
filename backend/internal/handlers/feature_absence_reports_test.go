package handlers

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

// The seeded parent seeduser27@example.com owns STU001. Chiying (s1) teaches the
// test class; Nadine (s2) does not.
const (
	absenceStudent = "STU001"
	otherTeacher   = "nadine@studyhub.com"
)

type absenceFixture struct {
	r       *chi.Mux
	db      *store.DB
	classID string
	date    string
	cleanup func()
}

// absenceClass is a 90-minute class at start, with STU001 enrolled since January.
func newAbsenceFixture(t *testing.T, start time.Time) absenceFixture {
	t.Helper()
	r, cleanup := setupTestApp(t)
	db := store.InitDB(testDSN())
	f := absenceFixture{r: r, db: db, classID: core.GenerateID("CLS"), date: start.Format("2006-01-02"),
		cleanup: func() { db.Close(); cleanup() }}
	end := start.Add(90 * time.Minute)
	if _, err := db.Exec(`INSERT INTO classes(id,tenant_id,name,teacher_ids,day,time,end_time,classroom) VALUES(?,1,'Absence Test',?,?,?,?,'Room A')`,
		f.classID, `["s1"]`, start.Weekday().String(), start.Format("15:04"), end.Format("15:04")); err != nil {
		t.Fatalf("insert class: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO enrollments(id,tenant_id,student_id,class_id,started_on) VALUES(?,1,?,?,'2026-01-01')`,
		core.GenerateID("ENR"), absenceStudent, f.classID); err != nil {
		t.Fatalf("insert enrollment: %v", err)
	}
	if _, err := db.Exec(`UPDATE students SET enrolled_classes=? WHERE id=?`, `["`+f.classID+`"]`, absenceStudent); err != nil {
		t.Fatalf("enrol: %v", err)
	}
	return f
}

func (f absenceFixture) report(t *testing.T, token string) (int, models.AbsenceReport) {
	t.Helper()
	w := doRequest(f.r, "POST", "/api/absence-reports", token, map[string]string{"studentId": absenceStudent, "classId": f.classID, "date": f.date, "reason": "dentist"})
	var rep models.AbsenceReport
	json.NewDecoder(w.Body).Decode(&rep)
	return w.Code, rep
}

func (f absenceFixture) decide(token, id string, approve bool, note string) (int, map[string]any) {
	w := doRequest(f.r, "POST", "/api/absence-reports/"+id+"/decision", token, map[string]any{"approve": approve, "note": note})
	var out map[string]any
	json.NewDecoder(w.Body).Decode(&out)
	return w.Code, out
}

func (f absenceFixture) credits(t *testing.T) (rows, units int) {
	t.Helper()
	f.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(minutes),0) FROM replacement_credits WHERE student_id=? AND class_id=? AND date=? AND type='earned'`,
		absenceStudent, f.classID, f.date).Scan(&rows, &units)
	return rows, units
}

// nextWeekAt: same weekday and minute next week, so the session is well past the 3-hour notice.
func nextWeekAt(hour int) time.Time {
	d := time.Now().AddDate(0, 0, 7)
	return time.Date(d.Year(), d.Month(), d.Day(), hour, 0, 0, 0, time.Local)
}

func TestAParentCanSeeAndReportTheirChildsUpcomingSession(t *testing.T) {
	f := newAbsenceFixture(t, nextWeekAt(16))
	defer f.cleanup()
	parent := getParentToken(t, f.r)

	w := doRequest(f.r, "GET", "/api/absence-reports/sessions?studentId="+absenceStudent, parent, nil)
	var sessions []reportableSession
	json.NewDecoder(w.Body).Decode(&sessions)
	var found *reportableSession
	for i := range sessions {
		if sessions[i].ClassID == f.classID && sessions[i].Date == f.date {
			found = &sessions[i]
		}
	}
	if found == nil || !found.InTime {
		t.Fatalf("the session next week is missing or not in time: %+v", sessions)
	}

	code, rep := f.report(t, parent)
	if code != http.StatusCreated || rep.Status != models.AbsencePending || !rep.InTime {
		t.Fatalf("report: %d %+v", code, rep)
	}
	if code, _ := f.report(t, parent); code != http.StatusConflict {
		t.Errorf("a second report of the same session: %d, want 409", code)
	}
	if rows, _ := f.credits(t); rows != 0 {
		t.Errorf("a pending report already granted %d credit rows", rows)
	}
}

func TestOnlyTheChildsParentCanReport(t *testing.T) {
	f := newAbsenceFixture(t, nextWeekAt(16))
	defer f.cleanup()
	for name, token := range map[string]string{"admin": getAdminToken(t, f.r), "teacher": getTeacherToken(t, f.r), "another parent": getToken(t, f.r, "seeduser13@example.com", "parent123")} {
		if code, _ := f.report(t, token); code != http.StatusForbidden {
			t.Errorf("%s reporting STU001: %d, want 403", name, code)
		}
	}
}

func TestReportingADayTheClassDoesNotRunIsRefused(t *testing.T) {
	f := newAbsenceFixture(t, nextWeekAt(16))
	defer f.cleanup()
	f.date = nextWeekAt(16).AddDate(0, 0, 1).Format("2006-01-02")
	if code, _ := f.report(t, getParentToken(t, f.r)); code != http.StatusBadRequest {
		t.Errorf("reporting a day with no class: %d, want 400", code)
	}
}

func TestApprovalGrantsOneCreditSizedByTheSession(t *testing.T) {
	f := newAbsenceFixture(t, nextWeekAt(16))
	defer f.cleanup()
	_, rep := f.report(t, getParentToken(t, f.r))

	if code, _ := f.decide(getToken(t, f.r, otherTeacher, "Teacher123!"), rep.ID, true, ""); code != http.StatusForbidden {
		t.Errorf("a teacher of another class approving: %d, want 403", code)
	}
	code, out := f.decide(getTeacherToken(t, f.r), rep.ID, true, "")
	if code != http.StatusOK || out["credited"] != true {
		t.Fatalf("class teacher approving: %d %v", code, out)
	}
	if rows, units := f.credits(t); rows != 1 || units != 6 {
		t.Errorf("credits after approval: %d rows, %d units; want 1 row of 6 (90 min / 15)", rows, units)
	}
	if code, _ := f.decide(getAdminToken(t, f.r), rep.ID, true, ""); code != http.StatusConflict {
		t.Errorf("deciding twice: %d, want 409", code)
	}

	// The teacher's own "Absent + credit" for the same session must not pay again.
	w := doRequest(f.r, "POST", "/api/replacement-credits", getTeacherToken(t, f.r), map[string]any{
		"studentId": absenceStudent, "type": "earned", "minutes": 6, "classId": f.classID, "date": f.date, "category": "class", "note": "Absent"})
	if w.Code != http.StatusConflict {
		t.Errorf("a second credit for the session: %d, want 409", w.Code)
	}
	if rows, _ := f.credits(t); rows != 1 {
		t.Errorf("credit rows after the duplicate attempt: %d, want 1", rows)
	}
}

func TestACancelledClassDoesNotCreditAnAlreadyCreditedAbsence(t *testing.T) {
	f := newAbsenceFixture(t, nextWeekAt(16))
	defer f.cleanup()
	_, rep := f.report(t, getParentToken(t, f.r))
	f.decide(getAdminToken(t, f.r), rep.ID, true, "")
	w := doRequest(f.r, "POST", "/api/cancelled-classes", getAdminToken(t, f.r), map[string]string{"classId": f.classID, "date": f.date, "reason": "Teacher sick"})
	if w.Code >= 300 {
		t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
	}
	if rows, _ := f.credits(t); rows != 1 {
		t.Errorf("credit rows after approval then cancellation: %d, want 1", rows)
	}
}

func TestDecliningNeedsAReasonAndGrantsNothing(t *testing.T) {
	f := newAbsenceFixture(t, nextWeekAt(16))
	defer f.cleanup()
	_, rep := f.report(t, getParentToken(t, f.r))
	admin := getAdminToken(t, f.r)
	if code, _ := f.decide(admin, rep.ID, false, ""); code != http.StatusBadRequest {
		t.Errorf("declining without a reason: %d, want 400", code)
	}
	if code, _ := f.decide(admin, rep.ID, false, "We had already planned a make-up class"); code != http.StatusOK {
		t.Fatalf("decline: %d", code)
	}
	if rows, _ := f.credits(t); rows != 0 {
		t.Errorf("a declined report granted %d credit rows", rows)
	}
}

func TestLessThanThreeHoursNoticeIsRecordedAsLate(t *testing.T) {
	soon := time.Now().Add(time.Hour).Truncate(time.Minute)
	if soon.Day() != time.Now().Day() || soon.Add(90*time.Minute).Day() != soon.Day() {
		t.Skip("too close to midnight to build a same-day class")
	}
	f := newAbsenceFixture(t, soon)
	defer f.cleanup()
	code, rep := f.report(t, getParentToken(t, f.r))
	if code != http.StatusCreated || rep.Status != models.AbsenceLate || rep.InTime {
		t.Fatalf("an hour's notice: %d %+v, want late", code, rep)
	}
	if code, _ := f.decide(getAdminToken(t, f.r), rep.ID, true, ""); code != http.StatusConflict {
		t.Errorf("approving a late report: %d, want 409", code)
	}
}

func TestEachRoleSeesOnlyItsOwnReports(t *testing.T) {
	f := newAbsenceFixture(t, nextWeekAt(16))
	defer f.cleanup()
	_, rep := f.report(t, getParentToken(t, f.r))
	seen := func(token string) bool {
		store.SnapshotCacheInvalidateAll()
		var snap models.Snapshot
		json.NewDecoder(doRequest(f.r, "GET", "/api/snapshot", token, nil).Body).Decode(&snap)
		for _, a := range snap.AbsenceReports {
			if a.ID == rep.ID {
				return true
			}
		}
		return false
	}
	if !seen(getParentToken(t, f.r)) || !seen(getTeacherToken(t, f.r)) || !seen(getAdminToken(t, f.r)) {
		t.Error("the parent, the class teacher and the admin should all see the report")
	}
	if seen(getToken(t, f.r, "seeduser13@example.com", "parent123")) {
		t.Error("another family's parent sees the report")
	}
	if seen(getToken(t, f.r, otherTeacher, "Teacher123!")) {
		t.Error("a teacher of another class sees the report")
	}
}
