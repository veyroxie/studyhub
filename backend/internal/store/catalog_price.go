package store

import (
	"math"
	"sort"
	"strings"
	"time"

	"studyhub/internal/core"
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
// explained rather than just displayed.
const (
	SourcePackage       = "package"
	SourceOverride      = "class rate"
	SourceCreditCovered = "credit-covered"
	SourcePlan          = "tier"
	SourceDiscount      = "standing discount"
	SourceUnpriceable   = "unpriceable"
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
	plans := map[string]float64{}
	prows, err := db.Query(`SELECT category_id, tier_name, COALESCE(sessions_per_week,1), COALESCE(monthly_fee,0)
		FROM pricing_plans WHERE deleted_at IS NULL`+tw, twArgs...)
	if err != nil {
		core.Logger.Error("catalog price: plan load failed", "err", err)
		return nil
	}
	for prows.Next() {
		var cat, tier string
		var slots int
		var fee float64
		if prows.Scan(&cat, &tier, &slots, &fee) == nil {
			plans[planKey(cat, tier, slots)] = fee
		}
	}
	prows.Close()

	type stu struct {
		name       string
		pkg        float64
		discount   float64
		discReason string
	}
	students := map[string]stu{}
	order := []string{}
	srows, err := db.Query(`SELECT id, first_name || ' ' || last_name, COALESCE(package_amount,0),
		COALESCE(standing_discount,0), COALESCE(standing_discount_reason,'')
		FROM students WHERE deleted_at IS NULL`+tw+` ORDER BY first_name, last_name`, twArgs...)
	if err != nil {
		core.Logger.Error("catalog price: student load failed", "err", err)
		return nil
	}
	for srows.Next() {
		var id string
		var s stu
		if srows.Scan(&id, &s.name, &s.pkg, &s.discount, &s.discReason) == nil {
			students[id] = s
			order = append(order, id)
		}
	}
	srows.Close()

	enrol := map[string][]string{} // studentID -> classIDs
	tier := map[string]string{}    // studentID|classID -> tier chosen at enrolment
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
		if erows.Scan(&sid, &cid, &tn) == nil {
			enrol[sid] = append(enrol[sid], cid)
			tier[sid+"|"+cid] = tn
		}
	}
	erows.Close()

	out := []StudentPrice{}
	for _, sid := range order {
		st := students[sid]
		out = append(out, priceOne(sid, st.name, st.pkg, st.discount, st.discReason, enrol[sid], tier, classes, plans))
	}
	return out
}

func planKey(cat, tierName string, slots int) string {
	return cat + "|" + tierName + "|" + itoaSmall(slots)
}

func itoaSmall(n int) string {
	if n < 0 || n > 9 {
		return "?"
	}
	return string(rune('0' + n))
}

func priceOne(studentID, name string, pkg, discount float64, discReason string, classIDs []string,
	tier map[string]string, classes map[string]catClass, plans map[string]float64) StudentPrice {

	sp := StudentPrice{StudentID: studentID, StudentName: name, Lines: []PriceLine{}}

	// A package is the whole price. AI_DOCS/billing.md: it short-circuits
	// per-class pricing entirely, and that rule does not change here.
	if pkg > 0 {
		sp.Lines = append(sp.Lines, PriceLine{Amount: round2(pkg), Source: SourcePackage, ClassName: "Package"})
		sp.Total = round2(pkg + applyDiscount(&sp, discount, discReason))
		return sp
	}

	// Group the live enrolments by category. A category is priced ONCE for the
	// whole group, because the frequency tier already covers every slot in it
	// -- pricing per class would double a twice-weekly student's bill, which is
	// the most expensive mistake available in this file.
	type group struct {
		categoryName string
		creditCover  bool
		classNames   []string
		tiers        map[string]bool
	}
	groups := map[string]*group{}
	catOrder := []string{}

	for _, cid := range classIDs {
		m, ok := classes[cid]
		if !ok {
			continue
		}
		// A per-class rate is a price for THAT class, not a frequency tier, so
		// it bills on its own and never joins a group.
		if m.override > 0 {
			sp.Lines = append(sp.Lines, PriceLine{ClassID: cid, ClassName: m.name,
				CategoryName: m.categoryName, Amount: round2(m.override), Source: SourceOverride})
			continue
		}
		if m.categoryID == "" {
			sp.Lines = append(sp.Lines, PriceLine{ClassID: cid, ClassName: m.name,
				Source: SourceUnpriceable, Problem: "no pricing category"})
			sp.Unpriceable = true
			continue
		}
		g, seen := groups[m.categoryID]
		if !seen {
			g = &group{categoryName: m.categoryName, creditCover: m.creditCovered, tiers: map[string]bool{}}
			groups[m.categoryID] = g
			catOrder = append(catOrder, m.categoryID)
		}
		g.classNames = append(g.classNames, m.name)
		tn := tier[studentID+"|"+cid]
		if tn == "" {
			tn = m.defaultTier
		}
		g.tiers[tn] = true
	}

	for _, catID := range catOrder {
		g := groups[catID]
		line := PriceLine{CategoryName: g.categoryName, ClassName: strings.Join(g.classNames, ", "),
			SessionsPerWeek: len(g.classNames)}

		if g.creditCover {
			line.Source = SourceCreditCovered
			sp.Lines = append(sp.Lines, line)
			continue
		}
		if g.tiers[""] {
			line.Source, line.Problem = SourceUnpriceable, "no tier on the enrolment or the class"
			sp.Lines, sp.Unpriceable = append(sp.Lines, line), true
			continue
		}
		// Section 11 of notes/pricing-bands.md: a student taking two levels in
		// one category has no defined price. Summing both frequency tiers
		// would invent one -- and be confidently wrong, which is worse than
		// refusing. Flagged for Nadine, exactly as an unpriced class is.
		if len(g.tiers) > 1 {
			names := []string{}
			for t := range g.tiers {
				names = append(names, t)
			}
			sort.Strings(names)
			line.TierName = strings.Join(names, " + ")
			line.Source = SourceUnpriceable
			line.Problem = "two levels in one category, which has no agreed price"
			sp.Lines, sp.Unpriceable = append(sp.Lines, line), true
			continue
		}
		for t := range g.tiers {
			line.TierName = t
		}

		fee, found := plans[planKey(catID, line.TierName, line.SessionsPerWeek)]
		if !found {
			line.Source = SourceUnpriceable
			line.Problem = "no price for " + line.TierName + " at " + itoaSmall(line.SessionsPerWeek) + "x a week"
			sp.Lines, sp.Unpriceable = append(sp.Lines, line), true
			continue
		}
		line.Amount, line.Source = round2(fee), SourcePlan
		sp.Lines = append(sp.Lines, line)
	}

	total := 0.0
	for _, l := range sp.Lines {
		total += l.Amount
	}
	// A student who cannot be priced gets no discount line either. Subtracting
	// from a total we do not have would produce a negative bill and imply the
	// pricing was resolved when it was not.
	if !sp.Unpriceable && total > 0 {
		total += applyDiscount(&sp, discount, discReason)
	}
	sp.Total = round2(total)
	return sp
}

// applyDiscount appends the standing discount as its own NEGATIVE line and
// returns what it takes off. A line rather than a smaller total is the whole
// point: five students were invoiced below the catalogue with no discount
// recorded anywhere, so nobody could say why (ADR-013).
func applyDiscount(sp *StudentPrice, amount float64, reason string) float64 {
	if amount <= 0 {
		return 0
	}
	name := reason
	if name == "" {
		name = "Standing discount"
	}
	sp.Lines = append(sp.Lines, PriceLine{ClassName: name, Amount: round2(-amount), Source: SourceDiscount})
	return -round2(amount)
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
