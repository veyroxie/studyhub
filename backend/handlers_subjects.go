package main

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ── Subject CRUD ──────────────────────────────────────────────────────────────

func listSubjects(db *DB, c *Claims) []Subject {
	tid := tenantID(c)
	rows, err := db.Query(`SELECT id,name,category,level,description,monthly_fee,color FROM subjects WHERE (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY name`, tid, tid)
	if err != nil {
		return []Subject{}
	}
	defer rows.Close()
	out := []Subject{}
	for rows.Next() {
		var s Subject
		rows.Scan(&s.ID, &s.Name, &s.Category, &s.Level, &s.Description, &s.MonthlyFee, &s.Color)
		out = append(out, s)
	}
	return out
}

func handleListSubjects(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		respond(w, listSubjects(db, c))
	}
}

func handleCreateSubject(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		var s Subject
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		if msg := validationError("name", s.Name, "category", s.Category); msg != "" {
			respondError(w, msg, 400)
			return
		}
		if s.ID == "" {
			s.ID = generateID("sub")
		}
		tid := tenantID(c)
		_, err := db.Exec(`INSERT INTO subjects(id,tenant_id,name,category,level,description,monthly_fee,color) VALUES(?,?,?,?,?,?,?,?)`,
			s.ID, tid, s.Name, s.Category, s.Level, s.Description, s.MonthlyFee, s.Color)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		if c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "subject_created", "subject", s.ID, s.Name)
		}
		w.WriteHeader(http.StatusCreated)
		respond(w, s)
	}
}

func handleUpdateSubject(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var s Subject
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		s.ID = id
		tid := tenantID(c)
		res, err := db.Exec(`UPDATE subjects SET name=?,category=?,level=?,description=?,monthly_fee=?,color=? WHERE id=? AND (tenant_id=? OR ?=0)`,
			s.Name, s.Category, s.Level, s.Description, s.MonthlyFee, s.Color, id, tid, tid)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			respondError(w, "subject not found", 404)
			return
		}
		if c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "subject_updated", "subject", id, s.Name)
		}
		respond(w, s)
	}
}

func handleDeleteSubject(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tid := tenantID(c)
		db.Exec(`UPDATE subjects SET deleted_at=NOW() WHERE id=? AND (tenant_id=? OR ?=0)`, id, tid, tid)
		if c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "subject_deleted", "subject", id, "soft deleted")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
