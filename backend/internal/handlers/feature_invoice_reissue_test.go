package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"studyhub/internal/models"
	"studyhub/internal/store"
)

func readInvoiceRow(t *testing.T, db *store.DB, id string) (status, voidedAt, supersededBy, number string, amount float64) {
	t.Helper()
	if err := db.QueryRow(`SELECT status, COALESCE(voided_at,''), COALESCE(superseded_by,''), COALESCE(invoice_no,''), amount
		FROM invoices WHERE id=?`, id).Scan(&status, &voidedAt, &supersededBy, &number, &amount); err != nil {
		t.Fatalf("read invoice %s: %v", id, err)
	}
	return
}

// The correction mechanism from ADR-016: the original keeps its number and its
// figures, and the pair reads as one correction rather than two loose rows.
func TestReissueVoidsTheOriginalAndLinksTheReplacement(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	original := createdInvoice(t, r, token, "STU001", "2026-09-01")

	w := doRequest(r, "POST", "/api/invoices/"+original.ID+"/reissue", token,
		map[string]any{"amount": 240, "description": "Corrected"})
	if w.Code != http.StatusOK {
		t.Fatalf("reissue: %d %s", w.Code, w.Body.String())
	}
	var replacement models.Invoice
	json.NewDecoder(w.Body).Decode(&replacement)

	status, voidedAt, supersededBy, number, amount := readInvoiceRow(t, db, original.ID)
	if status != models.InvoiceStatusVoid {
		t.Errorf("original status %q, want Void", status)
	}
	if voidedAt == "" {
		t.Error("voided invoice has no voided_at")
	}
	if supersededBy != replacement.ID {
		t.Errorf("superseded_by %q, want the replacement %q — the pair cannot be read as one correction", supersededBy, replacement.ID)
	}
	if number != original.InvoiceNo {
		t.Errorf("voiding changed the original's number from %q to %q", original.InvoiceNo, number)
	}
	if amount != original.Amount {
		t.Errorf("voiding changed the original's amount from %.2f to %.2f — an issued document must keep its figures", original.Amount, amount)
	}

	newStatus, _, _, newNumber, newAmount := readInvoiceRow(t, db, replacement.ID)
	if newStatus != models.InvoiceStatusUnpaid {
		t.Errorf("replacement status %q, want Unpaid", newStatus)
	}
	if newNumber == "" || newNumber == original.InvoiceNo {
		t.Errorf("replacement number %q must be new and not reuse %q", newNumber, original.InvoiceNo)
	}
	if newAmount != 240 {
		t.Errorf("replacement amount %.2f, want the corrected 240", newAmount)
	}
}

// A paid invoice is refused: voiding it strands the payment and the receipt
// already in the parent's hands against a cancelled document (ADR-016).
func TestReissueRefusesAPaidInvoice(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	inv := createdInvoice(t, r, token, "STU001", "2026-09-01")
	if w := doRequest(r, "PUT", "/api/invoices/"+inv.ID+"/pay", token, nil); w.Code != http.StatusOK {
		t.Fatalf("mark paid: %d", w.Code)
	}

	w := doRequest(r, "POST", "/api/invoices/"+inv.ID+"/reissue", token, nil)
	if w.Code != http.StatusConflict {
		t.Errorf("reissuing a paid invoice returned %d, want 409", w.Code)
	}
	status, _, _, _, _ := readInvoiceRow(t, db, inv.ID)
	if status != models.InvoiceStatusPaid {
		t.Errorf("a refused reissue still changed the invoice to %q", status)
	}
}

// 0039 stops a student being billed twice for a month. A voided invoice used to
// keep occupying that slot, so the reissue would have been refused by the index
// that exists to prevent double billing.
func TestReissueOfAMonthlyInvoiceIsNotBlockedByTheMonthlyUniqueIndex(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())
	db.Exec(`DELETE FROM invoices WHERE student_id=?`, "STU001")

	monthly := models.Invoice{StudentID: "STU001", Description: "Sept", Type: "Monthly",
		Amount: 230, DueDate: "2026-09-07", CreatedOn: "2026-09-01"}
	cw := doRequest(r, "POST", "/api/invoices", token, monthly)
	if cw.Code != http.StatusOK {
		t.Fatalf("create monthly: %d %s", cw.Code, cw.Body.String())
	}
	var created models.Invoice
	json.NewDecoder(cw.Body).Decode(&created)

	w := doRequest(r, "POST", "/api/invoices/"+created.ID+"/reissue", token, map[string]any{"amount": 240})
	if w.Code != http.StatusOK {
		t.Fatalf("reissuing a Monthly invoice was refused: %d %s", w.Code, w.Body.String())
	}
	var replacement models.Invoice
	json.NewDecoder(w.Body).Decode(&replacement)

	// Exactly one live Monthly invoice for that period afterwards.
	var live int
	db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE student_id=? AND type='Monthly' AND period='2026-09' AND deleted_at IS NULL AND status<>'Void'`,
		"STU001").Scan(&live)
	if live != 1 {
		t.Errorf("%d live monthly invoices for the period, want 1", live)
	}
}

// An issued invoice is frozen, not just a paid one (ADR-016). Repricing an
// unpaid one changes what a parent was asked for with no record that it moved.
func TestIssuedInvoiceCannotBeRepriced(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	inv := createdInvoice(t, r, token, "STU001", "2026-09-01")
	if inv.Status == models.InvoiceStatusPaid {
		t.Fatal("setup: this test is about an UNPAID issued invoice")
	}

	inv.Amount = 999
	w := doRequest(r, "PUT", "/api/invoices/"+inv.ID, token, inv)
	if w.Code != http.StatusConflict {
		t.Errorf("repricing an issued unpaid invoice returned %d, want 409", w.Code)
	}
	var amount float64
	db.QueryRow(`SELECT amount FROM invoices WHERE id=?`, inv.ID).Scan(&amount)
	if amount == 999 {
		t.Error("the amount changed anyway")
	}

	// The wording is still editable: the edit modal resubmits line items on
	// every save, so freezing that would block fixing a typo.
	inv.Amount = amount
	inv.Description = "Corrected wording"
	if w := doRequest(r, "PUT", "/api/invoices/"+inv.ID, token, inv); w.Code != http.StatusOK {
		t.Errorf("editing only the description returned %d, want 200: %s", w.Code, w.Body.String())
	}
}
