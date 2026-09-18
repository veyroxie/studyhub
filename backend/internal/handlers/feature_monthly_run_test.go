package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/auth"

	"studyhub/internal/core"
	"studyhub/internal/jobs"
	"studyhub/internal/models"
)

// The month Nadine actually runs: draft every billable student, look at the
// list, then issue the lot. Before this the cron wrote Unpaid invoices directly
// -- all 48 in production carry no number at all, because nothing ever went
// through the issue transition that 0067 added to assign one.
func TestTheMonthIsDraftedReviewedThenIssued(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	r := chi.NewRouter()
	r.Post("/api/auth/login", auth.HandleLogin(db))
	r.Group(func(g chi.Router) {
		g.Use(auth.JWTMiddleware(db))
		g.Get("/api/billing/month", HandleMonthRun(db))
		g.Post("/api/billing/month/draft", HandleMonthDraft(db))
		g.Post("/api/billing/month/issue", HandleMonthIssue(db))
	})
	token := getToken(t, r, "admin@studyhub.com", "admin123")

	const month = "2027-05"
	classID := core.GenerateID("CLS")
	studentID := core.GenerateID("STU")
	wipe := func() {
		db.Exec(`DELETE FROM applied_discounts WHERE invoice_id IN (SELECT id FROM invoices WHERE period=?)`, month)
		db.Exec(`DELETE FROM outbox`)
		db.Exec(`DELETE FROM email_queue WHERE subject LIKE '%Month Runner%'`)
		db.Exec(`DELETE FROM invoices WHERE period=?`, month)
		db.Exec(`DELETE FROM enrollments WHERE student_id=?`, studentID)
		db.Exec(`DELETE FROM students WHERE id=?`, studentID)
		db.Exec(`DELETE FROM classes WHERE id=?`, classID)
	}
	wipe()
	t.Cleanup(wipe)

	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,classroom,class_type,level_band,pricing_category_id,default_tier_name,monthly_fee_override)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		classID, 1, "Run Level 3 & 4", "Monday", "16:00", "17:00", "R1", "Group", "", "PC_group", "Level 3-4", 0)
	db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,contact,status,subscription_status,package_amount,package_self_study_hours,family_id,enrolled_classes,registered_on)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		studentID, 1, "Month", "Runner", "runner@example.com", "Active", "active", 0, 0, "",
		models.JSONArr([]string{classID}), "2027-01-01")
	db.Exec(`INSERT INTO enrollments(id,tenant_id,student_id,class_id,started_on,tier_name,created_by,created_on)
		VALUES(?,?,?,?,?,?,?,?)`,
		core.GenerateID("ENR"), 1, studentID, classID, "2027-01-01", "", "test", "2027-01-01")

	// Draft.
	w := authedJSON(t, r, "POST", "/api/billing/month/draft", token, map[string]any{"month": month})
	if w.Code != http.StatusOK {
		t.Fatalf("draft: %d %s", w.Code, w.Body.String())
	}

	// Review. The draft is listed and carries no number.
	w = authedJSON(t, r, "GET", "/api/billing/month?month="+month, token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("review: %d %s", w.Code, w.Body.String())
	}
	var run struct {
		Drafts []struct {
			InvoiceID, StudentID, Status, InvoiceNo string
			Amount                                  float64
		} `json:"drafts"`
		Issued []struct {
			InvoiceNo string `json:"invoiceNo"`
		} `json:"issued"`
	}
	json.NewDecoder(w.Body).Decode(&run)
	var mine string
	for _, d := range run.Drafts {
		if d.StudentID == studentID {
			mine = d.InvoiceID
			if d.Status != models.InvoiceStatusDraft {
				t.Errorf("status %q, want Draft", d.Status)
			}
			if d.InvoiceNo != "" {
				t.Errorf("a draft carries no number, got %q", d.InvoiceNo)
			}
		}
	}
	if mine == "" {
		t.Fatal("the drafted student is not on the review list")
	}
	// Nothing has reached the parent yet: that is what the review step is for.
	var queued int
	db.QueryRow(`SELECT count(*) FROM email_queue WHERE subject LIKE '%Month Runner%'`).Scan(&queued)
	if queued != 0 {
		t.Errorf("a draft emailed the parent %d time(s)", queued)
	}

	// Issue.
	w = authedJSON(t, r, "POST", "/api/billing/month/issue", token, map[string]any{"month": month})
	if w.Code != http.StatusOK {
		t.Fatalf("issue: %d %s", w.Code, w.Body.String())
	}
	var status, number string
	db.QueryRow(`SELECT status, COALESCE(invoice_no,'') FROM invoices WHERE id=?`, mine).Scan(&status, &number)
	if status != models.InvoiceStatusUnpaid {
		t.Errorf("status %q after issue, want Unpaid", status)
	}
	if number == "" {
		t.Error("issuing assigned no invoice number, which is the whole point of the transition")
	}

	// The email is an OUTBOX row, written in the issuing transaction, not a
	// send inside it.
	var pending int
	db.QueryRow(`SELECT count(*) FROM outbox WHERE payload=? AND processed_at IS NULL`, mine).Scan(&pending)
	if pending != 1 {
		t.Fatalf("outbox rows for the issued invoice: %d, want 1", pending)
	}

	// The relay turns it into a queued email, exactly once.
	jobs.ProcessOutbox(db)
	db.QueryRow(`SELECT count(*) FROM email_queue WHERE subject LIKE '%Month Runner%'`).Scan(&queued)
	if queued != 1 {
		t.Errorf("queued %d emails after the relay ran, want 1", queued)
	}
	jobs.ProcessOutbox(db)
	db.QueryRow(`SELECT count(*) FROM email_queue WHERE subject LIKE '%Month Runner%'`).Scan(&queued)
	if queued != 1 {
		t.Errorf("the relay sent again on a second pass: %d emails. It must not re-send a processed row", queued)
	}
}
