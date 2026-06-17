package pdf

import (
	"bytes"
	"database/sql"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"studyhub/internal/core"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
	"github.com/jung-kurt/gofpdf"
)

// invoicePDFData is the minimum projection needed to render an invoice or
// receipt PDF. It joins invoice + student rows so the template doesn't need
// to know about the database.
type invoicePDFData struct {
	InvoiceID       string
	ReceiptNo       string
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
		SELECT i.id, COALESCE(i.receipt_no,''), i.description, i.amount, i.due_date, i.created_on, i.paid_on, i.status,
		       COALESCE(i.payment_method,''), COALESCE(i.reference_no,''),
		       COALESCE(i.discount_pct,0), COALESCE(i.sibling_discount,0), COALESCE(i.referral_credit,0),
		       s.id, s.first_name || ' ' || s.last_name,
		       COALESCE(s.parent_name,''), COALESCE(s.contact,'')
		FROM invoices i
		JOIN students s ON s.id = i.student_id
		WHERE i.id = ? AND i.deleted_at IS NULL`+tw,
		args...).Scan(
		&d.InvoiceID, &d.ReceiptNo, &d.Description, &d.Amount, &d.DueDate, &d.CreatedOn, &paidOn, &d.Status,
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

// rmFmt formats a ringgit amount the way a Malaysian invoice presents it.
func rmFmt(amount float64) string {
	return fmt.Sprintf("RM %.2f", amount)
}

var htmlTagRE = regexp.MustCompile(`<[^>]*>`)

// stripHTML reduces an HTML footer note to plain text for the PDF. The footer
// is stored as HTML for the email/web surface; the PDF only wants the words.
func stripHTML(s string) string {
	return strings.TrimSpace(htmlTagRE.ReplaceAllString(s, ""))
}

// renderInvoicePDF lays out an A4 invoice (or receipt) following Malaysian
// convention: letterhead with registered name + SSM no + address, document
// title, party block, a line-item table, totals, amount in words, bank /
// payment details, terms, and footer. Discounts stored on the row are
// reversed so the gross line + discount lines sum back to the stored total.
func renderInvoicePDF(d invoicePDFData, s *store.TenantSettings, paid bool) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 15, 15)
	pdf.AddPage()

	renderLetterhead(pdf, s)
	renderTitleBlock(pdf, d, paid)
	renderBillTo(pdf, d)
	gross, discounts := renderLineItems(pdf, d)
	renderTotals(pdf, d, gross, discounts)
	renderPaymentSection(pdf, d, s, paid)
	renderTerms(pdf, s)
	renderFooter(pdf, s)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func renderLetterhead(pdf *gofpdf.Fpdf, s *store.TenantSettings) {
	pdf.SetFont("Helvetica", "B", 20)
	pdf.SetTextColor(15, 15, 15)
	pdf.Cell(0, 9, s.BrandName)
	pdf.Ln(8)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(110, 110, 110)
	if s.BrandTagline != "" {
		pdf.Cell(0, 5, s.BrandTagline)
		pdf.Ln(5)
	}
	if s.AddressLine1 != "" {
		pdf.Cell(0, 5, s.AddressLine1)
		pdf.Ln(5)
	}
	if s.AddressLine2 != "" {
		pdf.Cell(0, 5, s.AddressLine2)
		pdf.Ln(5)
	}
	contact := joinNonEmpty([]string{s.SupportPhone, s.SupportEmail}, "  |  ")
	if contact != "" {
		pdf.Cell(0, 5, contact)
		pdf.Ln(5)
	}
	if s.TaxID != "" {
		pdf.Cell(0, 5, "SSM/Co. Reg. No: "+s.TaxID)
		pdf.Ln(5)
	}
	pdf.Ln(2)
	pdf.SetDrawColor(220, 220, 215)
	pdf.Line(15, pdf.GetY(), 195, pdf.GetY())
	pdf.Ln(4)
}

func renderTitleBlock(pdf *gofpdf.Fpdf, d invoicePDFData, paid bool) {
	title := "INVOICE"
	if paid {
		title = "OFFICIAL RECEIPT"
	}
	pdf.SetFont("Helvetica", "B", 16)
	pdf.SetTextColor(15, 15, 15)
	pdf.Cell(0, 9, title)
	if paid {
		pdf.SetFont("Helvetica", "B", 24)
		pdf.SetTextColor(34, 197, 94)
		pdf.CellFormat(0, 9, "PAID", "", 0, "R", false, 0, "")
	}
	pdf.Ln(11)

	pdf.SetTextColor(60, 60, 60)
	if paid && d.ReceiptNo != "" {
		infoBlock(pdf, "Receipt No", d.ReceiptNo)
	}
	infoBlock(pdf, "Invoice No", d.InvoiceID)
	infoBlock(pdf, "Invoice Date", d.CreatedOn)
	infoBlock(pdf, "Due Date", d.DueDate)
	infoBlock(pdf, "Reference", d.InvoiceID)
	pdf.Ln(3)
}

func renderBillTo(pdf *gofpdf.Fpdf, d invoicePDFData) {
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetTextColor(15, 15, 15)
	pdf.Cell(0, 6, "Bill To")
	pdf.Ln(6)
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(60, 60, 60)
	pdf.Cell(0, 5, d.ParentName)
	pdf.Ln(5)
	if d.ParentEmail != "" {
		pdf.Cell(0, 5, d.ParentEmail)
		pdf.Ln(5)
	}
	pdf.Cell(0, 5, "Student: "+d.StudentName+" ("+d.StudentID+")")
	pdf.Ln(9)
}

// renderLineItems draws the Description | Qty | Unit Price | Tax% | Amount
// table. Current invoices carry a single gross figure, so we render one item
// row at the reconstructed gross price, then discount rows beneath it.
// Returns the gross subtotal and the total discount applied.
func renderLineItems(pdf *gofpdf.Fpdf, d invoicePDFData) (float64, float64) {
	pdf.SetFillColor(248, 248, 245)
	pdf.SetFont("Helvetica", "B", 9)
	pdf.SetTextColor(15, 15, 15)
	itemTableHeader(pdf)

	gross := reconstructGross(d)
	earlyBird := earlyBirdDiscount(d, gross)

	itemRow(pdf, d.Description, 1, gross, 0, gross)

	discounts := 0.0
	if earlyBird > 0 {
		discountRow(pdf, "Early bird discount", -earlyBird)
		discounts += earlyBird
	}
	if d.DiscountPct > 0 {
		amt := gross * d.DiscountPct / 100
		discountRow(pdf, fmt.Sprintf("Discount (%g%%)", d.DiscountPct), -amt)
		discounts += amt
	}
	if d.SiblingDiscount > 0 {
		discountRow(pdf, "Sibling discount", -d.SiblingDiscount)
		discounts += d.SiblingDiscount
	}
	if d.ReferralCredit > 0 {
		discountRow(pdf, "Referral discount", -d.ReferralCredit)
		discounts += d.ReferralCredit
	}
	return gross, discounts
}

// reconstructGross reverses the stored discounts to recover the pre-discount
// figure, mirroring how billing applied them in the first place.
func reconstructGross(d invoicePDFData) float64 {
	gross := d.Amount
	if d.DiscountPct > 0 {
		gross = gross / (1 - d.DiscountPct/100)
	}
	return gross + d.SiblingDiscount + d.ReferralCredit
}

// earlyBirdDiscount detects an early-bird reduction that wasn't stored as a
// percentage — the leftover between gross and the known components.
func earlyBirdDiscount(d invoicePDFData, gross float64) float64 {
	const epsilon = 0.001
	leftover := gross - d.Amount - d.SiblingDiscount - d.ReferralCredit
	if d.DiscountPct == 0 && leftover > epsilon {
		return leftover
	}
	return 0
}

func renderTotals(pdf *gofpdf.Fpdf, d invoicePDFData, gross, discounts float64) {
	pdf.Ln(1)
	pdf.SetDrawColor(220, 220, 215)
	pdf.Line(110, pdf.GetY(), 195, pdf.GetY())
	pdf.Ln(1)

	totalRow(pdf, "Subtotal", rmFmt(gross), false)
	if discounts > 0 {
		totalRow(pdf, "Discount", "-"+rmFmt(discounts), false)
	}
	totalRow(pdf, "Total", rmFmt(d.Amount), true)
	pdf.Ln(4)

	pdf.SetFont("Helvetica", "I", 9)
	pdf.SetTextColor(60, 60, 60)
	pdf.MultiCell(0, 5, amountInWords(d.Amount), "", "L", false)
	pdf.Ln(4)
}

func renderPaymentSection(pdf *gofpdf.Fpdf, d invoicePDFData, s *store.TenantSettings, paid bool) {
	if paid {
		renderReceiptDetails(pdf, d)
		return
	}
	renderBankDetails(pdf, s)
}

func renderReceiptDetails(pdf *gofpdf.Fpdf, d invoicePDFData) {
	sectionHeading(pdf, "Payment Received")
	if d.ReceiptNo != "" {
		labelValue(pdf, "Receipt No", d.ReceiptNo)
	}
	labelValue(pdf, "Paid On", d.PaidOn)
	labelValue(pdf, "Method", d.PaymentMethod)
	if d.ReferenceNo != "" {
		labelValue(pdf, "Reference", d.ReferenceNo)
	}
	pdf.Ln(3)
}

func renderBankDetails(pdf *gofpdf.Fpdf, s *store.TenantSettings) {
	hasBank := s.BankName != "" || s.BankAccountNo != "" || s.BankAccountHolder != ""
	if !hasBank && s.PaymentInstructions == "" {
		return
	}
	sectionHeading(pdf, "Payment Details")
	if s.BankName != "" {
		labelValue(pdf, "Bank", s.BankName)
	}
	if s.BankAccountHolder != "" {
		labelValue(pdf, "Account Name", s.BankAccountHolder)
	}
	if s.BankAccountNo != "" {
		labelValue(pdf, "Account No", s.BankAccountNo)
	}
	if s.PaymentInstructions != "" {
		pdf.Ln(1)
		pdf.SetFont("Helvetica", "", 9)
		pdf.SetTextColor(60, 60, 60)
		pdf.MultiCell(0, 5, s.PaymentInstructions, "", "L", false)
	}
	pdf.Ln(3)
}

func renderTerms(pdf *gofpdf.Fpdf, s *store.TenantSettings) {
	if s.InvoiceTerms == "" {
		return
	}
	sectionHeading(pdf, "Terms & Conditions")
	pdf.SetFont("Helvetica", "", 8)
	pdf.SetTextColor(110, 110, 110)
	pdf.MultiCell(0, 4, s.InvoiceTerms, "", "L", false)
	pdf.Ln(3)
}

func renderFooter(pdf *gofpdf.Fpdf, s *store.TenantSettings) {
	footer := stripHTML(s.InvoiceFooterHTML)
	if footer == "" {
		return
	}
	pdf.SetDrawColor(230, 230, 225)
	pdf.Line(15, pdf.GetY(), 195, pdf.GetY())
	pdf.Ln(3)
	pdf.SetFont("Helvetica", "I", 8)
	pdf.SetTextColor(140, 140, 140)
	pdf.MultiCell(0, 4, footer, "", "C", false)
}

// ── low-level cell helpers ───────────────────────────────────────────────────

func infoBlock(pdf *gofpdf.Fpdf, label, value string) {
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(120, 120, 120)
	pdf.CellFormat(30, 5, label, "", 0, "L", false, 0, "")
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetTextColor(20, 20, 20)
	pdf.CellFormat(0, 5, value, "", 0, "L", false, 0, "")
	pdf.Ln(5)
}

func sectionHeading(pdf *gofpdf.Fpdf, label string) {
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetTextColor(15, 15, 15)
	pdf.Cell(0, 6, label)
	pdf.Ln(6)
}

func labelValue(pdf *gofpdf.Fpdf, label, value string) {
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(120, 120, 120)
	pdf.CellFormat(32, 5, label, "", 0, "L", false, 0, "")
	pdf.SetFont("Helvetica", "B", 9)
	pdf.SetTextColor(40, 40, 40)
	pdf.CellFormat(0, 5, value, "", 0, "L", false, 0, "")
	pdf.Ln(5)
}

// itemTableHeader / itemRow / discountRow share this column layout:
// Description 90 | Qty 15 | Unit Price 30 | Tax% 15 | Amount 30 (= 180mm).
func itemTableHeader(pdf *gofpdf.Fpdf) {
	pdf.CellFormat(90, 8, " Description", "", 0, "L", true, 0, "")
	pdf.CellFormat(15, 8, "Qty", "", 0, "C", true, 0, "")
	pdf.CellFormat(30, 8, "Unit Price (RM)", "", 0, "R", true, 0, "")
	pdf.CellFormat(15, 8, "Tax %", "", 0, "C", true, 0, "")
	pdf.CellFormat(30, 8, "Amount (RM) ", "", 0, "R", true, 0, "")
	pdf.Ln(8)
}

func itemRow(pdf *gofpdf.Fpdf, desc string, qty int, unitPrice, taxPct, amount float64) {
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(40, 40, 40)
	pdf.CellFormat(90, 7, " "+desc, "", 0, "L", false, 0, "")
	pdf.CellFormat(15, 7, fmt.Sprintf("%d", qty), "", 0, "C", false, 0, "")
	pdf.CellFormat(30, 7, fmt.Sprintf("%.2f", unitPrice), "", 0, "R", false, 0, "")
	pdf.CellFormat(15, 7, fmt.Sprintf("%g", taxPct), "", 0, "C", false, 0, "")
	pdf.CellFormat(30, 7, fmt.Sprintf("%.2f ", amount), "", 0, "R", false, 0, "")
	pdf.Ln(7)
}

func discountRow(pdf *gofpdf.Fpdf, label string, amount float64) {
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(90, 90, 90)
	pdf.CellFormat(90, 7, " "+label, "", 0, "L", false, 0, "")
	pdf.CellFormat(15, 7, "", "", 0, "C", false, 0, "")
	pdf.CellFormat(30, 7, "", "", 0, "R", false, 0, "")
	pdf.CellFormat(15, 7, "", "", 0, "C", false, 0, "")
	pdf.CellFormat(30, 7, fmt.Sprintf("%.2f ", amount), "", 0, "R", false, 0, "")
	pdf.Ln(7)
}

func totalRow(pdf *gofpdf.Fpdf, label, value string, emphasise bool) {
	style := ""
	size := 10.0
	if emphasise {
		style = "B"
		size = 12
		pdf.SetTextColor(15, 15, 15)
	} else {
		pdf.SetTextColor(70, 70, 70)
	}
	pdf.SetFont("Helvetica", style, size)
	pdf.CellFormat(110, 8, "", "", 0, "L", false, 0, "")
	pdf.CellFormat(45, 8, label, "", 0, "R", false, 0, "")
	pdf.CellFormat(25, 8, value+" ", "", 0, "R", false, 0, "")
	pdf.Ln(8)
}

func joinNonEmpty(parts []string, sep string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}

// ── amount in words ──────────────────────────────────────────────────────────

var onesWords = []string{
	"", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine",
	"Ten", "Eleven", "Twelve", "Thirteen", "Fourteen", "Fifteen", "Sixteen",
	"Seventeen", "Eighteen", "Nineteen",
}

var tensWords = []string{
	"", "", "Twenty", "Thirty", "Forty", "Fifty", "Sixty", "Seventy", "Eighty", "Ninety",
}

// amountInWords renders a ringgit amount in the Malaysian receipt phrasing,
// e.g. 780.00 -> "Ringgit Malaysia Seven Hundred Eighty Only" and
// 780.50 -> "Ringgit Malaysia Seven Hundred Eighty and Sen Fifty Only".
// Correct for 0 .. 999,999.99.
func amountInWords(rm float64) string {
	const centsPerRinggit = 100
	whole := int(rm)
	sen := int((rm-float64(whole))*centsPerRinggit + 0.5)
	words := "Ringgit Malaysia " + wholeNumberWords(whole)
	if sen > 0 {
		words += " and Sen " + threeDigitWords(sen)
	}
	return words + " Only"
}

func wholeNumberWords(n int) string {
	if n == 0 {
		return "Zero"
	}
	const thousand = 1000
	if n < thousand {
		return threeDigitWords(n)
	}
	parts := []string{}
	if thousands := n / thousand; thousands > 0 {
		parts = append(parts, threeDigitWords(thousands)+" Thousand")
	}
	if remainder := n % thousand; remainder > 0 {
		parts = append(parts, threeDigitWords(remainder))
	}
	return strings.Join(parts, " ")
}

func threeDigitWords(n int) string {
	const hundred = 100
	parts := []string{}
	if h := n / hundred; h > 0 {
		parts = append(parts, onesWords[h]+" Hundred")
	}
	if rem := n % hundred; rem > 0 {
		parts = append(parts, twoDigitWords(rem))
	}
	return strings.Join(parts, " ")
}

func twoDigitWords(n int) string {
	const twenty = 20
	if n < twenty {
		return onesWords[n]
	}
	const ten = 10
	tens := tensWords[n/ten]
	if ones := n % ten; ones > 0 {
		return tens + " " + onesWords[ones]
	}
	return tens
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

		s := store.LoadTenantSettings(db, store.TenantID(c))
		bytes, err := renderInvoicePDF(d, s, receipt)
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
