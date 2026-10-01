package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"studyhub/internal/core"
	"studyhub/internal/store"
)

const devEmail = "admin@studyhub.com"

func TestDeveloperScreensAreForTheDeveloperOnly(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	admin := getAdminToken(t, r)
	for _, path := range []string{"/api/dev/audit-logs", "/api/dev/failures", "/api/dev/health"} {
		t.Setenv("DEVELOPER_EMAILS", "")
		if code := doRequest(r, "GET", path, admin, nil).Code; code != http.StatusForbidden {
			t.Errorf("%s as an admin who is not the developer: %d, want 403", path, code)
		}
		if code := doRequest(r, "GET", path, getParentToken(t, r), nil).Code; code != http.StatusForbidden {
			t.Errorf("%s as a parent: %d, want 403", path, code)
		}
		t.Setenv("DEVELOPER_EMAILS", "someone@else.com, "+strings.ToUpper(devEmail))
		if code := doRequest(r, "GET", path, admin, nil).Code; code != http.StatusOK {
			t.Errorf("%s as the developer: %d, want 200", path, code)
		}
	}
}

func TestTheAuditLogFiltersBySearchActionAndDate(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	t.Setenv("DEVELOPER_EMAILS", devEmail)
	db := store.InitDB(testDSN())
	defer db.Close()
	core.LogAudit(db, 1, "nadine@studyhub.com", "invoice_paid", "invoice", "INV_DEVTEST_1", "cash 100%_off")
	core.LogAudit(db, 1, "chiying@studyhub.com", "email_changed", "user", "7", "from=a@b.com")
	admin := getAdminToken(t, r)

	read := func(query string) (int, []devAuditEntry) {
		w := doRequest(r, "GET", "/api/dev/audit-logs"+query, admin, nil)
		var out struct {
			Entries []devAuditEntry `json:"entries"`
		}
		json.NewDecoder(w.Body).Decode(&out)
		return w.Code, out.Entries
	}
	if _, rows := read("?q=INV_DEVTEST_1"); len(rows) != 1 || rows[0].Action != "invoice_paid" {
		t.Errorf("search by entity id: %+v", rows)
	}
	if _, rows := read("?q=100%25_off"); len(rows) != 1 {
		t.Errorf("percent and underscore are literal, got %d rows", len(rows))
	}
	if _, rows := read("?action=email_changed"); len(rows) == 0 || rows[0].ActorEmail != "chiying@studyhub.com" {
		t.Errorf("filter by action: %+v", rows)
	}
	if _, rows := read("?q=INV_DEVTEST_1&to=2000-01-01"); len(rows) != 0 {
		t.Errorf("a date range before the entry still returned %d rows", len(rows))
	}
	if code, _ := read("?from=yesterday"); code != http.StatusBadRequest {
		t.Errorf("bad date: %d, want 400", code)
	}
}

func TestHealthReportsConfigWithoutRevealingIt(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	t.Setenv("DEVELOPER_EMAILS", devEmail)
	t.Setenv("OUTBOUND_ALLOWLIST", "secret-inbox@example.com")
	t.Setenv("BILLPLZ_API_KEY", "sk-should-never-appear")
	w := doRequest(r, "GET", "/api/dev/health", getAdminToken(t, r), nil)
	body := w.Body.String()
	for _, leaked := range []string{"secret-inbox@example.com", "sk-should-never-appear"} {
		if strings.Contains(body, leaked) {
			t.Errorf("health leaked %q", leaked)
		}
	}
	var h struct {
		EmailOnlyTo int `json:"emailOnlyTo"`
		Migrations  struct {
			Count int `json:"count"`
		} `json:"migrations"`
	}
	json.Unmarshal([]byte(body), &h)
	if h.EmailOnlyTo != 1 || h.Migrations.Count == 0 {
		t.Errorf("health = %s", body)
	}
}
