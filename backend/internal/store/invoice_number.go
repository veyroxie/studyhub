package store

import (
	"fmt"
	"time"

	"studyhub/internal/core"
	"studyhub/internal/models"
)

// NextInvoiceNumber allocates the next gapless invoice number for a tenant and
// year, formatted INV-2026-0001.
//
// It MUST be called inside the transaction that issues the invoice. That is the
// whole reason this is a counter row and not a Postgres sequence: nextval is
// never rolled back, so a failed issue would burn a number and leave a hole.
// An ordinary UPDATE rolls back with everything else.
//
// ON CONFLICT DO UPDATE is what makes it safe under concurrency: the update
// takes a row lock, so a second issuer waits rather than reading the same value.
// Callers queue on this row, which at one operator issuing monthly costs
// nothing and is the price of gaplessness (ADR-016).
func NextInvoiceNumber(tx *Tx, tenantID int, on time.Time) (string, error) {
	year := on.Year()
	var n int
	err := tx.QueryRow(`
		INSERT INTO invoice_number_counters(tenant_id, year, next_no) VALUES(?,?,2)
		ON CONFLICT (tenant_id, year) DO UPDATE SET next_no = invoice_number_counters.next_no + 1
		RETURNING next_no - 1`, tenantID, year).Scan(&n)
	if err != nil {
		return "", fmt.Errorf("allocate invoice number for %d: %w", year, err)
	}
	return fmt.Sprintf("INV-%d-%04d", year, n), nil
}

// IssueInvoice moves a draft to issued: it assigns the number and stamps the
// issue date, in one statement guarded on the row still being a draft so a
// second issue cannot renumber an invoice already in a parent's hands.
//
// Returns the number assigned, or an empty string if the row was not a draft
// (already issued, voided, or gone), which callers treat as "nothing to do"
// rather than an error.
func IssueInvoice(tx *Tx, c *core.Claims, invoiceID, status string) (string, error) {
	tid, err := WriteTenantID(c)
	if err != nil {
		return "", err
	}
	now := time.Now()
	number, err := NextInvoiceNumber(tx, tid, now)
	if err != nil {
		return "", err
	}
	res, err := tx.Exec(`UPDATE invoices SET invoice_no=?, issued_at=?, status=?
		WHERE id=? AND tenant_id=? AND status=? AND deleted_at IS NULL`,
		number, now.Format("2006-01-02"), status, invoiceID, tid, models.InvoiceStatusDraft)
	if err != nil {
		return "", fmt.Errorf("issue invoice %s: %w", invoiceID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("issue invoice %s: rows affected unreadable: %w", invoiceID, err)
	}
	if n == 0 {
		return "", nil
	}
	return number, nil
}
