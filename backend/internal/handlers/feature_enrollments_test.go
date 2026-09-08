package handlers

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"studyhub/internal/core"
)

// TestEnrollments_DualWriteLifecycle locks the B6 shadow-table contract:
// every mutation of students.enrolled_classes must be mirrored into the
// enrollments join table, where removal ENDS a row (ended_on) rather than
// deleting it — session billing needs the start/end history the JSON lacks.
func TestEnrollments_DualWriteLifecycle(t *testing.T) {
	r, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	tok := getToken(t, r, "admin@studyhub.com", "admin123")
	var tenantID int
	if err := db.QueryRow(`SELECT tenant_id FROM users WHERE email=?`, "admin@studyhub.com").Scan(&tenantID); err != nil {
		t.Fatalf("seed admin missing: %v", err)
	}

	classes := map[string]string{}
	for _, name := range []string{"Enrol A", "Enrol B", "Enrol C"} {
		id := core.GenerateID("CLS")
		classes[name] = id
		if _, err := db.Exec(
			`INSERT INTO classes(id,tenant_id,name,day,time,end_time,classroom) VALUES(?,?,?,?,?,?,?)`,
			id, tenantID, name, "Monday", "10:00", "11:00", "Room E",
		); err != nil {
			t.Fatalf("insert class %s: %v", name, err)
		}
	}

	w := authedJSON(t, r, "POST", "/api/students", tok, map[string]any{
		"firstName":       "Enrol",
		"lastName":        "Lifecycle",
		"contact":         "enrol-lifecycle@example.com",
		"parentName":      "Enrol Parent",
		"phone":           "60123450000",
		"branch":          "The Study Hub",
		"enrolledClasses": []string{classes["Enrol A"], classes["Enrol B"]},
	})
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("create student failed: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("create response missing id: %s", w.Body.String())
	}

	liveClasses := func() map[string]bool {
		rows, err := db.Query(`SELECT class_id FROM enrollments WHERE student_id=? AND ended_on IS NULL`, created.ID)
		if err != nil {
			t.Fatalf("read live enrollments: %v", err)
		}
		defer rows.Close()
		out := map[string]bool{}
		for rows.Next() {
			var cid string
			rows.Scan(&cid)
			out[cid] = true
		}
		return out
	}

	live := liveClasses()
	if len(live) != 2 || !live[classes["Enrol A"]] || !live[classes["Enrol B"]] {
		t.Fatalf("after create: expected live rows for A+B, got %v", live)
	}

	// Swap B for C: B's row must be ENDED (not deleted), C gets a new row,
	// A's original row survives untouched.
	var rowIDA string
	db.QueryRow(`SELECT id FROM enrollments WHERE student_id=? AND class_id=?`, created.ID, classes["Enrol A"]).Scan(&rowIDA)
	w = authedJSON(t, r, "PUT", "/api/students/"+created.ID, tok, map[string]any{
		"firstName":       "Enrol",
		"lastName":        "Lifecycle",
		"contact":         "enrol-lifecycle@example.com",
		"parentName":      "Enrol Parent",
		"phone":           "60123450000",
		"branch":          "The Study Hub",
		"enrolledClasses": []string{classes["Enrol A"], classes["Enrol C"]},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update student failed: %d %s", w.Code, w.Body.String())
	}
	live = liveClasses()
	if len(live) != 2 || !live[classes["Enrol A"]] || !live[classes["Enrol C"]] {
		t.Fatalf("after update: expected live rows for A+C, got %v", live)
	}
	var endedB string
	db.QueryRow(`SELECT COALESCE(ended_on,'') FROM enrollments WHERE student_id=? AND class_id=?`, created.ID, classes["Enrol B"]).Scan(&endedB)
	if endedB == "" {
		t.Fatal("removed class B should have ended_on set, not be deleted or live")
	}
	var rowIDAAfter string
	db.QueryRow(`SELECT id FROM enrollments WHERE student_id=? AND class_id=? AND ended_on IS NULL`, created.ID, classes["Enrol A"]).Scan(&rowIDAAfter)
	if rowIDAAfter != rowIDA {
		t.Fatalf("class A row should be untouched by the update: was %s, now %s", rowIDA, rowIDAAfter)
	}

	w = authedJSON(t, r, "DELETE", "/api/students/"+created.ID, tok, nil)
	if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Fatalf("delete student failed: %d %s", w.Code, w.Body.String())
	}
	if live = liveClasses(); len(live) != 0 {
		t.Fatalf("after delete: expected no live enrollments, got %v", live)
	}
	var total int
	total = countRows(t, db, `SELECT COUNT(*) FROM enrollments WHERE student_id=?`, created.ID)
	if total != 3 {
		t.Fatalf("history must survive the delete: expected 3 rows (A, B, C), got %d", total)
	}
}

// TestEnrollments_StartDateIsChosenNotAssumed locks the fix for the August
// attendance bug: 35 enrolments recorded the day the ROW was created as the
// join date, so enrolledOn hid 85 real attendance records behind windows that
// had not opened yet (migration 0055). An admin must be able to say WHEN the
// student joined, a bad date must be refused rather than quietly becoming
// today, and omitting it must still mean today.
func TestEnrollments_StartDateIsChosenNotAssumed(t *testing.T) {
	r, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	tok := getToken(t, r, "admin@studyhub.com", "admin123")
	var tenantID int
	if err := db.QueryRow(`SELECT tenant_id FROM users WHERE email=?`, "admin@studyhub.com").Scan(&tenantID); err != nil {
		t.Fatalf("seed admin missing: %v", err)
	}

	classes := map[string]string{}
	for _, name := range []string{"Backdate A", "Backdate B"} {
		id := core.GenerateID("CLS")
		classes[name] = id
		if _, err := db.Exec(
			`INSERT INTO classes(id,tenant_id,name,day,time,end_time,classroom) VALUES(?,?,?,?,?,?,?)`,
			id, tenantID, name, "Wednesday", "16:00", "17:00", "Room B",
		); err != nil {
			t.Fatalf("insert class %s: %v", name, err)
		}
	}

	w := authedJSON(t, r, "POST", "/api/students", tok, map[string]any{
		"firstName": "Backdate", "lastName": "Joiner",
		"contact": "backdate-joiner@example.com", "parentName": "BD Parent",
		"phone": "60123450001", "branch": "The Study Hub",
	})
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("create student failed: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("create response missing id: %s", w.Body.String())
	}

	// The create path takes the same field and must reject the same garbage.
	// started_on is TEXT compared lexically, so an unvalidated "05/08/2026"
	// would store and then sort wrong for the life of the row.
	w = authedJSON(t, r, "POST", "/api/students", tok, map[string]any{
		"firstName": "Bad", "lastName": "Date",
		"contact": "bad-date@example.com", "parentName": "BD Parent",
		"phone": "60123450002", "branch": "The Study Hub",
		"enrolledClasses": []string{classes["Backdate A"]},
		"enrolledFrom":    "05/08/2026",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed enrolledFrom on create must be refused, got %d %s", w.Code, w.Body.String())
	}

	startedOn := func(classID string) string {
		var got string
		db.QueryRow(`SELECT started_on FROM enrollments WHERE student_id=? AND class_id=? AND ended_on IS NULL`,
			created.ID, classID).Scan(&got)
		return got
	}

	// Creating a student with a backdated start records it too. Add Student and
	// Edit Student embed the same enrolment field as the Classes tab, so all
	// three must carry the date -- the original bug was one surface having it.
	w = authedJSON(t, r, "POST", "/api/students", tok, map[string]any{
		"firstName": "Created", "lastName": "Backdated",
		"contact": "created-backdated@example.com", "parentName": "CB Parent",
		"phone": "60123450003", "branch": "The Study Hub",
		"enrolledClasses": []string{classes["Backdate B"]},
		"enrolledFrom":    "2026-08-01",
	})
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("create with a backdate failed: %d %s", w.Code, w.Body.String())
	}
	var madeBackdated struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &madeBackdated)
	var createdStart string
	db.QueryRow(`SELECT started_on FROM enrollments WHERE student_id=? AND class_id=?`,
		madeBackdated.ID, classes["Backdate B"]).Scan(&createdStart)
	if createdStart != "2026-08-01" {
		t.Fatalf("create path must record the chosen start: want 2026-08-01, got %s", createdStart)
	}

	const backdate = "2026-08-05"
	w = authedJSON(t, r, "PUT", "/api/students/"+created.ID, tok, map[string]any{
		"firstName": "Backdate", "lastName": "Joiner",
		"contact": "backdate-joiner@example.com", "parentName": "BD Parent",
		"phone": "60123450001", "branch": "The Study Hub",
		"enrolledClasses": []string{classes["Backdate A"]},
		"enrolledFrom":    backdate,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("backdated enrol failed: %d %s", w.Code, w.Body.String())
	}
	if got := startedOn(classes["Backdate A"]); got != backdate {
		t.Fatalf("started_on must be the date the admin chose: want %s, got %s", backdate, got)
	}

	// A malformed date is refused. Falling back to today is precisely how the
	// original bug wrote 35 wrong join dates without anyone noticing.
	w = authedJSON(t, r, "PUT", "/api/students/"+created.ID, tok, map[string]any{
		"firstName": "Backdate", "lastName": "Joiner",
		"contact": "backdate-joiner@example.com", "parentName": "BD Parent",
		"phone": "60123450001", "branch": "The Study Hub",
		"enrolledClasses": []string{classes["Backdate A"], classes["Backdate B"]},
		"enrolledFrom":    "05/08/2026",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed enrolledFrom must be refused, got %d %s", w.Code, w.Body.String())
	}
	if got := startedOn(classes["Backdate B"]); got != "" {
		t.Fatalf("refused request must not have enrolled B, got started_on %s", got)
	}

	// Omitted means today, so every existing caller keeps its behaviour.
	w = authedJSON(t, r, "PUT", "/api/students/"+created.ID, tok, map[string]any{
		"firstName": "Backdate", "lastName": "Joiner",
		"contact": "backdate-joiner@example.com", "parentName": "BD Parent",
		"phone": "60123450001", "branch": "The Study Hub",
		"enrolledClasses": []string{classes["Backdate A"], classes["Backdate B"]},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("enrol without a date failed: %d %s", w.Code, w.Body.String())
	}
	today := time.Now().Format("2006-01-02")
	if got := startedOn(classes["Backdate B"]); got != today {
		t.Fatalf("omitted enrolledFrom must mean today: want %s, got %s", today, got)
	}
	if got := startedOn(classes["Backdate A"]); got != backdate {
		t.Fatalf("a later save must not rewrite an existing enrolment: want %s, got %s", backdate, got)
	}
}
