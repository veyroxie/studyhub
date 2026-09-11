package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/models"
)

// The builder asks the catalogue instead of carrying its own price list.
func TestProposedInvoiceComesFromTheCatalogue(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	r := chi.NewRouter()
	r.Post("/api/auth/login", auth.HandleLogin(db))
	r.Group(func(g chi.Router) {
		g.Use(auth.JWTMiddleware(db))
		g.Get("/api/billing/proposed-invoice", HandleProposedInvoice(db))
	})
	tok := getToken(t, r, "admin@studyhub.com", "admin123")

	var tenantID int
	db.QueryRow(`SELECT tenant_id FROM users WHERE email=?`, "admin@studyhub.com").Scan(&tenantID)

	// Two live enrolments in one category: the case the old dropdown could not
	// express at all, because it had no sessions-per-week dimension.
	classA, classB := core.GenerateID("CLS"), core.GenerateID("CLS")
	for _, id := range []string{classA, classB} {
		if _, err := db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,classroom,pricing_category_id,default_tier_name,monthly_fee_override)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, id, tenantID, "Twice "+id[len(id)-4:], "Monday", "16:00", "17:00", "R", "PC_group", "Level 3-4", 0); err != nil {
			t.Fatalf("class: %v", err)
		}
	}
	studentID := core.GenerateID("STU")
	if _, err := db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,contact,status,package_amount)
		VALUES(?,?,?,?,?,?,?)`, studentID, tenantID, "Twice", "Weekly", "twice@example.com", "Active", 0); err != nil {
		t.Fatalf("student: %v", err)
	}
	for _, cid := range []string{classA, classB} {
		if _, err := db.Exec(`INSERT INTO enrollments(id,tenant_id,student_id,class_id,started_on,created_by,created_on)
			VALUES(?,?,?,?,?,?,?)`, core.GenerateID("ENR"), tenantID, studentID, cid, "2026-08-01", "test", "2026-08-01"); err != nil {
			t.Fatalf("enrol: %v", err)
		}
	}

	w := authedJSON(t, r, "GET", "/api/billing/proposed-invoice?studentId="+studentID+"&month=2026-09", tok, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("proposed invoice: %d %s", w.Code, w.Body.String())
	}
	var got ProposedInvoice
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Unpriceable {
		t.Fatalf("priced student came back unpriceable: %v", got.Problems)
	}
	// Seeded catalogue: Group Level 3-4 is 490 at 2x. One line, not two at 260.
	if len(got.Lines) != 1 {
		t.Fatalf("got %d lines, want 1 for one category: %+v", len(got.Lines), got.Lines)
	}
	if got.Total != 490 {
		t.Errorf("total %.2f, want 490 — the twice-weekly tier, not two single-session charges", got.Total)
	}
	if got.Lines[0].Kind != models.LineItemKindItem {
		t.Errorf("line kind %q, want item", got.Lines[0].Kind)
	}
	if got.Lines[0].Descriptor == "" {
		t.Error("line has no descriptor, so the figure cannot be checked by eye")
	}
}

// An unpriceable class must never reach an invoice as a silent zero. It comes
// back as a problem the builder has to show, not as a line it can save.
func TestProposedInvoiceReportsUnpriceableAsAProblemNotALine(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	r := chi.NewRouter()
	r.Post("/api/auth/login", auth.HandleLogin(db))
	r.Group(func(g chi.Router) {
		g.Use(auth.JWTMiddleware(db))
		g.Get("/api/billing/proposed-invoice", HandleProposedInvoice(db))
	})
	tok := getToken(t, r, "admin@studyhub.com", "admin123")

	var tenantID int
	db.QueryRow(`SELECT tenant_id FROM users WHERE email=?`, "admin@studyhub.com").Scan(&tenantID)

	classID := core.GenerateID("CLS")
	if _, err := db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,classroom,pricing_category_id,default_tier_name,monthly_fee_override)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, classID, tenantID, "No Tier Class", "Monday", "16:00", "17:00", "R", "PC_group", "", 0); err != nil {
		t.Fatalf("class: %v", err)
	}
	studentID := core.GenerateID("STU")
	if _, err := db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,contact,status,package_amount)
		VALUES(?,?,?,?,?,?,?)`, studentID, tenantID, "No", "Tier", "notier@example.com", "Active", 0); err != nil {
		t.Fatalf("student: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO enrollments(id,tenant_id,student_id,class_id,started_on,created_by,created_on)
		VALUES(?,?,?,?,?,?,?)`, core.GenerateID("ENR"), tenantID, studentID, classID, "2026-08-01", "test", "2026-08-01"); err != nil {
		t.Fatalf("enrol: %v", err)
	}

	w := authedJSON(t, r, "GET", "/api/billing/proposed-invoice?studentId="+studentID+"&month=2026-09", tok, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("proposed invoice: %d %s", w.Code, w.Body.String())
	}
	var got ProposedInvoice
	json.NewDecoder(w.Body).Decode(&got)

	if !got.Unpriceable {
		t.Error("a class with no tier was priced")
	}
	if len(got.Problems) == 0 {
		t.Error("no problem reported, so the builder has nothing to show and would save a wrong invoice")
	}
	for _, l := range got.Lines {
		if l.Amount == 0 {
			t.Errorf("an unpriceable class reached the lines as a zero: %+v", l)
		}
	}
}

// A malformed month reaches CatalogPrices as an asOf and then a ?::date cast,
// so it has to be refused up front rather than becoming an empty result.
func TestProposedInvoiceRejectsAMalformedMonth(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	r := chi.NewRouter()
	r.Post("/api/auth/login", auth.HandleLogin(db))
	r.Group(func(g chi.Router) {
		g.Use(auth.JWTMiddleware(db))
		g.Get("/api/billing/proposed-invoice", HandleProposedInvoice(db))
	})
	tok := getToken(t, r, "admin@studyhub.com", "admin123")

	for _, bad := range []string{"2026-13", "abcdefg", "2026-9", ""} {
		w := authedJSON(t, r, "GET", "/api/billing/proposed-invoice?studentId=STU001&month="+bad, tok, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("month %q returned %d, want 400", bad, w.Code)
		}
	}
}
