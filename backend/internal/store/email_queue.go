package store

import (
	"database/sql"
	"studyhub/internal/core"
	"time"
)

// Email queue — persists outbound mail with retry + exponential backoff.
// Wraps mailer.Send so any send-site can opt in by calling
// QueueEmail(...) instead. Retains the same fire-and-forget feel for
// callers but adds durability + observability for free.
//
// Worker: runs every 30s, picks up to N pending rows whose
// next_attempt_at <= NOW(), tries each, on success marks sent_at, on
// failure increments attempts and schedules the next try with backoff.

const (
	emailMaxAttempts  = 5
	emailWorkerBatch  = 20
	emailWorkerPeriod = 30 * time.Second
)

// backoff schedule per attempt count. attempts=1 means "this is the first
// retry" — index 0 in this slice.
var emailRetryDelays = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
	12 * time.Hour,
}

func nextAttemptAt(attempts int) time.Time {
	idx := attempts - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(emailRetryDelays) {
		idx = len(emailRetryDelays) - 1
	}
	return time.Now().Add(emailRetryDelays[idx])
}

// queueEmail persists the message and returns the row id. The worker picks
// it up on its next tick; for low-latency paths (e.g. parent registration)
// callers may also call mailer.Send synchronously and skip the queue.
func QueueEmail(db *DB, tenantID int, to, subject, bodyHTML string) (int64, error) {
	var id int64
	err := db.QueryRow(
		`INSERT INTO email_queue(tenant_id, to_email, subject, body_html, status, next_attempt_at) VALUES(?,?,?,?,?,NOW()) RETURNING id`,
		tenantID, to, subject, bodyHTML, "pending",
	).Scan(&id)
	return id, err
}

// processEmailQueue is the worker. Picks pending rows, tries to send,
// updates state. Returns the count attempted (for log visibility).
func ProcessEmailQueue(db *DB) int {
	rows, err := db.Query(
		`SELECT id, to_email, subject, body_html, attempts FROM email_queue
		  WHERE status='pending' AND next_attempt_at <= NOW()
		  ORDER BY next_attempt_at ASC LIMIT ?`,
		emailWorkerBatch,
	)
	if err != nil {
		core.Logger.Error("email_queue: select pending failed", "err", err)
		return 0
	}
	type job struct {
		id       int64
		to       string
		subject  string
		body     string
		attempts int
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.to, &j.subject, &j.body, &j.attempts); err != nil {
			continue
		}
		jobs = append(jobs, j)
	}
	rows.Close()

	count := 0
	for _, j := range jobs {
		err := core.SendEmail(j.to, j.subject, j.body)
		count++
		if err == nil {
			db.Exec(`UPDATE email_queue SET status='sent', sent_at=NOW(), last_error=NULL WHERE id=?`, j.id)
			continue
		}
		nextAttempts := j.attempts + 1
		if nextAttempts >= emailMaxAttempts {
			db.Exec(`UPDATE email_queue SET status='failed', attempts=?, last_error=? WHERE id=?`,
				nextAttempts, err.Error(), j.id)
			core.Logger.Error("email_queue: gave up", "id", j.id, "to", j.to, "err", err)
			continue
		}
		db.Exec(`UPDATE email_queue SET attempts=?, next_attempt_at=?, last_error=? WHERE id=?`,
			nextAttempts, nextAttemptAt(nextAttempts), err.Error(), j.id)
		core.Logger.Warn("email_queue: send failed, will retry", "id", j.id, "attempts", nextAttempts, "err", err)
	}
	return count
}

// purgeOldEmailQueueRows drops sent/failed rows older than 90 days. Keeps
// the table from growing unbounded over years of operation.
func PurgeOldEmailQueueRows(db *DB) {
	res, err := db.Exec(`DELETE FROM email_queue WHERE status IN ('sent','failed') AND created_at < NOW() - INTERVAL '90 days'`)
	if err != nil {
		core.Logger.Error("email_queue: purge failed", "err", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		core.Logger.Info("email_queue: pruned", "count", n)
	}
}

// Stats projection for ops dashboard. Returned by /api/admin/email-queue
// when wired into the frontend; included here so the helper is testable.
type emailQueueStats struct {
	Pending     int        `json:"pending"`
	Sent24h     int        `json:"sent24h"`
	Failed      int        `json:"failed"`
	OldestStuck *time.Time `json:"oldestStuck,omitempty"`
}

func EmailQueueSummary(db *DB) emailQueueStats {
	var s emailQueueStats
	db.QueryRow(`SELECT COUNT(*) FILTER (WHERE status='pending'),
	                    COUNT(*) FILTER (WHERE status='sent' AND sent_at > NOW() - INTERVAL '24 hours'),
	                    COUNT(*) FILTER (WHERE status='failed')
	             FROM email_queue`).Scan(&s.Pending, &s.Sent24h, &s.Failed)
	var t sql.NullTime
	db.QueryRow(`SELECT MIN(created_at) FROM email_queue WHERE status='pending' AND attempts >= ?`, emailMaxAttempts-1).Scan(&t)
	if t.Valid {
		s.OldestStuck = &t.Time
	}
	return s
}
