package store

import (
	"database/sql"
	"strings"

	"studyhub/internal/core"
	"studyhub/internal/models"
)

// PaymentChange is one move of an invoice's payment state.
type PaymentChange struct {
	Status    string
	Method    string
	Reference string
	Today     string
	ByParent  bool
	// Note is why a payment went back to Unpaid, shown to the parent; other moves clear it.
	Note string
}

// PaymentNoteMaxLen keeps a rejection reason to a sentence or two.
const PaymentNoteMaxLen = 300

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// legacyPendingStatus predates "Pending Verification"; old rows can still be settled, nothing sets it.
const legacyPendingStatus = "Pending"

// openStatuses are issued and not yet settled. Overdue is derived from the due date, never written.
var openStatuses = []string{
	models.InvoiceStatusUnpaid, models.InvoiceStatusOverdue,
	models.InvoiceStatusPendingVerification, legacyPendingStatus,
}

// paymentSources lists, per destination, the statuses an invoice may move from.
// Paid only ever leaves to Unpaid: a reversal, which surrenders the receipt.
func paymentSources(to string, byParent bool) []string {
	if byParent {
		if to == models.InvoiceStatusPendingVerification {
			return openStatuses
		}
		return nil
	}
	switch to {
	case models.InvoiceStatusPaid, models.InvoiceStatusUnpaid:
		return append([]string{models.InvoiceStatusPaid}, openStatuses...)
	case models.InvoiceStatusPendingVerification:
		return openStatuses
	}
	return nil
}

// PaymentMoveAllowed reports whether an invoice stored as `from` may move to `to`.
func PaymentMoveAllowed(from, to string, byParent bool) bool {
	for _, s := range paymentSources(to, byParent) {
		if s == from {
			return true
		}
	}
	return false
}

// paymentGuard mirrors PaymentMoveAllowed in SQL, so a status that moved after it was read still cannot.
func paymentGuard(to string, byParent bool) (string, []any) {
	sources := paymentSources(to, byParent)
	if len(sources) == 0 {
		return ` AND FALSE`, nil
	}
	args := make([]any, len(sources))
	for i, s := range sources {
		args[i] = s
	}
	return ` AND status IN (?` + strings.Repeat(`,?`, len(sources)-1) + `)`, args
}

// RecordInvoicePayment applies p to one invoice and reports whether the row changed.
// tw/twArgs is the caller's tenant scope; ex is the pool or the caller's transaction.
func RecordInvoicePayment(ex execer, tw string, twArgs []any, invoiceID string, p PaymentChange) (bool, error) {
	// Re-paying a Paid invoice is a no-op, so the original paid_on and receipt survive.
	paidGuard := ""
	if p.Status == models.InvoiceStatusPaid {
		paidGuard = ` AND status<>'` + models.InvoiceStatusPaid + `'`
	}
	submitClause := ""
	if p.ByParent {
		submitClause = ", submitted_by_parent=TRUE"
	}
	// Reversing to Unpaid undoes what becoming Paid did; the receipt number is surrendered, never reused.
	note := ""
	if p.Status == models.InvoiceStatusUnpaid {
		note = p.Note
	}
	guard, guardArgs := paymentGuard(p.Status, p.ByParent)
	args := append([]any{p.Status, p.Status, p.Today, p.Status, p.Status, p.Status, p.Method, p.Status, p.Reference, note, p.Status, p.Today, invoiceID}, twArgs...)
	args = append(args, guardArgs...)
	res, err := ex.Exec(`UPDATE invoices SET status=?,
		paid_on=CASE WHEN ?='Paid' THEN ? WHEN ?='Unpaid' THEN NULL ELSE paid_on END,
		receipt_no=CASE WHEN ?='Unpaid' THEN '' ELSE receipt_no END,
		payment_method=CASE WHEN ?='Unpaid' THEN '' ELSE COALESCE(NULLIF(?,''),payment_method) END,
		reference_no=CASE WHEN ?='Unpaid' THEN '' ELSE COALESCE(NULLIF(?,''),reference_no) END,
		payment_note=?,
		early_bird_cutoff=CASE WHEN ? IN ('Paid','Pending Verification') AND COALESCE(early_bird_cutoff,'')<>'' AND ? <= early_bird_cutoff THEN '' ELSE early_bird_cutoff END`+submitClause+
		` WHERE id=? AND deleted_at IS NULL`+tw+paidGuard+guard, args...)
	if err != nil {
		return false, err
	}
	changed, _ := res.RowsAffected()
	if p.Status != models.InvoiceStatusPaid {
		return changed > 0, nil
	}
	// Runs on a no-op re-pay too, so a Paid invoice from before receipt numbers still gets one.
	rcptArgs := append([]any{invoiceID}, twArgs...)
	if _, err := ex.Exec(`UPDATE invoices SET receipt_no='RCPT-'||lpad(nextval('receipt_no_seq')::text,6,'0') WHERE id=? AND status='Paid' AND (receipt_no IS NULL OR receipt_no='')`+tw, rcptArgs...); err != nil {
		// The payment itself is recorded; a missing receipt number must not undo it.
		core.Logger.Error("failed to assign receipt number", "err", err, "invoice_id", invoiceID)
	}
	return changed > 0, nil
}
