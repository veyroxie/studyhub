package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"studyhub/internal/core"
	"studyhub/internal/jobs"
	"studyhub/internal/mailer"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

// Matches the file names HandleUploadProof writes: proof_<invoiceID>_<unix>.<ext>.
var proofPathPattern = regexp.MustCompile(`^uploads/proof_(.+)_\d+\.(jpg|jpeg|png|pdf)$`)

type familyBillPayReq struct {
	Period        string   `json:"period"`
	ParentEmail   string   `json:"parentEmail"` // admin only; a parent always pays their own
	InvoiceIDs    []string `json:"invoiceIds"`
	ExpectedTotal float64  `json:"expectedTotal"`
	Status        string   `json:"status"`
	PaymentMethod string   `json:"paymentMethod"`
	ReferenceNo   string   `json:"referenceNo"`
	PaymentProof  string   `json:"paymentProof"`
	Note          string   `json:"note"` // why a rejection sent the bill back, for the parent
}

// HandleFamilyBillPay records one payment across every child's invoice in a family bill.
//
// POST /api/family-bills/pay
func HandleFamilyBillPay(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		req, contact, ok := decodeFamilyBillPay(w, r, c)
		if !ok {
			return
		}
		tid, tOK := writeTenant(w, c)
		if !tOK {
			return
		}
		paid, err := applyFamilyBillPayment(r.Context(), db, c, tid, contact, req)
		if err != nil {
			respondFamilyBillError(w, r, err)
			return
		}
		afterFamilyBillPayment(db, c, tid, req, paid)
		core.Respond(w, map[string]any{"status": req.Status, "invoiceIds": invoiceIDsOf(paid), "total": totalOf(paid)})
	}
}

func decodeFamilyBillPay(w http.ResponseWriter, r *http.Request, c *core.Claims) (familyBillPayReq, string, bool) {
	var req familyBillPayReq
	if c == nil || (c.Role != "parent" && !core.IsAdminRole(c)) {
		core.RespondError(w, "forbidden", http.StatusForbidden)
		return req, "", false
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		core.RespondError(w, "bad request body", http.StatusBadRequest)
		return req, "", false
	}
	if msg := familyBillPayRequestError(c, req); msg != "" {
		core.RespondError(w, msg, http.StatusBadRequest)
		return req, "", false
	}
	if c.Role == "parent" {
		return req, c.Email, true
	}
	return req, req.ParentEmail, true
}

func familyBillPayRequestError(c *core.Claims, req familyBillPayReq) string {
	if !monthPattern.MatchString(req.Period) {
		return "period must be YYYY-MM"
	}
	if len(req.InvoiceIDs) == 0 {
		return "no invoices to pay"
	}
	if c.Role == "parent" && req.Status != models.InvoiceStatusPendingVerification {
		return "parents may only submit payment for verification"
	}
	if c.Role != "parent" && req.ParentEmail == "" {
		return "parentEmail is required"
	}
	if len(strings.TrimSpace(req.Note)) > store.PaymentNoteMaxLen {
		return "keep the reason under 300 characters"
	}
	allowed := req.Status == models.InvoiceStatusPaid || req.Status == models.InvoiceStatusPendingVerification ||
		req.Status == models.InvoiceStatusUnpaid
	if !allowed {
		return "invalid status"
	}
	return ""
}

type familyBillError struct {
	status  int
	message string
}

func (e familyBillError) Error() string { return e.message }

var errFamilyBillChanged = familyBillError{http.StatusConflict,
	"this family bill changed since it was opened — reload and try again"}

func respondFamilyBillError(w http.ResponseWriter, r *http.Request, err error) {
	if fe, ok := err.(familyBillError); ok {
		core.RespondError(w, fe.message, fe.status)
		return
	}
	core.LogFromReq(r).Error("family bill payment failed", "err", err)
	core.RespondError(w, "could not record the payment", http.StatusInternalServerError)
}

// applyFamilyBillPayment moves every target in one transaction: all of the bill is paid, or none of it.
func applyFamilyBillPayment(ctx context.Context, db *store.DB, c *core.Claims, tid int, contact string, req familyBillPayReq) ([]store.FamilyBillMember, error) {
	tx, err := db.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	members, err := store.FamilyBillMembers(tx, tid, contact, req.Period, true)
	if err != nil {
		return nil, err
	}
	targets := store.FamilyBillTargets(members, req.Status)
	if !store.SameFamilyBill(targets, req.InvoiceIDs, req.ExpectedTotal) {
		return nil, errFamilyBillChanged
	}
	if err := checkFamilyBillTargets(c, targets, req); err != nil {
		return nil, err
	}
	if err := recordFamilyBillTargets(tx, c, tid, targets, req); err != nil {
		return nil, err
	}
	return targets, tx.Commit()
}

func checkFamilyBillTargets(c *core.Claims, targets []store.FamilyBillMember, req familyBillPayReq) error {
	byParent := c.Role == "parent"
	for _, m := range targets {
		if !store.PaymentMoveAllowed(m.Status, req.Status, byParent) {
			return familyBillError{http.StatusConflict, payConflictMessage(m.Status)}
		}
		method, ref := firstNonEmpty(req.PaymentMethod, m.PaymentMethod), firstNonEmpty(req.ReferenceNo, m.ReferenceNo)
		if req.Status != models.InvoiceStatusUnpaid && method != "" && method != "Cash" && ref == "" {
			return familyBillError{http.StatusBadRequest, "reference number required for " + method}
		}
	}
	if req.PaymentProof != "" && !proofBelongsToBill(req.PaymentProof, targets) {
		return familyBillError{http.StatusBadRequest, "that proof was not uploaded for this bill"}
	}
	return nil
}

// A proof is uploaded once, against one member, and then shared by the rest.
func proofBelongsToBill(path string, targets []store.FamilyBillMember) bool {
	m := proofPathPattern.FindStringSubmatch(path)
	if m == nil {
		return false
	}
	for _, t := range targets {
		if t.InvoiceID == m[1] {
			return true
		}
	}
	return false
}

func recordFamilyBillTargets(tx *store.Tx, c *core.Claims, tid int, targets []store.FamilyBillMember, req familyBillPayReq) error {
	tw, twArgs := " AND tenant_id=?", []any{tid}
	change := store.PaymentChange{Status: req.Status, Method: req.PaymentMethod, Reference: req.ReferenceNo,
		Today: core.Today(), ByParent: c.Role == "parent", Note: strings.TrimSpace(req.Note)}
	for _, m := range targets {
		changed, err := store.RecordInvoicePayment(tx, tw, twArgs, m.InvoiceID, change)
		if err != nil {
			return err
		}
		// The rows are locked, so a miss means the guard refused one: keep the bill whole.
		if !changed {
			return errFamilyBillChanged
		}
		if req.PaymentProof == "" {
			continue
		}
		if _, err := tx.Exec(`UPDATE invoices SET payment_proof=? WHERE id=? AND tenant_id=?`, req.PaymentProof, m.InvoiceID, tid); err != nil {
			return err
		}
	}
	return nil
}

// Side effects run after commit and never undo it, matching the single-invoice path.
func afterFamilyBillPayment(db *store.DB, c *core.Claims, tid int, req familyBillPayReq, paid []store.FamilyBillMember) {
	for _, m := range paid {
		detail, _ := json.Marshal(map[string]any{"studentId": m.StudentID, "amount": m.Amount, "status": req.Status,
			"method": req.PaymentMethod, "familyBill": req.Period, "familyBillInvoices": invoiceIDsOf(paid)})
		core.LogAudit(db, tid, c.Email, invoiceAuditAction(req.Status), "invoice", m.InvoiceID, string(detail))
	}
	if req.Status == models.InvoiceStatusPaid || req.Status == models.InvoiceStatusUnpaid {
		for _, studentID := range uniqueStudents(paid) {
			store.ReferralReconcile(db, studentID, c)
		}
	}
	if req.Status == models.InvoiceStatusPaid {
		sendFamilyBillPaidEmail(db, tid, req, paid)
	}
}

// One "payment received" email per bill, and only for a payment the parent submitted.
func sendFamilyBillPaidEmail(db *store.DB, tid int, req familyBillPayReq, paid []store.FamilyBillMember) {
	var submitted bool
	var contact string
	ids := invoiceIDsOf(paid)
	args := []any{tid}
	for _, id := range ids {
		args = append(args, id)
	}
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	db.QueryRow(`SELECT COALESCE(bool_or(i.submitted_by_parent),false), COALESCE(MAX(s.contact),'')
		FROM invoices i JOIN students s ON s.id=i.student_id AND s.tenant_id=i.tenant_id
		WHERE i.tenant_id=? AND i.id IN (`+placeholders+`)`, args...).Scan(&submitted, &contact)
	if !submitted || contact == "" {
		return
	}
	description := familyBillDescription(req.Period, paid)
	method := firstNonEmpty(req.PaymentMethod, paid[0].PaymentMethod)
	go func() {
		body := mailer.RenderPaymentReceivedEmail(paid[0].ParentName, description, fmt.Sprintf("%.2f", totalOf(paid)), method)
		if err := core.SendEmail(contact, "Payment received — "+description, body); err != nil {
			core.Logger.Error("family bill payment email failed", "err", err, "email", contact, "period", req.Period)
		}
	}()
}

func familyBillDescription(period string, members []store.FamilyBillMember) string {
	names := make([]string, len(members))
	for i, m := range members {
		names[i] = strings.SplitN(m.StudentName, " ", 2)[0]
	}
	return "Family bill " + period + " — " + strings.Join(names, ", ")
}

func invoiceIDsOf(members []store.FamilyBillMember) []string {
	ids := make([]string, len(members))
	for i, m := range members {
		ids[i] = m.InvoiceID
	}
	return ids
}

func totalOf(members []store.FamilyBillMember) float64 {
	total := 0.0
	for _, m := range members {
		total += m.Amount
	}
	return round2cmp(total)
}

func uniqueStudents(members []store.FamilyBillMember) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, m := range members {
		if !seen[m.StudentID] {
			seen[m.StudentID] = true
			out = append(out, m.StudentID)
		}
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

type familyInvoiceReq struct {
	ParentEmail string `json:"parentEmail"`
	Month       string `json:"month"`
}

func decodeFamilyInvoice(w http.ResponseWriter, r *http.Request, c *core.Claims) (familyInvoiceReq, int, bool) {
	var req familyInvoiceReq
	if !core.IsAdminRole(c) {
		core.RespondError(w, "admin only", http.StatusForbidden)
		return req, 0, false
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ParentEmail == "" || !monthPattern.MatchString(req.Month) {
		core.RespondError(w, "parentEmail and a YYYY-MM month are required", http.StatusBadRequest)
		return req, 0, false
	}
	tid, ok := writeTenant(w, c)
	return req, tid, ok
}

// HandleFamilyInvoiceDraft drafts one parent's month through the monthly run itself,
// so sibling, referral and early-bird rules are the ones every other invoice gets.
//
// POST /api/billing/family-invoice
func HandleFamilyInvoiceDraft(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		req, tid, ok := decodeFamilyInvoice(w, r, c)
		if !ok {
			return
		}
		jobs.RunMonthlyInvoicesForParent(db, draftClock(req.Month), c, req.ParentEmail)
		drafts, err := store.ParentMonthDrafts(db, tid, req.ParentEmail, req.Month)
		if err != nil {
			core.RespondError(w, "could not read the drafts", http.StatusInternalServerError)
			return
		}
		if len(drafts) == 0 {
			core.RespondError(w, "nothing drafted for "+req.Month+": each child already has a monthly invoice, is frozen or inactive, or has a class with no price", http.StatusConflict)
			return
		}
		core.LogAudit(db, tid, c.Email, "family_invoice_drafted", "invoice", req.Month, "parent="+req.ParentEmail)
		core.Respond(w, map[string]any{"month": req.Month, "drafts": drafts})
	}
}

// HandleFamilyInvoiceIssue issues one parent's drafts for the month together, with one email.
//
// POST /api/billing/family-invoice/issue
func HandleFamilyInvoiceIssue(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		req, tid, ok := decodeFamilyInvoice(w, r, c)
		if !ok {
			return
		}
		groups, err := store.MonthDraftGroups(db, " AND i.tenant_id=? AND s.contact=?", []any{tid, req.ParentEmail}, req.Month)
		if err != nil {
			core.RespondError(w, "could not read the drafts", http.StatusInternalServerError)
			return
		}
		if len(groups) == 0 {
			core.RespondError(w, "no drafts to issue for this family and month", http.StatusConflict)
			return
		}
		issued, failed := issueDraftGroups(r, db, c, groups)
		if failed > 0 {
			core.RespondError(w, "could not issue the family's invoices — nothing was sent", http.StatusInternalServerError)
			return
		}
		store.SnapshotCacheInvalidateAll()
		core.Respond(w, map[string]any{"month": req.Month, "issued": issued})
	}
}
