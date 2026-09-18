package store

import (
	"strings"

	"studyhub/internal/models"
	"studyhub/internal/rating"
)

// SettleEarlyBirdOnIssue resolves the conditional discount at the moment the
// invoice becomes a document.
//
// Drafting and issuing used to be the same instant, so a cutoff set at drafting
// was always still in the future. They are separate acts now: a draft made on
// the 1st carries a cutoff of the 7th, and issuing it on the 20th would produce
// an invoice born with an expired cutoff -- which applyEarlyBirdExpiry, running
// hourly, would immediately void and replace. The parent would receive an
// invoice reading "pay by <a date that has passed>" and then a correction.
//
// So a draft whose cutoff has gone is issued at full price instead. The
// discount was always described as granted ON ISSUE and pending until earned;
// this is that description made true.
//
// Runs inside the issuing transaction: the invoice and what it charges must
// commit together.
func SettleEarlyBirdOnIssue(tx *Tx, tenantID int, invoiceID, today string) error {
	var amount, discount float64
	var cutoff, itemsJSON string
	err := tx.QueryRow(`SELECT amount, COALESCE(early_bird_discount,0),
		COALESCE(early_bird_cutoff,''), COALESCE(line_items,'[]')
		FROM invoices WHERE id=? AND tenant_id=?`, invoiceID, tenantID).
		Scan(&amount, &discount, &cutoff, &itemsJSON)
	if err != nil {
		return err
	}
	if discount <= 0 || cutoff == "" || cutoff >= today {
		return nil
	}

	// Which line to drop comes from the typed record; the name match is only
	// for invoices drafted before 0069.
	typedName := ""
	rows, qerr := tx.Query(`SELECT name FROM applied_discounts
		WHERE tenant_id=? AND invoice_id=? AND type_id=?`, tenantID, invoiceID, rating.TypeEarlyBird)
	if qerr == nil {
		for rows.Next() {
			rows.Scan(&typedName)
		}
		rows.Close()
	}
	kept := []models.InvoiceLineItem{}
	for _, it := range models.ParseLineItems(itemsJSON) {
		isEarlyBird := it.Kind == models.LineItemKindDiscount &&
			((typedName != "" && it.Name == typedName) ||
				(typedName == "" && strings.HasPrefix(it.Name, models.EarlyBirdLinePrefix)))
		if isEarlyBird {
			continue
		}
		kept = append(kept, it)
	}

	if _, err := tx.Exec(`UPDATE invoices SET amount=?, early_bird_discount=0,
		early_bird_cutoff='', line_items=? WHERE id=? AND tenant_id=?`,
		amount+discount, models.MarshalLineItems(kept), invoiceID, tenantID); err != nil {
		return err
	}
	// It was granted pending and has not been earned.
	_, err = tx.Exec(`UPDATE applied_discounts SET state=? WHERE tenant_id=? AND invoice_id=? AND type_id=?`,
		rating.StateForfeited, tenantID, invoiceID, rating.TypeEarlyBird)
	return err
}
