package jobs

import (
	"math"
	"net/http"
	"time"

	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

// Session-billing DRY RUN (8.7 / F5). Computes what session-based invoices
// WOULD charge for a month and lays the result beside today's monthly fees,
// so the centre can compare a real month line by line before the cron
// switches over. Reads everything, writes nothing.
//
// F5 rules encoded here and carried into the eventual switchover:
//   - a class line with zero billable sessions is SKIPPED, not billed at 0;
//   - a student with no package and no billable lines is SKIPPED entirely,
//     and issuing no invoice means no referral credit is consumed;
//   - a pricing hole (no rate, no band, bad times) FLAGS the line for a
//     human instead of silently pricing at 0 or dropping the student.

type PreviewLine struct {
	ClassID    string  `json:"classId"`
	ClassName  string  `json:"className"`
	Held       int     `json:"held"`
	Cancelled  int     `json:"cancelled"`
	MovedOut   int     `json:"movedOut"`
	Holiday    int     `json:"holiday"`
	Billable   int     `json:"billable"`
	Rate       float64 `json:"rate"`
	Amount     float64 `json:"amount"`
	MonthlyFee float64 `json:"monthlyFee"`
	Skipped    bool    `json:"skipped"`
	Flagged    bool    `json:"flagged"`
	Reason     string  `json:"reason,omitempty"`
}

type PreviewStudent struct {
	StudentID     string        `json:"studentId"`
	Name          string        `json:"name"`
	LevelBand     string        `json:"levelBand,omitempty"`
	PackageAmount float64       `json:"packageAmount,omitempty"`
	Lines         []PreviewLine `json:"lines"`
	SessionTotal  float64       `json:"sessionTotal"`
	MonthlyTotal  float64       `json:"monthlyTotal"`
	Delta         float64       `json:"delta"`
	Skipped       bool          `json:"skipped"`
	Flagged       bool          `json:"flagged"`
	Reason        string        `json:"reason,omitempty"`
}

type SessionPreview struct {
	Month           string           `json:"month"`
	Students        []PreviewStudent `json:"students"`
	SessionTotal    float64          `json:"sessionTotal"`
	MonthlyTotal    float64          `json:"monthlyTotal"`
	SkippedStudents int              `json:"skippedStudents"`
	FlaggedStudents int              `json:"flaggedStudents"`
}

func SessionBillingPreview(db *store.DB, c *core.Claims, month time.Time) SessionPreview {
	monthStart := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.Local)
	from := monthStart.Format("2006-01-02")
	to := monthStart.AddDate(0, 1, -1).Format("2006-01-02")
	out := SessionPreview{Month: monthStart.Format("2006-01"), Students: []PreviewStudent{}}

	monthlyFees := monthlyFeeByClass(db)
	tw, twArgs := store.ScopeTenant(c, "s")
	rows, err := db.Query(`
		SELECT s.id, s.first_name || ' ' || s.last_name, COALESCE(s.level_band,''),
		       s.package_amount, COALESCE(s.enrolled_classes,'[]')
		FROM students s
		WHERE s.deleted_at IS NULL
		  AND COALESCE(s.subscription_status,'active') = 'active'
		  AND COALESCE(s.status,'Active') NOT IN ('Inactive','Waitlisted')`+tw+`
		ORDER BY s.first_name, s.last_name`, twArgs...)
	if err != nil {
		core.Logger.Error("session preview student query failed", "err", err)
		return out
	}
	defer rows.Close()

	for rows.Next() {
		var ps PreviewStudent
		var enrolled string
		if rows.Scan(&ps.StudentID, &ps.Name, &ps.LevelBand, &ps.PackageAmount, &enrolled) != nil {
			continue
		}
		previewStudent(db, &ps, models.ParseArr(enrolled), monthlyFees, from, to)
		out.SessionTotal += ps.SessionTotal
		out.MonthlyTotal += ps.MonthlyTotal
		if ps.Skipped {
			out.SkippedStudents++
		}
		if ps.Flagged {
			out.FlaggedStudents++
		}
		out.Students = append(out.Students, ps)
	}
	return out
}

func previewStudent(db *store.DB, ps *PreviewStudent, enrolled []string, fees map[string]classMeta, from, to string) {
	// package_amount stays a whole-student override under session billing —
	// the confirmed decision, so both columns show the same number.
	if ps.PackageAmount > 0 {
		ps.SessionTotal, ps.MonthlyTotal = ps.PackageAmount, ps.PackageAmount
		ps.Reason = "package amount overrides per-class pricing"
		return
	}
	ps.Lines = []PreviewLine{}
	for _, cid := range enrolled {
		line := previewLine(db, cid, ps.LevelBand, fees, from, to)
		ps.SessionTotal += line.Amount
		if !line.Skipped && !line.Flagged {
			ps.MonthlyTotal += line.MonthlyFee
		}
		if line.Flagged {
			ps.Flagged = true
		}
		ps.Lines = append(ps.Lines, line)
	}
	ps.Delta = round2(ps.SessionTotal - ps.MonthlyTotal)
	if ps.SessionTotal == 0 && !ps.Flagged {
		ps.Skipped = true
		ps.Reason = "no billable sessions this month — no invoice, no referral credit consumed"
	}
}

func previewLine(db *store.DB, classID, studentBand string, fees map[string]classMeta, from, to string) PreviewLine {
	m := fees[classID]
	line := PreviewLine{ClassID: classID, ClassName: m.name, MonthlyFee: m.fee}
	sessions, err := store.SessionsInPeriod(db, classID, from, to)
	if err != nil {
		line.Flagged, line.Reason = true, err.Error()
		return line
	}
	for _, sess := range sessions {
		switch sess.Status {
		case store.SessionHeld:
			line.Held++
		case store.SessionCancelled:
			line.Cancelled++
		case store.SessionMovedOut:
			line.MovedOut++
		case store.SessionHoliday:
			line.Holiday++
		}
		if sess.Billable() {
			line.Billable++
		}
	}
	if line.Billable == 0 {
		line.Skipped, line.Reason = true, "no billable sessions"
		return line
	}
	rate, err := store.SessionRateFor(db, classID, studentBand)
	if err != nil {
		line.Flagged, line.Reason = true, err.Error()
		return line
	}
	line.Rate = rate
	line.Amount = round2(rate * float64(line.Billable))
	return line
}

// monthlyFeeByClass mirrors the live cron's fee derivation exactly
// (COALESCE(override, tier fee, 0)) so the comparison column is what the
// current system would actually bill.
func monthlyFeeByClass(db *store.DB) map[string]classMeta {
	out := map[string]classMeta{}
	rows, err := db.Query(`SELECT c.id, COALESCE(NULLIF(c.monthly_fee_override,0), pt.monthly_fee, 0), COALESCE(c.name,''), COALESCE(c.class_type,''), COALESCE(c.level_band,''), COALESCE(c.subject,'')
		FROM classes c
		LEFT JOIN pricing_tiers pt ON pt.class_type = c.class_type AND pt.level_band = c.level_band AND pt.tenant_id = c.tenant_id AND pt.deleted_at IS NULL
		WHERE c.deleted_at IS NULL`)
	if err != nil {
		core.Logger.Error("session preview fee query failed", "err", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var cid string
		var m classMeta
		if rows.Scan(&cid, &m.fee, &m.name, &m.classType, &m.band, &m.subject) == nil {
			out[cid] = m
		}
	}
	return out
}

func round2(x float64) float64 {
	return math.Round(x*100) / 100
}

// HandleSessionBillingPreview serves the dry run: GET ?month=YYYY-MM
// (default: the current month). Admin only; computes, never writes.
func HandleSessionBillingPreview(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		month := time.Now()
		if q := r.URL.Query().Get("month"); q != "" {
			parsed, err := time.ParseInLocation("2006-01", q, time.Local)
			if err != nil {
				core.RespondError(w, "month must be YYYY-MM", 400)
				return
			}
			month = parsed
		}
		core.Respond(w, SessionBillingPreview(db, c, month))
	}
}
