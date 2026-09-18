package store

import (
	"fmt"

	"studyhub/internal/core"
)

// SaveAppliedDiscounts records what actually came off an invoice, with the type
// that identifies it and the reason the student qualified.
//
// Written in the SAME transaction as the invoice it belongs to: a discount row
// with no invoice, or an invoice whose discounts were lost, is money either way.
//
// Why a table at all, when the invoice already has early_bird_discount,
// sibling_discount and referral_credit columns: two of our discounts are both a
// flat RM10, so an amount alone cannot say which one an invoice carries, and a
// column per discount cannot describe a discount nobody has invented yet. The
// early-bird clawback identifies its own discount by matching the line NAME,
// which breaks the day anyone renames what prints.
func SaveAppliedDiscounts(tx *Tx, tenantID int, invoiceID string, ds []AppliedDiscount) error {
	for _, d := range ds {
		if d.Amount <= 0 {
			continue
		}
		// ON CONFLICT so a retry of the issuing transaction cannot double-grant:
		// the unique index is (invoice_id, type_id).
		if _, err := tx.Exec(`INSERT INTO applied_discounts(id,tenant_id,invoice_id,type_id,name,source,sequence,amount,state)
			VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT (invoice_id, type_id) DO NOTHING`,
			core.GenerateID("ADI"), tenantID, invoiceID, d.TypeID, d.Name, d.Source,
			d.Sequence, d.Amount, d.State); err != nil {
			return fmt.Errorf("record %s discount on %s: %w", d.TypeID, invoiceID, err)
		}
	}
	return nil
}

// AppliedDiscountsFor reads back what came off one invoice.
func AppliedDiscountsFor(db *DB, tenantID int, invoiceID string) ([]AppliedDiscount, error) {
	rows, err := db.Query(`SELECT type_id, name, source, sequence, amount, state
		FROM applied_discounts WHERE tenant_id=? AND invoice_id=? ORDER BY sequence, type_id`,
		tenantID, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AppliedDiscount{}
	for rows.Next() {
		var d AppliedDiscount
		if rows.Scan(&d.TypeID, &d.Name, &d.Source, &d.Sequence, &d.Amount, &d.State) == nil {
			out = append(out, d)
		}
	}
	return out, nil
}

// ForfeitDiscount records that a conditional discount was not earned. The early
// bird is the only one: granted on issue, lost if the invoice is still unpaid
// at the cutoff. Recording it keeps the invoice able to say why it cost what it
// did even after the clawback has replaced it.
func ForfeitDiscount(db *DB, tenantID int, invoiceID, typeID string) error {
	_, err := db.Exec(`UPDATE applied_discounts SET state='forfeited'
		WHERE tenant_id=? AND invoice_id=? AND type_id=?`, tenantID, invoiceID, typeID)
	return err
}
