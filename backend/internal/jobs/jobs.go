package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"os"
	"strings"
	"studyhub/internal/core"
	"studyhub/internal/mailer"
	"studyhub/internal/models"
	"studyhub/internal/store"
	"sync"
	"syscall"
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
func StartJobs(ctx context.Context, wg *sync.WaitGroup, db *store.DB) {
	core.Logger.Info("background jobs starting")

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
		// Hourly: expire the early-bird discount on monthly invoices left unpaid
		// past their cutoff (the 10th) — restore full price.
		{1 * time.Hour, "early-bird-expiry", func() { applyEarlyBirdExpiry(db) }},
		// Daily: prune email_tokens rows that are either used or expired and
		// older than 30 days.
		{24 * time.Hour, "email-tokens-purge", func() { purgeExpiredEmailTokens(db) }},
		// Hourly: self-check (disk, backup freshness, stuck email queue) and
		// alert the operator. Complements an external uptime monitor (which
		// catches "app down") by catching "app up but degraded".
		{1 * time.Hour, "health-selfcheck", func() { runHealthSelfCheck(db) }},
		// Every 30s: drain the email queue (send + retry with backoff).
		{30 * time.Second, "email-queue-worker", func() { store.ProcessEmailQueue(db) }},
		// Daily: prune long-since-sent/failed email rows.
		{24 * time.Hour, "email-queue-prune", func() { store.PurgeOldEmailQueueRows(db) }},
	}
	// Expectations derive from each job's own interval, so nothing needs
	// declaring twice. 3x the interval tolerates one missed tick and a slow
	// run before it complains.
	jobLimits = map[string]time.Duration{}
	for _, j := range jobs {
		jobLimits[j.name] = 3 * j.every
	}
	for k, v := range externalJobLimits {
		jobLimits[k] = v
	}
	// An allowlist mutes nearly every send by design, so "no delivery in a
	// week" stops being evidence of a broken transport and becomes the
	// expected state. Watching it anyway would train the operator to ignore
	// the alert that matters — so the check states plainly that it is off.
	if os.Getenv("OUTBOUND_ALLOWLIST") != "" {
		delete(jobLimits, "email-delivery")
		core.Logger.Warn("OUTBOUND_ALLOWLIST is set — mail to anyone else is dropped, and the email-delivery health check is disabled")
	}
	for _, j := range jobs {
		wg.Add(1)
		go runEvery(ctx, wg, j.every, j.name, j.fn, db)
	}
}

// jobLimits maps a job name to how long it may go without reporting success.
// Populated from each job's own interval at startup; see StartBackgroundJobs.
var jobLimits = map[string]time.Duration{}

// externalJobLimits covers work scheduled OUTSIDE this process — the host
// crontab — which cannot register itself. These two write their heartbeat with
// psql, and only on genuine success: for the backup that means the off-site
// upload completed, not merely that a local dump was written. The distinction
// is the outage of 2026-09-01, where the dump always worked and the upload
// silently did nothing for months.
//
// A job here that stops being scheduled at all still alerts, because a missing
// heartbeat is treated the same as a stale one.
var externalJobLimits = map[string]time.Duration{
	// Recorded on actual DELIVERY (store.ProcessEmailQueue), not on the worker
	// running. A week with no successful send is a broken transport, not a
	// quiet week — password resets and verifications alone make that
	// implausible.
	"email-delivery": 7 * 24 * time.Hour,
	"backup-upload":  36 * time.Hour,     // nightly at 02:00
	"backup-verify":  9 * 24 * time.Hour, // weekly on Sunday
}

// alertOnce throttles a given alert category to at most once per 24h so the
// hourly self-check doesn't spam the operator. In-memory is fine: a restart
// just re-arms alerts, which is the safe direction.
var (
	alertMu       sync.Mutex
	alertLastSent = map[string]time.Time{}
)

func alertOnce(key string) bool {
	alertMu.Lock()
	defer alertMu.Unlock()
	if last, ok := alertLastSent[key]; ok && time.Since(last) < 24*time.Hour {
		return false
	}
	alertLastSent[key] = time.Now()
	return true
}

// backupIsStale reports whether the newest file in dir is older than maxAge
// (or missing/unreadable) — a proxy for "nightly DB backup stopped running".
func backupIsStale(dir string, maxAge time.Duration) (bool, string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true, "backups directory unreadable: " + err.Error()
	}
	var newest time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	if newest.IsZero() {
		return true, "no backup files found in " + dir
	}
	if age := time.Since(newest); age > maxAge {
		return true, fmt.Sprintf("newest backup is %s old", age.Round(time.Hour))
	}
	return false, ""
}

// runHealthSelfCheck looks for "up but degraded" conditions (low disk, stale
// backups, a wedged email queue) and notifies ALERT_EMAIL. If ALERT_EMAIL is
// unset it still logs at ERROR so a log-based alerting pipeline can catch it.
// Each category is throttled to once per 24h.
func runHealthSelfCheck(db *store.DB) {
	var alerts []string

	var st syscall.Statfs_t
	if err := syscall.Statfs("/app", &st); err == nil && st.Blocks > 0 {
		freePct := float64(st.Bavail) / float64(st.Blocks) * 100
		if freePct < 15 && alertOnce("disk") {
			alerts = append(alerts, fmt.Sprintf("Low disk space: %.1f%% free on the app volume.", freePct))
		}
	}

	if stale, detail := backupIsStale("/app/backups", 36*time.Hour); stale && alertOnce("backup") {
		alerts = append(alerts, "Database backup looks stale — "+detail+".")
	}

	// Any scheduled job that has gone quiet — including the two run from the
	// host crontab. This replaces per-symptom checks with one that covers
	// every job, so the next one added is watched without being thought about.
	for _, sj := range store.StaleJobs(db, jobLimits, core.BootTime) {
		if alertOnce("job:" + sj.Name) {
			alerts = append(alerts, sj.String()+".")
		}
	}

	// Fresh local backups are NOT a working backup: they sit on the same
	// droplet as the database, so losing the machine loses both. This went
	// unnoticed for months because the freshness check above was passing --
	// the nightly dump ran fine, it just never left the box.
	if core.AppEnv() == "production" && os.Getenv("S3_BUCKET") == "" && alertOnce("backup_offsite") {
		alerts = append(alerts, "Backups are LOCAL ONLY — S3_BUCKET is unset, so nothing is copied off this droplet. Losing the droplet loses every backup with it.")
	}

	var stuck int
	if err := db.QueryRow(`SELECT COUNT(*) FROM email_queue WHERE status='pending' AND next_attempt_at < NOW() - INTERVAL '2 hours'`).Scan(&stuck); err == nil && stuck > 0 && alertOnce("email_queue") {
		alerts = append(alerts, fmt.Sprintf("%d email(s) stuck in the send queue for over 2 hours.", stuck))
	}

	// Nothing getting THROUGH. The check above watches mail stuck pending, so a
	// queue that fails everything looks idle and healthy — which is exactly what
	// happened: five weeks of every send failing on an unverified sending
	// domain, 2,354 logged failures, no alert, and ten accounts left waiting on
	// a verification email that could never arrive.
	var failedMail, sent24h int
	if err := db.QueryRow(`SELECT COUNT(*) FILTER (WHERE status='failed'),
	                              COUNT(*) FILTER (WHERE status='sent' AND sent_at > NOW() - INTERVAL '24 hours')
	                       FROM email_queue`).Scan(&failedMail, &sent24h); err == nil {
		if failedMail > 0 && sent24h == 0 && alertOnce("email_delivery") {
			alerts = append(alerts, fmt.Sprintf(
				"%d email(s) permanently failed and NONE delivered in 24h — outbound mail is down, not slow. Check the sending domain is verified.", failedMail))
		}
	}

	if len(alerts) == 0 {
		return
	}
	joined := strings.Join(alerts, "; ")
	core.Logger.Error("health self-check found issues", "issues", joined)

	to := os.Getenv("ALERT_EMAIL")
	if to == "" {
		return // logged above; set ALERT_EMAIL to also receive an email
	}
	items := make([]string, len(alerts))
	for i, a := range alerts {
		items[i] = "<li>" + html.EscapeString(a) + "</li>"
	}
	body := "<p>StudyHub health check found the following issue(s):</p><ul>" + strings.Join(items, "") + "</ul>"
	if err := core.SendEmail(to, fmt.Sprintf("StudyHub alert: %d issue(s)", len(alerts)), body); err != nil {
		core.Logger.Error("health alert email failed", "err", err)
	}
}

// applyEarlyBirdExpiry restores full price on monthly invoices whose early-bird
// cutoff (the 7th) has passed while still unpaid. It adds the exact discount
// back to amount, removes the matching "Early bird discount" line item so the
// customer-facing PDF still balances (Subtotal − discounts = Total Due), and
// clears the early-bird fields — so it runs once per invoice and is safe to
// call repeatedly. Pending-Verification and Paid invoices are left alone (the
// parent already paid in time). Mutating amount keeps it the single source of
// truth for every payment path (admin/parent/online).
func applyEarlyBirdExpiry(db *store.DB) {
	tx, err := db.BeginTx(context.Background())
	if err != nil {
		core.Logger.Error("early-bird-expiry tx begin failed", "err", err)
		return
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT id, COALESCE(line_items,'[]') FROM invoices
		WHERE type = 'Monthly'
		  AND status IN ('Unpaid','Overdue')
		  AND early_bird_cutoff <> ''
		  AND early_bird_cutoff < ?
		  AND deleted_at IS NULL`, core.Today())
	if err != nil {
		core.Logger.Error("early-bird-expiry select failed", "err", err)
		return
	}
	type expired struct {
		id        string
		lineItems string
	}
	var toExpire []expired
	for rows.Next() {
		var e expired
		var raw string
		if err := rows.Scan(&e.id, &raw); err != nil {
			continue
		}
		items := models.ParseLineItems(raw)
		kept := items[:0]
		for _, it := range items {
			if it.Kind == models.LineItemKindDiscount && strings.HasPrefix(it.Name, "Early bird") {
				continue
			}
			kept = append(kept, it)
		}
		e.lineItems = models.MarshalLineItems(kept)
		toExpire = append(toExpire, e)
	}
	rows.Close()

	for _, e := range toExpire {
		if _, err := tx.Exec(`UPDATE invoices
			SET amount = amount + early_bird_discount,
			    status = 'Overdue',
			    early_bird_discount = 0,
			    early_bird_cutoff = '',
			    line_items = ?
			WHERE id = ?`, e.lineItems, e.id); err != nil {
			core.Logger.Error("early-bird-expiry update failed", "err", err, "invoice_id", e.id)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		core.Logger.Error("early-bird-expiry tx commit failed", "err", err)
		return
	}
	if len(toExpire) > 0 {
		core.Logger.Info("early-bird discount expired on unpaid invoices", "count", len(toExpire))
		store.SnapshotCacheInvalidateAll()
	}
}

// purgeExpiredEmailTokens removes spent or expired email verification tokens
// older than 30 days. Active (not yet used, not yet expired) tokens are kept
// regardless of age.
func purgeExpiredEmailTokens(db *store.DB) {
	res, err := db.Exec(`DELETE FROM email_tokens WHERE (used_at IS NOT NULL OR expires_at < NOW()) AND created_at < NOW() - INTERVAL '30 days'`)
	if err != nil {
		core.Logger.Error("email-tokens-purge failed", "err", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		core.Logger.Info("email tokens purged", "count", n)
	}
	// Same pass: drop revoked-token entries whose underlying JWT has
	// already expired. Keeping them around achieves nothing — the JWT
	// itself wouldn't validate anymore.
	if res, err := db.Exec(`DELETE FROM revoked_tokens WHERE expires_at < NOW()`); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			core.Logger.Info("revoked tokens purged", "count", n)
		}
	}
	if err := store.PurgeExpiredRefreshTokens(db); err != nil {
		core.Logger.Error("refresh-tokens-purge failed", "err", err)
	}
	// MFA intermediate tokens are normally consumed by /api/auth/mfa/verify
	// (DELETE … RETURNING). Abandoned logins leave rows behind that nothing
	// else cleans. Sweep daily — 5-minute TTL means expired rows are inert.
	if res, err := db.Exec(`DELETE FROM mfa_intermediate WHERE expires_at < NOW()`); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			core.Logger.Info("mfa intermediate purged", "count", n)
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
func runEvery(ctx context.Context, wg *sync.WaitGroup, d time.Duration, name string, fn func(), db *store.DB) {
	defer wg.Done()
	// Heartbeat after every run, here rather than inside each job, so a job
	// added later is monitored without anyone remembering to wire it up. Note
	// what this proves: the job RAN to completion without panicking. It cannot
	// know whether the work was meaningful -- backup.sh records its own
	// heartbeat only after the upload actually succeeds, which is the stronger
	// claim and the one that was missing.
	beat := func() {
		safeRun(name, fn)
		store.RecordJobSuccess(db, name, "")
	}
	// Run once on startup so freshly-deployed servers don't wait for the
	// first tick before doing useful work.
	beat()
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			core.Logger.Info("background job stopped", "job", name)
			return
		case <-t.C:
			beat()
		}
	}
}

// safeRun calls fn and recovers from any panic, logging it as a fatal job
// error. Without this a single nil-pointer in a job would crash the whole
// API server.
func safeRun(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			core.Logger.Error("background job panic", "job", name, "panic", fmt.Sprint(r))
		}
	}()
	start := time.Now()
	fn()
	core.Logger.Debug("background job tick", "job", name, "duration_ms", time.Since(start).Milliseconds())
}

// ── archive-announcements ───────────────────────────────────────────────────

// archiveExpiredAnnouncements flips status to 'archived' for any announcement
// whose archive_on date has passed. Idempotent — running it twice in a row
// is a no-op.
func archiveExpiredAnnouncements(db *store.DB) {
	res, err := db.Exec(`UPDATE announcements SET status='archived' WHERE archive_on IS NOT NULL AND archive_on != '' AND archive_on <= ? AND COALESCE(status,'')!='archived'`, core.Today())
	if err != nil {
		core.Logger.Error("archive-announcements failed", "err", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		core.Logger.Info("announcements archived", "count", n)
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
func isHolidayToday(db *store.DB, tenantID string) bool {
	// The old SQL predicate treated an empty end_date as open-ended, so one
	// past single-day holiday suppressed reminders forever. Filter through
	// the canonical core.HolidayCovers instead.
	t := core.Today()
	rows, err := db.Query(`SELECT date, COALESCE(end_date,'') FROM holidays WHERE tenant_id=? AND deleted_at IS NULL`, tenantID)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var date, endDate string
		if rows.Scan(&date, &endDate) == nil && core.HolidayCovers(date, endDate, t) {
			return true
		}
	}
	return false
}

func sendOverdueInvoiceReminders(db *store.DB) {
	rows, err := db.Query(`
		SELECT i.id, i.tenant_id, i.student_id, i.description, i.amount, i.due_date,
		       COALESCE(s.first_name||' '||s.last_name,'student'), COALESCE(s.contact,''), COALESCE(s.parent_name,'')
		FROM invoices i
		LEFT JOIN students s ON s.id = i.student_id
		WHERE i.status IN ('Unpaid','Overdue')
		  AND i.due_date < ?
		  AND i.deleted_at IS NULL
		  AND (i.reminder_sent_on IS NULL OR i.reminder_sent_on < ?)
	`, core.Today(), threeDaysAgo())
	if err != nil {
		core.Logger.Error("overdue-reminders query failed", "err", err)
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
	core.Logger.Info("sending overdue reminders", "count", len(batch))

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
		// Honor parent's notification preference. Default true (opt-out) on a
		// missing row OR a scan error — the zero value is false, which would
		// silently mark the invoice reminded without ever sending.
		wantReminders := true
		if err := db.QueryRow(`SELECT COALESCE(notify_invoice_reminders,true) FROM users WHERE email=?`, p.parentEmail).Scan(&wantReminders); err != nil && err != sql.ErrNoRows {
			wantReminders = true
		}
		if !wantReminders {
			// Still mark reminder_sent_on so we don't re-evaluate
			// this invoice every hour — keeps the query selective.
			db.Exec(`UPDATE invoices SET reminder_sent_on=? WHERE id=? AND tenant_id=?`, core.Today(), p.invoiceID, p.tenantID)
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
		// Claim BEFORE sending: mark reminder_sent_on first so two overlapping
		// instances (rolling deploy) can't both email the same parent. The
		// guarded WHERE makes exactly one claimer win; the loser skips. On a
		// send failure the claim is released so a later tick retries.
		res, err := db.Exec(`UPDATE invoices SET reminder_sent_on=? WHERE id=? AND tenant_id=? AND (reminder_sent_on IS NULL OR reminder_sent_on < ?)`,
			core.Today(), p.invoiceID, p.tenantID, threeDaysAgo())
		if err != nil {
			core.Logger.Error("overdue reminder claim failed", "err", err, "invoice_id", p.invoiceID)
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // another instance claimed it
		}
		billingURL := mailer.AppURL() + "/#billing"
		body := mailer.RenderInvoiceReminderEmail(p.parentName, p.studentName, p.description, p.amountRM, p.dueDate, label, billingURL)
		if err := core.SendEmail(p.parentEmail, "Reminder: invoice "+label, body); err != nil {
			core.Logger.Error("overdue reminder send failed", "err", err, "invoice_id", p.invoiceID, "email", p.parentEmail)
			db.Exec(`UPDATE invoices SET reminder_sent_on=NULL WHERE id=? AND tenant_id=?`, p.invoiceID, p.tenantID)
			continue
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
func recheckReferralMilestones(db *store.DB) {
	rows, err := db.Query(`SELECT referred_student_id, tenant_id FROM referral_rewards WHERE status='pending'`)
	if err != nil {
		core.Logger.Error("referral-recheck query failed", "err", err)
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
	core.Logger.Debug("rechecking referral milestones", "pending", len(pending))

	for _, p := range pending {
		// Synthesise a tenant-scoped claim so the helper's scopeTenant
		// query targets the row's actual tenant rather than running
		// cross-tenant.
		store.ReferralCheckMilestoneOnPay(db, p.studentID, &core.Claims{TenantID: p.tenantID, Role: "system"})
	}
}
