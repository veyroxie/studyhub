package handlers

import (
	"encoding/json"
	"net/http"
	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
)

// ── Announcements ─────────────────────────────────────────────────────────────

func listAnnouncements(db *store.DB, c *core.Claims) []models.Announcement {
	tw, twArgs := store.ScopeTenant(c, "")
	vw, vwArgs := store.AnnounceVisibilityClause(c)
	args := append(append([]any{}, twArgs...), vwArgs...)
	rows, err := db.Query(`SELECT `+store.AnnouncementColumns+` FROM announcements WHERE 1=1`+tw+vw+` ORDER BY created_on DESC LIMIT 5000`, args...)
	if err != nil {
		core.Logger.Error("list query failed", "err", err, "type", "Announcement")
		return []models.Announcement{}
	}
	defer rows.Close()
	out := store.ScanAnnouncements(rows)
	if c != nil && c.Role == "parent" {
		out = store.ParentAnnouncementFilter(out, store.ParentClassIDs(db, c))
	}
	return out
}

func listAnnouncementsPaged(db *store.DB, c *core.Claims, p core.Pagination) ([]models.Announcement, int) {
	tw, twArgs := store.ScopeTenant(c, "")
	vw, vwArgs := store.AnnounceVisibilityClause(c)
	baseArgs := append(append([]any{}, twArgs...), vwArgs...)
	var total int
	db.QueryRow(`SELECT COUNT(*) FROM announcements WHERE 1=1`+tw+vw, baseArgs...).Scan(&total)
	pageArgs := append(append([]any{}, baseArgs...), p.Limit, p.Offset)
	rows, err := db.Query(`SELECT `+store.AnnouncementColumns+` FROM announcements WHERE 1=1`+tw+vw+` ORDER BY created_on DESC LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		core.Logger.Error("list query failed", "err", err, "type", "Announcement")
		return []models.Announcement{}, total
	}
	defer rows.Close()
	out := store.ScanAnnouncements(rows)
	if c != nil && c.Role == "parent" {
		out = store.ParentAnnouncementFilter(out, store.ParentClassIDs(db, c))
	}
	return out, total
}

func HandleAnnouncements(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			p := core.ParsePagination(r)
			if !p.Active {
				core.Respond(w, listAnnouncements(db, c))
				return
			}
			data, total := listAnnouncementsPaged(db, c, p)
			core.Respond(w, core.PaginatedResponse{Data: data, Total: total, Limit: p.Limit, Offset: p.Offset})
		case http.MethodPost:
			if !core.IsAdminRole(c) && c.Role != "teacher" {
				core.RespondError(w, "admin or teacher only", 403)
				return
			}
			var a models.Announcement
			if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
				core.RespondError(w, "bad body", 400)
				return
			}
			if msg := validationError("title", a.Title, "message", a.Message); msg != "" {
				core.RespondError(w, msg, 400)
				return
			}
			if a.ID == "" {
				a.ID = core.GenerateID("ANN")
			}
			if a.CreatedOn == "" {
				a.CreatedOn = core.Today()
			}
			if a.CreatedBy == "" {
				a.CreatedBy = c.Name
			}
			// Only admins publish directly. Teacher-created announcements are
			// forced into the approval queue regardless of the submitted status —
			// the approve endpoint and the pending visibility filter already exist,
			// but the old default let a teacher publish to every parent unreviewed.
			if core.IsAdminRole(c) {
				if a.Status == "" {
					a.Status = "published"
				}
			} else {
				// Must match the value the admin approval queue filters on
				// (communication.js) — "pending" would hide the submission
				// from the queue entirely, which is worse than publishing it.
				a.Status = "pending_approval"
			}
			// Reject an audience the visibility rules cannot serve, rather than
			// storing a row that reaches nobody and looks fine in the admin list.
			if !models.ValidAudience(a.Audience) {
				core.RespondError(w, "unknown audience "+a.Audience+" — expected all, parents, staff or class:<id>", http.StatusBadRequest)
				return
			}

			// The board is the centre's policies, so only an admin pins (D2, 2026-10-01).
			// A teacher's pin request was stored but never read; it is no longer taken.
			if !core.IsAdminRole(c) {
				a.Pinned, a.PinRequested = false, false
			}
			if a.Category == "" {
				a.Category = models.AnnouncementCategoryNotice
			}
			if !models.ValidAnnouncementCategory(a.Category) {
				core.RespondError(w, "unknown category "+a.Category+": expected policy, notice or event", http.StatusBadRequest)
				return
			}
			a.UpdatedOn = a.CreatedOn
			tid, tOK := writeTenant(w, c)
			if !tOK {
				return
			}
			if _, err := db.Exec(`INSERT INTO announcements(id,tenant_id,title,message,audience,type,created_on,created_by,status,archive_on,target_class_ids,category,pinned,pin_requested,updated_on) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				a.ID, tid, a.Title, a.Message, a.Audience, a.Type, a.CreatedOn, a.CreatedBy, a.Status, a.ArchiveOn, models.JSONArr(a.TargetClassIDs), a.Category, a.Pinned, a.PinRequested, a.UpdatedOn); err != nil {
				core.RespondError(w, "could not create announcement", 500)
				return
			}
			core.LogAudit(db, store.TenantID(c), c.Email, "announcement_created", "announcement", a.ID, a.Title)
			core.Respond(w, a)
		}
	}
}

func HandleAnnouncementDelete(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")
		args := append([]any{id}, twArgs...)
		if _, err := db.Exec(`DELETE FROM announcements WHERE id=?`+tw, args...); err != nil {
			core.RespondError(w, "could not delete announcement", 500)
			return
		}
		core.LogAudit(db, store.TenantID(c), c.Email, "announcement_deleted", "announcement", id, "deleted")
		w.WriteHeader(http.StatusNoContent)
	}
}

func HandleAnnouncementUpdate(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		// Category and pinned are kept when not sent: a wording edit used to unpin a policy.
		var body struct {
			Title     string  `json:"title"`
			Message   string  `json:"message"`
			Type      string  `json:"type"`
			ArchiveOn string  `json:"archiveOn"`
			Category  *string `json:"category"`
			Pinned    *bool   `json:"pinned"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		if msg := validationError("title", body.Title, "message", body.Message); msg != "" {
			core.RespondError(w, msg, http.StatusBadRequest)
			return
		}
		var category, pinned any
		if body.Category != nil {
			if !models.ValidAnnouncementCategory(*body.Category) {
				core.RespondError(w, "unknown category "+*body.Category+": expected policy, notice or event", http.StatusBadRequest)
				return
			}
			category = *body.Category
		}
		if body.Pinned != nil {
			pinned = *body.Pinned
		}
		// updated_on moves, created_on does not: for a policy the amendment
		// date is what tells a parent whether they have read the current text.
		tw, twArgs := store.ScopeTenant(c, "")
		args := append([]any{body.Title, body.Message, body.Type, body.ArchiveOn, category, pinned, core.Today(), id}, twArgs...)
		res, err := db.Exec(`UPDATE announcements SET title=?,message=?,type=?,archive_on=?,category=COALESCE(?,category),pinned=COALESCE(?,pinned),pin_requested=FALSE,updated_on=? WHERE id=?`+tw, args...)
		if err != nil {
			core.RespondError(w, "could not update announcement", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			core.RespondError(w, "announcement not found", http.StatusNotFound)
			return
		}
		core.LogAudit(db, store.TenantID(c), c.Email, "announcement_updated", "announcement", id, body.Title)
		w.WriteHeader(http.StatusNoContent)
	}
}

func HandleAnnouncementApprove(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
			core.RespondError(w, "bad request body", http.StatusBadRequest)
			return
		}
		if body.Status == "" {
			body.Status = "published"
		}
		tw, twArgs := store.ScopeTenant(c, "")
		args := append([]any{body.Status, id}, twArgs...)
		if _, err := db.Exec(`UPDATE announcements SET status=? WHERE id=?`+tw, args...); err != nil {
			core.RespondError(w, "could not update announcement", 500)
			return
		}
		core.LogAudit(db, store.TenantID(c), c.Email, "announcement_"+body.Status, "announcement", id, "status changed to "+body.Status)
		w.WriteHeader(http.StatusNoContent)
	}
}
