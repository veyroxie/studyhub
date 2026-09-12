package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

// HandleInvoiceIssue turns a draft into an issued invoice: it draws the next
// gapless number, stamps the issue date, and freezes the figures.
//
// The act is explicit and one-way. Before it an invoice is working material;
// after it the parent has a numbered document, and the only correction is to
// void and reissue (ADR-016).
//
// POST /api/invoices/{id}/issue
func HandleInvoiceIssue(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		id := chi.URLParam(r, "id")
		tx, err := db.BeginTx(r.Context())
		if err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		defer tx.Rollback()

		number, err := store.IssueInvoice(tx, c, id, models.InvoiceStatusUnpaid)
		if err != nil {
			core.LogFromReq(r).Error("issue failed", "err", err, "invoice_id", id)
			core.RespondError(w, "could not issue the invoice", 500)
			return
		}
		if number == "" {
			core.RespondError(w, "this invoice is not a draft — it has already been issued", http.StatusConflict)
			return
		}
		if err := tx.Commit(); err != nil {
			core.RespondError(w, "could not issue the invoice", 500)
			return
		}
		core.LogAudit(db, store.TenantID(c), c.Email, "invoice_issued", "invoice", id, number)
		core.Respond(w, map[string]string{"id": id, "invoiceNo": number, "status": models.InvoiceStatusUnpaid, "issuedAt": core.Today()})
	}
}
