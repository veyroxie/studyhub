package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ── Invoices ──────────────────────────────────────────────────────────────────

func listInvoices(db *DB, c *Claims) []Invoice {
	var rows *sql.Rows
	var err error
	tid := tenantID(c)
	if c != nil && c.Role == "parent" {
		// Parents are always tenant-scoped (never superadmin), so we can drop
		// the (tenant_id=? OR ?=0) pattern. The plain equality lets Postgres
		// use idx_invoices_tenant_deleted instead of falling back to a scan.
		rows, err = db.Query(`SELECT i.id,i.student_id,i.description,i.type,i.amount,i.due_date,i.status,i.created_on,i.paid_on,COALESCE(i.payment_proof,''),COALESCE(i.payment_method,''),COALESCE(i.discount_pct,0),COALESCE(i.submitted_by_parent,false),COALESCE(i.sibling_ids,''),COALESCE(i.sibling_discount,0),COALESCE(i.referral_credit,0),COALESCE(i.reference_no,'') FROM invoices i JOIN students s ON s.id=i.student_id WHERE s.contact=? AND s.tenant_id=? AND i.tenant_id=? AND i.deleted_at IS NULL ORDER BY i.created_on DESC`, c.Email, tid, tid)
	} else {
		tw, twArgs := scopeTenant(c, "")
		rows, err = db.Query(`SELECT id,student_id,description,type,amount,due_date,status,created_on,paid_on,COALESCE(payment_proof,''),COALESCE(payment_method,''),COALESCE(discount_pct,0),COALESCE(submitted_by_parent,false),COALESCE(sibling_ids,''),COALESCE(sibling_discount,0),COALESCE(referral_credit,0),COALESCE(reference_no,'') FROM invoices WHERE deleted_at IS NULL`+tw+` ORDER BY created_on DESC`, twArgs...)
	}
	if err != nil {
		return []Invoice{}
	}
	defer rows.Close()
	out := []Invoice{}
	for rows.Next() {
		var inv Invoice
		var paidOn sql.NullString
		if err := rows.Scan(&inv.ID, &inv.StudentID, &inv.Description, &inv.Type, &inv.Amount, &inv.DueDate, &inv.Status, &inv.CreatedOn, &paidOn, &inv.PaymentProof, &inv.PaymentMethod, &inv.DiscountPct, &inv.SubmittedByParent, &inv.SiblingIds, &inv.SiblingDiscount, &inv.ReferralCredit, &inv.ReferenceNo); err != nil {
			continue
		}
		if paidOn.Valid {
			inv.PaidOn = &paidOn.String
		}
		out = append(out, inv)
	}
	return out
}

func listInvoicesPaged(db *DB, c *Claims, p Pagination) ([]Invoice, int) {
	tid := tenantID(c)
	var total int
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		db.QueryRow(`SELECT COUNT(*) FROM invoices i JOIN students s ON s.id=i.student_id WHERE s.contact=? AND s.tenant_id=? AND i.tenant_id=? AND i.deleted_at IS NULL`, c.Email, tid, tid).Scan(&total)
		rows, err = db.Query(`SELECT i.id,i.student_id,i.description,i.type,i.amount,i.due_date,i.status,i.created_on,i.paid_on,COALESCE(i.payment_proof,''),COALESCE(i.payment_method,''),COALESCE(i.discount_pct,0),COALESCE(i.submitted_by_parent,false),COALESCE(i.sibling_ids,''),COALESCE(i.sibling_discount,0),COALESCE(i.referral_credit,0),COALESCE(i.reference_no,'') FROM invoices i JOIN students s ON s.id=i.student_id WHERE s.contact=? AND s.tenant_id=? AND i.tenant_id=? AND i.deleted_at IS NULL ORDER BY i.created_on DESC LIMIT ? OFFSET ?`, c.Email, tid, tid, p.Limit, p.Offset)
	} else {
		tw, twArgs := scopeTenant(c, "")
		db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE deleted_at IS NULL`+tw, twArgs...).Scan(&total)
		pageArgs := append(append([]any{}, twArgs...), p.Limit, p.Offset)
		rows, err = db.Query(`SELECT id,student_id,description,type,amount,due_date,status,created_on,paid_on,COALESCE(payment_proof,''),COALESCE(payment_method,''),COALESCE(discount_pct,0),COALESCE(submitted_by_parent,false),COALESCE(sibling_ids,''),COALESCE(sibling_discount,0),COALESCE(referral_credit,0),COALESCE(reference_no,'') FROM invoices WHERE deleted_at IS NULL`+tw+` ORDER BY created_on DESC LIMIT ? OFFSET ?`, pageArgs...)
	}
	if err != nil {
		return []Invoice{}, total
	}
	defer rows.Close()
	out := []Invoice{}
	for rows.Next() {
		var inv Invoice
		var paidOn sql.NullString
		if err := rows.Scan(&inv.ID, &inv.StudentID, &inv.Description, &inv.Type, &inv.Amount, &inv.DueDate, &inv.Status, &inv.CreatedOn, &paidOn, &inv.PaymentProof, &inv.PaymentMethod, &inv.DiscountPct, &inv.SubmittedByParent, &inv.SiblingIds, &inv.SiblingDiscount, &inv.ReferralCredit, &inv.ReferenceNo); err != nil {
			continue
		}
		if paidOn.Valid {
			inv.PaidOn = &paidOn.String
		}
		out = append(out, inv)
	}
	return out, total
}

func handleInvoices(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			p := parsePagination(r)
			if !p.Active {
				respond(w, listInvoices(db, c))
				return
			}
			data, total := listInvoicesPaged(db, c, p)
			respond(w, PaginatedResponse{Data: data, Total: total, Limit: p.Limit, Offset: p.Offset})
		case http.MethodPost:
			if !isAdminRole(c) {
				respondError(w, "admin only", 403)
				return
			}
			var inv Invoice
			if err := json.NewDecoder(r.Body).Decode(&inv); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			if msg := validationError("studentId", inv.StudentID, "description", inv.Description, "dueDate", inv.DueDate); msg != "" {
				respondError(w, msg, 400)
				return
			}
			if !validAmount(inv.Amount) {
				respondError(w, "amount must be greater than 0", 400)
				return
			}
			if inv.DiscountPct < 0 || inv.DiscountPct > 100 {
				respondError(w, "discountPct must be between 0 and 100", 400)
				return
			}
			if inv.SiblingDiscount < 0 {
				respondError(w, "siblingDiscount cannot be negative", 400)
				return
			}
			if inv.ReferralCredit < 0 {
				respondError(w, "referralCredit cannot be negative", 400)
				return
			}
			if inv.ID == "" {
				inv.ID = generateID("INV")
			}
			if inv.CreatedOn == "" {
				inv.CreatedOn = today()
			}
			inv.Status = "Unpaid"
			tid := tenantID(c)

			// Server-side referral credit validation: if the client claims a
			// referral credit, verify the student's family actually has an
			// earned reward with remaining credits. Zero the credit only when
			// the family genuinely has no rewards — not on transient DB errors.
			if inv.ReferralCredit > 0 {
				tw, twArgs := scopeTenant(c, "")
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

			if _, err := db.Exec(`INSERT INTO invoices(id,tenant_id,student_id,description,type,amount,due_date,status,created_on,paid_on,payment_method,discount_pct,submitted_by_parent,sibling_ids,sibling_discount,referral_credit,reference_no) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				inv.ID, tid, inv.StudentID, inv.Description, inv.Type, inv.Amount, inv.DueDate, inv.Status, inv.CreatedOn, nil, inv.PaymentMethod, inv.DiscountPct, inv.SubmittedByParent, inv.SiblingIds, inv.SiblingDiscount, inv.ReferralCredit, inv.ReferenceNo); err != nil {
				respondError(w, "could not create invoice", 500)
				return
			}
			logAudit(db, c.Email, "invoice_created", "invoice", inv.ID, inv.StudentID+" "+inv.Description)
			respond(w, inv)
		}
	}
}

// handleInvoiceUpdate edits the safe, admin-facing fields of an invoice:
// description, type, amount, due date and the issue date (created_on). It does
// NOT touch status/paid_on/referral — those have dedicated payment flows.
// Admin-only. Replaces the old frontend-only edit that never persisted.
func handleInvoiceUpdate(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if !isAdminRole(c) {
			respondError(w, "admin only", http.StatusForbidden)
			return
		}
		var inv Invoice
		if err := json.NewDecoder(r.Body).Decode(&inv); err != nil {
			respondError(w, "bad body", http.StatusBadRequest)
			return
		}
		if msg := validationError("description", inv.Description, "dueDate", inv.DueDate); msg != "" {
			respondError(w, msg, http.StatusBadRequest)
			return
		}
		if !validAmount(inv.Amount) {
			respondError(w, "amount must be greater than 0", http.StatusBadRequest)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := scopeTenant(c, "")
		args := append([]any{inv.Description, inv.Type, inv.Amount, inv.DueDate, inv.CreatedOn, id}, twArgs...)
		res, err := db.Exec(`UPDATE invoices SET description=?, type=?, amount=?, due_date=?, created_on=? WHERE id=?`+tw+` AND deleted_at IS NULL`, args...)
		if err != nil {
			respondError(w, "could not update invoice", http.StatusInternalServerError)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			respondError(w, "invoice not found", http.StatusNotFound)
			return
		}
		inv.ID = id
		logAudit(db, c.Email, "invoice_updated", "invoice", id, inv.Description+" RM"+fmt.Sprintf("%.2f", inv.Amount))
		respond(w, inv)
	}
}

// handleInvoiceDelete soft-deletes an invoice. Admin-only. Used by the admin
// UI when an invoice was created in error or needs voiding — refund logic
// (returning money to the parent) is out of scope and handled by admin
// externally. Audit log records the actor for post-hoc review.
func handleInvoiceDelete(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if !isAdminRole(c) {
			respondError(w, "admin only", http.StatusForbidden)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := scopeTenant(c, "")
		// Capture the invoice's state for the audit trail BEFORE soft-delete.
		var studentID, status string
		var amount float64
		readArgs := append([]any{id}, twArgs...)
		if err := db.QueryRow(`SELECT student_id, COALESCE(status,''), COALESCE(amount,0) FROM invoices WHERE id=? AND deleted_at IS NULL`+tw, readArgs...).Scan(&studentID, &status, &amount); err != nil {
			respondError(w, "invoice not found", http.StatusNotFound)
			return
		}
		args := append([]any{id}, twArgs...)
		res, err := db.Exec(`UPDATE invoices SET deleted_at=NOW() WHERE id=?`+tw+` AND deleted_at IS NULL`, args...)
		if err != nil {
			respondError(w, "could not delete invoice", http.StatusInternalServerError)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			respondError(w, "invoice not found", http.StatusNotFound)
			return
		}
		detailBytes, _ := json.Marshal(map[string]any{
			"studentId": studentID,
			"status":    status,
			"amount":    amount,
		})
		logAudit(db, c.Email, "invoice_deleted", "invoice", id, string(detailBytes))
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleInvoicePay(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil {
			respondError(w, "auth required", http.StatusUnauthorized)
			return
		}
		// Only parents (self-pay submission) and admins can hit this route.
		// Teachers and any other role are explicitly rejected — previously
		// they bypassed the parent-ownership check and could mark any
		// invoice in the tenant as Paid.
		if c.Role != "admin" && c.Role != "superadmin" && c.Role != "parent" {
			respondError(w, "admin only", http.StatusForbidden)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := scopeTenant(c, "")

		// Verify invoice exists in caller's tenant and check ownership for parents.
		var studentID string
		var amount float64
		var existingMethod, existingRef string
		invArgs := append([]any{id}, twArgs...)
		if err := db.QueryRow(`SELECT student_id, amount, COALESCE(payment_method,''), COALESCE(reference_no,'') FROM invoices WHERE id=? AND deleted_at IS NULL`+tw, invArgs...).Scan(&studentID, &amount, &existingMethod, &existingRef); err != nil {
			respondError(w, "invoice not found", 404)
			return
		}
		if c.Role == "parent" {
			var ownerEmail string
			stuArgs := append([]any{studentID}, twArgs...)
			if err := db.QueryRow(`SELECT contact FROM students WHERE id=?`+tw, stuArgs...).Scan(&ownerEmail); err != nil {
				logFromReq(r).Error("failed to look up student contact for invoice ownership", "err", err, "student_id", studentID)
			}
			if ownerEmail != c.Email {
				respondError(w, "not your invoice", 403)
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
			respondError(w, "bad request body", http.StatusBadRequest)
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
				respondError(w, "invalid status", http.StatusBadRequest)
				return
			}
			if c.Role == "parent" && body.Status != "Pending Verification" {
				respondError(w, "parents may only submit payment for verification", http.StatusForbidden)
				return
			}
			newStatus = body.Status
		} else if c.Role == "parent" {
			// Parents with empty body cannot self-mark Paid — they must
			// explicitly submit "Pending Verification".
			respondError(w, "parents must submit status=Pending Verification", http.StatusBadRequest)
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
			respondError(w, "reference number required for "+effectiveMethod, http.StatusBadRequest)
			return
		}

		t := today()
		args := append([]any{newStatus, t, body.PaymentMethod, body.ReferenceNo, id}, twArgs...)
		if _, err := db.Exec(`UPDATE invoices SET status=?, paid_on=?, payment_method=COALESCE(NULLIF(?,''),payment_method), reference_no=COALESCE(NULLIF(?,''),reference_no) WHERE id=?`+tw, args...); err != nil {
			respondError(w, "could not update invoice", 500)
			return
		}
		detailBytes, _ := json.Marshal(map[string]any{
			"studentId": studentID,
			"amount":    amount,
			"paidOn":    t,
			"method":    body.PaymentMethod,
		})
		logAudit(db, c.Email, "invoice_paid", "invoice", id, string(detailBytes))

		// Referral milestone: re-evaluate the referred student's progress.
		// Only relevant for Monthly invoices, but the helper checks itself.
		if newStatus == "Paid" {
			referralCheckMilestoneOnPay(db, studentID, c)
		}

		// Send a payment-received confirmation email to the parent when THEY
		// submit payment (not when admin marks it paid from the admin panel).
		// This closes the "did you get my money?" feedback loop.
		if c.Role == "parent" {
			var description string
			descArgs := append([]any{id}, twArgs...)
			if err := db.QueryRow(`SELECT description FROM invoices WHERE id=?`+tw, descArgs...).Scan(&description); err != nil {
				description = "Invoice " + id
			}
			go func() {
				if err := mailer.Send(c.Email, "Payment received — "+description, renderPaymentReceivedEmail(
					c.Name, description, fmt.Sprintf("%.2f", amount), body.PaymentMethod,
				)); err != nil {
					logger.Error("payment confirmation email failed", "err", err, "email", c.Email, "invoice_id", id)
				}
			}()
		}

		respond(w, map[string]string{"status": newStatus, "paidOn": t})
	}
}
