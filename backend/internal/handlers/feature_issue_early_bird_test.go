package handlers

import (
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/jobs"
	"studyhub/internal/models"
)

// Splitting drafting from issuing split WHEN the early bird is decided from
// when the invoice becomes a document. A draft made on the 1st carries the
// discount and a cutoff of the 7th; if Nadine issues on the 20th, that invoice
// is born with an already-expired cutoff -- and applyEarlyBirdExpiry, which
// runs hourly, immediately voids and replaces it. The parent gets an invoice
// saying "pay by <date in the past>" followed by a correction.
//
// The discount is granted ON ISSUE, which is what its pending state has always
// claimed, so a draft whose cutoff has passed is issued at full price instead.
func TestIssuingAfterTheCutoffDropsTheEarlyBird(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	r := chi.NewRouter()
	r.Post("/api/auth/login", auth.HandleLogin(db))
	r.Group(func(g chi.Router) {
		g.Use(auth.JWTMiddleware(db))
		g.Post("/api/billing/month/issue", HandleMonthIssue(db))
	})
	token := getToken(t, r, "admin@studyhub.com", "admin123")

	// A month whose 7th is long past, so issuing now is issuing late.
	const month = "2026-02"
	classID := core.GenerateID("CLS")
	studentID := core.GenerateID("STU")
	wipe := func() {
		db.Exec(`DELETE FROM applied_discounts WHERE invoice_id IN (SELECT id FROM invoices WHERE period=?)`, month)
		db.Exec(`DELETE FROM outbox`)
		db.Exec(`DELETE FROM invoices WHERE period=?`, month)
		db.Exec(`DELETE FROM enrollments WHERE student_id=?`, studentID)
		db.Exec(`DELETE FROM students WHERE id=?`, studentID)
		db.Exec(`DELETE FROM classes WHERE id=?`, classID)
	}
	wipe()
	t.Cleanup(wipe)

	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,classroom,class_type,level_band,pricing_category_id,default_tier_name,monthly_fee_override)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		classID, 1, "Late Issue 3 & 4", "Monday", "16:00", "17:00", "R1", "Group", "", "PC_group", "Level 3-4", 0)
	db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,contact,status,subscription_status,package_amount,package_self_study_hours,family_id,enrolled_classes,registered_on)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		studentID, 1, "Late", "Issue", "", "Active", "active", 0, 0, "", models.JSONArr([]string{classID}), "2026-01-01")
	db.Exec(`INSERT INTO enrollments(id,tenant_id,student_id,class_id,started_on,tier_name,created_by,created_on)
		VALUES(?,?,?,?,?,?,?,?)`,
		core.GenerateID("ENR"), 1, studentID, classID, "2026-01-01", "", "test", "2026-01-01")

	// Drafted on the 1st: the early bird applies at that moment.
	jobs.RunMonthlyInvoices(db, time.Date(2026, 2, 1, 9, 0, 0, 0, time.Local))

	var id, cutoff string
	var amount, earlyBird float64
	if err := db.QueryRow(`SELECT id, amount, COALESCE(early_bird_discount,0), COALESCE(early_bird_cutoff,'')
		FROM invoices WHERE student_id=? AND period=?`, studentID, month).
		Scan(&id, &amount, &earlyBird, &cutoff); err != nil {
		t.Fatalf("nothing drafted: %v", err)
	}
	if earlyBird != 10 || amount != 250 {
		t.Fatalf("draft is %.2f with a %.2f early bird, want 250 and 10", amount, earlyBird)
	}

	// Issued long after that cutoff.
	w := authedJSON(t, r, "POST", "/api/billing/month/issue", token, map[string]any{"month": month})
	if w.Code != http.StatusOK {
		t.Fatalf("issue: %d %s", w.Code, w.Body.String())
	}

	var issuedAmount, issuedEarlyBird float64
	var issuedCutoff, number, status string
	db.QueryRow(`SELECT amount, COALESCE(early_bird_discount,0), COALESCE(early_bird_cutoff,''),
		COALESCE(invoice_no,''), status FROM invoices WHERE id=?`, id).
		Scan(&issuedAmount, &issuedEarlyBird, &issuedCutoff, &number, &status)

	if number == "" || status != models.InvoiceStatusUnpaid {
		t.Fatalf("not issued: number %q status %q", number, status)
	}
	if issuedEarlyBird != 0 {
		t.Errorf("issued carrying a %.2f early bird whose cutoff passed on %s", issuedEarlyBird, cutoff)
	}
	if issuedCutoff != "" {
		t.Errorf("early_bird_cutoff %q on an issued invoice; the hourly clawback will void this immediately", issuedCutoff)
	}
	if issuedAmount != 260 {
		t.Errorf("issued at %.2f, want the full 260", issuedAmount)
	}

	// And the discount line must be gone, or the invoice shows a discount it
	// did not get.
	var itemsJSON string
	db.QueryRow(`SELECT COALESCE(line_items,'[]') FROM invoices WHERE id=?`, id).Scan(&itemsJSON)
	for _, it := range models.ParseLineItems(itemsJSON) {
		if it.Kind == models.LineItemKindDiscount && it.Name == models.EarlyBirdLineName {
			t.Errorf("the early-bird line survived on an invoice issued after its cutoff")
		}
	}
}
