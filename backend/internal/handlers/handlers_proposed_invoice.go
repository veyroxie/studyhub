package handlers

import (
	"net/http"
	"regexp"
	"strconv"

	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

// monthPattern is stricter than a length check on purpose: the month reaches
// CatalogPrices as an asOf date and then a Postgres ?::date cast (0066), so a
// malformed one stops being a 400 and becomes a failed query the caller sees as
// an empty result.
var monthPattern = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// ProposedInvoice is what the catalogue says a student should be billed for a
// month, in the shape the invoice editor already renders.
//
// It exists because the invoice builder had its OWN price list
// (billing.js _packageCatalog), built from the superseded pricing_tiers table
// and bucketed into levels 1-6. It could not express Level 0, Mandarin,
// Phonics or a twice-weekly tier, so correct figures had to be typed by hand --
// which is how every September invoice ended up hand-made with the discount
// baked into the amount.
type ProposedInvoice struct {
	StudentID   string                   `json:"studentId"`
	StudentName string                   `json:"studentName"`
	Month       string                   `json:"month"`
	Lines       []models.InvoiceLineItem `json:"lines"`
	Total       float64                  `json:"total"`
	Unpriceable bool                     `json:"unpriceable"`
	// Problems are the reasons a student cannot be priced. They are NOT lines:
	// an unpriceable class must never reach an invoice as a silent zero.
	Problems []string `json:"problems"`
}

// HandleProposedInvoice serves GET /api/billing/proposed-invoice.
//
// GET /api/billing/proposed-invoice?studentId=STU_x&month=2026-09
func HandleProposedInvoice(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		studentID := r.URL.Query().Get("studentId")
		if studentID == "" {
			core.RespondError(w, "studentId is required", http.StatusBadRequest)
			return
		}
		month := r.URL.Query().Get("month")
		if !monthPattern.MatchString(month) {
			core.RespondError(w, "month must be YYYY-MM", http.StatusBadRequest)
			return
		}
		// Mid-month, like the differ: the enrolments and the price VERSION in
		// force that month, not today's. Re-proposing an earlier month must
		// give the figure that month was billed at (0066).
		for _, sp := range store.CatalogPrices(db, c, month+"-15") {
			if sp.StudentID != studentID {
				continue
			}
			core.Respond(w, toProposedInvoice(sp, month))
			return
		}
		core.RespondError(w, "student not found", http.StatusNotFound)
	}
}

func toProposedInvoice(sp store.StudentPrice, month string) ProposedInvoice {
	out := ProposedInvoice{StudentID: sp.StudentID, StudentName: sp.StudentName, Month: month,
		Lines: []models.InvoiceLineItem{}, Unpriceable: sp.Unpriceable, Problems: []string{}}
	for _, l := range sp.Lines {
		if l.Source == store.SourceUnpriceable {
			out.Problems = append(out.Problems, problemText(l))
			continue
		}
		if l.Source == store.SourceDiscount {
			out.Lines = append(out.Lines, models.InvoiceLineItem{
				Kind: models.LineItemKindDiscount, Name: l.ClassName,
				Qty: 1, UnitPrice: -l.Amount, Amount: l.Amount,
			})
			continue
		}
		out.Lines = append(out.Lines, models.InvoiceLineItem{
			Kind: models.LineItemKindItem, Name: lineName(l), Descriptor: lineDescriptor(l),
			Qty: 1, UnitPrice: l.Amount, Amount: l.Amount,
		})
	}
	out.Total = sp.Total
	return out
}

func lineName(l store.PriceLine) string {
	if l.CategoryName != "" {
		return l.CategoryName
	}
	return l.ClassName
}

// The descriptor is what makes a figure checkable by eye: which tier, how many
// sessions a week, which classes it covers.
func lineDescriptor(l store.PriceLine) string {
	if l.TierName == "" {
		return l.ClassName
	}
	d := l.TierName
	if l.SessionsPerWeek > 0 {
		d += ", " + strconv.Itoa(l.SessionsPerWeek) + "x a week"
	}
	if l.ClassName != "" {
		d += " (" + l.ClassName + ")"
	}
	return d
}

func problemText(l store.PriceLine) string {
	who := l.ClassName
	if who == "" {
		who = l.CategoryName
	}
	if who == "" {
		return l.Problem
	}
	return who + ": " + l.Problem
}
