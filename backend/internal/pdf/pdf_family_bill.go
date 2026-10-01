package pdf

import (
	"bytes"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jung-kurt/gofpdf"

	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

// familyBillPDFData is one parent's bill for one month: each child's invoice, rendered in turn.
type familyBillPDFData struct {
	Period      string
	ParentName  string
	ParentEmail string
	Children    []invoicePDFData
	LogoPath    string
}

func (f familyBillPDFData) total() float64 {
	sum := 0.0
	for _, c := range f.Children {
		sum += c.Amount
	}
	return sum
}

func (f familyBillPDFData) allPaid() bool {
	for _, c := range f.Children {
		if !strings.EqualFold(c.Status, models.InvoiceStatusPaid) {
			return false
		}
	}
	return true
}

// HandleFamilyBillPDF serves the family bill (or its receipt) that the named invoice belongs to.
//
// GET /api/family-bills/{invoiceId}/pdf and /receipt.pdf
func HandleFamilyBillPDF(db *store.DB, receipt bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if c == nil || (c.Role != "parent" && !core.IsAdminRole(c)) {
			core.RespondError(w, "forbidden", http.StatusForbidden)
			return
		}
		f, status, msg := loadFamilyBillPDFData(db, c, chi.URLParam(r, "invoiceId"))
		if status != http.StatusOK {
			core.RespondError(w, msg, status)
			return
		}
		if receipt && !f.allPaid() {
			core.RespondError(w, "the family bill is not fully paid yet", http.StatusBadRequest)
			return
		}
		serveFamilyBillPDF(w, db, c, f, receipt)
	}
}

func serveFamilyBillPDF(w http.ResponseWriter, db *store.DB, c *core.Claims, f familyBillPDFData, receipt bool) {
	s := store.LoadTenantSettings(db, store.TenantID(c))
	f.LogoPath = s.LogoPath
	if f.LogoPath == "" {
		f.LogoPath = bundledLogoPath()
	}
	out, err := renderFamilyBillPDF(f, s, receipt)
	if err != nil {
		core.RespondError(w, "could not render PDF", http.StatusInternalServerError)
		return
	}
	name := "family-bill-" + f.Period + ".pdf"
	if receipt {
		name = "family-receipt-" + f.Period + ".pdf"
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Write(out)
}

// loadFamilyBillPDFData derives the bill from one member, exactly as the pay endpoint does.
func loadFamilyBillPDFData(db *store.DB, c *core.Claims, invoiceID string) (familyBillPDFData, int, string) {
	var f familyBillPDFData
	var tid int
	tw, twArgs := store.ScopeTenant(c, "i")
	err := db.QueryRow(`SELECT i.tenant_id, COALESCE(i.period,''), COALESCE(s.contact,'') FROM invoices i
		JOIN students s ON s.id=i.student_id AND s.tenant_id=i.tenant_id
		WHERE i.id=? AND i.type='Monthly' AND i.deleted_at IS NULL`+tw, append([]any{invoiceID}, twArgs...)...).
		Scan(&tid, &f.Period, &f.ParentEmail)
	if err != nil || f.Period == "" {
		return f, http.StatusNotFound, "family bill not found"
	}
	if c.Role == "parent" && f.ParentEmail != c.Email {
		return f, http.StatusForbidden, "not your bill"
	}
	members, err := store.FamilyBillMembers(db, tid, f.ParentEmail, f.Period, false)
	if err != nil || !hasMember(members, invoiceID) {
		return f, http.StatusNotFound, "family bill not found"
	}
	for _, m := range members {
		d, err := loadInvoicePDFData(db, c, m.InvoiceID)
		if err != nil {
			return f, http.StatusNotFound, "family bill not found"
		}
		f.Children = append(f.Children, d)
		f.ParentName = firstNonBlank(f.ParentName, d.ParentName)
	}
	return f, http.StatusOK, ""
}

func hasMember(members []store.FamilyBillMember, invoiceID string) bool {
	for _, m := range members {
		if m.InvoiceID == invoiceID {
			return true
		}
	}
	return false
}

func firstNonBlank(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func renderFamilyBillPDF(f familyBillPDFData, s *store.TenantSettings, paid bool) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(pageLeft, 15, 15)
	pdf.SetAutoPageBreak(true, 15)
	pdf.AddPage()

	// Same rule as renderInvoicePDF: translate every printed string once, never LogoPath.
	tr := pdf.UnicodeTranslatorFromDescriptor("")
	f = translateFamilyBillData(f, tr)
	s = translateSettings(s, tr)

	renderLetterhead(pdf, s, f.LogoPath)
	renderFamilyTitle(pdf, paid)
	renderFamilyInfo(pdf, f)
	for _, child := range f.Children {
		renderFamilyChild(pdf, child)
	}
	renderFamilyTotal(pdf, f)
	renderFamilyNote(pdf, f, s, paid)
	renderTermsNumbered(pdf, s)
	renderFooter(pdf, s)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func translateFamilyBillData(f familyBillPDFData, tr func(string) string) familyBillPDFData {
	f.ParentName = tr(f.ParentName)
	f.ParentEmail = tr(f.ParentEmail)
	children := make([]invoicePDFData, len(f.Children))
	for i, c := range f.Children {
		c = translateInvoiceData(c, tr)
		c.InvoiceID = tr(c.InvoiceID)
		c.InvoiceNo = tr(c.InvoiceNo)
		children[i] = c
	}
	f.Children = children
	return f
}

func renderFamilyTitle(pdf *gofpdf.Fpdf, paid bool) {
	title := "FAMILY BILL"
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

func renderFamilyInfo(pdf *gofpdf.Fpdf, f familyBillPDFData) {
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetTextColor(15, 15, 15)
	pdf.MultiCell(0, 6, joinNonEmpty([]string{f.ParentName, f.ParentEmail}, " - "), "", "L", false)
	pdf.Ln(2)
	infoRow(pdf, "Billing month", fmtMonth(f.Period))
	infoRow(pdf, "Due date", fmtDMY(earliestDue(f.Children)))
	pdf.Ln(4)
}

func earliestDue(children []invoicePDFData) string {
	dues := []string{}
	for _, c := range children {
		if c.DueDate != "" {
			dues = append(dues, c.DueDate)
		}
	}
	if len(dues) == 0 {
		return ""
	}
	sort.Strings(dues)
	return dues[0]
}

// fmtMonth turns "2026-10" into "Oct 2026", falling back to the raw period.
func fmtMonth(period string) string {
	t, err := time.Parse("2006-01", period)
	if err != nil {
		return period
	}
	return t.Format("Jan 2006")
}

// renderFamilyChild prints one child's invoice: its items, its discounts, its own total.
func renderFamilyChild(pdf *gofpdf.Fpdf, d invoicePDFData) {
	items := d.LineItems
	if len(items) == 0 {
		items = synthesizeLineItems(d)
	}
	sectionHeading(pdf, joinNonEmpty([]string{d.StudentName, "Invoice " + d.Number()}, " - "))
	renderItemsTable(pdf, items)
	for _, it := range items {
		if it.Kind == models.LineItemKindDiscount && it.Amount < 0 {
			totalRow(pdf, it.Name, "- "+rmFmt(-it.Amount), false)
		}
	}
	totalRow(pdf, "Total for "+firstWord(d.StudentName), rmFmt(d.Amount), false)
	pdf.Ln(4)
}

func firstWord(s string) string {
	return strings.SplitN(s, " ", 2)[0]
}

func renderFamilyTotal(pdf *gofpdf.Fpdf, f familyBillPDFData) {
	pdf.SetDrawColor(210, 210, 205)
	pdf.Line(pageLeft, pdf.GetY(), pageRight, pdf.GetY())
	pdf.Ln(2)
	totalRow(pdf, "Family total due", rmFmt(f.total()), true)
	pdf.Ln(3)
	pdf.SetFont("Helvetica", "I", 9)
	pdf.SetTextColor(90, 90, 90)
	pdf.MultiCell(0, 5, amountInWords(f.total()), "", "L", false)
	pdf.Ln(3)
}

// A receipt lists each child's receipt number; an open bill shows the bank details once.
func renderFamilyNote(pdf *gofpdf.Fpdf, f familyBillPDFData, s *store.TenantSettings, paid bool) {
	if !paid {
		renderNoteBlock(pdf, invoicePDFData{}, s, false)
		return
	}
	sectionHeading(pdf, "Payment Received")
	for _, c := range f.Children {
		labelValue(pdf, "Receipt No ("+firstWord(c.StudentName)+")", c.ReceiptNo)
	}
	first := f.Children[0]
	labelValue(pdf, "Paid On", fmtDMY(first.PaidOn))
	labelValue(pdf, "Method", first.PaymentMethod)
	if first.ReferenceNo != "" {
		labelValue(pdf, "Reference", first.ReferenceNo)
	}
	pdf.Ln(3)
}
