package handlers

import (
	"encoding/json"
	"net/http"
	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
)

// validateTeacherIDs rejects class create/update payloads referencing staff
// rows that don't exist in the caller's tenant. Without this a typo or
// foreign id silently goes into teacher_ids JSON and clash detection misses.
func validateTeacherIDs(db *store.DB, c *core.Claims, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	tw, twArgs := store.ScopeTenant(c, "")
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

func listClasses(db *store.DB, c *core.Claims) []models.Class {
	tw, twArgs := store.ScopeTenant(c, "")
	rows, err := db.Query(`SELECT id,name,teacher_ids,classroom,day,time,end_time,capacity,enrolled,color,category,COALESCE(class_type,'Group'),COALESCE(level_band,'') FROM classes WHERE deleted_at IS NULL`+tw, twArgs...)
	if err != nil {
		core.Logger.Error("list query failed", "err", err, "type", "Class")
		return []models.Class{}
	}
	defer rows.Close()
	out := []models.Class{}
	for rows.Next() {
		var c models.Class
		var tids string
		if err := rows.Scan(&c.ID, &c.Name, &tids, &c.Classroom, &c.Day, &c.Time, &c.EndTime, &c.Capacity, &c.Enrolled, &c.Color, &c.Category, &c.ClassType, &c.LevelBand); err != nil {
			continue
		}
		c.TeacherIDs = models.ParseArr(tids)
		out = append(out, c)
	}
	return out
}

// listPricingTiers loads the type×level fee matrix for the snapshot, so the
// class form + Pricing screen can show fees and the cron can price students.
func listPricingTiers(db *store.DB, c *core.Claims) []models.PricingTier {
	tw, twArgs := store.ScopeTenant(c, "")
	rows, err := db.Query(`SELECT id,class_type,level_band,COALESCE(monthly_fee,0) FROM pricing_tiers WHERE deleted_at IS NULL`+tw+` ORDER BY class_type, level_band`, twArgs...)
	if err != nil {
		core.Logger.Error("list query failed", "err", err, "type", "PricingTier")
		return []models.PricingTier{}
	}
	defer rows.Close()
	out := []models.PricingTier{}
	for rows.Next() {
		var p models.PricingTier
		if err := rows.Scan(&p.ID, &p.ClassType, &p.LevelBand, &p.MonthlyFee); err != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

func HandleClasses(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl := core.ClaimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			core.Respond(w, listClasses(db, cl))
		case http.MethodPost:
			if cl == nil || (cl.Role != "admin" && cl.Role != "superadmin") {
				core.RespondError(w, "admin only", 403)
				return
			}
			var c models.Class
			if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
				core.RespondError(w, "bad body", 400)
				return
			}
			if msg := validationError("name", c.Name, "day", c.Day, "time", c.Time, "endTime", c.EndTime); msg != "" {
				core.RespondError(w, msg, 400)
				return
			}
			if c.Capacity < 1 {
				c.Capacity = 5
			}
			if c.Time >= c.EndTime {
				core.RespondError(w, "end time must be after start time", 400)
				return
			}
			if c.ID == "" {
				c.ID = core.GenerateID("cls")
			}

			if err := validateTeacherIDs(db, cl, c.TeacherIDs); err != nil {
				core.RespondError(w, err.Error(), http.StatusBadRequest)
				return
			}

			// ── Clash detection ────────────────────────────────────────────────
			// Two intervals [s1,e1) and [s2,e2) overlap when s1<e2 AND s2<e1.
			// Scoped to the caller's tenant so a teacher booked in tenant A
			// does not produce a false-positive conflict when tenant B tries
			// to create a class at the same time.
			ctw, ctwArgs := store.ScopeTenant(cl, "")
			for _, tid2 := range c.TeacherIDs {
				var cnt int
				clashArgs := append([]any{c.Day, c.ID, c.Time, c.EndTime, tid2}, ctwArgs...)
				if err := db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND id!=? AND time<? AND end_time>? AND teacher_ids LIKE '%"'||?||'"%' AND deleted_at IS NULL`+ctw,
					clashArgs...).Scan(&cnt); err != nil {
					core.RespondError(w, "server error checking class conflicts", 500)
					return
				}
				if cnt > 0 {
					core.RespondError(w, "Conflict: teacher "+tid2+" is already booked at this time", http.StatusConflict)
					return
				}
			}
			if c.Classroom != "" {
				var cnt int
				roomArgs := append([]any{c.Day, c.Classroom, c.ID, c.Time, c.EndTime}, ctwArgs...)
				if err := db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND classroom=? AND id!=? AND time<? AND end_time>? AND deleted_at IS NULL`+ctw,
					roomArgs...).Scan(&cnt); err != nil {
					core.RespondError(w, "server error checking class conflicts", 500)
					return
				}
				if cnt > 0 {
					core.RespondError(w, "Conflict: "+c.Classroom+" is already booked at this time", http.StatusConflict)
					return
				}
			}

			if c.Category == "" {
				c.Category = "Academic"
			}
			tid := store.TenantID(cl)
			if _, err := db.Exec(`INSERT INTO classes(id,tenant_id,name,teacher_ids,classroom,day,time,end_time,capacity,enrolled,color,category,class_type,level_band) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				c.ID, tid, c.Name, models.JSONArr(c.TeacherIDs), c.Classroom, c.Day, c.Time, c.EndTime, c.Capacity, c.Enrolled, c.Color, c.Category, c.ClassType, c.LevelBand); err != nil {
				core.RespondError(w, "could not create class", 500)
				return
			}
			core.LogAudit(db, store.TenantID(cl), cl.Email, "class_created", "class", c.ID, c.Name)
			core.Respond(w, c)
		}
	}
}

func HandleClassByID(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		id := chi.URLParam(r, "id")
		switch r.Method {
		case http.MethodPut:
			if !core.IsAdminRole(c) {
				core.RespondError(w, "admin only", 403)
				return
			}
			var cl models.Class
			if err := json.NewDecoder(r.Body).Decode(&cl); err != nil {
				core.RespondError(w, "bad body", 400)
				return
			}
			cl.ID = id

			if err := validateTeacherIDs(db, c, cl.TeacherIDs); err != nil {
				core.RespondError(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Clash detection (same as create) — tenant-scoped.
			tw, twArgs := store.ScopeTenant(c, "")
			for _, tid2 := range cl.TeacherIDs {
				var cnt int
				clashArgs := append([]any{cl.Day, cl.ID, cl.Time, cl.EndTime, tid2}, twArgs...)
				if err := db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND id!=? AND time<? AND end_time>? AND teacher_ids LIKE '%"'||?||'"%' AND deleted_at IS NULL`+tw,
					clashArgs...).Scan(&cnt); err != nil {
					core.RespondError(w, "server error checking class conflicts", 500)
					return
				}
				if cnt > 0 {
					core.RespondError(w, "Conflict: teacher "+tid2+" is already booked at this time", http.StatusConflict)
					return
				}
			}
			if cl.Classroom != "" {
				var cnt int
				roomArgs := append([]any{cl.Day, cl.Classroom, cl.ID, cl.Time, cl.EndTime}, twArgs...)
				if err := db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND classroom=? AND id!=? AND time<? AND end_time>? AND deleted_at IS NULL`+tw,
					roomArgs...).Scan(&cnt); err != nil {
					core.RespondError(w, "server error checking class conflicts", 500)
					return
				}
				if cnt > 0 {
					core.RespondError(w, "Conflict: "+cl.Classroom+" is already booked at this time", http.StatusConflict)
					return
				}
			}

			args := append([]any{cl.Name, models.JSONArr(cl.TeacherIDs), cl.Classroom, cl.Day, cl.Time, cl.EndTime, cl.Capacity, cl.Enrolled, cl.Color, cl.Category, cl.ClassType, cl.LevelBand, id}, twArgs...)
			res, err := db.Exec(`UPDATE classes SET name=?,teacher_ids=?,classroom=?,day=?,time=?,end_time=?,capacity=?,enrolled=?,color=?,category=?,class_type=?,level_band=? WHERE id=?`+tw, args...)
			if err != nil {
				core.RespondError(w, "could not update class", 500)
				return
			}
			if n, _ := res.RowsAffected(); n == 0 {
				core.RespondError(w, "class not found", 404)
				return
			}
			if c != nil {
				core.LogAudit(db, store.TenantID(c), c.Email, "class_updated", "class", id, cl.Name)
			}
			core.Respond(w, cl)

		case http.MethodDelete:
			if !core.IsAdminRole(c) {
				core.RespondError(w, "admin only", 403)
				return
			}
			tw, twArgs := store.ScopeTenant(c, "")
			args := append([]any{id}, twArgs...)
			res, err := db.Exec(`UPDATE classes SET deleted_at=NOW() WHERE id=?`+tw+` AND deleted_at IS NULL`, args...)
			if err != nil {
				core.RespondError(w, "could not delete class", 500)
				return
			}
			if n, _ := res.RowsAffected(); n == 0 {
				core.RespondError(w, "class not found", 404)
				return
			}
			if c != nil {
				core.LogAudit(db, store.TenantID(c), c.Email, "class_deleted", "class", id, "soft deleted")
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}
}
