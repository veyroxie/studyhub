package main

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ── Announcements ─────────────────────────────────────────────────────────────

// announceVisibilityClause returns a WHERE-clause fragment + args that hide
// drafts and pending-approval announcements from non-staff callers, plus
// restrict the audience pool to "all" / "parents" for parents. Staff
// (admin, superadmin, teacher) get the unfiltered set.
func announceVisibilityClause(c *Claims) (string, []any) {
	if c != nil && (c.Role == "admin" || c.Role == "superadmin" || c.Role == "teacher") {
		return "", nil
	}
	// Parents see broadcast announcements ('all', 'parents') AND targeted
	// class announcements ('class:<id>') for classes any of their children
	// is enrolled in. Per-class targeting is used by the cancelled-class
	// auto-announcement so the affected parents actually get notified.
	// The class-id allow-list is built per-request below so each parent
	// only sees their own children's classes.
	return ` AND COALESCE(status,'published')='published' AND (audience IN ('all','parents') OR audience LIKE 'class:%')`, nil
}

// parentAnnouncementFilter post-filters the broad SQL result to drop
// class-targeted rows whose class is not in the parent's enrollment set.
// We can't easily filter in SQL because enrolled_classes is JSON text on
// students, but the row count for a parent is small so this is cheap.
func parentAnnouncementFilter(rows []Announcement, classIDs map[string]bool) []Announcement {
	out := make([]Announcement, 0, len(rows))
	for _, a := range rows {
		if len(a.Audience) > 6 && a.Audience[:6] == "class:" {
			if !classIDs[a.Audience[6:]] {
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

func listAnnouncements(db *DB, c *Claims) []Announcement {
	tw, twArgs := scopeTenant(c, "")
	vw, vwArgs := announceVisibilityClause(c)
	args := append(append([]any{}, twArgs...), vwArgs...)
	rows, err := db.Query(`SELECT id,title,message,audience,type,created_on,created_by,status,archive_on FROM announcements WHERE 1=1`+tw+vw+` ORDER BY created_on DESC LIMIT 5000`, args...)
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
	if c != nil && c.Role == "parent" {
		out = parentAnnouncementFilter(out, parentClassIDs(db, c))
	}
	return out
}

func listAnnouncementsPaged(db *DB, c *Claims, p Pagination) ([]Announcement, int) {
	tw, twArgs := scopeTenant(c, "")
	vw, vwArgs := announceVisibilityClause(c)
	baseArgs := append(append([]any{}, twArgs...), vwArgs...)
	var total int
	db.QueryRow(`SELECT COUNT(*) FROM announcements WHERE 1=1`+tw+vw, baseArgs...).Scan(&total)
	pageArgs := append(append([]any{}, baseArgs...), p.Limit, p.Offset)
	rows, err := db.Query(`SELECT id,title,message,audience,type,created_on,created_by,status,archive_on FROM announcements WHERE 1=1`+tw+vw+` ORDER BY created_on DESC LIMIT ? OFFSET ?`, pageArgs...)
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
	if c != nil && c.Role == "parent" {
		out = parentAnnouncementFilter(out, parentClassIDs(db, c))
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
			logAudit(db, c.Email, "announcement_created", "announcement", a.ID, a.Title)
			respond(w, a)
		}
	}
}

func handleAnnouncementDelete(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if !isAdminRole(c) {
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
		if !isAdminRole(c) {
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
		logAudit(db, c.Email, "announcement_updated", "announcement", id, a.Title)
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAnnouncementApprove(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if !isAdminRole(c) {
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
