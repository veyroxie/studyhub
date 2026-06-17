package pdf

import (
	"bytes"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"studyhub/internal/core"
	"studyhub/internal/mailer"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
	"github.com/jung-kurt/gofpdf"
)

// invoicePDFData is the minimum projection needed to render an invoice or
// receipt PDF. It joins invoice + student rows so the template doesn't need
// to know about the database.
type invoicePDFData struct {
	InvoiceID       string
	Description     string
	StudentName     string
	StudentID       string
	ParentName      string
	ParentEmail     string
	CreatedOn       string
	DueDate         string
	PaidOn          string
	Status          string
	PaymentMethod   string
	ReferenceNo     string
	Amount          float64
	DiscountPct     float64
	SiblingDiscount float64
	ReferralCredit  float64
}

func loadInvoicePDFData(db *store.DB, c *core.Claims, invoiceID string) (invoicePDFData, error) {
	var d invoicePDFData
	var paidOn sql.NullString
	tw, twArgs := store.ScopeTenant(c, "i")
	args := append([]any{invoiceID}, twArgs...)
	err := db.QueryRow(`
		SELECT i.id, i.description, i.amount, i.due_date, i.created_on, i.paid_on, i.status,
		       COALESCE(i.payment_method,''), COALESCE(i.reference_no,''),
		       COALESCE(i.discount_pct,0), COALESCE(i.sibling_discount,0), COALESCE(i.referral_credit,0),
		       s.id, s.first_name || ' ' || s.last_name,
		       COALESCE(s.parent_name,''), COALESCE(s.contact,'')
		FROM invoices i
		JOIN students s ON s.id = i.student_id
		WHERE i.id = ? AND i.deleted_at IS NULL`+tw,
		args...).Scan(
		&d.InvoiceID, &d.Description, &d.Amount, &d.DueDate, &d.CreatedOn, &paidOn, &d.Status,
		&d.PaymentMethod, &d.ReferenceNo,
		&d.DiscountPct, &d.SiblingDiscount, &d.ReferralCredit,
		&d.StudentID, &d.StudentName,
		&d.ParentName, &d.ParentEmail,
	)
	if err != nil {
		return d, err
	}
	if paidOn.Valid {
		d.PaidOn = paidOn.String
	}
	return d, nil
}

// renderInvoicePDF lays out an invoice page and returns the bytes. Lays out
// the line items by reversing the discounts that were stored on the row so
// the parent sees the breakdown that produced the total.
func renderInvoicePDF(d invoicePDFData, paid bool) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 15, 15)
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 22)
	pdf.SetTextColor(15, 15, 15)
	pdf.Cell(0, 10, mailer.Brand().BrandName)
	pdf.Ln(10)
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(120, 120, 120)
	if paid {
		pdf.Cell(0, 6, "Receipt")
	} else {
		pdf.Cell(0, 6, "Invoice")
	}
	pdf.Ln(10)

	if paid {
		pdf.SetFont("Helvetica", "B", 28)
		pdf.SetTextColor(34, 197, 94)
		pdf.CellFormat(0, 14, "PAID", "", 0, "R", false, 0, "")
		pdf.Ln(14)
	}

	pdf.SetTextColor(60, 60, 60)
	pdf.SetFont("Helvetica", "", 10)
	infoBlock(pdf, "Invoice #", d.InvoiceID)
	infoBlock(pdf, "Issued", d.CreatedOn)
	infoBlock(pdf, "Due", d.DueDate)
	if paid {
		infoBlock(pdf, "Paid on", d.PaidOn)
		infoBlock(pdf, "Method", d.PaymentMethod)
		if d.ReferenceNo != "" {
			infoBlock(pdf, "Reference", d.ReferenceNo)
		}
	}
	pdf.Ln(4)

	pdf.SetFont("Helvetica", "B", 11)
	pdf.SetTextColor(15, 15, 15)
	pdf.Cell(0, 7, "Bill to")
	pdf.Ln(6)
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(60, 60, 60)
	pdf.Cell(0, 5, d.ParentName)
	pdf.Ln(5)
	pdf.Cell(0, 5, d.ParentEmail)
	pdf.Ln(5)
	pdf.Cell(0, 5, "Student: "+d.StudentName+" ("+d.StudentID+")")
	pdf.Ln(10)

	pdf.SetFillColor(248, 248, 245)
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetTextColor(15, 15, 15)
	pdf.CellFormat(120, 8, " Description", "", 0, "L", true, 0, "")
	pdf.CellFormat(60, 8, "Amount (RM)", "", 0, "R", true, 0, "")
	pdf.Ln(8)

	// Reverse the stored discounts so the breakdown sums to the total.
	gross := d.Amount
	if d.DiscountPct > 0 {
		gross = gross / (1 - d.DiscountPct/100)
	}
	gross = gross + d.SiblingDiscount + d.ReferralCredit
	earlyBird := 0.0
	if d.DiscountPct == 0 && gross-d.Amount-d.SiblingDiscount-d.ReferralCredit > 0.001 {
		earlyBird = gross - d.Amount - d.SiblingDiscount - d.ReferralCredit
	}

	lineRow(pdf, " "+d.Description, gross)
	if earlyBird > 0 {
		lineRow(pdf, " Early bird discount", -earlyBird)
	}
	if d.DiscountPct > 0 {
		lineRow(pdf, fmt.Sprintf(" Discount (%g%%)", d.DiscountPct), -(gross * d.DiscountPct / 100))
	}
	if d.SiblingDiscount > 0 {
		lineRow(pdf, " Sibling discount", -d.SiblingDiscount)
	}
	if d.ReferralCredit > 0 {
		lineRow(pdf, " Referral discount", -d.ReferralCredit)
	}

	pdf.Ln(2)
	pdf.SetDrawColor(220, 220, 215)
	pdf.Line(15, pdf.GetY(), 195, pdf.GetY())
	pdf.Ln(2)

	pdf.SetFont("Helvetica", "B", 12)
	pdf.SetTextColor(15, 15, 15)
	pdf.CellFormat(120, 9, " Total", "", 0, "L", false, 0, "")
	pdf.CellFormat(60, 9, fmt.Sprintf("RM %.2f", d.Amount), "", 0, "R", false, 0, "")
	pdf.Ln(15)

	if !paid {
		pdf.SetFont("Helvetica", "I", 9)
		pdf.SetTextColor(120, 120, 120)
		pdf.MultiCell(0, 5,
			"Pay before "+d.DueDate+" to keep your subscription active. "+
				"Bank transfers and QR payments require a reference number on submission.",
			"", "L", false)
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func infoBlock(pdf *gofpdf.Fpdf, label, value string) {
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(120, 120, 120)
	pdf.CellFormat(28, 5, label, "", 0, "L", false, 0, "")
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetTextColor(20, 20, 20)
	pdf.CellFormat(0, 5, value, "", 0, "L", false, 0, "")
	pdf.Ln(5)
}

func lineRow(pdf *gofpdf.Fpdf, label string, amount float64) {
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(50, 50, 50)
	pdf.CellFormat(120, 7, label, "", 0, "L", false, 0, "")
	pdf.CellFormat(60, 7, fmt.Sprintf("RM %.2f", amount), "", 0, "R", false, 0, "")
	pdf.Ln(7)
}

// handleInvoicePDF returns the invoice PDF for download. Parents can only
// fetch invoices that belong to their own children. Receipt mode requires
// the invoice to be Paid.
func HandleInvoicePDF(db *store.DB, receipt bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		id := chi.URLParam(r, "id")

		d, err := loadInvoicePDFData(db, c, id)
		if err != nil {
			core.RespondError(w, "invoice not found", 404)
			return
		}
		if c != nil && c.Role == "parent" && d.ParentEmail != c.Email {
			core.RespondError(w, "not your invoice", 403)
			return
		}
		if receipt && !strings.EqualFold(d.Status, "Paid") {
			core.RespondError(w, "invoice is not paid yet", 400)
			return
		}

		bytes, err := renderInvoicePDF(d, receipt)
		if err != nil {
			core.RespondError(w, "could not render PDF", 500)
			return
		}

		filename := "invoice-" + d.InvoiceID + ".pdf"
		if receipt {
			filename = "receipt-" + d.InvoiceID + ".pdf"
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		w.Write(bytes)
	}
}
