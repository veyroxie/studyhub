package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/models"
	"studyhub/internal/store"
)

// The seeded parent (seeduser27@example.com) has two children, STU001 and STU002.
const (
	billParent = "seeduser27@example.com"
	billPeriod = "2026-10"
)

func monthlyInvoice(t *testing.T, r *chi.Mux, admin, studentID string, price float64, status string) string {
	t.Helper()
	inv := models.Invoice{
		StudentID: studentID, Description: "Oct tuition", Type: "Monthly", Status: status,
		DueDate: "2026-10-07", CreatedOn: "2026-10-01",
		LineItems: []models.InvoiceLineItem{{Kind: models.LineItemKindItem, Name: "Level 3 & 4", Qty: 1, UnitPrice: price, Amount: price}},
	}
	w := doRequest(r, "POST", "/api/invoices", admin, inv)
	if w.Code != http.StatusOK {
		t.Fatalf("create invoice for %s: %d %s", studentID, w.Code, w.Body.String())
	}
	var created models.Invoice
	json.NewDecoder(w.Body).Decode(&created)
	return created.ID
}

type billRow struct {
	status, proof, receipt, method string
	byParent                       bool
}

func readBillRow(t *testing.T, db *store.DB, id string) billRow {
	t.Helper()
	var b billRow
	if err := db.QueryRow(`SELECT status, COALESCE(payment_proof,''), COALESCE(receipt_no,''), COALESCE(payment_method,''), COALESCE(submitted_by_parent,false) FROM invoices WHERE id=?`, id).
		Scan(&b.status, &b.proof, &b.receipt, &b.method, &b.byParent); err != nil {
		t.Fatalf("read invoice %s: %v", id, err)
	}
	return b
}

type billFixture struct {
	r             *chi.Mux
	db            *store.DB
	admin, parent string
	zayden, lucy  string
	cleanup       func()
}

func newBillFixture(t *testing.T) billFixture {
	t.Helper()
	r, cleanup := setupTestApp(t)
	f := billFixture{r: r, cleanup: cleanup, db: store.InitDB(testDSN())}
	f.admin = getAdminToken(t, r)
	f.parent = getParentToken(t, r)
	f.zayden = monthlyInvoice(t, r, f.admin, "STU001", 240, "")
	f.lucy = monthlyInvoice(t, r, f.admin, "STU002", 260, "")
	return f
}

func (f billFixture) pay(token string, body map[string]any) int {
	if _, ok := body["period"]; !ok {
		body["period"] = billPeriod
	}
	return doRequest(f.r, "POST", "/api/family-bills/pay", token, body).Code
}

func (f billFixture) parentClaim() map[string]any {
	return map[string]any{"invoiceIds": []string{f.zayden, f.lucy}, "expectedTotal": 500,
		"status": "Pending Verification", "paymentMethod": "Bank Transfer", "referenceNo": "MBB123",
		"paymentProof": "uploads/proof_" + f.zayden + "_1700000000.png"}
}

func TestAParentClaimsTheWholeFamilyBillInOneGo(t *testing.T) {
	f := newBillFixture(t)
	defer f.cleanup()

	if code := f.pay(f.parent, f.parentClaim()); code != http.StatusOK {
		t.Fatalf("claim: got %d", code)
	}
	for _, id := range []string{f.zayden, f.lucy} {
		row := readBillRow(t, f.db, id)
		if row.status != models.InvoiceStatusPendingVerification || !row.byParent {
			t.Errorf("%s: status %q byParent %v, want Pending Verification by the parent", id, row.status, row.byParent)
		}
		if row.proof != "uploads/proof_"+f.zayden+"_1700000000.png" {
			t.Errorf("%s: proof %q, want the one shared upload", id, row.proof)
		}
	}
}

func TestABillThatChangedSinceItWasShownIsRefusedWhole(t *testing.T) {
	f := newBillFixture(t)
	defer f.cleanup()

	missingChild := f.parentClaim()
	missingChild["invoiceIds"] = []string{f.zayden}
	missingChild["expectedTotal"] = 240
	if code := f.pay(f.parent, missingChild); code != http.StatusConflict {
		t.Fatalf("claim missing a child: got %d, want 409", code)
	}
	wrongTotal := f.parentClaim()
	wrongTotal["expectedTotal"] = 480
	if code := f.pay(f.parent, wrongTotal); code != http.StatusConflict {
		t.Fatalf("claim at a stale total: got %d, want 409", code)
	}
	for _, id := range []string{f.zayden, f.lucy} {
		if got := readBillRow(t, f.db, id).status; got != models.InvoiceStatusUnpaid {
			t.Errorf("%s: status %q after a refused claim, want Unpaid", id, got)
		}
	}
}

func TestConfirmingTheBillPaysEveryChildWithItsOwnReceipt(t *testing.T) {
	f := newBillFixture(t)
	defer f.cleanup()
	if code := f.pay(f.parent, f.parentClaim()); code != http.StatusOK {
		t.Fatalf("claim: %d", code)
	}

	confirm := map[string]any{"parentEmail": billParent, "invoiceIds": []string{f.zayden, f.lucy}, "expectedTotal": 500, "status": "Paid"}
	if code := f.pay(f.admin, confirm); code != http.StatusOK {
		t.Fatalf("confirm: got %d", code)
	}
	z, l := readBillRow(t, f.db, f.zayden), readBillRow(t, f.db, f.lucy)
	if z.status != models.InvoiceStatusPaid || l.status != models.InvoiceStatusPaid {
		t.Fatalf("statuses %q/%q, want both Paid", z.status, l.status)
	}
	if z.receipt == "" || l.receipt == "" || z.receipt == l.receipt {
		t.Fatalf("receipts %q/%q, want one each", z.receipt, l.receipt)
	}
	if z.method != "Bank Transfer" {
		t.Errorf("method %q, want the parent's Bank Transfer kept", z.method)
	}
}

func TestRejectingTheBillReopensOnlyTheClaimedInvoices(t *testing.T) {
	f := newBillFixture(t)
	defer f.cleanup()
	if code := f.pay(f.parent, f.parentClaim()); code != http.StatusOK {
		t.Fatalf("claim: %d", code)
	}

	reject := map[string]any{"parentEmail": billParent, "invoiceIds": []string{f.zayden, f.lucy}, "expectedTotal": 500, "status": "Unpaid"}
	if code := f.pay(f.admin, reject); code != http.StatusOK {
		t.Fatalf("reject: got %d", code)
	}
	for _, id := range []string{f.zayden, f.lucy} {
		if got := readBillRow(t, f.db, id).status; got != models.InvoiceStatusUnpaid {
			t.Errorf("%s: status %q, want Unpaid", id, got)
		}
	}
}

// A parent always pays their own bill: naming another family changes nothing.
func TestAParentCannotPayAnotherFamilysInvoices(t *testing.T) {
	f := newBillFixture(t)
	defer f.cleanup()
	other := monthlyInvoice(t, f.r, f.admin, "STU003", 240, "")

	claim := f.parentClaim()
	claim["invoiceIds"] = []string{f.zayden, f.lucy, other}
	claim["expectedTotal"] = 740
	claim["parentEmail"] = "someone-else@example.com"
	if code := f.pay(f.parent, claim); code != http.StatusConflict {
		t.Fatalf("claim including another family's invoice: got %d, want 409", code)
	}
	if got := readBillRow(t, f.db, other).status; got != models.InvoiceStatusUnpaid {
		t.Fatalf("other family's invoice moved to %q", got)
	}
}

// A draft is not part of the bill yet, so the parent pays only what was issued.
func TestADraftSiblingIsNotPartOfTheBill(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	admin, parent := getAdminToken(t, r), getParentToken(t, r)
	issued := monthlyInvoice(t, r, admin, "STU001", 240, "")
	draft := monthlyInvoice(t, r, admin, "STU002", 260, models.InvoiceStatusDraft)

	claim := map[string]any{"period": billPeriod, "invoiceIds": []string{issued}, "expectedTotal": 240,
		"status": "Pending Verification", "paymentMethod": "Cash"}
	if w := doRequest(r, "POST", "/api/family-bills/pay", parent, claim); w.Code != http.StatusOK {
		t.Fatalf("claim: got %d %s", w.Code, w.Body.String())
	}
	if got := readBillRow(t, db, draft).status; got != models.InvoiceStatusDraft {
		t.Fatalf("draft moved to %q", got)
	}
}

func TestAProofFromOutsideTheBillIsRefused(t *testing.T) {
	f := newBillFixture(t)
	defer f.cleanup()
	claim := f.parentClaim()
	claim["paymentProof"] = "uploads/proof_INV_someone_else_1700000000.png"
	if code := f.pay(f.parent, claim); code != http.StatusBadRequest {
		t.Fatalf("foreign proof: got %d, want 400", code)
	}
}

func TestABankTransferClaimNeedsAReference(t *testing.T) {
	f := newBillFixture(t)
	defer f.cleanup()
	claim := f.parentClaim()
	delete(claim, "referenceNo")
	if code := f.pay(f.parent, claim); code != http.StatusBadRequest {
		t.Fatalf("no reference: got %d, want 400", code)
	}
}

func TestTheFamilyBillPDFIsTheParentsAndOnlyTheirs(t *testing.T) {
	f := newBillFixture(t)
	defer f.cleanup()

	w := doRequest(f.r, "GET", "/api/family-bills/"+f.zayden+"/pdf", f.parent, nil)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("parent bill PDF: got %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	if w := doRequest(f.r, "GET", "/api/family-bills/"+f.lucy+"/pdf", f.admin, nil); w.Code != http.StatusOK {
		t.Fatalf("admin bill PDF: got %d", w.Code)
	}
	other := monthlyInvoice(t, f.r, f.admin, "STU003", 240, "")
	if w := doRequest(f.r, "GET", "/api/family-bills/"+other+"/pdf", f.parent, nil); w.Code != http.StatusForbidden {
		t.Fatalf("another family's bill: got %d, want 403", w.Code)
	}
}

func TestTheFamilyReceiptWaitsUntilEveryChildIsPaid(t *testing.T) {
	f := newBillFixture(t)
	defer f.cleanup()
	if w := doRequest(f.r, "GET", "/api/family-bills/"+f.zayden+"/receipt.pdf", f.parent, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("receipt for an unpaid bill: got %d, want 400", w.Code)
	}
	confirm := map[string]any{"parentEmail": billParent, "invoiceIds": []string{f.zayden, f.lucy}, "expectedTotal": 500,
		"status": "Paid", "paymentMethod": "Cash"}
	if code := f.pay(f.admin, confirm); code != http.StatusOK {
		t.Fatalf("mark bill paid: %d", code)
	}
	if w := doRequest(f.r, "GET", "/api/family-bills/"+f.zayden+"/receipt.pdf", f.parent, nil); w.Code != http.StatusOK {
		t.Fatalf("receipt for a paid bill: got %d", w.Code)
	}
}

func TestADraftHasNoFamilyBillPDFForTheParent(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	admin, parent := getAdminToken(t, r), getParentToken(t, r)
	draft := monthlyInvoice(t, r, admin, "STU001", 240, models.InvoiceStatusDraft)
	if w := doRequest(r, "GET", "/api/family-bills/"+draft+"/pdf", parent, nil); w.Code != http.StatusNotFound {
		t.Fatalf("draft bill PDF: got %d, want 404", w.Code)
	}
}
