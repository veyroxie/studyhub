package rating

import "sort"

// A discount is an event with provenance, not a number subtracted from a
// total. Two of ours are both a flat RM10 -- the early bird and the referral
// credit -- so an amount alone cannot say which one an invoice carries. That
// ambiguity is what made five invoices unexplainable (ADR-013) and what a
// line-name tag only papered over.
type DiscountKind string

const (
	KindFixed   DiscountKind = "fixed"
	KindPercent DiscountKind = "percent"
)

// Stable type identities. These are what a stored discount refers to, so
// renaming what PRINTS on an invoice never changes what it IS.
const (
	TypeStanding  = "standing"
	TypeReferral  = "referral"
	TypeSibling   = "sibling"
	TypeEarlyBird = "early_bird"
	TypeFOC       = "foc"
)

// A conditional discount is granted before its condition is known. The early
// bird is the only one: it is given on issue and only truly earned if the
// parent pays by the cutoff.
const (
	StateApplied   = "applied"
	StatePending   = "pending"
	StateEarned    = "earned"
	StateForfeited = "forfeited"
)

// Sequence is the stacking order, and it is load-bearing: 10% off then RM10
// off is not the same as RM10 off then 10% off. Recording it as data rather
// than leaving it implicit in the order of statements is what makes a past
// invoice reproducible.
const (
	SeqReferral  = 10
	SeqSibling   = 20
	SeqEarlyBird = 30 // last, so it comes off a total the others have already reduced
	SeqStanding  = 40
)

type Discount struct {
	TypeID   string
	Name     string // what prints on the invoice
	Kind     DiscountKind
	Amount   Money   // KindFixed
	Percent  float64 // KindPercent
	Sequence int
	// Source says WHY this student qualified: which referral reward row, which
	// siblings, whose authorisation. Without it a discount can be seen but not
	// justified.
	Source string
	// Conditional discounts start pending and are resolved after issue.
	Conditional bool
}

// AppliedDiscount is what actually came off, which is not always what was
// asked for: a discount is clamped so it can never take a bill below zero.
// Recording the exact figure is what lets the early-bird clawback restore
// precisely the amount it removed, rather than assuming the full RM10.
type AppliedDiscount struct {
	TypeID   string
	Name     string
	Source   string
	Sequence int
	Amount   Money // positive: what was taken off
	State    string
}

// applyDiscounts runs the discounts in recorded order against a running total,
// appending a negative line for each and returning what the total becomes.
// A percentage is taken on the running total at its point in the order, which
// is why the order has to be data.
func applyDiscounts(r *Result, total Money, ds []Discount) Money {
	ordered := make([]Discount, len(ds))
	copy(ordered, ds)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Sequence < ordered[j].Sequence })

	for _, d := range ordered {
		want := d.Amount
		if d.Kind == KindPercent {
			want = total.Percent(d.Percent)
		}
		if want <= 0 {
			continue
		}
		if want > total {
			want = total
		}
		state := StateApplied
		if d.Conditional {
			state = StatePending
		}
		name := d.Name
		if name == "" {
			name = d.TypeID
		}
		r.Lines = append(r.Lines, Line{ClassName: name, Amount: -want, Source: SourceDiscount})
		r.Discounts = append(r.Discounts, AppliedDiscount{TypeID: d.TypeID, Name: name,
			Source: d.Source, Sequence: d.Sequence, Amount: want, State: state})
		total -= want
		if total == 0 {
			break
		}
	}
	return total
}
