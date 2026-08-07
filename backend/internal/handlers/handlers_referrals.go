package handlers

import (
	"database/sql"
	"net/http"
	"strings"
	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
)

// listReferralRewards returns referral_rewards rows visible to the caller.
// Admins see everything; parents see only rewards where they are the referrer.
// Joined names are filled for easy rendering on the frontend.
func listReferralRewards(db *store.DB, c *core.Claims) []models.ReferralReward {
	// The referral ledger carries family names, referred-student names and credit
	// balances — billing data. Admins see all, a parent sees their own, everyone
	// else (teachers) gets nothing.
	if c == nil || (!core.IsAdminRole(c) && c.Role != "parent") {
		return []models.ReferralReward{}
	}
	tw, twArgs := store.ScopeTenant(c, "r")
	var rows *sql.Rows
	var err error
	q := `SELECT r.id, r.referrer_family_id, r.referred_student_id, r.status,
	             r.paid_invoice_count, r.credits_remaining, COALESCE(r.milestone_met_on,''), COALESCE(r.created_at::text,''),
	             COALESCE(f.name,''), COALESCE(s.first_name||' '||s.last_name,'')
	      FROM referral_rewards r
	      LEFT JOIN families f ON f.id = r.referrer_family_id
	      LEFT JOIN students s ON s.id = r.referred_student_id
	      WHERE 1=1` + tw
	if c != nil && c.Role == "parent" {
		q += ` AND f.contact = ? ORDER BY r.created_at DESC`
		args := append(append([]any{}, twArgs...), c.Email)
		rows, err = db.Query(q, args...)
	} else {
		q += ` ORDER BY r.created_at DESC`
		rows, err = db.Query(q, twArgs...)
	}
	if err != nil {
		core.Logger.Error("list query failed", "err", err, "type", "ReferralReward")
		return []models.ReferralReward{}
	}
	defer rows.Close()
	out := []models.ReferralReward{}
	for rows.Next() {
		var rr models.ReferralReward
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
func HandleReferrals(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		core.Respond(w, listReferralRewards(db, c))
	}
}

// handleReferralEarn marks a referral_rewards row as earned. The caller (the
// admin UI or the billing module) is responsible for verifying the referred
// student has paid 3 monthly invoices before calling this.
//
// POST /api/referrals/{id}/earn
func HandleReferralEarn(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")

		var status string
		var studentID string
		args := append([]any{id}, twArgs...)
		if err := db.QueryRow(`SELECT status, referred_student_id FROM referral_rewards WHERE id=?`+tw, args...).Scan(&status, &studentID); err != nil {
			core.RespondError(w, "referral not found", 404)
			return
		}
		if status != "pending" {
			// Already earned/exhausted — idempotent no-op.
			core.Respond(w, map[string]string{"status": status})
			return
		}

		// Recount paid Monthly invoices server-side as a sanity check. If the
		// frontend tracks more (because invoices are localStorage-only) we accept
		// the call regardless — the admin UI is the gate.
		var paidCount int
		countArgs := append([]any{studentID}, twArgs...)
		db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE student_id=? AND type='Monthly' AND status='Paid' AND deleted_at IS NULL`+tw, countArgs...).Scan(&paidCount)

		updArgs := append([]any{paidCount, core.Today(), id}, twArgs...)
		if _, err := db.Exec(`UPDATE referral_rewards SET status='earned', credits_remaining=3, paid_invoice_count=GREATEST(paid_invoice_count, ?), milestone_met_on=? WHERE id=?`+tw, updArgs...); err != nil {
			core.RespondError(w, "could not mark earned", 500)
			return
		}
		core.LogAudit(db, store.TenantID(c), c.Email, "referral_milestone_met", "referral", id, "student="+studentID)
		core.Respond(w, map[string]string{"status": "earned"})
	}
}

// handleReferralConsume decrements credits_remaining by 1 and marks the row
// exhausted when it hits zero. Called by the billing module when a referral
// credit is applied to a freshly-generated invoice.
//
// POST /api/referrals/{id}/consume
func HandleReferralConsume(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")

		// Atomic decrement — a single UPDATE that both validates and mutates
		// in one statement, preventing the TOCTOU race where two concurrent
		// requests could both read remaining=1 and both decrement to 0.
		args := append([]any{id}, twArgs...)
		res, err := db.Exec(`UPDATE referral_rewards SET credits_remaining = credits_remaining - 1,
			status = CASE WHEN credits_remaining - 1 <= 0 THEN 'exhausted' ELSE 'earned' END
			WHERE id=?`+tw+` AND status='earned' AND credits_remaining > 0`, args...)
		if err != nil {
			core.RespondError(w, "could not consume credit", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			core.RespondError(w, "no credits available", 400)
			return
		}
		// Read back the new state for the response.
		var newStatus string
		var newRemaining int
		readArgs := append([]any{id}, twArgs...)
		db.QueryRow(`SELECT status, credits_remaining FROM referral_rewards WHERE id=?`+tw, readArgs...).Scan(&newStatus, &newRemaining)
		core.LogAudit(db, store.TenantID(c), c.Email, "referral_credit_applied", "referral", id, "remaining="+itoa(newRemaining))
		core.Respond(w, map[string]any{"status": newStatus, "creditsRemaining": newRemaining})
	}
}

// handleFamilyReferral returns the referral context for a single family —
// the parent dashboard tile uses this. Auth: parents may only fetch their own.
//
// GET /api/families/{id}/referral
func HandleFamilyReferral(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")

		var code, contact string
		args := append([]any{id}, twArgs...)
		if err := db.QueryRow(`SELECT COALESCE(referral_code,''), contact FROM families WHERE id=?`+tw+` AND deleted_at IS NULL`, args...).Scan(&code, &contact); err != nil {
			core.RespondError(w, "family not found", 404)
			return
		}
		if c != nil && c.Role == "parent" && !strings.EqualFold(contact, c.Email) {
			core.RespondError(w, "forbidden", 403)
			return
		}

		// Backfill code on read if it's somehow still empty (defence in depth).
		if code == "" {
			code = core.NewReferralCode()
			backfillArgs := append([]any{code, id}, twArgs...)
			db.Exec(`UPDATE families SET referral_code=? WHERE id=?`+tw+` AND (referral_code IS NULL OR referral_code='')`, backfillArgs...)
		}

		rewards := []models.ReferralReward{}
		// Reuse the same tenant scope (alias r) so we don't leak rewards
		// from another tenant should family ids ever collide.
		rtw, rtwArgs := store.ScopeTenant(c, "r")
		rewArgs := append([]any{id}, rtwArgs...)
		rows, _ := db.Query(`SELECT r.id, r.referrer_family_id, r.referred_student_id, r.status,
		                            r.paid_invoice_count, r.credits_remaining, COALESCE(r.milestone_met_on,''),
		                            COALESCE(s.first_name||' '||s.last_name,'')
		                     FROM referral_rewards r
		                     LEFT JOIN students s ON s.id = r.referred_student_id
		                     WHERE r.referrer_family_id=?`+rtw+` ORDER BY r.created_at DESC`, rewArgs...)
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var rr models.ReferralReward
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

		core.Respond(w, map[string]any{
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
