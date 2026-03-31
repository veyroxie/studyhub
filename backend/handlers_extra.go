package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

// ── Feedback CRUD ─────────────────────────────────────────────────────────────

func listFeedback(db *DB, c *Claims) []Feedback {
	tid := tenantID(c)
	rows, err := db.Query(`SELECT id,class_id,date,teacher_id,topic,mood,notes,student_notes FROM feedback WHERE deleted_at IS NULL AND (tenant_id=? OR ?=0) ORDER BY date DESC`, tid, tid)
	if err != nil {
		return []Feedback{}
	}
	defer rows.Close()
	out := []Feedback{}
	for rows.Next() {
		var f Feedback
		var sn string
		rows.Scan(&f.ID, &f.ClassID, &f.Date, &f.TeacherID, &f.Topic, &f.Mood, &f.Notes, &sn)
		if sn != "" {
			json.Unmarshal([]byte(sn), &f.StudentNotes)
		}
		if f.StudentNotes == nil {
			f.StudentNotes = []StudentNote{}
		}
		out = append(out, f)
	}
	return out
}

// parentClassIDs returns the set of class IDs the parent's children are enrolled in.
func parentClassIDs(db *DB, email string) map[string]bool {
	rows, err := db.Query(`SELECT enrolled_classes FROM students WHERE contact=? AND deleted_at IS NULL`, email)
	if err != nil {
		return nil
	}
	defer rows.Close()
	ids := map[string]bool{}
	for rows.Next() {
		var raw string
		rows.Scan(&raw)
		var arr []string
		json.Unmarshal([]byte(raw), &arr)
		for _, id := range arr {
			ids[id] = true
		}
	}
	return ids
}

// filterFeedbackForParent keeps only feedback for classes the parent's children attend.
func filterFeedbackForParent(all []Feedback, classIDs map[string]bool) []Feedback {
	if classIDs == nil {
		return []Feedback{}
	}
	out := []Feedback{}
	for _, f := range all {
		if classIDs[f.ClassID] {
			out = append(out, f)
		}
	}
	return out
}

func handleListFeedback(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		isParent := c != nil && c.Role != "admin" && c.Role != "superadmin" && c.Role != "teacher"

		date := r.URL.Query().Get("date")
		classID := r.URL.Query().Get("classId")

		// Parents use in-memory filtering — skip pagination for them
		if isParent {
			if date == "" && classID == "" {
				all := listFeedback(db, c)
				all = filterFeedbackForParent(all, parentClassIDs(db, c.Email))
				respond(w, all)
				return
			}
			tid := tenantID(c)
			q := `SELECT id,class_id,date,teacher_id,topic,mood,notes,student_notes FROM feedback WHERE deleted_at IS NULL AND (tenant_id=? OR ?=0)`
			args := []any{tid, tid}
			if date != "" {
				q += ` AND date=?`
				args = append(args, date)
			}
			if classID != "" {
				q += ` AND class_id=?`
				args = append(args, classID)
			}
			q += ` ORDER BY date DESC`
			rows, err := db.Query(q, args...)
			if err != nil {
				respond(w, []Feedback{})
				return
			}
			defer rows.Close()
			out := []Feedback{}
			for rows.Next() {
				var f Feedback
				var sn string
				rows.Scan(&f.ID, &f.ClassID, &f.Date, &f.TeacherID, &f.Topic, &f.Mood, &f.Notes, &sn)
				if sn != "" {
					json.Unmarshal([]byte(sn), &f.StudentNotes)
				}
				if f.StudentNotes == nil {
					f.StudentNotes = []StudentNote{}
				}
				out = append(out, f)
			}
			out = filterFeedbackForParent(out, parentClassIDs(db, c.Email))
			respond(w, out)
			return
		}

		// Admin/teacher path — supports pagination
		p := parsePagination(r)

		if date == "" && classID == "" && !p.Active {
			respond(w, listFeedback(db, c))
			return
		}

		tid := tenantID(c)
		where := `deleted_at IS NULL AND (tenant_id=? OR ?=0)`
		args := []any{tid, tid}
		if date != "" {
			where += ` AND date=?`
			args = append(args, date)
		}
		if classID != "" {
			where += ` AND class_id=?`
			args = append(args, classID)
		}

		if p.Active {
			var total int
			db.QueryRow(`SELECT COUNT(*) FROM feedback WHERE `+where, args...).Scan(&total)
			dataArgs := append(args, p.Limit, p.Offset)
			rows, err := db.Query(`SELECT id,class_id,date,teacher_id,topic,mood,notes,student_notes FROM feedback WHERE `+where+` ORDER BY date DESC LIMIT ? OFFSET ?`, dataArgs...)
			if err != nil {
				respond(w, PaginatedResponse{Data: []Feedback{}, Total: total, Limit: p.Limit, Offset: p.Offset})
				return
			}
			defer rows.Close()
			out := []Feedback{}
			for rows.Next() {
				var f Feedback
				var sn string
				rows.Scan(&f.ID, &f.ClassID, &f.Date, &f.TeacherID, &f.Topic, &f.Mood, &f.Notes, &sn)
				if sn != "" {
					json.Unmarshal([]byte(sn), &f.StudentNotes)
				}
				if f.StudentNotes == nil {
					f.StudentNotes = []StudentNote{}
				}
				out = append(out, f)
			}
			respond(w, PaginatedResponse{Data: out, Total: total, Limit: p.Limit, Offset: p.Offset})
			return
		}

		// Non-paginated with filters
		rows, err := db.Query(`SELECT id,class_id,date,teacher_id,topic,mood,notes,student_notes FROM feedback WHERE `+where+` ORDER BY date DESC`, args...)
		if err != nil {
			respond(w, []Feedback{})
			return
		}
		defer rows.Close()
		out := []Feedback{}
		for rows.Next() {
			var f Feedback
			var sn string
			rows.Scan(&f.ID, &f.ClassID, &f.Date, &f.TeacherID, &f.Topic, &f.Mood, &f.Notes, &sn)
			if sn != "" {
				json.Unmarshal([]byte(sn), &f.StudentNotes)
			}
			if f.StudentNotes == nil {
				f.StudentNotes = []StudentNote{}
			}
			out = append(out, f)
		}
		respond(w, out)
	}
}

func handleCreateFeedback(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || (c.Role != "admin" && c.Role != "teacher") {
			respondError(w, "staff only", 403)
			return
		}
		var f Feedback
		if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		if msg := validationError("classId", f.ClassID, "date", f.Date, "teacherId", f.TeacherID); msg != "" {
			respondError(w, msg, 400)
			return
		}
		if f.ID == "" {
			f.ID = generateID("FB")
		}
		snJSON, _ := json.Marshal(f.StudentNotes)
		if f.StudentNotes == nil {
			snJSON = []byte("[]")
		}
		tid := tenantID(c)
		_, err := db.Exec(`INSERT INTO feedback(id,tenant_id,class_id,date,teacher_id,topic,mood,notes,student_notes) VALUES(?,?,?,?,?,?,?,?,?)`,
			f.ID, tid, f.ClassID, f.Date, f.TeacherID, f.Topic, f.Mood, f.Notes, string(snJSON))
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		if c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "feedback_created", "feedback", f.ID, "class="+f.ClassID)
		}
		w.WriteHeader(http.StatusCreated)
		respond(w, f)
	}
}

func handleUpdateFeedback(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || (c.Role != "admin" && c.Role != "teacher") {
			respondError(w, "staff only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var f Feedback
		if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		f.ID = id
		snJSON, _ := json.Marshal(f.StudentNotes)
		if f.StudentNotes == nil {
			snJSON = []byte("[]")
		}
		tid := tenantID(c)
		res, err := db.Exec(`UPDATE feedback SET class_id=?,date=?,teacher_id=?,topic=?,mood=?,notes=?,student_notes=? WHERE id=? AND deleted_at IS NULL AND (tenant_id=? OR ?=0)`,
			f.ClassID, f.Date, f.TeacherID, f.Topic, f.Mood, f.Notes, string(snJSON), id, tid, tid)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			respondError(w, "feedback not found", 404)
			return
		}
		if c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "feedback_updated", "feedback", id, "")
		}
		respond(w, f)
	}
}

func handleDeleteFeedback(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || (c.Role != "admin" && c.Role != "teacher") {
			respondError(w, "staff only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tid := tenantID(c)
		db.Exec(`UPDATE feedback SET deleted_at=NOW() WHERE id=? AND (tenant_id=? OR ?=0)`, id, tid, tid)
		if c := claimsFrom(r); c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "feedback_deleted", "feedback", id, "soft deleted")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

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

// ── Workshop CRUD ─────────────────────────────────────────────────────────────

func listWorkshops(db *DB, c *Claims) []Workshop {
	tid := tenantID(c)
	rows, err := db.Query(`SELECT id,name,description,date,time,end_time,classroom,capacity,enrolled,fee,teacher_ids,status FROM workshops WHERE (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY date DESC`, tid, tid)
	if err != nil {
		return []Workshop{}
	}
	defer rows.Close()
	out := []Workshop{}
	for rows.Next() {
		var ws Workshop
		var tids string
		rows.Scan(&ws.ID, &ws.Name, &ws.Description, &ws.Date, &ws.Time, &ws.EndTime, &ws.Classroom, &ws.Capacity, &ws.Enrolled, &ws.Fee, &tids, &ws.Status)
		ws.TeacherIDs = parseArr(tids)
		out = append(out, ws)
	}
	return out
}

func handleListWorkshops(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		respond(w, listWorkshops(db, c))
	}
}

func handleCreateWorkshop(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		var ws Workshop
		if err := json.NewDecoder(r.Body).Decode(&ws); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		if msg := validationError("name", ws.Name, "date", ws.Date); msg != "" {
			respondError(w, msg, 400)
			return
		}
		if ws.ID == "" {
			ws.ID = generateID("ws")
		}
		if ws.Status == "" {
			ws.Status = "upcoming"
		}
		tid := tenantID(c)
		_, err := db.Exec(`INSERT INTO workshops(id,tenant_id,name,description,date,time,end_time,classroom,capacity,enrolled,fee,teacher_ids,status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			ws.ID, tid, ws.Name, ws.Description, ws.Date, ws.Time, ws.EndTime, ws.Classroom, ws.Capacity, ws.Enrolled, ws.Fee, jsonArr(ws.TeacherIDs), ws.Status)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		if c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "workshop_created", "workshop", ws.ID, ws.Name)
		}
		w.WriteHeader(http.StatusCreated)
		respond(w, ws)
	}
}

func handleUpdateWorkshop(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var ws Workshop
		if err := json.NewDecoder(r.Body).Decode(&ws); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		ws.ID = id
		tid := tenantID(c)
		res, err := db.Exec(`UPDATE workshops SET name=?,description=?,date=?,time=?,end_time=?,classroom=?,capacity=?,enrolled=?,fee=?,teacher_ids=?,status=? WHERE id=? AND (tenant_id=? OR ?=0)`,
			ws.Name, ws.Description, ws.Date, ws.Time, ws.EndTime, ws.Classroom, ws.Capacity, ws.Enrolled, ws.Fee, jsonArr(ws.TeacherIDs), ws.Status, id, tid, tid)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			respondError(w, "workshop not found", 404)
			return
		}
		if c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "workshop_updated", "workshop", id, ws.Name)
		}
		respond(w, ws)
	}
}

func handleDeleteWorkshop(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tid := tenantID(c)
		db.Exec(`UPDATE workshops SET deleted_at=NOW() WHERE id=? AND (tenant_id=? OR ?=0)`, id, tid, tid)
		if c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "workshop_deleted", "workshop", id, "soft deleted")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

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
		rows.Scan(&id)
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
		rows.Scan(&s.ID, &s.StudentID, &s.Date, &s.StartTime, &s.EndTime, &s.DurationMin, &s.Notes)
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
				rows.Scan(&s.ID, &s.StudentID, &s.Date, &s.StartTime, &s.EndTime, &s.DurationMin, &s.Notes)
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
				rows.Scan(&s.ID, &s.StudentID, &s.Date, &s.StartTime, &s.EndTime, &s.DurationMin, &s.Notes)
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
			rows.Scan(&s.ID, &s.StudentID, &s.Date, &s.StartTime, &s.EndTime, &s.DurationMin, &s.Notes)
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

// ── Performance Reviews ───────────────────────────────────────────────────────

func listPerformanceReviews(db *DB, c *Claims) []PerformanceReview {
	tid := tenantID(c)
	rows, err := db.Query(`SELECT id,staff_id,reviewer_email,date,rating,parent_rating,notes FROM performance_reviews WHERE (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY date DESC`, tid, tid)
	if err != nil {
		return []PerformanceReview{}
	}
	defer rows.Close()
	out := []PerformanceReview{}
	for rows.Next() {
		var p PerformanceReview
		rows.Scan(&p.ID, &p.StaffID, &p.ReviewerEmail, &p.Date, &p.Rating, &p.ParentRating, &p.Notes)
		out = append(out, p)
	}
	return out
}

func handleListPerformanceReviews(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		// Parents cannot view staff performance reviews
		if c != nil && c.Role != "admin" && c.Role != "superadmin" && c.Role != "teacher" {
			respond(w, []PerformanceReview{})
			return
		}
		staffID := r.URL.Query().Get("staffId")
		if staffID != "" {
			// Filtered by staffId — small dataset, no pagination
			tid := tenantID(c)
			rows, err := db.Query(`SELECT id,staff_id,reviewer_email,date,rating,parent_rating,notes FROM performance_reviews WHERE staff_id=? AND (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY date DESC`, staffID, tid, tid)
			if err != nil {
				respond(w, []PerformanceReview{})
				return
			}
			defer rows.Close()
			out := []PerformanceReview{}
			for rows.Next() {
				var pr PerformanceReview
				rows.Scan(&pr.ID, &pr.StaffID, &pr.ReviewerEmail, &pr.Date, &pr.Rating, &pr.ParentRating, &pr.Notes)
				out = append(out, pr)
			}
			respond(w, out)
			return
		}

		pg := parsePagination(r)
		if !pg.Active {
			respond(w, listPerformanceReviews(db, c))
			return
		}

		tid := tenantID(c)
		var total int
		db.QueryRow(`SELECT COUNT(*) FROM performance_reviews WHERE (tenant_id=? OR ?=0) AND deleted_at IS NULL`, tid, tid).Scan(&total)
		rows, err := db.Query(`SELECT id,staff_id,reviewer_email,date,rating,parent_rating,notes FROM performance_reviews WHERE (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY date DESC LIMIT ? OFFSET ?`, tid, tid, pg.Limit, pg.Offset)
		if err != nil {
			respond(w, PaginatedResponse{Data: []PerformanceReview{}, Total: total, Limit: pg.Limit, Offset: pg.Offset})
			return
		}
		defer rows.Close()
		out := []PerformanceReview{}
		for rows.Next() {
			var pr PerformanceReview
			rows.Scan(&pr.ID, &pr.StaffID, &pr.ReviewerEmail, &pr.Date, &pr.Rating, &pr.ParentRating, &pr.Notes)
			out = append(out, pr)
		}
		respond(w, PaginatedResponse{Data: out, Total: total, Limit: pg.Limit, Offset: pg.Offset})
	}
}

func handleCreatePerformanceReview(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || (c.Role != "admin" && c.Role != "teacher") {
			respondError(w, "staff only", 403)
			return
		}
		var p PerformanceReview
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		if msg := validationError("staffId", p.StaffID); msg != "" {
			respondError(w, msg, 400)
			return
		}
		if p.ID == "" {
			p.ID = generateID("PR")
		}
		if p.Date == "" {
			p.Date = today()
		}
		if p.ReviewerEmail == "" && c != nil {
			p.ReviewerEmail = c.Email
		}
		tid := tenantID(c)
		_, err := db.Exec(`INSERT INTO performance_reviews(id,tenant_id,staff_id,reviewer_email,date,rating,parent_rating,notes) VALUES(?,?,?,?,?,?,?,?)`,
			p.ID, tid, p.StaffID, p.ReviewerEmail, p.Date, p.Rating, p.ParentRating, p.Notes)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		if c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "performance_review_created", "performance_review", p.ID, "staff="+p.StaffID)
		}
		w.WriteHeader(http.StatusCreated)
		respond(w, p)
	}
}

func handleDeletePerformanceReview(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tid := tenantID(c)
		db.Exec(`UPDATE performance_reviews SET deleted_at=NOW() WHERE id=? AND (tenant_id=? OR ?=0)`, id, tid, tid)
		if c := claimsFrom(r); c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "performance_review_deleted", "performance_review", id, "soft deleted")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── Cancelled Classes ─────────────────────────────────────────────────────────

func listCancelledClasses(db *DB, c *Claims) []CancelledClass {
	tid := tenantID(c)
	rows, err := db.Query(`SELECT id,class_id,date,reason,cancelled_by,created_on FROM cancelled_classes WHERE (tenant_id=? OR ?=0) ORDER BY date DESC`, tid, tid)
	if err != nil {
		return []CancelledClass{}
	}
	defer rows.Close()
	out := []CancelledClass{}
	for rows.Next() {
		var cc CancelledClass
		rows.Scan(&cc.ID, &cc.ClassID, &cc.Date, &cc.Reason, &cc.CancelledBy, &cc.CreatedOn)
		out = append(out, cc)
	}
	return out
}

func handleListCancelledClasses(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		respond(w, listCancelledClasses(db, c))
	}
}

func handleCreateCancelledClass(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || (c.Role != "admin" && c.Role != "superadmin") {
			respondError(w, "admin only", 403)
			return
		}
		var cc CancelledClass
		if err := json.NewDecoder(r.Body).Decode(&cc); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		if msg := validationError("classId", cc.ClassID, "date", cc.Date); msg != "" {
			respondError(w, msg, 400)
			return
		}
		if cc.ID == "" {
			cc.ID = generateID("CC")
		}
		if cc.CreatedOn == "" {
			cc.CreatedOn = today()
		}
		if cc.CancelledBy == "" && c != nil {
			cc.CancelledBy = c.Email
		}
		tid := tenantID(c)
		_, err := db.Exec(`INSERT INTO cancelled_classes(id,tenant_id,class_id,date,reason,cancelled_by,created_on) VALUES(?,?,?,?,?,?,?)`,
			cc.ID, tid, cc.ClassID, cc.Date, cc.Reason, cc.CancelledBy, cc.CreatedOn)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		if c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "class_cancelled", "cancelled_class", cc.ID, "class="+cc.ClassID+" date="+cc.Date)
		}
		w.WriteHeader(http.StatusCreated)
		respond(w, cc)
	}
}

// ── Class update/delete (missing from existing handlers) ──────────────────────

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

// ── Staff update/delete (missing from existing handlers) ──────────────────────

func handleStaffByID(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		id := chi.URLParam(r, "id")
		switch r.Method {
		case http.MethodPut:
			if c == nil || c.Role != "admin" {
				respondError(w, "admin only", 403)
				return
			}
			var s Staff
			if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			s.ID = id
			if s.EmploymentType == "" {
				s.EmploymentType = "Full-time"
			}
			tid := tenantID(c)
			db.Exec(`UPDATE staff SET name=?,full_name=?,role=?,email=?,phone=?,salary=?,join_date=?,status=?,specialization=?,nric=?,emergency_name=?,emergency_phone=?,employment_type=?,hourly_rate=? WHERE id=? AND (tenant_id=? OR ?=0)`,
				s.Name, s.FullName, s.Role, s.Email, s.Phone, s.Salary, s.JoinDate, s.Status, s.Specialization, s.NRIC, s.EmergencyName, s.EmergencyPhone, s.EmploymentType, s.HourlyRate, id, tid, tid)
			if c != nil {
				db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
					c.Email, "staff_updated", "staff", id, s.Name)
			}
			respond(w, s)

		case http.MethodDelete:
			if c == nil || c.Role != "admin" {
				respondError(w, "admin only", 403)
				return
			}
			tid := tenantID(c)
			db.Exec(`UPDATE staff SET deleted_at=NOW() WHERE id=? AND (tenant_id=? OR ?=0)`, id, tid, tid)
			if c != nil {
				db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
					c.Email, "staff_deleted", "staff", id, "soft deleted")
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// ── Password Reset (public endpoint) ──────────────────────────────────────────

func handleForgotPassword(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		req.Email = strings.ToLower(strings.TrimSpace(req.Email))
		if !validateEmail(req.Email) {
			respondError(w, "invalid email", 400)
			return
		}

		// Always return same response to prevent account enumeration
		var userID int
		err := db.QueryRow(`SELECT id FROM users WHERE email=?`, req.Email).Scan(&userID)
		if err != nil {
			// User not found — return generic success to prevent enumeration
			respond(w, map[string]string{"message": "If an account with that email exists, a temporary password has been generated. Please contact admin."})
			return
		}

		// Generate random 8-char alphanumeric password
		rawBytes := make([]byte, 4)
		if _, err := rand.Read(rawBytes); err != nil {
			respondError(w, "could not generate password", 500)
			return
		}
		tempPassword := "Sh-" + hex.EncodeToString(rawBytes) // e.g. Sh-a3f8c2d1

		hash, err := bcrypt.GenerateFromPassword([]byte(tempPassword), bcrypt.DefaultCost)
		if err != nil {
			respondError(w, "could not hash password", 500)
			return
		}
		_, err = db.Exec(`UPDATE users SET password_hash=? WHERE id=?`, string(hash), userID)
		if err != nil {
			respondError(w, "could not update password", 500)
			return
		}

		// Audit log (no claims since public endpoint)
		db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
			req.Email, "password_reset", "user", fmt.Sprintf("%d", userID), "temp password generated")

		// Return temp password — admin shares it manually (no email service yet)
		respond(w, map[string]string{"tempPassword": tempPassword, "message": "Temporary password generated. Share with user securely."})
	}
}

// ── Teacher Registration (public endpoint) ────────────────────────────────────

// ── Payment Proof Upload ──────────────────────────────────────────────────────

func handleUploadProof(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Limit request body to 5MB
		r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
		if err := r.ParseMultipartForm(5 << 20); err != nil {
			respondError(w, "file too large (max 5MB)", 400)
			return
		}
		invoiceID := r.FormValue("invoiceId")
		if invoiceID == "" {
			respondError(w, "invoiceId is required", 400)
			return
		}

		// Verify invoice exists and check ownership for parents
		c := claimsFrom(r)
		var studentID string
		if err := db.QueryRow(`SELECT student_id FROM invoices WHERE id=? AND deleted_at IS NULL`, invoiceID).Scan(&studentID); err != nil {
			respondError(w, "invoice not found", 404)
			return
		}
		if c != nil && c.Role == "parent" {
			var ownerEmail string
			db.QueryRow(`SELECT contact FROM students WHERE id=?`, studentID).Scan(&ownerEmail)
			if ownerEmail != c.Email {
				respondError(w, "not your invoice", 403)
				return
			}
		}

		file, header, err := r.FormFile("proof")
		if err != nil {
			respondError(w, "proof file is required", 400)
			return
		}
		defer file.Close()

		// Validate file type
		ext := strings.ToLower(filepath.Ext(header.Filename))
		allowed := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".pdf": true}
		if !allowed[ext] {
			respondError(w, "only jpg, jpeg, png, pdf files are allowed", 400)
			return
		}

		// Validate MIME type by reading file header (magic bytes)
		buf := make([]byte, 512)
		n, _ := file.Read(buf)
		mime := http.DetectContentType(buf[:n])
		file.Seek(0, io.SeekStart) // reset reader position
		allowedMIME := map[string]bool{
			"image/jpeg":      true,
			"image/png":       true,
			"application/pdf": true,
		}
		if !allowedMIME[mime] {
			respondError(w, "file content does not match allowed types (jpg, png, pdf)", 400)
			return
		}

		// Create uploads directory
		if err := os.MkdirAll("uploads", 0755); err != nil {
			respondError(w, "could not create uploads directory", 500)
			return
		}

		// Save file
		ts := time.Now().Unix()
		filename := fmt.Sprintf("proof_%s_%d%s", invoiceID, ts, ext)
		savePath := filepath.Join("uploads", filename)
		dst, err := os.Create(savePath)
		if err != nil {
			respondError(w, "could not save file", 500)
			return
		}
		defer dst.Close()
		if _, err := io.Copy(dst, file); err != nil {
			respondError(w, "could not write file", 500)
			return
		}

		// Update invoice record
		proofPath := "uploads/" + filename
		tid := tenantID(c)
		db.Exec(`UPDATE invoices SET payment_proof=? WHERE id=? AND (tenant_id=? OR ?=0)`, proofPath, invoiceID, tid, tid)

		// Audit log
		if c := claimsFrom(r); c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "proof_uploaded", "invoice", invoiceID, proofPath)
		}

		respond(w, map[string]string{"path": proofPath})
	}
}

func handleServeUpload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filename := chi.URLParam(r, "filename")
		// Sanitize: only allow alphanumeric, underscores, dots, dashes
		for _, c := range filename {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-') {
				respondError(w, "invalid filename", 400)
				return
			}
		}
		filePath := filepath.Join("uploads", filename)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			respondError(w, "file not found", 404)
			return
		}

		ext := strings.ToLower(filepath.Ext(filename))
		switch ext {
		case ".jpg", ".jpeg":
			w.Header().Set("Content-Type", "image/jpeg")
		case ".png":
			w.Header().Set("Content-Type", "image/png")
		case ".pdf":
			w.Header().Set("Content-Type", "application/pdf")
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", "inline; filename="+filename)
		http.ServeFile(w, r, filePath)
	}
}

// ── Holiday CRUD ──────────────────────────────────────────────────────────────

func listHolidays(db *DB, c *Claims) []Holiday {
	tid := tenantID(c)
	rows, err := db.Query(`SELECT id,name,date,end_date,type,notes,created_by FROM holidays WHERE (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY date`, tid, tid)
	if err != nil {
		return []Holiday{}
	}
	defer rows.Close()
	out := []Holiday{}
	for rows.Next() {
		var h Holiday
		var endDate, notes, createdBy sql.NullString
		rows.Scan(&h.ID, &h.Name, &h.Date, &endDate, &h.Type, &notes, &createdBy)
		h.EndDate = nullStr(endDate)
		h.Notes = nullStr(notes)
		h.CreatedBy = nullStr(createdBy)
		if h.Type == "" {
			h.Type = "holiday"
		}
		out = append(out, h)
	}
	return out
}

func handleListHolidays(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		respond(w, listHolidays(db, c))
	}
}

func handleCreateHoliday(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		var h Holiday
		if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		if msg := validationError("name", h.Name, "date", h.Date); msg != "" {
			respondError(w, msg, 400)
			return
		}
		if h.ID == "" {
			h.ID = generateID("HOL")
		}
		if h.Type == "" {
			h.Type = "holiday"
		}
		if h.CreatedBy == "" && c != nil {
			h.CreatedBy = c.Email
		}
		tid := tenantID(c)
		_, err := db.Exec(`INSERT INTO holidays(id,tenant_id,name,date,end_date,type,notes,created_by) VALUES(?,?,?,?,?,?,?,?)`,
			h.ID, tid, h.Name, h.Date, h.EndDate, h.Type, h.Notes, h.CreatedBy)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
			c.Email, "holiday_created", "holiday", h.ID, h.Name+" on "+h.Date)
		w.WriteHeader(http.StatusCreated)
		respond(w, h)
	}
}

func handleUpdateHoliday(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var h Holiday
		if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		h.ID = id
		if h.Type == "" {
			h.Type = "holiday"
		}
		tid := tenantID(c)
		res, err := db.Exec(`UPDATE holidays SET name=?,date=?,end_date=?,type=?,notes=? WHERE id=? AND (tenant_id=? OR ?=0)`,
			h.Name, h.Date, h.EndDate, h.Type, h.Notes, id, tid, tid)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			respondError(w, "holiday not found", 404)
			return
		}
		db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
			c.Email, "holiday_updated", "holiday", id, h.Name)
		respond(w, h)
	}
}

func handleDeleteHoliday(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tid := tenantID(c)
		db.Exec(`UPDATE holidays SET deleted_at=NOW() WHERE id=? AND (tenant_id=? OR ?=0)`, id, tid, tid)
		db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
			c.Email, "holiday_deleted", "holiday", id, "soft deleted")
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── Replacement Credits ──────────────────────────────────────────────────────

func listReplacementCredits(db *DB, c *Claims) []ReplacementCredit {
	tid := tenantID(c)
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		rows, err = db.Query(`SELECT rc.id,rc.student_id,rc.type,rc.minutes,rc.note,rc.class_id,rc.date,rc.created_by FROM replacement_credits rc JOIN students s ON s.id=rc.student_id WHERE s.contact=? AND (rc.tenant_id=? OR ?=0) ORDER BY rc.created_at DESC`, c.Email, tid, tid)
	} else {
		rows, err = db.Query(`SELECT id,student_id,type,minutes,note,class_id,date,created_by FROM replacement_credits WHERE (tenant_id=? OR ?=0) ORDER BY created_at DESC`, tid, tid)
	}
	if err != nil {
		return []ReplacementCredit{}
	}
	defer rows.Close()
	out := []ReplacementCredit{}
	for rows.Next() {
		var rc ReplacementCredit
		rows.Scan(&rc.ID, &rc.StudentID, &rc.Type, &rc.Minutes, &rc.Note, &rc.ClassID, &rc.Date, &rc.CreatedBy)
		out = append(out, rc)
	}
	return out
}

func handleListReplacementCredits(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		studentID := r.URL.Query().Get("studentId")
		if studentID == "" {
			respond(w, listReplacementCredits(db, c))
			return
		}
		if c != nil && c.Role == "parent" {
			stuIDs := parentStudentIDs(db, c.Email)
			if !stuIDs[studentID] {
				respond(w, []ReplacementCredit{})
				return
			}
		}
		tid := tenantID(c)
		rows, err := db.Query(`SELECT id,student_id,type,minutes,note,class_id,date,created_by FROM replacement_credits WHERE student_id=? AND (tenant_id=? OR ?=0) ORDER BY created_at DESC`, studentID, tid, tid)
		if err != nil {
			respond(w, []ReplacementCredit{})
			return
		}
		defer rows.Close()
		out := []ReplacementCredit{}
		for rows.Next() {
			var rc ReplacementCredit
			rows.Scan(&rc.ID, &rc.StudentID, &rc.Type, &rc.Minutes, &rc.Note, &rc.ClassID, &rc.Date, &rc.CreatedBy)
			out = append(out, rc)
		}
		respond(w, out)
	}
}

func handleCreateReplacementCredit(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || (c.Role != "admin" && c.Role != "teacher") {
			respondError(w, "staff only", 403)
			return
		}
		var rc ReplacementCredit
		if err := json.NewDecoder(r.Body).Decode(&rc); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		if msg := validationError("studentId", rc.StudentID, "type", rc.Type, "date", rc.Date); msg != "" {
			respondError(w, msg, 400)
			return
		}
		if rc.Type != "earned" && rc.Type != "used" {
			respondError(w, "type must be 'earned' or 'used'", 400)
			return
		}
		if rc.Minutes <= 0 {
			respondError(w, "minutes must be greater than 0", 400)
			return
		}
		if rc.ID == "" {
			rc.ID = generateID("RC")
		}
		if rc.CreatedBy == "" && c != nil {
			rc.CreatedBy = c.Email
		}
		tid := tenantID(c)
		if rc.Type == "used" {
			tx, err := db.BeginTx(r.Context())
			if err != nil {
				respondError(w, "server error", 500)
				return
			}
			defer tx.Rollback()

			var earned, used int
			tx.QueryRow(`SELECT COALESCE(SUM(CASE WHEN type='earned' THEN minutes ELSE 0 END),0), COALESCE(SUM(CASE WHEN type='used' THEN minutes ELSE 0 END),0) FROM replacement_credits WHERE student_id=? AND (tenant_id=? OR ?=0)`, rc.StudentID, tid, tid).Scan(&earned, &used)
			balance := earned - used
			if rc.Minutes > balance {
				respondError(w, fmt.Sprintf("insufficient balance: %d min available, trying to use %d min", balance, rc.Minutes), 400)
				return
			}

			_, err = tx.Exec(`INSERT INTO replacement_credits(id,tenant_id,student_id,type,minutes,note,class_id,date,created_by) VALUES(?,?,?,?,?,?,?,?,?)`,
				rc.ID, tid, rc.StudentID, rc.Type, rc.Minutes, rc.Note, rc.ClassID, rc.Date, rc.CreatedBy)
			if err != nil {
				respondError(w, "server error", 500)
				return
			}
			if err := tx.Commit(); err != nil {
				respondError(w, "server error", 500)
				return
			}
		} else {
			_, err := db.Exec(`INSERT INTO replacement_credits(id,tenant_id,student_id,type,minutes,note,class_id,date,created_by) VALUES(?,?,?,?,?,?,?,?,?)`,
				rc.ID, tid, rc.StudentID, rc.Type, rc.Minutes, rc.Note, rc.ClassID, rc.Date, rc.CreatedBy)
			if err != nil {
				respondError(w, "server error", 500)
				return
			}
		}
		db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
			c.Email, "replacement_credit_"+rc.Type, "replacement_credit", rc.ID, fmt.Sprintf("student=%s minutes=%d", rc.StudentID, rc.Minutes))
		w.WriteHeader(http.StatusCreated)
		respond(w, rc)
	}
}

func handleDeleteReplacementCredit(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tid := tenantID(c)
		db.Exec(`DELETE FROM replacement_credits WHERE id=? AND (tenant_id=? OR ?=0)`, id, tid, tid)
		db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
			c.Email, "replacement_credit_deleted", "replacement_credit", id, "deleted")
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleReplacementBalance(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		studentID := r.URL.Query().Get("studentId")
		if studentID == "" {
			respondError(w, "studentId is required", 400)
			return
		}
		if c != nil && c.Role == "parent" {
			stuIDs := parentStudentIDs(db, c.Email)
			if !stuIDs[studentID] {
				respondError(w, "not your student", 403)
				return
			}
		}
		tid := tenantID(c)
		var earned, used int
		db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN type='earned' THEN minutes ELSE 0 END),0), COALESCE(SUM(CASE WHEN type='used' THEN minutes ELSE 0 END),0) FROM replacement_credits WHERE student_id=? AND (tenant_id=? OR ?=0)`, studentID, tid, tid).Scan(&earned, &used)
		respond(w, map[string]int{"earned": earned, "used": used, "balance": earned - used})
	}
}

func handleRegisterTeacher(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var reg struct {
			FullName       string `json:"fullName"`
			DisplayName    string `json:"displayName"`
			Email          string `json:"email"`
			Phone          string `json:"phone"`
			Specialty      string `json:"specialty"`
			EmploymentType string `json:"employmentType"`
			Experience     string `json:"experience"`
			Qualifications string `json:"qualifications"`
			Bio            string `json:"bio"`
			Schedule       string `json:"schedule"`
			ExpectedSalary string `json:"expectedSalary"`
			EmergencyName  string `json:"emergencyName"`
			EmergencyPhone string `json:"emergencyPhone"`
			NRIC           string `json:"nric"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
			respondError(w, "bad request", 400)
			return
		}
		if reg.FullName == "" || reg.Email == "" {
			respondError(w, "full name and email are required", 400)
			return
		}
		if !validateEmail(reg.Email) {
			respondError(w, "invalid email address", 400)
			return
		}
		if reg.DisplayName == "" {
			reg.DisplayName = reg.FullName
		}
		if reg.EmploymentType == "" {
			reg.EmploymentType = "Full-time"
		}

		id := generateID("REG")
		_, err := db.Exec(`INSERT INTO registrations(id,parent_name,email,phone,emergency_name,emergency_phone,student_first_name,student_last_name,student_dob,student_gender,gender,school_name,year_grade,class_type_interest,subject_interest,school_fees,registration_date,workshop_interest,class_interest,notes,submitted_on,status,type,specialization,nric,display_name,employment_type,experience,qualifications,bio,schedule,expected_salary) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, reg.FullName, reg.Email, reg.Phone, reg.EmergencyName, reg.EmergencyPhone,
			reg.FullName, "", "", "", "", "", "", "", "", 0, "", "",
			"", reg.Bio, today(), "pending", "teacher",
			reg.Specialty, reg.NRIC, reg.DisplayName, reg.EmploymentType, reg.Experience, reg.Qualifications, reg.Bio, reg.Schedule, reg.ExpectedSalary)
		if err != nil {
			respondError(w, "could not save registration", 500)
			return
		}
		w.WriteHeader(http.StatusCreated)
		respond(w, map[string]string{"id": id, "status": "pending", "type": "teacher"})
	}
}

// ── Families ─────────────────────────────────────────────────────────────────

func listFamilies(db *DB, c *Claims) []Family {
	tid := tenantID(c)
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		rows, err = db.Query(`SELECT id,name,contact,phone,parent_name,COALESCE(address,''),COALESCE(notes,'') FROM families WHERE contact=? AND (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY name`, c.Email, tid, tid)
	} else {
		rows, err = db.Query(`SELECT id,name,contact,phone,parent_name,COALESCE(address,''),COALESCE(notes,'') FROM families WHERE (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY name`, tid, tid)
	}
	if err != nil {
		return []Family{}
	}
	defer rows.Close()
	out := []Family{}
	for rows.Next() {
		var f Family
		rows.Scan(&f.ID, &f.Name, &f.Contact, &f.Phone, &f.ParentName, &f.Address, &f.Notes)
		out = append(out, f)
	}
	return out
}

func handleFamilies(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			respond(w, listFamilies(db, c))
		case http.MethodPost:
			if c.Role != "admin" {
				respondError(w, "admin only", 403)
				return
			}
			var f Family
			if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			if msg := validationError("name", f.Name, "contact", f.Contact); msg != "" {
				respondError(w, msg, 400)
				return
			}
			if f.ID == "" {
				f.ID = generateID("FAM")
			}
			tid := tenantID(c)
			_, err := db.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name,address,notes) VALUES(?,?,?,?,?,?,?,?)`,
				f.ID, tid, f.Name, f.Contact, f.Phone, f.ParentName, f.Address, f.Notes)
			if err != nil {
				respondError(w, "server error", 500)
				return
			}
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "family_created", "family", f.ID, f.Name)
			w.WriteHeader(http.StatusCreated)
			respond(w, f)
		}
	}
}

func handleFamilyByID(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tid := tenantID(c)
		switch r.Method {
		case http.MethodPut:
			var f Family
			if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			f.ID = id
			if _, err := db.Exec(`UPDATE families SET name=?,contact=?,phone=?,parent_name=?,address=?,notes=? WHERE id=? AND (tenant_id=? OR ?=0)`,
				f.Name, f.Contact, f.Phone, f.ParentName, f.Address, f.Notes, id, tid, tid); err != nil {
				respondError(w, "could not update family", 500)
				return
			}
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "family_updated", "family", id, f.Name)
			respond(w, f)
		case http.MethodDelete:
			if _, err := db.Exec(`UPDATE families SET deleted_at=NOW() WHERE id=? AND (tenant_id=? OR ?=0)`, id, tid, tid); err != nil {
				respondError(w, "could not delete family", 500)
				return
			}
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "family_deleted", "family", id, "soft deleted")
			w.WriteHeader(http.StatusNoContent)
		}
	}
}
