package store

import (
	"database/sql"
	"errors"
	"strings"

	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/rating"
)

// referralSourcePrefix tags a referral discount with the reward it drew a credit
// from, so deleting the invoice can give that exact credit back.
const referralSourcePrefix = "referral_reward:"

// referralCreditsPerReward is what an earned reward starts with (3 months of RM10).
const referralCreditsPerReward = 3

// ReferralSource is the applied-discount source for a credit drawn from rewardID.
func ReferralSource(rewardID string) string { return referralSourcePrefix + rewardID }

// ReturnReferralCredit gives back the referral credit a deleted, unpaid invoice spent.
// The applied_discounts row moves to 'returned', so it can happen only once.
func ReturnReferralCredit(tx *Tx, tenantID int, invoiceID string) error {
	var source string
	err := tx.QueryRow(`SELECT source FROM applied_discounts
		WHERE tenant_id=? AND invoice_id=? AND type_id=? AND state='applied'`,
		tenantID, invoiceID, rating.TypeReferral).Scan(&source)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	rewardID, err := rewardForReturn(tx, tenantID, invoiceID, source)
	if err != nil || rewardID == "" {
		return err
	}
	if _, err := tx.Exec(`UPDATE referral_rewards SET credits_remaining = credits_remaining + 1, status='earned'
		WHERE id=? AND tenant_id=?`, rewardID, tenantID); err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE applied_discounts SET state='returned'
		WHERE tenant_id=? AND invoice_id=? AND type_id=?`, tenantID, invoiceID, rating.TypeReferral)
	return err
}

// rewardForReturn names the reward to refund. Invoices drafted before the source was
// tagged fall back to the family's newest part-spent reward: credits are spent oldest
// first, so the most recent spend is always on the newest reward below full.
func rewardForReturn(tx *Tx, tenantID int, invoiceID, source string) (string, error) {
	if strings.HasPrefix(source, referralSourcePrefix) {
		return strings.TrimPrefix(source, referralSourcePrefix), nil
	}
	var rewardID string
	err := tx.QueryRow(`SELECT r.id FROM referral_rewards r
		JOIN students s ON s.family_id = r.referrer_family_id AND s.tenant_id = r.tenant_id
		JOIN invoices i ON i.student_id = s.id AND i.tenant_id = s.tenant_id
		WHERE i.id=? AND r.tenant_id=? AND r.credits_remaining < ?
		ORDER BY r.created_at DESC LIMIT 1`, invoiceID, tenantID, referralCreditsPerReward).Scan(&rewardID)
	if errors.Is(err, sql.ErrNoRows) {
		core.Logger.Warn("referral credit not returned: no part-spent reward found", "invoice_id", invoiceID)
		return "", nil
	}
	return rewardID, err
}

// IsRefundableOnDelete reports whether deleting an invoice in this status should give
// its referral credit back: a paid invoice used the discount, so it stays spent.
func IsRefundableOnDelete(status string) bool {
	return status != models.InvoiceStatusPaid
}
