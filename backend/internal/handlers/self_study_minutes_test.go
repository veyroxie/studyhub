package handlers

import (
	"testing"

	"studyhub/internal/models"
)

// Billed overflow is summed from these minutes, so the server decides them, not the form.
func TestSelfStudyMinutes(t *testing.T) {
	cases := []struct {
		name   string
		in     models.SelfStudySession
		want   int
		reject bool
	}{
		{"a typed duration within the form's range", models.SelfStudySession{DurationMin: 60}, 60, false},
		{"start and end decide, whatever was typed", models.SelfStudySession{StartTime: "15:00", EndTime: "16:30", DurationMin: 5}, 90, false},
		{"an arrival with no end yet counts nothing", models.SelfStudySession{StartTime: "15:00", DurationMin: 600}, 0, false},
		{"a negative duration would cancel billed overflow", models.SelfStudySession{DurationMin: -600}, 0, true},
		{"longer than a day at the centre", models.SelfStudySession{DurationMin: 100000}, 0, true},
		{"nothing logged", models.SelfStudySession{}, 0, true},
		{"end before start", models.SelfStudySession{StartTime: "16:00", EndTime: "15:00"}, 0, true},
		{"unreadable times", models.SelfStudySession{StartTime: "3pm", EndTime: "4pm"}, 0, true},
	}
	for _, c := range cases {
		got, msg := selfStudyMinutes(c.in)
		if c.reject != (msg != "") || (!c.reject && got != c.want) {
			t.Errorf("%s: got %d, %q; want %d, reject=%v", c.name, got, msg, c.want, c.reject)
		}
	}
}
