// Command billingcheck proves that the monthly cron ISSUES what the catalogue
// QUOTES, on real data, before anything is deployed.
//
// pricecheck answers "what would the catalogue charge", which is a question
// about the resolver. This answers the different question the switchover
// actually turns on: when the cron runs, does the invoice it writes match? A
// wrong discount column, a dropped sibling id or a student silently skipped are
// all invisible to a totals comparison.
//
// It RUNS THE CRON, so it writes invoices. Point it only at the throwaway copy
// that scripts/migration-dryrun.sh restores -- never at production. It refuses
// to start against the production host for that reason.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"studyhub/internal/core"
	"studyhub/internal/jobs"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

// dryRunDatabase is the throwaway copy scripts/migration-dryrun.sh restores
// production into. Keep the two in step.
const dryRunDatabase = "studyhub_dryrun"

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set")
	}
	// The first thing this tool does is DELETE a month of invoices, so it
	// allowlists the one database it may touch rather than denylisting hosts:
	// blocking the droplet still left the local dev database wide open, and
	// that holds real data too.
	if !strings.Contains(dsn, dryRunDatabase) {
		log.Fatalf("billingcheck issues invoices and deletes the month first: it runs only against the %q copy that scripts/migration-dryrun.sh creates", dryRunDatabase)
	}
	monthFlag := flag.String("month", "", "month to issue and check, YYYY-MM")
	flag.Parse()
	month := *monthFlag
	if len(month) != 7 {
		log.Fatal("pass -month=YYYY-MM")
	}
	on, err := time.Parse("2006-01-02", month+"-01")
	if err != nil {
		log.Fatalf("bad month: %v", err)
	}

	core.InitLogger()
	db := store.InitDB(dsn)
	defer db.Close()
	claims := &core.Claims{Email: "billingcheck", Role: "admin", TenantID: 1}

	// Start from a clean month so the run actually issues rather than being
	// deduped against invoices the copy already carries.
	if _, err := db.Exec(`DELETE FROM invoices WHERE type='Monthly' AND period=?`, month); err != nil {
		log.Fatalf("clear the month on the copy: %v", err)
	}

	drafted := jobs.RunMonthlyInvoices(db, on)

	// The run drafts; issuing is a separate, explicit act. Do both here, so the
	// check covers the whole path a month actually takes -- including the
	// numbering transition, which no production invoice has ever been through.
	issued, failedIssue := issueEveryDraft(db, month)

	type inv struct {
		amount, earlyBird, sibling, referral float64
	}
	got := map[string]inv{}
	unnumbered := 0
	rows, err := db.Query(`SELECT student_id, amount, COALESCE(early_bird_discount,0),
		COALESCE(sibling_discount,0), COALESCE(referral_credit,0), COALESCE(invoice_no,'')
		FROM invoices WHERE deleted_at IS NULL AND type='Monthly' AND period=?`, month)
	if err != nil {
		log.Fatalf("read issued invoices: %v", err)
	}
	for rows.Next() {
		var id, number string
		var v inv
		if rows.Scan(&id, &v.amount, &v.earlyBird, &v.sibling, &v.referral, &number) == nil {
			got[id] = v
			if number == "" {
				unnumbered++
			}
		}
	}
	rows.Close()

	// Why the cron declined a student. Without this the "not billed" list is
	// mostly frozen students the cron is RIGHT to skip, and the one student who
	// should have been billed is buried in it.
	declined := map[string]string{}
	drows, derr := db.Query(`SELECT id, COALESCE(NULLIF(subscription_status,''),'active'),
		COALESCE(NULLIF(status,''),'Active') FROM students WHERE deleted_at IS NULL`)
	if derr != nil {
		log.Fatalf("read student status: %v", derr)
	}
	for drows.Next() {
		var id, sub, st string
		if drows.Scan(&id, &sub, &st) != nil {
			continue
		}
		switch {
		case sub != "active":
			declined[id] = sub
		case st == "Inactive" || st == "Waitlisted":
			declined[id] = strings.ToLower(st)
		}
	}
	drows.Close()

	type line struct{ name, note string }
	var mismatched, matched, skipped int
	var bad, unbilled []line

	for _, sp := range store.CatalogPrices(db, claims, month+"-15") {
		v, has := got[sp.StudentID]
		if !has {
			// The catalogue prices everyone; the cron deliberately bills only
			// active, unfrozen students. A student it declined for a recorded
			// reason is correct behaviour, not a finding.
			if why, ok := declined[sp.StudentID]; ok {
				skipped++
				_ = why
				continue
			}
			if sp.Total > 0 && !sp.Unpriceable {
				unbilled = append(unbilled, line{sp.StudentName, fmt.Sprintf("catalogue %.2f, active, but the cron issued nothing", sp.Total)})
			}
			skipped++
			continue
		}
		// The identity that must hold: what the parent owes, plus every
		// discount the cron recorded, is what the catalogue quoted.
		gross := math.Round((v.amount+v.earlyBird+v.sibling+v.referral)*100) / 100
		want := math.Round(sp.Total*100) / 100
		if gross != want {
			mismatched++
			bad = append(bad, line{sp.StudentName, fmt.Sprintf(
				"invoice %.2f + discounts %.2f = %.2f, catalogue %.2f",
				v.amount, v.earlyBird+v.sibling+v.referral, gross, want)})
			continue
		}
		matched++
	}

	sort.Slice(bad, func(i, j int) bool { return bad[i].name < bad[j].name })
	sort.Slice(unbilled, func(i, j int) bool { return unbilled[i].name < unbilled[j].name })

	fmt.Printf("  the run drafted %d and issued %d invoice(s) for %s\n\n", drafted, issued, month)
	if failedIssue > 0 {
		fmt.Printf("  %d draft(s) could not be issued\n", failedIssue)
	}
	for _, l := range bad {
		fmt.Printf("  %-30s MISMATCH  %s\n", l.name, l.note)
	}
	for _, l := range unbilled {
		fmt.Printf("  %-30s not billed  %s\n", l.name, l.note)
	}
	fmt.Printf("\n  %s: %d issued invoices match the catalogue, %d do not, %d active students were missed (%d frozen or inactive, correctly skipped)\n",
		month, matched, mismatched, len(unbilled), skipped-len(unbilled))
	if unnumbered > 0 {
		fmt.Printf("  %d ISSUED invoice(s) carry no number.\n", unnumbered)
		os.Exit(1)
	}
	if len(unbilled) > 0 {
		fmt.Printf("  %d ACTIVE student(s) the catalogue can price were not billed.\n", len(unbilled))
		os.Exit(1)
	}
	if mismatched > 0 {
		fmt.Printf("  CRON DISAGREES WITH THE CATALOGUE on %d student(s).\n", mismatched)
		os.Exit(1)
	}
	fmt.Println("  CRON AGREES: every invoice it issued is what the catalogue quotes.")
}

// issueEveryDraft finalises the month exactly as the review screen does: one
// transaction per draft, each writing its outbox row, so one failure cannot
// strand the rest and a number allocated inside a rolled-back transaction comes
// back with it.
func issueEveryDraft(db *store.DB, month string) (int, int) {
	rows, err := db.Query(`SELECT id, tenant_id FROM invoices
		WHERE type='Monthly' AND period=? AND status=? AND deleted_at IS NULL ORDER BY id`,
		month, models.InvoiceStatusDraft)
	if err != nil {
		log.Fatalf("read drafts: %v", err)
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
		claims := &core.Claims{Email: "billingcheck", Role: "admin", TenantID: d.tenantID}
		tx, err := db.BeginTx(context.Background())
		if err != nil {
			failed++
			continue
		}
		// Exactly what the review screen does, settle included -- otherwise this
		// check exercises a path production does not have.
		if err := store.SettleEarlyBirdOnIssue(tx, d.tenantID, d.id, core.Today()); err != nil {
			tx.Rollback()
			failed++
			continue
		}
		number, err := store.IssueInvoice(tx, claims, d.id, models.InvoiceStatusUnpaid)
		if err != nil || number == "" {
			tx.Rollback()
			failed++
			continue
		}
		if err := store.EnqueueOutbox(tx, d.tenantID, store.OutboxTopicInvoiceIssued, d.id); err != nil {
			tx.Rollback()
			failed++
			continue
		}
		if err := tx.Commit(); err != nil {
			failed++
			continue
		}
		issued++
	}
	return issued, failed
}
