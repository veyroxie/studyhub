package store

import (
	"testing"

	"studyhub/internal/models"
)

// Overdue is arithmetic, not a stored fact. Nothing writes it any more: the
// early-bird job used to, as a side effect of clawing back a discount, and it
// reissues now instead.
func TestDisplayStatusDerivesOverdue(t *testing.T) {
	const today = "2026-09-12"
	cases := []struct {
		name    string
		status  string
		dueDate string
		want    string
	}{
		{"unpaid and past due reads overdue", models.InvoiceStatusUnpaid, "2026-09-07", models.InvoiceStatusOverdue},
		{"due today is not yet overdue", models.InvoiceStatusUnpaid, today, models.InvoiceStatusUnpaid},
		{"due tomorrow is not overdue", models.InvoiceStatusUnpaid, "2026-09-13", models.InvoiceStatusUnpaid},
		{"paid stays paid however late", models.InvoiceStatusPaid, "2026-01-01", models.InvoiceStatusPaid},
		{"a draft is never overdue", models.InvoiceStatusDraft, "2026-01-01", models.InvoiceStatusDraft},
		{"a void invoice is never overdue", models.InvoiceStatusVoid, "2026-01-01", models.InvoiceStatusVoid},
		{"pending verification is not overdue", models.InvoiceStatusPendingVerification, "2026-01-01", models.InvoiceStatusPendingVerification},
		{"no due date cannot be past it", models.InvoiceStatusUnpaid, "", models.InvoiceStatusUnpaid},
		{"a stored Overdue still reads overdue", models.InvoiceStatusOverdue, "2026-09-13", models.InvoiceStatusOverdue},
	}
	for _, tc := range cases {
		if got := DisplayStatus(tc.status, tc.dueDate, today); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
