package store

import (
	"fmt"
	"sort"
	"time"

	"studyhub/internal/core"
)

// Session classification (F6). Billing counts Billable() entries, iCal and
// the calendar render all of them — a filter that dropped cancelled dates
// could never emit STATUS:CANCELLED under the same UID (V2 plan, F6).
const (
	SessionHeld      = "held"
	SessionCancelled = "cancelled"
	SessionHoliday   = "holiday"
	SessionMovedOut  = "moved_out"
	SessionMovedIn   = "moved_in"
)

// ClassSession is one dated occurrence of a class inside a query window.
// Date is the natural occurrence date, except for moved_in entries where it
// is the destination date (MovedFrom carries the origin).
type ClassSession struct {
	Date      string
	Status    string
	MovedTo   string
	MovedFrom string
	// Cancelled marks a moved_in entry whose DESTINATION date was cancelled.
	// It is display-only: billing rides on the origin's moved_out, so this
	// never changes Billable().
	Cancelled bool
	// Time and EndTime are the schedule AS IT WAS on Date (0047), not the
	// class row's current times. Duration-aware pricing and historical iCal
	// stamps both depend on this.
	Time    string
	EndTime string
}

// Billable reports whether this entry counts toward a month's session total.
// Decision trail (Nadine, 2026-08): a cancellation compensates with credits
// and never reduces the invoice; only holidays reduce session counts. A
// moved session is billed on its ORIGIN date (moved_out counts, moved_in
// does not), so a move across a month boundary cannot double- or zero-bill.
func (s ClassSession) Billable() bool {
	return s.Status != SessionHoliday && s.Status != SessionMovedIn
}

// SessionsInPeriod expands one class's weekly recurrence over [from, to]
// (TEXT YYYY-MM-DD, inclusive) and classifies every occurrence against the
// exception tables. Dates are returned as local YYYY-MM-DD strings — never
// time.Time — to match the schema's lexically-compared TEXT dates.
// Tenant scope is derived from the class row, not claims: the iCal caller
// authenticates with a synthetic claim set.
func SessionsInPeriod(db *DB, classID, from, to string) ([]ClassSession, error) {
	var tenantID int
	var dayName, startTime, endTime string
	if err := db.QueryRow(`SELECT tenant_id, COALESCE(day,''), COALESCE(time,''), COALESCE(end_time,'') FROM classes WHERE id=? AND deleted_at IS NULL`, classID).Scan(&tenantID, &dayName, &startTime, &endTime); err != nil {
		return nil, fmt.Errorf("session expand: class %s: %w", classID, err)
	}
	if core.ParseDayName(dayName) < 0 {
		return nil, fmt.Errorf("session expand: class %s has no weekday (day=%q)", classID, dayName)
	}
	start, err := time.ParseInLocation("2006-01-02", from, time.Local)
	if err != nil {
		return nil, fmt.Errorf("session expand: bad from %q: %w", from, err)
	}
	end, err := time.ParseInLocation("2006-01-02", to, time.Local)
	if err != nil {
		return nil, fmt.Errorf("session expand: bad to %q: %w", to, err)
	}

	versions, err := ClassScheduleVersions(db, tenantID, classID)
	if err != nil {
		return nil, err
	}
	current := ScheduleVersion{Day: dayName, Time: startTime, EndTime: endTime}
	cancelled, err := datedSet(db, `SELECT date FROM cancelled_classes WHERE tenant_id=? AND class_id=? AND deleted_at IS NULL AND date>=? AND date<=?`, tenantID, classID, from, to)
	if err != nil {
		return nil, err
	}
	movedOut, movedIn, err := movesInWindow(db, tenantID, classID, from, to)
	if err != nil {
		return nil, err
	}
	holidays, err := tenantHolidays(db, tenantID)
	if err != nil {
		return nil, err
	}

	var out []ClassSession
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		sched := ScheduleOn(versions, current, date)
		if int(d.Weekday()) != core.ParseDayName(sched.Day) {
			continue
		}
		sess := classifyOccurrence(date, cancelled, movedOut, holidays)
		sess.Time, sess.EndTime = sched.Time, sched.EndTime
		out = append(out, sess)
	}
	// A cancellation landing on a DESTINATION date used to be invisible: the
	// natural-date loop never visits a cross-weekday destination, so renderers
	// showed a session the centre had cancelled. Status stays moved_in and
	// Billable() is deliberately untouched -- the charge sits on the origin's
	// moved_out, and promoting this to SessionCancelled (which bills) would
	// charge the same session twice.
	for dst, src := range movedIn {
		// Times resolve at the DESTINATION: that is the date the session runs.
		sched := ScheduleOn(versions, current, dst)
		out = append(out, ClassSession{Date: dst, Status: SessionMovedIn, MovedFrom: src, Cancelled: cancelled[dst], Time: sched.Time, EndTime: sched.EndTime})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].Status < out[j].Status
	})
	return out, nil
}

// classifyOccurrence resolves one natural date. Precedence: a move wins (the
// session no longer happens here), then cancellation (credits were granted,
// still billable), then holiday. Cancelled-over-holiday matters: cancelling
// on a holiday granted credits, and holiday status would silently unbill it.
func classifyOccurrence(date string, cancelled map[string]bool, movedOut map[string]string, holidays []holidayRange) ClassSession {
	if to, ok := movedOut[date]; ok {
		// Cancelled tracks the DESTINATION here: the session as relocated is
		// what a renderer draws, and an in-window move is emitted from this
		// entry (the matching moved_in is skipped as a duplicate).
		return ClassSession{Date: date, Status: SessionMovedOut, MovedTo: to, Cancelled: cancelled[to]}
	}
	if cancelled[date] {
		return ClassSession{Date: date, Status: SessionCancelled}
	}
	for _, h := range holidays {
		if core.HolidayCovers(h.date, h.endDate, date) {
			return ClassSession{Date: date, Status: SessionHoliday}
		}
	}
	return ClassSession{Date: date, Status: SessionHeld}
}

// ScheduleVersion is one class schedule and the date it took effect (0047).
type ScheduleVersion struct {
	EffectiveFrom string
	Day           string
	Time          string
	EndTime       string
}

// ClassScheduleVersions loads a class's schedule versions, oldest first.
// Exported because the frontend endpoint and the differ both need them.
func ClassScheduleVersions(db *DB, tenantID int, classID string) ([]ScheduleVersion, error) {
	rows, err := db.Query(`SELECT effective_from, day, time, end_time FROM class_schedule_versions WHERE tenant_id=? AND class_id=? ORDER BY effective_from`, tenantID, classID)
	if err != nil {
		return nil, fmt.Errorf("session expand: schedule versions: %w", err)
	}
	defer rows.Close()
	var out []ScheduleVersion
	for rows.Next() {
		var v ScheduleVersion
		if rows.Scan(&v.EffectiveFrom, &v.Day, &v.Time, &v.EndTime) == nil {
			out = append(out, v)
		}
	}
	return out, rows.Err()
}

// ScheduleOn resolves the schedule in force on one date: the version with the
// greatest effective_from <= date. Falls back to `fallback` when a class has no
// versions at all, which only happens for a class created before 0047 has run.
// Mirrors App.Utils.scheduleOn in js/utils.js -- keep the two in sync.
func ScheduleOn(versions []ScheduleVersion, fallback ScheduleVersion, date string) ScheduleVersion {
	out := fallback
	found := false
	for _, v := range versions {
		if v.EffectiveFrom <= date {
			out, found = v, true
			continue
		}
		break // ordered by effective_from, so nothing later can apply
	}
	if !found && len(versions) > 0 {
		// Every version starts after this date: the class did not exist yet in
		// its current form, so the oldest known schedule is the best answer.
		return versions[0]
	}
	return out
}

type holidayRange struct{ date, endDate string }

func tenantHolidays(db *DB, tenantID int) ([]holidayRange, error) {
	rows, err := db.Query(`SELECT date, COALESCE(end_date,'') FROM holidays WHERE tenant_id=? AND deleted_at IS NULL`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("session expand: holidays: %w", err)
	}
	defer rows.Close()
	var out []holidayRange
	for rows.Next() {
		var h holidayRange
		if rows.Scan(&h.date, &h.endDate) == nil {
			out = append(out, h)
		}
	}
	return out, nil
}

// movesInWindow returns from->to for moves originating in the window and
// to->from for moves landing in it. A move fully inside the window appears
// in both maps; consumers that render moved_in must skip entries whose
// origin is also in-window or they will draw the session twice.
func movesInWindow(db *DB, tenantID int, classID, from, to string) (map[string]string, map[string]string, error) {
	rows, err := db.Query(`SELECT from_date, to_date FROM class_session_moves WHERE tenant_id=? AND class_id=? AND deleted_at IS NULL AND (from_date BETWEEN ? AND ? OR to_date BETWEEN ? AND ?)`,
		tenantID, classID, from, to, from, to)
	if err != nil {
		return nil, nil, fmt.Errorf("session expand: moves: %w", err)
	}
	defer rows.Close()
	movedOut, movedIn := map[string]string{}, map[string]string{}
	for rows.Next() {
		var f, t string
		if rows.Scan(&f, &t) != nil {
			continue
		}
		if f >= from && f <= to {
			movedOut[f] = t
		}
		if t >= from && t <= to {
			movedIn[t] = f
		}
	}
	return movedOut, movedIn, nil
}

func datedSet(db *DB, query string, args ...any) (map[string]bool, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("session expand: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var d string
		if rows.Scan(&d) == nil {
			out[d] = true
		}
	}
	return out, nil
}
