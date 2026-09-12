package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

// HandleInvoiceReissue voids an invoice and issues its replacement, in one
// transaction.
//
// This is the only correction mechanism for an issued invoice (ADR-016). It is
// one operation rather than a void followed by a create because those two must
// not be separable: a void that succeeds without its replacement leaves a
// student with no invoice for the month and no sign that one was intended.
//
// The original keeps its number and its figures -- that is the point of an
// issued document -- and gains voided_at plus superseded_by, so the pair reads
// as one correction rather than two unrelated rows.
//
// A PAID invoice is refused. Voiding it would strand the payment and the
// receipt already in the parent's hands against a cancelled document; that case
// stays manual until credit notes exist, which is the trade recorded in
// ADR-016.
//
// POST /api/invoices/{id}/reissue
func HandleInvoiceReissue(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")

		var old models.Invoice
		var lineItems, period string
		selArgs := append([]any{id}, twArgs...)
		if err := db.QueryRow(`SELECT student_id, description, type, amount, due_date, status,
			created_on, COALESCE(period,''), COALESCE(line_items,'[]'), COALESCE(invoice_no,'')
			FROM invoices WHERE id=?`+tw+` AND deleted_at IS NULL`, selArgs...).
			Scan(&old.StudentID, &old.Description, &old.Type, &old.Amount, &old.DueDate, &old.Status,
				&old.CreatedOn, &period, &lineItems, &old.InvoiceNo); err != nil {
			core.RespondError(w, "invoice not found", http.StatusNotFound)
			return
		}
		if old.Status == models.InvoiceStatusPaid {
			core.RespondError(w, "this invoice is paid — reissuing would cancel the document its receipt refers to; adjust it by hand", http.StatusConflict)
			return
		}
		if old.Status == models.InvoiceStatusVoid {
			core.RespondError(w, "this invoice is already void", http.StatusConflict)
			return
		}

		// The corrected figures, or the originals when the body is empty: a
		// clawback reissues the same invoice at a different amount, while a
		// mistyped description reissues with new wording.
		next := old
		next.LineItems = models.ParseLineItems(lineItems)
		if err := json.NewDecoder(r.Body).Decode(&next); err != nil && err.Error() != "EOF" {
			core.RespondError(w, "bad request body", http.StatusBadRequest)
			return
		}
		if len(next.LineItems) > 0 {
			next.Amount = models.NormalizeLineItems(next.LineItems)
		}
		if !core.ValidAmount(next.Amount) {
			core.RespondError(w, "an invoice needs an amount above zero", http.StatusBadRequest)
			return
		}

		ebCutoff, ebDiscount := earlyBirdFromLines(next.Type, period, next.LineItems)
		newID, number, err := store.ReissueInvoice(r.Context(), db, c, id, store.Reissue{
			StudentID: next.StudentID, Description: next.Description, Type: next.Type,
			Amount: next.Amount, DueDate: next.DueDate, CreatedOn: next.CreatedOn, Period: period,
			LineItems: next.LineItems, EarlyBirdCutoff: ebCutoff, EarlyBirdDiscount: ebDiscount,
		})
		if errors.Is(err, store.ErrInvoiceNotFound) {
			core.RespondError(w, "invoice not found", http.StatusNotFound)
			return
		}
		if err != nil {
			core.LogFromReq(r).Error("reissue failed", "err", err, "invoice_id", id)
			core.RespondError(w, "could not reissue", 500)
			return
		}

		detail, _ := json.Marshal(map[string]any{
			"voided": id, "voidedNumber": old.InvoiceNo,
			"replacement": newID, "replacementNumber": number,
			"amountWas": old.Amount, "amountNow": next.Amount,
		})
		core.LogAudit(db, store.TenantID(c), c.Email, "invoice_reissued", "invoice", id, string(detail))

		next.ID, next.InvoiceNo, next.Status, next.IssuedAt = newID, number, models.InvoiceStatusUnpaid, core.Today()
		core.Respond(w, next)
	}
}
