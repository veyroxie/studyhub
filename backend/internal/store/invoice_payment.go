package store

import (
	"database/sql"

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
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// PayableFrom reports whether an invoice stored as `from` may take a payment change.
func PayableFrom(from string, byParent bool) bool {
	if from == models.InvoiceStatusDraft || from == models.InvoiceStatusVoid {
		return false
	}
	// A parent claims payment on an open bill; a confirmed one is not theirs to reopen.
	if byParent {
		return from == models.InvoiceStatusUnpaid || from == models.InvoiceStatusOverdue ||
			from == models.InvoiceStatusPendingVerification
	}
	return true
}

// Mirrors PayableFrom in SQL, so a status that moved after it was read still cannot be paid.
func payableGuard(byParent bool) string {
	if byParent {
		return ` AND status IN ('` + models.InvoiceStatusUnpaid + `','` + models.InvoiceStatusOverdue + `','` +
			models.InvoiceStatusPendingVerification + `')`
	}
	return ` AND status NOT IN ('` + models.InvoiceStatusDraft + `','` + models.InvoiceStatusVoid + `')`
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
	args := append([]any{p.Status, p.Status, p.Today, p.Status, p.Status, p.Status, p.Method, p.Status, p.Reference, invoiceID}, twArgs...)
	res, err := ex.Exec(`UPDATE invoices SET status=?,
		paid_on=CASE WHEN ?='Paid' THEN ? WHEN ?='Unpaid' THEN NULL ELSE paid_on END,
		receipt_no=CASE WHEN ?='Unpaid' THEN '' ELSE receipt_no END,
		payment_method=CASE WHEN ?='Unpaid' THEN '' ELSE COALESCE(NULLIF(?,''),payment_method) END,
		reference_no=CASE WHEN ?='Unpaid' THEN '' ELSE COALESCE(NULLIF(?,''),reference_no) END`+submitClause+
		` WHERE id=? AND deleted_at IS NULL`+tw+paidGuard+payableGuard(p.ByParent), args...)
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
