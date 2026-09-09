package store

import "studyhub/internal/core"

// A referral is earned once the referred student has three Monthly invoices
// paid, and pays out as three credits. Separate constants because they are
// separate facts that happen to share a value.
const (
	referralMilestoneInvoices = 3
	referralRewardCredits     = 3
)

// ReferralReconcile re-derives a referral reward from the referred student's
// paid Monthly invoices. Called whenever that count can have moved: marking an
// invoice Paid, and reversing one back out of Paid.
//
// It reconciles in both directions on purpose. Counting only upward left an
// earned reward standing after the invoice that completed it was reversed --
// three credits granted on a count of two, and nothing anywhere disagreed.
//
// A reward whose credits have already been spent is NOT clawed back: that
// benefit has usually reached a real invoice, and silently un-granting it would
// make a correction of one mistake into a second one. Those are logged for an
// admin to settle by hand instead.
//
// paid_invoice_count is the honest count and is no longer frozen at the
// milestone once earned; students.js only renders it while the row is pending.
//
// Tenant-scoped so a paid invoice in tenant A cannot settle a referral row
// owned by tenant B. Errors are swallowed -- referral logic must never break
// payment processing.
func ReferralReconcile(db *DB, studentID string, c *core.Claims) {
	tw, twArgs := ScopeTenant(c, "")
	var rrID, status string
	var creditsRemaining, rrTenantID int
	selArgs := append([]any{studentID}, twArgs...)
	if err := db.QueryRow(`SELECT id, status, credits_remaining, tenant_id FROM referral_rewards WHERE referred_student_id=?`+tw, selArgs...).
		Scan(&rrID, &status, &creditsRemaining, &rrTenantID); err != nil {
		return
	}
	var paid int
	paidArgs := append([]any{studentID}, twArgs...)
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE student_id=? AND type='Monthly' AND status='Paid' AND deleted_at IS NULL`+tw, paidArgs...).Scan(&paid); err != nil {
		core.Logger.Error("failed to count paid invoices for referral", "err", err, "referral_reward_id", rrID)
		return
	}

	if status == "pending" && paid >= referralMilestoneInvoices {
		milestoneArgs := append([]any{referralRewardCredits, paid, core.Today(), rrID}, twArgs...)
		if _, err := db.Exec(`UPDATE referral_rewards SET status='earned', credits_remaining=?, paid_invoice_count=?, milestone_met_on=? WHERE id=?`+tw, milestoneArgs...); err != nil {
			core.Logger.Error("failed to update referral milestone", "err", err, "referral_reward_id", rrID)
			return
		}
		core.LogAudit(db, rrTenantID, "system", "referral_milestone_met", "referral", rrID, "student="+studentID)
		return
	}

	if status == "earned" && paid < referralMilestoneInvoices {
		if creditsRemaining < referralRewardCredits {
			// The count is written even here, so the row itself shows the
			// discrepancy the audit line describes: earned, on fewer paid
			// invoices than the milestone needs.
			staleArgs := append([]any{paid, rrID}, twArgs...)
			if _, err := db.Exec(`UPDATE referral_rewards SET paid_invoice_count=? WHERE id=?`+tw, staleArgs...); err != nil {
				core.Logger.Error("failed to update referral paid_invoice_count", "err", err, "referral_reward_id", rrID)
			}
			core.LogAudit(db, rrTenantID, "system", "referral_milestone_stale", "referral", rrID,
				"student="+studentID+" — an invoice behind this reward was reversed or deleted, but its credits are already partly spent; settle by hand")
			return
		}
		reopenArgs := append([]any{paid, rrID}, twArgs...)
		if _, err := db.Exec(`UPDATE referral_rewards SET status='pending', credits_remaining=0, milestone_met_on=NULL, paid_invoice_count=? WHERE id=?`+tw, reopenArgs...); err != nil {
			core.Logger.Error("failed to re-open referral milestone", "err", err, "referral_reward_id", rrID)
			return
		}
		core.LogAudit(db, rrTenantID, "system", "referral_milestone_reversed", "referral", rrID, "student="+studentID)
		return
	}

	updArgs := append([]any{paid, rrID}, twArgs...)
	if _, err := db.Exec(`UPDATE referral_rewards SET paid_invoice_count=? WHERE id=?`+tw, updArgs...); err != nil {
		core.Logger.Error("failed to update referral paid_invoice_count", "err", err, "referral_reward_id", rrID)
	}
}
