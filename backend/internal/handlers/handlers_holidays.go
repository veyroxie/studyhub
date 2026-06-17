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

// ── Holiday CRUD ──────────────────────────────────────────────────────────────

func listHolidays(db *store.DB, c *core.Claims) []models.Holiday {
	tw, twArgs := store.ScopeTenant(c, "")
	rows, err := db.Query(`SELECT id,name,date,end_date,type,notes,created_by FROM holidays WHERE deleted_at IS NULL`+tw+` ORDER BY date LIMIT 5000`, twArgs...)
	if err != nil {
		return []models.Holiday{}
	}
	defer rows.Close()
	out := []models.Holiday{}
	for rows.Next() {
		var h models.Holiday
		var endDate, notes, createdBy sql.NullString
		if err := rows.Scan(&h.ID, &h.Name, &h.Date, &endDate, &h.Type, &notes, &createdBy); err != nil {
			continue
		}
		h.EndDate = models.NullStr(endDate)
		h.Notes = models.NullStr(notes)
		h.CreatedBy = models.NullStr(createdBy)
		if h.Type == "" {
			h.Type = "holiday"
		}
		out = append(out, h)
	}
	return out
}

func HandleListHolidays(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		core.Respond(w, listHolidays(db, c))
	}
}

func HandleCreateHoliday(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		var h models.Holiday
		if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		if msg := validationError("name", h.Name, "date", h.Date); msg != "" {
			core.RespondError(w, msg, 400)
			return
		}
		if h.ID == "" {
			h.ID = core.GenerateID("HOL")
		}
		if h.Type == "" {
			h.Type = "holiday"
		}
		if h.CreatedBy == "" && c != nil {
			h.CreatedBy = c.Email
		}
		tid := store.TenantID(c)
		_, err := db.Exec(`INSERT INTO holidays(id,tenant_id,name,date,end_date,type,notes,created_by) VALUES(?,?,?,?,?,?,?,?)`,
			h.ID, tid, h.Name, h.Date, h.EndDate, h.Type, h.Notes, h.CreatedBy)
		if err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		core.LogAudit(db, c.Email, "holiday_created", "holiday", h.ID, h.Name+" on "+h.Date)
		w.WriteHeader(http.StatusCreated)
		core.Respond(w, h)
	}
}

func HandleUpdateHoliday(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var h models.Holiday
		if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		h.ID = id
		if h.Type == "" {
			h.Type = "holiday"
		}
		tw, twArgs := store.ScopeTenant(c, "")
		args := append([]any{h.Name, h.Date, h.EndDate, h.Type, h.Notes, id}, twArgs...)
		res, err := db.Exec(`UPDATE holidays SET name=?,date=?,end_date=?,type=?,notes=? WHERE id=?`+tw, args...)
		if err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			core.RespondError(w, "holiday not found", 404)
			return
		}
		core.LogAudit(db, c.Email, "holiday_updated", "holiday", id, h.Name)
		core.Respond(w, h)
	}
}

func HandleDeleteHoliday(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")
		args := append([]any{id}, twArgs...)
		db.Exec(`UPDATE holidays SET deleted_at=NOW() WHERE id=?`+tw, args...)
		core.LogAudit(db, c.Email, "holiday_deleted", "holiday", id, "soft deleted")
		w.WriteHeader(http.StatusNoContent)
	}
}
