package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"studyhub/internal/core"
	"studyhub/internal/mailer"
	"studyhub/internal/models"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
)

// ── Invoices ──────────────────────────────────────────────────────────────────

func listInvoices(db *store.DB, c *core.Claims) []models.Invoice {
	// Billing is admin + own-family-parent only. Teachers (and any other role)
	// get nothing — they must never see another family's financial data.
	if c == nil || (c.Role != "parent" && !core.IsAdminRole(c)) {
		return []models.Invoice{}
	}
	var rows *sql.Rows
	var err error
	tid := store.TenantID(c)
	if c != nil && c.Role == "parent" {
		// Parents are always tenant-scoped (never superadmin), so we can drop
		// the (tenant_id=? OR ?=0) pattern. The plain equality lets Postgres
		// use idx_invoices_tenant_deleted instead of falling back to a scan.
		rows, err = db.Query(`SELECT i.id,i.student_id,i.description,i.type,i.amount,i.due_date,i.status,i.created_on,i.paid_on,COALESCE(i.payment_proof,''),COALESCE(i.payment_method,''),COALESCE(i.discount_pct,0),COALESCE(i.submitted_by_parent,false),COALESCE(i.sibling_ids,''),COALESCE(i.sibling_discount,0),COALESCE(i.referral_credit,0),COALESCE(i.reference_no,''),COALESCE(i.early_bird_cutoff,''),COALESCE(i.early_bird_discount,0) FROM invoices i JOIN students s ON s.id=i.student_id WHERE s.contact=? AND s.tenant_id=? AND i.tenant_id=? AND i.deleted_at IS NULL ORDER BY i.created_on DESC`, c.Email, tid, tid)
	} else {
		tw, twArgs := store.ScopeTenant(c, "")
		rows, err = db.Query(`SELECT id,student_id,description,type,amount,due_date,status,created_on,paid_on,COALESCE(payment_proof,''),COALESCE(payment_method,''),COALESCE(discount_pct,0),COALESCE(submitted_by_parent,false),COALESCE(sibling_ids,''),COALESCE(sibling_discount,0),COALESCE(referral_credit,0),COALESCE(reference_no,''),COALESCE(early_bird_cutoff,''),COALESCE(early_bird_discount,0) FROM invoices WHERE deleted_at IS NULL`+tw+` ORDER BY created_on DESC`, twArgs...)
	}
	if err != nil {
		core.Logger.Error("list query failed", "err", err, "type", "Invoice")
		return []models.Invoice{}
	}
	defer rows.Close()
	out := []models.Invoice{}
	for rows.Next() {
		var inv models.Invoice
		var paidOn sql.NullString
		if err := rows.Scan(&inv.ID, &inv.StudentID, &inv.Description, &inv.Type, &inv.Amount, &inv.DueDate, &inv.Status, &inv.CreatedOn, &paidOn, &inv.PaymentProof, &inv.PaymentMethod, &inv.DiscountPct, &inv.SubmittedByParent, &inv.SiblingIds, &inv.SiblingDiscount, &inv.ReferralCredit, &inv.ReferenceNo, &inv.EarlyBirdCutoff, &inv.EarlyBirdDiscount); err != nil {
			continue
		}
		if paidOn.Valid {
			inv.PaidOn = &paidOn.String
		}
		out = append(out, inv)
	}
	return out
}

func listInvoicesPaged(db *store.DB, c *core.Claims, p core.Pagination) ([]models.Invoice, int) {
	if c == nil || (c.Role != "parent" && !core.IsAdminRole(c)) {
		return []models.Invoice{}, 0
	}
	tid := store.TenantID(c)
	var total int
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		db.QueryRow(`SELECT COUNT(*) FROM invoices i JOIN students s ON s.id=i.student_id WHERE s.contact=? AND s.tenant_id=? AND i.tenant_id=? AND i.deleted_at IS NULL`, c.Email, tid, tid).Scan(&total)
		rows, err = db.Query(`SELECT i.id,i.student_id,i.description,i.type,i.amount,i.due_date,i.status,i.created_on,i.paid_on,COALESCE(i.payment_proof,''),COALESCE(i.payment_method,''),COALESCE(i.discount_pct,0),COALESCE(i.submitted_by_parent,false),COALESCE(i.sibling_ids,''),COALESCE(i.sibling_discount,0),COALESCE(i.referral_credit,0),COALESCE(i.reference_no,''),COALESCE(i.early_bird_cutoff,''),COALESCE(i.early_bird_discount,0) FROM invoices i JOIN students s ON s.id=i.student_id WHERE s.contact=? AND s.tenant_id=? AND i.tenant_id=? AND i.deleted_at IS NULL ORDER BY i.created_on DESC LIMIT ? OFFSET ?`, c.Email, tid, tid, p.Limit, p.Offset)
	} else {
		tw, twArgs := store.ScopeTenant(c, "")
		db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE deleted_at IS NULL`+tw, twArgs...).Scan(&total)
		pageArgs := append(append([]any{}, twArgs...), p.Limit, p.Offset)
		rows, err = db.Query(`SELECT id,student_id,description,type,amount,due_date,status,created_on,paid_on,COALESCE(payment_proof,''),COALESCE(payment_method,''),COALESCE(discount_pct,0),COALESCE(submitted_by_parent,false),COALESCE(sibling_ids,''),COALESCE(sibling_discount,0),COALESCE(referral_credit,0),COALESCE(reference_no,''),COALESCE(early_bird_cutoff,''),COALESCE(early_bird_discount,0) FROM invoices WHERE deleted_at IS NULL`+tw+` ORDER BY created_on DESC LIMIT ? OFFSET ?`, pageArgs...)
	}
	if err != nil {
		core.Logger.Error("list query failed", "err", err, "type", "Invoice")
		return []models.Invoice{}, total
	}
	defer rows.Close()
	out := []models.Invoice{}
	for rows.Next() {
		var inv models.Invoice
		var paidOn sql.NullString
		if err := rows.Scan(&inv.ID, &inv.StudentID, &inv.Description, &inv.Type, &inv.Amount, &inv.DueDate, &inv.Status, &inv.CreatedOn, &paidOn, &inv.PaymentProof, &inv.PaymentMethod, &inv.DiscountPct, &inv.SubmittedByParent, &inv.SiblingIds, &inv.SiblingDiscount, &inv.ReferralCredit, &inv.ReferenceNo, &inv.EarlyBirdCutoff, &inv.EarlyBirdDiscount); err != nil {
			continue
		}
		if paidOn.Valid {
			inv.PaidOn = &paidOn.String
		}
		out = append(out, inv)
	}
	return out, total
}

func HandleInvoices(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			p := core.ParsePagination(r)
			if !p.Active {
				core.Respond(w, listInvoices(db, c))
				return
			}
			data, total := listInvoicesPaged(db, c, p)
			core.Respond(w, core.PaginatedResponse{Data: data, Total: total, Limit: p.Limit, Offset: p.Offset})
		case http.MethodPost:
			if !core.IsAdminRole(c) {
				core.RespondError(w, "admin only", 403)
				return
			}
			var inv models.Invoice
			if err := json.NewDecoder(r.Body).Decode(&inv); err != nil {
				core.RespondError(w, "bad body", 400)
				return
			}
			// Multi-line invoices: the total is derived server-side from the
			// items (the client's amount is ignored) so it can't be tampered
			// with. Legacy single-amount posts skip this and keep the old path.
			if len(inv.LineItems) > 0 {
				inv.Amount = models.NormalizeLineItems(inv.LineItems)
				if inv.Description == "" {
					inv.Description = models.LineItemsSummary(inv.LineItems)
				}
			}
			if msg := validationError("studentId", inv.StudentID, "description", inv.Description, "dueDate", inv.DueDate); msg != "" {
				core.RespondError(w, msg, 400)
				return
			}
			if !core.ValidAmount(inv.Amount) {
				core.RespondError(w, "amount must be greater than 0", 400)
				return
			}
			if inv.DiscountPct < 0 || inv.DiscountPct > 100 {
				core.RespondError(w, "discountPct must be between 0 and 100", 400)
				return
			}
			if inv.SiblingDiscount < 0 {
				core.RespondError(w, "siblingDiscount cannot be negative", 400)
				return
			}
			if inv.ReferralCredit < 0 {
				core.RespondError(w, "referralCredit cannot be negative", 400)
				return
			}
			if inv.ID == "" {
				inv.ID = core.GenerateID("INV")
			}
			if inv.CreatedOn == "" {
				inv.CreatedOn = core.Today()
			}
			inv.Status = "Unpaid"
			tid := store.TenantID(c)

			// Server-side referral credit validation: if the client claims a
			// referral credit, verify the student's family actually has an
			// earned reward with remaining credits. Zero the credit only when
			// the family genuinely has no rewards — not on transient DB errors.
			if inv.ReferralCredit > 0 {
				tw, twArgs := store.ScopeTenant(c, "")
				var famID string
				famArgs := append([]any{inv.StudentID}, twArgs...)
				if err := db.QueryRow(`SELECT family_id FROM students WHERE id=?`+tw, famArgs...).Scan(&famID); err != nil || famID == "" {
					inv.ReferralCredit = 0
				} else {
					var earned int
					rewArgs := append([]any{famID}, twArgs...)
					if err := db.QueryRow(`SELECT COUNT(*) FROM referral_rewards WHERE referrer_family_id=? AND status='earned' AND credits_remaining > 0`+tw, rewArgs...).Scan(&earned); err == nil && earned == 0 {
						inv.ReferralCredit = 0
					}
				}
			}

			if _, err := db.Exec(`INSERT INTO invoices(id,tenant_id,student_id,description,type,amount,due_date,status,created_on,paid_on,payment_method,discount_pct,submitted_by_parent,sibling_ids,sibling_discount,referral_credit,reference_no,line_items) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				inv.ID, tid, inv.StudentID, inv.Description, inv.Type, inv.Amount, inv.DueDate, inv.Status, inv.CreatedOn, nil, inv.PaymentMethod, inv.DiscountPct, inv.SubmittedByParent, inv.SiblingIds, inv.SiblingDiscount, inv.ReferralCredit, inv.ReferenceNo, models.MarshalLineItems(inv.LineItems)); err != nil {
				core.RespondError(w, "could not create invoice", 500)
				return
			}
			core.LogAudit(db, store.TenantID(c), c.Email, "invoice_created", "invoice", inv.ID, inv.StudentID+" "+inv.Description)
			core.Respond(w, inv)
		}
	}
}

// handleInvoiceUpdate edits the safe, admin-facing fields of an invoice:
// description, type, amount, due date and the issue date (created_on). It does
// NOT touch status/paid_on/referral — those have dedicated payment flows.
// Admin-only. Replaces the old frontend-only edit that never persisted.
func HandleInvoiceUpdate(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		var inv models.Invoice
		if err := json.NewDecoder(r.Body).Decode(&inv); err != nil {
			core.RespondError(w, "bad body", http.StatusBadRequest)
			return
		}
		if msg := validationError("description", inv.Description, "dueDate", inv.DueDate); msg != "" {
			core.RespondError(w, msg, http.StatusBadRequest)
			return
		}
		if !core.ValidAmount(inv.Amount) {
			core.RespondError(w, "amount must be greater than 0", http.StatusBadRequest)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")
		// Only clear line items when the amount actually changed: a manual amount
		// override makes the itemisation inconsistent, but a description/due-date
		// edit must preserve the breakdown the PDF and detail view rely on. The
		// CASE compares the pre-update amount (SET RHS sees old row values).
		args := append([]any{inv.Description, inv.Type, inv.Amount, inv.DueDate, inv.CreatedOn, inv.Amount, id}, twArgs...)
		res, err := db.Exec(`UPDATE invoices SET description=?, type=?, amount=?, due_date=?, created_on=?, line_items=CASE WHEN ROUND(amount::numeric,2)<>ROUND(?::numeric,2) THEN '[]' ELSE line_items END WHERE id=?`+tw+` AND deleted_at IS NULL`, args...)
		if err != nil {
			core.RespondError(w, "could not update invoice", http.StatusInternalServerError)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			core.RespondError(w, "invoice not found", http.StatusNotFound)
			return
		}
		inv.ID = id
		core.LogAudit(db, store.TenantID(c), c.Email, "invoice_updated", "invoice", id, inv.Description+" RM"+fmt.Sprintf("%.2f", inv.Amount))
		core.Respond(w, inv)
	}
}

// handleInvoiceDelete soft-deletes an invoice. Admin-only. Used by the admin
// UI when an invoice was created in error or needs voiding — refund logic
// (returning money to the parent) is out of scope and handled by admin
// externally. Audit log records the actor for post-hoc review.
func HandleInvoiceDelete(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")
		// Capture the invoice's state for the audit trail BEFORE soft-delete.
		var studentID, status string
		var amount float64
		readArgs := append([]any{id}, twArgs...)
		if err := db.QueryRow(`SELECT student_id, COALESCE(status,''), COALESCE(amount,0) FROM invoices WHERE id=? AND deleted_at IS NULL`+tw, readArgs...).Scan(&studentID, &status, &amount); err != nil {
			core.RespondError(w, "invoice not found", http.StatusNotFound)
			return
		}
		args := append([]any{id}, twArgs...)
		res, err := db.Exec(`UPDATE invoices SET deleted_at=NOW() WHERE id=?`+tw+` AND deleted_at IS NULL`, args...)
		if err != nil {
			core.RespondError(w, "could not delete invoice", http.StatusInternalServerError)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			core.RespondError(w, "invoice not found", http.StatusNotFound)
			return
		}
		detailBytes, _ := json.Marshal(map[string]any{
			"studentId": studentID,
			"status":    status,
			"amount":    amount,
		})
		core.LogAudit(db, store.TenantID(c), c.Email, "invoice_deleted", "invoice", id, string(detailBytes))
		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleInvoicesBulkDelete soft-deletes several invoices in one request. Only
// Unpaid/Overdue invoices are removed — Paid ones are financial records and
// Pending Verification ones carry parent-submitted proof, so both are left
// untouched even when their id is in the list. Mirrors HandleInvoiceDelete's
// admin-only + soft-delete + audit behaviour.
func HandleInvoicesBulkDelete(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		var body struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
			core.RespondError(w, "no invoices selected", http.StatusBadRequest)
			return
		}
		placeholders := strings.Repeat("?,", len(body.IDs)-1) + "?"
		tw, twArgs := store.ScopeTenant(c, "")
		args := make([]any, 0, len(body.IDs)+len(twArgs))
		for _, id := range body.IDs {
			args = append(args, id)
		}
		args = append(args, twArgs...)
		// RETURNING id tells us which ones actually matched the status filter, so
		// the response can report exactly how many were deleted vs kept.
		rows, err := db.Query(`UPDATE invoices SET deleted_at=NOW() WHERE id IN (`+placeholders+`) AND status IN ('Unpaid','Overdue') AND deleted_at IS NULL`+tw+` RETURNING id`, args...)
		if err != nil {
			core.RespondError(w, "could not delete invoices", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		deleted := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err == nil {
				deleted = append(deleted, id)
			}
		}
		detail, _ := json.Marshal(map[string]any{"ids": deleted, "count": len(deleted)})
		core.LogAudit(db, store.TenantID(c), c.Email, "invoices_bulk_deleted", "invoice", "", string(detail))
		core.Respond(w, map[string]any{"deleted": len(deleted), "skipped": len(body.IDs) - len(deleted)})
	}
}

func HandleInvoicePay(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if c == nil {
			core.RespondError(w, "auth required", http.StatusUnauthorized)
			return
		}
		// Only parents (self-pay submission) and admins can hit this route.
		// Teachers and any other role are explicitly rejected — previously
		// they bypassed the parent-ownership check and could mark any
		// invoice in the tenant as Paid.
		if c.Role != "admin" && c.Role != "superadmin" && c.Role != "parent" {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")

		// Verify invoice exists in caller's tenant and check ownership for parents.
		var studentID string
		var amount float64
		var existingMethod, existingRef string
		invArgs := append([]any{id}, twArgs...)
		if err := db.QueryRow(`SELECT student_id, amount, COALESCE(payment_method,''), COALESCE(reference_no,'') FROM invoices WHERE id=? AND deleted_at IS NULL`+tw, invArgs...).Scan(&studentID, &amount, &existingMethod, &existingRef); err != nil {
			core.RespondError(w, "invoice not found", 404)
			return
		}
		if c.Role == "parent" {
			var ownerEmail string
			stuArgs := append([]any{studentID}, twArgs...)
			if err := db.QueryRow(`SELECT contact FROM students WHERE id=?`+tw, stuArgs...).Scan(&ownerEmail); err != nil {
				core.LogFromReq(r).Error("failed to look up student contact for invoice ownership", "err", err, "student_id", studentID)
			}
			if ownerEmail != c.Email {
				core.RespondError(w, "not your invoice", 403)
				return
			}
		}

		// Decode optional body (status override, payment method).
		// Body may be empty for simple mark-paid — only error on
		// genuinely malformed JSON, not EOF.
		var body struct {
			Status        string `json:"status"`
			PaymentMethod string `json:"paymentMethod"`
			ReferenceNo   string `json:"referenceNo"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
			core.RespondError(w, "bad request body", http.StatusBadRequest)
			return
		}
		newStatus := "Paid"
		if body.Status != "" {
			// Parents may only submit "Pending Verification" — admins can
			// set any whitelisted status. This prevents a parent from
			// self-marking an invoice as Paid.
			allowed := map[string]bool{
				"Paid":                 true,
				"Pending Verification": true,
				"Pending":              true,
				"Overdue":              true,
			}
			if !allowed[body.Status] {
				core.RespondError(w, "invalid status", http.StatusBadRequest)
				return
			}
			if c.Role == "parent" && body.Status != "Pending Verification" {
				core.RespondError(w, "parents may only submit payment for verification", http.StatusForbidden)
				return
			}
			newStatus = body.Status
		} else if c.Role == "parent" {
			// Parents with empty body cannot self-mark Paid — they must
			// explicitly submit "Pending Verification".
			core.RespondError(w, "parents must submit status=Pending Verification", http.StatusBadRequest)
			return
		}

		// Reference number is mandatory for non-cash payments. Resolve the
		// effective method+ref after this update (body overrides existing,
		// otherwise existing wins via COALESCE in the UPDATE below) and
		// reject when the resulting state has a non-cash method but no ref.
		// This closes the bypass where admin marked Paid with an empty body
		// on an invoice that already had method="Bank Transfer", ref="".
		effectiveMethod := body.PaymentMethod
		if effectiveMethod == "" {
			effectiveMethod = existingMethod
		}
		effectiveRef := body.ReferenceNo
		if effectiveRef == "" {
			effectiveRef = existingRef
		}
		if effectiveMethod != "" && effectiveMethod != "Cash" && effectiveRef == "" {
			core.RespondError(w, "reference number required for "+effectiveMethod, http.StatusBadRequest)
			return
		}

		t := core.Today()
		// Re-paying an already-Paid invoice must be a no-op: guard the Paid
		// transition on status<>'Paid' so the original paid_on/reference survive
		// and the referral milestone below fires at most once.
		paidGuard := ""
		if newStatus == "Paid" {
			paidGuard = " AND status<>'Paid'"
		}
		// Only stamp paid_on when actually transitioning to Paid. Otherwise a
		// parent's "Pending Verification" submission (or an admin setting
		// Overdue) would give an unpaid invoice a paid date.
		// Record that the parent themselves claimed this payment. Nothing else
		// ever set submitted_by_parent, so the confirmation email below (which
		// gates on it) could never fire, and admin had no way to tell a parent
		// claim apart from an admin-entered payment.
		submitClause := ""
		if c.Role == "parent" {
			submitClause = ", submitted_by_parent=TRUE"
		}
		args := append([]any{newStatus, newStatus, t, body.PaymentMethod, body.ReferenceNo, id}, twArgs...)
		res, err := db.Exec(`UPDATE invoices SET status=?, paid_on=CASE WHEN ?='Paid' THEN ? ELSE paid_on END, payment_method=COALESCE(NULLIF(?,''),payment_method), reference_no=COALESCE(NULLIF(?,''),reference_no)`+submitClause+` WHERE id=?`+tw+paidGuard, args...)
		if err != nil {
			core.RespondError(w, "could not update invoice", 500)
			return
		}
		rowsChanged, _ := res.RowsAffected()

		// Assign a receipt number the first time an invoice becomes Paid. Drawn
		// from receipt_no_seq so numbers are monotonic (RCPT-000001, ...). The
		// guard on status='Paid' AND empty receipt_no makes this idempotent —
		// re-paying an already-paid invoice keeps the original receipt number.
		if newStatus == "Paid" {
			rcptArgs := append([]any{id}, twArgs...)
			if _, err := db.Exec(`UPDATE invoices SET receipt_no='RCPT-'||lpad(nextval('receipt_no_seq')::text,6,'0') WHERE id=? AND status='Paid' AND (receipt_no IS NULL OR receipt_no='')`+tw, rcptArgs...); err != nil {
				core.LogFromReq(r).Error("failed to assign receipt number", "err", err, "invoice_id", id)
			}
		}
		detailBytes, _ := json.Marshal(map[string]any{
			"studentId": studentID,
			"amount":    amount,
			"paidOn":    t,
			"method":    body.PaymentMethod,
		})
		core.LogAudit(db, store.TenantID(c), c.Email, "invoice_paid", "invoice", id, string(detailBytes))

		// Referral milestone: re-evaluate the referred student's progress.
		// Only relevant for Monthly invoices, but the helper checks itself.
		// Gated on rowsChanged so a re-pay no-op can't double-count the milestone.
		if newStatus == "Paid" && rowsChanged > 0 {
			store.ReferralCheckMilestoneOnPay(db, studentID, c)
		}

		// Send the "payment received" confirmation only when CONFIRMING a payment
		// the parent themselves submitted (submitted_by_parent) — that's the
		// "did you get my money?" loop. Admin marking cash paid directly, and
		// bulk mark-paid, are NOT parent-submitted, so they stay silent and don't
		// blast every parent when reconciling. Recipient is the owning parent.
		if newStatus == "Paid" && rowsChanged > 0 {
			var parentEmail, parentName, description string
			var submittedByParent bool
			stuArgs := append([]any{studentID}, twArgs...)
			if err := db.QueryRow(`SELECT contact, COALESCE(parent_name,'') FROM students WHERE id=?`+tw, stuArgs...).Scan(&parentEmail, &parentName); err != nil {
				core.LogFromReq(r).Error("payment email: student lookup failed", "err", err, "invoice_id", id)
			}
			descArgs := append([]any{id}, twArgs...)
			if err := db.QueryRow(`SELECT description, COALESCE(submitted_by_parent,false) FROM invoices WHERE id=?`+tw, descArgs...).Scan(&description, &submittedByParent); err != nil {
				description = "Invoice " + id
			}
			if parentEmail != "" && submittedByParent {
				go func() {
					if err := core.SendEmail(parentEmail, "Payment received — "+description, mailer.RenderPaymentReceivedEmail(
						parentName, description, fmt.Sprintf("%.2f", amount), effectiveMethod,
					)); err != nil {
						core.Logger.Error("payment confirmation email failed", "err", err, "email", parentEmail, "invoice_id", id)
					}
				}()
			}
		}

		core.Respond(w, map[string]string{"status": newStatus, "paidOn": t})
	}
}
