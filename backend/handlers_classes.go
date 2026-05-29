package main

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// validateTeacherIDs rejects class create/update payloads referencing staff
// rows that don't exist in the caller's tenant. Without this a typo or
// foreign id silently goes into teacher_ids JSON and clash detection misses.
func validateTeacherIDs(db *DB, c *Claims, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	tw, twArgs := scopeTenant(c, "")
	for _, id := range ids {
		var exists int
		args := append([]any{id}, twArgs...)
		db.QueryRow(`SELECT 1 FROM staff WHERE id=? AND deleted_at IS NULL`+tw, args...).Scan(&exists)
		if exists != 1 {
			return errClassTeacherNotFound{id: id}
		}
	}
	return nil
}

type errClassTeacherNotFound struct{ id string }

func (e errClassTeacherNotFound) Error() string { return "teacher not found in tenant: " + e.id }

// ── Classes ───────────────────────────────────────────────────────────────────

func listClasses(db *DB, c *Claims) []Class {
	tw, twArgs := scopeTenant(c, "")
	rows, err := db.Query(`SELECT id,name,teacher_ids,classroom,day,time,end_time,capacity,enrolled,color,category FROM classes WHERE deleted_at IS NULL`+tw, twArgs...)
	if err != nil {
		return []Class{}
	}
	defer rows.Close()
	out := []Class{}
	for rows.Next() {
		var c Class
		var tids string
		if err := rows.Scan(&c.ID, &c.Name, &tids, &c.Classroom, &c.Day, &c.Time, &c.EndTime, &c.Capacity, &c.Enrolled, &c.Color, &c.Category); err != nil {
			continue
		}
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

			if err := validateTeacherIDs(db, cl, c.TeacherIDs); err != nil {
				respondError(w, err.Error(), http.StatusBadRequest)
				return
			}

			// ── Clash detection ────────────────────────────────────────────────
			// Two intervals [s1,e1) and [s2,e2) overlap when s1<e2 AND s2<e1.
			// Scoped to the caller's tenant so a teacher booked in tenant A
			// does not produce a false-positive conflict when tenant B tries
			// to create a class at the same time.
			ctw, ctwArgs := scopeTenant(cl, "")
			for _, tid2 := range c.TeacherIDs {
				var cnt int
				clashArgs := append([]any{c.Day, c.ID, c.Time, c.EndTime, tid2}, ctwArgs...)
				if err := db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND id!=? AND time<? AND end_time>? AND teacher_ids LIKE '%"'||?||'"%' AND deleted_at IS NULL`+ctw,
					clashArgs...).Scan(&cnt); err != nil {
					respondError(w, "server error checking class conflicts", 500)
					return
				}
				if cnt > 0 {
					respondError(w, "Conflict: teacher "+tid2+" is already booked at this time", http.StatusConflict)
					return
				}
			}
			if c.Classroom != "" {
				var cnt int
				roomArgs := append([]any{c.Day, c.Classroom, c.ID, c.Time, c.EndTime}, ctwArgs...)
				if err := db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND classroom=? AND id!=? AND time<? AND end_time>? AND deleted_at IS NULL`+ctw,
					roomArgs...).Scan(&cnt); err != nil {
					respondError(w, "server error checking class conflicts", 500)
					return
				}
				if cnt > 0 {
					respondError(w, "Conflict: "+c.Classroom+" is already booked at this time", http.StatusConflict)
					return
				}
			}

			if c.Category == "" {
				c.Category = "Academic"
			}
			tid := tenantID(cl)
			if _, err := db.Exec(`INSERT INTO classes(id,tenant_id,name,teacher_ids,classroom,day,time,end_time,capacity,enrolled,color,category) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
				c.ID, tid, c.Name, jsonArr(c.TeacherIDs), c.Classroom, c.Day, c.Time, c.EndTime, c.Capacity, c.Enrolled, c.Color, c.Category); err != nil {
				respondError(w, "could not create class", 500)
				return
			}
			logAudit(db, cl.Email, "class_created", "class", c.ID, c.Name)
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
			if !isAdminRole(c) {
				respondError(w, "admin only", 403)
				return
			}
			var cl Class
			if err := json.NewDecoder(r.Body).Decode(&cl); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			cl.ID = id

			if err := validateTeacherIDs(db, c, cl.TeacherIDs); err != nil {
				respondError(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Clash detection (same as create) — tenant-scoped.
			tw, twArgs := scopeTenant(c, "")
			for _, tid2 := range cl.TeacherIDs {
				var cnt int
				clashArgs := append([]any{cl.Day, cl.ID, cl.Time, cl.EndTime, tid2}, twArgs...)
				if err := db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND id!=? AND time<? AND end_time>? AND teacher_ids LIKE '%"'||?||'"%' AND deleted_at IS NULL`+tw,
					clashArgs...).Scan(&cnt); err != nil {
					respondError(w, "server error checking class conflicts", 500)
					return
				}
				if cnt > 0 {
					respondError(w, "Conflict: teacher "+tid2+" is already booked at this time", http.StatusConflict)
					return
				}
			}
			if cl.Classroom != "" {
				var cnt int
				roomArgs := append([]any{cl.Day, cl.Classroom, cl.ID, cl.Time, cl.EndTime}, twArgs...)
				if err := db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND classroom=? AND id!=? AND time<? AND end_time>? AND deleted_at IS NULL`+tw,
					roomArgs...).Scan(&cnt); err != nil {
					respondError(w, "server error checking class conflicts", 500)
					return
				}
				if cnt > 0 {
					respondError(w, "Conflict: "+cl.Classroom+" is already booked at this time", http.StatusConflict)
					return
				}
			}


			args := append([]any{cl.Name, jsonArr(cl.TeacherIDs), cl.Classroom, cl.Day, cl.Time, cl.EndTime, cl.Capacity, cl.Enrolled, cl.Color, cl.Category, id}, twArgs...)
			db.Exec(`UPDATE classes SET name=?,teacher_ids=?,classroom=?,day=?,time=?,end_time=?,capacity=?,enrolled=?,color=?,category=? WHERE id=?`+tw, args...)
			if c != nil {
				logAudit(db, c.Email, "class_updated", "class", id, cl.Name)
			}
			respond(w, cl)

		case http.MethodDelete:
			if !isAdminRole(c) {
				respondError(w, "admin only", 403)
				return
			}
			tw, twArgs := scopeTenant(c, "")
			args := append([]any{id}, twArgs...)
			db.Exec(`UPDATE classes SET deleted_at=NOW() WHERE id=?`+tw, args...)
			if c != nil {
				logAudit(db, c.Email, "class_deleted", "class", id, "soft deleted")
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}
}
