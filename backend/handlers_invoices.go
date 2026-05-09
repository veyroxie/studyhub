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
		rows, err = db.Query(`SELECT i.id,i.student_id,i.description,i.type,i.amount,i.due_date,i.status,i.created_on,i.paid_on,COALESCE(i.payment_proof,''),COALESCE(i.payment_method,''),COALESCE(i.discount_pct,0),COALESCE(i.submitted_by_parent,false),COALESCE(i.sibling_ids,''),COALESCE(i.sibling_discount,0),COALESCE(i.referral_credit,0),COALESCE(i.reference_no,'') FROM invoices i JOIN students s ON s.id=i.student_id WHERE s.contact=? AND i.deleted_at IS NULL AND (i.tenant_id=? OR ?=0) ORDER BY i.created_on DESC`, c.Email, tid, tid)
	} else {
		rows, err = db.Query(`SELECT id,student_id,description,type,amount,due_date,status,created_on,paid_on,COALESCE(payment_proof,''),COALESCE(payment_method,''),COALESCE(discount_pct,0),COALESCE(submitted_by_parent,false),COALESCE(sibling_ids,''),COALESCE(sibling_discount,0),COALESCE(referral_credit,0),COALESCE(reference_no,'') FROM invoices WHERE deleted_at IS NULL AND (tenant_id=? OR ?=0) ORDER BY created_on DESC`, tid, tid)
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
		db.QueryRow(`SELECT COUNT(*) FROM invoices i JOIN students s ON s.id=i.student_id WHERE s.contact=? AND i.deleted_at IS NULL AND (i.tenant_id=? OR ?=0)`, c.Email, tid, tid).Scan(&total)
		rows, err = db.Query(`SELECT i.id,i.student_id,i.description,i.type,i.amount,i.due_date,i.status,i.created_on,i.paid_on,COALESCE(i.payment_proof,''),COALESCE(i.payment_method,''),COALESCE(i.discount_pct,0),COALESCE(i.submitted_by_parent,false),COALESCE(i.sibling_ids,''),COALESCE(i.sibling_discount,0),COALESCE(i.referral_credit,0),COALESCE(i.reference_no,'') FROM invoices i JOIN students s ON s.id=i.student_id WHERE s.contact=? AND i.deleted_at IS NULL AND (i.tenant_id=? OR ?=0) ORDER BY i.created_on DESC LIMIT ? OFFSET ?`, c.Email, tid, tid, p.Limit, p.Offset)
	} else {
		db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE deleted_at IS NULL AND (tenant_id=? OR ?=0)`, tid, tid).Scan(&total)
		rows, err = db.Query(`SELECT id,student_id,description,type,amount,due_date,status,created_on,paid_on,COALESCE(payment_proof,''),COALESCE(payment_method,''),COALESCE(discount_pct,0),COALESCE(submitted_by_parent,false),COALESCE(sibling_ids,''),COALESCE(sibling_discount,0),COALESCE(referral_credit,0),COALESCE(reference_no,'') FROM invoices WHERE deleted_at IS NULL AND (tenant_id=? OR ?=0) ORDER BY created_on DESC LIMIT ? OFFSET ?`, tid, tid, p.Limit, p.Offset)
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
			if c.Role != "admin" {
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
				var famID string
				if err := db.QueryRow(`SELECT family_id FROM students WHERE id=?`, inv.StudentID).Scan(&famID); err != nil || famID == "" {
					inv.ReferralCredit = 0
				} else {
					var earned int
					if err := db.QueryRow(`SELECT COUNT(*) FROM referral_rewards WHERE referrer_family_id=? AND status='earned' AND credits_remaining > 0`, famID).Scan(&earned); err == nil && earned == 0 {
						inv.ReferralCredit = 0
					}
				}
			}

			if _, err := db.Exec(`INSERT INTO invoices(id,tenant_id,student_id,description,type,amount,due_date,status,created_on,paid_on,payment_method,discount_pct,submitted_by_parent,sibling_ids,sibling_discount,referral_credit,reference_no) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				inv.ID, tid, inv.StudentID, inv.Description, inv.Type, inv.Amount, inv.DueDate, inv.Status, inv.CreatedOn, nil, inv.PaymentMethod, inv.DiscountPct, inv.SubmittedByParent, inv.SiblingIds, inv.SiblingDiscount, inv.ReferralCredit, inv.ReferenceNo); err != nil {
				respondError(w, "could not create invoice", 500)
				return
			}
			respond(w, inv)
		}
	}
}

func handleInvoicePay(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		id := chi.URLParam(r, "id")

		// Verify invoice exists and check ownership for parents
		var studentID string
		var amount float64
		if err := db.QueryRow(`SELECT student_id, amount FROM invoices WHERE id=? AND deleted_at IS NULL`, id).Scan(&studentID, &amount); err != nil {
			respondError(w, "invoice not found", 404)
			return
		}
		if c != nil && c.Role == "parent" {
			var ownerEmail string
			if err := db.QueryRow(`SELECT contact FROM students WHERE id=?`, studentID).Scan(&ownerEmail); err != nil {
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
			newStatus = body.Status
		}

		// Reference number is mandatory for non-cash payments. Cash uses
		// the empty default. This also runs for parent self-submit (when
		// status is "Pending Verification").
		methodNeedsRef := body.PaymentMethod != "" && body.PaymentMethod != "Cash"
		if methodNeedsRef && body.ReferenceNo == "" {
			respondError(w, "reference number required for "+body.PaymentMethod, http.StatusBadRequest)
			return
		}

		t := today()
		tid := tenantID(c)
		if _, err := db.Exec(`UPDATE invoices SET status=?, paid_on=?, payment_method=COALESCE(NULLIF(?,''),payment_method), reference_no=COALESCE(NULLIF(?,''),reference_no) WHERE id=? AND (tenant_id=? OR ?=0)`, newStatus, t, body.PaymentMethod, body.ReferenceNo, id, tid, tid); err != nil {
			respondError(w, "could not update invoice", 500)
			return
		}
		if c != nil {
			detail := fmt.Sprintf(`{"studentId":"%s","amount":%.2f,"paidOn":"%s","method":"%s"}`, studentID, amount, t, body.PaymentMethod)
			logAudit(db, c.Email, "invoice_paid", "invoice", id, detail)
		}
		// Referral milestone: re-evaluate the referred student's progress.
		// Only relevant for Monthly invoices, but the helper checks itself.
		if newStatus == "Paid" {
			referralCheckMilestoneOnPay(db, studentID)
		}

		// Send a payment-received confirmation email to the parent when THEY
		// submit payment (not when admin marks it paid from the admin panel).
		// This closes the "did you get my money?" feedback loop.
		if c != nil && c.Role == "parent" {
			var description string
			if err := db.QueryRow(`SELECT description FROM invoices WHERE id=?`, id).Scan(&description); err != nil {
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
