package store

import (
	"fmt"
	"math"
	"time"
)

// SessionRateFor resolves the price of ONE session of a class for a student
// (F8). Resolution order, per the confirmed rate rules: the class's own
// session_rate override wins; otherwise the (class_type, band) hourly matrix
// times the class's duration, where band is the STUDENT's own level_band and
// falls back to the class's. A mixed L3&4 class straddles the 1-3 / 4-6
// boundary, so the L4 student pays the 4-6 rate in an L3-banded class.
//
// No silent zeros (v2 constraint 4): any hole — no band anywhere, no matrix
// row, a zero rate, missing times — is an error, never RM 0.
func SessionRateFor(db *DB, classID, studentBand string) (float64, error) {
	return SessionRateOn(db, classID, studentBand, "", "")
}

// SessionRateOn prices one session using the times it ACTUALLY ran at, which
// for a past date is not necessarily the class row's current times (0047).
// Pass "" for both to use the class row -- the "price it as it stands now"
// case. Without this a schedule change that alters a class's LENGTH silently
// reprices every earlier month (NEW-31).
func SessionRateOn(db *DB, classID, studentBand, atStartHM, atEndHM string) (float64, error) {
	var tenantID int
	var classType, classBand, startHM, endHM string
	var override float64
	err := db.QueryRow(
		`SELECT tenant_id, COALESCE(class_type,'Group'), COALESCE(level_band,''), COALESCE(time,''), COALESCE(end_time,''), COALESCE(session_rate,0)
		   FROM classes WHERE id=? AND deleted_at IS NULL`, classID,
	).Scan(&tenantID, &classType, &classBand, &startHM, &endHM, &override)
	if err != nil {
		return 0, fmt.Errorf("session rate: class %s: %w", classID, err)
	}
	if override > 0 {
		// A per-session override is a flat price, so duration does not enter.
		return override, nil
	}
	if atStartHM != "" && atEndHM != "" {
		startHM, endHM = atStartHM, atEndHM
	}
	band := studentBand
	if band == "" {
		band = classBand
	}
	if band == "" {
		return 0, fmt.Errorf("session rate: class %s has no level band and no session_rate override", classID)
	}
	var hourly float64
	err = db.QueryRow(
		`SELECT COALESCE(hourly_rate,0) FROM pricing_tiers WHERE tenant_id=? AND class_type=? AND level_band=? AND deleted_at IS NULL`,
		tenantID, classType, band,
	).Scan(&hourly)
	if err != nil {
		return 0, fmt.Errorf("session rate: no pricing tier for (%s, %s): %w", classType, band, err)
	}
	if hourly <= 0 {
		return 0, fmt.Errorf("session rate: tier (%s, %s) has no hourly rate set", classType, band)
	}
	hours, err := durationHours(startHM, endHM)
	if err != nil {
		return 0, fmt.Errorf("session rate: class %s: %w", classID, err)
	}
	return math.Round(hourly*hours*100) / 100, nil
}

func durationHours(startHM, endHM string) (float64, error) {
	s, errS := time.Parse("15:04", startHM)
	e, errE := time.Parse("15:04", endHM)
	if errS != nil || errE != nil {
		return 0, fmt.Errorf("unparsable times %q-%q", startHM, endHM)
	}
	mins := e.Sub(s).Minutes()
	if mins <= 0 {
		return 0, fmt.Errorf("non-positive duration %q-%q", startHM, endHM)
	}
	return mins / 60, nil
}
