package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/store"
)

// getTokenAllowFail is getToken without the t.Fatalf: used to assert a
// credential NO LONGER works, where a failed login is the expected result.
func getTokenAllowFail(t *testing.T, r *chi.Mux, email, password string) string {
	t.Helper()
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		return ""
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "sh_token" {
			return c.Value
		}
	}
	return ""
}

// A password an admin chose is temporary by construction: the holder can reach
// the setup endpoint and nothing else until they replace it. Enforced on the
// server, because a forced-setup screen the client draws is only a suggestion.
func TestAdminIssuedPasswordCanDoNothingButReplaceItself(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	adminToken := getAdminToken(t, r)
	db := store.InitDB(testDSN())
	id, email := makeThrowawayStaff(t, db, "forcedsetup")

	const temp = "temporary-password-1"
	if w := doRequest(r, "PUT", "/api/users/"+id+"/credentials", adminToken,
		map[string]string{"password": temp}); w.Code != http.StatusOK {
		t.Fatalf("admin set password: %d %s", w.Code, w.Body.String())
	}

	roseToken := getToken(t, r, email, temp)
	if roseToken == "" {
		t.Fatal("the temporary password does not sign in")
	}
	// Any ordinary request is refused until setup is done.
	if w := doRequest(r, "GET", "/api/students", roseToken, nil); w.Code != http.StatusPreconditionRequired {
		t.Errorf("a forced-setup session reached /api/students with %d, want 428", w.Code)
	}

	const chosen = "her-own-chosen-password"
	const personal = "rose.owns.this@example.com"
	if w := doRequest(r, "POST", "/api/auth/complete-setup", roseToken,
		map[string]string{"email": personal, "password": chosen}); w.Code != http.StatusOK {
		t.Fatalf("complete setup: %d %s", w.Code, w.Body.String())
	}

	// The staff row moved with the login, or she signs in to an empty timetable.
	var staffAtNew int
	db.QueryRow(`SELECT COUNT(*) FROM staff WHERE email=? AND deleted_at IS NULL`, personal).Scan(&staffAtNew)
	if staffAtNew == 0 {
		t.Error("staff row did not follow the new email")
	}

	// The temporary password is dead and the chosen one works.
	if tok := getTokenAllowFail(t, r, email, temp); tok != "" {
		t.Error("the temporary password still signs in after setup")
	}
	newToken := getToken(t, r, personal, chosen)
	if newToken == "" {
		t.Fatal("cannot sign in with the chosen credentials")
	}
	if w := doRequest(r, "GET", "/api/students", newToken, nil); w.Code == http.StatusPreconditionRequired {
		t.Error("still gated after completing setup")
	}
}

// Setup is a one-time act, not a second password-change endpoint.
func TestCompleteSetupRefusedWhenNotInSetup(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	adminToken := getAdminToken(t, r)

	if w := doRequest(r, "POST", "/api/auth/complete-setup", adminToken,
		map[string]string{"password": "some-other-password"}); w.Code != http.StatusConflict {
		t.Errorf("an already-set-up account got %d, want 409", w.Code)
	}
}

func TestCompleteSetupRejectsAWeakPassword(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	adminToken := getAdminToken(t, r)
	db := store.InitDB(testDSN())
	id, email := makeThrowawayStaff(t, db, "weakpw")

	const temp = "temporary-password-2"
	doRequest(r, "PUT", "/api/users/"+id+"/credentials", adminToken, map[string]string{"password": temp})
	tok := getToken(t, r, email, temp)
	if tok == "" {
		t.Fatal("temporary password does not sign in")
	}
	if w := doRequest(r, "POST", "/api/auth/complete-setup", tok, map[string]string{"password": "short"}); w.Code != http.StatusBadRequest {
		t.Errorf("a weak password was accepted at setup: %d", w.Code)
	}
}

// Onboarding is one step: creating the account forces the first-sign-in setup,
// rather than needing a second call to set must_change_credentials.
func TestAdminCreatedAccountMustSetItsOwnCredentials(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	adminToken := getAdminToken(t, r)

	const email = "brand.new.teacher@example.com"
	const handover = "handover-password-1"
	w := doRequest(r, "POST", "/api/users", adminToken, map[string]string{
		"email": email, "password": handover, "role": "teacher", "name": "Brand New",
	})
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("create user: %d %s", w.Code, w.Body.String())
	}

	tok := getToken(t, r, email, handover)
	if tok == "" {
		t.Fatal("the handover password does not sign in")
	}
	if w := doRequest(r, "GET", "/api/students", tok, nil); w.Code != http.StatusPreconditionRequired {
		t.Errorf("a freshly created account reached /api/students with %d, want 428 — onboarding did not force setup", w.Code)
	}
	if w := doRequest(r, "POST", "/api/auth/complete-setup", tok,
		map[string]string{"password": "the-password-they-chose"}); w.Code != http.StatusOK {
		t.Fatalf("complete setup: %d %s", w.Code, w.Body.String())
	}
	if tok2 := getToken(t, r, email, "the-password-they-chose"); tok2 == "" {
		t.Error("cannot sign in with the chosen password")
	}
}
