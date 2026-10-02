package handlers

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"studyhub/internal/core"
	"studyhub/internal/store"
)

const undoTeacher = "chiying@studyhub.com"

func teacherStudents(t *testing.T, db *store.DB) (own, other string) {
	t.Helper()
	mine := teacherStudentIDSet(db, &core.Claims{TenantID: 1, Role: "teacher", Email: undoTeacher})
	rows, err := db.Query(`SELECT id FROM students WHERE tenant_id=1 AND deleted_at IS NULL ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		rows.Scan(&id)
		if mine[id] && own == "" {
			own = id
		}
		if !mine[id] && other == "" {
			other = id
		}
	}
	if own == "" || other == "" {
		t.Fatalf("seed has no own/other student for %s (own=%q other=%q)", undoTeacher, own, other)
	}
	return own, other
}

func attendanceRow(t *testing.T, db *store.DB, studentID, date, status string) string {
	t.Helper()
	id := core.GenerateID("ATT")
	if _, err := db.Exec(`INSERT INTO attendance(id,tenant_id,person_id,person_type,date,class_id,check_in,status) VALUES(?,?,?,?,?,?,?,?)`,
		id, 1, studentID, "student", date, "c1", "16:00", status); err != nil {
		t.Fatalf("seed attendance: %v", err)
	}
	return id
}

// A teacher can take back a mis-tapped mark on their own class the same day. An
// absence that earned a make-up credit stays with the admin: undo does not claw it back.
func TestATeacherCanUndoOnlyTheirOwnSameDayCheckIn(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	defer db.Close()
	teacher := getToken(t, r, undoTeacher, "Teacher123!")
	own, other := teacherStudents(t, db)
	today := core.Today()
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	cases := []struct {
		name, student, date, status string
		want                        int
	}{
		{"own class, today, present", own, today, "Present", http.StatusNoContent},
		{"own class, today, absent with no credit", own, today, "Absent", http.StatusNoContent},
		{"another class, today, present", other, today, "Present", http.StatusForbidden},
		{"own class, yesterday, present", own, yesterday, "Present", http.StatusForbidden},
	}
	for _, tc := range cases {
		id := attendanceRow(t, db, tc.student, tc.date, tc.status)
		if w := doRequest(r, "DELETE", "/api/attendance/"+id, teacher, nil); w.Code != tc.want {
			t.Errorf("%s: got %d, want %d (%s)", tc.name, w.Code, tc.want, w.Body.String())
		}
	}
	credited := attendanceRow(t, db, own, today, "Absent")
	db.Exec(`INSERT INTO replacement_credits(id,tenant_id,student_id,type,minutes,note,class_id,date,category) VALUES(?,1,?,'earned',4,'Absent','c1',?,'class')`,
		core.GenerateID("RC"), own, today)
	if w := doRequest(r, "DELETE", "/api/attendance/"+credited, teacher, nil); w.Code != http.StatusForbidden {
		t.Errorf("own class, today, absent with a credit: got %d, want 403", w.Code)
	}
}

// A teacher sees their own hours by payroll's rule, and nobody else's.
func TestATeacherSeesTheirOwnHoursAndPay(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	defer db.Close()
	var staffID string
	db.QueryRow(`SELECT id FROM staff WHERE email=?`, undoTeacher).Scan(&staffID)
	const month = "2026-08"
	db.Exec(`DELETE FROM attendance WHERE person_id=? AND date LIKE ?`, staffID, month+"%")
	db.Exec(`DELETE FROM payroll WHERE staff_id=? AND month=?`, staffID, month)
	db.Exec(`INSERT INTO attendance(id,tenant_id,person_id,person_type,date,check_in,check_out,status) VALUES(?,?,?,?,?,?,?,?)`,
		core.GenerateID("ATT"), 1, staffID, "staff", month+"-12", "16:00", "18:30", "Present")
	db.Exec(`INSERT INTO payroll(id,tenant_id,staff_id,month,total,status) VALUES(?,?,?,?,?,?)`, core.GenerateID("PAY"), 1, staffID, month, 312.5, "Paid")

	teacher := getToken(t, r, undoTeacher, "Teacher123!")
	w := doRequest(r, "GET", "/api/me/hours?month="+month, teacher, nil)
	var got struct {
		Hours float64 `json:"hours"`
		Pay   *struct {
			Total  float64 `json:"total"`
			Status string  `json:"status"`
		} `json:"pay"`
	}
	json.NewDecoder(w.Body).Decode(&got)
	if w.Code != http.StatusOK || got.Hours != 2.5 || got.Pay == nil || got.Pay.Total != 312.5 || got.Pay.Status != "Paid" {
		t.Fatalf("code %d, got %+v pay %+v", w.Code, got, got.Pay)
	}
	if w := doRequest(r, "GET", "/api/me/hours", getAdminToken(t, r), nil); w.Code != http.StatusForbidden {
		t.Fatalf("admin: got %d, want 403", w.Code)
	}
}
