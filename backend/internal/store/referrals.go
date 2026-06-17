package store

import "studyhub/internal/core"

// ReferralCheckMilestoneOnPay advances a referral reward's progress when an
// invoice is marked Paid. It updates paid_invoice_count and, if the count
// reaches 3, flips the row to 'earned' state. Tenant-scoped so a paid invoice
// in tenant A cannot accidentally settle a referral row owned by tenant B.
// Errors are swallowed — referral logic must never break payment processing.
func ReferralCheckMilestoneOnPay(db *DB, studentID string, c *core.Claims) {
	tw, twArgs := ScopeTenant(c, "")
	var rrID, status string
	selArgs := append([]any{studentID}, twArgs...)
	if err := db.QueryRow(`SELECT id, status FROM referral_rewards WHERE referred_student_id=?`+tw, selArgs...).Scan(&rrID, &status); err != nil {
		return
	}
	if status != "pending" {
		return
	}
	var paid int
	paidArgs := append([]any{studentID}, twArgs...)
	db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE student_id=? AND type='Monthly' AND status='Paid' AND deleted_at IS NULL`+tw, paidArgs...).Scan(&paid)
	if paid < 3 {
		updArgs := append([]any{paid, rrID}, twArgs...)
		if _, err := db.Exec(`UPDATE referral_rewards SET paid_invoice_count=? WHERE id=?`+tw, updArgs...); err != nil {
			core.Logger.Error("failed to update referral paid_invoice_count", "err", err, "referral_reward_id", rrID)
		}
		return
	}
	milestoneArgs := append([]any{paid, core.Today(), rrID}, twArgs...)
	if _, err := db.Exec(`UPDATE referral_rewards SET status='earned', credits_remaining=3, paid_invoice_count=?, milestone_met_on=? WHERE id=?`+tw, milestoneArgs...); err != nil {
		core.Logger.Error("failed to update referral milestone", "err", err, "referral_reward_id", rrID)
	}
	core.LogAudit(db, "system", "referral_milestone_met", "referral", rrID, "student="+studentID)
}
