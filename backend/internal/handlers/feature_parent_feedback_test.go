package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

// Parents read progress reports, not the class feed. The feed carries notes on
// every child in the class, so each leak below exposed classmates' notes.

const (
	feedOwnClass   = "FB_PARENT_OWN"   // c3: the parent's STU001 and another family's STU004
	feedOtherClass = "FB_PARENT_OTHER" // c1: none of the parent's children
)

func seedParentFeed(t *testing.T) func() {
	t.Helper()
	db := store.InitDB(testDSN())
	rows := [][]any{
		{feedOwnClass, "c3", "s3", core.Today(), `[{"studentId":"STU001","note":"own child"},{"studentId":"STU004","note":"classmate"}]`},
		{feedOtherClass, "c1", "s1", core.Today(), `[{"studentId":"STU006","note":"other class"}]`},
	}
	for _, f := range rows {
		if _, err := db.Exec(`INSERT INTO feedback(id,tenant_id,class_id,teacher_id,date,topic,mood,notes,student_notes) VALUES(?,1,?,?,?,'t','Good','n',?)`, f...); err != nil {
			t.Fatalf("seed feedback: %v", err)
		}
	}
	return func() { db.Close() }
}

func TestParentSnapshotCarriesNoClassFeed(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	defer seedParentFeed(t)()
	store.SnapshotCacheInvalidateAll()

	w := doRequest(r, "GET", "/api/snapshot", getParentToken(t, r), nil)
	var snap models.Snapshot
	json.NewDecoder(w.Body).Decode(&snap)
	if len(snap.Feedback) != 0 || len(snap.FeedbackReplies) != 0 {
		t.Fatalf("parent snapshot has %d feed rows and %d replies, want none", len(snap.Feedback), len(snap.FeedbackReplies))
	}
}

func TestAdminSnapshotStillCarriesClassFeed(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	defer seedParentFeed(t)()
	store.SnapshotCacheInvalidateAll()

	w := doRequest(r, "GET", "/api/snapshot", getAdminToken(t, r), nil)
	var snap models.Snapshot
	json.NewDecoder(w.Body).Decode(&snap)
	if len(snap.Feedback) == 0 {
		t.Fatal("staff lost the class feed")
	}
}

func TestParentFeedListIsEmpty(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	defer seedParentFeed(t)()

	w := doRequest(r, "GET", "/api/feedback?classId=c3", getParentToken(t, r), nil)
	var rows []models.Feedback
	json.NewDecoder(w.Body).Decode(&rows)
	if w.Code != http.StatusOK || len(rows) != 0 {
		t.Fatalf("parent feed list: %d with %d rows, want 200 and none", w.Code, len(rows))
	}
}

func TestParentCannotReplyToClassFeed(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	defer seedParentFeed(t)()

	w := doRequest(r, "POST", "/api/feedback-replies", getParentToken(t, r), map[string]string{"feedbackId": feedOwnClass, "message": "hi"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("parent reply: %d, want 403", w.Code)
	}
}

func TestParentDataExportHoldsOnlyTheirChildrensNotes(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	defer seedParentFeed(t)()

	w := doRequest(r, "GET", "/api/account/export-my-data", getParentToken(t, r), nil)
	var out struct {
		Feedback []models.Feedback `json:"feedback"`
	}
	json.NewDecoder(w.Body).Decode(&out)
	for _, f := range out.Feedback {
		if f.ClassID != "c3" {
			t.Errorf("export holds feedback for class %s, which none of the children attend", f.ClassID)
		}
		for _, n := range f.StudentNotes {
			if n.StudentID != "STU001" && n.StudentID != "STU002" {
				t.Errorf("export holds a note about %s, another family's child", n.StudentID)
			}
		}
	}
}
