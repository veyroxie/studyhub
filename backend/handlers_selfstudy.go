package main

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ── Self-study Sessions ───────────────────────────────────────────────────────

// parentStudentIDs returns the set of student IDs owned by the parent
// (matched on contact email) — scoped to the parent's tenant so an email
// shared across tenants cannot leak student ids.
func parentStudentIDs(db *DB, c *Claims) map[string]bool {
	if c == nil {
		return map[string]bool{}
	}
	tw, twArgs := scopeTenant(c, "")
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

func listSelfStudy(db *DB, c *Claims) []SelfStudySession {
	tw, twArgs := scopeTenant(c, "")
	rows, err := db.Query(`SELECT id,student_id,date,start_time,end_time,duration_min,notes FROM self_study_sessions WHERE deleted_at IS NULL`+tw+` ORDER BY date DESC`, twArgs...)
	if err != nil {
		return []SelfStudySession{}
	}
	defer rows.Close()
	out := []SelfStudySession{}
	for rows.Next() {
		var s SelfStudySession
		if err := rows.Scan(&s.ID, &s.StudentID, &s.Date, &s.StartTime, &s.EndTime, &s.DurationMin, &s.Notes); err != nil {
			continue
		}
		out = append(out, s)
	}
	return out
}

func handleListSelfStudy(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		isParent := c != nil && c.Role != "admin" && c.Role != "superadmin" && c.Role != "teacher"

		studentID := r.URL.Query().Get("studentId")

		// Parents use in-memory filtering — skip pagination
		if isParent {
			if studentID == "" {
				all := listSelfStudy(db, c)
				stuIDs := parentStudentIDs(db, c)
				filtered := []SelfStudySession{}
				for _, s := range all {
					if stuIDs[s.StudentID] {
						filtered = append(filtered, s)
					}
				}
				respond(w, filtered)
				return
			}
			stuIDs := parentStudentIDs(db, c)
			if !stuIDs[studentID] {
				respond(w, []SelfStudySession{})
				return
			}
			tw, twArgs := scopeTenant(c, "")
			args := append([]any{studentID}, twArgs...)
			rows, err := db.Query(`SELECT id,student_id,date,start_time,end_time,duration_min,notes FROM self_study_sessions WHERE student_id=?`+tw+` AND deleted_at IS NULL ORDER BY date DESC`, args...)
			if err != nil {
				respond(w, []SelfStudySession{})
				return
			}
			defer rows.Close()
			out := []SelfStudySession{}
			for rows.Next() {
				var s SelfStudySession
				if err := rows.Scan(&s.ID, &s.StudentID, &s.Date, &s.StartTime, &s.EndTime, &s.DurationMin, &s.Notes); err != nil {
					continue
				}
				out = append(out, s)
			}
			respond(w, out)
			return
		}

		// Admin/teacher path — supports pagination
		p := parsePagination(r)
		tw, twArgs := scopeTenant(c, "")

		if studentID != "" {
			// Filtered by studentId — no pagination needed (small dataset)
			args := append([]any{studentID}, twArgs...)
			rows, err := db.Query(`SELECT id,student_id,date,start_time,end_time,duration_min,notes FROM self_study_sessions WHERE student_id=?`+tw+` AND deleted_at IS NULL ORDER BY date DESC`, args...)
			if err != nil {
				respond(w, []SelfStudySession{})
				return
			}
			defer rows.Close()
			out := []SelfStudySession{}
			for rows.Next() {
				var s SelfStudySession
				if err := rows.Scan(&s.ID, &s.StudentID, &s.Date, &s.StartTime, &s.EndTime, &s.DurationMin, &s.Notes); err != nil {
					continue
				}
				out = append(out, s)
			}
			respond(w, out)
			return
		}

		if !p.Active {
			respond(w, listSelfStudy(db, c))
			return
		}

		var total int
		db.QueryRow(`SELECT COUNT(*) FROM self_study_sessions WHERE deleted_at IS NULL`+tw, twArgs...).Scan(&total)
		pageArgs := append(append([]any{}, twArgs...), p.Limit, p.Offset)
		rows, err := db.Query(`SELECT id,student_id,date,start_time,end_time,duration_min,notes FROM self_study_sessions WHERE deleted_at IS NULL`+tw+` ORDER BY date DESC LIMIT ? OFFSET ?`, pageArgs...)
		if err != nil {
			respond(w, PaginatedResponse{Data: []SelfStudySession{}, Total: total, Limit: p.Limit, Offset: p.Offset})
			return
		}
		defer rows.Close()
		out := []SelfStudySession{}
		for rows.Next() {
			var s SelfStudySession
			if err := rows.Scan(&s.ID, &s.StudentID, &s.Date, &s.StartTime, &s.EndTime, &s.DurationMin, &s.Notes); err != nil {
				continue
			}
			out = append(out, s)
		}
		respond(w, PaginatedResponse{Data: out, Total: total, Limit: p.Limit, Offset: p.Offset})
	}
}

func handleCreateSelfStudy(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if !isStaffRole(c) {
			respondError(w, "staff only", 403)
			return
		}
		var s SelfStudySession
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		if msg := validationError("studentId", s.StudentID, "date", s.Date); msg != "" {
			respondError(w, msg, 400)
			return
		}
		if s.ID == "" {
			s.ID = generateID("SS")
		}
		tid := tenantID(c)
		_, err := db.Exec(`INSERT INTO self_study_sessions(id,tenant_id,student_id,date,start_time,end_time,duration_min,notes) VALUES(?,?,?,?,?,?,?,?)`,
			s.ID, tid, s.StudentID, s.Date, s.StartTime, s.EndTime, s.DurationMin, s.Notes)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		if c != nil {
			logAudit(db, c.Email, "self_study_created", "self_study", s.ID, "student="+s.StudentID)
		}
		w.WriteHeader(http.StatusCreated)
		respond(w, s)
	}
}

func handleDeleteSelfStudy(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if !isStaffRole(c) {
			respondError(w, "staff only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := scopeTenant(c, "")
		args := append([]any{id}, twArgs...)
		if _, err := db.Exec(`UPDATE self_study_sessions SET deleted_at=NOW() WHERE id=?`+tw, args...); err != nil {
			respondError(w, "could not delete session", 500)
			return
		}
		if c := claimsFrom(r); c != nil {
			logAudit(db, c.Email, "self_study_deleted", "self_study", id, "soft deleted")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
