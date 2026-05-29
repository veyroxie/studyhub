package main

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ── Feedback CRUD ─────────────────────────────────────────────────────────────

func listFeedback(db *DB, c *Claims) []Feedback {
	tw, twArgs := scopeTenant(c, "")
	rows, err := db.Query(`SELECT id,class_id,date,teacher_id,topic,mood,notes,student_notes FROM feedback WHERE deleted_at IS NULL`+tw+` ORDER BY date DESC`, twArgs...)
	if err != nil {
		return []Feedback{}
	}
	defer rows.Close()
	out := []Feedback{}
	for rows.Next() {
		var f Feedback
		var sn string
		if err := rows.Scan(&f.ID, &f.ClassID, &f.Date, &f.TeacherID, &f.Topic, &f.Mood, &f.Notes, &sn); err != nil {
			continue
		}
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

// parentClassIDs returns the set of class IDs the parent's children are
// enrolled in — scoped to the parent's tenant.
func parentClassIDs(db *DB, c *Claims) map[string]bool {
	if c == nil {
		return map[string]bool{}
	}
	tw, twArgs := scopeTenant(c, "")
	args := append([]any{c.Email}, twArgs...)
	rows, err := db.Query(`SELECT enrolled_classes FROM students WHERE contact=? AND deleted_at IS NULL`+tw, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	ids := map[string]bool{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
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

// stripStudentNotesForParent removes per-student notes that don't belong
// to the parent's own children. Without this, curl/devtools would expose
// the entire student_notes array (other classmates' notes) for any
// feedback row a parent's child is enrolled in.
func stripStudentNotesForParent(rows []Feedback, ownIDs map[string]bool) []Feedback {
	for i := range rows {
		kept := make([]StudentNote, 0, len(rows[i].StudentNotes))
		for _, n := range rows[i].StudentNotes {
			if ownIDs[n.StudentID] {
				kept = append(kept, n)
			}
		}
		rows[i].StudentNotes = kept
	}
	return rows
}

func handleListFeedback(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		isParent := c != nil && c.Role != "admin" && c.Role != "superadmin" && c.Role != "teacher"

		date := r.URL.Query().Get("date")
		classID := r.URL.Query().Get("classId")

		// Parents use in-memory filtering — skip pagination for them
		if isParent {
			ownIDs := parentStudentIDs(db, c)
			if date == "" && classID == "" {
				all := listFeedback(db, c)
				all = filterFeedbackForParent(all, parentClassIDs(db, c))
				all = stripStudentNotesForParent(all, ownIDs)
				respond(w, all)
				return
			}
			tw, twArgs := scopeTenant(c, "")
			q := `SELECT id,class_id,date,teacher_id,topic,mood,notes,student_notes FROM feedback WHERE deleted_at IS NULL` + tw
			args := append([]any{}, twArgs...)
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
				if err := rows.Scan(&f.ID, &f.ClassID, &f.Date, &f.TeacherID, &f.Topic, &f.Mood, &f.Notes, &sn); err != nil {
					continue
				}
				if sn != "" {
					json.Unmarshal([]byte(sn), &f.StudentNotes)
				}
				if f.StudentNotes == nil {
					f.StudentNotes = []StudentNote{}
				}
				out = append(out, f)
			}
			out = filterFeedbackForParent(out, parentClassIDs(db, c))
			out = stripStudentNotesForParent(out, ownIDs)
			respond(w, out)
			return
		}

		// Admin/teacher path — supports pagination
		p := parsePagination(r)

		if date == "" && classID == "" && !p.Active {
			respond(w, listFeedback(db, c))
			return
		}

		tw, twArgs := scopeTenant(c, "")
		where := `deleted_at IS NULL` + tw
		args := append([]any{}, twArgs...)
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
				if err := rows.Scan(&f.ID, &f.ClassID, &f.Date, &f.TeacherID, &f.Topic, &f.Mood, &f.Notes, &sn); err != nil {
					continue
				}
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
			if err := rows.Scan(&f.ID, &f.ClassID, &f.Date, &f.TeacherID, &f.Topic, &f.Mood, &f.Notes, &sn); err != nil {
				continue
			}
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
		if !isStaffRole(c) {
			respondError(w, "staff only", 403)
			return
		}
		var f Feedback
		if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		if msg := validationError("classId", f.ClassID, "date", f.Date); msg != "" {
			respondError(w, msg, 400)
			return
		}
		// Authorship is derived from the authenticated session — a teacher
		// cannot post feedback under another teacher's id. Admins keep the
		// freedom to set teacher_id when filing on a teacher's behalf, but
		// only after verifying that staff row belongs to this tenant.
		if c.Role == "teacher" {
			var staffID string
			tw, twArgs := scopeTenant(c, "")
			args := append([]any{c.Email}, twArgs...)
			db.QueryRow(`SELECT id FROM staff WHERE email=? AND deleted_at IS NULL`+tw, args...).Scan(&staffID)
			if staffID == "" {
				respondError(w, "no staff record matches your account", http.StatusForbidden)
				return
			}
			f.TeacherID = staffID
		} else if f.TeacherID != "" {
			var ok int
			tw, twArgs := scopeTenant(c, "")
			args := append([]any{f.TeacherID}, twArgs...)
			db.QueryRow(`SELECT 1 FROM staff WHERE id=? AND deleted_at IS NULL`+tw, args...).Scan(&ok)
			if ok != 1 {
				respondError(w, "teacher not found in tenant", http.StatusBadRequest)
				return
			}
		}
		if f.TeacherID == "" {
			respondError(w, "teacherId is required", http.StatusBadRequest)
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
			logAudit(db, c.Email, "feedback_created", "feedback", f.ID, "class="+f.ClassID)
		}
		w.WriteHeader(http.StatusCreated)
		respond(w, f)
	}
}

func handleUpdateFeedback(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if !isStaffRole(c) {
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
		tw, twArgs := scopeTenant(c, "")
		// Ownership: a teacher may only edit feedback rows they authored.
		// Admins/superadmins keep tenant-wide edit rights. teacher_id can
		// never be reassigned via PUT — preserves audit trail.
		if c.Role == "teacher" {
			var existingTeacher string
			ownArgs := append([]any{id}, twArgs...)
			db.QueryRow(`SELECT teacher_id FROM feedback WHERE id=? AND deleted_at IS NULL`+tw, ownArgs...).Scan(&existingTeacher)
			var myStaffID string
			staffArgs := append([]any{c.Email}, twArgs...)
			db.QueryRow(`SELECT id FROM staff WHERE email=? AND deleted_at IS NULL`+tw, staffArgs...).Scan(&myStaffID)
			if existingTeacher == "" || existingTeacher != myStaffID {
				respondError(w, "you can only edit feedback you authored", http.StatusForbidden)
				return
			}
			f.TeacherID = existingTeacher
		}
		args := append([]any{f.ClassID, f.Date, f.TeacherID, f.Topic, f.Mood, f.Notes, string(snJSON), id}, twArgs...)
		res, err := db.Exec(`UPDATE feedback SET class_id=?,date=?,teacher_id=?,topic=?,mood=?,notes=?,student_notes=? WHERE id=? AND deleted_at IS NULL`+tw, args...)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			respondError(w, "feedback not found", 404)
			return
		}
		if c != nil {
			logAudit(db, c.Email, "feedback_updated", "feedback", id, "")
		}
		respond(w, f)
	}
}

func handleDeleteFeedback(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if !isStaffRole(c) {
			respondError(w, "staff only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := scopeTenant(c, "")
		// Same ownership gate as PUT — teachers delete only their own.
		if c.Role == "teacher" {
			var existingTeacher string
			ownArgs := append([]any{id}, twArgs...)
			db.QueryRow(`SELECT teacher_id FROM feedback WHERE id=? AND deleted_at IS NULL`+tw, ownArgs...).Scan(&existingTeacher)
			var myStaffID string
			staffArgs := append([]any{c.Email}, twArgs...)
			db.QueryRow(`SELECT id FROM staff WHERE email=? AND deleted_at IS NULL`+tw, staffArgs...).Scan(&myStaffID)
			if existingTeacher == "" || existingTeacher != myStaffID {
				respondError(w, "you can only delete feedback you authored", http.StatusForbidden)
				return
			}
		}
		args := append([]any{id}, twArgs...)
		if _, err := db.Exec(`UPDATE feedback SET deleted_at=NOW() WHERE id=?`+tw, args...); err != nil {
			respondError(w, "could not delete feedback", 500)
			return
		}
		if c := claimsFrom(r); c != nil {
			logAudit(db, c.Email, "feedback_deleted", "feedback", id, "soft deleted")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── Feedback Replies ─────────────────────────────────────────────────────────

func listFeedbackReplies(db *DB, c *Claims) []FeedbackReply {
	tw, twArgs := scopeTenant(c, "fr")
	// Join through feedback so we can scope visible replies by class
	// membership. Admins (and superadmins) see everything; teachers see
	// replies on classes they teach; parents see replies on classes their
	// children are enrolled in.
	q := `SELECT fr.id, fr.feedback_id, fr.author_email, fr.author_name, fr.message, fr.created_at, f.class_id
	      FROM feedback_replies fr
	      JOIN feedback f ON f.id = fr.feedback_id
	      WHERE f.deleted_at IS NULL` + tw
	rows, err := db.Query(q+` ORDER BY fr.created_at DESC`, twArgs...)
	if err != nil {
		return []FeedbackReply{}
	}
	defer rows.Close()

	var classFilter map[string]bool
	if c != nil && c.Role == "parent" {
		classFilter = parentClassIDs(db, c)
	} else if c != nil && c.Role == "teacher" {
		classFilter = teacherClassIDSet(db, c)
	}

	out := []FeedbackReply{}
	for rows.Next() {
		var r FeedbackReply
		var classID string
		if err := rows.Scan(&r.ID, &r.FeedbackID, &r.AuthorEmail, &r.AuthorName, &r.Message, &r.CreatedAt, &classID); err != nil {
			continue
		}
		if classFilter != nil && !classFilter[classID] {
			continue
		}
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
