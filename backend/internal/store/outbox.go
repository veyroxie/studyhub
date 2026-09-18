package store

import (
	"database/sql"
	"fmt"
)

// The outbox decouples "the invoice was issued" from "the parent was told".
// Calling an external system inside the issuing transaction is how events get
// lost: the invoice commits and the email never sends, or the email sends and
// the invoice rolls back. The row goes in with the invoice; a relay performs
// the effect afterwards.
const (
	OutboxTopicInvoiceIssued = "invoice.issued"
)

// EnqueueOutbox writes an effect to perform once the caller's transaction
// commits. Payload is the invoice id -- deliberately not a rendered email: the
// relay reads the invoice as it stands when it runs, so a correction made
// between issue and send is reflected rather than frozen into the payload.
func EnqueueOutbox(tx *Tx, tenantID int, topic, payload string) error {
	if _, err := tx.Exec(`INSERT INTO outbox(tenant_id, topic, payload) VALUES(?,?,?)`,
		tenantID, topic, payload); err != nil {
		return fmt.Errorf("enqueue %s: %w", topic, err)
	}
	return nil
}

// OutboxJob is one pending effect, claimed for this worker only.
type OutboxJob struct {
	ID       int64
	TenantID int
	Topic    string
	Payload  string
	Attempts int
}

// ClaimOutbox takes up to limit pending rows inside tx, skipping any another
// worker already holds. FOR UPDATE SKIP LOCKED is what makes a second instance
// (a rolling deploy, a manual run overlapping the tick) safe: it takes
// different rows rather than the same ones.
//
// The caller must perform the effect and call MarkOutboxDone in the SAME
// transaction, then commit. Effects that are themselves transactional (queueing
// an email is) are then exactly-once; a non-transactional effect would be
// at-least-once and must be idempotent.
func ClaimOutbox(tx *Tx, limit int) ([]OutboxJob, error) {
	rows, err := tx.Query(`SELECT id, tenant_id, topic, payload, attempts
		FROM outbox WHERE processed_at IS NULL
		ORDER BY created_at
		LIMIT ? FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OutboxJob{}
	for rows.Next() {
		var j OutboxJob
		if err := rows.Scan(&j.ID, &j.TenantID, &j.Topic, &j.Payload, &j.Attempts); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func MarkOutboxDone(tx *Tx, id int64) error {
	_, err := tx.Exec(`UPDATE outbox SET processed_at=NOW(), last_error='' WHERE id=?`, id)
	return err
}

// MarkOutboxFailed records the attempt without consuming the row, so the relay
// retries it. Runs on its own connection because the caller's transaction is
// being rolled back.
func MarkOutboxFailed(db *DB, id int64, cause string) {
	if _, err := db.Exec(`UPDATE outbox SET attempts = attempts + 1, last_error=? WHERE id=?`,
		cause, id); err != nil {
		return
	}
}

// PendingOutboxCount is for the health endpoint: a backlog that stops draining
// means parents are not being told about invoices that exist.
func PendingOutboxCount(db *DB) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM outbox WHERE processed_at IS NULL`).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}
