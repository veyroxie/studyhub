package jobs

import (
	"testing"
	"time"

	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/rating"
	"studyhub/internal/store"
)

// The monthly cron priced classes by joining pricing_tiers on class_type +
// level_band. 39 of 43 production classes have no band, so that join returned
// nothing, the fee came out 0, the `base <= 0` rule skipped the student, and
// the cron issued nothing at all -- every invoice for months was made by hand,
// and an active student with four priced classes was simply never billed.
//
// This is that student's shape: a class the catalogue prices and the old join
// could not. It also asserts the whole invoice row, because the shadow run
// compares totals and cannot see a dropped sibling id or a wrong cutoff.
func TestMonthlyCronBillsAClassTheOldTierJoinPricedAtZero(t *testing.T) {
	db := store.InitDB(testDSN())
	const period = "2027-03"
	// Day 1, so the early bird applies (isLateRun is day > 7).
	now := time.Date(2027, 3, 1, 9, 0, 0, 0, time.Local)

	classID := core.GenerateID("CLS")
	studentID := core.GenerateID("STU")
	cleanup := func() {
		db.Exec(`DELETE FROM email_queue WHERE to_email LIKE 'cron-test%'`)
		// Every student the run billed for this period, not just ours: the cron
		// spans all tenants and this database is shared with other suites.
		db.Exec(`DELETE FROM applied_discounts WHERE invoice_id IN (SELECT id FROM invoices WHERE period=?)`, period)
		db.Exec(`DELETE FROM outbox`)
		db.Exec(`DELETE FROM invoices WHERE period=?`, period)
		db.Exec(`DELETE FROM enrollments WHERE student_id=?`, studentID)
		db.Exec(`DELETE FROM students WHERE id=?`, studentID)
		db.Exec(`DELETE FROM classes WHERE id=?`, classID)
	}
	cleanup()
	t.Cleanup(cleanup)

	// No level_band and no monthly_fee_override: priced only by the catalogue.
	if _, err := db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,classroom,class_type,level_band,pricing_category_id,default_tier_name,monthly_fee_override)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		classID, 1, "Cron Level 3 & 4", "Monday", "16:00", "17:00", "R1", "Group", "", "PC_group", "Level 3-4", 0); err != nil {
		t.Fatalf("seed class: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,contact,status,subscription_status,package_amount,package_self_study_hours,family_id,enrolled_classes,registered_on)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		studentID, 1, "Cron", "Tester", "", "Active", "active", 0, 0, "", models.JSONArr([]string{classID}), "2027-01-01"); err != nil {
		t.Fatalf("seed student: %v", err)
	}
	// The cron reads live enrolments, not students.enrolled_classes, because
	// only the enrolment carries the tier the student is priced at.
	if _, err := db.Exec(`INSERT INTO enrollments(id,tenant_id,student_id,class_id,started_on,tier_name,created_by,created_on)
		VALUES(?,?,?,?,?,?,?,?)`,
		core.GenerateID("ENR"), 1, studentID, classID, "2027-01-01", "", "test", "2027-01-01"); err != nil {
		t.Fatalf("seed enrolment: %v", err)
	}

	if n := generateMonthlyInvoices(db, now); n == 0 {
		t.Fatal("the run issued nothing at all")
	}

	var amount, earlyBird, sibling, referral float64
	var gotPeriod, cutoff, itemsJSON, status, invoiceNo, invoiceID string
	err := db.QueryRow(`SELECT id, amount, period, COALESCE(early_bird_discount,0), COALESCE(sibling_discount,0),
		COALESCE(referral_credit,0), COALESCE(early_bird_cutoff,''), COALESCE(line_items,'[]'),
		status, COALESCE(invoice_no,'')
		FROM invoices WHERE student_id=? AND type='Monthly' AND period=?`, studentID, period).
		Scan(&invoiceID, &amount, &gotPeriod, &earlyBird, &sibling, &referral, &cutoff, &itemsJSON, &status, &invoiceNo)
	if err != nil {
		t.Fatalf("no monthly invoice was drafted for a student the catalogue can price: %v", err)
	}

	// The run DRAFTS. Nothing reaches a parent and no number is burned until
	// someone reviews the month and issues it.
	if status != models.InvoiceStatusDraft {
		t.Errorf("status %q, want %q -- the run must draft, not issue", status, models.InvoiceStatusDraft)
	}
	if invoiceNo != "" {
		t.Errorf("a draft carries no number, got %q", invoiceNo)
	}
	var queued int
	db.QueryRow(`SELECT count(*) FROM email_queue WHERE subject LIKE '%Cron Tester%'`).Scan(&queued)
	if queued != 0 {
		t.Errorf("drafting emailed a parent %d time(s); a draft tells them nothing", queued)
	}

	// Group / Level 3-4 once a week is 260 in the seeded catalogue, less the
	// RM10 early bird. The old join billed this student nothing.
	if amount != 250 {
		t.Errorf("amount %.2f, want 250 (260 catalogue less the RM10 early bird)", amount)
	}
	if earlyBird != 10 {
		t.Errorf("early_bird_discount %.2f, want 10", earlyBird)
	}
	if sibling != 0 || referral != 0 {
		t.Errorf("sibling %.2f / referral %.2f, want 0 for a student with no family", sibling, referral)
	}
	if cutoff != "2027-03-07" {
		t.Errorf("early_bird_cutoff %q, want 2027-03-07", cutoff)
	}

	items := models.ParseLineItems(itemsJSON)
	var tuition, discount int
	for _, it := range items {
		switch it.Kind {
		case models.LineItemKindDiscount:
			discount++
			// Discount lines carry a NEGATIVE amount, as appendDiscount always did.
			if it.Name == models.EarlyBirdLineName && it.Amount != -10 {
				t.Errorf("early bird line is %.2f, want -10", it.Amount)
			}
		default:
			tuition++
			if it.Amount != 260 {
				t.Errorf("tuition line is %.2f, want the catalogue's 260", it.Amount)
			}
			if it.PeriodStart != "2027-03-01" || it.PeriodEnd != "2027-03-31" {
				t.Errorf("line period %s..%s, want the billed month", it.PeriodStart, it.PeriodEnd)
			}
		}
	}
	if tuition != 1 || discount != 1 {
		t.Errorf("got %d tuition and %d discount lines, want 1 and 1: %s", tuition, discount, itemsJSON)
	}

	// The discount is recorded with its TYPE, not just an amount in a column.
	// Two of ours are both a flat RM10, so an amount alone identifies neither.
	ds, derr := store.AppliedDiscountsFor(db, 1, invoiceID)
	if derr != nil {
		t.Fatalf("read applied discounts: %v", derr)
	}
	var found bool
	for _, d := range ds {
		if d.TypeID != rating.TypeEarlyBird {
			continue
		}
		found = true
		if d.Amount != 10 {
			t.Errorf("recorded early bird %.2f, want 10", d.Amount)
		}
		if d.State != rating.StatePending {
			t.Errorf("early bird state %q, want %q -- it is granted conditionally", d.State, rating.StatePending)
		}
	}
	if !found {
		t.Errorf("no early-bird row in applied_discounts for %s", invoiceID)
	}
}
