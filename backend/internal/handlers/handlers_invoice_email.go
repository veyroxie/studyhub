package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

// invoiceEmailResult tells the admin whether the email will actually reach the parent:
// OUTBOUND_ALLOWLIST drops anything outside it, and a "sent" toast over a drop is a lie.
func invoiceEmailResult(to string) map[string]any {
	if core.AllowedRecipient(to) {
		return map[string]any{"queued": true, "to": to}
	}
	return map[string]any{"queued": false, "to": to,
		"reason": "outbound email is restricted to the allowlist, so this would not reach " + to}
}

// HandleInvoiceEmail sends the parent an issued invoice on demand, for invoices made
// by hand (which send nothing on their own) or a parent who lost the first email.
//
// POST /api/invoices/{id}/email
func HandleInvoiceEmail(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "i")
		var tenantID int
		var status, contact string
		err := db.QueryRow(`SELECT i.tenant_id, i.status, COALESCE(s.contact,'') FROM invoices i
			JOIN students s ON s.id=i.student_id AND s.tenant_id=i.tenant_id
			WHERE i.id=? AND i.deleted_at IS NULL`+tw, append([]any{id}, twArgs...)...).Scan(&tenantID, &status, &contact)
		if err != nil {
			core.RespondError(w, "invoice not found", http.StatusNotFound)
			return
		}
		if msg := invoiceEmailBlocker(status, contact); msg != "" {
			core.RespondError(w, msg, http.StatusConflict)
			return
		}
		queueInvoiceEmail(w, r, db, c, tenantID, contact, []string{id})
	}
}

// HandleFamilyBillEmail sends the parent every unpaid invoice in a family bill, in one email.
//
// POST /api/family-bills/{invoiceId}/email
func HandleFamilyBillEmail(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		tw, twArgs := store.ScopeTenant(c, "i")
		var tenantID int
		var period, contact string
		err := db.QueryRow(`SELECT i.tenant_id, COALESCE(i.period,''), COALESCE(s.contact,'') FROM invoices i
			JOIN students s ON s.id=i.student_id AND s.tenant_id=i.tenant_id
			WHERE i.id=? AND i.type='Monthly' AND i.deleted_at IS NULL`+tw,
			append([]any{chi.URLParam(r, "invoiceId")}, twArgs...)...).Scan(&tenantID, &period, &contact)
		if err != nil || period == "" {
			core.RespondError(w, "family bill not found", http.StatusNotFound)
			return
		}
		members, err := store.FamilyBillMembers(db, tenantID, contact, period, false)
		if err != nil {
			core.RespondError(w, "could not read the family bill", http.StatusInternalServerError)
			return
		}
		owed := store.FamilyBillTargets(members, models.InvoiceStatusPendingVerification)
		if len(owed) == 0 {
			core.RespondError(w, "everything in this family bill is already paid", http.StatusConflict)
			return
		}
		queueInvoiceEmail(w, r, db, c, tenantID, contact, invoiceIDsOf(owed))
	}
}

// Only an issued, unpaid invoice with a parent email is worth sending.
func invoiceEmailBlocker(status, contact string) string {
	if !store.IsParentVisibleStatus(status) {
		return "a draft or voided invoice cannot be sent to the parent"
	}
	if status == models.InvoiceStatusPaid {
		return "this invoice is already paid — send the receipt instead"
	}
	if strings.TrimSpace(contact) == "" {
		return "this student has no parent email on file"
	}
	return ""
}

// queueInvoiceEmail enqueues through the outbox, the same path issuing uses, so the
// email reads the invoices as they stand when it goes out.
func queueInvoiceEmail(w http.ResponseWriter, r *http.Request, db *store.DB, c *core.Claims, tenantID int, contact string, ids []string) {
	if !core.AllowedRecipient(contact) {
		core.Respond(w, invoiceEmailResult(contact))
		return
	}
	topic, payload := store.OutboxTopicInvoiceIssued, ids[0]
	if len(ids) > 1 {
		topic, payload = store.OutboxTopicFamilyBillIssued, strings.Join(ids, ",")
	}
	tx, err := db.BeginTx(r.Context())
	if err != nil {
		core.RespondError(w, "could not queue the email", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	if err := store.EnqueueOutbox(tx, tenantID, topic, payload); err != nil || tx.Commit() != nil {
		core.LogFromReq(r).Error("invoice email enqueue failed", "err", err, "invoices", ids)
		core.RespondError(w, "could not queue the email", http.StatusInternalServerError)
		return
	}
	for _, id := range ids {
		core.LogAudit(db, tenantID, c.Email, "invoice_emailed", "invoice", id, "to="+contact)
	}
	core.Respond(w, invoiceEmailResult(contact))
}
