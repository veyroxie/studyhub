package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

// TestSessionRateFor locks the F8 resolution order (class override, then
// student band, then class band against the hourly matrix x duration) and
// the no-silent-zeros constraint: every pricing hole errors, never RM 0.
// The tier rates come from migration 0045's backfill (monthly / 4), so the
// expected 60/65 also verify the backfill itself.
func TestSessionRateFor(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	mkClass := func(band, start, end string, sessionRate float64) string {
		id := core.GenerateID("CLS")
		db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,class_type,level_band,session_rate) VALUES(?,?,?,?,?,?,?,?,?)`,
			id, 1, "Rate "+id, "Monday", start, end, "Group", band, sessionRate)
		return id
	}

	oneHour13 := mkClass("1-3", "10:00", "11:00", 0)
	if got, err := store.SessionRateFor(db, oneHour13, ""); err != nil || got != 60 {
		t.Errorf("1h Group 1-3 via class band: want 60, got %v (err %v)", got, err)
	}
	// The L4 student in a mixed class banded 1-3 pays their OWN band's rate.
	if got, err := store.SessionRateFor(db, oneHour13, "4-6"); err != nil || got != 65 {
		t.Errorf("student band 4-6 must win over class band: want 65, got %v (err %v)", got, err)
	}

	halfHour := mkClass("1-3", "15:00", "15:30", 0)
	if got, err := store.SessionRateFor(db, halfHour, ""); err != nil || got != 30 {
		t.Errorf("30-min class: want 30, got %v (err %v)", got, err)
	}
	threeQuarters := mkClass("4-6", "09:00", "09:45", 0)
	if got, err := store.SessionRateFor(db, threeQuarters, ""); err != nil || got != 48.75 {
		t.Errorf("45-min at RM65/hr: want 48.75, got %v (err %v)", got, err)
	}

	override := mkClass("1-3", "10:00", "11:00", 35)
	if got, err := store.SessionRateFor(db, override, "4-6"); err != nil || got != 35 {
		t.Errorf("session_rate override must win over everything: want 35, got %v (err %v)", got, err)
	}

	if _, err := store.SessionRateFor(db, mkClass("", "10:00", "11:00", 0), ""); err == nil {
		t.Error("no band anywhere must error, not bill RM 0")
	}
	if _, err := store.SessionRateFor(db, mkClass("1-3", "", "", 0), ""); err == nil {
		t.Error("missing times must error, not bill RM 0")
	}
	noTier := core.GenerateID("CLS")
	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,class_type,level_band) VALUES(?,?,?,?,?,?,?,?)`,
		noTier, 1, "Workshop Rate", "Monday", "10:00", "11:00", "Workshop", "1-3")
	if _, err := store.SessionRateFor(db, noTier, ""); err == nil {
		t.Error("missing pricing tier must error, not bill RM 0")
	}
	if _, err := store.SessionRateFor(db, "CLS_missing", ""); err == nil {
		t.Error("unknown class must error")
	}
}

// TestPricingTierPatch_OmittedFieldSurvives locks the partial-update contract:
// a PUT carrying only monthlyFee must not zero hourly_rate. The bare-float
// version silently wiped migration 0045's backfill, which is the rate session
// billing reads.
func TestPricingTierPatch_OmittedFieldSurvives(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	r := chi.NewRouter()
	r.Post("/api/auth/login", auth.HandleLogin(db))
	r.Group(func(g chi.Router) {
		g.Use(auth.JWTMiddleware(db))
		g.Put("/api/pricing/{id}", HandleUpdatePricingTier(db))
	})
	tok := getToken(t, r, "admin@studyhub.com", "admin123")

	var tierID string
	db.QueryRow(`SELECT id FROM pricing_tiers WHERE deleted_at IS NULL LIMIT 1`).Scan(&tierID)
	if tierID == "" {
		t.Skip("no seeded pricing tiers")
	}
	// pricing_tiers is seeded once and is NOT in setupFeatureTestApp's reset
	// list, so it is shared state. Restore it or the explicit-zero case below
	// leaks a 0 rate into every later test that prices a session.
	var origMonthly, origHourly float64
	if err := db.QueryRow(`SELECT COALESCE(monthly_fee,0), COALESCE(hourly_rate,0) FROM pricing_tiers WHERE id=?`, tierID).Scan(&origMonthly, &origHourly); err != nil {
		t.Fatalf("read original rates: %v", err)
	}
	// defer, NOT t.Cleanup: cleanup callbacks run after the test's deferred
	// calls, and setupFeatureTestApp's deferred cleanup closes the DB -- the
	// restore would run against a closed connection and fail silently.
	// Registered second, so LIFO runs it before that close.
	defer func() {
		if _, err := db.Exec(`UPDATE pricing_tiers SET monthly_fee=?, hourly_rate=? WHERE id=?`, origMonthly, origHourly, tierID); err != nil {
			t.Errorf("restoring shared pricing tier failed, later tests will see wrong rates: %v", err)
		}
	}()
	if _, err := db.Exec(`UPDATE pricing_tiers SET monthly_fee=240, hourly_rate=60 WHERE id=?`, tierID); err != nil {
		t.Fatalf("seed rates: %v", err)
	}

	// Only monthlyFee sent — hourly_rate must survive untouched.
	if w := authedJSON(t, r, "PUT", "/api/pricing/"+tierID, tok, map[string]any{"monthlyFee": 260}); w.Code != http.StatusOK {
		t.Fatalf("partial update failed: %d %s", w.Code, w.Body.String())
	}
	var monthly, hourly float64
	if err := db.QueryRow(`SELECT monthly_fee, hourly_rate FROM pricing_tiers WHERE id=?`, tierID).Scan(&monthly, &hourly); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if monthly != 260 || hourly != 60 {
		t.Fatalf("want monthly 260 and hourly 60 preserved, got %v / %v", monthly, hourly)
	}

	// An explicit zero is still honoured — absent and zero must differ.
	if w := authedJSON(t, r, "PUT", "/api/pricing/"+tierID, tok, map[string]any{"hourlyRate": 0}); w.Code != http.StatusOK {
		t.Fatalf("explicit zero failed: %d", w.Code)
	}
	db.QueryRow(`SELECT hourly_rate FROM pricing_tiers WHERE id=?`, tierID).Scan(&hourly)
	if hourly != 0 {
		t.Fatalf("explicit zero must be written, got %v", hourly)
	}

	if w := authedJSON(t, r, "PUT", "/api/pricing/"+tierID, tok, map[string]any{}); w.Code != http.StatusBadRequest {
		t.Fatalf("empty patch should 400, got %d", w.Code)
	}
	if w := authedJSON(t, r, "PUT", "/api/pricing/"+tierID, tok, map[string]any{"monthlyFee": -5}); w.Code != http.StatusBadRequest {
		t.Fatalf("negative fee should 400, got %d", w.Code)
	}
}

// TestSessionRateOn_PricesTheDurationThatRan locks NEW-31: a schedule change
// that alters a class's LENGTH must not reprice earlier months. The rate for a
// past session comes from the times that session actually ran at, not the
// class row's current ones.
func TestSessionRateOn_PricesTheDurationThatRan(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	var tierID string
	db.QueryRow(`SELECT id FROM pricing_tiers WHERE class_type='Group' AND level_band='1-3' AND deleted_at IS NULL LIMIT 1`).Scan(&tierID)
	if tierID == "" {
		t.Skip("no seeded Group 1-3 tier")
	}
	var origMonthly, origHourly float64
	db.QueryRow(`SELECT COALESCE(monthly_fee,0), COALESCE(hourly_rate,0) FROM pricing_tiers WHERE id=?`, tierID).Scan(&origMonthly, &origHourly)
	defer func() {
		if _, err := db.Exec(`UPDATE pricing_tiers SET monthly_fee=?, hourly_rate=? WHERE id=?`, origMonthly, origHourly, tierID); err != nil {
			t.Errorf("restoring shared pricing tier failed: %v", err)
		}
	}()
	db.Exec(`UPDATE pricing_tiers SET hourly_rate=60 WHERE id=?`, tierID)

	// The class now runs 30 minutes; it used to run a full hour.
	classID := core.GenerateID("CLS")
	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,class_type,level_band) VALUES(?,?,?,?,?,?,?,?)`,
		classID, 1, "Shrinking Class", "Monday", "10:00", "10:30", "Group", "1-3")

	// Priced as it stands now: half an hour at RM60/hr.
	if got, err := store.SessionRateFor(db, classID, ""); err != nil || got != 30 {
		t.Fatalf("current duration: want 30, got %v (err %v)", got, err)
	}
	// Priced as it ran back then: a full hour.
	if got, err := store.SessionRateOn(db, classID, "", "10:00", "11:00"); err != nil || got != 60 {
		t.Fatalf("historical duration: want 60, got %v (err %v)", got, err)
	}
	// A flat per-session override ignores duration entirely.
	db.Exec(`UPDATE classes SET session_rate=35 WHERE id=?`, classID)
	if got, err := store.SessionRateOn(db, classID, "", "10:00", "11:00"); err != nil || got != 35 {
		t.Fatalf("override must win over duration: want 35, got %v (err %v)", got, err)
	}
}

// TestClassCreate_AlwaysGetsAPricingCategory locks the create-path half of the
// 0053/0056 fix. The migration categorised every class that existed when it
// ran; the handler never wrote the column, so three classes were created with
// none in the following two days, two of them with live students, each
// silently unpriceable. Deriving on write is what stops the next one.
func TestClassCreate_AlwaysGetsAPricingCategory(t *testing.T) {
	r, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()
	tok := getToken(t, r, "admin@studyhub.com", "admin123")

	categoryOf := func(id string) string {
		var got string
		db.QueryRow(`SELECT COALESCE(pricing_category_id,'') FROM classes WHERE id=?`, id).Scan(&got)
		return got
	}
	create := func(name, classType string) string {
		w := authedJSON(t, r, "POST", "/api/classes", tok, map[string]any{
			"name": name, "day": "Wednesday", "time": "16:00", "endTime": "17:00",
			"classroom": name + " Room", "capacity": 8, "classType": classType,
		})
		if w.Code != http.StatusOK && w.Code != http.StatusCreated {
			t.Fatalf("create %s failed: %d %s", name, w.Code, w.Body.String())
		}
		var out struct {
			ID string `json:"id"`
		}
		json.Unmarshal(w.Body.Bytes(), &out)
		if out.ID == "" {
			t.Fatalf("create %s returned no id: %s", name, w.Body.String())
		}
		return out.ID
	}

	for _, tc := range []struct{ name, classType, wantCat string }{
		{"Level 3 & 4", "Group", "Group"},
		{"Teacher Rose (Someone)", "Private", "Private"},
		{"Self-Study", "Group", "Self-Study"},
	} {
		id := create(tc.name, tc.classType)
		var gotName string
		db.QueryRow(`SELECT name FROM pricing_categories WHERE id=?`, categoryOf(id)).Scan(&gotName)
		if gotName != tc.wantCat {
			t.Fatalf("%s: want category %s, got %q", tc.name, tc.wantCat, gotName)
		}
	}

	// A partial edit must not blank the tier. The class edit payload in
	// calendar.js enumerates fields and never sends defaultTierName, so the
	// UPDATE writing it is only safe because classByID prefills the stored row
	// before the body decodes on top. Remove that prefill and every class edit
	// silently destroys the tier 0053 backfilled -- flagged in review, and this
	// is what proves it does not happen.
	tiered := create("Level 1 & 2", "Group")
	if _, err := db.Exec(`UPDATE classes SET default_tier_name='Level 1-2' WHERE id=?`, tiered); err != nil {
		t.Fatalf("seed tier: %v", err)
	}
	w := authedJSON(t, r, "PUT", "/api/classes/"+tiered, tok, map[string]any{
		"name": "Level 1 & 2", "day": "Wednesday", "time": "16:00", "endTime": "17:00",
		"classroom": "Moved Room", "capacity": 8, "classType": "Group",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("edit class failed: %d %s", w.Code, w.Body.String())
	}
	var tierAfter string
	db.QueryRow(`SELECT COALESCE(default_tier_name,'') FROM classes WHERE id=?`, tiered).Scan(&tierAfter)
	if tierAfter != "Level 1-2" {
		t.Fatalf("a partial class edit must not blank the tier: got %q", tierAfter)
	}

	// An explicit but unknown category is refused rather than dropped on the
	// floor, which would leave the class uncategorised exactly as before.
	w = authedJSON(t, r, "POST", "/api/classes", tok, map[string]any{
		"name": "Bogus Cat", "day": "Friday", "time": "10:00", "endTime": "11:00",
		"classroom": "Bogus Room", "capacity": 8, "classType": "Group",
		"pricingCategoryId": "PC_does_not_exist",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown pricing category must be refused, got %d %s", w.Code, w.Body.String())
	}
}

// TestResolvePricingCategory_TenantWithoutCatalogue locks the superadmin case
// found in review. store.TenantID returns 0 for a superadmin (scope.go:11) and
// the catalogue is seeded at tenant 1, so requiring a category outright made
// class create and edit fail unconditionally for that role. A tenant with no
// catalogue at all is not onboarded onto it; one that HAS a catalogue but is
// missing the category is a real fault and still errors.
func TestResolvePricingCategory_TenantWithoutCatalogue(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	var noCatalogue models.Class
	noCatalogue.Name = "Level 3 & 4"
	noCatalogue.ClassType = "Group"
	if err := resolvePricingCategory(db, 0, &noCatalogue); err != nil {
		t.Fatalf("a tenant with no catalogue must not block the save: %v", err)
	}
	if noCatalogue.PricingCategoryID != "" {
		t.Fatalf("nothing to resolve to, so it stays blank: got %q", noCatalogue.PricingCategoryID)
	}

	var seeded models.Class
	seeded.Name = "Level 3 & 4"
	seeded.ClassType = "Group"
	if err := resolvePricingCategory(db, 1, &seeded); err != nil {
		t.Fatalf("tenant 1 has the catalogue: %v", err)
	}
	if seeded.PricingCategoryID == "" {
		t.Fatal("tenant 1 must resolve to the Group category")
	}

	// A category that is named but absent is still a hard error.
	bogus := models.Class{Name: "X", ClassType: "Group", PricingCategoryID: "PC_nope"}
	if err := resolvePricingCategory(db, 1, &bogus); err == nil {
		t.Fatal("an unknown category id must still be refused")
	}
}

// TestPricingCatalogue_CRUD locks the guards on the catalogue editor. Until
// these routes existed the prices were in the database with no way to change
// them without a deploy (ADR-004), so the editor is the switchover's
// precondition -- and the two rules below are what stop it reintroducing the
// silent zero the whole rework exists to close.
func TestPricingCatalogue_CRUD(t *testing.T) {
	r, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()
	tok := getToken(t, r, "admin@studyhub.com", "admin123")

	post := func(path string, body map[string]any) *httptest.ResponseRecorder {
		return authedJSON(t, r, "POST", path, tok, body)
	}

	// A category Nadine could add herself -- Mandarin, which has no level and
	// so never fitted the old grid.
	w := post("/api/pricing-categories", map[string]any{"name": "Mandarin", "sortOrder": 9})
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("create category: %d %s", w.Code, w.Body.String())
	}
	var cat struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &cat)
	if cat.ID == "" {
		t.Fatalf("no category id: %s", w.Body.String())
	}

	// Duplicate names are refused, so two "Mandarin" categories cannot exist
	// for a class to point at the wrong one.
	if w = post("/api/pricing-categories", map[string]any{"name": "Mandarin"}); w.Code != http.StatusConflict {
		t.Fatalf("duplicate category must conflict, got %d", w.Code)
	}

	// RULE 1: a tier with no price at all cannot be saved.
	w = post("/api/pricing-plans", map[string]any{
		"categoryId": cat.ID, "tierName": "Group", "sessionsPerWeek": 1,
		"monthlyFee": 0, "hourlyRate": 0,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("an unpriced tier must be refused, got %d %s", w.Code, w.Body.String())
	}

	w = post("/api/pricing-plans", map[string]any{
		"categoryId": cat.ID, "tierName": "Group", "sessionsPerWeek": 1, "monthlyFee": 240,
	})
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("create plan: %d %s", w.Code, w.Body.String())
	}
	var plan struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &plan)

	var fee float64
	db.QueryRow(`SELECT monthly_fee FROM pricing_plans WHERE id=?`, plan.ID).Scan(&fee)
	if fee != 240 {
		t.Fatalf("stored fee: want 240, got %v", fee)
	}

	// An hourly-only tier is legitimate -- self-study overflow is priced that
	// way (0054) -- so the rule is "priced somehow", not "has a monthly fee".
	w = post("/api/pricing-plans", map[string]any{
		"categoryId": cat.ID, "tierName": "Beyond included hours", "sessionsPerWeek": 1, "hourlyRate": 10,
	})
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("hourly-only tier must be allowed: %d %s", w.Code, w.Body.String())
	}

	// RULE 2: a category still used by a class cannot be deleted, because
	// those classes would resolve to no price and be skipped in silence.
	var tenantID int
	db.QueryRow(`SELECT tenant_id FROM users WHERE email=?`, "admin@studyhub.com").Scan(&tenantID)
	clsID := core.GenerateID("CLS")
	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,classroom,pricing_category_id) VALUES(?,?,?,?,?,?,?,?)`,
		clsID, tenantID, "Mandarin", "Thursday", "16:00", "17:00", "Room M", cat.ID)

	w = authedJSON(t, r, "DELETE", "/api/pricing-categories/"+cat.ID, tok, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("a category in use must not be deletable, got %d %s", w.Code, w.Body.String())
	}

	// Freed up, it deletes -- and takes its tiers with it, so a later category
	// of the same name does not inherit prices nobody set.
	db.Exec(`UPDATE classes SET pricing_category_id=NULL WHERE id=?`, clsID)
	if w = authedJSON(t, r, "DELETE", "/api/pricing-categories/"+cat.ID, tok, nil); w.Code != http.StatusOK {
		t.Fatalf("delete freed category: %d %s", w.Code, w.Body.String())
	}
	var livePlans int
	db.QueryRow(`SELECT COUNT(*) FROM pricing_plans WHERE category_id=? AND deleted_at IS NULL`, cat.ID).Scan(&livePlans)
	if livePlans != 0 {
		t.Fatalf("deleting a category must retire its tiers, %d left", livePlans)
	}
}
