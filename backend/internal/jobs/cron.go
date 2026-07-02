package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"studyhub/internal/core"
	"studyhub/internal/mailer"
	"studyhub/internal/models"
	"studyhub/internal/store"
	"sync"
	"time"
)

// EarlyBirdRM is the flat discount applied when an invoice is created on or
// before the 7th of the month. Replaces the previous 10% logic.
const EarlyBirdRM = 10.0

// ReferralMonthlyRM is the per-month referral discount granted for 3 months
// when a new family registers using an existing family's referral code.
const ReferralMonthlyRM = 10.0

// SiblingMonthlyRM is the per-month per-child sibling discount applied when
// a family has 2+ active subscribed students. Flat RM10 off each invoice.
const SiblingMonthlyRM = 10.0

// SelfStudyOverflowRatePerHour is what we bill per hour beyond the student's
// package_self_study_hours quota for the month. Cheap on purpose — the
// quota is generous and overflow is a soft nudge, not a profit centre.
const SelfStudyOverflowRatePerHour = 10.0

// classMeta carries the per-class detail the monthly cron needs to render one
// invoice line item per enrolled class (name, type and band for the label,
// plus the monthly fee from the pricing matrix).
type classMeta struct {
	fee       float64
	name      string
	classType string
	band      string
}

// appendDiscount adds a negative "discount" line item when amt > 0. Keeps the
// invoice builder flat instead of nesting an if around every discount append.
func appendDiscount(items []models.InvoiceLineItem, name string, amt float64) []models.InvoiceLineItem {
	if amt <= 0 {
		return items
	}
	return append(items, models.InvoiceLineItem{Kind: models.LineItemKindDiscount, Name: name, Amount: -amt})
}

// monthlyClassLineName renders the bold line-item heading for an enrolled
// class, e.g. "Singapore Math - Group".
func monthlyClassLineName(m classMeta) string {
	if m.classType == "" {
		return m.name
	}
	return m.name + " - " + m.classType
}

// monthlyClassDescriptor renders the gray sub-line, e.g. "Group class, Level 1-3".
func monthlyClassDescriptor(m classMeta) string {
	parts := []string{}
	if m.classType != "" {
		parts = append(parts, m.classType+" class")
	}
	if m.band != "" {
		parts = append(parts, "Level "+m.band)
	}
	return strings.Join(parts, ", ")
}

// startCron launches the background scheduler. It wakes up daily at 00:05
// local time and, on days 1–7 of the month, ensures every active student
// has a Monthly invoice for the current month. Idempotent: skips students
// that already have one. The 7-day window gives the cron room to catch up
// if a deploy or outage caused the 1st to be missed.
func StartCron(ctx context.Context, wg *sync.WaitGroup, db *store.DB) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Run once shortly after boot in case the server was down at the
		// scheduled time.
		runMonthlyInvoiceCycle(db)

		for {
			next := nextRunAt(time.Now())
			select {
			case <-ctx.Done():
				core.Logger.Info("monthly cron stopped")
				return
			case <-time.After(time.Until(next)):
				runMonthlyInvoiceCycle(db)
			}
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

func runMonthlyInvoiceCycle(db *store.DB) {
	now := time.Now()
	if now.Day() > 7 {
		return
	}
	created := generateMonthlyInvoices(db, now)
	if created > 0 {
		core.Logger.Info("monthly invoice cron created invoices", "count", created, "month", now.Format("2006-01"))
	}
	overflow := generateSelfStudyOverflowInvoices(db, now)
	if overflow > 0 {
		core.Logger.Info("self-study overflow invoices created", "count", overflow, "month", previousMonth(now).Format("2006-01"))
	}
	payrolls := generateMonthlyPayroll(db, now)
	if payrolls > 0 {
		core.Logger.Info("monthly payroll cron created rows", "count", payrolls, "month", previousMonth(now).Format("2006-01"))
	}
}

// generateSelfStudyOverflowInvoices bills students for self-study hours used
// in the previous month beyond their package_self_study_hours quota. Hours
// are summed from self_study_sessions.duration_min (rounded UP to whole
// hours so a 61-minute session counts as 2). Idempotent: skips students that
// already have a "Self-study overflow — <YYYY-MM>" invoice for the period.
func generateSelfStudyOverflowInvoices(db *store.DB, now time.Time) int {
	prev := previousMonth(now)
	monthHuman := prev.Format("Jan 2006")
	monthStart := prev.Format("2006-01-02")
	monthEnd := time.Date(prev.Year(), prev.Month()+1, 1, 0, 0, 0, 0, prev.Location()).AddDate(0, 0, -1).Format("2006-01-02")

	// Pre-load the set of (tenant_id|student_id) pairs that already have an
	// overflow invoice for the billing period, so the per-student dedup check
	// in the loop is a map lookup instead of a SELECT COUNT(*) round-trip.
	// The period is matched on the human month in the description (e.g.
	// "Jan 2006") — NOT created_on, because overflow bills the PREVIOUS month
	// but is issued in the current one, so created_on is the current date. The
	// cron runs daily during days 1–7, so without this an unpaid student would
	// be billed once per run (up to 7 duplicate invoices/month).
	existingOverflow := map[string]bool{}
	if existRows, err := db.Query(`SELECT tenant_id, student_id FROM invoices WHERE type='Self-study Overflow' AND description LIKE ? AND deleted_at IS NULL`, "%"+monthHuman+"%"); err == nil {
		for existRows.Next() {
			var t, s string
			if err := existRows.Scan(&t, &s); err == nil {
				existingOverflow[t+"|"+s] = true
			}
		}
		existRows.Close()
	}

	// Same idea for the leftover-minutes that get banked as self-study credits:
	// the note carries the billing month so a re-run during the 1–7 window
	// doesn't bank the same rollover twice. A student with only a partial hour
	// over quota has no invoice to dedup against, so this guard is essential.
	existingRollover := map[string]bool{}
	if rcRows, err := db.Query(`SELECT tenant_id, student_id FROM replacement_credits WHERE category='self-study' AND type='earned' AND note LIKE ?`, "%"+rolloverNote(monthHuman)+"%"); err == nil {
		for rcRows.Next() {
			var t, s string
			if err := rcRows.Scan(&t, &s); err == nil {
				existingRollover[t+"|"+s] = true
			}
		}
		rcRows.Close()
	}

	rows, err := db.Query(`
		SELECT s.id, s.tenant_id, s.first_name, s.last_name,
		       COALESCE(s.package_self_study_hours,4),
		       COALESCE(SUM(ss.duration_min),0) AS used_min
		  FROM students s
		  LEFT JOIN self_study_sessions ss ON ss.student_id = s.id AND ss.deleted_at IS NULL AND ss.date BETWEEN ? AND ?
		 WHERE s.deleted_at IS NULL
		   AND COALESCE(s.subscription_status,'active') = 'active'
		   AND COALESCE(s.status,'Active') NOT IN ('Inactive','Waitlisted')
		 GROUP BY s.id, s.tenant_id, s.first_name, s.last_name, s.package_self_study_hours
	`, monthStart, monthEnd)
	if err != nil {
		core.Logger.Error("self-study overflow query failed", "err", err)
		return 0
	}
	defer rows.Close()

	created := 0
	createdOn := now.Format("2006-01-02")
	dueDate := time.Date(now.Year(), now.Month(), 7, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	for rows.Next() {
		var stuID, tid, firstName, lastName string
		var quotaHours, usedMin int
		if err := rows.Scan(&stuID, &tid, &firstName, &lastName, &quotaHours, &usedMin); err != nil {
			continue
		}
		// Minutes used beyond the free quota. Always billed in whole hours at
		// RM10/hr, rounding UP (no fractional charge). The unused slice of that
		// rounded-up hour is credited back to the student's self-study ledger so
		// they don't lose what they paid for. e.g. 45m over → bill 1 hr (RM10),
		// credit 15m; 1h30m over → bill 2 hr (RM20), credit 30m.
		overflowMin := usedMin - quotaHours*60
		if overflowMin <= 0 {
			continue
		}
		billHours := overflowMin / 60
		if overflowMin%60 != 0 {
			billHours++
		}
		creditMin := billHours*60 - overflowMin

		if !existingOverflow[tid+"|"+stuID] {
			amount := float64(billHours) * SelfStudyOverflowRatePerHour
			desc := "Self-study overflow — " + monthHuman + " — " + firstName + " " + lastName +
				" (" + fmt.Sprintf("%d", billHours) + " hr over quota)"
			overflowItems := []models.InvoiceLineItem{{
				Kind: models.LineItemKindItem, Name: "Self-study overflow — " + monthHuman,
				Descriptor:  fmt.Sprintf("%d hours over the included quota", billHours),
				PeriodStart: monthStart, PeriodEnd: monthEnd,
				Qty: float64(billHours), UnitPrice: SelfStudyOverflowRatePerHour, Amount: amount,
			}}
			invID := core.GenerateID("INV")
			if _, err := db.Exec(`INSERT INTO invoices(id,tenant_id,student_id,description,type,amount,due_date,status,created_on,paid_on,payment_method,discount_pct,submitted_by_parent,sibling_ids,sibling_discount,referral_credit,reference_no,line_items) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				invID, tid, stuID, desc, "Self-study Overflow", amount, dueDate, "Unpaid", createdOn, nil, "", 0.0, false, "[]", 0.0, 0.0, "", models.MarshalLineItems(overflowItems)); err != nil {
				core.Logger.Error("self-study overflow insert failed", "err", err, "student_id", stuID)
				continue
			}
			created++
		}

		// Credit back the unused slice of the rounded-up hour (1 credit = 15 min,
		// stored as raw minutes). Skipped if already banked for this month.
		if creditMin > 0 && !existingRollover[tid+"|"+stuID] {
			note := rolloverNote(monthHuman)
			rcID := core.GenerateID("RC")
			if _, err := db.Exec(`INSERT INTO replacement_credits(id,tenant_id,student_id,type,minutes,note,class_id,date,created_by,category) VALUES(?,?,?,?,?,?,?,?,?,?)`,
				rcID, tid, stuID, "earned", creditMin, note, "", createdOn, "system", "self-study"); err != nil {
				core.Logger.Error("self-study rollover credit insert failed", "err", err, "student_id", stuID)
			}
		}
	}
	return created
}

// rolloverNote is the marker stored on a banked self-study credit so the cron
// can tell, on a later same-month run, that this student's leftover minutes
// were already banked (preventing double-banking during the 1–7 day window).
func rolloverNote(monthHuman string) string {
	return "Self-study rollover — " + monthHuman
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
func generateMonthlyInvoices(db *store.DB, now time.Time) int {
	monthPrefix := now.Format("2006-01")

	// Key by (tenant_id, student_id) so a colliding STU_<ts> across tenants
	// (unlikely under millisecond timestamps but not impossible) cannot
	// suppress a real invoice for the other tenant.
	existing := loadExistingMonthlyInvoiceStudentIDs(db, monthPrefix)

	// Pre-load classID → monthly fee, derived from the type×level pricing matrix
	// (a class joins pricing_tiers on its class_type + level_band). Class IDs are
	// unique, so a flat map keyed by class id is enough (cron spans all tenants).
	classByID := map[string]classMeta{}
	if frows, ferr := db.Query(`SELECT c.id, COALESCE(pt.monthly_fee,0), COALESCE(c.name,''), COALESCE(c.class_type,''), COALESCE(c.level_band,'') FROM classes c LEFT JOIN pricing_tiers pt ON pt.class_type = c.class_type AND pt.level_band = c.level_band AND pt.tenant_id = c.tenant_id AND pt.deleted_at IS NULL WHERE c.deleted_at IS NULL`); ferr == nil {
		for frows.Next() {
			var cid string
			var m classMeta
			if err := frows.Scan(&cid, &m.fee, &m.name, &m.classType, &m.band); err == nil {
				classByID[cid] = m
			}
		}
		frows.Close()
	}

	rows, err := db.Query(`
		SELECT s.id, s.tenant_id, s.first_name, s.last_name, s.family_id, s.package_amount,
		       COALESCE(s.contact,''), COALESCE(s.parent_name,''), COALESCE(s.enrolled_classes,'[]'),
		       COALESCE(s.package_self_study_hours,4)
		FROM students s
		WHERE s.deleted_at IS NULL
		  AND COALESCE(s.subscription_status,'active') = 'active'
		  AND COALESCE(s.status,'Active') NOT IN ('Inactive','Waitlisted')
	`)
	if err != nil {
		core.Logger.Error("monthly invoice cron query failed", "err", err)
		return 0
	}
	type row struct {
		id, tenantID, firstName, lastName, familyID string
		packageAmount                               float64
		contact, parentName, enrolledClasses        string
		selfStudyHours                              int
	}
	var pending []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.tenantID, &r.firstName, &r.lastName, &r.familyID, &r.packageAmount, &r.contact, &r.parentName, &r.enrolledClasses, &r.selfStudyHours); err != nil {
			continue
		}
		if existing[r.tenantID+"|"+r.id] {
			continue
		}
		pending = append(pending, r)
	}
	rows.Close()
	if len(pending) == 0 {
		return 0
	}

	// Build the (tenantID, familyID) pair list so loadFamilyReferralCredits
	// can scope each lookup to the right tenant.
	pairSet := map[string]bool{}
	pairs := []familyTenantPair{}
	for _, s := range pending {
		if s.familyID == "" {
			continue
		}
		k := s.tenantID + "|" + s.familyID
		if pairSet[k] {
			continue
		}
		pairSet[k] = true
		pairs = append(pairs, familyTenantPair{tenantID: s.tenantID, familyID: s.familyID})
	}
	familyCredits := loadFamilyReferralCredits(db, pairs)

	// Build family → [sibling student IDs] map. The 2+-kids threshold is
	// based on the WHOLE family (any billable student counts: active
	// subscription and lifecycle status not Inactive/Waitlisted),
	// not just kids with package_amount > 0 — a family with one paying
	// kid and one free trial kid still qualifies for the sibling discount.
	// Query the full family roster up front to avoid the case where the
	// `pending` set is filtered by package_amount.
	siblingsByFamily := map[string][]string{}
	familyIDsForCount := uniqueFamilyIDs(pending, func(i int) string { return pending[i].familyID })
	if len(familyIDsForCount) > 0 {
		fph := make([]string, len(familyIDsForCount))
		fargs := make([]any, len(familyIDsForCount))
		for i, id := range familyIDsForCount {
			fph[i] = "?"
			fargs[i] = id
		}
		rows, err := db.Query(`SELECT id, family_id FROM students WHERE deleted_at IS NULL AND COALESCE(subscription_status,'active')='active' AND COALESCE(status,'Active') NOT IN ('Inactive','Waitlisted') AND family_id IN (`+strings.Join(fph, ",")+`)`, fargs...)
		if err == nil {
			for rows.Next() {
				var sid, fid string
				if err := rows.Scan(&sid, &fid); err == nil && fid != "" {
					siblingsByFamily[fid] = append(siblingsByFamily[fid], sid)
				}
			}
			rows.Close()
		}
	}

	tx, err := db.BeginTx(context.Background())
	if err != nil {
		core.Logger.Error("monthly invoice cron tx begin failed", "err", err)
		return 0
	}

	// Issued on the 1st (catch-up window to the 7th), due the 7th. The
	// early-bird discount is kept only if paid by the cutoff (= due date);
	// applyEarlyBirdExpiry restores full price on unpaid invoices afterwards.
	dueDate := time.Date(now.Year(), now.Month(), 7, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	earlyBirdCutoff := dueDate
	createdOn := now.Format("2006-01-02")
	monthLabel := now.Format("Jan 2006")
	// Billing period shown on each line item: the 1st to the last day of the month.
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	periodEnd := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location()).AddDate(0, 0, -1).Format("2006-01-02")

	// Invoice emails are queued AFTER commit so a rollback never emails a
	// parent about an invoice that doesn't exist.
	type issuedEmail struct {
		tid                            int
		to, parentName, studentName    string
		amount, dueDate, earlyBirdNote string
	}
	var emails []issuedEmail

	created := 0
	for _, s := range pending {
		// Base tuition: a manual package_amount (>0) overrides; otherwise sum
		// the subject fees of the student's enrolled classes. Each priced class
		// becomes its own invoice line item; the manual override is one line.
		base := s.packageAmount
		var items []models.InvoiceLineItem
		if base > 0 {
			items = append(items, models.InvoiceLineItem{
				Kind: models.LineItemKindItem, Name: "Monthly tuition — " + monthLabel,
				PeriodStart: periodStart, PeriodEnd: periodEnd,
				Qty: 1, UnitPrice: base, Amount: base,
			})
		} else {
			for _, cid := range models.ParseArr(s.enrolledClasses) {
				m := classByID[cid]
				if m.fee <= 0 {
					continue
				}
				base += m.fee
				items = append(items, models.InvoiceLineItem{
					Kind: models.LineItemKindItem, Name: monthlyClassLineName(m),
					Descriptor: monthlyClassDescriptor(m), PeriodStart: periodStart, PeriodEnd: periodEnd,
					Qty: 1, UnitPrice: m.fee, Amount: m.fee,
				})
			}
		}
		if base <= 0 {
			continue // no classes priced and no manual amount — nothing to bill
		}

		referralCredit := 0.0
		creditKey := s.tenantID + "|" + s.familyID
		if s.familyID != "" && familyCredits[creditKey] > 0 {
			referralCredit = ReferralMonthlyRM
		}
		// Sibling discount applies when the family has 2+ active subscribed
		// students this month. Each child gets RM10 off — flat per child.
		siblingDiscount := 0.0
		siblingIDsJSON := "[]"
		if sibs := siblingsByFamily[s.familyID]; len(sibs) >= 2 {
			siblingDiscount = SiblingMonthlyRM
			// Store the other siblings' IDs so the frontend can render
			// "shared family: X, Y" without re-querying.
			others := make([]string, 0, len(sibs)-1)
			for _, id := range sibs {
				if id != s.id {
					others = append(others, id)
				}
			}
			siblingIDsJSON = models.JSONArr(others)
		}
		// Full price (after sibling/referral); early-bird is applied now but
		// clawed back later if unpaid past the cutoff. earlyBirdApplied is the
		// exact RM removed, so the clawback restores precisely the full price.
		full := base - referralCredit - siblingDiscount
		if full < 0 {
			full = 0
		}
		discounted := full - EarlyBirdRM
		if discounted < 0 {
			discounted = 0
		}
		earlyBirdApplied := full - discounted

		// Included self-study: shown as a membership line then fully waived by a
		// matching FOC discount, so it nets to zero and never moves the total.
		if s.selfStudyHours > 0 {
			ssValue := float64(s.selfStudyHours) * SelfStudyOverflowRatePerHour
			items = append(items, models.InvoiceLineItem{
				Kind: models.LineItemKindItem, Name: "TSH Membership",
				Descriptor:  fmt.Sprintf("%d self-study hours included", s.selfStudyHours),
				PeriodStart: periodStart, PeriodEnd: periodEnd,
				Qty: 1, UnitPrice: ssValue, Amount: ssValue,
			})
			items = appendDiscount(items, "Special pass FOC (self-study included)", ssValue)
		}
		items = appendDiscount(items, "Referral discount", referralCredit)
		items = appendDiscount(items, "Sibling discount", siblingDiscount)
		items = appendDiscount(items, "Early bird discount", earlyBirdApplied)

		invID := core.GenerateID("INV")
		desc := "Monthly tuition — " + monthLabel + " — " + s.firstName + " " + s.lastName

		_, err := tx.Exec(`
			INSERT INTO invoices(id,tenant_id,student_id,description,type,amount,due_date,status,created_on,paid_on,payment_method,discount_pct,submitted_by_parent,sibling_ids,sibling_discount,referral_credit,reference_no,early_bird_cutoff,early_bird_discount,line_items)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			invID, s.tenantID, s.id, desc, "Monthly", discounted, dueDate, "Unpaid",
			createdOn, nil, "", 0.0, false, siblingIDsJSON, siblingDiscount, referralCredit, "",
			earlyBirdCutoff, earlyBirdApplied, models.MarshalLineItems(items),
		)
		if err != nil {
			core.Logger.Error("could not insert monthly invoice", "err", err, "student_id", s.id)
			continue
		}
		if s.contact != "" {
			note := ""
			if earlyBirdApplied > 0 {
				note = fmt.Sprintf("Pay by %s to keep the RM%.0f early-bird discount (RM%.2f after).", dueDate, earlyBirdApplied, full)
			}
			tid := 1
			if n, convErr := strconv.Atoi(s.tenantID); convErr == nil {
				tid = n
			}
			emails = append(emails, issuedEmail{
				tid: tid, to: s.contact, parentName: s.parentName,
				studentName: s.firstName + " " + s.lastName,
				amount:      fmt.Sprintf("%.2f", discounted), dueDate: dueDate, earlyBirdNote: note,
			})
		}
		if referralCredit > 0 && s.familyID != "" {
			// Decrement the oldest earned referral_rewards row that still has
			// credits remaining. The CASE flips status to 'exhausted' when
			// the row hits zero so future cron runs skip it. Tenant-scoped
			// to keep an id collision across tenants from settling the wrong row.
			if _, err := tx.Exec(`
				UPDATE referral_rewards
				   SET credits_remaining = credits_remaining - 1,
				       status = CASE WHEN credits_remaining - 1 <= 0 THEN 'exhausted' ELSE 'earned' END
				 WHERE id = (
				   SELECT id FROM referral_rewards
				    WHERE referrer_family_id=? AND tenant_id=? AND status='earned' AND credits_remaining > 0
				    ORDER BY created_at ASC LIMIT 1
				 )`, s.familyID, s.tenantID); err != nil {
				core.Logger.Error("could not decrement referral credit", "err", err, "family_id", s.familyID)
			}
			familyCredits[creditKey]--
		}
		created++
	}

	if err := tx.Commit(); err != nil {
		core.Logger.Error("monthly invoice cron tx commit failed", "err", err)
		return 0
	}
	for _, e := range emails {
		body := mailer.RenderInvoiceIssuedEmail(e.parentName, e.studentName, "Monthly tuition — "+monthLabel, e.amount, e.dueDate, e.earlyBirdNote)
		if _, err := store.QueueEmail(db, e.tid, e.to, "Invoice for "+monthLabel+" — "+e.studentName, body); err != nil {
			core.Logger.Error("could not queue invoice email", "err", err, "to", e.to)
		}
	}
	if created > 0 {
		store.SnapshotCacheInvalidateAll()
	}
	return created
}

// loadExistingMonthlyInvoiceStudentIDs returns the set of (tenant_id|student_id)
// pairs that already have a Monthly invoice for the given YYYY-MM prefix.
func loadExistingMonthlyInvoiceStudentIDs(db *store.DB, monthPrefix string) map[string]bool {
	out := map[string]bool{}
	rows, err := db.Query(`
		SELECT DISTINCT tenant_id, student_id FROM invoices
		WHERE type='Monthly' AND created_on LIKE ? AND deleted_at IS NULL`,
		monthPrefix+"%")
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var tid, sid string
		if err := rows.Scan(&tid, &sid); err == nil {
			out[tid+"|"+sid] = true
		}
	}
	return out
}

// loadFamilyReferralCredits returns a map of family_id → remaining credits
// for the requested (familyID, tenantID) pairs. The source of truth is the
// per-referral referral_rewards.credits_remaining counter; the previous
// families.referral_credits_remaining column was never incremented anywhere
// and is intentionally ignored here. Empty input returns an empty map
// without running a query.
//
// The result is keyed by "tenantID|familyID" so a cross-tenant id collision
// (millisecond timestamp clash on FAM_ ids) cannot bleed credits between
// tenants. Callers compose the lookup key with the student's tenant.
func loadFamilyReferralCredits(db *store.DB, pairs []familyTenantPair) map[string]int {
	out := map[string]int{}
	if len(pairs) == 0 {
		return out
	}
	// Group by tenant so we can issue one IN-list per tenant.
	byTenant := map[string][]string{}
	for _, p := range pairs {
		byTenant[p.tenantID] = append(byTenant[p.tenantID], p.familyID)
	}
	for tid, fids := range byTenant {
		placeholders := make([]string, len(fids))
		args := make([]any, 0, len(fids)+1)
		args = append(args, tid)
		for i, id := range fids {
			placeholders[i] = "?"
			args = append(args, id)
		}
		q := `SELECT referrer_family_id, COALESCE(SUM(credits_remaining),0)
		      FROM referral_rewards
		      WHERE status='earned' AND tenant_id=? AND referrer_family_id IN (` + strings.Join(placeholders, ",") + `)
		      GROUP BY referrer_family_id`
		rows, err := db.Query(q, args...)
		if err != nil {
			continue
		}
		for rows.Next() {
			var fid string
			var rem int
			if err := rows.Scan(&fid, &rem); err == nil {
				out[tid+"|"+fid] = rem
			}
		}
		rows.Close()
	}
	return out
}

type familyTenantPair struct {
	tenantID string
	familyID string
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
func HandleRunMonthlyCron(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		actor := c.Email
		// Skip overlapping runs. A session-level advisory lock is bound to ONE
		// connection, so we hold a dedicated *sql.Conn for the lock's lifetime —
		// acquiring via the pool (db.QueryRow) could acquire and release on
		// different pooled connections, so a second concurrent click would NOT
		// see the lock and would run a duplicate batch. A concurrent request
		// gets a different connection, so its pg_try_advisory_lock returns false
		// and we 409 it. Closing the connection releases the lock even if the
		// process dies mid-run.
		lockKey := core.AdvisoryLockKey("monthly_cron")
		ctx := context.Background()
		conn, err := db.DB.Conn(ctx)
		if err != nil {
			core.RespondError(w, "server busy, try again", http.StatusServiceUnavailable)
			return
		}
		var got bool
		if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, lockKey).Scan(&got); err != nil {
			conn.Close()
			core.RespondError(w, "server error", http.StatusInternalServerError)
			return
		}
		if !got {
			conn.Close()
			core.RespondError(w, "monthly cron is already running — wait for it to finish", http.StatusConflict)
			return
		}
		go func() {
			defer conn.Close()
			defer conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, lockKey)
			now := time.Now()
			invoices := generateMonthlyInvoices(db, now)
			overflow := generateSelfStudyOverflowInvoices(db, now)
			payrolls := generateMonthlyPayroll(db, now)
			core.Logger.Info("monthly cron manual run finished", "actor", actor, "invoices", invoices, "overflow", overflow, "payrolls", payrolls)
			core.LogAudit(db, actor, "monthly_cron_manual_run", "system", "", "")
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
func generateMonthlyPayroll(db *store.DB, now time.Time) int {
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
		core.Logger.Error("monthly payroll cron query failed", "err", err)
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

	// Pre-load (staff_id, month, tenant_id) triples that already have a
	// payroll row → map lookup instead of N×SELECT COUNT(*).
	existingPayroll := map[string]bool{}
	if er, err := db.Query(`SELECT staff_id, tenant_id FROM payroll WHERE month=?`, monthLabel); err == nil {
		for er.Next() {
			var sid, tid string
			if err := er.Scan(&sid, &tid); err == nil {
				existingPayroll[sid+"|"+tid] = true
			}
		}
		er.Close()
	}

	// Pre-compute hours for every part-time staff member in one grouped
	// query — replaces N×JOIN against attendance+classes.
	partTimeIDs := []string{}
	for _, s := range staff {
		if s.employmentType == "Part-time" && s.hourlyRate > 0 {
			partTimeIDs = append(partTimeIDs, s.id)
		}
	}
	hoursByStaff := teacherHoursWorkedAll(db, partTimeIDs, monthStart, monthEnd)

	created := 0
	for _, s := range staff {
		if existingPayroll[s.id+"|"+s.tenantID] {
			continue
		}

		var total float64
		switch s.employmentType {
		case "Part-time":
			if s.hourlyRate <= 0 {
				continue
			}
			hours := hoursByStaff[s.id]
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

		id := core.GenerateID("PAY")
		_, err := db.Exec(`
			INSERT INTO payroll(id,tenant_id,staff_id,month,base_salary,bonus,deductions,total,status,paid_on)
			VALUES(?,?,?,?,?,?,?,?,?,?)`,
			id, s.tenantID, s.id, monthLabel, total, 0.0, 0.0, total, "Pending", nil,
		)
		if err != nil {
			core.Logger.Error("could not insert payroll row", "err", err, "staff_id", s.id)
			continue
		}
		created++
	}
	return created
}

// teacherHoursWorkedAll computes hours-worked for every staff_id in the
// given list with one grouped query. The previous per-staff variant ran
// 50× SELECT … JOIN against attendance, blowing payroll runtime as the
// staff count grows. This collapses to a single query that groups by
// person_id. Empty input returns an empty map without a round-trip.
func teacherHoursWorkedAll(db *store.DB, staffIDs []string, start, end string) map[string]float64 {
	out := map[string]float64{}
	if len(staffIDs) == 0 {
		return out
	}
	placeholders := make([]string, len(staffIDs))
	args := make([]any, 0, len(staffIDs)+2)
	for i, id := range staffIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, start, end)
	rows, err := db.Query(`
		SELECT a.person_id, a.check_in, a.check_out, c.time, c.end_time
		FROM attendance a
		LEFT JOIN classes c ON c.id = a.class_id
		WHERE a.person_id IN (`+strings.Join(placeholders, ",")+`)
		  AND a.person_type IN ('teacher','staff')
		  AND a.check_in IS NOT NULL AND a.check_in <> ''
		  AND a.date BETWEEN ? AND ?
	`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var personID string
		var checkIn, checkOut, classTime, classEnd sql.NullString
		if err := rows.Scan(&personID, &checkIn, &checkOut, &classTime, &classEnd); err != nil {
			continue
		}
		if checkOut.Valid && checkOut.String != "" {
			if h := hoursBetween(checkIn.String, checkOut.String); h > 0 {
				out[personID] += h
				continue
			}
		}
		if classTime.Valid && classEnd.Valid {
			if h := hoursBetween(classTime.String, classEnd.String); h > 0 {
				out[personID] += h
			}
		}
	}
	return out
}

// teacherHoursWorked sums hours from teacher attendance check-in rows in
// the [start, end] window. Falls back to the scheduled class duration
// (end_time − time) when check_out is missing — covers the case where a
// teacher checked in but never tapped checkout.
//
// Accepts both person_type='teacher' and person_type='staff' because the
// frontend self-check-in flow writes 'staff' while older bulk admin paths
// wrote 'teacher'. Both refer to a row in the staff table.
func teacherHoursWorked(db *store.DB, staffID, start, end string) float64 {
	rows, err := db.Query(`
		SELECT a.check_in, a.check_out, c.time, c.end_time
		FROM attendance a
		LEFT JOIN classes c ON c.id = a.class_id
		WHERE a.person_id = ? AND a.person_type IN ('teacher','staff')
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

// hoursBetween parses two HH:MM or HH:MM:SS strings and returns the
// elapsed hours. Accepts both formats because browser <input type="time">
// can render either depending on user agent + step attribute. Tolerates
// zero-padding and silently returns 0 on bad input rather than crashing
// the cron.
func hoursBetween(start, end string) float64 {
	parse := func(v string) (time.Time, error) {
		if t, err := time.Parse("15:04:05", v); err == nil {
			return t, nil
		}
		return time.Parse("15:04", v)
	}
	s, errS := parse(start)
	e, errE := parse(end)
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
func HandleRegeneratePayroll(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		monthArg := r.URL.Query().Get("month")
		var target time.Time
		if monthArg == "" {
			target = time.Now()
		} else {
			t, err := time.Parse("2006-01", monthArg)
			if err != nil {
				core.RespondError(w, "month must be YYYY-MM", 400)
				return
			}
			// generateMonthlyPayroll uses previousMonth(now), so pass the
			// next month to compute payroll for the requested month.
			target = t.AddDate(0, 1, 0)
		}
		actor := c.Email
		// Serialise concurrent regenerations so two clicks can't both pre-load
		// the dedup set before either commits and create duplicate payroll rows.
		// Held on a dedicated connection for the lock's lifetime (see
		// handleRunMonthlyCron for why the pool can't be used directly).
		lockKey := core.AdvisoryLockKey("regenerate_payroll")
		ctx := context.Background()
		conn, err := db.DB.Conn(ctx)
		if err != nil {
			core.RespondError(w, "server busy, try again", http.StatusServiceUnavailable)
			return
		}
		var got bool
		if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, lockKey).Scan(&got); err != nil {
			conn.Close()
			core.RespondError(w, "server error", http.StatusInternalServerError)
			return
		}
		if !got {
			conn.Close()
			core.RespondError(w, "payroll regeneration is already running — wait for it to finish", http.StatusConflict)
			return
		}
		go func() {
			defer conn.Close()
			defer conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, lockKey)
			count := generateMonthlyPayroll(db, target)
			core.Logger.Info("payroll regenerated", "actor", actor, "month", monthArg, "created", count)
			core.LogAudit(db, actor, "payroll_regenerated", "system", monthArg, "")
		}()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"status":"started"}`))
	}
}
