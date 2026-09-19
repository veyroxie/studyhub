package handlers

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/models"
)

// The monthly scheduler spans every tenant by design. The draft ENDPOINT must
// not: an admin of one centre drafting a month must not create invoices for
// another centre's students. generateMonthlyInvoices selected students with no
// tenant filter, which was correct for the cron and wrong the moment it sat
// behind an admin route -- and CLAUDE.md calls a missing tenant filter a
// data-integrity bug, not a style problem.
//
// The second tenant is given a real catalogue on purpose. Without one its
// student is unpriceable and would be skipped anyway, so the test would pass
// whether or not the scoping existed.
func TestDraftingAMonthStopsAtTheAdminsOwnTenant(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	r := chi.NewRouter()
	r.Post("/api/auth/login", auth.HandleLogin(db))
	r.Group(func(g chi.Router) {
		g.Use(auth.JWTMiddleware(db))
		g.Post("/api/billing/month/draft", HandleMonthDraft(db))
	})
	token := getToken(t, r, "admin@studyhub.com", "admin123")

	const month = "2026-03"
	const otherTenant = 2
	catID := core.GenerateID("PC")
	classID := core.GenerateID("CLS")
	studentID := core.GenerateID("STU")
	// Cleaned by TENANT, not by the ids generated in this run: those are new
	// every time, so an id-keyed wipe leaves the previous run's rows behind and
	// this synthetic second tenant accumulates across the suite.
	wipe := func() {
		db.Exec(`DELETE FROM applied_discounts WHERE invoice_id IN (SELECT id FROM invoices WHERE period=?)`, month)
		db.Exec(`DELETE FROM outbox`)
		db.Exec(`DELETE FROM invoices WHERE period=? OR tenant_id=?`, month, otherTenant)
		db.Exec(`DELETE FROM enrollments WHERE tenant_id=?`, otherTenant)
		db.Exec(`DELETE FROM students WHERE tenant_id=?`, otherTenant)
		db.Exec(`DELETE FROM classes WHERE tenant_id=?`, otherTenant)
		db.Exec(`DELETE FROM pricing_plans WHERE tenant_id=?`, otherTenant)
		db.Exec(`DELETE FROM pricing_categories WHERE tenant_id=?`, otherTenant)
	}
	wipe()
	t.Cleanup(wipe)

	// A second centre, fully priceable.
	if _, err := db.Exec(`INSERT INTO pricing_categories(id,tenant_id,name,credit_covered,sort_order)
		VALUES(?,?,?,?,?)`, catID, otherTenant, "Group", false, 1); err != nil {
		t.Fatalf("seed other tenant category: %v", err)
	}
	db.Exec(`INSERT INTO pricing_plans(id,tenant_id,category_id,tier_name,sessions_per_week,monthly_fee,sort_order,effective_from)
		VALUES(?,?,?,?,1,?,0,DATE '2000-01-01')`, core.GenerateID("PP"), otherTenant, catID, "Level 1-2", 300)
	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,classroom,class_type,pricing_category_id,default_tier_name,monthly_fee_override)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		classID, otherTenant, "Other Centre Group", "Monday", "16:00", "17:00", "R1", "Group", catID, "Level 1-2", 0)
	db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,contact,status,subscription_status,package_amount,package_self_study_hours,family_id,enrolled_classes,registered_on)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		studentID, otherTenant, "Other", "Centre", "", "Active", "active", 0, 0, "",
		models.JSONArr([]string{classID}), "2026-01-01")
	db.Exec(`INSERT INTO enrollments(id,tenant_id,student_id,class_id,started_on,tier_name,created_by,created_on)
		VALUES(?,?,?,?,?,?,?,?)`,
		core.GenerateID("ENR"), otherTenant, studentID, classID, "2026-01-01", "", "test", "2026-01-01")

	// A tenant-1 admin drafts the month.
	w := authedJSON(t, r, "POST", "/api/billing/month/draft", token, map[string]any{"month": month})
	if w.Code != http.StatusOK {
		t.Fatalf("draft: %d %s", w.Code, w.Body.String())
	}

	var leaked int
	db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE student_id=? AND period=? AND deleted_at IS NULL`,
		studentID, month).Scan(&leaked)
	if leaked != 0 {
		t.Errorf("a tenant-1 admin drafted %d invoice(s) for a tenant-%d student", leaked, otherTenant)
	}
	var otherTenantRows int
	db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE tenant_id=? AND period=? AND deleted_at IS NULL`,
		otherTenant, month).Scan(&otherTenantRows)
	if otherTenantRows != 0 {
		t.Errorf("%d invoice(s) written against tenant %d", otherTenantRows, otherTenant)
	}
}
