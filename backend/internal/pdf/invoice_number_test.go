package pdf

import "testing"

// Parents are quoted the issued number; the internal id printed on every PDF matched nothing they had.
func TestAnIssuedInvoiceShowsItsNumberNotItsID(t *testing.T) {
	d := invoicePDFData{InvoiceID: "INV_20260930164229228cb408540", InvoiceNo: "INV-2026-0034"}
	if got := d.Number(); got != "INV-2026-0034" {
		t.Fatalf("Number() = %q, want the issued number", got)
	}
}

func TestADraftWithoutANumberFallsBackToItsID(t *testing.T) {
	d := invoicePDFData{InvoiceID: "INV_1"}
	if got := d.Number(); got != "INV_1" {
		t.Fatalf("Number() = %q, want the id", got)
	}
}
