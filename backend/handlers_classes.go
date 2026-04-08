package main

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ── Classes ───────────────────────────────────────────────────────────────────

func listClasses(db *DB, c *Claims) []Class {
	tid := tenantID(c)
	rows, err := db.Query(`SELECT id,name,teacher_ids,classroom,day,time,end_time,capacity,enrolled,color,category FROM classes WHERE deleted_at IS NULL AND (tenant_id=? OR ?=0)`, tid, tid)
	if err != nil {
		return []Class{}
	}
	defer rows.Close()
	out := []Class{}
	for rows.Next() {
		var c Class
		var tids string
		rows.Scan(&c.ID, &c.Name, &tids, &c.Classroom, &c.Day, &c.Time, &c.EndTime, &c.Capacity, &c.Enrolled, &c.Color, &c.Category)
		c.TeacherIDs = parseArr(tids)
		out = append(out, c)
	}
	return out
}

func handleClasses(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			respond(w, listClasses(db, cl))
		case http.MethodPost:
			if cl == nil || (cl.Role != "admin" && cl.Role != "superadmin") {
				respondError(w, "admin only", 403)
				return
			}
			var c Class
			if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			if msg := validationError("name", c.Name, "day", c.Day, "time", c.Time, "endTime", c.EndTime); msg != "" {
				respondError(w, msg, 400)
				return
			}
			if c.Capacity < 1 {
				c.Capacity = 5
			}
			if c.Time >= c.EndTime {
				respondError(w, "end time must be after start time", 400)
				return
			}
			if c.ID == "" {
				c.ID = generateID("cls")
			}

			// ── Clash detection ────────────────────────────────────────────────
			// Two intervals [s1,e1) and [s2,e2) overlap when s1<e2 AND s2<e1
			for _, tid2 := range c.TeacherIDs {
				var cnt int
				db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND id!=? AND time<?  AND end_time>? AND teacher_ids LIKE '%'||?||'%' AND deleted_at IS NULL`,
					c.Day, c.ID, c.Time, c.EndTime, tid2).Scan(&cnt)
				if cnt > 0 {
					respondError(w, "Conflict: teacher "+tid2+" is already booked at this time", http.StatusConflict)
					return
				}
			}
			if c.Classroom != "" {
				var cnt int
				db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND classroom=? AND id!=? AND time<? AND end_time>? AND deleted_at IS NULL`,
					c.Day, c.Classroom, c.ID, c.Time, c.EndTime).Scan(&cnt)
				if cnt > 0 {
					respondError(w, "Conflict: "+c.Classroom+" is already booked at this time", http.StatusConflict)
					return
				}
			}

			if c.Category == "" {
				c.Category = "Academic"
			}
			tid := tenantID(cl)
			db.Exec(`INSERT INTO classes(id,tenant_id,name,teacher_ids,classroom,day,time,end_time,capacity,enrolled,color,category) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
				c.ID, tid, c.Name, jsonArr(c.TeacherIDs), c.Classroom, c.Day, c.Time, c.EndTime, c.Capacity, c.Enrolled, c.Color, c.Category)
			respond(w, c)
		}
	}
}

func handleClassByID(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		id := chi.URLParam(r, "id")
		switch r.Method {
		case http.MethodPut:
			if c == nil || (c.Role != "admin" && c.Role != "superadmin") {
				respondError(w, "admin only", 403)
				return
			}
			var cl Class
			if err := json.NewDecoder(r.Body).Decode(&cl); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			cl.ID = id

			// Clash detection (same as create)
			for _, tid2 := range cl.TeacherIDs {
				var cnt int
				db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND id!=? AND time<? AND end_time>? AND teacher_ids LIKE '%'||?||'%' AND deleted_at IS NULL`,
					cl.Day, cl.ID, cl.Time, cl.EndTime, tid2).Scan(&cnt)
				if cnt > 0 {
					respondError(w, "Conflict: teacher "+tid2+" is already booked at this time", http.StatusConflict)
					return
				}
			}
			if cl.Classroom != "" {
				var cnt int
				db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND classroom=? AND id!=? AND time<? AND end_time>? AND deleted_at IS NULL`,
					cl.Day, cl.Classroom, cl.ID, cl.Time, cl.EndTime).Scan(&cnt)
				if cnt > 0 {
					respondError(w, "Conflict: "+cl.Classroom+" is already booked at this time", http.StatusConflict)
					return
				}
			}

			tid := tenantID(c)
			db.Exec(`UPDATE classes SET name=?,teacher_ids=?,classroom=?,day=?,time=?,end_time=?,capacity=?,enrolled=?,color=?,category=? WHERE id=? AND (tenant_id=? OR ?=0)`,
				cl.Name, jsonArr(cl.TeacherIDs), cl.Classroom, cl.Day, cl.Time, cl.EndTime, cl.Capacity, cl.Enrolled, cl.Color, cl.Category, id, tid, tid)
			if c != nil {
				db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
					c.Email, "class_updated", "class", id, cl.Name)
			}
			respond(w, cl)

		case http.MethodDelete:
			if c == nil || (c.Role != "admin" && c.Role != "superadmin") {
				respondError(w, "admin only", 403)
				return
			}
			tid := tenantID(c)
			db.Exec(`UPDATE classes SET deleted_at=NOW() WHERE id=? AND (tenant_id=? OR ?=0)`, id, tid, tid)
			if c != nil {
				db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
					c.Email, "class_deleted", "class", id, "soft deleted")
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}
}
