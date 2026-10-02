package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

// Parent absence reports (0071). A parent says a child will miss one session;
// told 3+ hours before it starts, an admin or the class's teacher approves the
// replacement credit; told later, it is recorded and earns nothing.

const (
	absenceNotice        = 3 * time.Hour
	absenceLookAheadDays = 21
	absenceHistoryDays   = 30
	absenceReasonMax     = 300
	sessionStartLayout   = "2006-01-02 15:04"
)

// reportableSession is one session a child is due at: the class runs on date (held, or moved
// in and not cancelled) and the child is enrolled that day. start is local time.
type reportableSession struct {
	ClassID   string    `json:"classId"`
	ClassName string    `json:"className"`
	Date      string    `json:"date"`
	Time      string    `json:"time"`
	EndTime   string    `json:"endTime"`
	Start     time.Time `json:"start"`
	InTime    bool      `json:"inTime"`
	Reported  string    `json:"reported,omitempty"` // status of an existing report
}

func sessionRuns(s store.ClassSession) bool {
	return s.Status == store.SessionHeld || (s.Status == store.SessionMovedIn && !s.Cancelled)
}

// findReportableSession resolves classID on date for studentID through the canonical
// session expander, so moves, cancellations and schedule versions all count.
func findReportableSession(db *store.DB, tid int, studentID, classID, date string) (reportableSession, error) {
	sessions, err := store.SessionsInPeriod(db, classID, date, date)
	if err != nil {
		return reportableSession{}, fmt.Errorf("expand sessions: %w", err)
	}
	for _, s := range sessions {
		if s.Date != date || !sessionRuns(s) {
			continue
		}
		enrolled, _, err := store.StudentsInClassOn(db, tid, classID, date)
		if err != nil {
			return reportableSession{}, err
		}
		if !containsString(enrolled, studentID) {
			return reportableSession{}, userError("this child is not in that class on that day")
		}
		start, err := time.ParseInLocation(sessionStartLayout, date+" "+s.Time, time.Local)
		if err != nil {
			return reportableSession{}, fmt.Errorf("session start %q: %w", s.Time, err)
		}
		return reportableSession{ClassID: classID, Date: date, Time: s.Time, EndTime: s.EndTime, Start: start,
			InTime: time.Until(start) >= absenceNotice}, nil
	}
	return reportableSession{}, userError("that class does not run on that day")
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// parentOwnsStudent: a parent may report only for their own child.
func parentOwnsStudent(db *store.DB, c *core.Claims, studentID string) bool {
	return c != nil && c.Role == "parent" && parentStudentIDs(db, c)[studentID]
}

// GET /api/absence-reports/sessions?studentId= : the child's sessions in the next three weeks.
func HandleAbsenceSessions(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		studentID := r.URL.Query().Get("studentId")
		if !parentOwnsStudent(db, c, studentID) {
			core.RespondError(w, "you can only report absences for your own child", http.StatusForbidden)
			return
		}
		out, err := upcomingSessions(db, store.TenantID(c), studentID)
		if err != nil {
			core.LogFromReq(r).Error("absence sessions failed", "err", err, "student_id", studentID)
			core.RespondError(w, "could not load the schedule", http.StatusInternalServerError)
			return
		}
		core.Respond(w, out)
	}
}

func upcomingSessions(db *store.DB, tid int, studentID string) ([]reportableSession, error) {
	now := time.Now()
	from, to := now.Format("2006-01-02"), now.AddDate(0, 0, absenceLookAheadDays).Format("2006-01-02")
	classes, err := studentClassesFrom(db, tid, studentID, from)
	if err != nil {
		return nil, err
	}
	reported, err := reportedSessions(db, tid, studentID, from)
	if err != nil {
		return nil, err
	}
	out := []reportableSession{}
	for id, name := range classes {
		sessions, err := store.SessionsInPeriod(db, id, from, to)
		if err != nil {
			return nil, fmt.Errorf("expand sessions for %s: %w", id, err)
		}
		for _, s := range sessions {
			rs, ok := upcomingSession(db, tid, studentID, id, name, s, now)
			if !ok {
				continue
			}
			rs.Reported = reported[id+"|"+s.Date]
			out = append(out, rs)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

// upcomingSession keeps a session that runs, has not started, and the child is enrolled in that day.
func upcomingSession(db *store.DB, tid int, studentID, classID, name string, s store.ClassSession, now time.Time) (reportableSession, bool) {
	if !sessionRuns(s) {
		return reportableSession{}, false
	}
	start, err := time.ParseInLocation(sessionStartLayout, s.Date+" "+s.Time, time.Local)
	if err != nil || !start.After(now) {
		return reportableSession{}, false
	}
	enrolled, _, err := store.StudentsInClassOn(db, tid, classID, s.Date)
	if err != nil || !containsString(enrolled, studentID) {
		return reportableSession{}, false
	}
	return reportableSession{ClassID: classID, ClassName: name, Date: s.Date, Time: s.Time, EndTime: s.EndTime,
		Start: start, InTime: start.Sub(now) >= absenceNotice}, true
}

// studentClassesFrom: classes the child is enrolled in on or after from, id -> name.
func studentClassesFrom(db *store.DB, tid int, studentID, from string) (map[string]string, error) {
	// Dated enrolments, plus the current roster for classes with no enrolment rows (see store.StudentsInClassOn).
	rows, err := db.Query(`SELECT DISTINCT e.class_id, cl.name FROM enrollments e
		JOIN classes cl ON cl.id = e.class_id AND cl.tenant_id = e.tenant_id AND cl.deleted_at IS NULL
		WHERE e.tenant_id=? AND e.student_id=? AND (e.ended_on IS NULL OR e.ended_on > ?)
		UNION
		SELECT cl.id, cl.name FROM classes cl JOIN students s ON s.tenant_id = cl.tenant_id AND s.id=?
		WHERE cl.tenant_id=? AND cl.deleted_at IS NULL AND s.enrolled_classes LIKE '%"'||cl.id||'"%'
		  AND NOT EXISTS (SELECT 1 FROM enrollments e2 WHERE e2.tenant_id=cl.tenant_id AND e2.class_id=cl.id)`, tid, studentID, from, studentID, tid)
	if err != nil {
		return nil, fmt.Errorf("student classes: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

func reportedSessions(db *store.DB, tid int, studentID, from string) (map[string]string, error) {
	rows, err := db.Query(`SELECT class_id, session_date, status FROM absence_reports WHERE tenant_id=? AND student_id=? AND session_date >= ? AND deleted_at IS NULL`, tid, studentID, from)
	if err != nil {
		return nil, fmt.Errorf("reported sessions: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var classID, date, status string
		if err := rows.Scan(&classID, &date, &status); err != nil {
			return nil, err
		}
		out[classID+"|"+date] = status
	}
	return out, rows.Err()
}

// POST /api/absence-reports {studentId, classId, date, reason}
func HandleCreateAbsenceReport(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		var body struct {
			StudentID string `json:"studentId"`
			ClassID   string `json:"classId"`
			Date      string `json:"date"`
			Reason    string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			core.RespondError(w, "bad body", http.StatusBadRequest)
			return
		}
		if !parentOwnsStudent(db, c, body.StudentID) {
			core.RespondError(w, "you can only report absences for your own child", http.StatusForbidden)
			return
		}
		body.Reason = strings.TrimSpace(body.Reason)
		if utf8.RuneCountInString(body.Reason) > absenceReasonMax {
			core.RespondError(w, "keep the reason under 300 characters", http.StatusBadRequest)
			return
		}
		tid := store.TenantID(c)
		sess, err := findReportableSession(db, tid, body.StudentID, body.ClassID, body.Date)
		if err != nil {
			respondCheckError(w, r, err, http.StatusBadRequest)
			return
		}
		if !sess.Start.After(time.Now()) {
			core.RespondError(w, "that class has already started", http.StatusBadRequest)
			return
		}
		rep, err := insertAbsenceReport(db, tid, c.Email, body.StudentID, body.Reason, sess)
		if store.IsUniqueViolation(err) {
			core.RespondError(w, "you have already reported this absence", http.StatusConflict)
			return
		}
		if err != nil {
			core.LogFromReq(r).Error("absence report insert failed", "err", err)
			core.RespondError(w, "could not save the report", http.StatusInternalServerError)
			return
		}
		core.LogAudit(db, tid, c.Email, "absence_reported", "absence_report", rep.ID, fmt.Sprintf("student=%s class=%s date=%s in_time=%v", rep.StudentID, rep.ClassID, rep.SessionDate, rep.InTime))
		w.WriteHeader(http.StatusCreated)
		core.Respond(w, rep)
	}
}

func insertAbsenceReport(db *store.DB, tid int, by, studentID, reason string, sess reportableSession) (models.AbsenceReport, error) {
	rep := models.AbsenceReport{ID: core.GenerateID("ABS"), StudentID: studentID, ClassID: sess.ClassID, SessionDate: sess.Date,
		SessionTime: sess.Time, SessionEnd: sess.EndTime, ReportedAt: time.Now(), ReportedBy: by, Reason: reason,
		InTime: sess.InTime, Status: models.AbsenceLate}
	if sess.InTime {
		rep.Status = models.AbsencePending
	}
	_, err := db.Exec(`INSERT INTO absence_reports(id,tenant_id,student_id,class_id,session_date,session_time,session_end,reported_at,reported_by,reason,in_time,status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		rep.ID, tid, rep.StudentID, rep.ClassID, rep.SessionDate, rep.SessionTime, rep.SessionEnd, rep.ReportedAt, rep.ReportedBy, rep.Reason, rep.InTime, rep.Status)
	return rep, err
}

// mayDecideAbsence: an admin, or a teacher of that class.
func mayDecideAbsence(db *store.DB, c *core.Claims, classID string) bool {
	if core.IsAdminRole(c) {
		return true
	}
	return c != nil && c.Role == "teacher" && teacherClassIDSet(db, c)[classID]
}

// POST /api/absence-reports/{id}/decision {approve, note}
func HandleDecideAbsenceReport(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		var body struct {
			Approve bool   `json:"approve"`
			Note    string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			core.RespondError(w, "bad body", http.StatusBadRequest)
			return
		}
		body.Note = strings.TrimSpace(body.Note)
		if !body.Approve && body.Note == "" {
			core.RespondError(w, "tell the parent why the credit was not given", http.StatusBadRequest)
			return
		}
		if utf8.RuneCountInString(body.Note) > absenceReasonMax {
			core.RespondError(w, "keep the note under 300 characters", http.StatusBadRequest)
			return
		}
		credited, err := decideAbsenceReport(r, db, c, chi.URLParam(r, "id"), body.Approve, body.Note)
		if err != nil {
			respondCheckError(w, r, err, absenceDecisionStatus(err))
			return
		}
		core.Respond(w, map[string]any{"approved": body.Approve, "credited": credited})
	}
}

var (
	errAbsenceNotFound = userError("absence report not found")
	errAbsenceDecided  = userError("this absence report was already decided")
	errAbsenceNotYours = userError("only an admin or the class's teacher can decide this")
	errAbsenceAttended = userError("this child was marked present for that class, so there is nothing to make up")
)

func absenceDecisionStatus(err error) int {
	switch {
	case errors.Is(err, errAbsenceNotFound):
		return http.StatusNotFound
	case errors.Is(err, errAbsenceDecided), errors.Is(err, errAbsenceAttended):
		return http.StatusConflict
	case errors.Is(err, errAbsenceNotYours):
		return http.StatusForbidden
	}
	return http.StatusBadRequest
}

// decideAbsenceReport locks the report, grants the credit on approval, and records the decision, in one transaction.
func decideAbsenceReport(r *http.Request, db *store.DB, c *core.Claims, id string, approve bool, note string) (bool, error) {
	tx, err := db.BeginTx(r.Context())
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	tw, twArgs := store.ScopeTenant(c, "")
	var rep models.AbsenceReport
	var tid int
	err = tx.QueryRow(`SELECT tenant_id, student_id, class_id, session_date, session_time, session_end, status FROM absence_reports WHERE id=? AND deleted_at IS NULL`+tw+` FOR UPDATE`,
		append([]any{id}, twArgs...)...).Scan(&tid, &rep.StudentID, &rep.ClassID, &rep.SessionDate, &rep.SessionTime, &rep.SessionEnd, &rep.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, errAbsenceNotFound
	}
	if err != nil {
		return false, fmt.Errorf("load absence report: %w", err)
	}
	if !mayDecideAbsence(db, c, rep.ClassID) {
		return false, errAbsenceNotYours
	}
	if rep.Status != models.AbsencePending {
		return false, errAbsenceDecided
	}
	status, creditID, credited := models.AbsenceDeclined, "", false
	if approve {
		present, err := attendedSession(tx, tid, rep.StudentID, rep.ClassID, rep.SessionDate)
		if err != nil {
			return false, fmt.Errorf("attendance check: %w", err)
		}
		if present {
			return false, errAbsenceAttended
		}
		status = models.AbsenceApproved
		// credit_id names the credit this report relies on, new or already there, so undoing a cancellation keeps it.
		creditID, credited, err = store.GrantSessionCredit(tx, store.SessionCredit{TenantID: tid, StudentID: rep.StudentID, ClassID: rep.ClassID,
			Date: rep.SessionDate, Credits: sessionCreditUnits(db, tid, rep.ClassID, rep.SessionDate), Note: "Absence reported for " + rep.SessionDate, CreatedBy: c.Email})
		if err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec(`UPDATE absence_reports SET status=?, decided_by=?, decided_at=now(), decision_note=?, credit_id=? WHERE id=?`,
		status, c.Email, note, creditID, id); err != nil {
		return false, fmt.Errorf("record decision: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	core.LogAudit(db, tid, c.Email, "absence_"+status, "absence_report", id, fmt.Sprintf("student=%s credited=%v", rep.StudentID, credited))
	return credited, nil
}

// listAbsenceReports: a parent's own children, a teacher's classes, everything for an admin; last 30 days onward.
func listAbsenceReports(db *store.DB, c *core.Claims) []models.AbsenceReport {
	tw, twArgs := store.ScopeTenant(c, "")
	since := time.Now().AddDate(0, 0, -absenceHistoryDays).Format("2006-01-02")
	rows, err := db.Query(`SELECT id,student_id,class_id,session_date,session_time,session_end,reported_at,reported_by,reason,in_time,status,decided_by,decided_at,decision_note,credit_id
		FROM absence_reports WHERE deleted_at IS NULL AND (session_date >= ? OR status='pending')`+tw+` ORDER BY session_date, reported_at`, append([]any{since}, twArgs...)...)
	if err != nil {
		core.Logger.Error("absence reports query failed", "err", err)
		return []models.AbsenceReport{}
	}
	defer rows.Close()
	keep := absenceScope(db, c)
	out := []models.AbsenceReport{}
	for rows.Next() {
		var a models.AbsenceReport
		if err := rows.Scan(&a.ID, &a.StudentID, &a.ClassID, &a.SessionDate, &a.SessionTime, &a.SessionEnd, &a.ReportedAt, &a.ReportedBy, &a.Reason,
			&a.InTime, &a.Status, &a.DecidedBy, &a.DecidedAt, &a.DecisionNote, &a.CreditID); err != nil {
			core.Logger.Error("absence report row unreadable", "err", err)
			continue
		}
		if keep(a) {
			out = append(out, a)
		}
	}
	return out
}

func absenceScope(db *store.DB, c *core.Claims) func(models.AbsenceReport) bool {
	if core.IsAdminRole(c) {
		return func(models.AbsenceReport) bool { return true }
	}
	if c != nil && c.Role == "teacher" {
		classes := teacherClassIDSet(db, c)
		return func(a models.AbsenceReport) bool { return classes[a.ClassID] }
	}
	own := parentStudentIDs(db, c)
	return func(a models.AbsenceReport) bool { return own[a.StudentID] }
}
