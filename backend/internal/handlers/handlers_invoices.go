package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
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
		inv.Status = store.DisplayStatusLocal(inv.Status, inv.DueDate)
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
		inv.Status = store.DisplayStatusLocal(inv.Status, inv.DueDate)
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
			// A client may ask for a draft; everything else is issued as Unpaid.
			// The status is not otherwise taken from the request: a caller must
			// not be able to create an invoice that is already Paid.
			if inv.Status != models.InvoiceStatusDraft {
				inv.Status = models.InvoiceStatusUnpaid
			}
			tid, tOK := writeTenant(w, c)
			if !tOK {
				return
			}

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

			newPeriod := monthlyPeriod(inv.Type, inv.CreatedOn)
			ebCutoff, ebDiscount := earlyBirdFromLines(inv.Type, newPeriod, inv.LineItems)
			// Written as a draft and issued in the same transaction, so an
			// invoice is never half a document: it has a number and an issue
			// date, or it does not exist. Creating still issues immediately --
			// nothing about the admin's flow changes here -- but the transition
			// is the real one, exercised on every create rather than waiting
			// unused until the review screen needs it.
			tx, err := db.BeginTx(r.Context())
			if err != nil {
				core.RespondError(w, "server error", 500)
				return
			}
			defer tx.Rollback()

			if _, err := tx.Exec(`INSERT INTO invoices(id,tenant_id,student_id,description,type,amount,due_date,status,created_on,paid_on,payment_method,discount_pct,submitted_by_parent,sibling_ids,sibling_discount,referral_credit,reference_no,line_items,period,early_bird_cutoff,early_bird_discount) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				inv.ID, tid, inv.StudentID, inv.Description, inv.Type, inv.Amount, inv.DueDate, models.InvoiceStatusDraft, inv.CreatedOn, nil, inv.PaymentMethod, inv.DiscountPct, inv.SubmittedByParent, inv.SiblingIds, inv.SiblingDiscount, inv.ReferralCredit, inv.ReferenceNo, models.MarshalLineItems(inv.LineItems), newPeriod, ebCutoff, ebDiscount); err != nil {
				// One monthly invoice per student per month (migration 0039).
				// Without this the admin gets an opaque 500 for a situation that
				// has an obvious explanation and an obvious fix.
				if strings.Contains(err.Error(), "idx_invoices_monthly_unique") {
					core.RespondError(w, "This student already has a monthly invoice for "+monthlyPeriod(inv.Type, inv.CreatedOn)+". Edit that invoice, or set a different issue date.", http.StatusConflict)
					return
				}
				core.RespondError(w, "could not create invoice", 500)
				return
			}
			// Asking for a Draft keeps it one: unnumbered, freely editable, and
			// issued later by an explicit act. Anything else is issued now, so
			// the existing flows are unchanged. This is the loop the line-item
			// editor needs -- an issued invoice is frozen (ADR-016), so without
			// a draft state the only way to correct a figure would be to reissue
			// and burn a number over a typo.
			var number string
			if inv.Status != models.InvoiceStatusDraft {
				number, err = store.IssueInvoice(tx, c, inv.ID, inv.Status)
				if err != nil {
					core.LogFromReq(r).Error("could not issue invoice", "err", err, "invoice_id", inv.ID)
					core.RespondError(w, "could not create invoice", 500)
					return
				}
			}
			if err := tx.Commit(); err != nil {
				core.RespondError(w, "could not create invoice", 500)
				return
			}
			inv.InvoiceNo = number
			action := "invoice_issued"
			if number == "" {
				action = "invoice_drafted"
			} else {
				inv.IssuedAt = core.Today()
			}
			core.LogAudit(db, store.TenantID(c), c.Email, action, "invoice", inv.ID, number+" "+inv.StudentID+" "+inv.Description)
			core.Respond(w, inv)
		}
	}
}

// monthlyPeriod is the billing month a Monthly invoice covers, taken from its
// issue date. Only Monthly invoices carry one: a registration fee or a
// self-study overflow line is not "for" a month, and giving it a period would
// put it in the way of the monthly run's one-per-student-per-month rule.
// Invoice statuses whose transitions carry side effects: becoming Paid draws a
// receipt number, and leaving Paid has to give it back. "Pending" and
// "Pending Verification" are also valid statuses but change nothing on their own.
const (
	invoiceStatusPaid   = "Paid"
	invoiceStatusUnpaid = "Unpaid"
)

// invoiceAuditAction names the audit row after the transition it records, so
// the trail distinguishes a payment from its reversal.
func invoiceAuditAction(newStatus string) string {
	switch newStatus {
	case invoiceStatusPaid:
		return "invoice_paid"
	case invoiceStatusUnpaid:
		return "invoice_payment_reversed"
	}
	return "invoice_status_changed"
}

// earlyBirdFromLines reads the clawback terms off an invoice's own line items.
//
// The monthly run records early_bird_cutoff and early_bird_discount directly. A
// hand-made invoice carried the discount only as a line -- or, before the
// editor offered one, only as a smaller number typed into the amount -- so the
// hourly expiry job matched nothing and the RM10 stood whether or not the
// parent paid by the 7th. Every September invoice in production was in that
// state.
//
// The line is the signal, never the amount: EarlyBirdRM and ReferralMonthlyRM
// are both 10.00, so an invoice sitting RM10 under the catalogue price could
// equally be a referral, and clawing that back would be wrong.
//
// The cutoff is the 7th of the invoice's own period, not of today, so editing
// an old invoice cannot move a deadline that has already passed. Arming a
// cutoff already in the past is deliberate: the early bird IS "paid by the
// 7th", so an unpaid invoice past it should lose the discount on the next run.
func earlyBirdFromLines(invoiceType, period string, items []models.InvoiceLineItem) (string, float64) {
	if invoiceType != "Monthly" || len(period) < 7 {
		return "", 0
	}
	for _, it := range items {
		if it.Kind == models.LineItemKindDiscount && strings.HasPrefix(it.Name, models.EarlyBirdLinePrefix) {
			return period + "-07", math.Abs(it.Amount)
		}
	}
	return "", 0
}

func monthlyPeriod(invoiceType, createdOn string) string {
	if invoiceType != "Monthly" || len(createdOn) < 7 {
		return ""
	}
	return createdOn[:7]
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
		// Editing the itemisation, mirroring create: the total is derived from
		// the lines and the client's amount is ignored, so NormalizeLineItems
		// stays the single authority on what an invoice adds up to. Until this
		// existed the only way to change an invoice was to overwrite its total,
		// which then WIPED the breakdown (see the CASE below) -- so correcting
		// a figure silently destroyed the itemisation the PDF prints.
		editingItems := len(inv.LineItems) > 0
		if editingItems {
			inv.Amount = models.NormalizeLineItems(inv.LineItems)
			if inv.Description == "" {
				inv.Description = models.LineItemsSummary(inv.LineItems)
			}
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

		var curStatus, curType, curDueDate, curCreatedOn, curPeriod string
		var curAmount float64
		selArgs := append([]any{id}, twArgs...)
		if err := db.QueryRow(`SELECT status, amount, type, COALESCE(due_date,''), COALESCE(created_on,''), COALESCE(period,'') FROM invoices WHERE id=?`+tw+` AND deleted_at IS NULL`, selArgs...).
			Scan(&curStatus, &curAmount, &curType, &curDueDate, &curCreatedOn, &curPeriod); err != nil {
			core.RespondError(w, "invoice not found", http.StatusNotFound)
			return
		}
		// An ISSUED invoice is a document the parent already holds, and it is
		// frozen (ADR-016) -- not just a paid one. Repricing a paid invoice would
		// serve its receipt number against a figure never collected; repricing an
		// unpaid one changes what someone was asked for with no record that it
		// moved. Corrections go through reissue, which voids this one and issues
		// a replacement carrying its own number.
		//
		// What is frozen is the money and the dates, not the wording: the edit
		// modal resubmits the existing line items on every save, so rejecting on
		// `editingItems` would block fixing a typo on any invoice with a
		// breakdown, which is most of them. A breakdown edit that leaves the
		// total unchanged passes.
		if curStatus != models.InvoiceStatusDraft && (round2cmp(inv.Amount) != round2cmp(curAmount) ||
			inv.Type != curType || inv.DueDate != curDueDate || inv.CreatedOn != curCreatedOn) {
			core.RespondError(w, "this invoice has been issued — reissue it to change the money or the dates, or edit only its description", http.StatusConflict)
			return
		}
		// period is the monthly run's dedup key. It still has to follow a TYPE
		// change, or an invoice reclassified as Monthly would be invisible to the
		// duplicate check -- but once set it must not move, because a back-dated
		// created_on vacates the month and the next run inside the 1-7 window
		// bills that student again. Hand-made, back-dated invoices are routine.
		// An existing period is never moved and never cleared -- not by a
		// back-dated created_on, and not by a type whose monthlyPeriod is empty
		// (Self-study Overflow carries a period too, set by the cron). An empty
		// one is still filled, so reclassifying an invoice as Monthly gives the
		// duplicate check something to see.
		newPeriod := curPeriod
		if newPeriod == "" {
			newPeriod = monthlyPeriod(inv.Type, inv.CreatedOn)
		}
		// Only clear line items when the amount actually changed: a manual amount
		// override makes the itemisation inconsistent, but a description/due-date
		// edit must preserve the breakdown the PDF and detail view rely on. The
		// CASE compares the pre-update amount (SET RHS sees old row values).
		var res sql.Result
		var err error
		if editingItems {
			// Lines were sent, so they ARE the new breakdown and the total came
			// from them. No CASE: there is nothing inconsistent to clear.
			ebCutoff, ebDiscount := earlyBirdFromLines(inv.Type, newPeriod, inv.LineItems)
			itemArgs := append([]any{inv.Description, inv.Type, inv.Amount, inv.DueDate, inv.CreatedOn,
				newPeriod, models.MarshalLineItems(inv.LineItems), ebCutoff, ebDiscount, id}, twArgs...)
			res, err = db.Exec(`UPDATE invoices SET description=?, type=?, amount=?, due_date=?, created_on=?, period=?, line_items=?, early_bird_cutoff=?, early_bird_discount=? WHERE id=?`+tw+` AND deleted_at IS NULL`, itemArgs...)
		} else {
			// The early-bird fields follow line_items exactly: an amount override
			// wipes the breakdown, which takes the early-bird line with it, so
			// the clawback terms must go too or they would outlive their evidence.
			args := append([]any{inv.Description, inv.Type, inv.Amount, inv.DueDate, inv.CreatedOn, newPeriod, inv.Amount, inv.Amount, inv.Amount, id}, twArgs...)
			res, err = db.Exec(`UPDATE invoices SET description=?, type=?, amount=?, due_date=?, created_on=?, period=?, line_items=CASE WHEN ROUND(amount::numeric,2)<>ROUND(?::numeric,2) THEN '[]' ELSE line_items END, early_bird_cutoff=CASE WHEN ROUND(amount::numeric,2)<>ROUND(?::numeric,2) THEN '' ELSE early_bird_cutoff END, early_bird_discount=CASE WHEN ROUND(amount::numeric,2)<>ROUND(?::numeric,2) THEN 0 ELSE early_bird_discount END WHERE id=?`+tw+` AND deleted_at IS NULL`, args...)
		}
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
		// Deleting a paid invoice drops the referral count exactly as reversing
		// one does, so it has to re-derive the reward too. Bulk delete refuses
		// Paid invoices outright and needs no equivalent.
		if status == invoiceStatusPaid {
			store.ReferralReconcile(db, studentID, c)
		}
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
			// "Unpaid" is the schema default and the status two buttons in
			// billing.js already send ("Reject (Mark Unpaid)" and "Mark
			// Unpaid"). Leaving it out meant an invoice marked Paid in error,
			// or a bogus parent payment claim, could never be reversed: this
			// endpoint was the only route to status, and HandleInvoiceUpdate
			// deliberately never touches it.
			allowed := map[string]bool{
				invoiceStatusPaid:      true,
				"Pending Verification": true,
				"Pending":              true,
				"Overdue":              true,
				invoiceStatusUnpaid:    true,
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
		// Reversing out of Paid has to undo what becoming Paid did. Widening
		// the whitelist alone would have left an "Unpaid" invoice still
		// carrying its paid date, its receipt number and the payment method --
		// and the receipt PDF renders off those. The number is surrendered, not
		// reused: the next Paid transition draws a fresh one from the sequence,
		// so a receipt number is never issued twice for different money.
		args := append([]any{newStatus, newStatus, t, newStatus, newStatus, newStatus, body.PaymentMethod, newStatus, body.ReferenceNo, id}, twArgs...)
		res, err := db.Exec(`UPDATE invoices SET status=?,
			paid_on=CASE WHEN ?='Paid' THEN ? WHEN ?='Unpaid' THEN NULL ELSE paid_on END,
			receipt_no=CASE WHEN ?='Unpaid' THEN '' ELSE receipt_no END,
			payment_method=CASE WHEN ?='Unpaid' THEN '' ELSE COALESCE(NULLIF(?,''),payment_method) END,
			reference_no=CASE WHEN ?='Unpaid' THEN '' ELSE COALESCE(NULLIF(?,''),reference_no) END`+submitClause+` WHERE id=?`+tw+paidGuard, args...)
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
		// The audit row has to say what happened, not that the endpoint ran.
		// This logged "invoice_paid" with today as paidOn for every status,
		// so reversing a payment recorded a payment, and a no-op re-pay
		// recorded a second one.
		if rowsChanged > 0 {
			detail := map[string]any{
				"studentId": studentID,
				"amount":    amount,
				"status":    newStatus,
			}
			if newStatus == invoiceStatusPaid {
				detail["paidOn"] = t
				detail["method"] = body.PaymentMethod
			}
			detailBytes, _ := json.Marshal(detail)
			core.LogAudit(db, store.TenantID(c), c.Email, invoiceAuditAction(newStatus), "invoice", id, string(detailBytes))
		}

		// Referral milestone: re-derive the referred student's progress. Both
		// directions, because reversing the invoice that completed a milestone
		// has to re-open it. Gated on rowsChanged so a no-op cannot move it.
		if rowsChanged > 0 && (newStatus == invoiceStatusPaid || newStatus == invoiceStatusUnpaid) {
			store.ReferralReconcile(db, studentID, c)
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
