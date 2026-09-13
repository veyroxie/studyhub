package pdf

import (
	"strings"
	"testing"

	"github.com/jung-kurt/gofpdf"

	"studyhub/internal/models"
)

// The core PDF fonts are cp1252, so any UTF-8 that reaches them untranslated
// prints as mojibake. Every catalogue line name carries an em dash --
// "Deposit (1 month) — Group Level 4-6" -- so line items are the likeliest
// place for a new field to be added and quietly skip the translator.
//
// translateInvoiceData is the single point that prevents that. This guards it
// directly rather than by inspecting a rendered PDF: content streams are
// compressed, so searching the bytes proves nothing, which I established the
// hard way trying to probe this earlier.
func TestLineItemTextIsTranslatedForTheCoreFont(t *testing.T) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	tr := pdf.UnicodeTranslatorFromDescriptor("")

	d := invoicePDFData{
		StudentName: "Haruto Ouchi",
		LineItems: []models.InvoiceLineItem{{
			Kind:       models.LineItemKindItem,
			Name:       "Deposit (1 month) — Group Level 4-6",
			Descriptor: "Refunded against the final month — held to the end",
			Details:    []string{"Joined 8 September — mid-month"},
		}},
	}
	out := translateInvoiceData(d, tr)

	for label, got := range map[string]string{
		"name":       out.LineItems[0].Name,
		"descriptor": out.LineItems[0].Descriptor,
		"detail":     out.LineItems[0].Details[0],
	} {
		if strings.Contains(got, "—") {
			t.Errorf("%s still holds a raw UTF-8 em dash: it would print as three wrong characters", label)
		}
		if !strings.Contains(got, "\x97") {
			t.Errorf("%s lost its em dash entirely rather than being encoded as cp1252 0x97: %q", label, got)
		}
	}
}
