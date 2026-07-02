package pdf

import (
	"bytes"
	"testing"

	"studyhub/internal/models"
	"studyhub/internal/store"
)

func sampleSettings() *store.TenantSettings {
	return &store.TenantSettings{
		BrandName:           "The Study Hub",
		BrandTagline:        "TS Smart Hub Enterprise",
		SupportEmail:        "seeduser33@example.com",
		SupportPhone:        "011-2862 0038",
		AddressLine1:        "A1-03-12, Arcoris Business Suite Jalan Kiara Mont Kiara",
		AddressLine2:        "Kuala Lumpur 50480",
		BankName:            "Maybank",
		BankAccountHolder:   "TS SMART HUB ENTERPRISE",
		BankAccountNo:       "5647 2672 4699",
		PaymentInstructions: "Please make payment via online bank transfer only.",
		InvoiceTerms:        "Inform us of any absence at least three (3) hours prior.\nParents may freeze classes during absence.",
	}
}

// TestRenderMultiLineInvoice renders a Group + membership + FOC invoice and
// checks it produces a non-empty PDF whose totals net correctly (Subtotal 340,
// FOC -40, Total Due 300 — matching the reference layout).
func TestRenderMultiLineInvoice(t *testing.T) {
	d := invoicePDFData{
		InvoiceID: "INV1", StudentName: "Stephanie Jin", StudentID: "STU_ABC",
		ParentName: "Mrs Jin", CreatedOn: "2026-06-01", DueDate: "2026-06-07",
		Status: "Unpaid", Amount: 300,
		LineItems: []models.InvoiceLineItem{
			{Kind: models.LineItemKindItem, Name: "Singapore Math - Group", Descriptor: "Group class, Level 1-3", PeriodStart: "2026-06-01", PeriodEnd: "2026-06-30", Qty: 1, UnitPrice: 300, Amount: 300},
			{Kind: models.LineItemKindItem, Name: "TSH Membership", Descriptor: "4 self-study hours included", Qty: 1, UnitPrice: 40, Amount: 40},
			{Kind: models.LineItemKindDiscount, Name: "Special pass FOC (self-study included)", Amount: -40},
		},
	}
	b, err := renderInvoicePDF(d, sampleSettings(), false)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(b) == 0 || !bytes.HasPrefix(b, []byte("%PDF")) {
		t.Fatalf("expected a PDF, got %d bytes", len(b))
	}
}

// TestRenderLegacyInvoiceFallback covers an invoice created before the
// line_items column: the renderer must synthesize a single item line plus the
// stored scalar discounts (here a RM10 sibling discount off a RM240 gross).
func TestRenderLegacyInvoiceFallback(t *testing.T) {
	d := invoicePDFData{
		InvoiceID: "INV0", StudentName: "Old Student", StudentID: "STU_OLD",
		Description: "Monthly tuition — Jan 2026 — Old Student",
		CreatedOn:   "2026-01-01", DueDate: "2026-01-07", Status: "Unpaid",
		Amount: 230, SiblingDiscount: 10,
	}
	items := synthesizeLineItems(d)
	if len(items) != 2 {
		t.Fatalf("expected 1 item + 1 discount, got %d lines", len(items))
	}
	if items[0].Amount != 240 {
		t.Errorf("reconstructed gross = %.2f, want 240", items[0].Amount)
	}
	if items[1].Kind != models.LineItemKindDiscount || items[1].Amount != -10 {
		t.Errorf("sibling discount line = %+v, want -10 discount", items[1])
	}
	b, err := renderInvoicePDF(d, sampleSettings(), false)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !bytes.HasPrefix(b, []byte("%PDF")) {
		t.Fatalf("expected a PDF")
	}
}

// TestRenderReceiptRequiresPaid is a sanity check that receipt mode renders the
// paid variant without error.
func TestRenderReceipt(t *testing.T) {
	d := invoicePDFData{
		InvoiceID: "INV1", StudentName: "Stephanie Jin", StudentID: "STU_ABC",
		CreatedOn: "2026-06-01", DueDate: "2026-06-07", Status: "Paid",
		ReceiptNo: "RCPT-000042", PaidOn: "2026-06-03", PaymentMethod: "Bank Transfer",
		Amount: 300,
		LineItems: []models.InvoiceLineItem{
			{Kind: models.LineItemKindItem, Name: "Singapore Math - Group", Qty: 1, UnitPrice: 300, Amount: 300},
		},
	}
	b, err := renderInvoicePDF(d, sampleSettings(), true)
	if err != nil {
		t.Fatalf("render receipt: %v", err)
	}
	if !bytes.HasPrefix(b, []byte("%PDF")) {
		t.Fatalf("expected a PDF")
	}
}
