package handlers

import (
	"net/http"
	"testing"

	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/store"
)

// The gap this closes: reset needs an emailed token, outbound mail is
// restricted (ADR-015), so an admin had no way to give a teacher working
// credentials short of deleting the account.
// makeThrowawayStaff creates a user AND a matching staff row for one test to
// own. The first version of these tests mutated the SEEDED accounts -- renaming
// rose@studyhub.com and resetting the password of chiying -- and left them that way, so a
// later run of the whole suite failed with "invalid credentials" on accounts
// that had nothing to do with the test being run. State, presenting as code.
func makeThrowawayStaff(t *testing.T, db *store.DB, label string) (userID, email string) {
	t.Helper()
	email = label + "-" + core.GenerateID("u") + "@example.com"
	hash, err := auth.HashPassword("initial-password-x")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO users(tenant_id,email,password_hash,role,name,status) VALUES(?,?,?,?,?,?) RETURNING id::text`,
		1, email, hash, "teacher", label, "active").Scan(&userID); err != nil {
		t.Fatalf("create throwaway user: %v", err)
	}
	staffID := core.GenerateID("STF")
	if _, err := db.Exec(`INSERT INTO staff(id,tenant_id,name,full_name,role,email,status) VALUES(?,?,?,?,?,?,?)`,
		staffID, 1, label, label, "Teacher", email, "Active"); err != nil {
		t.Fatalf("create throwaway staff: %v", err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM users WHERE id::text=?`, userID)
		db.Exec(`DELETE FROM staff WHERE id=?`, staffID)
	})
	return userID, email
}

func TestAdminCanSetAUserPasswordAndSignInWithIt(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	userID, email := makeThrowawayStaff(t, db, "setpw")

	w := doRequest(r, "PUT", "/api/users/"+userID+"/credentials", token,
		map[string]string{"password": "a-new-strong-password"})
	if w.Code != http.StatusOK {
		t.Fatalf("set password: %d %s", w.Code, w.Body.String())
	}
	if tok := getToken(t, r, email, "a-new-strong-password"); tok == "" {
		t.Error("the new password does not sign in")
	}
}

// The whole reason this is one transaction: a teacher's classes resolve through
// `staff WHERE email = <signed-in email>`. Move the login without the staff row
// and she signs in to an empty timetable, with no error anywhere.
func TestChangingTheSignInEmailMovesTheStaffRowToo(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	userID, oldEmail := makeThrowawayStaff(t, db, "moveemail")
	var staffBefore int
	db.QueryRow(`SELECT COUNT(*) FROM staff WHERE email=? AND deleted_at IS NULL`, oldEmail).Scan(&staffBefore)

	const personal = "rose.personal@example.com"
	w := doRequest(r, "PUT", "/api/users/"+userID+"/credentials", token,
		map[string]string{"email": personal, "password": "another-strong-password"})
	if w.Code != http.StatusOK {
		t.Fatalf("change email: %d %s", w.Code, w.Body.String())
	}

	var staffAtNew, staffAtOld int
	db.QueryRow(`SELECT COUNT(*) FROM staff WHERE email=? AND deleted_at IS NULL`, personal).Scan(&staffAtNew)
	db.QueryRow(`SELECT COUNT(*) FROM staff WHERE email=? AND deleted_at IS NULL`, oldEmail).Scan(&staffAtOld)
	if staffAtNew != staffBefore || staffAtOld != 0 {
		t.Errorf("staff row did not move: %d at the new address, %d left at the old — she would sign in to an empty timetable", staffAtNew, staffAtOld)
	}
	if tok := getToken(t, r, personal, "another-strong-password"); tok == "" {
		t.Error("cannot sign in at the new address")
	}
}

func TestCredentialsEndpointRejectsWeakAndMalformedInput(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	userID, _ := makeThrowawayStaff(t, db, "validation")
	cases := []struct {
		name string
		body map[string]string
	}{
		{"short password", map[string]string{"password": "short"}},
		{"malformed email", map[string]string{"email": "not-an-email"}},
		{"nothing at all", map[string]string{}},
	}
	for _, tc := range cases {
		if w := doRequest(r, "PUT", "/api/users/"+userID+"/credentials", token, tc.body); w.Code != http.StatusBadRequest {
			t.Errorf("%s returned %d, want 400", tc.name, w.Code)
		}
	}
}

// A parent must not be able to reach this at all.
func TestCredentialsEndpointIsAdminOnly(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	parentToken := getParentToken(t, r)

	w := doRequest(r, "PUT", "/api/users/1/credentials", parentToken, map[string]string{"password": "a-strong-password"})
	if w.Code != http.StatusForbidden {
		t.Errorf("a parent got %d, want 403", w.Code)
	}
}
