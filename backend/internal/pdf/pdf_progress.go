package pdf

import (
	"bytes"
	"studyhub/internal/models"
	"studyhub/internal/store"

	"github.com/jung-kurt/gofpdf"
)

// renderProgressReportPDF lays out a single termly progress report. Sections
// follow the structure parents expect from a printed report card: header,
// student block, grade, strengths, areas to improve, teacher comment, focus
// for next term, signature line. Branding is taken from the caller's tenant
// settings (not a hardcoded tenant-1 lookup). Every printed string is run
// through the gofpdf Unicode translator so non-Latin-1 characters (e.g. the
// "—" placeholders, accented names) render instead of turning into mojibake.
func RenderProgressReportPDF(pr models.ProgressReport, studentName, teacherName string, s *store.TenantSettings) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 18, 15)
	pdf.AddPage()
	tr := pdf.UnicodeTranslatorFromDescriptor("")

	pdf.SetFont("Helvetica", "B", 22)
	pdf.SetTextColor(15, 15, 15)
	pdf.Cell(0, 10, tr(s.BrandName))
	pdf.Ln(8)
	pdf.SetFont("Helvetica", "", 11)
	pdf.SetTextColor(120, 120, 120)
	pdf.Cell(0, 6, "Termly Progress Report")
	pdf.Ln(12)

	pdf.SetDrawColor(220, 220, 215)
	pdf.SetFillColor(250, 250, 248)
	pdf.Rect(15, pdf.GetY(), 180, 26, "FD")
	yTop := pdf.GetY() + 5
	pdf.SetXY(20, yTop)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(120, 120, 120)
	pdf.Cell(40, 5, "Student")
	pdf.SetX(75)
	pdf.Cell(40, 5, "Term")
	pdf.SetX(135)
	pdf.Cell(40, 5, "Teacher")

	pdf.SetXY(20, yTop+5)
	pdf.SetFont("Helvetica", "B", 12)
	pdf.SetTextColor(20, 20, 20)
	pdf.Cell(50, 7, tr(studentName))
	pdf.SetX(75)
	pdf.Cell(55, 7, tr(pr.Term))
	pdf.SetX(135)
	if teacherName == "" {
		teacherName = "—"
	}
	pdf.Cell(60, 7, tr(teacherName))

	pdf.SetY(yTop + 22)
	pdf.Ln(6)

	if pr.Subject != "" || pr.Grade != "" {
		pdf.SetFillColor(255, 251, 235)
		pdf.SetDrawColor(254, 243, 199)
		pdf.Rect(15, pdf.GetY(), 180, 14, "FD")
		gy := pdf.GetY() + 4
		pdf.SetXY(20, gy)
		pdf.SetFont("Helvetica", "", 9)
		pdf.SetTextColor(146, 64, 14)
		pdf.Cell(40, 4, "Subject")
		pdf.SetX(115)
		pdf.Cell(40, 4, "Grade")
		pdf.SetXY(20, gy+4)
		pdf.SetFont("Helvetica", "B", 12)
		pdf.SetTextColor(120, 53, 15)
		pdf.Cell(95, 6, tr(pr.Subject))
		pdf.SetX(115)
		pdf.Cell(60, 6, tr(pr.Grade))
		pdf.SetY(gy + 12)
		pdf.Ln(6)
	}

	section(pdf, tr, "Strengths", pr.Strengths)
	section(pdf, tr, "Areas to improve", pr.AreasToImprove)
	section(pdf, tr, "Teacher's comment", pr.TeacherComment)
	section(pdf, tr, "Focus for next term", pr.NextTermFocus)

	pdf.Ln(8)
	pdf.SetDrawColor(120, 120, 120)
	pdf.Line(15, pdf.GetY(), 90, pdf.GetY())
	pdf.SetFont("Helvetica", "", 8)
	pdf.SetTextColor(120, 120, 120)
	pdf.SetXY(15, pdf.GetY()+2)
	pdf.Cell(75, 5, "Teacher signature")

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func section(pdf *gofpdf.Fpdf, tr func(string) string, title, body string) {
	pdf.SetFont("Helvetica", "B", 11)
	pdf.SetTextColor(15, 15, 15)
	pdf.Cell(0, 6, title)
	pdf.Ln(6)
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(60, 60, 60)
	if body == "" {
		body = "—"
	}
	pdf.MultiCell(0, 5, tr(body), "", "L", false)
	pdf.Ln(3)
}
