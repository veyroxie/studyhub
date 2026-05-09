package main

import (
	"net/http"
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
}

// generateMonthlyInvoices is the core of the monthly subscription cycle.
// Returns the number of invoices created. Safe to call repeatedly: an
// existing Monthly invoice for the current month blocks duplicates.
func generateMonthlyInvoices(db *DB, now time.Time) int {
	monthPrefix := now.Format("2006-01")
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
	defer rows.Close()

	type row struct {
		id, tenantID, firstName, lastName, familyID string
		packageAmount                               float64
	}
	var students []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.tenantID, &r.firstName, &r.lastName, &r.familyID, &r.packageAmount); err != nil {
			continue
		}
		students = append(students, r)
	}

	created := 0
	for _, s := range students {
		var existing int
		db.QueryRow(`
			SELECT COUNT(*) FROM invoices
			WHERE student_id=? AND type='Monthly'
			  AND created_on LIKE ?
			  AND deleted_at IS NULL`,
			s.id, monthPrefix+"%",
		).Scan(&existing)
		if existing > 0 {
			continue
		}

		amount := s.packageAmount
		earlyBird := EarlyBirdRM // cron only runs day 1-7, so always applies
		referralCredit := 0.0
		if s.familyID != "" {
			var remaining int
			db.QueryRow(`SELECT COALESCE(referral_credits_remaining,0) FROM families WHERE id=?`, s.familyID).Scan(&remaining)
			if remaining > 0 {
				referralCredit = ReferralMonthlyRM
			}
		}
		total := amount - earlyBird - referralCredit
		if total < 0 {
			total = 0
		}

		invID := generateID("INV")
		dueDate := time.Date(now.Year(), now.Month(), 7, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
		desc := "Monthly tuition — " + now.Format("Jan 2006") + " — " + s.firstName + " " + s.lastName

		_, err := db.Exec(`
			INSERT INTO invoices(id,tenant_id,student_id,description,type,amount,due_date,status,created_on,paid_on,payment_method,discount_pct,submitted_by_parent,sibling_ids,sibling_discount,referral_credit,reference_no)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			invID, s.tenantID, s.id, desc, "Monthly", total, dueDate, "Unpaid",
			now.Format("2006-01-02"), nil, "", 0.0, false, "[]", 0.0, referralCredit, "",
		)
		if err != nil {
			logger.Error("could not insert monthly invoice", "err", err, "student_id", s.id)
			continue
		}

		if referralCredit > 0 && s.familyID != "" {
			db.Exec(`UPDATE families SET referral_credits_remaining = GREATEST(0, COALESCE(referral_credits_remaining,0) - 1) WHERE id=?`, s.familyID)
		}

		created++
	}
	return created
}

// handleRunMonthlyCron is the admin-triggered manual run for cases where
// the cron missed a window or a new student was set up mid-month.
func handleRunMonthlyCron(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		count := generateMonthlyInvoices(db, time.Now())
		logAudit(db, c.Email, "monthly_cron_manual_run", "system", "", "")
		respond(w, map[string]int{"created": count})
	}
}
