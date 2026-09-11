package handlers

import (
	"testing"

	"studyhub/internal/core"
	"studyhub/internal/store"
)

// Each test owns its own tier so neither touches the shared seeded catalogue.
// The first version of this file had the history test close a seeded plan and
// never reopen it, which left the overlap test asserting against an adjacent
// window rather than an overlapping one -- it passed for the wrong reason.
const historyTier = "History Test Tier"

func seedPlanVersion(t *testing.T, db *store.DB, tenantID int, tier string, fee float64, from, to string) string {
	t.Helper()
	id := core.GenerateID("PP")
	var err error
	if to == "" {
		_, err = db.Exec(`INSERT INTO pricing_plans(id,tenant_id,category_id,tier_name,sessions_per_week,monthly_fee,sort_order,effective_from)
			VALUES(?,?,?,?,1,?,0,?::date)`, id, tenantID, "PC_group", tier, fee, from)
	} else {
		_, err = db.Exec(`INSERT INTO pricing_plans(id,tenant_id,category_id,tier_name,sessions_per_week,monthly_fee,sort_order,effective_from,effective_to)
			VALUES(?,?,?,?,1,?,0,?::date,?::date)`, id, tenantID, "PC_group", tier, fee, from, to)
	}
	if err != nil {
		t.Fatalf("seed plan version [%s,%s): %v", from, to, err)
	}
	// setupFeatureTestApp does not clear pricing_plans, and the 0066 exclusion
	// constraint makes leftovers collide with the next run rather than merely
	// clutter -- so a second `go test` would fail on state, not on code.
	t.Cleanup(func() { db.Exec(`DELETE FROM pricing_plans WHERE id=?`, id) })
	return id
}

// Before 0066 the catalogue held one row per plan, so re-rating an earlier
// month applied whatever the price is today. The differ compares past months
// and the monthly run can be re-executed, so both read the wrong number and
// said nothing.
func TestPriceHistoryRatesEachMonthAtItsOwnPrice(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	var tenantID int
	if err := db.QueryRow(`SELECT tenant_id FROM users WHERE email=?`, "admin@studyhub.com").Scan(&tenantID); err != nil {
		t.Fatalf("seed admin missing: %v", err)
	}
	claims := &core.Claims{Email: "admin@studyhub.com", Role: "admin", TenantID: tenantID}

	// Two adjacent versions: 200 until 1 September, 255 from it.
	seedPlanVersion(t, db, tenantID, historyTier, 200, "2000-01-01", "2026-09-01")
	seedPlanVersion(t, db, tenantID, historyTier, 255, "2026-09-01", "")

	classID := core.GenerateID("CLS")
	if _, err := db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,classroom,pricing_category_id,default_tier_name,monthly_fee_override)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, classID, tenantID, "History Test", "Monday", "16:00", "17:00", "R", "PC_group", historyTier, 0); err != nil {
		t.Fatalf("class: %v", err)
	}
	studentID := core.GenerateID("STU")
	if _, err := db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,contact,status,package_amount)
		VALUES(?,?,?,?,?,?,?)`, studentID, tenantID, "Hist", "Test", "hist@example.com", "Active", 0); err != nil {
		t.Fatalf("student: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO enrollments(id,tenant_id,student_id,class_id,started_on,created_by,created_on)
		VALUES(?,?,?,?,?,?,?)`, core.GenerateID("ENR"), tenantID, studentID, classID, "2026-01-01", "test", "2026-01-01"); err != nil {
		t.Fatalf("enrol: %v", err)
	}

	priceOn := func(asOf string) float64 {
		for _, p := range store.CatalogPrices(db, claims, asOf) {
			if p.StudentID == studentID {
				return p.Total
			}
		}
		t.Fatalf("no price computed as of %s", asOf)
		return 0
	}

	cases := []struct {
		asOf string
		want float64
		why  string
	}{
		{"2026-08-15", 200, "mid-August rates at the version in force then"},
		{"2026-08-31", 200, "the last day of a version still rates at it"},
		{"2026-09-01", 255, "half-open: the successor starts the day the old one ends"},
		{"2026-09-15", 255, "mid-September rates at the new version"},
	}
	for _, tc := range cases {
		if got := priceOn(tc.asOf); got != tc.want {
			t.Errorf("%s: rated %.2f as of %s, want %.2f", tc.why, got, tc.asOf, tc.want)
		}
	}
}

// The database refuses two versions of one plan claiming the same day. Without
// it a mistimed price change silently gives a class two prices and whichever
// row the query happens to return wins.
func TestOverlappingPriceVersionsAreRejected(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	var tenantID int
	db.QueryRow(`SELECT tenant_id FROM users WHERE email=?`, "admin@studyhub.com").Scan(&tenantID)

	const tier = "Overlap Test Tier"
	seedPlanVersion(t, db, tenantID, tier, 200, "2026-01-01", "")

	// Open-ended already, so a successor that does not close it first overlaps.
	_, err := db.Exec(`INSERT INTO pricing_plans(id,tenant_id,category_id,tier_name,sessions_per_week,monthly_fee,sort_order,effective_from)
		VALUES(?,?,?,?,1,?,0,DATE '2026-09-01')`, core.GenerateID("PP"), tenantID, "PC_group", tier, 999)
	if err == nil {
		t.Fatal("an overlapping price version was accepted — the class now has two prices on the same day")
	}
	if !isDuplicate(err) {
		t.Errorf("overlap reported as %v, which the catalogue handler would return as a 500 rather than a 409", err)
	}

	// Adjacent is not overlapping: closing the first lets the second in.
	if _, err := db.Exec(`UPDATE pricing_plans SET effective_to=DATE '2026-09-01' WHERE tenant_id=? AND category_id='PC_group' AND tier_name=? AND effective_to IS NULL`,
		tenantID, tier); err != nil {
		t.Fatalf("close the live version: %v", err)
	}
	adjacentID := core.GenerateID("PP")
	t.Cleanup(func() { db.Exec(`DELETE FROM pricing_plans WHERE id=?`, adjacentID) })
	if _, err := db.Exec(`INSERT INTO pricing_plans(id,tenant_id,category_id,tier_name,sessions_per_week,monthly_fee,sort_order,effective_from)
		VALUES(?,?,?,?,1,?,0,DATE '2026-09-01')`, adjacentID, tenantID, "PC_group", tier, 999); err != nil {
		t.Errorf("an adjacent version was refused: %v", err)
	}
}
