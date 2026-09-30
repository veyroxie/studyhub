package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/models"
	"studyhub/internal/store"
)

const (
	familyParent    = "seeduser27@example.com"
	familyParentPwd = "parent123"
)

// An email change ends every session with sessions_invalid_before=NOW(), and a JWT's
// issue time is whole seconds, so a sign-in in that same second reads as "before".
// A person cannot sign in that fast; the test waits the way they would.
func afterSessionCutoff() {
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second + 20*time.Millisecond)))
}

func userIDFor(t *testing.T, db *store.DB, email string) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`SELECT id FROM users WHERE email=?`, email).Scan(&id); err != nil {
		t.Fatalf("user %s: %v", email, err)
	}
	return id
}

func studentsSeenBy(t *testing.T, r *chi.Mux, email, password string) []string {
	t.Helper()
	token := getToken(t, r, email, password)
	var students []models.Student
	json.NewDecoder(doRequest(r, "GET", "/api/students", token, nil).Body).Decode(&students)
	ids := []string{}
	for _, s := range students {
		ids = append(ids, s.ID)
	}
	return ids
}

// A parent is tied to their children only by email (students.contact). Moving the
// login without the children left the parent signed in to an empty account.
func TestAnAdminChangingAParentsEmailKeepsTheirChildren(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	defer db.Close()
	admin := getAdminToken(t, r)
	const moved = "tan.family@example.com"

	id := userIDFor(t, db, familyParent)
	if w := doRequest(r, "PUT", "/api/users/"+strconv.Itoa(id)+"/credentials", admin, map[string]string{"email": moved}); w.Code != http.StatusOK {
		t.Fatalf("change email: %d %s", w.Code, w.Body.String())
	}
	afterSessionCutoff()
	if got := studentsSeenBy(t, r, moved, familyParentPwd); len(got) != 2 {
		t.Fatalf("parent sees %v after the change, want both children", got)
	}
	var families int
	db.QueryRow(`SELECT count(*) FROM families WHERE contact=?`, moved).Scan(&families)
	if families == 0 {
		t.Error("the family record still names the old email")
	}
}

func TestAParentCanChangeTheirOwnEmailAndKeepTheirChildren(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	defer db.Close()
	token := getToken(t, r, familyParent, familyParentPwd)
	const moved = "tan.new@example.com"

	w := doRequest(r, "POST", "/api/auth/change-email", token, map[string]string{"currentPassword": familyParentPwd, "newEmail": "  Tan.New@Example.com "})
	if w.Code != http.StatusOK {
		t.Fatalf("change own email: %d %s", w.Code, w.Body.String())
	}
	if w := doRequest(r, "GET", "/api/students", token, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("old session after the change: got %d, want 401", w.Code)
	}
	afterSessionCutoff()
	if got := studentsSeenBy(t, r, moved, familyParentPwd); len(got) != 2 {
		t.Fatalf("parent sees %v under the new email, want both children", got)
	}
	if code := doRequest(r, "POST", "/api/auth/login", "", map[string]string{"email": familyParent, "password": familyParentPwd}).Code; code == http.StatusOK {
		t.Fatal("the old email still signs in")
	}
}

func TestAWrongPasswordOrATakenEmailChangesNothing(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	defer db.Close()
	token := getToken(t, r, familyParent, familyParentPwd)

	if w := doRequest(r, "POST", "/api/auth/change-email", token, map[string]string{"currentPassword": "wrong-password", "newEmail": "x@example.com"}); w.Code != http.StatusForbidden {
		t.Fatalf("wrong password: got %d, want 403", w.Code)
	}
	if w := doRequest(r, "POST", "/api/auth/change-email", token, map[string]string{"currentPassword": familyParentPwd, "newEmail": "admin@studyhub.com"}); w.Code != http.StatusConflict {
		t.Fatalf("taken email: got %d, want 409", w.Code)
	}
	var contact string
	db.QueryRow(`SELECT contact FROM students WHERE id='STU001'`).Scan(&contact)
	if contact != familyParent {
		t.Fatalf("children moved to %q after a refused change", contact)
	}
}

// A teacher's classes resolve through staff.email, so it moves with the login.
func TestATeacherChangingTheirEmailKeepsTheirStaffRecord(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	defer db.Close()
	const teacher, moved = "chiying@studyhub.com", "chiying.new@example.com"
	var staffID string
	db.QueryRow(`SELECT id FROM staff WHERE email=?`, teacher).Scan(&staffID)
	token := getToken(t, r, teacher, "Teacher123!")

	if w := doRequest(r, "POST", "/api/auth/change-email", token, map[string]string{"currentPassword": "Teacher123!", "newEmail": moved}); w.Code != http.StatusOK {
		t.Fatalf("change email: %d %s", w.Code, w.Body.String())
	}
	var email string
	db.QueryRow(`SELECT email FROM staff WHERE id=?`, staffID).Scan(&email)
	if staffID == "" || email != moved {
		t.Fatalf("staff %q email %q, want it moved to %q", staffID, email, moved)
	}
}
