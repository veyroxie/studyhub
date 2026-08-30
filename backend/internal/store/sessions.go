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
	var dayName string
	if err := db.QueryRow(`SELECT tenant_id, COALESCE(day,'') FROM classes WHERE id=? AND deleted_at IS NULL`, classID).Scan(&tenantID, &dayName); err != nil {
		return nil, fmt.Errorf("session expand: class %s: %w", classID, err)
	}
	weekday := core.ParseDayName(dayName)
	if weekday < 0 {
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

	changes, err := classScheduleChanges(db, tenantID, classID)
	if err != nil {
		return nil, err
	}
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
		if int(d.Weekday()) != weekdayOn(changes, weekday, date) {
			continue
		}
		out = append(out, classifyOccurrence(date, cancelled, movedOut, holidays))
	}
	for dst, src := range movedIn {
		out = append(out, ClassSession{Date: dst, Status: SessionMovedIn, MovedFrom: src})
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
		return ClassSession{Date: date, Status: SessionMovedOut, MovedTo: to}
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

type scheduleChangeRow struct {
	weekday   int
	changedOn string
}

// classScheduleChanges loads the class's dated schedule changes, oldest
// first. Each row's weekday applied to dates BEFORE its changedOn (0046).
func classScheduleChanges(db *DB, tenantID int, classID string) ([]scheduleChangeRow, error) {
	rows, err := db.Query(`SELECT day, changed_on FROM class_schedule_history WHERE tenant_id=? AND class_id=? ORDER BY changed_on`, tenantID, classID)
	if err != nil {
		return nil, fmt.Errorf("session expand: schedule history: %w", err)
	}
	defer rows.Close()
	var out []scheduleChangeRow
	for rows.Next() {
		var day, on string
		if rows.Scan(&day, &on) == nil {
			out = append(out, scheduleChangeRow{weekday: core.ParseDayName(day), changedOn: on})
		}
	}
	return out, nil
}

// weekdayOn resolves which weekday the class met on for one date: the oldest
// change strictly after the date wins, else the current classes row. Mirrors
// App.Utils.scheduleOn in js/utils.js -- keep the two in sync.
func weekdayOn(changes []scheduleChangeRow, current int, date string) int {
	for _, ch := range changes {
		if date < ch.changedOn {
			return ch.weekday
		}
	}
	return current
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
