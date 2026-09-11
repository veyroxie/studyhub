package rating

import "testing"

// No database, no fixtures, no HTTP. The whole reason for pulling this out of
// the cron and the store: pricing rules can be stated and checked directly.

func cat(plans map[PlanKey]Money, classes ...Class) Catalogue {
	byID := map[string]Class{}
	for _, c := range classes {
		byID[c.ID] = c
	}
	return Catalogue{Classes: byID, Plans: plans}
}

func enrolIn(ids ...string) []Enrolment {
	out := make([]Enrolment, 0, len(ids))
	for _, id := range ids {
		out = append(out, Enrolment{ClassID: id})
	}
	return out
}

func TestMoneyRoundsHalfAwayFromZero(t *testing.T) {
	cases := []struct {
		name string
		in   Money
		pct  float64
		want Money
	}{
		{"half a sen rounds up", 250, 5, 13}, // 12.5 sen -> 13
		{"a discount of the same size matches", -250, 5, -13},
		{"below half rounds down", 249, 5, 12}, // 12.45 -> 12
		{"exact needs no rounding", 24000, 10, 2400},
	}
	for _, tc := range cases {
		if got := tc.in.Percent(tc.pct); got != tc.want {
			t.Errorf("%s: %d.Percent(%v) = %d, want %d", tc.name, tc.in, tc.pct, got, tc.want)
		}
	}
	if got := FromRM(240.00); got != 24000 {
		t.Errorf("FromRM(240.00) = %d sen, want 24000", got)
	}
	if got := Money(23000).RM(); got != 230.00 {
		t.Errorf("2300 sen = %v, want 230.00", got)
	}
}

// The most expensive mistake available here: a student attending one category
// twice a week is on a twice-weekly tier, not billed twice.
func TestTwiceWeeklyInOneCategoryIsPricedOnce(t *testing.T) {
	c := cat(map[PlanKey]Money{
		{CategoryID: "grp", Tier: "L3", SessionsPerWeek: 1}: 26000,
		{CategoryID: "grp", Tier: "L3", SessionsPerWeek: 2}: 49000,
	},
		Class{ID: "a", Name: "Tue", CategoryID: "grp", Category: "Group", DefaultTier: "L3"},
		Class{ID: "b", Name: "Thu", CategoryID: "grp", Category: "Group", DefaultTier: "L3"},
	)
	r := Price(Student{ID: "s1", Enrolments: enrolIn("a", "b")}, c)
	if r.Total != 49000 {
		t.Errorf("total %d sen, want 49000 — two slots must take the 2x tier, not two 1x charges", r.Total)
	}
	if len(r.Lines) != 1 {
		t.Errorf("got %d lines, want 1 for one category", len(r.Lines))
	}
	if r.Lines[0].SessionsPerWeek != 2 {
		t.Errorf("sessions per week %d, want 2", r.Lines[0].SessionsPerWeek)
	}
}

func TestPackageIsTheWholePrice(t *testing.T) {
	c := cat(map[PlanKey]Money{{CategoryID: "grp", Tier: "L3", SessionsPerWeek: 1}: 26000},
		Class{ID: "a", CategoryID: "grp", DefaultTier: "L3"})
	r := Price(Student{ID: "s1", Package: 80000, Enrolments: enrolIn("a")}, c)
	if r.Total != 80000 {
		t.Errorf("total %d sen, want the package's 80000 — enrolments must not be read", r.Total)
	}
	if len(r.Lines) != 1 || r.Lines[0].Source != SourcePackage {
		t.Errorf("want a single package line, got %+v", r.Lines)
	}
}

func TestTwoTiersInOneCategoryRefusesToInventAPrice(t *testing.T) {
	c := cat(map[PlanKey]Money{
		{CategoryID: "grp", Tier: "L3", SessionsPerWeek: 2}: 49000,
		{CategoryID: "grp", Tier: "L4", SessionsPerWeek: 2}: 52000,
	},
		Class{ID: "a", CategoryID: "grp", DefaultTier: "L3"},
		Class{ID: "b", CategoryID: "grp", DefaultTier: "L4"},
	)
	r := Price(Student{ID: "s1", Enrolments: enrolIn("a", "b")}, c)
	if !r.Unpriceable {
		t.Error("two levels in one category produced a price — summing tiers invents one nobody agreed")
	}
	if r.Total != 0 {
		t.Errorf("total %d sen, want 0 for an unpriceable student", r.Total)
	}
}

func TestMissingPlanIsFlaggedNotZero(t *testing.T) {
	c := cat(map[PlanKey]Money{}, Class{ID: "a", Name: "Mandarin", CategoryID: "man", DefaultTier: "L1"})
	r := Price(Student{ID: "s1", Enrolments: enrolIn("a")}, c)
	if !r.Unpriceable {
		t.Fatal("a class with no agreed price was priced silently")
	}
	if r.Lines[0].Problem == "" {
		t.Error("unpriceable line carries no reason, so nobody can act on it")
	}
}

func TestCreditCoveredIsFreeOnPurpose(t *testing.T) {
	c := cat(map[PlanKey]Money{}, Class{ID: "a", Name: "Self-study", CategoryID: "ss", CreditCovered: true, DefaultTier: "x"})
	r := Price(Student{ID: "s1", Enrolments: enrolIn("a")}, c)
	if r.Unpriceable {
		t.Error("credit-covered was treated as unpriceable; it is deliberately free")
	}
	if r.Total != 0 || r.Lines[0].Source != SourceCreditCovered {
		t.Errorf("want one credit-covered line at 0, got total %d lines %+v", r.Total, r.Lines)
	}
}

// A per-class rate is a price for that class, not a frequency tier, so it must
// not be folded into the category group and change its slot count.
func TestOverrideBillsOnItsOwn(t *testing.T) {
	c := cat(map[PlanKey]Money{{CategoryID: "grp", Tier: "L3", SessionsPerWeek: 1}: 26000},
		Class{ID: "a", CategoryID: "grp", DefaultTier: "L3"},
		Class{ID: "b", Name: "One-off", CategoryID: "grp", DefaultTier: "L3", Override: 8000},
	)
	r := Price(Student{ID: "s1", Enrolments: enrolIn("a", "b")}, c)
	if r.Total != 34000 {
		t.Errorf("total %d sen, want 34000 (260 tier + 80 override)", r.Total)
	}
}

func TestStandingDiscountIsItsOwnLine(t *testing.T) {
	c := cat(map[PlanKey]Money{{CategoryID: "grp", Tier: "L3", SessionsPerWeek: 1}: 26000},
		Class{ID: "a", CategoryID: "grp", DefaultTier: "L3"})
	r := Price(Student{ID: "s1", StandingDiscount: 4000, DiscountReason: "Level 3 rate", Enrolments: enrolIn("a")}, c)
	if r.Total != 22000 {
		t.Errorf("total %d sen, want 22000", r.Total)
	}
	var found bool
	for _, l := range r.Lines {
		if l.Source == SourceDiscount {
			found = true
			if l.Amount != -4000 {
				t.Errorf("discount line is %d sen, want -4000", l.Amount)
			}
			if l.ClassName != "Level 3 rate" {
				t.Errorf("discount line reads %q, want the recorded reason", l.ClassName)
			}
		}
	}
	if !found {
		t.Error("no discount line — a smaller total with no line is how five invoices became unexplainable")
	}
}

// Subtracting from a total we do not have would produce a negative bill and
// imply the pricing resolved when it did not.
func TestUnpriceableStudentGetsNoDiscountLine(t *testing.T) {
	c := cat(map[PlanKey]Money{}, Class{ID: "a", CategoryID: "man", DefaultTier: "L1"})
	r := Price(Student{ID: "s1", StandingDiscount: 4000, Enrolments: enrolIn("a")}, c)
	for _, l := range r.Lines {
		if l.Source == SourceDiscount {
			t.Error("an unpriceable student was given a discount line, producing a negative bill")
		}
	}
	if r.Total != 0 {
		t.Errorf("total %d sen, want 0", r.Total)
	}
}
