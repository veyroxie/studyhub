package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
	"time"

	"github.com/go-chi/chi/v5"
)

// checkWorkshopClash rejects a workshop create/update whose classroom or
// teacher overlaps with an existing class OR workshop on the same date and
// time window. Workshops share rooms with regular classes, so the check
// queries both tables. Empty room/time fields skip the corresponding check.
func checkWorkshopClash(db *store.DB, c *core.Claims, ws models.Workshop) error {
	if ws.Date == "" || ws.Time == "" || ws.EndTime == "" {
		return nil
	}
	tw, twArgs := store.ScopeTenant(c, "")

	if ws.Classroom != "" {
		// Classroom clash against other workshops on the same date.
		var cnt int
		wsArgs := append([]any{ws.Date, ws.Classroom, ws.ID, ws.Time, ws.EndTime}, twArgs...)
		if err := db.QueryRow(`SELECT COUNT(*) FROM workshops WHERE date=? AND classroom=? AND id!=? AND time<? AND end_time>? AND deleted_at IS NULL`+tw, wsArgs...).Scan(&cnt); err != nil {
			return errors.New("server error checking workshop conflicts")
		}
		if cnt > 0 {
			return errors.New("Conflict: " + ws.Classroom + " is already booked at this time")
		}
		// Classroom clash against regular classes on the same weekday.
		weekday := dateWeekday(ws.Date)
		if weekday != "" {
			clsArgs := append([]any{weekday, ws.Classroom, ws.Time, ws.EndTime}, twArgs...)
			if err := db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND classroom=? AND time<? AND end_time>? AND deleted_at IS NULL`+tw, clsArgs...).Scan(&cnt); err != nil {
				return errors.New("server error checking class conflicts")
			}
			if cnt > 0 {
				return errors.New("Conflict: " + ws.Classroom + " has a regular class at this time")
			}
		}
	}

	for _, tid2 := range ws.TeacherIDs {
		var cnt int
		teacherArgs := append([]any{ws.Date, ws.ID, ws.Time, ws.EndTime, tid2}, twArgs...)
		if err := db.QueryRow(`SELECT COUNT(*) FROM workshops WHERE date=? AND id!=? AND time<? AND end_time>? AND teacher_ids LIKE '%"'||?||'"%' AND deleted_at IS NULL`+tw, teacherArgs...).Scan(&cnt); err != nil {
			return errors.New("server error checking teacher conflicts")
		}
		if cnt > 0 {
			return errors.New("Conflict: teacher " + tid2 + " is already booked at this time")
		}
	}
	return nil
}

// dateWeekday returns the weekday name ("Monday", "Tuesday", ...) for a
// YYYY-MM-DD date string. Empty result means the input was malformed —
// callers treat that as "skip the weekday-based check".
func dateWeekday(date string) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return ""
	}
	return t.Weekday().String()
}

// ── Workshop CRUD ─────────────────────────────────────────────────────────────

func listWorkshops(db *store.DB, c *core.Claims) []models.Workshop {
	tw, twArgs := store.ScopeTenant(c, "")
	rows, err := db.Query(`SELECT id,name,description,date,time,end_time,classroom,capacity,enrolled,fee,teacher_ids,status FROM workshops WHERE deleted_at IS NULL`+tw+` ORDER BY date DESC LIMIT 5000`, twArgs...)
	if err != nil {
		return []models.Workshop{}
	}
	defer rows.Close()
	out := []models.Workshop{}
	for rows.Next() {
		var ws models.Workshop
		var tids string
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.Description, &ws.Date, &ws.Time, &ws.EndTime, &ws.Classroom, &ws.Capacity, &ws.Enrolled, &ws.Fee, &tids, &ws.Status); err != nil {
			continue
		}
		ws.TeacherIDs = models.ParseArr(tids)
		out = append(out, ws)
	}
	return out
}

func HandleListWorkshops(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		core.Respond(w, listWorkshops(db, c))
	}
}

func HandleCreateWorkshop(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		var ws models.Workshop
		if err := json.NewDecoder(r.Body).Decode(&ws); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		if msg := validationError("name", ws.Name, "date", ws.Date); msg != "" {
			core.RespondError(w, msg, 400)
			return
		}
		if ws.Capacity < 1 {
			ws.Capacity = 10
		}
		if ws.Time != "" && ws.EndTime != "" && ws.Time >= ws.EndTime {
			core.RespondError(w, "end time must be after start time", 400)
			return
		}
		if ws.ID == "" {
			ws.ID = core.GenerateID("ws")
		}
		if ws.Status == "" {
			ws.Status = "upcoming"
		}
		if err := checkWorkshopClash(db, c, ws); err != nil {
			core.RespondError(w, err.Error(), http.StatusConflict)
			return
		}
		tid := store.TenantID(c)
		_, err := db.Exec(`INSERT INTO workshops(id,tenant_id,name,description,date,time,end_time,classroom,capacity,enrolled,fee,teacher_ids,status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			ws.ID, tid, ws.Name, ws.Description, ws.Date, ws.Time, ws.EndTime, ws.Classroom, ws.Capacity, ws.Enrolled, ws.Fee, models.JSONArr(ws.TeacherIDs), ws.Status)
		if err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		if c != nil {
			core.LogAudit(db, c.Email, "workshop_created", "workshop", ws.ID, ws.Name)
		}
		w.WriteHeader(http.StatusCreated)
		core.Respond(w, ws)
	}
}

func HandleUpdateWorkshop(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var ws models.Workshop
		if err := json.NewDecoder(r.Body).Decode(&ws); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		ws.ID = id
		if ws.Time != "" && ws.EndTime != "" && ws.Time >= ws.EndTime {
			core.RespondError(w, "end time must be after start time", 400)
			return
		}
		if ws.Capacity > 0 && ws.Enrolled > ws.Capacity {
			core.RespondError(w, "enrolled count exceeds capacity", 400)
			return
		}
		if err := checkWorkshopClash(db, c, ws); err != nil {
			core.RespondError(w, err.Error(), http.StatusConflict)
			return
		}
		tw, twArgs := store.ScopeTenant(c, "")
		args := append([]any{ws.Name, ws.Description, ws.Date, ws.Time, ws.EndTime, ws.Classroom, ws.Capacity, ws.Enrolled, ws.Fee, models.JSONArr(ws.TeacherIDs), ws.Status, id}, twArgs...)
		res, err := db.Exec(`UPDATE workshops SET name=?,description=?,date=?,time=?,end_time=?,classroom=?,capacity=?,enrolled=?,fee=?,teacher_ids=?,status=? WHERE id=?`+tw, args...)
		if err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			core.RespondError(w, "workshop not found", 404)
			return
		}
		if c != nil {
			core.LogAudit(db, c.Email, "workshop_updated", "workshop", id, ws.Name)
		}
		core.Respond(w, ws)
	}
}

func HandleDeleteWorkshop(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")
		args := append([]any{id}, twArgs...)
		if _, err := db.Exec(`UPDATE workshops SET deleted_at=NOW() WHERE id=?`+tw, args...); err != nil {
			core.RespondError(w, "could not delete workshop", 500)
			return
		}
		if c != nil {
			core.LogAudit(db, c.Email, "workshop_deleted", "workshop", id, "soft deleted")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
