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
	tid := tenantID(c)
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		rows, err = db.Query(`SELECT rc.id,rc.student_id,rc.type,rc.minutes,rc.note,rc.class_id,rc.date,rc.created_by,COALESCE(rc.category,'class') FROM replacement_credits rc JOIN students s ON s.id=rc.student_id WHERE s.contact=? AND (rc.tenant_id=? OR ?=0) ORDER BY rc.created_at DESC`, c.Email, tid, tid)
	} else {
		rows, err = db.Query(`SELECT id,student_id,type,minutes,note,class_id,date,created_by,COALESCE(category,'class') FROM replacement_credits WHERE (tenant_id=? OR ?=0) ORDER BY created_at DESC`, tid, tid)
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
		tid := tenantID(c)
		rows, err := db.Query(`SELECT id,student_id,type,minutes,note,class_id,date,created_by,COALESCE(category,'class') FROM replacement_credits WHERE student_id=? AND (tenant_id=? OR ?=0) ORDER BY created_at DESC`, studentID, tid, tid)
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
			tx.QueryRow(`SELECT COALESCE(SUM(CASE WHEN type='earned' THEN minutes ELSE 0 END),0), COALESCE(SUM(CASE WHEN type='used' THEN minutes ELSE 0 END),0) FROM replacement_credits WHERE student_id=? AND category=? AND (tenant_id=? OR ?=0)`, rc.StudentID, rc.Category, tid, tid).Scan(&earned, &used)
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
		db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
			c.Email, "replacement_credit_"+rc.Type, "replacement_credit", rc.ID, fmt.Sprintf("student=%s credits=%d category=%s", rc.StudentID, rc.Minutes, rc.Category))
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
		var classEarned, classUsed, ssEarned, ssUsed int
		db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN type='earned' THEN minutes ELSE 0 END),0), COALESCE(SUM(CASE WHEN type='used' THEN minutes ELSE 0 END),0) FROM replacement_credits WHERE student_id=? AND COALESCE(category,'class')='class' AND (tenant_id=? OR ?=0)`, studentID, tid, tid).Scan(&classEarned, &classUsed)
		db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN type='earned' THEN minutes ELSE 0 END),0), COALESCE(SUM(CASE WHEN type='used' THEN minutes ELSE 0 END),0) FROM replacement_credits WHERE student_id=? AND category='self-study' AND (tenant_id=? OR ?=0)`, studentID, tid, tid).Scan(&ssEarned, &ssUsed)
		respond(w, map[string]any{
			"class":     map[string]int{"earned": classEarned, "used": classUsed, "balance": classEarned - classUsed},
			"selfStudy": map[string]int{"earned": ssEarned, "used": ssUsed, "balance": ssEarned - ssUsed},
		})
	}
}
