package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/models"
	"studyhub/internal/store"
)

func createInvoiceWithLines(t *testing.T, r *chi.Mux, token string, items []models.InvoiceLineItem) string {
	t.Helper()
	// Created as a draft: line items are what the builder edits, and editing
	// them moves the total, which an issued invoice will not allow (ADR-016).
	inv := models.Invoice{
		StudentID: "STU001", Description: "Sept fees", Type: "Monthly",
		DueDate: "2026-09-07", CreatedOn: "2026-09-01", LineItems: items,
		Status: models.InvoiceStatusDraft,
	}
	w := doRequest(r, "POST", "/api/invoices", token, inv)
	if w.Code != http.StatusOK {
		t.Fatalf("create invoice: got %d: %s", w.Code, w.Body.String())
	}
	var created models.Invoice
	json.NewDecoder(w.Body).Decode(&created)
	return created.ID
}

func readEarlyBird(t *testing.T, db *store.DB, id string) (string, float64) {
	t.Helper()
	var cutoff string
	var discount float64
	if err := db.QueryRow(`SELECT COALESCE(early_bird_cutoff,''), COALESCE(early_bird_discount,0) FROM invoices WHERE id=?`, id).
		Scan(&cutoff, &discount); err != nil {
		t.Fatalf("read early bird fields: %v", err)
	}
	return cutoff, discount
}

func baseLine() models.InvoiceLineItem {
	return models.InvoiceLineItem{Kind: models.LineItemKindItem, Name: "Level 3 & 4", Qty: 1, UnitPrice: 240, Amount: 240}
}

func earlyBirdLine() models.InvoiceLineItem {
	return models.InvoiceLineItem{Kind: models.LineItemKindDiscount, Name: models.EarlyBirdLineName, Qty: 1, UnitPrice: 10, Amount: -10}
}

// A hand-made invoice carrying the early bird line arms the clawback, so the
// hourly job can find it. Before this, only cron-issued invoices were armed and
// every hand-made one kept its RM10 whether or not the parent paid by the 7th.
func TestEarlyBirdLineArmsTheClawbackOnCreate(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	id := createInvoiceWithLines(t, r, token, []models.InvoiceLineItem{baseLine(), earlyBirdLine()})

	cutoff, discount := readEarlyBird(t, db, id)
	if cutoff != "2026-09-07" {
		t.Errorf("cutoff %q, want 2026-09-07 — the 7th of the invoice's own period", cutoff)
	}
	if discount != 10 {
		t.Errorf("discount %.2f, want 10", discount)
	}
}

// No line, no clawback. The amount alone must never arm it: the referral
// discount is also RM10, so a price gap cannot tell the two apart.
func TestNoEarlyBirdLineLeavesTheClawbackDisarmed(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	referral := models.InvoiceLineItem{Kind: models.LineItemKindDiscount, Name: "Referral discount", Qty: 1, UnitPrice: 10, Amount: -10}
	id := createInvoiceWithLines(t, r, token, []models.InvoiceLineItem{baseLine(), referral})

	cutoff, discount := readEarlyBird(t, db, id)
	if cutoff != "" || discount != 0 {
		t.Errorf("a referral-only invoice armed the early bird clawback (%q/%.2f) — it would claw back the referral", cutoff, discount)
	}
}

// Removing the line on edit disarms it, so the terms never outlive the evidence.
func TestRemovingTheEarlyBirdLineDisarmsTheClawback(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	id := createInvoiceWithLines(t, r, token, []models.InvoiceLineItem{baseLine(), earlyBirdLine()})
	if cutoff, _ := readEarlyBird(t, db, id); cutoff == "" {
		t.Fatal("setup: expected the clawback armed before the edit")
	}

	inv := models.Invoice{
		StudentID: "STU001", Description: "Sept fees", Type: "Monthly",
		DueDate: "2026-09-07", CreatedOn: "2026-09-01",
		LineItems: []models.InvoiceLineItem{baseLine()},
	}
	if w := doRequest(r, "PUT", "/api/invoices/"+id, token, inv); w.Code != http.StatusOK {
		t.Fatalf("update invoice: got %d: %s", w.Code, w.Body.String())
	}

	cutoff, discount := readEarlyBird(t, db, id)
	if cutoff != "" || discount != 0 {
		t.Errorf("removing the line left the clawback armed (%q/%.2f)", cutoff, discount)
	}
}
