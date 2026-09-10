package handlers

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

// Teacher chiying (staff s1) teaches c1, c3 and c6. STU003 sits in c5, so she
// does not teach them; STU001 sits in c3, so she does.
const (
	notMyStudent = "STU003"
	myStudent    = "STU001"
)

func postAttendance(t *testing.T, r *chi.Mux, token string, body map[string]any) int {
	t.Helper()
	return doRequest(r, "POST", "/api/attendance", token, body).Code
}

// Manning the front desk is the point: a teacher must be able to check in a
// child who is not in their own classes, notification and all.
func TestTeacherMayCheckInAnyStudent(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getTeacherToken(t, r)

	code := postAttendance(t, r, token, map[string]any{
		"personId": notMyStudent, "personType": "student",
		"date": "2026-09-10", "checkIn": "09:00", "status": "Present",
	})
	if code != http.StatusOK {
		t.Errorf("check-in for a student outside the teacher's classes got %d, want 200", code)
	}
}

func TestTeacherMayCheckOutAnyStudent(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getTeacherToken(t, r)

	postAttendance(t, r, token, map[string]any{
		"personId": notMyStudent, "personType": "student",
		"date": "2026-09-10", "checkIn": "09:00", "status": "Present",
	})
	code := postAttendance(t, r, token, map[string]any{
		"personId": notMyStudent, "personType": "student",
		"date": "2026-09-10", "checkIn": "09:00", "checkOut": "11:00", "status": "Present",
	})
	if code != http.StatusOK {
		t.Errorf("check-out for a student outside the teacher's classes got %d, want 200", code)
	}
}

// The absence record stays owner-only: marking a child absent overwrites the
// row their real teacher wrote.
func TestTeacherMayNotMarkAnotherTeachersStudentAbsent(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getTeacherToken(t, r)

	code := postAttendance(t, r, token, map[string]any{
		"personId": notMyStudent, "personType": "student",
		"date": "2026-09-10", "status": "Absent",
	})
	if code != http.StatusForbidden {
		t.Errorf("marking a student outside the teacher's classes absent got %d, want 403", code)
	}
}

// status rides the same UPDATE as the times, so the exemption has to test it.
// A check-in time with status Absent must not buy the absence write.
func TestCheckInPayloadCannotSmuggleAnAbsence(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getTeacherToken(t, r)

	code := postAttendance(t, r, token, map[string]any{
		"personId": notMyStudent, "personType": "student",
		"date": "2026-09-10", "checkIn": "09:00", "status": "Absent",
	})
	if code != http.StatusForbidden {
		t.Errorf("check-in payload carrying status Absent got %d, want 403", code)
	}
}

// The teacher's own students are unaffected in either direction.
func TestTeacherMayStillMarkOwnStudentAbsent(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getTeacherToken(t, r)

	code := postAttendance(t, r, token, map[string]any{
		"personId": myStudent, "personType": "student",
		"date": "2026-09-10", "status": "Absent",
	})
	if code != http.StatusOK {
		t.Errorf("marking the teacher's own student absent got %d, want 200", code)
	}
}
