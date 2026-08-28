package handlers

import (
	"testing"
	"time"

	"studyhub/internal/core"
	"studyhub/internal/store"
)

// TestSessionsInPeriod_ClassifiesEveryOccurrence locks the F6 expander
// contract: classify, never filter; TEXT local dates; cancelled beats
// holiday (credits were granted, the session stays billable); a moved
// session bills on its origin date only.
func TestSessionsInPeriod_ClassifiesEveryOccurrence(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	monday := time.Now()
	for monday.Weekday() != time.Monday {
		monday = monday.AddDate(0, 0, 1)
	}
	day := func(offset int) string { return monday.AddDate(0, 0, offset).Format("2006-01-02") }
	// Window ends past day 23 so week 4's destination lands inside it; a
	// destination beyond `to` would (correctly) yield only the moved_out.
	from, to := day(0), day(25)

	classID := core.GenerateID("CLS")
	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time) VALUES(?,?,?,?,?,?)`,
		classID, 1, "Expander Class", "Monday", "10:00", "11:00")

	// Week 2: cancelled, under a holiday that also covers week 3.
	db.Exec(`INSERT INTO cancelled_classes(id,tenant_id,class_id,date,created_on) VALUES(?,?,?,?,?)`,
		core.GenerateID("CC"), 1, classID, day(7), core.Today())
	db.Exec(`INSERT INTO holidays(id,tenant_id,name,date,end_date) VALUES(?,?,?,?,?)`,
		core.GenerateID("HOL"), 1, "Expander Break", day(7), day(14))
	// Week 4: moved to that week's Wednesday.
	db.Exec(`INSERT INTO class_session_moves(id,tenant_id,class_id,from_date,to_date,created_on) VALUES(?,?,?,?,?,?)`,
		core.GenerateID("MOV"), 1, classID, day(21), day(23), core.Today())
	// A move landing inside the window from before it.
	db.Exec(`INSERT INTO class_session_moves(id,tenant_id,class_id,from_date,to_date,created_on) VALUES(?,?,?,?,?,?)`,
		core.GenerateID("MOV"), 1, classID, day(-7), day(2), core.Today())

	sessions, err := store.SessionsInPeriod(db, classID, from, to)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}

	want := []store.ClassSession{
		{Date: day(0), Status: store.SessionHeld},
		{Date: day(2), Status: store.SessionMovedIn, MovedFrom: day(-7)},
		{Date: day(7), Status: store.SessionCancelled},
		{Date: day(14), Status: store.SessionHoliday},
		{Date: day(21), Status: store.SessionMovedOut, MovedTo: day(23)},
		{Date: day(23), Status: store.SessionMovedIn, MovedFrom: day(21)},
	}
	if len(sessions) != len(want) {
		t.Fatalf("want %d entries, got %d: %+v", len(want), len(sessions), sessions)
	}
	for i, w := range want {
		if sessions[i] != w {
			t.Errorf("entry %d: want %+v, got %+v", i, w, sessions[i])
		}
	}

	billable := 0
	for _, s := range sessions {
		if s.Billable() {
			billable++
		}
	}
	if billable != 3 {
		t.Errorf("want 3 billable (held + cancelled + moved_out), got %d", billable)
	}

	if _, err := store.SessionsInPeriod(db, "CLS_missing", from, to); err == nil {
		t.Error("unknown class should error, not return an empty schedule")
	}
}
