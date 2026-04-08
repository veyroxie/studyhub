package main

import (
	"fmt"
	"time"
)

// startJobs spins up the in-process background workers. Called once from
// main() right after initDB. Each job runs in its own goroutine and uses a
// time.Ticker — there's no external scheduler, no Cron daemon, no NATS.
//
// Why this lightweight pattern at this scale:
//   - Single binary, single instance — no need for distributed scheduling.
//   - Jobs are pure DB sweeps + occasional emails. They tolerate restart
//     because every operation is idempotent or guarded by a "last_*"
//     timestamp column.
//   - Easy to reason about: read the job function, see what it does. No
//     hidden cron syntax, no DAGs.
//
// All jobs share the same DB pool — there's no per-job connection limit
// because the workload is tiny (one query every few minutes).
func startJobs(db *DB) {
	logger.Info("background jobs starting")

	// Daily: archive announcements past their archive_on date so parents
	// stop seeing stale "Holiday closure" notices in February.
	go runEvery(24*time.Hour, "archive-announcements", func() {
		archiveExpiredAnnouncements(db)
	})

	// Hourly: send overdue invoice reminders (deduped by reminder_sent_on
	// so a parent doesn't get spammed every hour for the same invoice).
	go runEvery(1*time.Hour, "overdue-reminders", func() {
		sendOverdueInvoiceReminders(db)
	})

	// Hourly: re-evaluate pending referral_rewards rows. The handleInvoicePay
	// path already triggers this on payment, but the recheck job catches
	// invoices that were marked paid via direct DB update or backfilled
	// imports.
	go runEvery(1*time.Hour, "referral-recheck", func() {
		recheckReferralMilestones(db)
	})
}

// runEvery is the goroutine wrapper used by every background job. It
// recovers from panics so a single buggy job can never crash the entire
// process, and it logs both the start and the duration of each cycle.
//
// The goroutine never exits — it's expected to live for the lifetime of
// the server. Graceful shutdown is handled at the HTTP layer; in-flight
// jobs are allowed to finish naturally.
func runEvery(d time.Duration, name string, fn func()) {
	// Run once on startup so freshly-deployed servers don't wait for the
	// first tick before doing useful work.
	safeRun(name, fn)
	t := time.NewTicker(d)
	defer t.Stop()
	for range t.C {
		safeRun(name, fn)
	}
}

// safeRun calls fn and recovers from any panic, logging it as a fatal job
// error. Without this a single nil-pointer in a job would crash the whole
// API server.
func safeRun(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("background job panic", "job", name, "panic", fmt.Sprint(r))
		}
	}()
	start := time.Now()
	fn()
	logger.Debug("background job tick", "job", name, "duration_ms", time.Since(start).Milliseconds())
}

// ── archive-announcements ───────────────────────────────────────────────────

// archiveExpiredAnnouncements flips status to 'archived' for any announcement
// whose archive_on date has passed. Idempotent — running it twice in a row
// is a no-op.
func archiveExpiredAnnouncements(db *DB) {
	res, err := db.Exec(`UPDATE announcements SET status='archived' WHERE archive_on IS NOT NULL AND archive_on != '' AND archive_on <= ? AND COALESCE(status,'')!='archived'`, today())
	if err != nil {
		logger.Error("archive-announcements failed", "err", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		logger.Info("announcements archived", "count", n)
	}
}

// ── overdue-reminders ───────────────────────────────────────────────────────

// sendOverdueInvoiceReminders sends a friendly reminder email for every
// unpaid invoice that's past its due date AND hasn't been reminded in the
// last 3 days. The dedup column is `invoices.reminder_sent_on` (added by
// migration). Reminders are batched per invoice — one email per invoice,
// not one email per parent — so the parent sees a clear breakdown if they
// have multiple kids in arrears.
func sendOverdueInvoiceReminders(db *DB) {
	rows, err := db.Query(`
		SELECT i.id, i.student_id, i.description, i.amount, i.due_date,
		       COALESCE(s.first_name||' '||s.last_name,'student'), COALESCE(s.contact,''), COALESCE(s.parent_name,'')
		FROM invoices i
		LEFT JOIN students s ON s.id = i.student_id
		WHERE i.status IN ('Unpaid','Overdue')
		  AND i.due_date < ?
		  AND i.deleted_at IS NULL
		  AND (i.reminder_sent_on IS NULL OR i.reminder_sent_on < ?)
	`, today(), threeDaysAgo())
	if err != nil {
		logger.Error("overdue-reminders query failed", "err", err)
		return
	}
	defer rows.Close()

	type pending struct {
		invoiceID    string
		studentID    string
		studentName  string
		parentEmail  string
		parentName   string
		description  string
		amountRM     string
		dueDate      string
		daysOverdue  int
	}
	var batch []pending
	for rows.Next() {
		var p pending
		var amount float64
		if err := rows.Scan(&p.invoiceID, &p.studentID, &p.description, &amount, &p.dueDate, &p.studentName, &p.parentEmail, &p.parentName); err != nil {
			continue
		}
		p.amountRM = fmt.Sprintf("%.2f", amount)
		// Days overdue from today. Best-effort parse — if the due_date isn't
		// ISO format we just say "overdue" without a count.
		if t, err := time.Parse("2006-01-02", p.dueDate); err == nil {
			p.daysOverdue = int(time.Since(t).Hours() / 24)
		}
		if p.parentEmail == "" {
			continue // can't reach them, skip
		}
		batch = append(batch, p)
	}

	if len(batch) == 0 {
		return
	}
	logger.Info("sending overdue reminders", "count", len(batch))

	for _, p := range batch {
		var label string
		if p.daysOverdue <= 0 {
			label = "overdue"
		} else if p.daysOverdue == 1 {
			label = "1 day overdue"
		} else {
			label = fmt.Sprintf("%d days overdue", p.daysOverdue)
		}
		billingURL := appURL() + "/#billing"
		body := renderInvoiceReminderEmail(p.parentName, p.studentName, p.description, p.amountRM, p.dueDate, label, billingURL)
		if err := mailer.Send(p.parentEmail, "Reminder: invoice "+label, body); err != nil {
			logger.Error("overdue reminder send failed", "err", err, "invoice_id", p.invoiceID, "email", p.parentEmail)
			continue
		}
		// Mark this invoice as reminded so it doesn't fire again for 3 days.
		if _, err := db.Exec(`UPDATE invoices SET reminder_sent_on=? WHERE id=?`, today(), p.invoiceID); err != nil {
			logger.Error("overdue reminder mark failed", "err", err, "invoice_id", p.invoiceID)
		}
	}
}

// threeDaysAgo returns today() minus 3 days as an ISO date string. Used by
// the overdue reminder dedup query so a parent gets at most one reminder
// per invoice every 3 days.
func threeDaysAgo() string {
	return time.Now().AddDate(0, 0, -3).Format("2006-01-02")
}

// ── referral-recheck ────────────────────────────────────────────────────────

// recheckReferralMilestones sweeps every pending referral_rewards row and
// re-evaluates whether the referred student has hit the 3-paid-month
// milestone. The handleInvoicePay path already does this on the happy path,
// but this job catches edge cases where invoices were paid via direct DB
// update, batch import, or admin manual entry that bypassed the API.
//
// Calls the same helper used by the inline path so the logic stays in one
// place — change the threshold once, both paths pick it up.
func recheckReferralMilestones(db *DB) {
	rows, err := db.Query(`SELECT referred_student_id FROM referral_rewards WHERE status='pending'`)
	if err != nil {
		logger.Error("referral-recheck query failed", "err", err)
		return
	}
	defer rows.Close()

	var studentIDs []string
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err == nil {
			studentIDs = append(studentIDs, sid)
		}
	}

	if len(studentIDs) == 0 {
		return
	}
	logger.Debug("rechecking referral milestones", "pending", len(studentIDs))

	for _, sid := range studentIDs {
		referralCheckMilestoneOnPay(db, sid)
	}
}

