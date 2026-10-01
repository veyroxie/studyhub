package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

// The board is the centre's standing policies, pinned by an admin, shown to every role.

func boardPolicy(t *testing.T, db *store.DB, id string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO announcements(id,tenant_id,title,message,audience,type,created_on,created_by,status,category,pinned,updated_on) VALUES(?,1,'Late pickup','RM10 per 15 minutes','all','Notice',?,'admin','published','policy',TRUE,?)`,
		id, core.Today(), core.Today()); err != nil {
		t.Fatalf("seed policy: %v", err)
	}
}

func snapshotAnnouncement(t *testing.T, w *http.Response, id string) *models.Announcement {
	t.Helper()
	var snap models.Snapshot
	json.NewDecoder(w.Body).Decode(&snap)
	for i := range snap.Announcements {
		if snap.Announcements[i].ID == id {
			return &snap.Announcements[i]
		}
	}
	return nil
}

func TestEveryRoleGetsThePinnedPolicyInTheSnapshot(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	defer db.Close()
	boardPolicy(t, db, "ANN_BOARD_1")
	for role, token := range map[string]string{"admin": getAdminToken(t, r), "teacher": getTeacherToken(t, r), "parent": getParentToken(t, r)} {
		store.SnapshotCacheInvalidateAll()
		a := snapshotAnnouncement(t, doRequest(r, "GET", "/api/snapshot", token, nil).Result(), "ANN_BOARD_1")
		if a == nil || !a.Pinned || a.Category != models.AnnouncementCategoryPolicy || a.UpdatedOn == "" {
			t.Errorf("%s snapshot lost the board fields: %+v", role, a)
		}
	}
}

func TestEditingAPolicysWordingKeepsItPinned(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	defer db.Close()
	boardPolicy(t, db, "ANN_BOARD_2")
	w := doRequest(r, "PUT", "/api/announcements/ANN_BOARD_2", getAdminToken(t, r), map[string]string{"title": "Late pickup", "message": "RM15 per 15 minutes", "type": "Notice"})
	if w.Code != http.StatusNoContent {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	var pinned bool
	var category, message string
	db.QueryRow(`SELECT pinned, category, message FROM announcements WHERE id='ANN_BOARD_2'`).Scan(&pinned, &category, &message)
	if !pinned || category != models.AnnouncementCategoryPolicy || message != "RM15 per 15 minutes" {
		t.Errorf("after a wording edit: pinned=%v category=%q message=%q", pinned, category, message)
	}
}

func TestOnlyAnAdminCanPin(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	w := doRequest(r, "POST", "/api/announcements", getTeacherToken(t, r), map[string]any{"title": "Mine", "message": "m", "audience": "parents", "pinned": true, "category": "policy"})
	var a models.Announcement
	json.NewDecoder(w.Body).Decode(&a)
	if a.Pinned || a.PinRequested {
		t.Errorf("a teacher's post came back pinned=%v pinRequested=%v", a.Pinned, a.PinRequested)
	}
	if code := doRequest(r, "PUT", "/api/announcements/"+a.ID, getTeacherToken(t, r), map[string]any{"pinned": true}).Code; code != http.StatusForbidden {
		t.Errorf("a teacher editing the board: %d, want 403", code)
	}
}

// Posted the way the form posts it: an admin picks "All Staff" and ticks the board box.
func TestAPolicyPostedFromTheFormReachesEveryRole(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	admin := getAdminToken(t, r)
	w := doRequest(r, "POST", "/api/announcements", admin, map[string]any{"title": "Make-up classes", "message": "Within 30 days", "audience": "staff", "type": "Notice", "pinned": true, "category": "policy"})
	var created models.Announcement
	json.NewDecoder(w.Body).Decode(&created)
	for role, token := range map[string]string{"teacher": getTeacherToken(t, r), "parent": getParentToken(t, r)} {
		store.SnapshotCacheInvalidateAll()
		if snapshotAnnouncement(t, doRequest(r, "GET", "/api/snapshot", token, nil).Result(), created.ID) == nil {
			t.Errorf("the %s never sees the board policy", role)
		}
	}

	// Pinning an existing staff-only notice from the edit form opens it to everyone too.
	w = doRequest(r, "POST", "/api/announcements", admin, map[string]any{"title": "Staff only", "message": "m", "audience": "staff", "type": "Notice"})
	var notice models.Announcement
	json.NewDecoder(w.Body).Decode(&notice)
	doRequest(r, "PUT", "/api/announcements/"+notice.ID, admin, map[string]any{"title": "Staff only", "message": "m", "type": "Notice", "pinned": true, "category": "policy"})
	store.SnapshotCacheInvalidateAll()
	if snapshotAnnouncement(t, doRequest(r, "GET", "/api/snapshot", getParentToken(t, r), nil).Result(), notice.ID) == nil {
		t.Error("a notice pinned from the edit form stayed hidden from parents")
	}
}
