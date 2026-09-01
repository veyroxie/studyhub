package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/mailer"
	"studyhub/internal/models"
	"studyhub/internal/store"
	"time"

	"github.com/go-chi/chi/v5"
)

func chiURLParam(r *http.Request, name string) string {
	return chi.URLParam(r, name)
}

// Per-parent iCalendar feed. Parents subscribe to this URL in their phone
// calendar app (Apple Calendar, Google Calendar, Fantastical, etc.) and
// their children's classes appear automatically.
//
// Auth: the URL carries a per-user HMAC signature instead of a cookie —
// calendar apps don't speak cookies, so we mint a stable token tied to
// the user's id + a server secret. Rotating JWT_SECRET (or adding a
// dedicated ICS_SECRET later) invalidates all outstanding feeds.
//
// Token: hex(HMAC-SHA256(jwt_secret, "ical:<user_id>:<email>"))
//
// URL: /api/calendar/<user_id>/<token>.ics
//
// We deliberately do NOT expose this through the cookie-based JWT
// middleware; that would force the calendar app to authenticate, which
// none of them do.

// icalToken signs the feed URL. The version counter is part of the signed
// input, so bumping users.ical_token_version (e.g. on password reset)
// invalidates any previously-issued feed URL without rotating JWT_SECRET.
func icalToken(userID int, email string, version int) string {
	mac := hmac.New(sha256.New, auth.JWTSecret())
	mac.Write([]byte(fmt.Sprintf("ical:%d:%s:%d", userID, email, version)))
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyIcalToken(userID int, email, token string, version int) bool {
	want := icalToken(userID, email, version)
	return hmac.Equal([]byte(want), []byte(token))
}

// icalTokenVersion returns the user's current feed-token version (0 default).
func icalTokenVersion(db *store.DB, userID int) int {
	var v int
	db.QueryRow(`SELECT COALESCE(ical_token_version,0) FROM users WHERE id=?`, userID).Scan(&v)
	return v
}

// handleParentCalendarURL returns the personalised feed URL for the
// authenticated parent. Frontend renders this as a "Subscribe to calendar"
// button.
//
// GET /api/account/calendar-url
func HandleParentCalendarURL(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if c == nil {
			core.RespondError(w, "auth required", http.StatusUnauthorized)
			return
		}
		base := mailer.AppURL()
		// webcal:// scheme tells iOS / macOS / many Android apps to add
		// the URL as a subscription instead of downloading a one-off
		// snapshot. https:// works too as a fallback.
		webcalBase := strings.Replace(base, "http://", "webcal://", 1)
		webcalBase = strings.Replace(webcalBase, "https://", "webcal://", 1)
		path := fmt.Sprintf("/api/calendar/%d/%s.ics", c.UserID, icalToken(c.UserID, c.Email, icalTokenVersion(db, c.UserID)))
		core.Respond(w, map[string]string{
			"webcalUrl": webcalBase + path,
			"httpsUrl":  base + path,
		})

	}
}

// handleParentCalendarFeed serves the .ics document for the given user.
// Verifies the signed token before doing anything else.
//
// GET /api/calendar/{userID}/{token}.ics
func HandleParentCalendarFeed(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userIDStr := chiURLParam(r, "userID")
		token := strings.TrimSuffix(chiURLParam(r, "token"), ".ics")
		var userID int
		fmt.Sscanf(userIDStr, "%d", &userID)
		if userID == 0 {
			core.RespondError(w, "not found", http.StatusNotFound)
			return
		}

		var email, role string
		var tenantID, tokenVersion int
		if err := db.QueryRow(`SELECT email, COALESCE(role,''), tenant_id, COALESCE(ical_token_version,0) FROM users WHERE id=?`, userID).Scan(&email, &role, &tenantID, &tokenVersion); err != nil {
			core.RespondError(w, "not found", http.StatusNotFound)
			return
		}
		if !verifyIcalToken(userID, email, token, tokenVersion) {
			core.RespondError(w, "invalid feed token", http.StatusUnauthorized)
			return
		}

		// Build the synthetic parent claim so existing parent-scope queries
		// reuse their tenant + filter logic without duplication.
		fakeClaims := &core.Claims{
			UserID:   userID,
			TenantID: tenantID,
			Email:    email,
			Role:     role,
		}

		// Children's enrolment → upcoming class events for the next 6 weeks.
		// Recurring weekly classes are flattened to individual VEVENTs per
		// occurrence so non-RRULE-aware clients render them correctly.
		classes := listClasses(db, fakeClaims)
		stuIDs := parentStudentIDs(db, fakeClaims)

		// Filter to classes the parent's kids are actually in.
		studentRoster := listStudents(db, fakeClaims)
		relevantClassIDs := map[string]bool{}
		for _, s := range studentRoster {
			if !stuIDs[s.ID] {
				continue
			}
			for _, cid := range s.EnrolledClasses {
				relevantClassIDs[cid] = true
			}
		}

		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		w.Header().Set("Cache-Control", "private, max-age=900") // 15min
		w.Header().Set("X-Content-Type-Options", "nosniff")

		var b strings.Builder
		b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//StudyHub//EN\r\nMETHOD:PUBLISH\r\nX-WR-CALNAME:Study Hub classes\r\nX-WR-TIMEZONE:Asia/Kuala_Lumpur\r\n")

		now := time.Now()
		// Look back 1 week + forward 6 weeks to cover what mobile calendars
		// typically render on the default month view.
		start := now.AddDate(0, 0, -7)
		end := now.AddDate(0, 0, 42)

		// The canonical expander classifies every occurrence (F6). Cancelled
		// sessions must still be emitted, as STATUS:CANCELLED under the SAME
		// UID — omitting them would leave the stale event in every calendar
		// that already synced it, which is how parents turned up to cancelled
		// classes (V2_IDEAS A15). Holidays stay display-only here: the class
		// still runs, so the event renders as normal.
		fromStr, toStr := start.Format("2006-01-02"), end.Format("2006-01-02")
		for _, cls := range classes {
			if !relevantClassIDs[cls.ID] {
				continue
			}
			sessions, err := store.SessionsInPeriod(db, cls.ID, fromStr, toStr)
			if err != nil {
				core.Logger.Error("ical session expand failed", "err", err, "class_id", cls.ID)
				continue
			}
			for _, sess := range sessions {
				day, err := time.ParseInLocation("2006-01-02", sess.Date, start.Location())
				if err != nil {
					continue
				}
				switch sess.Status {
				case store.SessionMovedOut:
					// Same UID, new DTSTART: synced calendars relocate the
					// event instead of keeping a stale copy on the old date.
					if newDay, err := time.ParseInLocation("2006-01-02", sess.MovedTo, start.Location()); err == nil {
						writeMovedEvent(&b, cls, day, newDay, sess.Cancelled, sess.Time, sess.EndTime)
					}
				case store.SessionMovedIn:
					// Origin in-window already emitted via moved_out; only a
					// move landing here from OUTSIDE the window needs an event.
					if sess.MovedFrom < fromStr || sess.MovedFrom > toStr {
						if origDay, err := time.ParseInLocation("2006-01-02", sess.MovedFrom, start.Location()); err == nil {
							writeMovedEvent(&b, cls, origDay, day, sess.Cancelled, sess.Time, sess.EndTime)
						}
					}
				case store.SessionCancelled:
					writeEvent(&b, cls, day, true, sess.Time, sess.EndTime)
				default:
					writeEvent(&b, cls, day, false, sess.Time, sess.EndTime)
				}
			}
		}

		b.WriteString("END:VCALENDAR\r\n")
		w.Write([]byte(b.String()))
	}
}

func writeEvent(b *strings.Builder, cls models.Class, day time.Time, isCancelled bool, atStart, atEnd string) {
	// Times as the class ran ON this date (0047), falling back to the class
	// row. A past event must keep its original slot: re-stamping it with a
	// newer time silently rewrites what parents already have in their
	// calendars.
	if atStart == "" || atEnd == "" {
		atStart, atEnd = cls.Time, cls.EndTime
	}
	// Parse HH:MM start / end into the day's local time. iCal floats local
	// when there's no TZID — many parents are in MY, the X-WR-TIMEZONE
	// hint above gets calendars to interpret naive times as Asia/KL.
	startT, ok := combineDateTime(day, atStart)
	if !ok {
		return
	}
	endT, ok := combineDateTime(day, atEnd)
	if !ok {
		endT = startT.Add(1 * time.Hour)
	}
	uid := fmt.Sprintf("class-%s-%s@studyhub.fit", cls.ID, day.Format("20060102"))
	b.WriteString("BEGIN:VEVENT\r\n")
	b.WriteString("UID:" + uid + "\r\n")
	b.WriteString("DTSTAMP:" + time.Now().UTC().Format("20060102T150405Z") + "\r\n")
	b.WriteString("DTSTART:" + startT.Format("20060102T150405") + "\r\n")
	b.WriteString("DTEND:" + endT.Format("20060102T150405") + "\r\n")
	if isCancelled {
		// STATUS is the standards-compliant signal; the SUMMARY prefix is for
		// clients that render cancelled events without any visual distinction.
		b.WriteString("STATUS:CANCELLED\r\n")
		b.WriteString("SUMMARY:" + icsEscape("Cancelled: "+cls.Name) + "\r\n")
	} else {
		b.WriteString("SUMMARY:" + icsEscape(cls.Name) + "\r\n")
	}
	if cls.Classroom != "" {
		b.WriteString("LOCATION:" + icsEscape(cls.Classroom) + "\r\n")
	}
	b.WriteString("END:VEVENT\r\n")
}

// writeMovedEvent emits a rescheduled occurrence under its ORIGINAL date's
// UID, with start/end on the new date, so calendar clients move the event.
func writeMovedEvent(b *strings.Builder, cls models.Class, origDay, newDay time.Time, cancelled bool, atStart, atEnd string) {
	if atStart == "" || atEnd == "" {
		atStart, atEnd = cls.Time, cls.EndTime
	}
	startT, ok := combineDateTime(newDay, atStart)
	if !ok {
		return
	}
	endT, ok := combineDateTime(newDay, atEnd)
	if !ok {
		endT = startT.Add(1 * time.Hour)
	}
	uid := fmt.Sprintf("class-%s-%s@studyhub.fit", cls.ID, origDay.Format("20060102"))
	b.WriteString("BEGIN:VEVENT\r\n")
	b.WriteString("UID:" + uid + "\r\n")
	b.WriteString("DTSTAMP:" + time.Now().UTC().Format("20060102T150405Z") + "\r\n")
	b.WriteString("DTSTART:" + startT.Format("20060102T150405") + "\r\n")
	b.WriteString("DTEND:" + endT.Format("20060102T150405") + "\r\n")
	summary := cls.Name + " (moved from " + origDay.Format("2 Jan") + ")"
	if cancelled {
		// Same UID as the move, so a synced calendar strikes the event through
		// in place rather than leaving a session the centre has cancelled.
		summary = "Cancelled: " + summary
		b.WriteString("STATUS:CANCELLED\r\n")
	}
	b.WriteString("SUMMARY:" + icsEscape(summary) + "\r\n")
	if cls.Classroom != "" {
		b.WriteString("LOCATION:" + icsEscape(cls.Classroom) + "\r\n")
	}
	b.WriteString("END:VEVENT\r\n")
}

func combineDateTime(day time.Time, hm string) (time.Time, bool) {
	if hm == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("15:04", hm)
	if err != nil {
		return time.Time{}, false
	}
	return time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), 0, 0, day.Location()), true
}

func icsEscape(s string) string {
	r := strings.NewReplacer(
		"\\", "\\\\",
		"\n", "\\n",
		"\r", "",
		",", "\\,",
		";", "\\;",
	)
	return r.Replace(s)
}
