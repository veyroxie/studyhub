package handlers

import (
	"net/http"
	"testing"
)

// A Paid invoice used to accept any status from staff, so it could be pushed back
// into "awaiting confirmation" with its receipt still attached.
func TestAPaidInvoiceOnlyLeavesPaidByAReversal(t *testing.T) {
	f := newBillFixture(t)
	defer f.cleanup()
	pay := func(body map[string]string) int {
		return doRequest(f.r, "PUT", "/api/invoices/"+f.zayden+"/pay", f.admin, body).Code
	}
	if code := pay(map[string]string{"status": "Paid", "paymentMethod": "Cash"}); code != http.StatusOK {
		t.Fatalf("mark paid: %d", code)
	}
	if code := pay(map[string]string{"status": "Pending Verification"}); code != http.StatusConflict {
		t.Errorf("paid -> pending verification: %d, want 409", code)
	}
	if code := pay(map[string]string{"status": "Overdue"}); code != http.StatusBadRequest {
		t.Errorf("setting overdue: %d, want 400", code)
	}
	if got := readBillRow(t, f.db, f.zayden).status; got != "Paid" {
		t.Fatalf("status after refused moves = %q, want Paid", got)
	}
	if code := pay(map[string]string{"status": "Unpaid", "note": "recorded on the wrong child"}); code != http.StatusOK {
		t.Errorf("reversal: %d, want 200", code)
	}
}
