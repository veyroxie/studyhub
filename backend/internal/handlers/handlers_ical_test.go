package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"studyhub/internal/core"

	"github.com/go-chi/chi/v5"
)

// TestICalFeed_CancelledSessionEmitsStatusCancelled locks the A15 fix: a
// cancelled session must still appear in the feed, under the same UID, but
// carrying STATUS:CANCELLED — omitting it would leave the stale event in
// any calendar that already synced it.
func TestICalFeed_CancelledSessionEmitsStatusCancelled(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	// The calendar route is public (token-authed), so mount it directly.
	r := chi.NewRouter()
	r.Get("/api/calendar/{userID}/{token}", HandleParentCalendarFeed(db))

	var userID, tenantID int
	var email string
	if err := db.QueryRow(`SELECT id, email, tenant_id FROM users WHERE role='parent' ORDER BY id LIMIT 1`).
		Scan(&userID, &email, &tenantID); err != nil {
		t.Skip("no seeded parent user")
	}

	// A class that occurs today, so both today (cancelled) and next week
	// (not cancelled) fall inside the feed's -7d..+42d window.
	classID := core.GenerateID("CLS")
	today := time.Now()
	if _, err := db.Exec(
		`INSERT INTO classes(id,tenant_id,name,day,time,end_time,classroom) VALUES(?,?,?,?,?,?,?)`,
		classID, tenantID, "ICal Test Class", today.Weekday().String(), "10:00", "11:00", "Room T",
	); err != nil {
		t.Fatalf("insert class: %v", err)
	}
	stuID := core.GenerateID("STU")
	// listStudents scans several columns without COALESCE, so the insert must
	// fill them or the row is silently dropped by CollectRows.
	if _, err := db.Exec(
		`INSERT INTO students(id,tenant_id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		stuID, tenantID, "ICal", "Child", "2015-01-01", "Female", "ICal Parent", email, "60110000000", "The Study Hub", "Active", core.Today(), `["`+classID+`"]`, "[]", "", "", "",
	); err != nil {
		t.Fatalf("insert student: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO cancelled_classes(id,tenant_id,class_id,date,reason,cancelled_by,created_on) VALUES(?,?,?,?,?,?,?)`,
		core.GenerateID("CC"), tenantID, classID, today.Format("2006-01-02"), "Teacher absent", "test", core.Today(),
	); err != nil {
		t.Fatalf("insert cancellation: %v", err)
	}

	tok := icalToken(userID, email, icalTokenVersion(db, userID))
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/calendar/%d/%s.ics", userID, tok), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("feed returned %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()

	uidCancelled := fmt.Sprintf("class-%s-%s@studyhub.fit", classID, today.Format("20060102"))
	uidNextWeek := fmt.Sprintf("class-%s-%s@studyhub.fit", classID, today.AddDate(0, 0, 7).Format("20060102"))

	blockFor := func(uid string) string {
		for _, ev := range strings.Split(body, "BEGIN:VEVENT") {
			if strings.Contains(ev, uid) {
				return ev
			}
		}
		return ""
	}

	cancelledBlock := blockFor(uidCancelled)
	if cancelledBlock == "" {
		t.Fatal("cancelled occurrence missing from feed entirely — same-UID replacement impossible")
	}
	if !strings.Contains(cancelledBlock, "STATUS:CANCELLED") {
		t.Error("cancelled occurrence lacks STATUS:CANCELLED")
	}
	if !strings.Contains(cancelledBlock, "Cancelled: ICal Test Class") {
		t.Error("cancelled occurrence lacks the Cancelled summary prefix")
	}

	normalBlock := blockFor(uidNextWeek)
	if normalBlock == "" {
		t.Fatal("next week's occurrence missing from feed")
	}
	if strings.Contains(normalBlock, "STATUS:CANCELLED") {
		t.Error("non-cancelled occurrence wrongly marked cancelled")
	}
}
