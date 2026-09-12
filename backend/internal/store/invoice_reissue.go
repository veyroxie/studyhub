package store

import (
	"context"
	"errors"
	"fmt"

	"studyhub/internal/core"
	"studyhub/internal/models"
)

// ErrInvoicePaid is returned when a correction is attempted on an invoice that
// has already been paid. Voiding it would cancel the document its receipt
// refers to, so that case stays manual (ADR-016).
var ErrInvoicePaid = errors.New("invoice is paid")

// ErrInvoiceNotFound covers gone, deleted, or already void.
var ErrInvoiceNotFound = errors.New("invoice not found")

// Reissue is the corrected invoice that replaces a voided one.
type Reissue struct {
	StudentID   string
	Description string
	Type        string
	Amount      float64
	DueDate     string
	CreatedOn   string
	Period      string
	LineItems   []models.InvoiceLineItem
	// EarlyBirdCutoff and EarlyBirdDiscount arm the clawback on the
	// replacement. Empty and zero means the replacement carries no early bird,
	// which is what a lapsed one becomes.
	EarlyBirdCutoff   string
	EarlyBirdDiscount float64
}

// ReissueInvoice voids an invoice and issues its replacement inside one
// transaction, returning the new id and number.
//
// One operation, not two, because a void without its replacement leaves a
// student with no invoice for the month and nothing to say one was intended.
// The original keeps its number and its figures and gains voided_at plus
// superseded_by, so the pair reads as a single correction.
//
// It lives in store rather than in the handler because the early-bird expiry
// job needs exactly this: a lapsed discount is a correction to an issued
// document, and editing the amount in place is the thing an issued invoice is
// not allowed to do.
func ReissueInvoice(ctx context.Context, db *DB, c *core.Claims, invoiceID string, next Reissue) (string, string, error) {
	tid, err := WriteTenantID(c)
	if err != nil {
		return "", "", err
	}
	newID := core.GenerateID("INV")
	tx, err := db.BeginTx(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`UPDATE invoices SET status=?, voided_at=?, superseded_by=?
		WHERE id=? AND tenant_id=? AND deleted_at IS NULL AND status<>? AND status<>?`,
		models.InvoiceStatusVoid, core.Today(), newID, invoiceID, tid,
		models.InvoiceStatusPaid, models.InvoiceStatusVoid)
	if err != nil {
		return "", "", fmt.Errorf("void %s: %w", invoiceID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", "", fmt.Errorf("void %s: rows affected unreadable: %w", invoiceID, err)
	}
	if n == 0 {
		return "", "", ErrInvoiceNotFound
	}

	if _, err := tx.Exec(`INSERT INTO invoices(id,tenant_id,student_id,description,type,amount,due_date,status,created_on,period,line_items,early_bird_cutoff,early_bird_discount)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		newID, tid, next.StudentID, next.Description, next.Type, next.Amount, next.DueDate,
		models.InvoiceStatusDraft, next.CreatedOn, next.Period, models.MarshalLineItems(next.LineItems),
		next.EarlyBirdCutoff, next.EarlyBirdDiscount); err != nil {
		return "", "", fmt.Errorf("reissue %s: %w", invoiceID, err)
	}
	number, err := IssueInvoice(tx, c, newID, models.InvoiceStatusUnpaid)
	if err != nil {
		return "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", err
	}
	return newID, number, nil
}
