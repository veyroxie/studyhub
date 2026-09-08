package handlers

import (
	"testing"

	"studyhub/internal/core"
	"studyhub/internal/store"
)

// TestCatalogPrices locks the resolver the switchover differ and, later, the
// monthly cron both run. Two cases here are the expensive ones: billing a
// twice-weekly student twice, and inventing a price for a combination nobody
// has agreed one for.
func TestCatalogPrices(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	var tenantID int
	if err := db.QueryRow(`SELECT tenant_id FROM users WHERE email=?`, "admin@studyhub.com").Scan(&tenantID); err != nil {
		t.Fatalf("seed admin missing: %v", err)
	}
	claims := &core.Claims{Email: "admin@studyhub.com", Role: "admin", TenantID: tenantID}

	mkClass := func(name, cat, tier string, override float64) string {
		id := core.GenerateID("CLS")
		if _, err := db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,classroom,pricing_category_id,default_tier_name,monthly_fee_override)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, id, tenantID, name, "Monday", "16:00", "17:00", "R", cat, tier, override); err != nil {
			t.Fatalf("class %s: %v", name, err)
		}
		return id
	}
	mkStudent := func(first string, pkg float64, classIDs ...string) string {
		id := core.GenerateID("STU")
		if _, err := db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,contact,status,package_amount)
			VALUES(?,?,?,?,?,?,?)`, id, tenantID, first, "Test", first+"@example.com", "Active", pkg); err != nil {
			t.Fatalf("student %s: %v", first, err)
		}
		for _, cid := range classIDs {
			if _, err := db.Exec(`INSERT INTO enrollments(id,tenant_id,student_id,class_id,started_on,created_by,created_on)
				VALUES(?,?,?,?,?,?,?)`, core.GenerateID("ENR"), tenantID, id, cid, "2026-08-01", "test", "2026-08-01"); err != nil {
				t.Fatalf("enrol %s: %v", first, err)
			}
		}
		return id
	}
	priceOf := func(all []store.StudentPrice, id string) store.StudentPrice {
		for _, p := range all {
			if p.StudentID == id {
				return p
			}
		}
		t.Fatalf("no price computed for %s", id)
		return store.StudentPrice{}
	}

	// Seeded catalogue (0051): Group Level 3-4 is 260 at 1x, 490 at 2x.
	g34a := mkClass("Level 3 & 4 Tue", "PC_group", "Level 3-4", 0)
	g34b := mkClass("Level 3 & 4 Thu", "PC_group", "Level 3-4", 0)
	g12 := mkClass("Level 1 & 2", "PC_group", "Level 1-2", 0)
	selfStudy := mkClass("Self-Study", "PC_selfstudy", "Beyond included hours", 0)
	phonics := mkClass("Phonics", "PC_group", "", 239.96)
	noTier := mkClass("Mandarin", "PC_group", "", 0)

	once := mkStudent("Once", 0, g34a)
	twice := mkStudent("Twice", 0, g34a, g34b)
	mixed := mkStudent("Mixed", 0, g34a, g12)
	packaged := mkStudent("Packaged", 360, g34a, g34b)
	credit := mkStudent("Credit", 0, selfStudy)
	overridden := mkStudent("Overridden", 0, phonics)
	untiered := mkStudent("Untiered", 0, noTier)

	all := store.CatalogPrices(db, claims, "")

	if p := priceOf(all, once); p.Total != 260 || p.Unpriceable {
		t.Fatalf("once weekly: want 260 priceable, got %v unpriceable=%v", p.Total, p.Unpriceable)
	}

	// THE expensive one. Two slots in one category is a twice-weekly student on
	// ONE price of 490 -- not two lots of 260, and not two lots of 490.
	if p := priceOf(all, twice); p.Total != 490 {
		t.Fatalf("twice weekly must bill 490 once, got %v", p.Total)
	}

	// Section 11: two levels in one category has no agreed price. Summing the
	// frequency tiers would produce 940 with total confidence.
	p := priceOf(all, mixed)
	if !p.Unpriceable {
		t.Fatalf("mixed levels must be flagged, got total %v", p.Total)
	}
	if p.Total != 0 {
		t.Fatalf("a flagged student must not carry an invented total, got %v", p.Total)
	}

	if p := priceOf(all, packaged); p.Total != 360 {
		t.Fatalf("package short-circuits: want 360, got %v", p.Total)
	}
	if p := priceOf(all, credit); p.Total != 0 || p.Unpriceable {
		t.Fatalf("credit-covered: want 0 priceable, got %v unpriceable=%v", p.Total, p.Unpriceable)
	}
	if p := priceOf(all, overridden); p.Total != 239.96 {
		t.Fatalf("class rate: want 239.96, got %v", p.Total)
	}
	if p := priceOf(all, untiered); !p.Unpriceable || p.Total != 0 {
		t.Fatalf("no tier must be flagged at 0, got %v unpriceable=%v", p.Total, p.Unpriceable)
	}

	// The standing discount is a LINE, not a smaller total. Five students were
	// invoiced below the catalogue with every discount column at zero, so the
	// switchover would have raised their bills with nothing to explain it.
	if _, err := db.Exec(`UPDATE students SET standing_discount=10, standing_discount_reason='Goodwill' WHERE id=?`, once); err != nil {
		t.Fatalf("set discount: %v", err)
	}
	p2 := priceOf(store.CatalogPrices(db, claims, ""), once)
	if p2.Total != 250 {
		t.Fatalf("260 less a 10 discount is 250, got %v", p2.Total)
	}
	var found bool
	for _, l := range p2.Lines {
		if l.Source == store.SourceDiscount {
			found = true
			if l.Amount != -10 {
				t.Fatalf("the discount line must be negative 10, got %v", l.Amount)
			}
			if l.ClassName != "Goodwill" {
				t.Fatalf("the line must carry the reason, got %q", l.ClassName)
			}
		}
	}
	if !found {
		t.Fatal("a discount must appear as its own line, not vanish into the total")
	}

	// A student who cannot be priced gets no discount line: subtracting from a
	// total that does not exist would invent a negative bill and imply the
	// pricing resolved when it did not.
	if _, err := db.Exec(`UPDATE students SET standing_discount=10, standing_discount_reason='Goodwill' WHERE id=?`, untiered); err != nil {
		t.Fatalf("set discount on untiered: %v", err)
	}
	if p3 := priceOf(store.CatalogPrices(db, claims, ""), untiered); p3.Total != 0 || !p3.Unpriceable {
		t.Fatalf("unpriceable student must stay 0 and flagged, got %v flagged=%v", p3.Total, p3.Unpriceable)
	}

	// Clear it again: the as-of assertions below reuse this student and expect
	// the undiscounted 260.
	if _, err := db.Exec(`UPDATE students SET standing_discount=0, standing_discount_reason='' WHERE id=?`, once); err != nil {
		t.Fatalf("clear discount: %v", err)
	}

	// As-of matters for the differ, which compares PAST months. Using today's
	// enrolments to price August priced two students who had since left at 0
	// against a real August invoice, reading as a mispricing rather than as
	// "they were still enrolled then". Half-open, so the leaving day is out.
	if _, err := db.Exec(`UPDATE enrollments SET ended_on='2026-08-20' WHERE student_id=?`, once); err != nil {
		t.Fatalf("end enrolment: %v", err)
	}
	if p := priceOf(store.CatalogPrices(db, claims, "2026-08-15"), once); p.Total != 260 {
		t.Fatalf("as-of before the end date must still price: want 260, got %v", p.Total)
	}
	if p := priceOf(store.CatalogPrices(db, claims, "2026-08-20"), once); p.Total != 0 || len(p.Lines) != 0 {
		t.Fatalf("the day they leave is not counted: want 0 with no lines, got %v", p.Total)
	}
	if p := priceOf(store.CatalogPrices(db, claims, ""), once); p.Total != 0 {
		t.Fatalf("live-now must not see an ended enrolment, got %v", p.Total)
	}
}
