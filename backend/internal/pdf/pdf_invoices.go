package pdf

import (
	"bytes"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
	"time"

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
	LineItems       []models.InvoiceLineItem
	LogoPath        string
}

func loadInvoicePDFData(db *store.DB, c *core.Claims, invoiceID string) (invoicePDFData, error) {
	var d invoicePDFData
	var paidOn sql.NullString
	var lineItems string
	tw, twArgs := store.ScopeTenant(c, "i")
	args := append([]any{invoiceID}, twArgs...)
	err := db.QueryRow(`
		SELECT i.id, COALESCE(i.receipt_no,''), i.description, i.amount, i.due_date, i.created_on, i.paid_on, i.status,
		       COALESCE(i.payment_method,''), COALESCE(i.reference_no,''),
		       COALESCE(i.discount_pct,0), COALESCE(i.sibling_discount,0), COALESCE(i.referral_credit,0),
		       COALESCE(i.line_items,'[]'),
		       COALESCE(NULLIF(s.student_no,''), s.id), s.first_name || ' ' || s.last_name,
		       COALESCE(s.parent_name,''), COALESCE(s.contact,'')
		FROM invoices i
		JOIN students s ON s.id = i.student_id
		WHERE i.id = ? AND i.deleted_at IS NULL`+tw,
		args...).Scan(
		&d.InvoiceID, &d.ReceiptNo, &d.Description, &d.Amount, &d.DueDate, &d.CreatedOn, &paidOn, &d.Status,
		&d.PaymentMethod, &d.ReferenceNo,
		&d.DiscountPct, &d.SiblingDiscount, &d.ReferralCredit,
		&lineItems,
		&d.StudentID, &d.StudentName,
		&d.ParentName, &d.ParentEmail,
	)
	if err != nil {
		return d, err
	}
	if paidOn.Valid {
		d.PaidOn = paidOn.String
	}
	d.LineItems = models.ParseLineItems(lineItems)
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

// fmtDMY renders an ISO date (2006-01-02) as "02 Jan 2006", the format the
// reference invoice uses. Falls back to the raw string if it doesn't parse.
func fmtDMY(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return t.Format("02 Jan 2006")
}

const (
	pageLeft  = 15.0
	pageRight = 195.0
	amountCol = 45.0 // right-hand "Amount (RM)" column width
)

// renderInvoicePDF lays out an A4 invoice (or receipt) in the centre's
// Skooly-style format: centered letterhead with logo, a two-column
// Items | Amount table where each item carries a descriptor and a qty sub-line,
// a totals block, a payment note, and numbered terms. Legacy flat invoices
// (no stored line items) are rendered via a synthesized single line so nothing
// created before this format still prints correctly.
func renderInvoicePDF(d invoicePDFData, s *store.TenantSettings, paid bool) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(pageLeft, 15, 15)
	pdf.AddPage()

	// gofpdf's core fonts are cp1252-encoded, so UTF-8 text (em-dashes from the
	// cron descriptions, accented names) must be translated or it prints as
	// mojibake. Translate every data string once, up front, so the render
	// helpers can stay encoding-agnostic. LogoPath is a real filesystem path and
	// must NOT be translated.
	tr := pdf.UnicodeTranslatorFromDescriptor("")
	d = translateInvoiceData(d, tr)
	s = translateSettings(s, tr)

	items := d.LineItems
	if len(items) == 0 {
		items = synthesizeLineItems(d)
	}

	renderLetterhead(pdf, s, d.LogoPath)
	renderTitleBlock(pdf, d, paid)
	renderPartyRef(pdf, d)
	renderInfoRows(pdf, d, paid)
	renderItemsTable(pdf, items)
	renderTotalsBlock(pdf, d, items)
	renderNoteBlock(pdf, d, s, paid)
	renderTermsNumbered(pdf, s)
	renderFooter(pdf, s)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// synthesizeLineItems reconstructs a single item line plus discount lines for
// legacy invoices that pre-date the line_items column, so they render in the
// same two-column table as modern multi-line invoices.
func synthesizeLineItems(d invoicePDFData) []models.InvoiceLineItem {
	gross := reconstructGross(d)
	items := []models.InvoiceLineItem{{
		Kind: models.LineItemKindItem, Name: d.Description,
		Qty: 1, UnitPrice: gross, Amount: gross,
	}}
	if earlyBird := earlyBirdDiscount(d, gross); earlyBird > 0 {
		items = appendDiscountLine(items, "Early bird discount", earlyBird)
	}
	if d.DiscountPct > 0 {
		items = appendDiscountLine(items, fmt.Sprintf("Discount (%g%%)", d.DiscountPct), gross*d.DiscountPct/100)
	}
	items = appendDiscountLine(items, "Sibling discount", d.SiblingDiscount)
	items = appendDiscountLine(items, "Referral discount", d.ReferralCredit)
	return items
}

// translateInvoiceData returns a copy of d with every printed string mapped
// through the cp1252 translator. LogoPath is deliberately left untranslated.
func translateInvoiceData(d invoicePDFData, tr func(string) string) invoicePDFData {
	d.Description = tr(d.Description)
	d.StudentName = tr(d.StudentName)
	d.StudentID = tr(d.StudentID)
	d.ParentName = tr(d.ParentName)
	d.ParentEmail = tr(d.ParentEmail)
	d.ReceiptNo = tr(d.ReceiptNo)
	d.ReferenceNo = tr(d.ReferenceNo)
	d.PaymentMethod = tr(d.PaymentMethod)
	items := make([]models.InvoiceLineItem, len(d.LineItems))
	for i, it := range d.LineItems {
		it.Name = tr(it.Name)
		it.Descriptor = tr(it.Descriptor)
		details := make([]string, len(it.Details))
		for j, line := range it.Details {
			details[j] = tr(line)
		}
		it.Details = details
		items[i] = it
	}
	d.LineItems = items
	return d
}

// translateSettings returns a copy of the tenant settings with printed strings
// mapped through the cp1252 translator. LogoPath is left untranslated so the
// image file still resolves on disk.
func translateSettings(s *store.TenantSettings, tr func(string) string) *store.TenantSettings {
	c := *s
	c.BrandName = tr(c.BrandName)
	c.BrandTagline = tr(c.BrandTagline)
	c.AddressLine1 = tr(c.AddressLine1)
	c.AddressLine2 = tr(c.AddressLine2)
	c.SupportEmail = tr(c.SupportEmail)
	c.SupportPhone = tr(c.SupportPhone)
	c.TaxID = tr(c.TaxID)
	c.BankName = tr(c.BankName)
	c.BankAccountNo = tr(c.BankAccountNo)
	c.BankAccountHolder = tr(c.BankAccountHolder)
	c.PaymentInstructions = tr(c.PaymentInstructions)
	c.InvoiceTerms = tr(c.InvoiceTerms)
	c.InvoiceFooterHTML = tr(c.InvoiceFooterHTML)
	return &c
}

func appendDiscountLine(items []models.InvoiceLineItem, name string, amt float64) []models.InvoiceLineItem {
	if amt <= 0 {
		return items
	}
	return append(items, models.InvoiceLineItem{Kind: models.LineItemKindDiscount, Name: name, Amount: -amt})
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

// ── letterhead ───────────────────────────────────────────────────────────────

func renderLetterhead(pdf *gofpdf.Fpdf, s *store.TenantSettings, logoPath string) {
	renderLogo(pdf, logoPath)
	pdf.SetFont("Helvetica", "B", 18)
	pdf.SetTextColor(15, 15, 15)
	centeredLine(pdf, s.BrandName, 8)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(90, 90, 90)
	centeredLine(pdf, s.BrandTagline, 5)
	centeredLine(pdf, joinNonEmpty([]string{prefixed("Email: ", s.SupportEmail), prefixed("Contact: ", s.SupportPhone)}, "   "), 5)
	centeredLine(pdf, joinNonEmpty([]string{s.AddressLine1, s.AddressLine2}, ", "), 5)
	if s.TaxID != "" {
		centeredLine(pdf, "SSM/Co. Reg. No: "+s.TaxID, 5)
	}
	pdf.Ln(3)
}

// renderLogo embeds the tenant logo centered above the letterhead. A missing or
// malformed file must never abort the render, so the image error state is
// cleared and the logo silently skipped.
// bundledLogoPath resolves the shipped default logo, mirroring server.go's dev
// (../frontend) vs Docker (./frontend) working-directory split. Empty when not
// found, which renderLogo treats as "no logo" (skips silently).
func bundledLogoPath() string {
	for _, p := range []string{"../frontend/logo.png", "frontend/logo.png"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func renderLogo(pdf *gofpdf.Fpdf, logoPath string) {
	if logoPath == "" {
		return
	}
	if _, err := os.Stat(logoPath); err != nil {
		return
	}
	imgType := strings.TrimPrefix(strings.ToLower(filepath.Ext(logoPath)), ".")
	if imgType == "jpeg" {
		imgType = "jpg"
	}
	if imgType != "png" && imgType != "jpg" {
		return
	}
	const logoW = 30.0
	opts := gofpdf.ImageOptions{ImageType: imgType, ReadDpi: true}
	pdf.RegisterImageOptions(logoPath, opts)
	if !pdf.Ok() {
		pdf.ClearError()
		return
	}
	x := (pageLeft + pageRight - logoW) / 2
	pdf.ImageOptions(logoPath, x, pdf.GetY(), logoW, 0, true, opts, 0, "")
	if !pdf.Ok() {
		pdf.ClearError()
		return
	}
	pdf.Ln(2)
}

// ── title + info ─────────────────────────────────────────────────────────────

func renderTitleBlock(pdf *gofpdf.Fpdf, d invoicePDFData, paid bool) {
	title := "INVOICE"
	if paid {
		title = "OFFICIAL RECEIPT"
	}
	pdf.SetFont("Helvetica", "B", 16)
	pdf.SetTextColor(15, 15, 15)
	centeredLine(pdf, title, 10)
	if paid {
		pdf.SetFont("Helvetica", "B", 13)
		pdf.SetTextColor(34, 197, 94)
		centeredLine(pdf, "PAID", 7)
	}
	pdf.Ln(2)
}

// renderPartyRef prints the "<student> — <id> — <parent>" reference line the
// reference invoice shows directly under the title.
func renderPartyRef(pdf *gofpdf.Fpdf, d invoicePDFData) {
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetTextColor(15, 15, 15)
	ref := joinNonEmpty([]string{d.StudentName, d.StudentID, d.ParentName}, " - ")
	pdf.MultiCell(0, 6, ref, "", "L", false)
	pdf.Ln(2)
}

func renderInfoRows(pdf *gofpdf.Fpdf, d invoicePDFData, paid bool) {
	if paid && d.ReceiptNo != "" {
		infoRow(pdf, "Receipt number", d.ReceiptNo)
	}
	infoRow(pdf, "Invoice number", d.InvoiceID)
	infoRow(pdf, "Due date", fmtDMY(d.DueDate))
	infoRow(pdf, "Invoice date", fmtDMY(d.CreatedOn))
	if d.ReferenceNo != "" {
		infoRow(pdf, "Reference", d.ReferenceNo)
	}
	pdf.Ln(4)
}

// ── items table ──────────────────────────────────────────────────────────────

func renderItemsTable(pdf *gofpdf.Fpdf, items []models.InvoiceLineItem) {
	pdf.SetDrawColor(210, 210, 205)
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetTextColor(15, 15, 15)
	pdf.CellFormat(pageRight-pageLeft-amountCol, 8, "Items", "", 0, "L", false, 0, "")
	pdf.CellFormat(amountCol, 8, "Amount (RM)", "", 0, "R", false, 0, "")
	pdf.Ln(8)
	pdf.Line(pageLeft, pdf.GetY(), pageRight, pdf.GetY())
	pdf.Ln(2)
	// Only positive item rows appear in the table; discount lines are shown in
	// the totals block below (matching the reference invoice layout).
	for _, it := range items {
		if it.Kind == models.LineItemKindDiscount {
			continue
		}
		renderItemBlock(pdf, it)
	}
}

func renderItemBlock(pdf *gofpdf.Fpdf, it models.InvoiceLineItem) {
	const nameW = 120.0
	y0 := pdf.GetY()
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetTextColor(20, 20, 20)
	pdf.SetXY(pageRight-amountCol, y0)
	pdf.CellFormat(amountCol, 6, rmFmt(it.Amount), "", 0, "R", false, 0, "")
	pdf.SetXY(pageLeft, y0)
	pdf.MultiCell(nameW, 6, itemHeading(it), "", "L", false)
	pdf.SetFont("Helvetica", "", 8)
	pdf.SetTextColor(120, 120, 120)
	if it.Descriptor != "" {
		pdf.SetX(pageLeft)
		pdf.MultiCell(nameW, 5, it.Descriptor, "", "L", false)
	}
	for _, line := range it.Details {
		pdf.SetX(pageLeft)
		pdf.MultiCell(nameW, 5, line, "", "L", false)
	}
	if it.Kind == models.LineItemKindItem && it.Qty > 0 {
		pdf.SetX(pageLeft)
		pdf.MultiCell(nameW, 5, fmt.Sprintf("%s x %s", models.FormatQty(it.Qty), rmFmt(it.UnitPrice)), "", "L", false)
	}
	pdf.Ln(2)
	pdf.SetDrawColor(230, 230, 225)
	pdf.Line(pageLeft, pdf.GetY(), pageRight, pdf.GetY())
	pdf.Ln(2)
}

// itemHeading is the bold first line: the item name plus its billing period.
func itemHeading(it models.InvoiceLineItem) string {
	if it.PeriodStart == "" {
		return it.Name
	}
	return it.Name + " (" + fmtDMY(it.PeriodStart) + " to " + fmtDMY(it.PeriodEnd) + ")"
}

// ── totals ───────────────────────────────────────────────────────────────────

func renderTotalsBlock(pdf *gofpdf.Fpdf, d invoicePDFData, items []models.InvoiceLineItem) {
	subtotal := 0.0
	for _, it := range items {
		if it.Amount > 0 {
			subtotal += it.Amount
		}
	}
	pdf.Ln(1)
	totalRow(pdf, "Subtotal", rmFmt(subtotal), false)
	totalRow(pdf, "Total Tax", rmFmt(0), false)
	for _, it := range items {
		if it.Kind == models.LineItemKindDiscount && it.Amount < 0 {
			totalRow(pdf, it.Name, "- "+rmFmt(-it.Amount), false)
		}
	}
	totalRow(pdf, "Total Due", rmFmt(d.Amount), true)
	pdf.Ln(3)
	pdf.SetFont("Helvetica", "I", 9)
	pdf.SetTextColor(90, 90, 90)
	pdf.MultiCell(0, 5, amountInWords(d.Amount), "", "L", false)
	pdf.Ln(3)
}

// ── payment note ─────────────────────────────────────────────────────────────

func renderNoteBlock(pdf *gofpdf.Fpdf, d invoicePDFData, s *store.TenantSettings, paid bool) {
	if paid {
		renderReceiptDetails(pdf, d)
		return
	}
	hasBank := s.BankName != "" || s.BankAccountNo != "" || s.BankAccountHolder != ""
	if !hasBank && s.PaymentInstructions == "" {
		return
	}
	sectionHeading(pdf, "Note")
	if s.BankName != "" {
		labelValue(pdf, "Bank", s.BankName)
	}
	if s.BankAccountHolder != "" {
		labelValue(pdf, "Bank name", s.BankAccountHolder)
	}
	if s.BankAccountNo != "" {
		labelValue(pdf, "Bank account", s.BankAccountNo)
	}
	if s.PaymentInstructions != "" {
		pdf.Ln(1)
		pdf.SetFont("Helvetica", "", 9)
		pdf.SetTextColor(60, 60, 60)
		pdf.MultiCell(0, 5, s.PaymentInstructions, "", "L", false)
	}
	pdf.Ln(3)
}

func renderReceiptDetails(pdf *gofpdf.Fpdf, d invoicePDFData) {
	sectionHeading(pdf, "Payment Received")
	if d.ReceiptNo != "" {
		labelValue(pdf, "Receipt No", d.ReceiptNo)
	}
	labelValue(pdf, "Paid On", fmtDMY(d.PaidOn))
	labelValue(pdf, "Method", d.PaymentMethod)
	if d.ReferenceNo != "" {
		labelValue(pdf, "Reference", d.ReferenceNo)
	}
	pdf.Ln(3)
}

func renderTermsNumbered(pdf *gofpdf.Fpdf, s *store.TenantSettings) {
	if s.InvoiceTerms == "" {
		return
	}
	sectionHeading(pdf, "Terms and conditions")
	pdf.SetFont("Helvetica", "", 8)
	pdf.SetTextColor(110, 110, 110)
	n := 0
	for _, raw := range strings.Split(s.InvoiceTerms, "\n") {
		term := strings.TrimSpace(raw)
		if term == "" {
			continue
		}
		n++
		pdf.MultiCell(0, 4, fmt.Sprintf("%d. %s", n, term), "", "L", false)
	}
	pdf.Ln(3)
}

func renderFooter(pdf *gofpdf.Fpdf, s *store.TenantSettings) {
	footer := stripHTML(s.InvoiceFooterHTML)
	if footer == "" {
		return
	}
	pdf.SetDrawColor(230, 230, 225)
	pdf.Line(pageLeft, pdf.GetY(), pageRight, pdf.GetY())
	pdf.Ln(3)
	pdf.SetFont("Helvetica", "I", 8)
	pdf.SetTextColor(140, 140, 140)
	pdf.MultiCell(0, 4, footer, "", "C", false)
}

// ── low-level cell helpers ───────────────────────────────────────────────────

// centeredLine prints one full-width centered line, skipping empty text.
func centeredLine(pdf *gofpdf.Fpdf, text string, h float64) {
	if text == "" {
		return
	}
	pdf.CellFormat(0, h, text, "", 1, "C", false, 0, "")
}

// prefixed returns "<prefix><value>" or "" when value is empty, so a label
// never prints with a missing value.
func prefixed(prefix, value string) string {
	if value == "" {
		return ""
	}
	return prefix + value
}

func infoRow(pdf *gofpdf.Fpdf, label, value string) {
	pdf.SetFont("Helvetica", "B", 9)
	pdf.SetTextColor(40, 40, 40)
	pdf.CellFormat(45, 6, label, "", 0, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(60, 60, 60)
	pdf.CellFormat(0, 6, value, "", 0, "L", false, 0, "")
	pdf.Ln(6)
}

func sectionHeading(pdf *gofpdf.Fpdf, label string) {
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetTextColor(15, 15, 15)
	pdf.Cell(0, 6, label)
	pdf.Ln(6)
}

func labelValue(pdf *gofpdf.Fpdf, label, value string) {
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(70, 70, 70)
	pdf.Cell(0, 5, label+" : "+value)
	pdf.Ln(5)
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
	pdf.CellFormat(110, 7, "", "", 0, "L", false, 0, "")
	pdf.CellFormat(45, 7, label, "", 0, "R", false, 0, "")
	pdf.CellFormat(25, 7, value+" ", "", 0, "R", false, 0, "")
	pdf.Ln(7)
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
		// Invoice/receipt PDFs are admin + own-family-parent only; teachers and
		// other roles must not be able to pull another family's billing document.
		if c == nil || (c.Role != "parent" && !core.IsAdminRole(c)) {
			core.RespondError(w, "forbidden", http.StatusForbidden)
			return
		}
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
		d.LogoPath = s.LogoPath
		if d.LogoPath == "" {
			d.LogoPath = bundledLogoPath()
		}
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
