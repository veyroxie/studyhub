package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/models"
	"studyhub/internal/store"
)

// The snapshot cache outlives a test and the test router skips its invalidating middleware.
func parentSnapshot(t *testing.T, r *chi.Mux, token string) models.Snapshot {
	t.Helper()
	store.SnapshotCacheInvalidateAll()
	var snap models.Snapshot
	json.NewDecoder(doRequest(r, "GET", "/api/snapshot", token, nil).Body).Decode(&snap)
	return snap
}

// The monthly run drafts every invoice on the 1st for Nadine to review. A draft
// is not yet a bill: until she issues it the parent must not see the figure.
func TestAParentDoesNotSeeADraftInvoice(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	admin := getAdminToken(t, r)
	parent := getParentToken(t, r)

	draftID := createInvoiceWithLines(t, r, admin, []models.InvoiceLineItem{baseLine()})

	snap := parentSnapshot(t, r, parent)
	for _, inv := range snap.Invoices {
		if inv.ID == draftID {
			t.Fatal("snapshot handed the parent an unissued draft")
		}
	}

	var listed []models.Invoice
	json.NewDecoder(doRequest(r, "GET", "/api/invoices", parent, nil).Body).Decode(&listed)
	for _, inv := range listed {
		if inv.ID == draftID {
			t.Fatal("GET /api/invoices handed the parent an unissued draft")
		}
	}

	if w := doRequest(r, "GET", "/api/invoices/"+draftID+"/pdf", parent, nil); w.Code != http.StatusNotFound {
		t.Fatalf("draft PDF for a parent: got %d, want 404", w.Code)
	}
}

// A reissue voids the old invoice. The parent should see only the replacement,
// not two bills for the same month.
func TestAParentDoesNotSeeAVoidedInvoice(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	admin := getAdminToken(t, r)
	parent := getParentToken(t, r)

	id := createInvoiceWithLines(t, r, admin, []models.InvoiceLineItem{baseLine()})
	if w := doRequest(r, "POST", "/api/invoices/"+id+"/issue", admin, nil); w.Code != http.StatusOK {
		t.Fatalf("issue: %d %s", w.Code, w.Body.String())
	}
	w := doRequest(r, "POST", "/api/invoices/"+id+"/reissue", admin, map[string]any{})
	if w.Code != http.StatusOK {
		t.Fatalf("reissue: %d %s", w.Code, w.Body.String())
	}
	var reissued struct {
		ID string `json:"id"`
	}
	json.NewDecoder(w.Body).Decode(&reissued)

	snap := parentSnapshot(t, r, parent)
	sawReplacement := false
	for _, inv := range snap.Invoices {
		if inv.ID == id {
			t.Fatal("snapshot handed the parent a voided invoice")
		}
		sawReplacement = sawReplacement || inv.ID == reissued.ID
	}
	if !sawReplacement {
		t.Fatalf("replacement %q missing from the parent's snapshot", reissued.ID)
	}
}
