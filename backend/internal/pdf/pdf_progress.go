package pdf

import (
	"bytes"
	"studyhub/internal/mailer"
	"studyhub/internal/models"

	"github.com/jung-kurt/gofpdf"
)

// renderProgressReportPDF lays out a single termly progress report. Sections
// follow the structure parents expect from a printed report card: header,
// student block, grade, strengths, areas to improve, teacher comment, focus
// for next term, signature line.
func RenderProgressReportPDF(pr models.ProgressReport, studentName, teacherName string) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 18, 15)
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 22)
	pdf.SetTextColor(15, 15, 15)
	pdf.Cell(0, 10, mailer.Brand().BrandName)
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
	pdf.Cell(50, 7, studentName)
	pdf.SetX(75)
	pdf.Cell(55, 7, pr.Term)
	pdf.SetX(135)
	if teacherName == "" {
		teacherName = "—"
	}
	pdf.Cell(60, 7, teacherName)

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
		pdf.Cell(95, 6, pr.Subject)
		pdf.SetX(115)
		pdf.Cell(60, 6, pr.Grade)
		pdf.SetY(gy + 12)
		pdf.Ln(6)
	}

	section(pdf, "Strengths", pr.Strengths)
	section(pdf, "Areas to improve", pr.AreasToImprove)
	section(pdf, "Teacher's comment", pr.TeacherComment)
	section(pdf, "Focus for next term", pr.NextTermFocus)

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

func section(pdf *gofpdf.Fpdf, title, body string) {
	pdf.SetFont("Helvetica", "B", 11)
	pdf.SetTextColor(15, 15, 15)
	pdf.Cell(0, 6, title)
	pdf.Ln(6)
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(60, 60, 60)
	if body == "" {
		body = "—"
	}
	pdf.MultiCell(0, 5, body, "", "L", false)
	pdf.Ln(3)
}
