package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"studyhub/internal/core"
	"studyhub/internal/store"
)

// The parent path end to end, without email: an admin mints a link, the parent
// follows it, sets their own password and signs in. In production 51 parent
// accounts have a set-password token that was emailed into a blocked
// allowlist, so none has ever been usable.
func TestInviteLinkLetsAParentSetTheirOwnPassword(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	// A parent account as the importer leaves them: random unusable password.
	email := "invited-" + core.GenerateID("t") + "@example.com"
	var userID string
	if err := db.QueryRow(`INSERT INTO users(tenant_id,email,password_hash,role,name,status) VALUES(?,?,?,?,?,?) RETURNING id::text`,
		1, email, "$argon2id$unusable", "parent", "Invited Parent", "pending_verification").Scan(&userID); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id::text=?`, userID) })

	w := doRequest(r, "POST", "/api/users/"+userID+"/invite-link", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("invite link: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Email, Link, ExpiresAt string
	}
	json.NewDecoder(w.Body).Decode(&out)
	if out.Email != email {
		t.Errorf("link issued for %q, want %q", out.Email, email)
	}
	if !strings.Contains(out.Link, "/set-password.html?token=") {
		t.Fatalf("link does not point at the set-password page: %q", out.Link)
	}
	if out.ExpiresAt == "" {
		t.Error("no expiry reported, so the admin cannot tell someone how long they have")
	}

	tokenValue := out.Link[strings.Index(out.Link, "token=")+len("token="):]
	sw := doRequest(r, "POST", "/api/set-password", "", map[string]string{
		"token": tokenValue, "newPassword": "a-password-they-chose",
	})
	if sw.Code != http.StatusOK {
		t.Fatalf("set password via the link: %d %s", sw.Code, sw.Body.String())
	}

	if tok := getToken(t, r, email, "a-password-they-chose"); tok == "" {
		t.Error("the parent cannot sign in after using their own invite link")
	}
	var status string
	db.QueryRow(`SELECT status FROM users WHERE id::text=?`, userID).Scan(&status)
	if status != "active" {
		t.Errorf("account is %q after setup, want active", status)
	}
}

// Issuing a second invitation kills the first. Two live links for one account
// means the older still works after the newer has been used, and nobody can
// say which was handed to whom.
func TestANewInviteLinkInvalidatesTheOldOne(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	email := "reinvited-" + core.GenerateID("t") + "@example.com"
	var userID string
	if err := db.QueryRow(`INSERT INTO users(tenant_id,email,password_hash,role,name,status) VALUES(?,?,?,?,?,?) RETURNING id::text`,
		1, email, "$argon2id$unusable", "parent", "Reinvited", "pending_verification").Scan(&userID); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id::text=?`, userID) })

	linkOf := func() string {
		w := doRequest(r, "POST", "/api/users/"+userID+"/invite-link", token, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("invite link: %d %s", w.Code, w.Body.String())
		}
		var out struct{ Link string }
		json.NewDecoder(w.Body).Decode(&out)
		return out.Link[strings.Index(out.Link, "token=")+len("token="):]
	}
	first := linkOf()
	second := linkOf()
	if first == second {
		t.Fatal("the second invitation reused the first token")
	}

	if w := doRequest(r, "POST", "/api/set-password", "", map[string]string{
		"token": first, "newPassword": "using-the-stale-link"}); w.Code == http.StatusOK {
		t.Error("the superseded link still worked")
	}
	if w := doRequest(r, "POST", "/api/set-password", "", map[string]string{
		"token": second, "newPassword": "using-the-current-link"}); w.Code != http.StatusOK {
		t.Errorf("the current link did not work: %d %s", w.Code, w.Body.String())
	}
}

func TestInviteLinkIsAdminOnly(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	parentToken := getParentToken(t, r)
	if w := doRequest(r, "POST", "/api/users/1/invite-link", parentToken, nil); w.Code != http.StatusForbidden {
		t.Errorf("a parent got %d, want 403", w.Code)
	}
}
