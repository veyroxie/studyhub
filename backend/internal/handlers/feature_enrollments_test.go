package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

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
	db.QueryRow(`SELECT COUNT(*) FROM enrollments WHERE student_id=?`, created.ID).Scan(&total)
	if total != 3 {
		t.Fatalf("history must survive the delete: expected 3 rows (A, B, C), got %d", total)
	}
}
