package main

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ── Workshop CRUD ─────────────────────────────────────────────────────────────

func listWorkshops(db *DB, c *Claims) []Workshop {
	tw, twArgs := scopeTenant(c, "")
	rows, err := db.Query(`SELECT id,name,description,date,time,end_time,classroom,capacity,enrolled,fee,teacher_ids,status FROM workshops WHERE deleted_at IS NULL`+tw+` ORDER BY date DESC`, twArgs...)
	if err != nil {
		return []Workshop{}
	}
	defer rows.Close()
	out := []Workshop{}
	for rows.Next() {
		var ws Workshop
		var tids string
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.Description, &ws.Date, &ws.Time, &ws.EndTime, &ws.Classroom, &ws.Capacity, &ws.Enrolled, &ws.Fee, &tids, &ws.Status); err != nil {
			continue
		}
		ws.TeacherIDs = parseArr(tids)
		out = append(out, ws)
	}
	return out
}

func handleListWorkshops(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		respond(w, listWorkshops(db, c))
	}
}

func handleCreateWorkshop(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		var ws Workshop
		if err := json.NewDecoder(r.Body).Decode(&ws); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		if msg := validationError("name", ws.Name, "date", ws.Date); msg != "" {
			respondError(w, msg, 400)
			return
		}
		if ws.ID == "" {
			ws.ID = generateID("ws")
		}
		if ws.Status == "" {
			ws.Status = "upcoming"
		}
		tid := tenantID(c)
		_, err := db.Exec(`INSERT INTO workshops(id,tenant_id,name,description,date,time,end_time,classroom,capacity,enrolled,fee,teacher_ids,status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			ws.ID, tid, ws.Name, ws.Description, ws.Date, ws.Time, ws.EndTime, ws.Classroom, ws.Capacity, ws.Enrolled, ws.Fee, jsonArr(ws.TeacherIDs), ws.Status)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		if c != nil {
			logAudit(db, c.Email, "workshop_created", "workshop", ws.ID, ws.Name)
		}
		w.WriteHeader(http.StatusCreated)
		respond(w, ws)
	}
}

func handleUpdateWorkshop(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var ws Workshop
		if err := json.NewDecoder(r.Body).Decode(&ws); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		ws.ID = id
		tw, twArgs := scopeTenant(c, "")
		args := append([]any{ws.Name, ws.Description, ws.Date, ws.Time, ws.EndTime, ws.Classroom, ws.Capacity, ws.Enrolled, ws.Fee, jsonArr(ws.TeacherIDs), ws.Status, id}, twArgs...)
		res, err := db.Exec(`UPDATE workshops SET name=?,description=?,date=?,time=?,end_time=?,classroom=?,capacity=?,enrolled=?,fee=?,teacher_ids=?,status=? WHERE id=?`+tw, args...)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			respondError(w, "workshop not found", 404)
			return
		}
		if c != nil {
			logAudit(db, c.Email, "workshop_updated", "workshop", id, ws.Name)
		}
		respond(w, ws)
	}
}

func handleDeleteWorkshop(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := scopeTenant(c, "")
		args := append([]any{id}, twArgs...)
		if _, err := db.Exec(`UPDATE workshops SET deleted_at=NOW() WHERE id=?`+tw, args...); err != nil {
			respondError(w, "could not delete workshop", 500)
			return
		}
		if c != nil {
			logAudit(db, c.Email, "workshop_deleted", "workshop", id, "soft deleted")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
