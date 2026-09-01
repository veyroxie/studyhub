package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
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
	rows, err := db.Query(`SELECT id,name,teacher_ids,classroom,day,time,end_time,capacity,enrolled,color,category,COALESCE(class_type,'Group'),COALESCE(level_band,''),COALESCE(subject,''),COALESCE(monthly_fee_override,0),COALESCE(session_rate,0) FROM classes WHERE deleted_at IS NULL`+tw, twArgs...)
	if err != nil {
		core.Logger.Error("list query failed", "err", err, "type", "Class")
		return []models.Class{}
	}
	defer rows.Close()
	out := []models.Class{}
	for rows.Next() {
		var c models.Class
		var tids string
		if err := rows.Scan(&c.ID, &c.Name, &tids, &c.Classroom, &c.Day, &c.Time, &c.EndTime, &c.Capacity, &c.Enrolled, &c.Color, &c.Category, &c.ClassType, &c.LevelBand, &c.Subject, &c.MonthlyFeeOverride, &c.SessionRate); err != nil {
			continue
		}
		c.TeacherIDs = models.ParseArr(tids)
		out = append(out, c)
	}
	return out
}

// listScheduleVersions loads every class's schedule versions for the snapshot;
// the calendar, attendance and dashboard resolve a date through them via
// App.Utils.scheduleOn. See migration 0047.
func listScheduleVersions(db *store.DB, c *core.Claims) []models.ScheduleVersion {
	tw, twArgs := store.ScopeTenant(c, "")
	rows, err := db.Query(`SELECT id,class_id,day,time,end_time,effective_from,created_by,created_on FROM class_schedule_versions WHERE 1=1`+tw+` ORDER BY class_id, effective_from`, twArgs...)
	return store.CollectRows(rows, err, "ScheduleVersion", func(r *sql.Rows) (models.ScheduleVersion, error) {
		var s models.ScheduleVersion
		err := r.Scan(&s.ID, &s.ClassID, &s.Day, &s.Time, &s.EndTime, &s.EffectiveFrom, &s.CreatedBy, &s.CreatedOn)
		return s, err
	})
}

// recordScheduleChange records an effective-dated schedule edit as a VERSION:
// a row stating the slot that applies FROM cl.ScheduleFrom (migration 0047).
// Out-of-order edits are ordinary here -- resolution picks the greatest
// effective_from <= a date, so inserting an earlier one just fills an earlier
// span. That is why 0046's guard, and the 409 whose advice named an undo that
// did not exist, are gone.
//
// Tenant comes from the CLASS, not the caller: store.TenantID returns 0 for a
// superadmin, which is right for reads and wrong for writes.
func recordScheduleChange(db *store.DB, c *core.Claims, id string, cl models.Class) (store.ScheduleVersion, error) {
	tw, twArgs := store.ScopeTenant(c, "")
	var tenantID int
	var day, tm, end string
	if err := db.QueryRow(`SELECT tenant_id,day,time,end_time FROM classes WHERE id=? AND deleted_at IS NULL`+tw, append([]any{id}, twArgs...)...).Scan(&tenantID, &day, &tm, &end); err != nil {
		return store.ScheduleVersion{}, err
	}
	if day == cl.Day && tm == cl.Time && end == cl.EndTime {
		return store.ScheduleVersion{Day: day, Time: tm, EndTime: end}, nil
	}
	// Same effective date twice: the later edit wins outright. The intermediate
	// schedule never applied to a real date, so there is nothing to preserve --
	// and re-editing back to the original values genuinely undoes the change.
	_, err := db.Exec(`INSERT INTO class_schedule_versions(id,tenant_id,class_id,effective_from,day,time,end_time,created_by,created_on)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT (tenant_id,class_id,effective_from)
		DO UPDATE SET day=EXCLUDED.day, time=EXCLUDED.time, end_time=EXCLUDED.end_time, created_by=EXCLUDED.created_by, created_on=EXCLUDED.created_on`,
		core.GenerateID("SV"), tenantID, id, cl.ScheduleFrom, cl.Day, cl.Time, cl.EndTime, c.Email, core.Today())
	if err != nil {
		return store.ScheduleVersion{}, err
	}
	core.LogAudit(db, tenantID, c.Email, "class_schedule_changed", "class", id, "was "+day+" "+tm+"-"+end+", now "+cl.Day+" "+cl.Time+"-"+cl.EndTime+" from "+cl.ScheduleFrom)
	return newestScheduleVersion(db, tenantID, id)
}

// newestScheduleVersion returns the class's latest declared schedule. The
// classes row mirrors it (the 0047 invariant), which matters for an
// OUT-OF-ORDER edit: adding a change effective before an existing one must not
// drag the class row backwards, because a later version still governs.
func newestScheduleVersion(db *store.DB, tenantID int, classID string) (store.ScheduleVersion, error) {
	var v store.ScheduleVersion
	err := db.QueryRow(`SELECT effective_from,day,time,end_time FROM class_schedule_versions WHERE tenant_id=? AND class_id=? ORDER BY effective_from DESC LIMIT 1`,
		tenantID, classID).Scan(&v.EffectiveFrom, &v.Day, &v.Time, &v.EndTime)
	return v, err
}

// syncCurrentScheduleVersion keeps the 0047 invariant: the version with the
// greatest effective_from mirrors the classes row. A plain (undated) edit is a
// retroactive correction, so it rewrites that newest version in place rather
// than opening a new span.
func syncCurrentScheduleVersion(db *store.DB, tenantID int, cl models.Class) error {
	var effectiveFrom string
	err := db.QueryRow(`SELECT effective_from FROM class_schedule_versions WHERE tenant_id=? AND class_id=? ORDER BY effective_from DESC LIMIT 1`, tenantID, cl.ID).Scan(&effectiveFrom)
	if errors.Is(err, sql.ErrNoRows) {
		// A class created before 0047 ran, or created since: seed its first
		// version at the sentinel epoch so every class has one.
		effectiveFrom = "0001-01-01"
	} else if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO class_schedule_versions(id,tenant_id,class_id,effective_from,day,time,end_time,created_by,created_on)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT (tenant_id,class_id,effective_from)
		DO UPDATE SET day=EXCLUDED.day, time=EXCLUDED.time, end_time=EXCLUDED.end_time`,
		core.GenerateID("SV"), tenantID, cl.ID, effectiveFrom, cl.Day, cl.Time, cl.EndTime, "sync", core.Today())
	return err
}

// listPricingTiers loads the type×level fee matrix for the snapshot, so the
// class form + Pricing screen can show fees and the cron can price students.
func listPricingTiers(db *store.DB, c *core.Claims) []models.PricingTier {
	tw, twArgs := store.ScopeTenant(c, "")
	rows, err := db.Query(`SELECT id,class_type,level_band,COALESCE(monthly_fee,0),COALESCE(hourly_rate,0) FROM pricing_tiers WHERE deleted_at IS NULL`+tw+` ORDER BY class_type, level_band`, twArgs...)
	return store.CollectRows(rows, err, "PricingTier", func(r *sql.Rows) (models.PricingTier, error) {
		var p models.PricingTier
		err := r.Scan(&p.ID, &p.ClassType, &p.LevelBand, &p.MonthlyFee, &p.HourlyRate)
		return p, err
	})
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
			if _, err := db.Exec(`INSERT INTO classes(id,tenant_id,name,teacher_ids,classroom,day,time,end_time,capacity,enrolled,color,category,class_type,level_band,subject,monthly_fee_override,session_rate) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				c.ID, tid, c.Name, models.JSONArr(c.TeacherIDs), c.Classroom, c.Day, c.Time, c.EndTime, c.Capacity, c.Enrolled, c.Color, c.Category, c.ClassType, c.LevelBand, c.Subject, c.MonthlyFeeOverride, c.SessionRate); err != nil {
				core.RespondError(w, "could not create class", 500)
				return
			}
			// Every class needs a version from the epoch, or a date before its
			// first schedule change would have nothing to resolve to (0047).
			if err := syncCurrentScheduleVersion(db, tid, c); err != nil {
				core.Logger.Error("seeding schedule version failed", "err", err, "class_id", c.ID)
			}
			core.LogAudit(db, store.TenantID(cl), cl.Email, "class_created", "class", c.ID, c.Name)
			core.Respond(w, c)
		}
	}
}

// classByID loads one class so an update can be decoded ON TOP of it.
// encoding/json leaves fields absent from the body untouched, so prefilling
// makes a partial PUT keep its stored values instead of zeroing them. That is
// the whole family of "editing a class quietly cleared X" bugs -- subject was
// the one that reached production (see calendar.js), and session_rate,
// level_band and monthly_fee_override had the same exposure.
func classByID(db *store.DB, c *core.Claims, id string) (models.Class, error) {
	tw, twArgs := store.ScopeTenant(c, "")
	var cl models.Class
	var tids string
	err := db.QueryRow(`SELECT id,name,teacher_ids,COALESCE(classroom,''),COALESCE(day,''),COALESCE(time,''),COALESCE(end_time,''),capacity,enrolled,COALESCE(color,''),COALESCE(category,''),COALESCE(class_type,'Group'),COALESCE(level_band,''),COALESCE(subject,''),COALESCE(monthly_fee_override,0),COALESCE(session_rate,0)
		FROM classes WHERE id=? AND deleted_at IS NULL`+tw, append([]any{id}, twArgs...)...).
		Scan(&cl.ID, &cl.Name, &tids, &cl.Classroom, &cl.Day, &cl.Time, &cl.EndTime, &cl.Capacity, &cl.Enrolled, &cl.Color, &cl.Category, &cl.ClassType, &cl.LevelBand, &cl.Subject, &cl.MonthlyFeeOverride, &cl.SessionRate)
	if err != nil {
		return cl, err
	}
	cl.TeacherIDs = models.ParseArr(tids)
	return cl, nil
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
			cl, err := classByID(db, c, id)
			if err != nil {
				core.RespondError(w, "class not found", http.StatusNotFound)
				return
			}
			// Decoded OVER the stored row: fields the client omits keep their
			// saved values rather than being zeroed.
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

			if cl.ScheduleFrom != "" {
				if !isoDate.MatchString(cl.ScheduleFrom) {
					core.RespondError(w, "scheduleFrom must be YYYY-MM-DD", http.StatusBadRequest)
					return
				}
				newest, err := recordScheduleChange(db, c, id, cl)
				if err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						core.RespondError(w, "class not found", http.StatusNotFound)
						return
					}
					core.Logger.Error("schedule version write failed", "err", err, "class_id", id)
					core.RespondError(w, "could not record the schedule change", http.StatusInternalServerError)
					return
				}
				// The classes row mirrors the LATEST declared schedule. For an
				// out-of-order edit the newest version is not the one just
				// written, so writing the submitted values would drag the row
				// backwards while a later version still governs.
				cl.Day, cl.Time, cl.EndTime = newest.Day, newest.Time, newest.EndTime
			}

			// NOTE: `enrolled` is deliberately NOT updated here. It is a derived
			// count maintained only by the authoritative recompute paths
			// (student add/edit/delete, registration approve). Trusting the
			// client-supplied cl.Enrolled here let a class edit silently
			// overwrite the true count, drifting capacity enforcement.
			args := append([]any{cl.Name, models.JSONArr(cl.TeacherIDs), cl.Classroom, cl.Day, cl.Time, cl.EndTime, cl.Capacity, cl.Color, cl.Category, cl.ClassType, cl.LevelBand, cl.Subject, cl.MonthlyFeeOverride, cl.SessionRate, id}, twArgs...)
			res, err := db.Exec(`UPDATE classes SET name=?,teacher_ids=?,classroom=?,day=?,time=?,end_time=?,capacity=?,color=?,category=?,class_type=?,level_band=?,subject=?,monthly_fee_override=?,session_rate=? WHERE id=?`+tw+` AND deleted_at IS NULL`, args...)
			if err != nil {
				core.RespondError(w, "could not update class", 500)
				return
			}
			if n, _ := res.RowsAffected(); n == 0 {
				core.RespondError(w, "class not found", 404)
				return
			}
			// An undated edit is a retroactive correction, so it rewrites the
			// newest version rather than opening a span. A dated edit already
			// wrote its own version above; this keeps the invariant either way.
			var rowTenant int
			db.QueryRow(`SELECT tenant_id FROM classes WHERE id=?`, id).Scan(&rowTenant)
			if err := syncCurrentScheduleVersion(db, rowTenant, cl); err != nil {
				core.Logger.Error("syncing schedule version failed", "err", err, "class_id", id)
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
