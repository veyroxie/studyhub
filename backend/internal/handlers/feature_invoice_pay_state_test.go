package handlers

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/models"
	"studyhub/internal/store"
)

func invoiceStatus(t *testing.T, db *store.DB, id string) string {
	t.Helper()
	var status string
	if err := db.QueryRow(`SELECT status FROM invoices WHERE id=?`, id).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	return status
}

func issuedInvoice(t *testing.T, r *chi.Mux, admin string) string {
	t.Helper()
	id := createInvoiceWithLines(t, r, admin, []models.InvoiceLineItem{baseLine()})
	if w := doRequest(r, "POST", "/api/invoices/"+id+"/issue", admin, nil); w.Code != http.StatusOK {
		t.Fatalf("issue: %d %s", w.Code, w.Body.String())
	}
	return id
}

var cashPaid = map[string]string{"status": "Paid", "paymentMethod": "Cash"}

// A draft is still being reviewed; taking money against it skips issuing it.
func TestADraftInvoiceCannotBePaid(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	admin := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	id := createInvoiceWithLines(t, r, admin, []models.InvoiceLineItem{baseLine()})
	if w := doRequest(r, "PUT", "/api/invoices/"+id+"/pay", admin, cashPaid); w.Code != http.StatusConflict {
		t.Fatalf("paying a draft: got %d, want 409", w.Code)
	}
	if got := invoiceStatus(t, db, id); got != models.InvoiceStatusDraft {
		t.Fatalf("status %q, want it left Draft", got)
	}
}

// A voided invoice has been replaced; paying it pays a document nobody owes.
func TestAVoidedInvoiceCannotBePaid(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	admin := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	id := issuedInvoice(t, r, admin)
	if w := doRequest(r, "POST", "/api/invoices/"+id+"/reissue", admin, map[string]any{}); w.Code != http.StatusOK {
		t.Fatalf("reissue: %d", w.Code)
	}
	if w := doRequest(r, "PUT", "/api/invoices/"+id+"/pay", admin, cashPaid); w.Code != http.StatusConflict {
		t.Fatalf("paying a void: got %d, want 409", w.Code)
	}
	if got := invoiceStatus(t, db, id); got != models.InvoiceStatusVoid {
		t.Fatalf("status %q, want it left Void", got)
	}
}

// A parent's "I've paid" on a confirmed payment would unconfirm it, keeping the receipt number on an unpaid row.
func TestAParentCannotUnconfirmAPaidInvoice(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	admin := getAdminToken(t, r)
	parent := getParentToken(t, r)
	db := store.InitDB(testDSN())

	id := issuedInvoice(t, r, admin)
	if w := doRequest(r, "PUT", "/api/invoices/"+id+"/pay", admin, cashPaid); w.Code != http.StatusOK {
		t.Fatalf("admin pay: %d %s", w.Code, w.Body.String())
	}
	claim := map[string]string{"status": "Pending Verification", "paymentMethod": "Cash"}
	if w := doRequest(r, "PUT", "/api/invoices/"+id+"/pay", parent, claim); w.Code != http.StatusConflict {
		t.Fatalf("parent re-claim on a paid invoice: got %d, want 409", w.Code)
	}
	if got := invoiceStatus(t, db, id); got != models.InvoiceStatusPaid {
		t.Fatalf("status %q, want it left Paid", got)
	}
}

// Paying twice stays the harmless no-op it has always been.
func TestPayingAPaidInvoiceAgainIsANoOp(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	admin := getAdminToken(t, r)

	id := issuedInvoice(t, r, admin)
	for i := 0; i < 2; i++ {
		if w := doRequest(r, "PUT", "/api/invoices/"+id+"/pay", admin, cashPaid); w.Code != http.StatusOK {
			t.Fatalf("pay #%d: got %d %s", i+1, w.Code, w.Body.String())
		}
	}
}
