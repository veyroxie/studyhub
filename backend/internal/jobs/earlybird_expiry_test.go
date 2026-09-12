package jobs

import (
	"os"
	"testing"

	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

func testDSN() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://stratum:stratum_dev@localhost:5432/studyhub_test?sslmode=disable"
}

func seedInvoice(t *testing.T, db *store.DB, id, cutoff string, discount, amount float64, items []models.InvoiceLineItem) {
	t.Helper()
	// Clear the (student, period) SLOT, not just this id: 0039's unique index is
	// what these rows collide on, and the handlers suite shares this database
	// and leaves monthly invoices for the same student behind.
	db.Exec(`DELETE FROM invoices WHERE student_id=? AND period=?`, "STU001", "2026-09")
	if _, err := db.Exec(`INSERT INTO invoices(id,tenant_id,student_id,description,type,amount,due_date,status,created_on,period,early_bird_cutoff,early_bird_discount,line_items)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, 1, "STU001", "Monthly test", "Monthly", amount, "2026-09-07", models.InvoiceStatusUnpaid,
		"2026-09-01", "2026-09", cutoff, discount, models.MarshalLineItems(items)); err != nil {
		t.Fatalf("seed invoice: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM invoices WHERE student_id=? AND period=?`, "STU001", "2026-09") })
}

func replacementOf(t *testing.T, db *store.DB, id string) string {
	t.Helper()
	var status, supersededBy string
	var amount float64
	if err := db.QueryRow(`SELECT status, COALESCE(superseded_by,''), amount FROM invoices WHERE id=?`, id).
		Scan(&status, &supersededBy, &amount); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	if status != models.InvoiceStatusVoid {
		t.Fatalf("original status %q, want Void", status)
	}
	if supersededBy == "" {
		t.Fatal("no replacement linked — the parent is left with a voided invoice and nothing else")
	}
	return supersededBy
}

// The clawback Nadine asked about, done as a correction rather than a silent
// edit: the RM230 invoice the parent holds is voided and replaced by one at
// RM240, so what they receive says what they owe.
func TestEarlyBirdExpiryReissuesAtFullPrice(t *testing.T) {
	core.InitLogger()
	db := store.InitDB(testDSN())
	items := []models.InvoiceLineItem{
		{Kind: models.LineItemKindItem, Name: "Level 3 & 4", Qty: 1, UnitPrice: 240, Amount: 240},
		{Kind: models.LineItemKindDiscount, Name: models.EarlyBirdLineName, Qty: 1, UnitPrice: 10, Amount: -10},
	}
	seedInvoice(t, db, "INV_eb_armed", "2026-09-07", 10, 230, items)

	applyEarlyBirdExpiry(db)

	newID := replacementOf(t, db, "INV_eb_armed")
	var originalAmount float64
	db.QueryRow(`SELECT amount FROM invoices WHERE id=?`, "INV_eb_armed").Scan(&originalAmount)
	if originalAmount != 230 {
		t.Errorf("the voided original now reads %.2f — an issued invoice keeps its figures", originalAmount)
	}

	var amount float64
	var status, cutoff, lines string
	if err := db.QueryRow(`SELECT amount, status, COALESCE(early_bird_cutoff,''), COALESCE(line_items,'[]') FROM invoices WHERE id=?`, newID).
		Scan(&amount, &status, &cutoff, &lines); err != nil {
		t.Fatalf("read replacement: %v", err)
	}
	if amount != 240 {
		t.Errorf("replacement is %.2f, want 240 — the RM10 was not put back", amount)
	}
	if status != models.InvoiceStatusUnpaid {
		t.Errorf("replacement status %q, want Unpaid", status)
	}
	if cutoff != "" {
		t.Errorf("replacement still carries cutoff %q, so it would lapse a second time", cutoff)
	}
	for _, li := range models.ParseLineItems(lines) {
		if li.Name == models.EarlyBirdLineName {
			t.Error("the early-bird line survived onto the replacement — the PDF would not balance")
		}
	}
}

// The restored figure comes from what was ACTUALLY taken off. On a bill smaller
// than the discount the clamp removed less than RM10, and putting RM10 back
// would overcharge the parent.
func TestEarlyBirdExpiryRestoresOnlyWhatWasTaken(t *testing.T) {
	core.InitLogger()
	db := store.InitDB(testDSN())
	items := []models.InvoiceLineItem{
		{Kind: models.LineItemKindItem, Name: "Short month", Qty: 1, UnitPrice: 6, Amount: 6},
		{Kind: models.LineItemKindDiscount, Name: models.EarlyBirdLineName, Qty: 1, UnitPrice: 6, Amount: -6},
	}
	seedInvoice(t, db, "INV_eb_clamped", "2026-09-07", 6, 0.01, items)

	applyEarlyBirdExpiry(db)

	newID := replacementOf(t, db, "INV_eb_clamped")
	var amount float64
	db.QueryRow(`SELECT amount FROM invoices WHERE id=?`, newID).Scan(&amount)
	if amount != 6.01 {
		t.Errorf("replacement is %.2f, want 6.01 — only the RM6 actually taken off should come back", amount)
	}
}

// Running twice must not reissue twice. The replacement carries no cutoff, so
// the second pass does not see it.
func TestEarlyBirdExpiryDoesNotReissueTwice(t *testing.T) {
	core.InitLogger()
	db := store.InitDB(testDSN())
	items := []models.InvoiceLineItem{
		{Kind: models.LineItemKindItem, Name: "Level 3 & 4", Qty: 1, UnitPrice: 240, Amount: 240},
		{Kind: models.LineItemKindDiscount, Name: models.EarlyBirdLineName, Qty: 1, UnitPrice: 10, Amount: -10},
	}
	seedInvoice(t, db, "INV_eb_twice", "2026-09-07", 10, 230, items)

	applyEarlyBirdExpiry(db)
	applyEarlyBirdExpiry(db)

	var live int
	db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE student_id=? AND period=? AND status<>? AND deleted_at IS NULL`,
		"STU001", "2026-09", models.InvoiceStatusVoid).Scan(&live)
	if live != 1 {
		t.Errorf("%d live invoices for the period after two runs, want 1", live)
	}
}

// This is the production state before any of this: a hand-made invoice priced
// RM10 low with nothing recorded. The job cannot see it, which is the whole
// answer to "will it change back on its own".
func TestEarlyBirdExpirySkipsAnInvoiceWithNoCutoff(t *testing.T) {
	core.InitLogger()
	db := store.InitDB(testDSN())
	seedInvoice(t, db, "INV_eb_unarmed", "", 0, 230, nil)

	applyEarlyBirdExpiry(db)

	var amount float64
	var status string
	if err := db.QueryRow(`SELECT amount, status FROM invoices WHERE id=?`, "INV_eb_unarmed").Scan(&amount, &status); err != nil {
		t.Fatalf("read invoice: %v", err)
	}
	if amount != 230 || status != models.InvoiceStatusUnpaid {
		t.Errorf("unarmed invoice became %.2f/%s, want 230/Unpaid — it must not be guessed at", amount, status)
	}
}
