package main

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ── Announcements ─────────────────────────────────────────────────────────────

func listAnnouncements(db *DB, c *Claims) []Announcement {
	tw, twArgs := scopeTenant(c, "")
	rows, err := db.Query(`SELECT id,title,message,audience,type,created_on,created_by,status,archive_on FROM announcements WHERE 1=1`+tw+` ORDER BY created_on DESC`, twArgs...)
	if err != nil {
		return []Announcement{}
	}
	defer rows.Close()
	out := []Announcement{}
	for rows.Next() {
		var a Announcement
		var status, archiveOn sql.NullString
		if err := rows.Scan(&a.ID, &a.Title, &a.Message, &a.Audience, &a.Type, &a.CreatedOn, &a.CreatedBy, &status, &archiveOn); err != nil {
			continue
		}
		a.Status = nullStr(status)
		if a.Status == "" {
			a.Status = "published"
		}
		a.ArchiveOn = nullStr(archiveOn)
		out = append(out, a)
	}
	return out
}

func listAnnouncementsPaged(db *DB, c *Claims, p Pagination) ([]Announcement, int) {
	tw, twArgs := scopeTenant(c, "")
	var total int
	db.QueryRow(`SELECT COUNT(*) FROM announcements WHERE 1=1`+tw, twArgs...).Scan(&total)
	pageArgs := append(append([]any{}, twArgs...), p.Limit, p.Offset)
	rows, err := db.Query(`SELECT id,title,message,audience,type,created_on,created_by,status,archive_on FROM announcements WHERE 1=1`+tw+` ORDER BY created_on DESC LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		return []Announcement{}, total
	}
	defer rows.Close()
	out := []Announcement{}
	for rows.Next() {
		var a Announcement
		var status, archiveOn sql.NullString
		if err := rows.Scan(&a.ID, &a.Title, &a.Message, &a.Audience, &a.Type, &a.CreatedOn, &a.CreatedBy, &status, &archiveOn); err != nil {
			continue
		}
		a.Status = nullStr(status)
		if a.Status == "" {
			a.Status = "published"
		}
		a.ArchiveOn = nullStr(archiveOn)
		out = append(out, a)
	}
	return out, total
}

func handleAnnouncements(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			p := parsePagination(r)
			if !p.Active {
				respond(w, listAnnouncements(db, c))
				return
			}
			data, total := listAnnouncementsPaged(db, c, p)
			respond(w, PaginatedResponse{Data: data, Total: total, Limit: p.Limit, Offset: p.Offset})
		case http.MethodPost:
			if c.Role != "admin" && c.Role != "teacher" {
				respondError(w, "admin or teacher only", 403)
				return
			}
			var a Announcement
			if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			if msg := validationError("title", a.Title, "message", a.Message); msg != "" {
				respondError(w, msg, 400)
				return
			}
			if a.ID == "" {
				a.ID = generateID("ANN")
			}
			if a.CreatedOn == "" {
				a.CreatedOn = today()
			}
			if a.CreatedBy == "" {
				a.CreatedBy = c.Name
			}
			if a.Status == "" {
				a.Status = "published"
			}
			tid := tenantID(c)
			if _, err := db.Exec(`INSERT INTO announcements(id,tenant_id,title,message,audience,type,created_on,created_by,status,archive_on) VALUES(?,?,?,?,?,?,?,?,?,?)`,
				a.ID, tid, a.Title, a.Message, a.Audience, a.Type, a.CreatedOn, a.CreatedBy, a.Status, a.ArchiveOn); err != nil {
				respondError(w, "could not create announcement", 500)
				return
			}
			respond(w, a)
		}
	}
}

func handleAnnouncementDelete(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := scopeTenant(c, "")
		args := append([]any{id}, twArgs...)
		if _, err := db.Exec(`DELETE FROM announcements WHERE id=?`+tw, args...); err != nil {
			respondError(w, "could not delete announcement", 500)
			return
		}
		logAudit(db, c.Email, "announcement_deleted", "announcement", id, "deleted")
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAnnouncementUpdate(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var a Announcement
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		tw, twArgs := scopeTenant(c, "")
		args := append([]any{a.Title, a.Message, a.Type, a.ArchiveOn, id}, twArgs...)
		if _, err := db.Exec(`UPDATE announcements SET title=?,message=?,type=?,archive_on=? WHERE id=?`+tw, args...); err != nil {
			respondError(w, "could not update announcement", 500)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAnnouncementApprove(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
			respondError(w, "bad request body", http.StatusBadRequest)
			return
		}
		if body.Status == "" {
			body.Status = "published"
		}
		tw, twArgs := scopeTenant(c, "")
		args := append([]any{body.Status, id}, twArgs...)
		if _, err := db.Exec(`UPDATE announcements SET status=? WHERE id=?`+tw, args...); err != nil {
			respondError(w, "could not update announcement", 500)
			return
		}
		logAudit(db, c.Email, "announcement_"+body.Status, "announcement", id, "status changed to "+body.Status)
		w.WriteHeader(http.StatusNoContent)
	}
}
