package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/notify"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
)

// ── Self-study Sessions ───────────────────────────────────────────────────────

// parentStudentIDs returns the set of student IDs owned by the parent
// (matched on contact email) — scoped to the parent's tenant so an email
// shared across tenants cannot leak student ids.
func parentStudentIDs(db *store.DB, c *core.Claims) map[string]bool {
	if c == nil {
		return map[string]bool{}
	}
	tw, twArgs := store.ScopeTenant(c, "")
	args := append([]any{c.Email}, twArgs...)
	rows, err := db.Query(`SELECT id FROM students WHERE contact=? AND deleted_at IS NULL`+tw, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	ids := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids[id] = true
	}
	return ids
}

func listSelfStudy(db *store.DB, c *core.Claims) []models.SelfStudySession {
	tw, twArgs := store.ScopeTenant(c, "")
	rows, err := db.Query(`SELECT id,student_id,date,start_time,end_time,duration_min,notes FROM self_study_sessions WHERE deleted_at IS NULL`+tw+` ORDER BY date DESC`, twArgs...)
	return store.CollectRows(rows, err, "SelfStudySession", func(r *sql.Rows) (models.SelfStudySession, error) {
		var s models.SelfStudySession
		err := r.Scan(&s.ID, &s.StudentID, &s.Date, &s.StartTime, &s.EndTime, &s.DurationMin, &s.Notes)
		return s, err
	})
}

func HandleListSelfStudy(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		// Parents AND teachers are limited to a subset of students; only admins
		// are unrestricted. Teachers previously fell through to the admin path
		// and could read any student's self-study sessions in the tenant.
		isRestricted := c != nil && !core.IsAdminRole(c)
		restrictedIDs := map[string]bool{}
		if isRestricted {
			if c.Role == "teacher" {
				restrictedIDs = teacherStudentIDSet(db, c)
			} else {
				restrictedIDs = parentStudentIDs(db, c)
			}
		}

		studentID := r.URL.Query().Get("studentId")

		// Restricted roles use in-memory filtering — skip pagination
		if isRestricted {
			if studentID == "" {
				all := listSelfStudy(db, c)
				stuIDs := restrictedIDs
				filtered := []models.SelfStudySession{}
				for _, s := range all {
					if stuIDs[s.StudentID] {
						filtered = append(filtered, s)
					}
				}
				core.Respond(w, filtered)
				return
			}
			if !restrictedIDs[studentID] {
				core.Respond(w, []models.SelfStudySession{})
				return
			}
			tw, twArgs := store.ScopeTenant(c, "")
			args := append([]any{studentID}, twArgs...)
			rows, err := db.Query(`SELECT id,student_id,date,start_time,end_time,duration_min,notes FROM self_study_sessions WHERE student_id=?`+tw+` AND deleted_at IS NULL ORDER BY date DESC`, args...)
			if err != nil {
				core.Respond(w, []models.SelfStudySession{})
				return
			}
			defer rows.Close()
			out := []models.SelfStudySession{}
			for rows.Next() {
				var s models.SelfStudySession
				if err := rows.Scan(&s.ID, &s.StudentID, &s.Date, &s.StartTime, &s.EndTime, &s.DurationMin, &s.Notes); err != nil {
					continue
				}
				out = append(out, s)
			}
			core.Respond(w, out)
			return
		}

		// Admin/teacher path — supports pagination
		p := core.ParsePagination(r)
		tw, twArgs := store.ScopeTenant(c, "")

		if studentID != "" {
			// Filtered by studentId — no pagination needed (small dataset)
			args := append([]any{studentID}, twArgs...)
			rows, err := db.Query(`SELECT id,student_id,date,start_time,end_time,duration_min,notes FROM self_study_sessions WHERE student_id=?`+tw+` AND deleted_at IS NULL ORDER BY date DESC`, args...)
			if err != nil {
				core.Respond(w, []models.SelfStudySession{})
				return
			}
			defer rows.Close()
			out := []models.SelfStudySession{}
			for rows.Next() {
				var s models.SelfStudySession
				if err := rows.Scan(&s.ID, &s.StudentID, &s.Date, &s.StartTime, &s.EndTime, &s.DurationMin, &s.Notes); err != nil {
					continue
				}
				out = append(out, s)
			}
			core.Respond(w, out)
			return
		}

		if !p.Active {
			core.Respond(w, listSelfStudy(db, c))
			return
		}

		var total int
		db.QueryRow(`SELECT COUNT(*) FROM self_study_sessions WHERE deleted_at IS NULL`+tw, twArgs...).Scan(&total)
		pageArgs := append(append([]any{}, twArgs...), p.Limit, p.Offset)
		rows, err := db.Query(`SELECT id,student_id,date,start_time,end_time,duration_min,notes FROM self_study_sessions WHERE deleted_at IS NULL`+tw+` ORDER BY date DESC LIMIT ? OFFSET ?`, pageArgs...)
		if err != nil {
			core.Respond(w, core.PaginatedResponse{Data: []models.SelfStudySession{}, Total: total, Limit: p.Limit, Offset: p.Offset})
			return
		}
		defer rows.Close()
		out := []models.SelfStudySession{}
		for rows.Next() {
			var s models.SelfStudySession
			if err := rows.Scan(&s.ID, &s.StudentID, &s.Date, &s.StartTime, &s.EndTime, &s.DurationMin, &s.Notes); err != nil {
				continue
			}
			out = append(out, s)
		}
		core.Respond(w, core.PaginatedResponse{Data: out, Total: total, Limit: p.Limit, Offset: p.Offset})
	}
}

func HandleCreateSelfStudy(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsStaffRole(c) {
			core.RespondError(w, "staff only", 403)
			return
		}
		var s models.SelfStudySession
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		if msg := validationError("studentId", s.StudentID, "date", s.Date); msg != "" {
			core.RespondError(w, msg, 400)
			return
		}
		if !teacherMayActOnStudent(db, c, s.StudentID) {
			core.RespondError(w, "you can only log sessions for students in your own classes", http.StatusForbidden)
			return
		}
		if s.ID == "" {
			s.ID = core.GenerateID("SS")
		}
		tid := store.TenantID(c)
		_, err := db.Exec(`INSERT INTO self_study_sessions(id,tenant_id,student_id,date,start_time,end_time,duration_min,notes) VALUES(?,?,?,?,?,?,?,?)`,
			s.ID, tid, s.StudentID, s.Date, s.StartTime, s.EndTime, s.DurationMin, s.Notes)
		if err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		if c != nil {
			core.LogAudit(db, store.TenantID(c), c.Email, "self_study_created", "self_study", s.ID, "student="+s.StudentID)
		}
		// Notify the parent only for a live arrival — a session logged with a
		// start but no end yet. A fully backfilled session (end already set) is
		// historical data entry, so a real-time "checked in" would be wrong.
		if s.StartTime != "" && s.EndTime == "" {
			goSafe("notify_self_study_check_in", func() { notify.NotifySelfStudyCheckIn(db, tid, s.StudentID, s.StartTime) })
		}
		w.WriteHeader(http.StatusCreated)
		core.Respond(w, s)
	}
}

func HandleDeleteSelfStudy(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsStaffRole(c) {
			core.RespondError(w, "staff only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")
		// Resolve the session's student so a teacher can't delete another
		// class's rows (the read path is scoped, the delete path was not).
		var ownerStudent string
		ownArgs := append([]any{id}, twArgs...)
		db.QueryRow(`SELECT student_id FROM self_study_sessions WHERE id=?`+tw, ownArgs...).Scan(&ownerStudent)
		if !teacherMayActOnStudent(db, c, ownerStudent) {
			core.RespondError(w, "you can only delete sessions for students in your own classes", http.StatusForbidden)
			return
		}
		args := append([]any{id}, twArgs...)
		if _, err := db.Exec(`UPDATE self_study_sessions SET deleted_at=NOW() WHERE id=?`+tw, args...); err != nil {
			core.RespondError(w, "could not delete session", 500)
			return
		}
		if c := core.ClaimsFrom(r); c != nil {
			core.LogAudit(db, store.TenantID(c), c.Email, "self_study_deleted", "self_study", id, "soft deleted")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
