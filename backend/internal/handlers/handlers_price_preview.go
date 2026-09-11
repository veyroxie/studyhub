package handlers

import (
	"database/sql"
	"net/http"
	"studyhub/internal/core"
	"studyhub/internal/store"
)

// The switchover differ, built as a screen rather than a script.
//
// Step 3 of notes/pricing-bands.md requires an old-vs-new comparison for every
// student before any invoice changes. Written as a one-off script it would be
// run once and thrown away; written as an endpoint it also becomes the
// per-student check Nadine asked for, and the evidence for deciding who is
// safe to move back to automatic invoicing.
//
// It compares COMPUTED prices, not whether an invoice exists. Sixty of seventy
// students currently have monthly invoicing switched off, so an
// existence-based differ would report "no invoice either way" for most of the
// roster and look clean while checking nothing.
//
// Read-only. Nothing here writes, so it can be run against production
// repeatedly and before anything switches over.

type PriceComparison struct {
	StudentID   string            `json:"studentId"`
	StudentName string            `json:"studentName"`
	Invoiced    float64           `json:"invoiced"`
	HasInvoice  bool              `json:"hasInvoice"`
	Computed    float64           `json:"computed"`
	Difference  float64           `json:"difference"`
	Unpriceable bool              `json:"unpriceable"`
	InvoiceIDs  []string          `json:"invoiceIds"`
	Lines       []store.PriceLine `json:"lines"`
}

type PricePreview struct {
	Month       string            `json:"month"`
	Students    []PriceComparison `json:"students"`
	Matching    int               `json:"matching"`
	Differing   int               `json:"differing"`
	Unpriceable int               `json:"unpriceable"`
	NotInvoiced int               `json:"notInvoiced"`
}

func HandlePricePreview(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		month := r.URL.Query().Get("month")
		if !monthPattern.MatchString(month) {
			core.RespondError(w, "month must be YYYY-MM", http.StatusBadRequest)
			return
		}

		// What each student was actually invoiced for that month. Keyed on
		// period, not created_on: an invoice raised in September for August is
		// an August invoice (cron.go:649-655).
		invoiced := map[string]float64{}
		invIDs := map[string][]string{}
		tw, twArgs := store.ScopeTenant(c, "")
		args := append([]any{month}, twArgs...)
		// Ids as well as totals: a row on this screen has to be able to open
		// the actual invoice, otherwise a difference is a number with nothing
		// behind it.
		rows, err := db.Query(`SELECT student_id, id, COALESCE(amount,0) FROM invoices
			WHERE deleted_at IS NULL AND type='Monthly' AND period=?`+tw+` ORDER BY created_on`, args...)
		if err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		func(rs *sql.Rows) {
			defer rs.Close()
			for rs.Next() {
				var sid, invID string
				var amt float64
				if rs.Scan(&sid, &invID, &amt) == nil {
					invoiced[sid] += amt
					invIDs[sid] = append(invIDs[sid], invID)
				}
			}
		}(rows)

		out := PricePreview{Month: month, Students: []PriceComparison{}}
		// Price the month being compared, not today. Two students who left
		// since August priced at 0 against a real August invoice, which read
		// as a mispricing rather than as "they were still here then".
		asOf := month + "-15"
		for _, sp := range store.CatalogPrices(db, c, asOf) {
			amt, has := invoiced[sp.StudentID]
			// A student with no enrolments and no package is not part of this
			// question; listing them would pad the report with rows nobody has
			// a decision to make about.
			if !has && len(sp.Lines) == 0 {
				continue
			}
			cmp := PriceComparison{
				StudentID: sp.StudentID, StudentName: sp.StudentName,
				Invoiced: amt, HasInvoice: has, Computed: sp.Total,
				Difference: round2cmp(sp.Total - amt), Unpriceable: sp.Unpriceable,
				InvoiceIDs: invIDs[sp.StudentID], Lines: sp.Lines,
			}
			out.Students = append(out.Students, cmp)
			switch {
			case sp.Unpriceable:
				out.Unpriceable++
			case !has:
				out.NotInvoiced++
			case cmp.Difference == 0:
				out.Matching++
			default:
				out.Differing++
			}
		}
		core.Respond(w, out)
	}
}

// Comparisons are made at the sen, matching how every stored amount is
// rounded (models.round2). Without this, float noise reports a difference of
// 0.0000001 as a mismatch on an invoice that is actually correct.
func round2cmp(v float64) float64 {
	r := float64(int64(v*100+copySign(0.5, v))) / 100
	if r == 0 {
		return 0
	}
	return r
}

func copySign(mag, sign float64) float64 {
	if sign < 0 {
		return -mag
	}
	return mag
}
