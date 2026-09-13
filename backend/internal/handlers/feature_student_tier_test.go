package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

// A student's price should come from the catalogue, not from a discount typed
// to make up a difference. Before this, the student form offered a hardcoded
// "level band" of 1-3 or 4-6 -- the retired pricing_tiers banding -- which the
// rating engine does not read at all, so choosing it changed nothing.
func TestStudentTierPricesFromTheCatalogue(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	var tenantID int
	db.QueryRow(`SELECT tenant_id FROM users WHERE email=?`, "admin@studyhub.com").Scan(&tenantID)

	// A cheaper tier in the same category, as "Level 3" would be.
	planID := core.GenerateID("PP")
	if _, err := db.Exec(`INSERT INTO pricing_plans(id,tenant_id,category_id,tier_name,sessions_per_week,monthly_fee,sort_order,effective_from)
		VALUES(?,?,?,?,1,?,0,DATE '2000-01-01')`, planID, tenantID, "PC_group", "Level 3 Only", 200); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM pricing_plans WHERE id=?`, planID) })

	classID := core.GenerateID("CLS")
	if _, err := db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,classroom,pricing_category_id,default_tier_name,monthly_fee_override)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, classID, tenantID, "Mixed 3 and 4", "Monday", "16:00", "17:00", "R", "PC_group", "Level 3-4", 0); err != nil {
		t.Fatalf("class: %v", err)
	}

	stu := models.Student{FirstName: "Tier", LastName: "Test", Contact: "tier@example.com",
		Status: "Active", EnrolledClasses: []string{classID}}
	w := doRequest(r, "POST", "/api/students", token, stu)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("create student: %d %s", w.Code, w.Body.String())
	}
	var made models.Student
	json.NewDecoder(w.Body).Decode(&made)
	t.Cleanup(func() { db.Exec(`DELETE FROM students WHERE id=?`, made.ID) })

	claims := &core.Claims{Email: "admin@studyhub.com", Role: "admin", TenantID: tenantID}
	priceOf := func() float64 {
		for _, sp := range store.CatalogPrices(db, claims, "") {
			if sp.StudentID == made.ID {
				return sp.Total
			}
		}
		t.Fatal("student not priced")
		return 0
	}

	// Class default: the mixed Level 3-4 tier.
	if got := priceOf(); got != 260 {
		t.Fatalf("class default priced at %.2f, want the seeded 260", got)
	}

	made.PricingTier = "Level 3 Only"
	if w := doRequest(r, "PUT", "/api/students/"+made.ID, token, made); w.Code != http.StatusOK {
		t.Fatalf("set tier: %d %s", w.Code, w.Body.String())
	}
	if got := priceOf(); got != 200 {
		t.Errorf("after choosing a tier the student is priced at %.2f, want 200 — the choice did nothing", got)
	}

	// And it reads back, or the form would always show "same as class".
	lw := doRequest(r, "GET", "/api/students", token, nil)
	var list []models.Student
	json.NewDecoder(lw.Body).Decode(&list)
	for _, s := range list {
		if s.ID == made.ID && s.PricingTier != "Level 3 Only" {
			t.Errorf("tier reads back as %q, want %q", s.PricingTier, "Level 3 Only")
		}
	}

	// Clearing it falls back to the class default rather than becoming unpriceable.
	made.PricingTier = ""
	if w := doRequest(r, "PUT", "/api/students/"+made.ID, token, made); w.Code != http.StatusOK {
		t.Fatalf("clear tier: %d", w.Code)
	}
	if got := priceOf(); got != 260 {
		t.Errorf("after clearing the tier the student is priced at %.2f, want the class default 260", got)
	}
}

// A student has one level but may sit in two categories. "Level 3" means
// nothing in Mandarin, and writing it onto that enrolment would make it
// unpriceable -- so it is only applied where the category prices it.
func TestStudentTierIsNotAppliedToACategoryThatDoesNotPriceIt(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	var tenantID int
	db.QueryRow(`SELECT tenant_id FROM users WHERE email=?`, "admin@studyhub.com").Scan(&tenantID)

	groupClass := core.GenerateID("CLS")
	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,classroom,pricing_category_id,default_tier_name,monthly_fee_override)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, groupClass, tenantID, "Group", "Monday", "16:00", "17:00", "R", "PC_group", "Level 3-4", 0)
	mandarinClass := core.GenerateID("CLS")
	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,classroom,pricing_category_id,default_tier_name,monthly_fee_override)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, mandarinClass, tenantID, "Mandarin", "Tuesday", "16:00", "17:00", "R", "PC_mandarin", "Group", 0)

	stu := models.Student{FirstName: "Two", LastName: "Categories", Contact: "twocat@example.com",
		Status: "Active", EnrolledClasses: []string{groupClass, mandarinClass}, PricingTier: "Level 1-2"}
	w := doRequest(r, "POST", "/api/students", token, stu)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var made models.Student
	json.NewDecoder(w.Body).Decode(&made)
	t.Cleanup(func() { db.Exec(`DELETE FROM students WHERE id=?`, made.ID) })

	var mandarinTier string
	db.QueryRow(`SELECT COALESCE(tier_name,'') FROM enrollments WHERE student_id=? AND class_id=?`, made.ID, mandarinClass).Scan(&mandarinTier)
	if mandarinTier != "" {
		t.Errorf("Mandarin enrolment got tier %q, which that category does not price — it would become unpriceable", mandarinTier)
	}
	var groupTier string
	db.QueryRow(`SELECT COALESCE(tier_name,'') FROM enrollments WHERE student_id=? AND class_id=?`, made.ID, groupClass).Scan(&groupTier)
	if groupTier != "Level 1-2" {
		t.Errorf("Group enrolment tier %q, want Level 1-2", groupTier)
	}
}

// students.level_band still feeds the admin session-price preview, but the
// student form stopped collecting it when the tier selector replaced the
// hardcoded 1-3 / 4-6 bands. A save must not blank it.
func TestSavingAStudentKeepsTheLevelBandTheFormNoLongerSends(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	db := store.InitDB(testDSN())

	stu := models.Student{FirstName: "Band", LastName: "Keeper", Contact: "band@example.com", Status: "Active"}
	w := doRequest(r, "POST", "/api/students", token, stu)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var made models.Student
	json.NewDecoder(w.Body).Decode(&made)
	t.Cleanup(func() { db.Exec(`DELETE FROM students WHERE id=?`, made.ID) })

	if _, err := db.Exec(`UPDATE students SET level_band='4-6' WHERE id=?`, made.ID); err != nil {
		t.Fatalf("seed band: %v", err)
	}

	made.LevelBand = "" // what the form now posts
	if w := doRequest(r, "PUT", "/api/students/"+made.ID, token, made); w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}

	var band string
	db.QueryRow(`SELECT COALESCE(level_band,'') FROM students WHERE id=?`, made.ID).Scan(&band)
	if band != "4-6" {
		t.Errorf("level_band is %q after a save, want 4-6 — the session-price preview reads this column", band)
	}
}
