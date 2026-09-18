package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"studyhub/internal/core"
	"studyhub/internal/jobs"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

// The month Nadine actually runs: draft every billable student, look at the
// list, fix what is wrong, then issue the lot.
//
// The review step exists because rating can be correct and still wrong for a
// student -- someone quit but the enrolment was never ended, a make-up lesson
// needs adding. Catching that BEFORE the document becomes immutable is the
// whole point of the draft state, and it is why drafting no longer emails
// anyone: a draft tells a parent nothing.

type monthRunStudent struct {
	InvoiceID   string  `json:"invoiceId"`
	StudentID   string  `json:"studentId"`
	StudentName string  `json:"studentName"`
	Status      string  `json:"status"`
	InvoiceNo   string  `json:"invoiceNo"`
	Amount      float64 `json:"amount"`
}

type monthRunProblem struct {
	StudentID   string `json:"studentId"`
	StudentName string `json:"studentName"`
	Reason      string `json:"reason"`
}

type monthRun struct {
	Month       string            `json:"month"`
	Drafts      []monthRunStudent `json:"drafts"`
	Issued      []monthRunStudent `json:"issued"`
	Problems    []monthRunProblem `json:"problems"`
	DraftTotal  float64           `json:"draftTotal"`
	IssuedTotal float64           `json:"issuedTotal"`
}

// draftClock keys the whole run: due date, billing period, month label and
// whether the early bird applies all derive from it. Drafting the CURRENT month
// uses the real clock, so a run on the 2nd grants the early bird and a run on
// the 20th does not. Drafting any other month is a late run by definition --
// full price, no early bird -- rather than back-dating a discount whose cutoff
// has already passed.
func draftClock(month string) time.Time {
	now := time.Now()
	if now.Format("2006-01") == month {
		return now
	}
	t, err := time.ParseInLocation("2006-01-02", month+"-08", now.Location())
	if err != nil {
		return now
	}
	return t
}

func monthFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	month := r.URL.Query().Get("month")
	if month == "" {
		var body struct {
			Month string `json:"month"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		month = body.Month
	}
	if !monthPattern.MatchString(month) {
		core.RespondError(w, "month must be YYYY-MM", http.StatusBadRequest)
		return "", false
	}
	return month, true
}

// HandleMonthRun lists what the month currently holds: drafts to review, what
// has already been issued, and the students the catalogue cannot price -- named
// rather than silently missing, which is the whole reason the list exists.
func HandleMonthRun(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		month, ok := monthFromRequest(w, r)
		if !ok {
			return
		}
		out := monthRun{Month: month, Drafts: []monthRunStudent{}, Issued: []monthRunStudent{}, Problems: []monthRunProblem{}}

		tw, twArgs := store.ScopeTenant(c, "i")
		args := append([]any{month}, twArgs...)
		rows, err := db.Query(`SELECT i.id, i.student_id,
			COALESCE(s.first_name,'')||' '||COALESCE(s.last_name,''), i.status,
			COALESCE(i.invoice_no,''), i.amount
			FROM invoices i LEFT JOIN students s ON s.id = i.student_id AND s.tenant_id = i.tenant_id
			WHERE i.type='Monthly' AND i.period=? AND i.deleted_at IS NULL`+tw+`
			ORDER BY 3`, args...)
		if err != nil {
			core.LogFromReq(r).Error("month run list failed", "err", err)
			core.RespondError(w, "could not read the month", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var m monthRunStudent
			if rows.Scan(&m.InvoiceID, &m.StudentID, &m.StudentName, &m.Status, &m.InvoiceNo, &m.Amount) != nil {
				continue
			}
			if m.Status == models.InvoiceStatusDraft {
				out.Drafts = append(out.Drafts, m)
				out.DraftTotal += m.Amount
				continue
			}
			out.Issued = append(out.Issued, m)
			out.IssuedTotal += m.Amount
		}

		// Priced mid-month, as every other caller does, so the review screen and
		// the run itself agree about which enrolments were in force.
		for _, sp := range store.CatalogPrices(db, c, month+"-15") {
			if !sp.Unpriceable {
				continue
			}
			_, problems := sp.InvoiceLines("", "")
			for _, p := range problems {
				out.Problems = append(out.Problems, monthRunProblem{
					StudentID: sp.StudentID, StudentName: sp.StudentName, Reason: p,
				})
			}
		}
		core.Respond(w, out)
	}
}

// HandleMonthDraft drafts every billable student for the month. Safe to run
// twice: a student who already has a Monthly invoice for the period is skipped,
// so this tops up after an enrolment changes rather than duplicating.
func HandleMonthDraft(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		month, ok := monthFromRequest(w, r)
		if !ok {
			return
		}
		drafted := jobs.RunMonthlyInvoices(db, draftClock(month))
		core.LogAudit(db, store.TenantID(c), c.Email, "monthly_drafted", "invoice", month,
			"drafts: "+itoa(drafted))
		store.SnapshotCacheInvalidateAll()
		core.Respond(w, map[string]any{"month": month, "drafted": drafted})
	}
}

// HandleMonthIssue issues every draft in the month. Each draft gets its own
// transaction, as #5 requires: one student whose issue fails must not strand
// the other forty, and a number allocated inside a rolled-back transaction
// comes back with it.
func HandleMonthIssue(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		month, ok := monthFromRequest(w, r)
		if !ok {
			return
		}
		tw, twArgs := store.ScopeTenant(c, "")
		args := append([]any{month, models.InvoiceStatusDraft}, twArgs...)
		rows, err := db.Query(`SELECT id, tenant_id FROM invoices
			WHERE type='Monthly' AND period=? AND status=? AND deleted_at IS NULL`+tw+`
			ORDER BY id`, args...)
		if err != nil {
			core.RespondError(w, "could not read the drafts", http.StatusInternalServerError)
			return
		}
		type draft struct {
			id       string
			tenantID int
		}
		drafts := []draft{}
		for rows.Next() {
			var d draft
			if rows.Scan(&d.id, &d.tenantID) == nil {
				drafts = append(drafts, d)
			}
		}
		rows.Close()

		issued, failed := 0, 0
		for _, d := range drafts {
			if issueOneDraft(r, db, c, d.id, d.tenantID) {
				issued++
				continue
			}
			failed++
		}
		core.LogAudit(db, store.TenantID(c), c.Email, "monthly_issued", "invoice", month,
			"issued: "+itoa(issued)+", failed: "+itoa(failed))
		store.SnapshotCacheInvalidateAll()
		core.Respond(w, map[string]any{"month": month, "issued": issued, "failed": failed})
	}
}

// issueOneDraft finalises a single draft and queues the parent's email in the
// SAME transaction, via the outbox. Sending inside the transaction is how the
// email goes out for an invoice that then rolls back.
func issueOneDraft(r *http.Request, db *store.DB, c *core.Claims, id string, tenantID int) bool {
	tx, err := db.BeginTx(r.Context())
	if err != nil {
		return false
	}
	defer tx.Rollback()

	number, err := store.IssueInvoice(tx, c, id, models.InvoiceStatusUnpaid)
	if err != nil || number == "" {
		core.LogFromReq(r).Error("issue failed", "err", err, "invoice_id", id)
		return false
	}
	if err := store.EnqueueOutbox(tx, tenantID, store.OutboxTopicInvoiceIssued, id); err != nil {
		core.LogFromReq(r).Error("outbox enqueue failed", "err", err, "invoice_id", id)
		return false
	}
	if err := tx.Commit(); err != nil {
		return false
	}
	core.LogAudit(db, tenantID, c.Email, "invoice_issued", "invoice", id, number)
	return true
}
