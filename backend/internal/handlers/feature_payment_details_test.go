package handlers

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Online payment is offered only when the gateway can also confirm the payment.
func TestOnlinePaymentNeedsTheWebhookSecretToo(t *testing.T) {
	t.Setenv("PAYMENT_PROVIDER", "billplz")
	t.Setenv("BILLPLZ_API_KEY", "k")
	t.Setenv("BILLPLZ_COLLECTION_ID", "c")
	t.Setenv("BILLPLZ_X_SIGNATURE", "")
	if onlinePaymentReady() {
		t.Fatal("offered online payment with no webhook secret: parents would pay and stay unpaid")
	}
	t.Setenv("BILLPLZ_X_SIGNATURE", "s")
	if !onlinePaymentReady() {
		t.Fatal("fully configured gateway not offered")
	}
}

func TestParentsGetBankDetailsAndTeachersDoNot(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	parent := doRequest(r, "GET", "/api/payments/details", getParentToken(t, r), nil)
	var body map[string]any
	json.NewDecoder(parent.Body).Decode(&body)
	if parent.Code != http.StatusOK || body["onlinePayment"] == nil {
		t.Fatalf("parent: %d %v", parent.Code, body)
	}
	if w := doRequest(r, "GET", "/api/payments/details", getTeacherToken(t, r), nil); w.Code != http.StatusForbidden {
		t.Fatalf("teacher: got %d, want 403", w.Code)
	}
}
