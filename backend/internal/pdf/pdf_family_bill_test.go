package pdf

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jung-kurt/gofpdf"

	"studyhub/internal/models"
)

func familySample() familyBillPDFData {
	child := func(id, name string, amount float64, status string) invoicePDFData {
		return invoicePDFData{
			InvoiceID: id, StudentName: name, ParentName: "Mrs Tan", DueDate: "2026-10-07", Status: status, Amount: amount,
			ReceiptNo: "RCPT-00000" + id[len(id)-1:], PaidOn: "2026-10-03", PaymentMethod: "Bank Transfer",
			LineItems: []models.InvoiceLineItem{
				{Kind: models.LineItemKindItem, Name: "Group — Level 3", Qty: 1, UnitPrice: amount + 20, Amount: amount + 20},
				{Kind: models.LineItemKindDiscount, Name: "Sibling discount", Amount: -10},
				{Kind: models.LineItemKindDiscount, Name: "Early bird discount", Amount: -10},
			},
		}
	}
	return familyBillPDFData{Period: "2026-10", ParentName: "Mrs Tan", ParentEmail: "tan@example.com",
		Children: []invoicePDFData{child("INV-2026-0001", "Zayden Tan", 230, "Unpaid"), child("INV-2026-0002", "Lucy Tan", 250, "Unpaid")}}
}

func TestAFamilyBillRendersEveryChildAndTheReceipt(t *testing.T) {
	for _, paid := range []bool{false, true} {
		out, err := renderFamilyBillPDF(familySample(), sampleSettings(), paid)
		if err != nil {
			t.Fatalf("paid=%v: %v", paid, err)
		}
		if !bytes.HasPrefix(out, []byte("%PDF")) {
			t.Fatalf("paid=%v: output is not a PDF", paid)
		}
	}
}

func TestTheFamilyTotalIsTheSumOfTheChildren(t *testing.T) {
	if got := familySample().total(); got != 480 {
		t.Fatalf("total %.2f, want 480", got)
	}
}

// Same guard as TestLineItemTextIsTranslatedForTheCoreFont, for the fields only the family bill prints.
func TestFamilyBillTextIsTranslatedForTheCoreFont(t *testing.T) {
	tr := gofpdf.New("P", "mm", "A4", "").UnicodeTranslatorFromDescriptor("")
	f := familySample()
	f.ParentName = "Puan Siti — Rahman"
	f.Children[0].InvoiceID = "INV — 1"
	out := translateFamilyBillData(f, tr)
	for label, got := range map[string]string{
		"parent":     out.ParentName,
		"invoice no": out.Children[0].InvoiceID,
		"line item":  out.Children[0].LineItems[0].Name,
	} {
		if strings.Contains(got, "—") || !strings.Contains(got, "\x97") {
			t.Errorf("%s not encoded for cp1252: %q", label, got)
		}
	}
}
