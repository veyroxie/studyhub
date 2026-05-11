package main

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ── Holiday CRUD ──────────────────────────────────────────────────────────────

func listHolidays(db *DB, c *Claims) []Holiday {
	tw, twArgs := scopeTenant(c, "")
	rows, err := db.Query(`SELECT id,name,date,end_date,type,notes,created_by FROM holidays WHERE deleted_at IS NULL`+tw+` ORDER BY date`, twArgs...)
	if err != nil {
		return []Holiday{}
	}
	defer rows.Close()
	out := []Holiday{}
	for rows.Next() {
		var h Holiday
		var endDate, notes, createdBy sql.NullString
		if err := rows.Scan(&h.ID, &h.Name, &h.Date, &endDate, &h.Type, &notes, &createdBy); err != nil {
			continue
		}
		h.EndDate = nullStr(endDate)
		h.Notes = nullStr(notes)
		h.CreatedBy = nullStr(createdBy)
		if h.Type == "" {
			h.Type = "holiday"
		}
		out = append(out, h)
	}
	return out
}

func handleListHolidays(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		respond(w, listHolidays(db, c))
	}
}

func handleCreateHoliday(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		var h Holiday
		if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		if msg := validationError("name", h.Name, "date", h.Date); msg != "" {
			respondError(w, msg, 400)
			return
		}
		if h.ID == "" {
			h.ID = generateID("HOL")
		}
		if h.Type == "" {
			h.Type = "holiday"
		}
		if h.CreatedBy == "" && c != nil {
			h.CreatedBy = c.Email
		}
		tid := tenantID(c)
		_, err := db.Exec(`INSERT INTO holidays(id,tenant_id,name,date,end_date,type,notes,created_by) VALUES(?,?,?,?,?,?,?,?)`,
			h.ID, tid, h.Name, h.Date, h.EndDate, h.Type, h.Notes, h.CreatedBy)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		logAudit(db, c.Email, "holiday_created", "holiday", h.ID, h.Name+" on "+h.Date)
		w.WriteHeader(http.StatusCreated)
		respond(w, h)
	}
}

func handleUpdateHoliday(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var h Holiday
		if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		h.ID = id
		if h.Type == "" {
			h.Type = "holiday"
		}
		tw, twArgs := scopeTenant(c, "")
		args := append([]any{h.Name, h.Date, h.EndDate, h.Type, h.Notes, id}, twArgs...)
		res, err := db.Exec(`UPDATE holidays SET name=?,date=?,end_date=?,type=?,notes=? WHERE id=?`+tw, args...)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			respondError(w, "holiday not found", 404)
			return
		}
		logAudit(db, c.Email, "holiday_updated", "holiday", id, h.Name)
		respond(w, h)
	}
}

func handleDeleteHoliday(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := scopeTenant(c, "")
		args := append([]any{id}, twArgs...)
		db.Exec(`UPDATE holidays SET deleted_at=NOW() WHERE id=?`+tw, args...)
		logAudit(db, c.Email, "holiday_deleted", "holiday", id, "soft deleted")
		w.WriteHeader(http.StatusNoContent)
	}
}
