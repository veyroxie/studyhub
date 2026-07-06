package handlers

import (
	"database/sql"
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
	rows, err := db.Query(`SELECT id,title,message,audience,type,created_on,created_by,status,archive_on FROM announcements WHERE 1=1`+tw+vw+` ORDER BY created_on DESC LIMIT 5000`, args...)
	if err != nil {
		core.Logger.Error("list query failed", "err", err, "type", "Announcement")
		return []models.Announcement{}
	}
	defer rows.Close()
	out := []models.Announcement{}
	for rows.Next() {
		var a models.Announcement
		var status, archiveOn sql.NullString
		if err := rows.Scan(&a.ID, &a.Title, &a.Message, &a.Audience, &a.Type, &a.CreatedOn, &a.CreatedBy, &status, &archiveOn); err != nil {
			continue
		}
		a.Status = models.NullStr(status)
		if a.Status == "" {
			a.Status = "published"
		}
		a.ArchiveOn = models.NullStr(archiveOn)
		out = append(out, a)
	}
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
	rows, err := db.Query(`SELECT id,title,message,audience,type,created_on,created_by,status,archive_on FROM announcements WHERE 1=1`+tw+vw+` ORDER BY created_on DESC LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		core.Logger.Error("list query failed", "err", err, "type", "Announcement")
		return []models.Announcement{}, total
	}
	defer rows.Close()
	out := []models.Announcement{}
	for rows.Next() {
		var a models.Announcement
		var status, archiveOn sql.NullString
		if err := rows.Scan(&a.ID, &a.Title, &a.Message, &a.Audience, &a.Type, &a.CreatedOn, &a.CreatedBy, &status, &archiveOn); err != nil {
			continue
		}
		a.Status = models.NullStr(status)
		if a.Status == "" {
			a.Status = "published"
		}
		a.ArchiveOn = models.NullStr(archiveOn)
		out = append(out, a)
	}
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
			if a.Status == "" {
				a.Status = "published"
			}
			tid := store.TenantID(c)
			if _, err := db.Exec(`INSERT INTO announcements(id,tenant_id,title,message,audience,type,created_on,created_by,status,archive_on) VALUES(?,?,?,?,?,?,?,?,?,?)`,
				a.ID, tid, a.Title, a.Message, a.Audience, a.Type, a.CreatedOn, a.CreatedBy, a.Status, a.ArchiveOn); err != nil {
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
		var a models.Announcement
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		tw, twArgs := store.ScopeTenant(c, "")
		args := append([]any{a.Title, a.Message, a.Type, a.ArchiveOn, id}, twArgs...)
		if _, err := db.Exec(`UPDATE announcements SET title=?,message=?,type=?,archive_on=? WHERE id=?`+tw, args...); err != nil {
			core.RespondError(w, "could not update announcement", 500)
			return
		}
		core.LogAudit(db, store.TenantID(c), c.Email, "announcement_updated", "announcement", id, a.Title)
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
