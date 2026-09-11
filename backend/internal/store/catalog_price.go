package store

import (
	"strings"
	"time"

	"studyhub/internal/core"
	"studyhub/internal/rating"
)

// CatalogPrice resolves what a student SHOULD be billed each month under the
// pricing catalogue (migrations 0051/0053/0054).
//
// This is the compute path the switchover differ uses, and the one the monthly
// cron must call when it stops reading pricing_tiers. Two implementations of
// "what does this student cost" is how the monthly and session paths drifted
// apart in the first place, so there is deliberately only one here.
//
// It computes and never writes. Running it changes nothing, which is what
// makes it safe to compare against live invoices before anything switches.
//
// Resolution order, from section 2 of notes/pricing-bands.md:
//
//	student.package_amount > 0   -> that IS the price, nothing else is read
//	  per live enrolment:
//	    class.monthly_fee_override > 0  -> use it (0 means unset, not free)
//	      -> category is credit-covered -> 0, deliberately
//	        -> plan for (category, tier, slots in that category) -> its fee
//	          -> UNPRICEABLE, named and surfaced, never silently 0
//
// Slots are counted, not stored: two live enrolments in one category means a
// twice-weekly student, and idx_enrollments_live (0044) already guarantees two
// rows are two real slots rather than a duplicate.

// PriceSource says WHY a line costs what it does, so a difference can be
// explained rather than just displayed. Defined once, in the engine: two copies
// of these strings is how a renamed source silently stops matching.
const (
	SourcePackage       = rating.SourcePackage
	SourceOverride      = rating.SourceOverride
	SourceCreditCovered = rating.SourceCreditCovered
	SourcePlan          = rating.SourcePlan
	SourceDiscount      = rating.SourceDiscount
	SourceUnpriceable   = rating.SourceUnpriceable
)

type PriceLine struct {
	ClassID         string  `json:"classId"`
	ClassName       string  `json:"className"`
	CategoryName    string  `json:"categoryName"`
	TierName        string  `json:"tierName"`
	SessionsPerWeek int     `json:"sessionsPerWeek"`
	Amount          float64 `json:"amount"`
	Source          string  `json:"source"`
	Problem         string  `json:"problem,omitempty"`
}

type StudentPrice struct {
	StudentID   string      `json:"studentId"`
	StudentName string      `json:"studentName"`
	Total       float64     `json:"total"`
	Lines       []PriceLine `json:"lines"`
	Unpriceable bool        `json:"unpriceable"`
}

type catClass struct {
	name, categoryID, categoryName, defaultTier string
	creditCovered                               bool
	override                                    float64
}

// CatalogPrices computes the catalogue price for every non-deleted student in
// the tenant, in a fixed number of queries rather than one per student.
//
// asOf is the date the enrolments are read AT, in YYYY-MM-DD. Pass "" for
// "live right now". It matters because the differ compares PAST months: using
// today's enrolments to price August charged nothing for two students who had
// left since, and would have shown a clean 0 against a real invoice. The
// window is half-open [started_on, ended_on), the same rule enrolledOn and
// EnrollmentWindowsIn already use, so the day a student leaves is not counted.
func CatalogPrices(db *DB, c *core.Claims, asOf string) []StudentPrice {
	tw, twArgs := ScopeTenant(c, "")

	classes := map[string]catClass{}
	rows, err := db.Query(`SELECT cl.id, cl.name, COALESCE(cl.pricing_category_id,''),
	       COALESCE(pc.name,''), COALESCE(pc.credit_covered,FALSE),
	       COALESCE(cl.default_tier_name,''), COALESCE(cl.monthly_fee_override,0)
	  FROM classes cl
	  LEFT JOIN pricing_categories pc ON pc.id = cl.pricing_category_id AND pc.deleted_at IS NULL
	 WHERE cl.deleted_at IS NULL`+strings.ReplaceAll(tw, "tenant_id", "cl.tenant_id"), twArgs...)
	if err != nil {
		core.Logger.Error("catalog price: class load failed", "err", err)
		return nil
	}
	for rows.Next() {
		var id string
		var m catClass
		if rows.Scan(&id, &m.name, &m.categoryID, &m.categoryName, &m.creditCovered, &m.defaultTier, &m.override) == nil {
			classes[id] = m
		}
	}
	rows.Close()

	// (category, tier, slots) -> monthly fee.
	plans := map[rating.PlanKey]rating.Money{}
	// The plan version in force on asOf, not the one in force today. 0066 gave
	// pricing_plans half-open [effective_from, effective_to) versions, so
	// re-rating an earlier month now reads the price that month was billed at
	// instead of silently applying a later rise. Empty asOf means today, which
	// is what every live caller wants.
	priceOn := asOf
	if priceOn == "" {
		priceOn = core.Today()
	}
	planArgs := append([]any{priceOn}, twArgs...)
	prows, err := db.Query(`SELECT category_id, tier_name, COALESCE(sessions_per_week,1), COALESCE(monthly_fee,0)
		FROM pricing_plans
		 WHERE deleted_at IS NULL
		   AND daterange(effective_from, effective_to, '[)') @> ?::date`+tw, planArgs...)
	if err != nil {
		core.Logger.Error("catalog price: plan load failed", "err", err)
		return nil
	}
	for prows.Next() {
		var cat, tier string
		var slots int
		var fee float64
		if prows.Scan(&cat, &tier, &slots, &fee) == nil {
			plans[rating.PlanKey{CategoryID: cat, Tier: tier, SessionsPerWeek: slots}] = rating.FromRM(fee)
		}
	}
	prows.Close()

	students := map[string]*rating.Student{}
	order := []string{}
	srows, err := db.Query(`SELECT id, first_name || ' ' || last_name, COALESCE(package_amount,0),
		COALESCE(standing_discount,0), COALESCE(standing_discount_reason,'')
		FROM students WHERE deleted_at IS NULL`+tw+` ORDER BY first_name, last_name`, twArgs...)
	if err != nil {
		core.Logger.Error("catalog price: student load failed", "err", err)
		return nil
	}
	for srows.Next() {
		var id, name, reason string
		var pkg, discount float64
		if srows.Scan(&id, &name, &pkg, &discount, &reason) == nil {
			students[id] = &rating.Student{ID: id, Name: name, Package: rating.FromRM(pkg),
				StandingDiscount: rating.FromRM(discount), DiscountReason: reason}
			order = append(order, id)
		}
	}
	srows.Close()

	// One window, always. The live branch used to be `ended_on IS NULL` with no
	// lower bound, so an enrolment starting next month was priced this month --
	// two answers to "what does this student cost" inside the resolver whose
	// whole point is that there is only one. Empty asOf means today.
	when := asOf
	if when == "" {
		when = time.Now().Format("2006-01-02")
	}
	enrolSQL := `SELECT student_id, class_id, COALESCE(tier_name,'')
		FROM enrollments WHERE started_on <= ? AND (ended_on IS NULL OR ended_on > ?)` + tw
	enrolArgs := append([]any{when, when}, twArgs...)
	erows, err := db.Query(enrolSQL, enrolArgs...)
	if err != nil {
		core.Logger.Error("catalog price: enrolment load failed", "err", err)
		return nil
	}
	for erows.Next() {
		var sid, cid, tn string
		if erows.Scan(&sid, &cid, &tn) != nil {
			continue
		}
		if st, ok := students[sid]; ok {
			st.Enrolments = append(st.Enrolments, rating.Enrolment{ClassID: cid, Tier: tn})
		}
	}
	erows.Close()

	cat := rating.Catalogue{Classes: ratingClasses(classes), Plans: plans}
	out := []StudentPrice{}
	for _, sid := range order {
		st := students[sid]
		out = append(out, toStudentPrice(*st, rating.Price(*st, cat)))
	}
	return out
}

// ratingClasses maps the loaded rows into the engine's shape. Kept here rather
// than in rating so the engine stays free of anything that knows about SQL.
func ratingClasses(in map[string]catClass) map[string]rating.Class {
	out := make(map[string]rating.Class, len(in))
	for id, c := range in {
		out[id] = rating.Class{ID: id, Name: c.name, CategoryID: c.categoryID, Category: c.categoryName,
			DefaultTier: c.defaultTier, CreditCovered: c.creditCovered, Override: rating.FromRM(c.override)}
	}
	return out
}

// toStudentPrice converts back to ringgit for the wire. The engine works in
// sen; the API, the frontend and the PDF are unchanged.
func toStudentPrice(s rating.Student, r rating.Result) StudentPrice {
	sp := StudentPrice{StudentID: s.ID, StudentName: s.Name, Total: r.Total.RM(),
		Unpriceable: r.Unpriceable, Lines: make([]PriceLine, 0, len(r.Lines))}
	for _, l := range r.Lines {
		sp.Lines = append(sp.Lines, PriceLine{ClassID: l.ClassID, ClassName: l.ClassName,
			CategoryName: l.CategoryName, TierName: l.TierName, SessionsPerWeek: l.SessionsPerWeek,
			Amount: l.Amount.RM(), Source: l.Source, Problem: l.Problem})
	}
	return sp
}
