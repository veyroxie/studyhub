package handlers

import (
	"testing"

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
