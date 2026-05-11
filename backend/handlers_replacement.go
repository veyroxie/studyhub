package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ── Replacement Credits ──────────────────────────────────────────────────────

func listReplacementCredits(db *DB, c *Claims) []ReplacementCredit {
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		// Parents are always tenant-scoped — drop the OR pattern.
		tid := tenantID(c)
		rows, err = db.Query(`SELECT rc.id,rc.student_id,rc.type,rc.minutes,rc.note,rc.class_id,rc.date,rc.created_by,COALESCE(rc.category,'class') FROM replacement_credits rc JOIN students s ON s.id=rc.student_id WHERE s.contact=? AND s.tenant_id=? AND rc.tenant_id=? ORDER BY rc.created_at DESC`, c.Email, tid, tid)
	} else {
		tw, twArgs := scopeTenant(c, "")
		rows, err = db.Query(`SELECT id,student_id,type,minutes,note,class_id,date,created_by,COALESCE(category,'class') FROM replacement_credits WHERE 1=1`+tw+` ORDER BY created_at DESC`, twArgs...)
	}
	if err != nil {
		return []ReplacementCredit{}
	}
	defer rows.Close()
	out := []ReplacementCredit{}
	for rows.Next() {
		var rc ReplacementCredit
		if err := rows.Scan(&rc.ID, &rc.StudentID, &rc.Type, &rc.Minutes, &rc.Note, &rc.ClassID, &rc.Date, &rc.CreatedBy, &rc.Category); err != nil {
			continue
		}
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
		tw, twArgs := scopeTenant(c, "")
		args := append([]any{studentID}, twArgs...)
		rows, err := db.Query(`SELECT id,student_id,type,minutes,note,class_id,date,created_by,COALESCE(category,'class') FROM replacement_credits WHERE student_id=?`+tw+` ORDER BY created_at DESC`, args...)
		if err != nil {
			respond(w, []ReplacementCredit{})
			return
		}
		defer rows.Close()
		out := []ReplacementCredit{}
		for rows.Next() {
			var rc ReplacementCredit
			if err := rows.Scan(&rc.ID, &rc.StudentID, &rc.Type, &rc.Minutes, &rc.Note, &rc.ClassID, &rc.Date, &rc.CreatedBy, &rc.Category); err != nil {
				continue
			}
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
		if rc.Category == "" {
			rc.Category = "class"
		}
		if rc.Category != "class" && rc.Category != "self-study" {
			respondError(w, "category must be 'class' or 'self-study'", 400)
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
			tw, twArgs := scopeTenant(c, "")
			balArgs := append([]any{rc.StudentID, rc.Category}, twArgs...)
			if err := tx.QueryRow(`SELECT COALESCE(SUM(CASE WHEN type='earned' THEN minutes ELSE 0 END),0), COALESCE(SUM(CASE WHEN type='used' THEN minutes ELSE 0 END),0) FROM replacement_credits WHERE student_id=? AND category=?`+tw, balArgs...).Scan(&earned, &used); err != nil {
				respondError(w, "server error", 500)
				return
			}
			balance := earned - used
			if rc.Minutes > balance {
				catLabel := "class"
				if rc.Category == "self-study" {
					catLabel = "self-study"
				}
				respondError(w, fmt.Sprintf("insufficient %s credits: %d available, trying to use %d", catLabel, balance, rc.Minutes), 400)
				return
			}

			_, err = tx.Exec(`INSERT INTO replacement_credits(id,tenant_id,student_id,type,minutes,note,class_id,date,created_by,category) VALUES(?,?,?,?,?,?,?,?,?,?)`,
				rc.ID, tid, rc.StudentID, rc.Type, rc.Minutes, rc.Note, rc.ClassID, rc.Date, rc.CreatedBy, rc.Category)
			if err != nil {
				respondError(w, "server error", 500)
				return
			}
			if err := tx.Commit(); err != nil {
				respondError(w, "server error", 500)
				return
			}
		} else {
			_, err := db.Exec(`INSERT INTO replacement_credits(id,tenant_id,student_id,type,minutes,note,class_id,date,created_by,category) VALUES(?,?,?,?,?,?,?,?,?,?)`,
				rc.ID, tid, rc.StudentID, rc.Type, rc.Minutes, rc.Note, rc.ClassID, rc.Date, rc.CreatedBy, rc.Category)
			if err != nil {
				respondError(w, "server error", 500)
				return
			}
		}
		logAudit(db, c.Email, "replacement_credit_"+rc.Type, "replacement_credit", rc.ID, fmt.Sprintf("student=%s credits=%d category=%s", rc.StudentID, rc.Minutes, rc.Category))
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
		tw, twArgs := scopeTenant(c, "")
		args := append([]any{id}, twArgs...)
		if _, err := db.Exec(`DELETE FROM replacement_credits WHERE id=?`+tw, args...); err != nil {
			respondError(w, "could not delete replacement credit", 500)
			return
		}
		logAudit(db, c.Email, "replacement_credit_deleted", "replacement_credit", id, "deleted")
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
		tw, twArgs := scopeTenant(c, "")
		var classEarned, classUsed, ssEarned, ssUsed int
		argsClass := append([]any{studentID}, twArgs...)
		if err := db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN type='earned' THEN minutes ELSE 0 END),0), COALESCE(SUM(CASE WHEN type='used' THEN minutes ELSE 0 END),0) FROM replacement_credits WHERE student_id=? AND COALESCE(category,'class')='class'`+tw, argsClass...).Scan(&classEarned, &classUsed); err != nil {
			respondError(w, "server error", 500)
			return
		}
		argsSS := append([]any{studentID}, twArgs...)
		if err := db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN type='earned' THEN minutes ELSE 0 END),0), COALESCE(SUM(CASE WHEN type='used' THEN minutes ELSE 0 END),0) FROM replacement_credits WHERE student_id=? AND category='self-study'`+tw, argsSS...).Scan(&ssEarned, &ssUsed); err != nil {
			respondError(w, "server error", 500)
			return
		}
		respond(w, map[string]any{
			"class":     map[string]int{"earned": classEarned, "used": classUsed, "balance": classEarned - classUsed},
			"selfStudy": map[string]int{"earned": ssEarned, "used": ssUsed, "balance": ssEarned - ssUsed},
		})
	}
}
