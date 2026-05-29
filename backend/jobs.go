package main

import (
	"context"
	"fmt"
	"sync"
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
func startJobs(ctx context.Context, wg *sync.WaitGroup, db *DB) {
	logger.Info("background jobs starting")

	jobs := []struct {
		every time.Duration
		name  string
		fn    func()
	}{
		// Daily: archive announcements past their archive_on date so parents
		// stop seeing stale "Holiday closure" notices in February.
		{24 * time.Hour, "archive-announcements", func() { archiveExpiredAnnouncements(db) }},
		// Hourly: send overdue invoice reminders (deduped by reminder_sent_on
		// so a parent doesn't get spammed every hour for the same invoice).
		{1 * time.Hour, "overdue-reminders", func() { sendOverdueInvoiceReminders(db) }},
		// Hourly: re-evaluate pending referral_rewards rows.
		{1 * time.Hour, "referral-recheck", func() { recheckReferralMilestones(db) }},
		// Daily: prune email_tokens rows that are either used or expired and
		// older than 30 days.
		{24 * time.Hour, "email-tokens-purge", func() { purgeExpiredEmailTokens(db) }},
		// Every 30s: drain the email queue (send + retry with backoff).
		{30 * time.Second, "email-queue-worker", func() { processEmailQueue(db) }},
		// Daily: prune long-since-sent/failed email rows.
		{24 * time.Hour, "email-queue-prune", func() { purgeOldEmailQueueRows(db) }},
	}
	for _, j := range jobs {
		wg.Add(1)
		go runEvery(ctx, wg, j.every, j.name, j.fn)
	}
}

// purgeExpiredEmailTokens removes spent or expired email verification tokens
// older than 30 days. Active (not yet used, not yet expired) tokens are kept
// regardless of age.
func purgeExpiredEmailTokens(db *DB) {
	res, err := db.Exec(`DELETE FROM email_tokens WHERE (used_at IS NOT NULL OR expires_at < NOW()) AND created_at < NOW() - INTERVAL '30 days'`)
	if err != nil {
		logger.Error("email-tokens-purge failed", "err", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		logger.Info("email tokens purged", "count", n)
	}
	// Same pass: drop revoked-token entries whose underlying JWT has
	// already expired. Keeping them around achieves nothing — the JWT
	// itself wouldn't validate anymore.
	if res, err := db.Exec(`DELETE FROM revoked_tokens WHERE expires_at < NOW()`); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			logger.Info("revoked tokens purged", "count", n)
		}
	}
	if err := purgeExpiredRefreshTokens(db); err != nil {
		logger.Error("refresh-tokens-purge failed", "err", err)
	}
	// MFA intermediate tokens are normally consumed by /api/auth/mfa/verify
	// (DELETE … RETURNING). Abandoned logins leave rows behind that nothing
	// else cleans. Sweep daily — 5-minute TTL means expired rows are inert.
	if res, err := db.Exec(`DELETE FROM mfa_intermediate WHERE expires_at < NOW()`); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			logger.Info("mfa intermediate purged", "count", n)
		}
	}
}

// runEvery is the goroutine wrapper used by every background job. It
// recovers from panics so a single buggy job can never crash the entire
// process, and it logs both the start and the duration of each cycle.
//
// The goroutine never exits — it's expected to live for the lifetime of
// the server. Graceful shutdown is handled at the HTTP layer; in-flight
// jobs are allowed to finish naturally.
func runEvery(ctx context.Context, wg *sync.WaitGroup, d time.Duration, name string, fn func()) {
	defer wg.Done()
	// Run once on startup so freshly-deployed servers don't wait for the
	// first tick before doing useful work.
	safeRun(name, fn)
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("background job stopped", "job", name)
			return
		case <-t.C:
			safeRun(name, fn)
		}
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
// isHolidayToday returns true when the given tenant has a holiday row whose
// date range covers today. Used by the reminder job to suppress parent-facing
// emails on public holidays (e.g. Hari Raya). end_date is optional — a single
// day holiday has end_date NULL/empty and matches only when date == today.
func isHolidayToday(db *DB, tenantID string) bool {
	t := today()
	var cnt int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM holidays
		  WHERE tenant_id=? AND deleted_at IS NULL
		    AND date <= ?
		    AND (COALESCE(end_date,'') = '' OR end_date >= ?)`,
		tenantID, t, t,
	).Scan(&cnt); err != nil {
		return false
	}
	return cnt > 0
}

func sendOverdueInvoiceReminders(db *DB) {
	rows, err := db.Query(`
		SELECT i.id, i.tenant_id, i.student_id, i.description, i.amount, i.due_date,
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
		invoiceID   string
		tenantID    string
		studentID   string
		studentName string
		parentEmail string
		parentName  string
		description string
		amountRM    string
		dueDate     string
		daysOverdue int
	}
	var batch []pending
	for rows.Next() {
		var p pending
		var amount float64
		if err := rows.Scan(&p.invoiceID, &p.tenantID, &p.studentID, &p.description, &amount, &p.dueDate, &p.studentName, &p.parentEmail, &p.parentName); err != nil {
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

	// Group skip-checks per tenant so the holiday lookup runs once per
	// tenant rather than once per reminder.
	holidaySkip := map[string]bool{}

	for _, p := range batch {
		if _, seen := holidaySkip[p.tenantID]; !seen {
			holidaySkip[p.tenantID] = isHolidayToday(db, p.tenantID)
		}
		if holidaySkip[p.tenantID] {
			continue
		}
		// Honor parent's notification preference. The default is true
		// (opt-out) so missing rows behave as before.
		var wantReminders bool
		db.QueryRow(`SELECT COALESCE(notify_invoice_reminders,true) FROM users WHERE email=?`, p.parentEmail).Scan(&wantReminders)
		if !wantReminders {
			// Still mark reminder_sent_on so we don't re-evaluate
			// this invoice every hour — keeps the query selective.
			db.Exec(`UPDATE invoices SET reminder_sent_on=? WHERE id=? AND tenant_id=?`, today(), p.invoiceID, p.tenantID)
			continue
		}
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
		// Pin to the original tenant so an id collision across tenants cannot
		// accidentally suppress a different tenant's reminder.
		if _, err := db.Exec(`UPDATE invoices SET reminder_sent_on=? WHERE id=? AND tenant_id=?`, today(), p.invoiceID, p.tenantID); err != nil {
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
	rows, err := db.Query(`SELECT referred_student_id, tenant_id FROM referral_rewards WHERE status='pending'`)
	if err != nil {
		logger.Error("referral-recheck query failed", "err", err)
		return
	}
	defer rows.Close()

	type pendingRow struct {
		studentID string
		tenantID  int
	}
	var pending []pendingRow
	for rows.Next() {
		var p pendingRow
		if err := rows.Scan(&p.studentID, &p.tenantID); err == nil {
			pending = append(pending, p)
		}
	}

	if len(pending) == 0 {
		return
	}
	logger.Debug("rechecking referral milestones", "pending", len(pending))

	for _, p := range pending {
		// Synthesise a tenant-scoped claim so the helper's scopeTenant
		// query targets the row's actual tenant rather than running
		// cross-tenant.
		referralCheckMilestoneOnPay(db, p.studentID, &Claims{TenantID: p.tenantID, Role: "system"})
	}
}

