package main

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
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

// ── Feedback Replies ─────────────────────────────────────────────────────────

func listFeedbackReplies(db *DB, c *Claims) []FeedbackReply {
	tid := tenantID(c)
	rows, err := db.Query(`SELECT id,feedback_id,author_email,author_name,message,created_at FROM feedback_replies WHERE (tenant_id=? OR ?=0) ORDER BY created_at DESC`, tid, tid)
	if err != nil {
		return []FeedbackReply{}
	}
	defer rows.Close()
	out := []FeedbackReply{}
	for rows.Next() {
		var r FeedbackReply
		rows.Scan(&r.ID, &r.FeedbackID, &r.AuthorEmail, &r.AuthorName, &r.Message, &r.CreatedAt)
		out = append(out, r)
	}
	return out
}

func handleCreateFeedbackReply(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil {
			respondError(w, "auth required", 401)
			return
		}
		var reply FeedbackReply
		if err := json.NewDecoder(r.Body).Decode(&reply); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		if reply.FeedbackID == "" || reply.Message == "" {
			respondError(w, "feedbackId and message are required", 400)
			return
		}
		reply.ID = generateID("FR")
		reply.AuthorEmail = c.Email
		reply.AuthorName = c.Name
		tid := tenantID(c)
		_, err := db.Exec(`INSERT INTO feedback_replies(id,tenant_id,feedback_id,author_email,author_name,message) VALUES(?,?,?,?,?,?)`,
			reply.ID, tid, reply.FeedbackID, reply.AuthorEmail, reply.AuthorName, reply.Message)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		w.WriteHeader(http.StatusCreated)
		respond(w, reply)
	}
}
