package rating

import "testing"

func line(name string, amount Money) InvoiceLine {
	return InvoiceLine{Name: name, Amount: amount}
}
func disc(name string, amount Money) InvoiceLine {
	return InvoiceLine{Name: name, IsDiscount: true, Amount: -amount}
}

// The case that started this: a joining month. Registration and a deposit are
// real charges the catalogue has no opinion on, so the raw difference looks
// like a RM250 overcharge and is nothing of the sort.
func TestJoiningMonthIsFullyExplained(t *testing.T) {
	a := Attribute(26000, 77000, []InvoiceLine{
		line("Registration Fee", 25000),
		line("Deposit (1 month) — Group Level 4-6", 26000),
		line("Group", 26000),
	})
	if !a.Explained() {
		t.Errorf("residual %d sen on a joining month, want 0 — reasons: %v", a.Residual, a.Reasons)
	}
	if a.OneOffs != 51000 {
		t.Errorf("one-offs %d sen, want 51000", a.OneOffs)
	}
}

// A hand-applied discount is a named cause, not a mispricing.
func TestADiscountedMonthIsExplained(t *testing.T) {
	a := Attribute(26000, 22000, []InvoiceLine{
		line("Level 3 & 4", 26000),
		disc("Level 3 rate", 4000),
	})
	if !a.Explained() {
		t.Errorf("residual %d sen, want 0", a.Residual)
	}
	if a.Discounts != 4000 {
		t.Errorf("discounts %d sen, want 4000", a.Discounts)
	}
}

// The early bird comes off the invoice and not the catalogue, so it has to be
// counted or every invoice raised before the 7th looks RM10 wrong.
func TestEarlyBirdIsExplained(t *testing.T) {
	a := Attribute(24000, 23000, []InvoiceLine{
		line("Group", 24000),
		disc("Early bird discount", 1000),
	})
	if !a.Explained() {
		t.Errorf("residual %d sen, want 0", a.Residual)
	}
}

// The one the gate exists for: a figure nobody can account for.
func TestAnUnexplainedGapShowsAsResidual(t *testing.T) {
	a := Attribute(26000, 30000, []InvoiceLine{line("Level 3 & 4", 26000)})
	if a.Explained() {
		t.Fatal("a RM40 gap with no discount and no one-off was reported as explained")
	}
	if a.Residual != 4000 {
		t.Errorf("residual %d sen, want 4000", a.Residual)
	}
}

// Haruto exactly: registration and deposit, and the tuition line left off. The
// invoice is internally consistent, so the residual is zero -- what is wrong
// with it is the MISSING line, which shows up as the catalogue being unbilled
// rather than as an unexplained figure.
func TestHarutoReadsAsExplainedButUnderbilled(t *testing.T) {
	a := Attribute(26000, 51000, []InvoiceLine{
		line("Registration Fee", 25000),
		line("Deposit (1 month) — Group Level 4-6", 26000),
	})
	if a.Residual != -26000 {
		t.Errorf("residual %d sen, want -26000: the month's tuition was never charged", a.Residual)
	}
	if a.Explained() {
		t.Error("an invoice missing a whole month of tuition must not read as explained")
	}
}

func TestNoLinesMeansTheWholeGapIsUnexplained(t *testing.T) {
	a := Attribute(26000, 51000, nil)
	if a.Residual != 25000 {
		t.Errorf("residual %d sen, want 25000", a.Residual)
	}
}
