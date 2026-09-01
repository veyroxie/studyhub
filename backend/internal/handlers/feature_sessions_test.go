package handlers

import (
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/auth"
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

// TestSessionsInPeriod_HonoursScheduleHistory locks the 0046 contract: a
// dated schedule change keeps earlier weeks on the old weekday while later
// weeks follow the class row, so a September day change cannot rewrite
// August's calendar, billing counts, or iCal feed.
func TestSessionsInPeriod_HonoursScheduleHistory(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	monday := time.Now()
	for monday.Weekday() != time.Monday {
		monday = monday.AddDate(0, 0, 1)
	}
	day := func(offset int) string { return monday.AddDate(0, 0, offset).Format("2006-01-02") }
	from, to := day(0), day(25)

	// Seeded holidays would reclassify dates; this test locks placement only.
	db.Exec(`DELETE FROM holidays`)

	classID := core.GenerateID("CLS")
	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time) VALUES(?,?,?,?,?,?)`,
		classID, 1, "Changed Class", "Thursday", "16:00", "17:00")
	// The class met on Fridays at 15:00 until day(14); Thursdays after.
	db.Exec(`INSERT INTO class_schedule_history(id,tenant_id,class_id,day,time,end_time,changed_on) VALUES(?,?,?,?,?,?,?)`,
		core.GenerateID("SCH"), 1, classID, "Friday", "15:00", "16:00", day(14))

	sessions, err := store.SessionsInPeriod(db, classID, from, to)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	// Fridays in weeks 1-2 are day(4), day(11); Thursdays from day(14) on
	// are day(17), day(24).
	want := []string{day(4), day(11), day(17), day(24)}
	if len(sessions) != len(want) {
		t.Fatalf("want %d sessions, got %d: %+v", len(want), len(sessions), sessions)
	}
	for i, sess := range sessions {
		if sess.Date != want[i] || sess.Status != store.SessionHeld {
			t.Errorf("session %d: want held on %s, got %s on %s", i, want[i], sess.Status, sess.Date)
		}
	}
}

// TestClassUpdate_ScheduleFromSnapshotsOldSlot locks the write side of 0046:
// a PUT carrying scheduleFrom stores the PREVIOUS day/time in
// class_schedule_history, a repeat edit with the same effective date keeps
// the first snapshot, and a plain PUT (typo fix) writes no history at all.
func TestClassUpdate_ScheduleFromSnapshotsOldSlot(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	r := chi.NewRouter()
	r.Post("/api/auth/login", auth.HandleLogin(db))
	r.Group(func(g chi.Router) {
		g.Use(auth.JWTMiddleware(db))
		g.Put("/api/classes/{id}", HandleClassByID(db))
	})
	tok := getToken(t, r, "admin@studyhub.com", "admin123")

	classID := core.GenerateID("CLS")
	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,capacity) VALUES(?,?,?,?,?,?,?)`,
		classID, 1, "Friday Class", "Friday", "15:00", "16:00", 6)

	put := func(day, timeStr, endTime, scheduleFrom string) int {
		w := authedJSON(t, r, "PUT", "/api/classes/"+classID, tok, map[string]any{
			"name": "Friday Class", "teacherIds": []string{}, "day": day,
			"time": timeStr, "endTime": endTime, "capacity": 6,
			"scheduleFrom": scheduleFrom,
		})
		return w.Code
	}

	if code := put("Thursday", "16:00", "17:00", "2026-09-01"); code != http.StatusOK {
		t.Fatalf("dated update failed: %d", code)
	}
	var day, tm, changedOn string
	db.QueryRow(`SELECT day,time,changed_on FROM class_schedule_history WHERE class_id=?`, classID).Scan(&day, &tm, &changedOn)
	if day != "Friday" || tm != "15:00" || changedOn != "2026-09-01" {
		t.Fatalf("history should hold the OLD slot: got %s %s from %s", day, tm, changedOn)
	}
	db.QueryRow(`SELECT day FROM classes WHERE id=?`, classID).Scan(&day)
	if day != "Thursday" {
		t.Fatalf("class row should hold the new slot, got %s", day)
	}

	// Re-editing with the same effective date keeps the original snapshot —
	// the intermediate Thursday schedule never applied to a real date.
	if code := put("Wednesday", "10:00", "11:00", "2026-09-01"); code != http.StatusOK {
		t.Fatalf("second dated update failed: %d", code)
	}
	count := countRows(t, db, `SELECT COUNT(*) FROM class_schedule_history WHERE class_id=?`, classID)
	if err := db.QueryRow(`SELECT day FROM class_schedule_history WHERE class_id=?`, classID).Scan(&day); err != nil {
		t.Fatalf("read history day: %v", err)
	}
	if count != 1 || day != "Friday" {
		t.Fatalf("same-date re-edit must keep the first snapshot: count=%d day=%s", count, day)
	}

	// A plain edit is a retroactive correction: no history row.
	if code := put("Monday", "09:00", "10:00", ""); code != http.StatusOK {
		t.Fatalf("plain update failed: %d", code)
	}
	if count = countRows(t, db, `SELECT COUNT(*) FROM class_schedule_history WHERE class_id=?`, classID); count != 1 {
		t.Fatalf("plain edit must not add history, got %d rows", count)
	}

	if code := put("Monday", "09:00", "10:00", "not-a-date"); code != http.StatusBadRequest {
		t.Fatalf("malformed scheduleFrom should 400, got %d", code)
	}

	// Out-of-order: a change EARLIER than the recorded 2026-09-01 change must
	// be rejected — the row already reflects the later edit, so snapshotting
	// it would backdate the wrong schedule (409, review finding).
	if code := put("Tuesday", "11:00", "12:00", "2026-08-15"); code != http.StatusConflict {
		t.Fatalf("out-of-order scheduleFrom should 409, got %d", code)
	}
	if count = countRows(t, db, `SELECT COUNT(*) FROM class_schedule_history WHERE class_id=?`, classID); count != 1 {
		t.Fatalf("rejected out-of-order edit must not add history, got %d rows", count)
	}
}

// TestSessionsInPeriod_CancelledDestinationIsVisible locks the fix for a
// cancellation landing on a MOVE DESTINATION. The natural-date loop never
// visits a cross-weekday destination, so the cancellation used to be invisible
// and renderers drew a session the centre had called off. Billing must not
// change: the charge sits on the origin's moved_out, and promoting the
// destination to SessionCancelled (which bills) would charge it twice.
func TestSessionsInPeriod_CancelledDestinationIsVisible(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()
	db.Exec(`DELETE FROM holidays`)

	monday := time.Now()
	for monday.Weekday() != time.Monday {
		monday = monday.AddDate(0, 0, 1)
	}
	day := func(o int) string { return monday.AddDate(0, 0, o).Format("2006-01-02") }

	classID := core.GenerateID("CLS")
	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time) VALUES(?,?,?,?,?,?)`,
		classID, 1, "Dest Cancel Class", "Monday", "10:00", "11:00")
	// Monday day(0) moves to Wednesday day(2) -- not a natural occurrence.
	db.Exec(`INSERT INTO class_session_moves(id,tenant_id,class_id,from_date,to_date,created_on) VALUES(?,?,?,?,?,?)`,
		core.GenerateID("MOV"), 1, classID, day(0), day(2), core.Today())
	db.Exec(`INSERT INTO cancelled_classes(id,tenant_id,class_id,date,created_on) VALUES(?,?,?,?,?)`,
		core.GenerateID("CC"), 1, classID, day(2), core.Today())

	sessions, err := store.SessionsInPeriod(db, classID, day(0), day(6))
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	var sawOut, sawIn bool
	billable := 0
	for _, s := range sessions {
		if s.Billable() {
			billable++
		}
		switch s.Status {
		case store.SessionMovedOut:
			sawOut = true
			if !s.Cancelled {
				t.Error("moved_out must carry the destination's cancellation for renderers")
			}
		case store.SessionMovedIn:
			sawIn = true
			if !s.Cancelled {
				t.Error("moved_in on a cancelled destination must be marked cancelled")
			}
		}
	}
	if !sawOut || !sawIn {
		t.Fatalf("expected both ends of the move, got %+v", sessions)
	}
	// Exactly one charge: the origin. A cancelled session still bills (credits
	// compensate) but it must not bill once per end of the move.
	if billable != 1 {
		t.Errorf("want 1 billable session across the move, got %d: %+v", billable, sessions)
	}
}

// TestSessionMove_RejectsOccupiedDestination locks the C2 fix: attendance is
// keyed (person, date, class), so two sessions of one class on one date cannot
// be recorded. Moving onto a date the class already meets must 409.
func TestSessionMove_RejectsOccupiedDestination(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	r := chi.NewRouter()
	r.Post("/api/auth/login", auth.HandleLogin(db))
	r.Group(func(g chi.Router) {
		g.Use(auth.JWTMiddleware(db))
		g.Post("/api/session-moves", HandleCreateSessionMove(db))
	})
	tok := getToken(t, r, "admin@studyhub.com", "admin123")

	monday := time.Now()
	for monday.Weekday() != time.Monday {
		monday = monday.AddDate(0, 0, 1)
	}
	day := func(o int) string { return monday.AddDate(0, 0, o).Format("2006-01-02") }

	classID := core.GenerateID("CLS")
	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time) VALUES(?,?,?,?,?,?)`,
		classID, 1, "Clash Class", "Monday", "10:00", "11:00")

	// Monday to the FOLLOWING Monday: the class already meets there.
	w := authedJSON(t, r, "POST", "/api/session-moves", tok, map[string]any{
		"classId": classID, "fromDate": day(0), "toDate": day(7),
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("same-weekday move should 409, got %d %s", w.Code, w.Body.String())
	}

	// A free date is still accepted.
	if w := authedJSON(t, r, "POST", "/api/session-moves", tok, map[string]any{
		"classId": classID, "fromDate": day(0), "toDate": day(2),
	}); w.Code != http.StatusCreated {
		t.Fatalf("free destination should be accepted, got %d %s", w.Code, w.Body.String())
	}

	// Re-targeting the SAME move must not clash with itself.
	if w := authedJSON(t, r, "POST", "/api/session-moves", tok, map[string]any{
		"classId": classID, "fromDate": day(0), "toDate": day(3),
	}); w.Code != http.StatusCreated {
		t.Fatalf("re-targeting the same move should succeed, got %d %s", w.Code, w.Body.String())
	}
}
