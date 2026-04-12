package main

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// listReferralRewards returns referral_rewards rows visible to the caller.
// Admins see everything; parents see only rewards where they are the referrer.
// Joined names are filled for easy rendering on the frontend.
func listReferralRewards(db *DB, c *Claims) []ReferralReward {
	tid := tenantID(c)
	var rows *sql.Rows
	var err error
	q := `SELECT r.id, r.referrer_family_id, r.referred_student_id, r.status,
	             r.paid_invoice_count, r.credits_remaining, COALESCE(r.milestone_met_on,''), COALESCE(r.created_at::text,''),
	             COALESCE(f.name,''), COALESCE(s.first_name||' '||s.last_name,'')
	      FROM referral_rewards r
	      LEFT JOIN families f ON f.id = r.referrer_family_id
	      LEFT JOIN students s ON s.id = r.referred_student_id
	      WHERE (r.tenant_id=? OR ?=0)`
	if c != nil && c.Role == "parent" {
		q += ` AND f.contact = ? ORDER BY r.created_at DESC`
		rows, err = db.Query(q, tid, tid, c.Email)
	} else {
		q += ` ORDER BY r.created_at DESC`
		rows, err = db.Query(q, tid, tid)
	}
	if err != nil {
		return []ReferralReward{}
	}
	defer rows.Close()
	out := []ReferralReward{}
	for rows.Next() {
		var rr ReferralReward
		if err := rows.Scan(&rr.ID, &rr.ReferrerFamilyID, &rr.ReferredStudentID, &rr.Status,
			&rr.PaidInvoiceCount, &rr.CreditsRemaining, &rr.MilestoneMetOn, &rr.CreatedAt,
			&rr.ReferrerName, &rr.ReferredName); err != nil {
			continue
		}
		out = append(out, rr)
	}
	return out
}

// handleReferrals serves GET /api/referrals — admin gets full ledger,
// parents get just their own referrer-side rewards.
func handleReferrals(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		respond(w, listReferralRewards(db, c))
	}
}

// handleReferralEarn marks a referral_rewards row as earned. The caller (the
// admin UI or the billing module) is responsible for verifying the referred
// student has paid 3 monthly invoices before calling this.
//
// POST /api/referrals/{id}/earn
func handleReferralEarn(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || (c.Role != "admin" && c.Role != "superadmin") {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tid := tenantID(c)

		var status string
		var studentID string
		if err := db.QueryRow(`SELECT status, referred_student_id FROM referral_rewards WHERE id=? AND (tenant_id=? OR ?=0)`, id, tid, tid).Scan(&status, &studentID); err != nil {
			respondError(w, "referral not found", 404)
			return
		}
		if status != "pending" {
			// Already earned/exhausted — idempotent no-op.
			respond(w, map[string]string{"status": status})
			return
		}

		// Recount paid Monthly invoices server-side as a sanity check. If the
		// frontend tracks more (because invoices are localStorage-only) we accept
		// the call regardless — the admin UI is the gate.
		var paidCount int
		db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE student_id=? AND type='Monthly' AND status='Paid' AND deleted_at IS NULL`, studentID).Scan(&paidCount)

		if _, err := db.Exec(`UPDATE referral_rewards SET status='earned', credits_remaining=3, paid_invoice_count=GREATEST(paid_invoice_count, ?), milestone_met_on=? WHERE id=?`, paidCount, today(), id); err != nil {
			respondError(w, "could not mark earned", 500)
			return
		}
		db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
			c.Email, "referral_milestone_met", "referral", id, "student="+studentID)
		respond(w, map[string]string{"status": "earned"})
	}
}

// handleReferralConsume decrements credits_remaining by 1 and marks the row
// exhausted when it hits zero. Called by the billing module when a referral
// credit is applied to a freshly-generated invoice.
//
// POST /api/referrals/{id}/consume
func handleReferralConsume(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || (c.Role != "admin" && c.Role != "superadmin") {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tid := tenantID(c)

		// Atomic decrement — a single UPDATE that both validates and mutates
		// in one statement, preventing the TOCTOU race where two concurrent
		// requests could both read remaining=1 and both decrement to 0.
		res, err := db.Exec(`UPDATE referral_rewards SET credits_remaining = credits_remaining - 1,
			status = CASE WHEN credits_remaining - 1 <= 0 THEN 'exhausted' ELSE 'earned' END
			WHERE id=? AND (tenant_id=? OR ?=0) AND status='earned' AND credits_remaining > 0`, id, tid, tid)
		if err != nil {
			respondError(w, "could not consume credit", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			respondError(w, "no credits available", 400)
			return
		}
		// Read back the new state for the response.
		var newStatus string
		var newRemaining int
		db.QueryRow(`SELECT status, credits_remaining FROM referral_rewards WHERE id=?`, id).Scan(&newStatus, &newRemaining)
		db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
			c.Email, "referral_credit_applied", "referral", id, "remaining="+itoa(newRemaining))
		respond(w, map[string]any{"status": newStatus, "creditsRemaining": newRemaining})
	}
}

// handleFamilyReferral returns the referral context for a single family —
// the parent dashboard tile uses this. Auth: parents may only fetch their own.
//
// GET /api/families/{id}/referral
func handleFamilyReferral(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		id := chi.URLParam(r, "id")
		tid := tenantID(c)

		var code, contact string
		if err := db.QueryRow(`SELECT COALESCE(referral_code,''), contact FROM families WHERE id=? AND (tenant_id=? OR ?=0) AND deleted_at IS NULL`, id, tid, tid).Scan(&code, &contact); err != nil {
			respondError(w, "family not found", 404)
			return
		}
		if c != nil && c.Role == "parent" && !strings.EqualFold(contact, c.Email) {
			respondError(w, "forbidden", 403)
			return
		}

		// Backfill code on read if it's somehow still empty (defence in depth).
		if code == "" {
			code = newReferralCode()
			db.Exec(`UPDATE families SET referral_code=? WHERE id=? AND (referral_code IS NULL OR referral_code='')`, code, id)
		}

		rewards := []ReferralReward{}
		rows, _ := db.Query(`SELECT r.id, r.referrer_family_id, r.referred_student_id, r.status,
		                            r.paid_invoice_count, r.credits_remaining, COALESCE(r.milestone_met_on,''),
		                            COALESCE(s.first_name||' '||s.last_name,'')
		                     FROM referral_rewards r
		                     LEFT JOIN students s ON s.id = r.referred_student_id
		                     WHERE r.referrer_family_id=? ORDER BY r.created_at DESC`, id)
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var rr ReferralReward
				if err := rows.Scan(&rr.ID, &rr.ReferrerFamilyID, &rr.ReferredStudentID, &rr.Status,
					&rr.PaidInvoiceCount, &rr.CreditsRemaining, &rr.MilestoneMetOn, &rr.ReferredName); err != nil {
					continue
				}
				rewards = append(rewards, rr)
			}
		}

		totalRemaining := 0
		for _, rr := range rewards {
			if rr.Status == "earned" {
				totalRemaining += rr.CreditsRemaining
			}
		}

		respond(w, map[string]any{
			"familyId":         id,
			"referralCode":     code,
			"rewards":          rewards,
			"creditsRemaining": totalRemaining,
		})
	}
}

// itoa is a tiny helper kept local to this file to avoid pulling strconv into
// the audit-log call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// referralCheckMilestoneOnPay is called from handleInvoicePay after an invoice
// is marked Paid. It updates paid_invoice_count and, if the count reaches 3,
// flips the row to 'earned' state. Errors are swallowed — referral logic must
// never break payment processing.
func referralCheckMilestoneOnPay(db *DB, studentID string) {
	var rrID, status string
	if err := db.QueryRow(`SELECT id, status FROM referral_rewards WHERE referred_student_id=?`, studentID).Scan(&rrID, &status); err != nil {
		return
	}
	if status != "pending" {
		return
	}
	var paid int
	db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE student_id=? AND type='Monthly' AND status='Paid' AND deleted_at IS NULL`, studentID).Scan(&paid)
	if paid < 3 {
		db.Exec(`UPDATE referral_rewards SET paid_invoice_count=? WHERE id=?`, paid, rrID)
		return
	}
	db.Exec(`UPDATE referral_rewards SET status='earned', credits_remaining=3, paid_invoice_count=?, milestone_met_on=? WHERE id=?`, paid, today(), rrID)
	db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
		"system", "referral_milestone_met", "referral", rrID, "student="+studentID)
}

