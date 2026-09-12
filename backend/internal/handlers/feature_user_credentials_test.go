package handlers

import (
	"net/http"
	"testing"

	"studyhub/internal/store"
)

// The gap this closes: reset needs an emailed token, outbound mail is
// restricted (ADR-015), so an admin had no way to give a teacher working
// credentials short of deleting the account.
func TestAdminCanSetAUserPasswordAndSignInWithIt(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	var userID string
	if err := db.QueryRow(`SELECT id::text FROM users WHERE email=?`, "rose@studyhub.com").Scan(&userID); err != nil {
		t.Skipf("no seeded rose account here: %v", err)
	}

	w := doRequest(r, "PUT", "/api/users/"+userID+"/credentials", token,
		map[string]string{"password": "a-new-strong-password"})
	if w.Code != http.StatusOK {
		t.Fatalf("set password: %d %s", w.Code, w.Body.String())
	}
	if tok := getToken(t, r, "rose@studyhub.com", "a-new-strong-password"); tok == "" {
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

	var userID string
	if err := db.QueryRow(`SELECT id::text FROM users WHERE email=?`, "rose@studyhub.com").Scan(&userID); err != nil {
		t.Skipf("no seeded rose account here: %v", err)
	}
	var staffBefore int
	db.QueryRow(`SELECT COUNT(*) FROM staff WHERE email=? AND deleted_at IS NULL`, "rose@studyhub.com").Scan(&staffBefore)
	if staffBefore == 0 {
		t.Skip("no staff row for rose in this fixture")
	}

	const personal = "rose.personal@example.com"
	w := doRequest(r, "PUT", "/api/users/"+userID+"/credentials", token,
		map[string]string{"email": personal, "password": "another-strong-password"})
	if w.Code != http.StatusOK {
		t.Fatalf("change email: %d %s", w.Code, w.Body.String())
	}

	var staffAtNew, staffAtOld int
	db.QueryRow(`SELECT COUNT(*) FROM staff WHERE email=? AND deleted_at IS NULL`, personal).Scan(&staffAtNew)
	db.QueryRow(`SELECT COUNT(*) FROM staff WHERE email=? AND deleted_at IS NULL`, "rose@studyhub.com").Scan(&staffAtOld)
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

	var userID string
	if err := db.QueryRow(`SELECT id::text FROM users WHERE email=?`, "rose@studyhub.com").Scan(&userID); err != nil {
		t.Skipf("no seeded rose account here: %v", err)
	}
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
