package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

// ── Class session overrides ───────────────────────────────────────────────────
//
// One dated session that differs from its recurring class. Today only the
// teachers differ; the table exists so "just this week" stops meaning "every
// week from now on". See migration 0040.

func listSessionOverrides(db *store.DB, c *core.Claims) []models.SessionOverride {
	tw, twArgs := store.ScopeTenant(c, "")
	rows, err := db.Query(`SELECT id,class_id,date,teacher_ids,note,created_by,created_on FROM class_session_overrides WHERE deleted_at IS NULL`+tw+` ORDER BY date DESC LIMIT 5000`, twArgs...)
	return store.CollectRows(rows, err, "SessionOverride", func(r *sql.Rows) (models.SessionOverride, error) {
		var so models.SessionOverride
		var tids string
		err := r.Scan(&so.ID, &so.ClassID, &so.Date, &tids, &so.Note, &so.CreatedBy, &so.CreatedOn)
		so.TeacherIDs = models.ParseArr(tids)
		return so, err
	})
}

func HandleListSessionOverrides(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		core.Respond(w, listSessionOverrides(db, core.ClaimsFrom(r)))
	}
}

// HandleUpsertSessionOverride records who actually taught one dated session.
// Upsert rather than create: swapping the same session twice should correct the
// record, not stack a second answer to "who taught this".
func HandleUpsertSessionOverride(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		var so models.SessionOverride
		if err := json.NewDecoder(r.Body).Decode(&so); err != nil {
			core.RespondError(w, "bad body", http.StatusBadRequest)
			return
		}
		if msg := validationError("classId", so.ClassID, "date", so.Date); msg != "" {
			core.RespondError(w, msg, http.StatusBadRequest)
			return
		}
		// An override naming nobody would read as "this session had no teacher",
		// which is indistinguishable from the class template being wrong. Removing
		// the override is how you go back to the usual teacher.
		if len(so.TeacherIDs) == 0 {
			core.RespondError(w, "pick at least one teacher, or remove the swap to go back to the usual teacher", http.StatusBadRequest)
			return
		}
		tid := store.TenantID(c)
		if so.ID == "" {
			so.ID = core.GenerateID("CSO")
		}
		so.CreatedBy = c.Email
		so.CreatedOn = core.Today()

		// ON CONFLICT targets the partial unique index from 0040, so a repeat swap
		// of the same session updates in place.
		if _, err := db.Exec(`
			INSERT INTO class_session_overrides(id,tenant_id,class_id,date,teacher_ids,note,created_by,created_on)
			VALUES(?,?,?,?,?,?,?,?)
			ON CONFLICT (tenant_id, class_id, date) WHERE deleted_at IS NULL
			DO UPDATE SET teacher_ids=EXCLUDED.teacher_ids, note=EXCLUDED.note,
			              created_by=EXCLUDED.created_by, created_on=EXCLUDED.created_on`,
			so.ID, tid, so.ClassID, so.Date, models.JSONArr(so.TeacherIDs), so.Note, so.CreatedBy, so.CreatedOn); err != nil {
			core.Logger.Error("session override upsert failed", "err", err, "class_id", so.ClassID, "date", so.Date)
			core.RespondError(w, "could not save the teacher swap", http.StatusInternalServerError)
			return
		}
		core.LogAudit(db, tid, c.Email, "session_override_saved", "class", so.ClassID, so.Date)
		core.Respond(w, so)
	}
}

// HandleDeleteSessionOverride puts a session back on its class's usual teachers.
func HandleDeleteSessionOverride(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")
		args := append([]any{id}, twArgs...)
		res, err := db.Exec(`UPDATE class_session_overrides SET deleted_at=NOW() WHERE id=?`+tw+` AND deleted_at IS NULL`, args...)
		if err != nil {
			core.RespondError(w, "could not remove the teacher swap", http.StatusInternalServerError)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			core.RespondError(w, "teacher swap not found", http.StatusNotFound)
			return
		}
		core.LogAudit(db, store.TenantID(c), c.Email, "session_override_removed", "class", id, "")
		core.Respond(w, map[string]string{"id": id})
	}
}
