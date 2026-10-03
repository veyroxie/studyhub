package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"studyhub/internal/models"
)

// Nadine could not add a class beside a Self-Study session: "Add anyway" resent the
// same request and the server refused every clash outright, naming the teacher by id.
func TestAClashIsAWarningAnAdminCanOverride(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	admin := getAdminToken(t, r)
	first := models.Class{Name: "Self-Study", Day: "Saturday", Time: "09:45", EndTime: "10:45", Classroom: "Classroom 3", TeacherIDs: []string{"s2"}, Capacity: 5}
	if w := doRequest(r, "POST", "/api/classes", admin, first); w.Code != http.StatusOK {
		t.Fatalf("first class: %d %s", w.Code, w.Body.String())
	}

	room := models.Class{Name: "Level 5", Day: "Saturday", Time: "10:00", EndTime: "11:00", Classroom: "Classroom 3", Capacity: 5}
	w := doRequest(r, "POST", "/api/classes", admin, room)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "Classroom 3 already has Self-Study") {
		t.Fatalf("room clash: %d %s, want 409 naming the class", w.Code, w.Body.String())
	}
	if w := doRequest(r, "POST", "/api/classes?allowClash=1", admin, room); w.Code != http.StatusOK {
		t.Errorf("add anyway: %d %s", w.Code, w.Body.String())
	}

	teacher := models.Class{Name: "Level 6", Day: "Saturday", Time: "10:00", EndTime: "11:00", Classroom: "Classroom 1", TeacherIDs: []string{"s2"}, Capacity: 5}
	w = doRequest(r, "POST", "/api/classes", admin, teacher)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "Nadine") || strings.Contains(w.Body.String(), "s2 ") {
		t.Errorf("teacher clash: %d %s, want the teacher named, not their id", w.Code, w.Body.String())
	}
	if w := doRequest(r, "POST", "/api/classes?allowClash=1", admin, teacher); w.Code != http.StatusOK {
		t.Errorf("add anyway with the teacher: %d %s", w.Code, w.Body.String())
	}
}

// A class that already overlaps another must still be renamable; only a move is checked.
func TestRenamingAnOverlappingClassIsNotBlocked(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	admin := getAdminToken(t, r)
	a := models.Class{Name: "A", Day: "Sunday", Time: "10:00", EndTime: "11:00", Classroom: "Classroom 2", TeacherIDs: []string{"s1"}, Capacity: 5}
	doRequest(r, "POST", "/api/classes", admin, a)
	var b models.Class
	w := doRequest(r, "POST", "/api/classes?allowClash=1", admin, models.Class{Name: "B", Day: "Sunday", Time: "10:30", EndTime: "11:30", Classroom: "Classroom 2", TeacherIDs: []string{"s1"}, Capacity: 5})
	json.NewDecoder(w.Body).Decode(&b)
	b.Name = "B renamed"
	if w := doRequest(r, "PUT", "/api/classes/"+b.ID, admin, b); w.Code >= 300 {
		t.Errorf("renaming an overlapping class: %d %s", w.Code, w.Body.String())
	}
	b.Time, b.EndTime = "10:15", "11:15"
	if w := doRequest(r, "PUT", "/api/classes/"+b.ID, admin, b); w.Code != http.StatusConflict {
		t.Errorf("moving it within the clash: %d, want 409", w.Code)
	}
}
