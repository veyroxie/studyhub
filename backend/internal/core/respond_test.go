package core

import "testing"

// HolidayCovers is the canonical range predicate (F4); its frontend mirror is
// App.Utils.holidayCovers in js/utils.js with matching cases in
// frontend/tests/unit/utils.test.mjs.
func TestHolidayCovers(t *testing.T) {
	cases := []struct {
		name               string
		date, endDate, day string
		want               bool
	}{
		{"range start inclusive", "2026-03-15", "2026-03-17", "2026-03-15", true},
		{"range middle", "2026-03-15", "2026-03-17", "2026-03-16", true},
		{"range end inclusive", "2026-03-15", "2026-03-17", "2026-03-17", true},
		{"day before range", "2026-03-15", "2026-03-17", "2026-03-14", false},
		{"day after range", "2026-03-15", "2026-03-17", "2026-03-18", false},
		{"single day matches its date", "2026-03-15", "", "2026-03-15", true},
		{"single day is never open-ended", "2026-03-15", "", "2026-04-01", false},
		{"malformed end degrades to single day", "2026-03-15", "2026-03-10", "2026-03-15", true},
		{"malformed end matches nothing else", "2026-03-15", "2026-03-10", "2026-03-12", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HolidayCovers(tc.date, tc.endDate, tc.day); got != tc.want {
				t.Errorf("HolidayCovers(%q, %q, %q) = %v, want %v", tc.date, tc.endDate, tc.day, got, tc.want)
			}
		})
	}
}
