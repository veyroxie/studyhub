package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/core"
	"studyhub/internal/store"
)

func referralCredits(t *testing.T, db *store.DB, rewardID string) (int, string) {
	t.Helper()
	var n int
	var status string
	if err := db.QueryRow(`SELECT credits_remaining, status FROM referral_rewards WHERE id=?`, rewardID).Scan(&n, &status); err != nil {
		t.Fatalf("read reward: %v", err)
	}
	return n, status
}

// Drafting spends a referral credit. Deleting the draft bills nobody, so the credit
// must come back, or the family loses a month of discount it never used.
func TestDeletingAnInvoiceGivesBackTheReferralCreditItSpent(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	defer db.Close()
	admin := getAdminToken(t, r)

	const month, parent, family = "2027-07", "refback@example.com", "FAM_REFBACK"
	pricedFamily(t, db, month, parent, family, "Aiden")
	rewardID := core.GenerateID("RR")
	db.Exec(`INSERT INTO referral_rewards(id,tenant_id,referrer_family_id,referred_student_id,status,paid_invoice_count,credits_remaining) VALUES(?,?,?,?,?,?,?)`,
		rewardID, 1, family, core.GenerateID("STU"), "earned", 3, 1)

	w := doRequest(r, "POST", "/api/billing/family-invoice", admin, map[string]string{"parentEmail": parent, "month": month})
	if w.Code != http.StatusOK {
		t.Fatalf("draft: %d %s", w.Code, w.Body.String())
	}
	var drafted struct {
		Drafts []store.FamilyBillMember `json:"drafts"`
	}
	json.NewDecoder(w.Body).Decode(&drafted)
	if n, status := referralCredits(t, db, rewardID); n != 0 || status != "exhausted" {
		t.Fatalf("after drafting: %d credits (%s), want the last one spent", n, status)
	}

	if w := doRequest(r, "DELETE", "/api/invoices/"+drafted.Drafts[0].InvoiceID, admin, nil); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if n, status := referralCredits(t, db, rewardID); n != 1 || status != "earned" {
		t.Fatalf("after deleting: %d credits (%s), want 1 given back and the reward earned again", n, status)
	}

	// Deleting cannot be repeated, so neither can the refund; drafting again spends it again.
	if w := doRequest(r, "POST", "/api/billing/family-invoice", admin, map[string]string{"parentEmail": parent, "month": month}); w.Code != http.StatusOK {
		t.Fatalf("redraft: %d %s", w.Code, w.Body.String())
	}
	if n, _ := referralCredits(t, db, rewardID); n != 0 {
		t.Fatalf("after redrafting: %d credits, want it spent again", n)
	}
}

func draftWithReferral(t *testing.T, r *chi.Mux, db *store.DB, admin, month, parent, family string) (string, string) {
	t.Helper()
	pricedFamily(t, db, month, parent, family, "Aiden")
	rewardID := core.GenerateID("RR")
	db.Exec(`INSERT INTO referral_rewards(id,tenant_id,referrer_family_id,referred_student_id,status,paid_invoice_count,credits_remaining) VALUES(?,?,?,?,?,?,?)`,
		rewardID, 1, family, core.GenerateID("STU"), "earned", 3, 2)
	w := doRequest(r, "POST", "/api/billing/family-invoice", admin, map[string]string{"parentEmail": parent, "month": month})
	if w.Code != http.StatusOK {
		t.Fatalf("draft: %d %s", w.Code, w.Body.String())
	}
	var drafted struct {
		Drafts []store.FamilyBillMember `json:"drafts"`
	}
	json.NewDecoder(w.Body).Decode(&drafted)
	return drafted.Drafts[0].InvoiceID, rewardID
}

func TestBulkDeletingIssuedInvoicesGivesTheirCreditsBack(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	defer db.Close()
	admin := getAdminToken(t, r)
	const month, parent = "2027-08", "refbulk@example.com"
	id, rewardID := draftWithReferral(t, r, db, admin, month, parent, "FAM_REFBULK")
	if w := doRequest(r, "POST", "/api/billing/family-invoice/issue", admin, map[string]string{"parentEmail": parent, "month": month}); w.Code != http.StatusOK {
		t.Fatalf("issue: %d", w.Code)
	}
	if w := doRequest(r, "POST", "/api/invoices/bulk-delete", admin, map[string]any{"ids": []string{id}}); w.Code != http.StatusOK {
		t.Fatalf("bulk delete: %d %s", w.Code, w.Body.String())
	}
	if n, _ := referralCredits(t, db, rewardID); n != 2 {
		t.Fatalf("%d credits after bulk delete, want the spent one back (2)", n)
	}
}

// Drafts made before the reward was recorded fall back to the family's newest part-spent reward.
func TestAnUntaggedReferralIsReturnedToTheNewestPartSpentReward(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	defer db.Close()
	admin := getAdminToken(t, r)
	id, rewardID := draftWithReferral(t, r, db, admin, "2027-09", "reflegacy@example.com", "FAM_REFLEGACY")
	db.Exec(`UPDATE applied_discounts SET source='referral_rewards balance' WHERE invoice_id=?`, id)

	if w := doRequest(r, "DELETE", "/api/invoices/"+id, admin, nil); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}
	if n, _ := referralCredits(t, db, rewardID); n != 2 {
		t.Fatalf("%d credits, want the spent one back (2)", n)
	}
}
