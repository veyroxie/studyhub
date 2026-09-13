package rating

import "strings"

// Attribution explains a gap between what the catalogue prices and what a
// student was actually invoiced.
//
// The catalogue prices TUITION. An invoice legitimately carries more: a
// registration fee, a deposit, a one-off class. And it legitimately carries
// less: a discount that was applied by hand. So "computed != invoiced" is the
// normal state for a real month, and a cutover gate of "zero differences" is
// unreachable by construction -- it would either block the switch forever or be
// waived, which is worse.
//
// The gate that IS reachable: every difference attributable to a named cause,
// and nothing left over. Residual is what nobody can explain, and that is the
// only number the gate should care about.
type Attribution struct {
	Catalogue Money // what the engine says the tuition costs
	Invoiced  Money // what the invoice actually totals
	OneOffs   Money // charges the catalogue does not price: registration, deposit, membership
	Discounts Money // reductions on the invoice, as a positive figure
	Residual  Money // invoiced - (catalogue + oneOffs - discounts). Zero means fully explained.
	Reasons   []string
}

// Explained reports whether the whole gap is accounted for.
func (a Attribution) Explained() bool { return a.Residual == 0 }

// oneOffLineNames are charges that are real but are not tuition, so the
// catalogue has no opinion on them. Kept in step with the invoice builder's own
// list: these are exactly the entries it offers that the engine cannot price.
var oneOffLineNames = []string{
	"Registration Fee",
	"Deposit",
	"TSH Membership",
	"Self-study",
}

// InvoiceLine is the subset of an invoice line this needs, so the engine stays
// free of the models package and of anything that knows about JSON or SQL.
type InvoiceLine struct {
	Name       string
	IsDiscount bool
	Amount     Money // as stored: negative for a discount
}

// Attribute explains one student's difference for one month.
func Attribute(catalogue Money, invoiced Money, lines []InvoiceLine) Attribution {
	a := Attribution{Catalogue: catalogue, Invoiced: invoiced}
	for _, l := range lines {
		switch {
		case l.IsDiscount:
			d := l.Amount
			if d < 0 {
				d = -d
			}
			a.Discounts += d
			a.Reasons = append(a.Reasons, "discount: "+l.Name)
		case isOneOff(l.Name):
			a.OneOffs += l.Amount
			a.Reasons = append(a.Reasons, "one-off: "+l.Name)
		}
	}
	a.Residual = invoiced - (catalogue + a.OneOffs - a.Discounts)
	return a
}

func isOneOff(name string) bool {
	for _, prefix := range oneOffLineNames {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
