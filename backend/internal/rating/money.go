// Package rating computes what a student should be charged. It is a pure
// calculation: no database, no invoice numbers, no writes. The monthly run and
// the manual invoice builder both call it, because two implementations of
// "what does this student cost" is exactly how the monthly and session paths
// drifted apart before.
package rating

import "math"

// Money is an exact amount in sen. Ringgit as float64 cannot represent 0.10,
// so every value carries error from birth and totals drift as discounts stack.
// Integers also force the rounding point to be written down: you cannot take a
// percentage off without deciding, in code, where the half sen goes.
type Money int64

// FromRM converts a ringgit figure at the edge of the system. Postgres hands
// back NUMERIC(12,2), which is already exact at two places, so this rounds
// nothing in practice -- it is here for the few float64 values that reach us
// from JSON.
func FromRM(v float64) Money { return Money(roundHalfAwayFromZero(v * 100)) }

// RM converts back for JSON and for the PDF. The wire format is unchanged:
// callers still see ringgit as a float64 with two places.
func (m Money) RM() float64 { return float64(m) / 100 }

// Percent takes a percentage of an amount, rounding half away from zero --
// RM0.125 becomes RM0.13, and -RM0.125 becomes -RM0.13 (ADR-016). Away from
// zero rather than half-up-toward-positive so a discount and a charge of the
// same size round to the same magnitude, which is what a hand calculation does.
//
// This is the ONLY place a percentage becomes money. Rounding once, here, is
// what keeps a line's own figures consistent; the invoice total is then the sum
// of already-rounded lines, so the lines always add up to what is charged.
func (m Money) Percent(pct float64) Money {
	return Money(roundHalfAwayFromZero(float64(m) * pct / 100))
}

// math.Round is already half away from zero. Named so the rule is greppable
// and so changing it means changing one function, not hunting calls.
func roundHalfAwayFromZero(v float64) float64 { return math.Round(v) }
