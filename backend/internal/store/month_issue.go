package store

import (
	"context"
	"fmt"
	"strings"

	"studyhub/internal/core"
	"studyhub/internal/models"
)

// OutboxTopicFamilyBillIssued tells a parent about several children's invoices in
// one email. Its payload is the comma-separated invoice ids issued together.
const OutboxTopicFamilyBillIssued = "family_bill.issued"

// DraftGroup is one family's drafts for a month, issued together or not at all.
type DraftGroup struct {
	TenantID   int
	InvoiceIDs []string
}

// MonthDraftGroups lists a month's Monthly drafts grouped by parent (students.contact,
// the family-bill key). tw/twArgs must be scoped on alias i. A draft with no parent
// email is a group of one.
func MonthDraftGroups(db *DB, tw string, twArgs []any, month string) ([]DraftGroup, error) {
	args := append([]any{month, models.InvoiceStatusDraft}, twArgs...)
	rows, err := db.Query(`SELECT i.id, i.tenant_id, COALESCE(s.contact,'') FROM invoices i
		LEFT JOIN students s ON s.id=i.student_id AND s.tenant_id=i.tenant_id
		WHERE i.type='Monthly' AND i.period=? AND i.status=? AND i.deleted_at IS NULL`+tw+`
		ORDER BY i.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	index := map[string]int{}
	groups := []DraftGroup{}
	for rows.Next() {
		var id, contact string
		var tid int
		if err := rows.Scan(&id, &tid, &contact); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%d|%s", tid, firstNonEmptyString(contact, "id:"+id))
		if i, ok := index[key]; ok {
			groups[i].InvoiceIDs = append(groups[i].InvoiceIDs, id)
			continue
		}
		index[key] = len(groups)
		groups = append(groups, DraftGroup{TenantID: tid, InvoiceIDs: []string{id}})
	}
	return groups, rows.Err()
}

func firstNonEmptyString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// IssueDraftGroup settles and issues a family's drafts in one transaction and queues
// one email for them. Returns each invoice's new number, in InvoiceIDs order.
func IssueDraftGroup(ctx context.Context, db *DB, c *core.Claims, g DraftGroup, today string) ([]string, error) {
	tx, err := db.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	numbers := make([]string, 0, len(g.InvoiceIDs))
	for _, id := range g.InvoiceIDs {
		// Settle before the row freezes: a draft past its early-bird cutoff must not carry it.
		if err := SettleEarlyBirdOnIssue(tx, g.TenantID, id, today); err != nil {
			return nil, fmt.Errorf("settle early bird on %s: %w", id, err)
		}
		number, err := IssueInvoice(tx, c, id, models.InvoiceStatusUnpaid)
		if err != nil {
			return nil, fmt.Errorf("issue %s: %w", id, err)
		}
		if number == "" {
			return nil, fmt.Errorf("issue %s: no longer a draft", id)
		}
		numbers = append(numbers, number)
	}
	topic, payload := OutboxTopicInvoiceIssued, g.InvoiceIDs[0]
	if len(g.InvoiceIDs) > 1 {
		topic, payload = OutboxTopicFamilyBillIssued, strings.Join(g.InvoiceIDs, ",")
	}
	if err := EnqueueOutbox(tx, g.TenantID, topic, payload); err != nil {
		return nil, err
	}
	return numbers, tx.Commit()
}
