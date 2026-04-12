package main

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ── Self-study Sessions ───────────────────────────────────────────────────────

func parentStudentIDs(db *DB, email string) map[string]bool {
	rows, err := db.Query(`SELECT id FROM students WHERE contact=? AND deleted_at IS NULL`, email)
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
	tid := tenantID(c)
	rows, err := db.Query(`SELECT id,student_id,date,start_time,end_time,duration_min,notes FROM self_study_sessions WHERE (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY date DESC`, tid, tid)
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
				stuIDs := parentStudentIDs(db, c.Email)
				filtered := []SelfStudySession{}
				for _, s := range all {
					if stuIDs[s.StudentID] {
						filtered = append(filtered, s)
					}
				}
				respond(w, filtered)
				return
			}
			stuIDs := parentStudentIDs(db, c.Email)
			if !stuIDs[studentID] {
				respond(w, []SelfStudySession{})
				return
			}
			tid := tenantID(c)
			rows, err := db.Query(`SELECT id,student_id,date,start_time,end_time,duration_min,notes FROM self_study_sessions WHERE student_id=? AND (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY date DESC`, studentID, tid, tid)
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
		tid := tenantID(c)

		if studentID != "" {
			// Filtered by studentId — no pagination needed (small dataset)
			rows, err := db.Query(`SELECT id,student_id,date,start_time,end_time,duration_min,notes FROM self_study_sessions WHERE student_id=? AND (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY date DESC`, studentID, tid, tid)
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
		db.QueryRow(`SELECT COUNT(*) FROM self_study_sessions WHERE (tenant_id=? OR ?=0) AND deleted_at IS NULL`, tid, tid).Scan(&total)
		rows, err := db.Query(`SELECT id,student_id,date,start_time,end_time,duration_min,notes FROM self_study_sessions WHERE (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY date DESC LIMIT ? OFFSET ?`, tid, tid, p.Limit, p.Offset)
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
		if c == nil || (c.Role != "admin" && c.Role != "teacher") {
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
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "self_study_created", "self_study", s.ID, "student="+s.StudentID)
		}
		w.WriteHeader(http.StatusCreated)
		respond(w, s)
	}
}

func handleDeleteSelfStudy(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || (c.Role != "admin" && c.Role != "teacher") {
			respondError(w, "staff only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tid := tenantID(c)
		db.Exec(`UPDATE self_study_sessions SET deleted_at=NOW() WHERE id=? AND (tenant_id=? OR ?=0)`, id, tid, tid)
		if c := claimsFrom(r); c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "self_study_deleted", "self_study", id, "soft deleted")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
