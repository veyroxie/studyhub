package jobs

import (
	"context"
	"fmt"

	"studyhub/internal/core"
	"studyhub/internal/mailer"
	"studyhub/internal/store"
)

// OutboxRelayJob names the relay in the heartbeat and alert paths.
const OutboxRelayJob = "outbox-relay"

// outboxBatch is small on purpose: each job holds a row lock for the length of
// its transaction, and a large batch means a long lock for no gain at one
// invoice run a month.
const outboxBatch = 50

// ProcessOutbox performs the effects queued by issuing invoices and returns how
// many it completed. Each job runs in its own transaction alongside the row
// that marks it done, so an effect and its bookkeeping commit together.
func ProcessOutbox(db *store.DB) int {
	done := 0
	for {
		n, err := processOutboxBatch(db)
		if err != nil {
			core.Logger.Error("outbox relay failed", "err", err)
			return done
		}
		done += n
		if n < outboxBatch {
			return done
		}
	}
}

func processOutboxBatch(db *store.DB) (int, error) {
	tx, err := db.BeginTx(context.Background())
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	jobs, err := store.ClaimOutbox(tx, outboxBatch)
	if err != nil {
		return 0, err
	}
	if len(jobs) == 0 {
		return 0, nil
	}
	for _, j := range jobs {
		if err := performOutboxJob(tx, j); err != nil {
			// One bad job must not strand the rest of the batch, and the row
			// has to survive for a retry -- so the whole transaction is
			// abandoned and the failure recorded outside it.
			tx.Rollback()
			store.MarkOutboxFailed(db, j.ID, err.Error())
			core.Logger.Error("outbox job failed", "err", err, "id", j.ID, "topic", j.Topic)
			return 0, nil
		}
		if err := store.MarkOutboxDone(tx, j.ID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(jobs), nil
}

func performOutboxJob(tx *store.Tx, j store.OutboxJob) error {
	if j.Topic != store.OutboxTopicInvoiceIssued {
		return fmt.Errorf("unknown outbox topic %q", j.Topic)
	}
	// Read the invoice as it stands NOW, not as it was when issued: a
	// correction made between issue and send should reach the parent.
	var parentName, contact, first, last, desc, dueDate, invoiceNo string
	var amount, earlyBird float64
	err := tx.QueryRow(`SELECT COALESCE(s.parent_name,''), COALESCE(s.contact,''),
		COALESCE(s.first_name,''), COALESCE(s.last_name,''),
		COALESCE(i.description,''), COALESCE(i.due_date,''), COALESCE(i.invoice_no,''),
		i.amount, COALESCE(i.early_bird_discount,0)
		FROM invoices i JOIN students s ON s.id = i.student_id AND s.tenant_id = i.tenant_id
		WHERE i.id=? AND i.deleted_at IS NULL`, j.Payload).
		Scan(&parentName, &contact, &first, &last, &desc, &dueDate, &invoiceNo, &amount, &earlyBird)
	if err != nil {
		return fmt.Errorf("load invoice %s: %w", j.Payload, err)
	}
	if contact == "" {
		// Nothing to send, and the job is complete rather than failed: a family
		// with no email on file is not an error to retry forever.
		core.Logger.Info("outbox: invoice has no parent email", "invoice_id", j.Payload)
		return nil
	}
	note := ""
	if earlyBird > 0 {
		note = fmt.Sprintf("Pay by %s to keep the RM%.0f early-bird discount (RM%.2f after).",
			dueDate, earlyBird, amount+earlyBird)
	}
	student := first + " " + last
	subject := "Invoice " + invoiceNo + " — " + student
	body := mailer.RenderInvoiceIssuedEmail(parentName, student, desc, fmt.Sprintf("%.2f", amount), dueDate, note)
	if _, err := store.QueueEmailTx(tx, j.TenantID, contact, subject, body); err != nil {
		return fmt.Errorf("queue email for %s: %w", j.Payload, err)
	}
	return nil
}
