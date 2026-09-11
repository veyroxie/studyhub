package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/models"
	"studyhub/internal/store"
)

// fmtSscan pulls the sequence number out of INV-YYYY-NNNN.
func fmtSscan(number string, n *int) (int, error) {
	parts := strings.Split(number, "-")
	if len(parts) != 3 {
		return 0, fmt.Errorf("not INV-YYYY-NNNN: %q", number)
	}
	return fmt.Sscanf(parts[2], "%d", n)
}

func createdInvoice(t *testing.T, r *chi.Mux, token, studentID, createdOn string) models.Invoice {
	t.Helper()
	inv := models.Invoice{StudentID: studentID, Description: "Numbering test", Type: "Adhoc",
		Amount: 100, DueDate: "2026-12-31", CreatedOn: createdOn}
	w := doRequest(r, "POST", "/api/invoices", token, inv)
	if w.Code != http.StatusOK {
		t.Fatalf("create invoice: %d %s", w.Code, w.Body.String())
	}
	var out models.Invoice
	json.NewDecoder(w.Body).Decode(&out)
	return out
}

// An invoice is issued with a number or it does not exist. The number is
// gapless and resets each year (ADR-016).
func TestIssuedInvoicesAreNumberedConsecutively(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())
	db.Exec(`DELETE FROM invoice_number_counters`)

	first := createdInvoice(t, r, token, "STU001", "2026-09-01")
	second := createdInvoice(t, r, token, "STU002", "2026-09-01")

	if first.InvoiceNo == "" || second.InvoiceNo == "" {
		t.Fatalf("an issued invoice has no number: %q then %q", first.InvoiceNo, second.InvoiceNo)
	}
	if !strings.HasPrefix(first.InvoiceNo, "INV-") {
		t.Errorf("number %q does not look like INV-YYYY-NNNN", first.InvoiceNo)
	}
	if first.InvoiceNo == second.InvoiceNo {
		t.Fatalf("two invoices share the number %q", first.InvoiceNo)
	}
	// Consecutive, with nothing skipped between them.
	var a, b int
	if _, err := fmtSscan(first.InvoiceNo, &a); err != nil {
		t.Fatalf("unparseable number %q: %v", first.InvoiceNo, err)
	}
	if _, err := fmtSscan(second.InvoiceNo, &b); err != nil {
		t.Fatalf("unparseable number %q: %v", second.InvoiceNo, err)
	}
	if b != a+1 {
		t.Errorf("numbers %d then %d leave a gap", a, b)
	}

	// And the row really carries it, not just the response body.
	var stored, issuedAt, status string
	if err := db.QueryRow(`SELECT COALESCE(invoice_no,''), COALESCE(issued_at,''), status FROM invoices WHERE id=?`, first.ID).
		Scan(&stored, &issuedAt, &status); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored != first.InvoiceNo {
		t.Errorf("stored number %q, response said %q", stored, first.InvoiceNo)
	}
	if issuedAt == "" {
		t.Error("issued invoice has no issue date")
	}
	if status == models.InvoiceStatusDraft {
		t.Error("invoice was left as a draft — create still issues immediately")
	}
}

// The reason this is a counter row and not a Postgres sequence: nextval is
// never rolled back, so a failed issue burns a number permanently and the
// ledger grows a hole nobody can account for. An UPDATE rolls back with its
// transaction.
//
// An earlier version of this test tried to force the rollback through a
// duplicate invoice id, which fails at the INSERT -- before any number is
// allocated -- so it passed without touching the behaviour it claimed to check.
func TestARolledBackIssueReturnsItsNumber(t *testing.T) {
	_, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	db.Exec(`DELETE FROM invoice_number_counters`)

	const tenantID = 1
	when := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)

	tx, err := db.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	abandoned, err := store.NextInvoiceNumber(tx, tenantID, when)
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	tx.Rollback()

	tx2, err := db.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("begin again: %v", err)
	}
	reused, err := store.NextInvoiceNumber(tx2, tenantID, when)
	if err != nil {
		t.Fatalf("allocate again: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if reused != abandoned {
		t.Errorf("abandoned %s then issued %s — the rolled back number was not returned, so the sequence has a hole", abandoned, reused)
	}
}

// Numbers restart at 1 each year rather than running on.
func TestNumberingResetsEachYear(t *testing.T) {
	_, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	db.Exec(`DELETE FROM invoice_number_counters`)

	alloc := func(year int) string {
		tx, err := db.BeginTx(context.Background())
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		n, err := store.NextInvoiceNumber(tx, 1, time.Date(year, 3, 1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("allocate %d: %v", year, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
		return n
	}
	if got := alloc(2026); got != "INV-2026-0001" {
		t.Errorf("first of 2026 is %q, want INV-2026-0001", got)
	}
	if got := alloc(2026); got != "INV-2026-0002" {
		t.Errorf("second of 2026 is %q, want INV-2026-0002", got)
	}
	if got := alloc(2027); got != "INV-2027-0001" {
		t.Errorf("first of 2027 is %q, want INV-2027-0001 — the counter did not reset", got)
	}
}
