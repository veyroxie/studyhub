package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/core"
	"studyhub/internal/jobs"
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
	db := store.InitDB(testDSN())
	f := billFixture{r: r, db: db, cleanup: func() { db.Close(); cleanup() }}
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
	defer db.Close()
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

func issueBillMonth(t *testing.T, r *chi.Mux, admin string) {
	t.Helper()
	w := doRequest(r, "POST", "/api/billing/month/issue", admin, map[string]any{"month": billPeriod})
	if w.Code != http.StatusOK {
		t.Fatalf("issue the month: %d %s", w.Code, w.Body.String())
	}
}

func familyOutbox(t *testing.T, db *store.DB, ids ...string) (string, string) {
	t.Helper()
	var topic, payload string
	for _, id := range ids {
		db.QueryRow(`SELECT topic, payload FROM outbox WHERE payload LIKE ? ORDER BY id DESC LIMIT 1`, "%"+id+"%").Scan(&topic, &payload)
	}
	return topic, payload
}

// Issuing the month tells a two-child parent once, not once per child.
func TestIssuingTheMonthEmailsAFamilyOnce(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	defer db.Close()
	db.Exec(`DELETE FROM outbox`)
	db.Exec(`DELETE FROM email_queue WHERE to_email=?`, billParent)
	admin := getAdminToken(t, r)
	zayden := monthlyInvoice(t, r, admin, "STU001", 240, models.InvoiceStatusDraft)
	lucy := monthlyInvoice(t, r, admin, "STU002", 260, models.InvoiceStatusDraft)

	issueBillMonth(t, r, admin)

	var rows int
	db.QueryRow(`SELECT count(*) FROM outbox WHERE payload LIKE ? OR payload LIKE ?`, "%"+zayden+"%", "%"+lucy+"%").Scan(&rows)
	if rows != 1 {
		t.Fatalf("%d outbox rows for the family, want 1", rows)
	}
	topic, payload := familyOutbox(t, db, zayden)
	if topic != store.OutboxTopicFamilyBillIssued || !strings.Contains(payload, zayden) || !strings.Contains(payload, lucy) {
		t.Fatalf("outbox %q %q, want one family_bill.issued naming both", topic, payload)
	}
	for _, id := range []string{zayden, lucy} {
		if got := readBillRow(t, db, id).status; got != models.InvoiceStatusUnpaid {
			t.Fatalf("%s: status %q after issue, want Unpaid", id, got)
		}
	}

	jobs.ProcessOutbox(db)
	var emails int
	var subject string
	db.QueryRow(`SELECT count(*), COALESCE(MAX(subject),'') FROM email_queue WHERE to_email=?`, billParent).Scan(&emails, &subject)
	if emails != 1 || !strings.HasPrefix(subject, "Family bill") {
		t.Fatalf("%d emails (%q) to the parent, want one family bill email", emails, subject)
	}
}

// A job whose invoices were all deleted before it ran completes quietly; failing it
// would block every email queued behind it.
func TestAFamilyEmailForDeletedInvoicesDoesNotBlockTheQueue(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	defer db.Close()
	db.Exec(`DELETE FROM outbox`)
	admin := getAdminToken(t, r)
	zayden := monthlyInvoice(t, r, admin, "STU001", 240, models.InvoiceStatusDraft)
	lucy := monthlyInvoice(t, r, admin, "STU002", 260, models.InvoiceStatusDraft)
	issueBillMonth(t, r, admin)
	db.Exec(`UPDATE invoices SET deleted_at=NOW() WHERE id IN (?,?)`, zayden, lucy)

	jobs.ProcessOutbox(db)
	var pending int
	db.QueryRow(`SELECT count(*) FROM outbox WHERE processed_at IS NULL`).Scan(&pending)
	if pending != 0 {
		t.Fatalf("%d outbox rows still pending, want the job completed", pending)
	}
}

// A reissue leaves the void beside its replacement; the review must count the month once.
func TestTheMonthReviewCountsAReissuedInvoiceOnce(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	admin := getAdminToken(t, r)
	id := monthlyInvoice(t, r, admin, "STU001", 240, "")
	if w := doRequest(r, "POST", "/api/invoices/"+id+"/reissue", admin, map[string]any{}); w.Code != http.StatusOK {
		t.Fatalf("reissue: %d", w.Code)
	}
	var run struct {
		Issued []struct {
			StudentID string `json:"studentId"`
		} `json:"issued"`
	}
	json.NewDecoder(doRequest(r, "GET", "/api/billing/month?month="+billPeriod, admin, nil).Body).Decode(&run)
	count := 0
	for _, i := range run.Issued {
		if i.StudentID == "STU001" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("STU001 listed %d times as issued, want once", count)
	}
}

// "Invoice this family" is the monthly run for one parent: the same sibling
// discount, drafted for review, then issued together with one email.
func TestInvoiceThisFamilyPricesLikeTheMonthlyRunAndIssuesTogether(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	defer db.Close()
	admin := getAdminToken(t, r)

	const month, parent, family = "2027-06", "famrun@example.com", "FAM_FAMRUN"
	pricedFamily(t, db, month, parent, family, "Zayden", "Lucy")

	w := doRequest(r, "POST", "/api/billing/family-invoice", admin, map[string]string{"parentEmail": parent, "month": month})
	if w.Code != http.StatusOK {
		t.Fatalf("draft the family: %d %s", w.Code, w.Body.String())
	}
	var drafted struct {
		Drafts []store.FamilyBillMember `json:"drafts"`
	}
	json.NewDecoder(w.Body).Decode(&drafted)
	if len(drafted.Drafts) != 2 {
		t.Fatalf("%d drafts, want one per child", len(drafted.Drafts))
	}
	for _, d := range drafted.Drafts {
		var items string
		db.QueryRow(`SELECT line_items FROM invoices WHERE id=?`, d.InvoiceID).Scan(&items)
		if !strings.Contains(items, "Sibling discount") {
			t.Errorf("%s has no sibling discount: %s", d.StudentName, items)
		}
	}

	if w := doRequest(r, "POST", "/api/billing/family-invoice/issue", admin, map[string]string{"parentEmail": parent, "month": month}); w.Code != http.StatusOK {
		t.Fatalf("issue the family: %d %s", w.Code, w.Body.String())
	}
	for _, d := range drafted.Drafts {
		if got := readBillRow(t, db, d.InvoiceID).status; got != models.InvoiceStatusUnpaid {
			t.Errorf("%s: status %q, want issued", d.InvoiceID, got)
		}
	}
	var rows int
	db.QueryRow(`SELECT count(*) FROM outbox WHERE topic=?`, store.OutboxTopicFamilyBillIssued).Scan(&rows)
	if rows != 1 {
		t.Fatalf("%d family outbox rows, want 1", rows)
	}

	again := doRequest(r, "POST", "/api/billing/family-invoice", admin, map[string]string{"parentEmail": parent, "month": month})
	if again.Code != http.StatusConflict {
		t.Fatalf("drafting an invoiced family again: got %d, want 409", again.Code)
	}
}

// pricedFamily seeds children of one parent and family, each in a priced class, and
// wipes the month afterwards. Returns the student ids.
func pricedFamily(t *testing.T, db *store.DB, month, parent, family string, names ...string) []string {
	t.Helper()
	classID := core.GenerateID("CLS")
	kids := make([]string, len(names))
	for i := range names {
		kids[i] = core.GenerateID("STU")
	}
	wipe := func() {
		db.Exec(`DELETE FROM outbox`)
		db.Exec(`DELETE FROM applied_discounts WHERE invoice_id IN (SELECT id FROM invoices WHERE period=?)`, month)
		db.Exec(`DELETE FROM invoices WHERE period=?`, month)
		for _, k := range kids {
			db.Exec(`DELETE FROM enrollments WHERE student_id=?`, k)
			db.Exec(`DELETE FROM students WHERE id=?`, k)
		}
		db.Exec(`DELETE FROM classes WHERE id=?`, classID)
		db.Exec(`DELETE FROM referral_rewards WHERE referrer_family_id=?`, family)
	}
	wipe()
	t.Cleanup(wipe)
	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,classroom,class_type,level_band,pricing_category_id,default_tier_name,monthly_fee_override)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, classID, 1, "Fam Level 3 & 4", "Monday", "16:00", "17:00", "R1", "Group", "", "PC_group", "Level 3-4", 0)
	for i, k := range kids {
		db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,contact,status,subscription_status,package_amount,package_self_study_hours,family_id,enrolled_classes,registered_on)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, k, 1, names[i], "Fam", parent, "Active", "active", 0, 0, family,
			models.JSONArr([]string{classID}), "2027-01-01")
		db.Exec(`INSERT INTO enrollments(id,tenant_id,student_id,class_id,started_on,tier_name,created_by,created_on)
			VALUES(?,?,?,?,?,?,?,?)`, core.GenerateID("ENR"), 1, k, classID, "2027-01-01", "", "test", "2027-01-01")
	}
	return kids
}

// Invoices made by hand send nothing on their own; the admin sends them on demand.
func TestAnAdminCanEmailAnInvoiceOrAWholeFamilyBill(t *testing.T) {
	f := newBillFixture(t)
	defer f.cleanup()
	f.db.Exec(`DELETE FROM outbox`)

	if w := doRequest(f.r, "POST", "/api/invoices/"+f.zayden+"/email", f.admin, nil); w.Code != http.StatusOK {
		t.Fatalf("email one invoice: %d %s", w.Code, w.Body.String())
	}
	if w := doRequest(f.r, "POST", "/api/family-bills/"+f.zayden+"/email", f.admin, nil); w.Code != http.StatusOK {
		t.Fatalf("email the family bill: %d %s", w.Code, w.Body.String())
	}
	var single, family int
	f.db.QueryRow(`SELECT count(*) FROM outbox WHERE topic=? AND payload=?`, store.OutboxTopicInvoiceIssued, f.zayden).Scan(&single)
	f.db.QueryRow(`SELECT count(*) FROM outbox WHERE topic=?`, store.OutboxTopicFamilyBillIssued).Scan(&family)
	if single != 1 || family != 1 {
		t.Fatalf("outbox single=%d family=%d, want one of each", single, family)
	}
	if w := doRequest(f.r, "POST", "/api/invoices/"+f.zayden+"/email", f.parent, nil); w.Code != http.StatusForbidden {
		t.Fatalf("parent sending: got %d, want 403", w.Code)
	}
}

// With the allowlist on, the button must not claim a send the queue would drop.
func TestEmailingAParentOutsideTheAllowlistQueuesNothing(t *testing.T) {
	f := newBillFixture(t)
	defer f.cleanup()
	f.db.Exec(`DELETE FROM outbox`)
	t.Setenv("OUTBOUND_ALLOWLIST", "etee3001@gmail.com")

	w := doRequest(f.r, "POST", "/api/invoices/"+f.zayden+"/email", f.admin, nil)
	var res struct {
		Queued bool `json:"queued"`
	}
	json.NewDecoder(w.Body).Decode(&res)
	var rows int
	f.db.QueryRow(`SELECT count(*) FROM outbox`).Scan(&rows)
	if w.Code != http.StatusOK || res.Queued || rows != 0 {
		t.Fatalf("code %d queued=%v rows=%d, want 200, not queued, nothing in the outbox", w.Code, res.Queued, rows)
	}
}

// A rejected claim used to go back to Unpaid in silence; the parent now sees why,
// and the reason clears the moment they claim again.
func TestARejectionReasonReachesTheParentAndClearsOnTheNextClaim(t *testing.T) {
	f := newBillFixture(t)
	defer f.cleanup()
	if code := f.pay(f.parent, f.parentClaim()); code != http.StatusOK {
		t.Fatalf("claim: %d", code)
	}
	reject := map[string]any{"parentEmail": billParent, "invoiceIds": []string{f.zayden, f.lucy}, "expectedTotal": 500,
		"status": "Unpaid", "note": "No transfer of RM500 in the bank on 3 Oct"}
	if code := f.pay(f.admin, reject); code != http.StatusOK {
		t.Fatalf("reject: %d", code)
	}
	noteFor := func(id string) string {
		for _, inv := range parentSnapshot(t, f.r, f.parent).Invoices {
			if inv.ID == id {
				return inv.PaymentNote
			}
		}
		return "<missing>"
	}
	if got := noteFor(f.lucy); got != "No transfer of RM500 in the bank on 3 Oct" {
		t.Fatalf("parent sees note %q", got)
	}
	if code := f.pay(f.parent, f.parentClaim()); code != http.StatusOK {
		t.Fatalf("claim again: %d", code)
	}
	if got := noteFor(f.lucy); got != "" {
		t.Fatalf("stale reason %q still shown beside a new claim", got)
	}
}
