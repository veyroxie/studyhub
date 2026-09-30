package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

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
	switch j.Topic {
	case store.OutboxTopicInvoiceIssued:
		return sendIssuedInvoicesEmail(tx, j, []string{j.Payload})
	case store.OutboxTopicFamilyBillIssued:
		return sendIssuedInvoicesEmail(tx, j, strings.Split(j.Payload, ","))
	}
	return fmt.Errorf("unknown outbox topic %q", j.Topic)
}

type issuedInvoice struct {
	parentName, contact, student, desc, dueDate, invoiceNo string
	amount, earlyBird                                      float64
}

// loadIssuedInvoice reads the invoice as it stands NOW, so a correction made between
// issue and send reaches the parent. ok=false means it was deleted or voided since.
func loadIssuedInvoice(tx *store.Tx, id string) (issuedInvoice, bool, error) {
	var inv issuedInvoice
	var first, last, status string
	err := tx.QueryRow(`SELECT COALESCE(s.parent_name,''), COALESCE(s.contact,''),
		COALESCE(s.first_name,''), COALESCE(s.last_name,''),
		COALESCE(i.description,''), COALESCE(i.due_date,''), COALESCE(i.invoice_no,''),
		i.amount, COALESCE(i.early_bird_discount,0), i.status
		FROM invoices i JOIN students s ON s.id = i.student_id AND s.tenant_id = i.tenant_id
		WHERE i.id=? AND i.deleted_at IS NULL`, id).
		Scan(&inv.parentName, &inv.contact, &first, &last, &inv.desc, &inv.dueDate, &inv.invoiceNo,
			&inv.amount, &inv.earlyBird, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return inv, false, nil
	}
	if err != nil {
		return inv, false, fmt.Errorf("load invoice %s: %w", id, err)
	}
	inv.student = first + " " + last
	return inv, store.IsParentVisibleStatus(status), nil
}

func (inv issuedInvoice) earlyBirdNote() string {
	if inv.earlyBird <= 0 {
		return ""
	}
	return fmt.Sprintf("Pay by %s to keep the RM%.0f early-bird discount (RM%.2f after).",
		inv.dueDate, inv.earlyBird, inv.amount+inv.earlyBird)
}

// sendIssuedInvoicesEmail queues one email for the invoices still worth telling the
// parent about. Nothing left, or no email on file, completes the job rather than
// failing it: a failed row blocks every email queued behind it.
func sendIssuedInvoicesEmail(tx *store.Tx, j store.OutboxJob, ids []string) error {
	invs := []issuedInvoice{}
	for _, id := range ids {
		inv, ok, err := loadIssuedInvoice(tx, id)
		if err != nil {
			return err
		}
		if ok {
			invs = append(invs, inv)
		}
	}
	if len(invs) == 0 || invs[0].contact == "" {
		core.Logger.Info("outbox: nothing to send", "topic", j.Topic, "payload", j.Payload)
		return nil
	}
	subject, body := issuedEmail(invs)
	if _, err := store.QueueEmailTx(tx, j.TenantID, invs[0].contact, subject, body); err != nil {
		return fmt.Errorf("queue email for %s: %w", j.Payload, err)
	}
	return nil
}

func issuedEmail(invs []issuedInvoice) (string, string) {
	if len(invs) == 1 {
		inv := invs[0]
		return "Invoice " + inv.invoiceNo + " — " + inv.student,
			mailer.RenderInvoiceIssuedEmail(inv.parentName, inv.student, inv.desc, fmt.Sprintf("%.2f", inv.amount), inv.dueDate, inv.earlyBirdNote())
	}
	lines := make([]mailer.FamilyBillEmailLine, len(invs))
	names := make([]string, len(invs))
	total := 0.0
	for i, inv := range invs {
		lines[i] = mailer.FamilyBillEmailLine{StudentName: inv.student, Description: inv.desc,
			AmountRM: fmt.Sprintf("%.2f", inv.amount), DueDate: inv.dueDate, Note: inv.earlyBirdNote()}
		names[i] = strings.SplitN(inv.student, " ", 2)[0]
		total += inv.amount
	}
	return "Family bill — " + strings.Join(names, ", "),
		mailer.RenderFamilyBillIssuedEmail(invs[0].parentName, lines, fmt.Sprintf("%.2f", total))
}
