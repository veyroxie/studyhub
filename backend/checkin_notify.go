package main

import "strings"

// Parent notification on student check-in / check-out.
//
// Fired from a goroutine in the attendance handler so it never blocks or fails
// the write. Three channels converge here:
//   - web push (sendPushToParent) — works when the app is closed
//   - email (opt-in via users.notify_checkin_email)
//   - in-app toast — handled separately by the WebSocket broadcast
//
// All three respect the same unpaid-Monthly gate the in-app toast uses
// (frontend api.js): a family behind on Monthly fees gets no check-in/out
// notification on any channel, so billing leverage stays consistent.

// checkinActionURL is where a push/notification click lands the parent.
const checkinActionURL = "/#attendance"

type studentNotify struct {
	firstName  string
	lastName   string
	contact    string
	parentName string
}

// notifyParentOnCheck sends the parent push + (opt-in) email for one class
// attendance check event. isCheckIn distinguishes arrival from departure.
func notifyParentOnCheck(db *DB, tenantID int, a Attendance, isCheckIn bool) {
	verb := "checked out"
	eventTime := timeOrEmpty(a.CheckOut)
	if isCheckIn {
		verb = "checked in"
		eventTime = timeOrEmpty(a.CheckIn)
	}
	context := classNameByID(db, tenantID, a.ClassID)
	dispatchParentNotify(db, tenantID, a.PersonID, verb, context, eventTime)
}

// notifySelfStudyCheckIn pings the parent when a live self-study session starts
// (logged with a start time but no end yet). Reuses the same billing gate and
// channels as class check-ins, labelled "Self-study" so the parent has context.
func notifySelfStudyCheckIn(db *DB, tenantID int, studentID, startTime string) {
	dispatchParentNotify(db, tenantID, studentID, "checked in", "Self-study", startTime)
}

// dispatchParentNotify is the shared core: resolve student + parent, apply the
// billing gate, then push + (opt-in) email. context is the class name or
// "Self-study"; eventTime is the HH:MM of the event.
func dispatchParentNotify(db *DB, tenantID int, studentID, verb, context, eventTime string) {
	stu, ok := lookupStudentForNotify(db, tenantID, studentID)
	if !ok || stu.contact == "" {
		return
	}
	if hasUnpaidMonthly(db, tenantID, stu.contact) {
		return
	}
	title := strings.TrimSpace(stu.firstName+" "+stu.lastName) + " " + verb
	detail := eventTime
	if context != "" {
		detail = context + " · " + eventTime
	}
	sendPushToParent(db, tenantID, stu.contact, pushPayload{
		Title: title, Body: detail, URL: checkinActionURL, Tag: "checkin-" + studentID,
	})
	maybeEmailParent(db, stu.contact, stu.parentName, title, detail)
}

// lookupStudentForNotify fetches the parent contact + names for a student.
func lookupStudentForNotify(db *DB, tenantID int, studentID string) (studentNotify, bool) {
	var s studentNotify
	err := db.QueryRow(`SELECT first_name, COALESCE(last_name,''), COALESCE(contact,''), COALESCE(parent_name,'')
		FROM students WHERE id=? AND tenant_id=?`, studentID, tenantID).
		Scan(&s.firstName, &s.lastName, &s.contact, &s.parentName)
	if err != nil {
		return s, false
	}
	return s, true
}

// classNameByID returns the class name, or "" when there's no class on the row.
func classNameByID(db *DB, tenantID int, classID *string) string {
	if classID == nil || *classID == "" {
		return ""
	}
	var name string
	db.QueryRow(`SELECT COALESCE(name,'') FROM classes WHERE id=? AND tenant_id=?`, *classID, tenantID).Scan(&name)
	return name
}

// hasUnpaidMonthly mirrors the frontend gate: any Monthly invoice still
// Unpaid/Overdue for one of the parent's children blocks check-in alerts.
//
// On a query error we log and return false (let the alert through). A check-in
// notification is a safety signal — suppressing it because a query hiccuped is
// worse than the rare case of one alert slipping past the billing gate.
func hasUnpaidMonthly(db *DB, tenantID int, parentEmail string) bool {
	var exists bool
	err := db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM invoices i JOIN students s ON s.id=i.student_id
		WHERE s.contact=? AND i.tenant_id=? AND i.type='Monthly'
		  AND (i.status='Unpaid' OR i.status='Overdue') AND i.deleted_at IS NULL
	)`, parentEmail, tenantID).Scan(&exists)
	if err != nil {
		logger.Error("check-in billing gate query failed; allowing alert", "err", err, "email", parentEmail)
		return false
	}
	return exists
}

func timeOrEmpty(t *string) string {
	if t == nil {
		return ""
	}
	return *t
}

// maybeEmailParent sends the check event email only when the parent opted in.
func maybeEmailParent(db *DB, parentEmail, parentName, subject, detail string) {
	var wantsEmail bool
	db.QueryRow(`SELECT COALESCE(notify_checkin_email,false) FROM users WHERE email=?`, parentEmail).Scan(&wantsEmail)
	if !wantsEmail {
		return
	}
	if err := mailer.Send(parentEmail, subject, renderCheckEventEmail(parentName, subject, detail)); err != nil {
		logger.Error("check-in email failed", "err", err, "email", parentEmail)
	}
}
