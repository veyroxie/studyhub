package rating

import (
	"sort"
	"strconv"
	"strings"
)

// Source says WHY a line costs what it does, so a difference between two
// engines, or between an engine and an issued invoice, can be explained rather
// than only displayed.
const (
	SourcePackage       = "package"
	SourceOverride      = "class rate"
	SourceCreditCovered = "credit-covered"
	SourcePlan          = "tier"
	SourceDiscount      = "standing discount"
	SourceUnpriceable   = "unpriceable"
)

// PlanKey is what a price is agreed against: a tier within a category, at a
// given weekly frequency. Slots are COUNTED from live enrolments, never stored.
type PlanKey struct {
	CategoryID      string
	Tier            string
	SessionsPerWeek int
}

type Class struct {
	ID, Name             string
	CategoryID, Category string
	DefaultTier          string
	CreditCovered        bool
	// Override is a price for THIS class rather than a frequency tier. Zero
	// means unset, not free.
	Override Money
}

type Enrolment struct {
	ClassID string
	// Tier on the enrolment wins over the class default; empty falls back.
	Tier string
}

type Student struct {
	ID, Name string
	// Package is the whole price when set: nothing else is read.
	Package          Money
	StandingDiscount Money
	DiscountReason   string
	Enrolments       []Enrolment
}

// Catalogue is the price list as it stood on the date being rated. Callers are
// responsible for loading the version in force then -- pricing_plans carries
// effective-dated versions since 0066.
type Catalogue struct {
	Classes map[string]Class
	Plans   map[PlanKey]Money
}

type Line struct {
	ClassID         string
	ClassName       string
	CategoryName    string
	TierName        string
	SessionsPerWeek int
	Amount          Money
	Source          string
	Problem         string
}

type Result struct {
	Lines       []Line
	Total       Money
	Unpriceable bool
}

// Price resolves what one student costs for a month. Resolution order, from
// section 2 of notes/pricing-bands.md:
//
//	student.Package > 0    -> that IS the price, nothing else is read
//	  per live enrolment:
//	    class.Override > 0        -> use it (0 means unset, not free)
//	      -> category credit-covered -> 0, deliberately
//	        -> plan for (category, tier, slots in that category) -> its fee
//	          -> UNPRICEABLE, named and surfaced, never silently 0
func Price(s Student, cat Catalogue) Result {
	if s.Package > 0 {
		r := Result{Lines: []Line{{Amount: s.Package, Source: SourcePackage, ClassName: "Package"}}}
		r.Total = s.Package + appendStandingDiscount(&r, s)
		return r
	}
	r := Result{Lines: []Line{}}
	groups, order := groupByCategory(&r, s, cat)
	for _, catID := range order {
		r.Lines = append(r.Lines, priceGroup(catID, groups[catID], cat, &r))
	}
	var total Money
	for _, l := range r.Lines {
		total += l.Amount
	}
	// A student who cannot be priced gets no discount line either. Subtracting
	// from a total we do not have would produce a negative bill and imply the
	// pricing was resolved when it was not.
	if !r.Unpriceable && total > 0 {
		total += appendStandingDiscount(&r, s)
	}
	r.Total = total
	return r
}

type categoryGroup struct {
	categoryName string
	creditCover  bool
	classNames   []string
	tiers        map[string]bool
}

// groupByCategory collects the enrolments a single frequency tier covers. A
// category is priced ONCE for the whole group: pricing per class would double a
// twice-weekly student's bill, which is the most expensive mistake available
// here. Per-class rates and uncategorised classes are emitted as lines directly
// and never join a group.
func groupByCategory(r *Result, s Student, cat Catalogue) (map[string]*categoryGroup, []string) {
	groups := map[string]*categoryGroup{}
	order := []string{}
	for _, e := range s.Enrolments {
		m, ok := cat.Classes[e.ClassID]
		if !ok {
			continue
		}
		if m.Override > 0 {
			r.Lines = append(r.Lines, Line{ClassID: e.ClassID, ClassName: m.Name,
				CategoryName: m.Category, Amount: m.Override, Source: SourceOverride})
			continue
		}
		if m.CategoryID == "" {
			r.Lines = append(r.Lines, Line{ClassID: e.ClassID, ClassName: m.Name,
				Source: SourceUnpriceable, Problem: "no pricing category"})
			r.Unpriceable = true
			continue
		}
		g, seen := groups[m.CategoryID]
		if !seen {
			g = &categoryGroup{categoryName: m.Category, creditCover: m.CreditCovered, tiers: map[string]bool{}}
			groups[m.CategoryID] = g
			order = append(order, m.CategoryID)
		}
		g.classNames = append(g.classNames, m.Name)
		tier := e.Tier
		if tier == "" {
			tier = m.DefaultTier
		}
		g.tiers[tier] = true
	}
	return groups, order
}

func priceGroup(catID string, g *categoryGroup, cat Catalogue, r *Result) Line {
	line := Line{CategoryName: g.categoryName, ClassName: strings.Join(g.classNames, ", "),
		SessionsPerWeek: len(g.classNames)}

	if g.creditCover {
		line.Source = SourceCreditCovered
		return line
	}
	if g.tiers[""] {
		line.Source, line.Problem = SourceUnpriceable, "no tier on the enrolment or the class"
		r.Unpriceable = true
		return line
	}
	// Section 11 of notes/pricing-bands.md: a student taking two levels in one
	// category has no defined price. Summing both frequency tiers would invent
	// one, and be confidently wrong, which is worse than refusing.
	if len(g.tiers) > 1 {
		names := make([]string, 0, len(g.tiers))
		for t := range g.tiers {
			names = append(names, t)
		}
		sort.Strings(names)
		line.TierName = strings.Join(names, " + ")
		line.Source = SourceUnpriceable
		line.Problem = "two levels in one category, which has no agreed price"
		r.Unpriceable = true
		return line
	}
	for t := range g.tiers {
		line.TierName = t
	}
	fee, found := cat.Plans[PlanKey{CategoryID: catID, Tier: line.TierName, SessionsPerWeek: line.SessionsPerWeek}]
	if !found {
		line.Source = SourceUnpriceable
		line.Problem = "no price for " + line.TierName + " at " + strconv.Itoa(line.SessionsPerWeek) + "x a week"
		r.Unpriceable = true
		return line
	}
	line.Amount, line.Source = fee, SourcePlan
	return line
}

// appendStandingDiscount adds the standing discount as its own NEGATIVE line
// and returns what it takes off. A line rather than a smaller total is the
// whole point: five students were invoiced below the catalogue with no discount
// recorded anywhere, so nobody could say why (ADR-013).
func appendStandingDiscount(r *Result, s Student) Money {
	if s.StandingDiscount <= 0 {
		return 0
	}
	name := s.DiscountReason
	if name == "" {
		name = "Standing discount"
	}
	r.Lines = append(r.Lines, Line{ClassName: name, Amount: -s.StandingDiscount, Source: SourceDiscount})
	return -s.StandingDiscount
}
