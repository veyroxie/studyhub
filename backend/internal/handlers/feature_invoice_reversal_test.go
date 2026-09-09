package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/models"
	"studyhub/internal/store"
)

// payInvoice drives the real endpoint rather than writing the row, so the
// referral and audit side effects under test are the ones production runs.
func payInvoice(t *testing.T, r *chi.Mux, token, invoiceID, status string) {
	t.Helper()
	var body any
	if status != "" {
		body = map[string]string{"status": status}
	}
	w := doRequest(r, "PUT", "/api/invoices/"+invoiceID+"/pay", token, body)
	if w.Code != http.StatusOK {
		t.Fatalf("set invoice %s to %q: got %d: %s", invoiceID, status, w.Code, w.Body.String())
	}
}

func createMonthlyInvoice(t *testing.T, r *chi.Mux, token, studentID, createdOn string) string {
	t.Helper()
	inv := models.Invoice{
		StudentID: studentID, Description: "Monthly " + createdOn, Type: "Monthly",
		Amount: 100, DueDate: "2026-12-31", CreatedOn: createdOn,
	}
	w := doRequest(r, "POST", "/api/invoices", token, inv)
	if w.Code != http.StatusOK {
		t.Fatalf("create invoice: got %d: %s", w.Code, w.Body.String())
	}
	var created models.Invoice
	json.NewDecoder(w.Body).Decode(&created)
	return created.ID
}

type referralRow struct {
	status           string
	paidInvoiceCount int
	creditsRemaining int
}

func readReferral(t *testing.T, db *store.DB, id string) referralRow {
	t.Helper()
	var row referralRow
	if err := db.QueryRow(`SELECT status, paid_invoice_count, credits_remaining FROM referral_rewards WHERE id=?`, id).
		Scan(&row.status, &row.paidInvoiceCount, &row.creditsRemaining); err != nil {
		t.Fatalf("read referral %s: %v", id, err)
	}
	return row
}

func seedReferral(t *testing.T, db *store.DB, id, studentID string) {
	t.Helper()
	// The demo seed already gives this student paid Monthly invoices, and the
	// milestone counts every one of them. Start the student from zero so the
	// assertions below measure this test's payments and not the fixture's.
	db.Exec(`DELETE FROM invoices WHERE student_id=?`, studentID)
	db.Exec(`DELETE FROM referral_rewards WHERE id=? OR referred_student_id=?`, id, studentID)
	if _, err := db.Exec(`INSERT INTO referral_rewards(id,tenant_id,referrer_family_id,referred_student_id,status,paid_invoice_count,credits_remaining) VALUES(?,?,?,?,?,?,?)`,
		id, 1, "FAM_test", studentID, "pending", 0, 0); err != nil {
		t.Fatalf("seed referral: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM referral_rewards WHERE id=?`, id) })
}

// Three paid Monthly invoices earn the reward; reversing one has to give it
// back. Counting only upward left credits_remaining=3 standing on a count of
// two, and the reversal that was meant to correct the mistake did not touch it.
func TestReferralMilestoneUnwindsWhenAPaymentIsReversed(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	const rewardID = "RR_reversal_test"
	seedReferral(t, db, rewardID, "STU001")

	invoices := []string{
		createMonthlyInvoice(t, r, token, "STU001", "2026-06-01"),
		createMonthlyInvoice(t, r, token, "STU001", "2026-07-01"),
		createMonthlyInvoice(t, r, token, "STU001", "2026-08-01"),
	}
	for _, id := range invoices {
		payInvoice(t, r, token, id, "")
	}

	earned := readReferral(t, db, rewardID)
	if earned.status != "earned" || earned.creditsRemaining != 3 || earned.paidInvoiceCount != 3 {
		t.Fatalf("after three payments want earned/3/3, got %s/%d/%d",
			earned.status, earned.creditsRemaining, earned.paidInvoiceCount)
	}

	payInvoice(t, r, token, invoices[2], "Unpaid")

	after := readReferral(t, db, rewardID)
	if after.status != "pending" {
		t.Errorf("reversal left status %q, want pending — the reward outlived the payment that earned it", after.status)
	}
	if after.creditsRemaining != 0 {
		t.Errorf("reversal left %d credits, want 0", after.creditsRemaining)
	}
	if after.paidInvoiceCount != 2 {
		t.Errorf("reversal left paid_invoice_count %d, want 2", after.paidInvoiceCount)
	}
}

// A reward whose credits are already partly spent is deliberately NOT clawed
// back: that benefit has usually reached a real invoice. It is flagged instead.
func TestReferralMilestoneKeepsPartlySpentCredits(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	const rewardID = "RR_spent_test"
	seedReferral(t, db, rewardID, "STU001")

	invoices := []string{
		createMonthlyInvoice(t, r, token, "STU001", "2026-06-01"),
		createMonthlyInvoice(t, r, token, "STU001", "2026-07-01"),
		createMonthlyInvoice(t, r, token, "STU001", "2026-08-01"),
	}
	for _, id := range invoices {
		payInvoice(t, r, token, id, "")
	}
	if _, err := db.Exec(`UPDATE referral_rewards SET credits_remaining=1 WHERE id=?`, rewardID); err != nil {
		t.Fatalf("spend credits: %v", err)
	}

	payInvoice(t, r, token, invoices[2], "Unpaid")

	after := readReferral(t, db, rewardID)
	if after.status != "earned" {
		t.Errorf("partly spent reward became %q, want earned — clawing it back undoes a benefit already given", after.status)
	}
	if after.creditsRemaining != 1 {
		t.Errorf("partly spent reward has %d credits, want 1 left alone", after.creditsRemaining)
	}
	if after.paidInvoiceCount != 2 {
		t.Errorf("flagged reward still reads paid_invoice_count %d, want 2 — the row must show the discrepancy the audit line describes", after.paidInvoiceCount)
	}
	var flagged int
	db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='referral_milestone_stale' AND entity_id=?`, rewardID).Scan(&flagged)
	if flagged == 0 {
		t.Error("no referral_milestone_stale audit row — the discrepancy is invisible to an admin")
	}
}

// The audit trail has to name the transition. This logged "invoice_paid" with
// today as paidOn for every status, so a reversal recorded a payment.
func TestReversalIsNotAuditedAsAPayment(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	invID := createMonthlyInvoice(t, r, token, "STU001", "2026-06-01")
	payInvoice(t, r, token, invID, "")
	payInvoice(t, r, token, invID, "Unpaid")

	var paidRows, reversedRows int
	db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='invoice_paid' AND entity_id=?`, invID).Scan(&paidRows)
	db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='invoice_payment_reversed' AND entity_id=?`, invID).Scan(&reversedRows)
	if paidRows != 1 {
		t.Errorf("got %d invoice_paid rows, want exactly 1 — the reversal must not record a second payment", paidRows)
	}
	if reversedRows != 1 {
		t.Errorf("got %d invoice_payment_reversed rows, want 1", reversedRows)
	}
}

// A re-pay changes nothing, so it must not leave an audit row claiming it did.
func TestRepayNoOpWritesNoAuditRow(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	invID := createMonthlyInvoice(t, r, token, "STU001", "2026-06-01")
	payInvoice(t, r, token, invID, "")
	payInvoice(t, r, token, invID, "")

	var paidRows int
	db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='invoice_paid' AND entity_id=?`, invID).Scan(&paidRows)
	if paidRows != 1 {
		t.Errorf("got %d invoice_paid rows for one payment, want 1", paidRows)
	}
}

// Deleting a paid invoice drops the referral count exactly as reversing one
// does. Closing only the reversal path would leave the reward standing for the
// admin who removes the invoice instead of un-paying it.
func TestReferralMilestoneUnwindsWhenAPaidInvoiceIsDeleted(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	const rewardID = "RR_delete_test"
	seedReferral(t, db, rewardID, "STU001")

	invoices := []string{
		createMonthlyInvoice(t, r, token, "STU001", "2026-06-01"),
		createMonthlyInvoice(t, r, token, "STU001", "2026-07-01"),
		createMonthlyInvoice(t, r, token, "STU001", "2026-08-01"),
	}
	for _, id := range invoices {
		payInvoice(t, r, token, id, "")
	}
	if got := readReferral(t, db, rewardID); got.status != "earned" {
		t.Fatalf("setup: want earned before the delete, got %s", got.status)
	}

	w := doRequest(r, "DELETE", "/api/invoices/"+invoices[2], token, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete invoice: got %d: %s", w.Code, w.Body.String())
	}

	after := readReferral(t, db, rewardID)
	if after.status != "pending" {
		t.Errorf("deleting a paid invoice left status %q, want pending", after.status)
	}
	if after.creditsRemaining != 0 {
		t.Errorf("deleting a paid invoice left %d credits, want 0", after.creditsRemaining)
	}
	if after.paidInvoiceCount != 2 {
		t.Errorf("deleting a paid invoice left paid_invoice_count %d, want 2", after.paidInvoiceCount)
	}
}
