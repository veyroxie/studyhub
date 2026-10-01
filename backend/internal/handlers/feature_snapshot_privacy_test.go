package handlers

import (
	"encoding/json"
	"testing"

	"studyhub/internal/models"
	"studyhub/internal/store"
)

// A parent's snapshot carries only their own children's enrolments, and no staff email.
func TestAParentHoldsOnlyTheirOwnChildrensEnrolmentsAndNoStaffEmails(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	defer db.Close()
	db.Exec(`INSERT INTO enrollments(id,tenant_id,student_id,class_id,started_on) VALUES('ENR_OTHER',1,'STU004','c3','2026-01-01'),('ENR_OWN',1,'STU001','c3','2026-01-01')`)
	db.Exec(`INSERT INTO replacement_credits(id,tenant_id,student_id,type,minutes,note,class_id,date,created_by,category) VALUES('RC_PRIV',1,'STU001','earned',4,'n','c3','2026-09-01','chiying@studyhub.com','class')`)
	store.SnapshotCacheInvalidateAll()
	var snap models.Snapshot
	json.NewDecoder(doRequest(r, "GET", "/api/snapshot", getParentToken(t, r), nil).Body).Decode(&snap)
	for _, e := range snap.Enrollments {
		if e.StudentID != "STU001" && e.StudentID != "STU002" {
			t.Errorf("parent holds another family's enrolment for %s", e.StudentID)
		}
	}
	for _, rc := range snap.ReplacementCredits {
		if rc.CreatedBy != "" {
			t.Errorf("credit %s carries staff email %q", rc.ID, rc.CreatedBy)
		}
	}
}

// A teacher keeps their own contact details but not a colleague's.
func TestATeacherDoesNotHoldColleaguesContacts(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	store.SnapshotCacheInvalidateAll()
	var snap models.Snapshot
	json.NewDecoder(doRequest(r, "GET", "/api/snapshot", getTeacherToken(t, r), nil).Body).Decode(&snap)
	sawSelf := false
	for _, s := range snap.Staff {
		if s.ID == "s1" {
			sawSelf = s.Email != ""
			continue
		}
		if s.Phone != "" || s.Email != "" || s.EmergencyPhone != "" {
			t.Errorf("teacher holds colleague %s's contacts: %q %q %q", s.ID, s.Phone, s.Email, s.EmergencyPhone)
		}
	}
	if !sawSelf {
		t.Error("the teacher lost their own email")
	}
}
