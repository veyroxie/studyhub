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
	db.Exec(`DELETE FROM invoices WHERE id=?`, id)
	if _, err := db.Exec(`INSERT INTO invoices(id,tenant_id,student_id,description,type,amount,due_date,status,created_on,period,early_bird_cutoff,early_bird_discount,line_items)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, 1, "STU001", "Monthly test", "Monthly", amount, "2026-09-07", "Unpaid", "2026-09-01", "2026-09",
		cutoff, discount, models.MarshalLineItems(items)); err != nil {
		t.Fatalf("seed invoice: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM invoices WHERE id=?`, id) })
}

func readInvoice(t *testing.T, db *store.DB, id string) (float64, string, string) {
	t.Helper()
	var amount float64
	var status, items string
	if err := db.QueryRow(`SELECT amount, status, COALESCE(line_items,'[]') FROM invoices WHERE id=?`, id).
		Scan(&amount, &status, &items); err != nil {
		t.Fatalf("read invoice: %v", err)
	}
	return amount, status, items
}

// The clawback Nadine asked about: unpaid past the 7th, the RM10 goes back on
// and the invoice becomes Overdue. This path had no test, and in production it
// had never once fired -- every invoice was hand-made with no cutoff recorded.
func TestEarlyBirdExpiryRestoresFullPrice(t *testing.T) {
	core.InitLogger()
	db := store.InitDB(testDSN())
	items := []models.InvoiceLineItem{
		{Kind: models.LineItemKindItem, Name: "Level 3 & 4", Qty: 1, UnitPrice: 240, Amount: 240},
		{Kind: models.LineItemKindDiscount, Name: models.EarlyBirdLineName, Qty: 1, UnitPrice: 10, Amount: -10},
	}
	seedInvoice(t, db, "INV_eb_armed", "2026-09-07", 10, 230, items)

	applyEarlyBirdExpiry(db)

	amount, status, lineItems := readInvoice(t, db, "INV_eb_armed")
	if amount != 240 {
		t.Errorf("amount %.2f after expiry, want 240 — the RM10 was not put back", amount)
	}
	if status != "Overdue" {
		t.Errorf("status %q after expiry, want Overdue", status)
	}
	for _, li := range models.ParseLineItems(lineItems) {
		if li.Name == models.EarlyBirdLineName {
			t.Error("the early bird line survived the clawback — the PDF would no longer balance")
		}
	}
}

// Running twice must not add the RM10 twice.
func TestEarlyBirdExpiryIsIdempotent(t *testing.T) {
	core.InitLogger()
	db := store.InitDB(testDSN())
	items := []models.InvoiceLineItem{
		{Kind: models.LineItemKindItem, Name: "Level 3 & 4", Qty: 1, UnitPrice: 240, Amount: 240},
		{Kind: models.LineItemKindDiscount, Name: models.EarlyBirdLineName, Qty: 1, UnitPrice: 10, Amount: -10},
	}
	seedInvoice(t, db, "INV_eb_twice", "2026-09-07", 10, 230, items)

	applyEarlyBirdExpiry(db)
	applyEarlyBirdExpiry(db)

	amount, _, _ := readInvoice(t, db, "INV_eb_twice")
	if amount != 240 {
		t.Errorf("amount %.2f after two runs, want 240", amount)
	}
}

// This is the production state before the fix: a hand-made invoice priced RM10
// low with nothing recorded. The job cannot see it, which is the whole answer
// to "will it change back on its own".
func TestEarlyBirdExpirySkipsAnInvoiceWithNoCutoff(t *testing.T) {
	core.InitLogger()
	db := store.InitDB(testDSN())
	seedInvoice(t, db, "INV_eb_unarmed", "", 0, 230, nil)

	applyEarlyBirdExpiry(db)

	amount, status, _ := readInvoice(t, db, "INV_eb_unarmed")
	if amount != 230 || status != "Unpaid" {
		t.Errorf("unarmed invoice became %.2f/%s, want 230/Unpaid — it must not be guessed at", amount, status)
	}
}
