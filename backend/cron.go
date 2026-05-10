package main

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"
)

// EarlyBirdRM is the flat discount applied when an invoice is created on or
// before the 7th of the month. Replaces the previous 10% logic.
const EarlyBirdRM = 10.0

// ReferralMonthlyRM is the per-month referral discount granted for 3 months
// when a new family registers using an existing family's referral code.
const ReferralMonthlyRM = 10.0

// startCron launches the background scheduler. It wakes up daily at 00:05
// local time and, on days 1–7 of the month, ensures every active student
// has a Monthly invoice for the current month. Idempotent: skips students
// that already have one. The 7-day window gives the cron room to catch up
// if a deploy or outage caused the 1st to be missed.
func startCron(db *DB) {
	go func() {
		// Run once shortly after boot in case the server was down at the
		// scheduled time.
		runMonthlyInvoiceCycle(db)

		for {
			next := nextRunAt(time.Now())
			time.Sleep(time.Until(next))
			runMonthlyInvoiceCycle(db)
		}
	}()
}

func nextRunAt(now time.Time) time.Time {
	target := time.Date(now.Year(), now.Month(), now.Day(), 0, 5, 0, 0, now.Location())
	if !target.After(now) {
		target = target.AddDate(0, 0, 1)
	}
	return target
}

func runMonthlyInvoiceCycle(db *DB) {
	now := time.Now()
	if now.Day() > 7 {
		return
	}
	created := generateMonthlyInvoices(db, now)
	if created > 0 {
		logger.Info("monthly invoice cron created invoices", "count", created, "month", now.Format("2006-01"))
	}
	payrolls := generateMonthlyPayroll(db, now)
	if payrolls > 0 {
		logger.Info("monthly payroll cron created rows", "count", payrolls, "month", previousMonth(now).Format("2006-01"))
	}
}

func previousMonth(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -1, 0)
}

// generateMonthlyInvoices is the core of the monthly subscription cycle.
// Returns the number of invoices created. Safe to call repeatedly: an
// existing Monthly invoice for the current month blocks duplicates.
//
// Performance: bulk-fetches the existing-invoice and family-credit lookups
// up-front, then issues all inserts inside a single transaction. The naive
// per-student loop ran ~3 queries × N students; for N=200 that's 600 round
// trips. The bulk version is 3 setup queries + N inserts in one transaction.
func generateMonthlyInvoices(db *DB, now time.Time) int {
	monthPrefix := now.Format("2006-01")

	existing := loadExistingMonthlyInvoiceStudentIDs(db, monthPrefix)

	rows, err := db.Query(`
		SELECT s.id, s.tenant_id, s.first_name, s.last_name, s.family_id, s.package_amount
		FROM students s
		WHERE s.deleted_at IS NULL
		  AND COALESCE(s.subscription_status,'active') = 'active'
		  AND COALESCE(s.package_amount,0) > 0
	`)
	if err != nil {
		logger.Error("monthly invoice cron query failed", "err", err)
		return 0
	}
	type row struct {
		id, tenantID, firstName, lastName, familyID string
		packageAmount                               float64
	}
	var pending []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.tenantID, &r.firstName, &r.lastName, &r.familyID, &r.packageAmount); err != nil {
			continue
		}
		if existing[r.id] {
			continue
		}
		pending = append(pending, r)
	}
	rows.Close()
	if len(pending) == 0 {
		return 0
	}

	familyIDs := uniqueFamilyIDs(pending, func(i int) string { return pending[i].familyID })
	familyCredits := loadFamilyReferralCredits(db, familyIDs)

	tx, err := db.BeginTx(context.Background())
	if err != nil {
		logger.Error("monthly invoice cron tx begin failed", "err", err)
		return 0
	}

	earlyBird := EarlyBirdRM
	dueDate := time.Date(now.Year(), now.Month(), 7, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	createdOn := now.Format("2006-01-02")
	monthLabel := now.Format("Jan 2006")

	created := 0
	for _, s := range pending {
		referralCredit := 0.0
		if s.familyID != "" && familyCredits[s.familyID] > 0 {
			referralCredit = ReferralMonthlyRM
		}
		total := s.packageAmount - earlyBird - referralCredit
		if total < 0 {
			total = 0
		}
		invID := generateID("INV")
		desc := "Monthly tuition — " + monthLabel + " — " + s.firstName + " " + s.lastName

		_, err := tx.Exec(`
			INSERT INTO invoices(id,tenant_id,student_id,description,type,amount,due_date,status,created_on,paid_on,payment_method,discount_pct,submitted_by_parent,sibling_ids,sibling_discount,referral_credit,reference_no)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			invID, s.tenantID, s.id, desc, "Monthly", total, dueDate, "Unpaid",
			createdOn, nil, "", 0.0, false, "[]", 0.0, referralCredit, "",
		)
		if err != nil {
			logger.Error("could not insert monthly invoice", "err", err, "student_id", s.id)
			continue
		}
		if referralCredit > 0 && s.familyID != "" {
			tx.Exec(`UPDATE families SET referral_credits_remaining = GREATEST(0, COALESCE(referral_credits_remaining,0) - 1) WHERE id=?`, s.familyID)
			familyCredits[s.familyID]--
		}
		created++
	}

	if err := tx.Commit(); err != nil {
		logger.Error("monthly invoice cron tx commit failed", "err", err)
		return 0
	}
	if created > 0 {
		snapshotCacheInvalidateAll()
	}
	return created
}

// loadExistingMonthlyInvoiceStudentIDs returns the set of student IDs that
// already have a Monthly invoice for the given YYYY-MM prefix.
func loadExistingMonthlyInvoiceStudentIDs(db *DB, monthPrefix string) map[string]bool {
	out := map[string]bool{}
	rows, err := db.Query(`
		SELECT DISTINCT student_id FROM invoices
		WHERE type='Monthly' AND created_on LIKE ? AND deleted_at IS NULL`,
		monthPrefix+"%")
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err == nil {
			out[sid] = true
		}
	}
	return out
}

// loadFamilyReferralCredits returns a map of family_id → remaining credits
// for the requested family IDs. Empty input returns an empty map without
// running a query.
func loadFamilyReferralCredits(db *DB, familyIDs []string) map[string]int {
	out := map[string]int{}
	if len(familyIDs) == 0 {
		return out
	}
	placeholders := make([]string, len(familyIDs))
	args := make([]any, len(familyIDs))
	for i, id := range familyIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	q := "SELECT id, COALESCE(referral_credits_remaining,0) FROM families WHERE id IN (" + strings.Join(placeholders, ",") + ")"
	rows, err := db.Query(q, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var fid string
		var rem int
		if err := rows.Scan(&fid, &rem); err == nil {
			out[fid] = rem
		}
	}
	return out
}

// uniqueFamilyIDs collects the distinct non-empty family IDs from a slice
// without an external dep — keeps the cron file self-contained.
func uniqueFamilyIDs[T any](items []T, get func(int) string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for i := range items {
		id := get(i)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// handleRunMonthlyCron is the admin-triggered manual run for cases where
// the cron missed a window or a new student was set up mid-month.
//
// Runs in a goroutine and returns 202 Accepted immediately so the admin
// dashboard does not block on a 30–60s job that holds DB connections.
// Status is reported through the audit log.
func handleRunMonthlyCron(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		actor := c.Email
		go func() {
			now := time.Now()
			invoices := generateMonthlyInvoices(db, now)
			payrolls := generateMonthlyPayroll(db, now)
			logger.Info("monthly cron manual run finished", "actor", actor, "invoices", invoices, "payrolls", payrolls)
			logAudit(db, actor, "monthly_cron_manual_run", "system", "", "")
		}()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"status":"started","note":"running in background; check audit log for completion"}`))
	}
}

// generateMonthlyPayroll builds payroll rows for the previous month for
// every active staff member. Idempotent — skips a (staff_id, month) row
// that already exists. Full-time staff get a flat base_salary row; part-
// time staff get a row built from their teacher check-in records (hours
// worked × hourly_rate). Returns the number of rows created. Status is
// always Pending so admin reviews/confirms via the existing UI.
func generateMonthlyPayroll(db *DB, now time.Time) int {
	prev := previousMonth(now)
	monthLabel := prev.Format("2006-01")
	monthStart := prev.Format("2006-01-02")
	monthEnd := time.Date(prev.Year(), prev.Month()+1, 1, 0, 0, 0, 0, prev.Location()).AddDate(0, 0, -1).Format("2006-01-02")

	rows, err := db.Query(`
		SELECT id, tenant_id, COALESCE(employment_type,'Full-time'),
		       COALESCE(salary,0), COALESCE(hourly_rate,0)
		FROM staff
		WHERE deleted_at IS NULL AND COALESCE(status,'Active') = 'Active'
	`)
	if err != nil {
		logger.Error("monthly payroll cron query failed", "err", err)
		return 0
	}
	defer rows.Close()

	type staffRow struct {
		id, tenantID, employmentType string
		salary, hourlyRate           float64
	}
	var staff []staffRow
	for rows.Next() {
		var s staffRow
		if err := rows.Scan(&s.id, &s.tenantID, &s.employmentType, &s.salary, &s.hourlyRate); err != nil {
			continue
		}
		staff = append(staff, s)
	}

	created := 0
	for _, s := range staff {
		var existing int
		db.QueryRow(`SELECT COUNT(*) FROM payroll WHERE staff_id=? AND month=?`, s.id, monthLabel).Scan(&existing)
		if existing > 0 {
			continue
		}

		var total float64
		switch s.employmentType {
		case "Part-time":
			if s.hourlyRate <= 0 {
				continue
			}
			hours := teacherHoursWorked(db, s.id, monthStart, monthEnd)
			if hours <= 0 {
				continue
			}
			total = hours * s.hourlyRate
		default: // Full-time
			if s.salary <= 0 {
				continue
			}
			total = s.salary
		}

		id := generateID("PAY")
		_, err := db.Exec(`
			INSERT INTO payroll(id,tenant_id,staff_id,month,base_salary,bonus,deductions,total,status,paid_on)
			VALUES(?,?,?,?,?,?,?,?,?,?)`,
			id, s.tenantID, s.id, monthLabel, total, 0.0, 0.0, total, "Pending", nil,
		)
		if err != nil {
			logger.Error("could not insert payroll row", "err", err, "staff_id", s.id)
			continue
		}
		created++
	}
	return created
}

// teacherHoursWorked sums hours from teacher attendance check-in rows in
// the [start, end] window. Falls back to the scheduled class duration
// (end_time − time) when check_out is missing — covers the case where a
// teacher checked in but never tapped checkout.
func teacherHoursWorked(db *DB, staffID, start, end string) float64 {
	rows, err := db.Query(`
		SELECT a.check_in, a.check_out, c.time, c.end_time
		FROM attendance a
		LEFT JOIN classes c ON c.id = a.class_id
		WHERE a.person_id = ? AND a.person_type = 'teacher'
		  AND a.check_in IS NOT NULL AND a.check_in <> ''
		  AND a.date BETWEEN ? AND ?
	`, staffID, start, end)
	if err != nil {
		return 0
	}
	defer rows.Close()
	total := 0.0
	for rows.Next() {
		var checkIn, checkOut, classTime, classEnd sql.NullString
		if err := rows.Scan(&checkIn, &checkOut, &classTime, &classEnd); err != nil {
			continue
		}
		if checkOut.Valid && checkOut.String != "" {
			h := hoursBetween(checkIn.String, checkOut.String)
			if h > 0 {
				total += h
				continue
			}
		}
		if classTime.Valid && classEnd.Valid {
			h := hoursBetween(classTime.String, classEnd.String)
			if h > 0 {
				total += h
			}
		}
	}
	return total
}

// hoursBetween parses two HH:MM strings and returns the elapsed hours.
// Tolerates zero-padding and silently returns 0 on bad input rather
// than crashing the cron.
func hoursBetween(start, end string) float64 {
	s, errS := time.Parse("15:04", start)
	e, errE := time.Parse("15:04", end)
	if errS != nil || errE != nil {
		return 0
	}
	d := e.Sub(s).Hours()
	if d <= 0 {
		return 0
	}
	return d
}

// handleRegeneratePayroll lets admin re-run payroll for a specific month
// when a teacher checks in late or a staff record changes after the
// cron has already fired. Runs in the background — see handleRunMonthlyCron.
func handleRegeneratePayroll(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		monthArg := r.URL.Query().Get("month")
		var target time.Time
		if monthArg == "" {
			target = time.Now()
		} else {
			t, err := time.Parse("2006-01", monthArg)
			if err != nil {
				respondError(w, "month must be YYYY-MM", 400)
				return
			}
			// generateMonthlyPayroll uses previousMonth(now), so pass the
			// next month to compute payroll for the requested month.
			target = t.AddDate(0, 1, 0)
		}
		actor := c.Email
		go func() {
			count := generateMonthlyPayroll(db, target)
			logger.Info("payroll regenerated", "actor", actor, "month", monthArg, "created", count)
			logAudit(db, actor, "payroll_regenerated", "system", monthArg, "")
		}()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"status":"started"}`))
	}
}
