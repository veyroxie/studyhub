package handlers

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/auth"
	"studyhub/internal/core"
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
