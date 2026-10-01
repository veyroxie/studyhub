package store

import "testing"

const legacyPending = "Pending"

func TestPaymentMoves(t *testing.T) {
	cases := []struct {
		name     string
		from, to string
		byParent bool
		want     bool
	}{
		{"staff confirms a claim", "Pending Verification", "Paid", false, true},
		{"staff records cash on an open invoice", "Unpaid", "Paid", false, true},
		{"staff reverses a payment made in error", "Paid", "Unpaid", false, true},
		{"staff rejects a claim", "Pending Verification", "Unpaid", false, true},
		{"a legacy Pending invoice can still be settled", legacyPending, "Paid", false, true},
		{"a paid invoice cannot be pushed back into review", "Paid", "Pending Verification", false, false},
		{"overdue is derived, never set", "Unpaid", "Overdue", false, false},
		{"the legacy status is never set again", "Unpaid", legacyPending, false, false},
		{"a draft takes no payment", "Draft", "Paid", false, false},
		{"a void invoice takes no payment", "Void", "Paid", false, false},
		{"a parent claims an open invoice", "Unpaid", "Pending Verification", true, true},
		{"a parent re-sends a claim", "Pending Verification", "Pending Verification", true, true},
		{"a parent cannot reopen a confirmed payment", "Paid", "Pending Verification", true, false},
		{"a parent cannot mark paid", "Unpaid", "Paid", true, false},
	}
	for _, c := range cases {
		if got := PaymentMoveAllowed(c.from, c.to, c.byParent); got != c.want {
			t.Errorf("%s: PaymentMoveAllowed(%q, %q, parent=%v) = %v, want %v", c.name, c.from, c.to, c.byParent, got, c.want)
		}
	}
}
