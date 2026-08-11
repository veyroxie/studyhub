package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
)

// ── Replacement Credits ──────────────────────────────────────────────────────

func listReplacementCredits(db *store.DB, c *core.Claims) []models.ReplacementCredit {
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		// Parents are always tenant-scoped — drop the OR pattern.
		tid := store.TenantID(c)
		rows, err = db.Query(`SELECT rc.id,rc.student_id,rc.type,rc.minutes,rc.note,rc.class_id,rc.date,rc.created_by,COALESCE(rc.category,'class') FROM replacement_credits rc JOIN students s ON s.id=rc.student_id WHERE s.contact=? AND s.tenant_id=? AND rc.tenant_id=? ORDER BY rc.created_at DESC LIMIT 5000`, c.Email, tid, tid)
	} else {
		tw, twArgs := store.ScopeTenant(c, "")
		rows, err = db.Query(`SELECT id,student_id,type,minutes,note,class_id,date,created_by,COALESCE(category,'class') FROM replacement_credits WHERE 1=1`+tw+` ORDER BY created_at DESC LIMIT 5000`, twArgs...)
	}
	out := store.CollectRows(rows, err, "ReplacementCredit", func(r *sql.Rows) (models.ReplacementCredit, error) {
		var rc models.ReplacementCredit
		err := r.Scan(&rc.ID, &rc.StudentID, &rc.Type, &rc.Minutes, &rc.Note, &rc.ClassID, &rc.Date, &rc.CreatedBy, &rc.Category)
		return rc, err
	})
	// Teachers see credits only for students in their own classes; the query
	// above is tenant-wide for every non-parent role.
	if c != nil && c.Role == "teacher" {
		stuIDs := teacherStudentIDSet(db, c)
		scoped := []models.ReplacementCredit{}
		for _, rc := range out {
			if stuIDs[rc.StudentID] {
				scoped = append(scoped, rc)
			}
		}
		return scoped
	}
	return out
}

func HandleListReplacementCredits(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		studentID := r.URL.Query().Get("studentId")
		if studentID == "" {
			core.Respond(w, listReplacementCredits(db, c))
			return
		}
		if c != nil && c.Role == "parent" {
			stuIDs := parentStudentIDs(db, c)
			if !stuIDs[studentID] {
				core.Respond(w, []models.ReplacementCredit{})
				return
			}
		}
		// Same owns-check for teachers — previously only parents were verified,
		// so a teacher could read credits for any student in the tenant.
		if c != nil && c.Role == "teacher" {
			stuIDs := teacherStudentIDSet(db, c)
			if !stuIDs[studentID] {
				core.Respond(w, []models.ReplacementCredit{})
				return
			}
		}
		tw, twArgs := store.ScopeTenant(c, "")
		args := append([]any{studentID}, twArgs...)
		rows, err := db.Query(`SELECT id,student_id,type,minutes,note,class_id,date,created_by,COALESCE(category,'class') FROM replacement_credits WHERE student_id=?`+tw+` ORDER BY created_at DESC LIMIT 5000`, args...)
		if err != nil {
			core.Respond(w, []models.ReplacementCredit{})
			return
		}
		defer rows.Close()
		out := []models.ReplacementCredit{}
		for rows.Next() {
			var rc models.ReplacementCredit
			if err := rows.Scan(&rc.ID, &rc.StudentID, &rc.Type, &rc.Minutes, &rc.Note, &rc.ClassID, &rc.Date, &rc.CreatedBy, &rc.Category); err != nil {
				continue
			}
			out = append(out, rc)
		}
		core.Respond(w, out)
	}
}

func HandleCreateReplacementCredit(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsStaffRole(c) {
			core.RespondError(w, "staff only", 403)
			return
		}
		var rc models.ReplacementCredit
		if err := json.NewDecoder(r.Body).Decode(&rc); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		if !teacherMayActOnStudent(db, c, rc.StudentID) {
			core.RespondError(w, "you can only manage credits for students in your own classes", http.StatusForbidden)
			return
		}
		if msg := validationError("studentId", rc.StudentID, "type", rc.Type, "date", rc.Date); msg != "" {
			core.RespondError(w, msg, 400)
			return
		}
		if rc.Type != "earned" && rc.Type != "used" {
			core.RespondError(w, "type must be 'earned' or 'used'", 400)
			return
		}
		if rc.Minutes <= 0 {
			core.RespondError(w, "minutes must be greater than 0", 400)
			return
		}
		if rc.Category == "" {
			rc.Category = "class"
		}
		if rc.Category != "class" && rc.Category != "self-study" {
			core.RespondError(w, "category must be 'class' or 'self-study'", 400)
			return
		}
		if rc.ID == "" {
			rc.ID = core.GenerateID("RC")
		}
		if rc.CreatedBy == "" && c != nil {
			rc.CreatedBy = c.Email
		}
		tid := store.TenantID(c)
		if rc.Type == "used" {
			tx, err := db.BeginTx(r.Context())
			if err != nil {
				core.RespondError(w, "server error", 500)
				return
			}
			defer tx.Rollback()

			// Serialise concurrent redemptions for the same (student, category)
			// via a transaction-scoped advisory lock so two parallel POSTs can't
			// both read the same balance and both pass the threshold check.
			// The lock is released automatically when the tx commits or rolls
			// back.
			lockKey := core.AdvisoryLockKey(rc.StudentID + "|" + rc.Category)
			if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(?)`, lockKey); err != nil {
				core.RespondError(w, "server error", 500)
				return
			}

			var earned, used int
			tw, twArgs := store.ScopeTenant(c, "")
			balArgs := append([]any{rc.StudentID, rc.Category}, twArgs...)
			if err := tx.QueryRow(`SELECT COALESCE(SUM(CASE WHEN type='earned' THEN minutes ELSE 0 END),0), COALESCE(SUM(CASE WHEN type='used' THEN minutes ELSE 0 END),0) FROM replacement_credits WHERE student_id=? AND category=?`+tw, balArgs...).Scan(&earned, &used); err != nil {
				core.RespondError(w, "server error", 500)
				return
			}
			balance := earned - used
			if rc.Minutes > balance {
				catLabel := "class"
				if rc.Category == "self-study" {
					catLabel = "self-study"
				}
				core.RespondError(w, fmt.Sprintf("insufficient %s credits: %d available, trying to use %d", catLabel, balance, rc.Minutes), 400)
				return
			}

			_, err = tx.Exec(`INSERT INTO replacement_credits(id,tenant_id,student_id,type,minutes,note,class_id,date,created_by,category) VALUES(?,?,?,?,?,?,?,?,?,?)`,
				rc.ID, tid, rc.StudentID, rc.Type, rc.Minutes, rc.Note, rc.ClassID, rc.Date, rc.CreatedBy, rc.Category)
			if err != nil {
				core.RespondError(w, "server error", 500)
				return
			}
			if err := tx.Commit(); err != nil {
				core.RespondError(w, "server error", 500)
				return
			}
		} else {
			_, err := db.Exec(`INSERT INTO replacement_credits(id,tenant_id,student_id,type,minutes,note,class_id,date,created_by,category) VALUES(?,?,?,?,?,?,?,?,?,?)`,
				rc.ID, tid, rc.StudentID, rc.Type, rc.Minutes, rc.Note, rc.ClassID, rc.Date, rc.CreatedBy, rc.Category)
			if err != nil {
				core.RespondError(w, "server error", 500)
				return
			}
		}
		core.LogAudit(db, store.TenantID(c), c.Email, "replacement_credit_"+rc.Type, "replacement_credit", rc.ID, fmt.Sprintf("student=%s credits=%d category=%s", rc.StudentID, rc.Minutes, rc.Category))
		w.WriteHeader(http.StatusCreated)
		core.Respond(w, rc)
	}
}

func HandleDeleteReplacementCredit(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")
		args := append([]any{id}, twArgs...)
		if _, err := db.Exec(`DELETE FROM replacement_credits WHERE id=?`+tw, args...); err != nil {
			core.RespondError(w, "could not delete replacement credit", 500)
			return
		}
		core.LogAudit(db, store.TenantID(c), c.Email, "replacement_credit_deleted", "replacement_credit", id, "deleted")
		w.WriteHeader(http.StatusNoContent)
	}
}

func HandleReplacementBalance(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		studentID := r.URL.Query().Get("studentId")
		if studentID == "" {
			core.RespondError(w, "studentId is required", 400)
			return
		}
		if c != nil && c.Role == "parent" {
			stuIDs := parentStudentIDs(db, c)
			if !stuIDs[studentID] {
				core.RespondError(w, "not your student", 403)
				return
			}
		}
		// Same owns-check for teachers — this by-id sibling of the list endpoint
		// was left open when the list was scoped.
		if c != nil && c.Role == "teacher" && !teacherMayActOnStudent(db, c, studentID) {
			core.RespondError(w, "not your student", http.StatusForbidden)
			return
		}
		tw, twArgs := store.ScopeTenant(c, "")
		var classEarned, classUsed, ssEarned, ssUsed int
		argsClass := append([]any{studentID}, twArgs...)
		if err := db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN type='earned' THEN minutes ELSE 0 END),0), COALESCE(SUM(CASE WHEN type='used' THEN minutes ELSE 0 END),0) FROM replacement_credits WHERE student_id=? AND COALESCE(category,'class')='class'`+tw, argsClass...).Scan(&classEarned, &classUsed); err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		argsSS := append([]any{studentID}, twArgs...)
		if err := db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN type='earned' THEN minutes ELSE 0 END),0), COALESCE(SUM(CASE WHEN type='used' THEN minutes ELSE 0 END),0) FROM replacement_credits WHERE student_id=? AND category='self-study'`+tw, argsSS...).Scan(&ssEarned, &ssUsed); err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		core.Respond(w, map[string]any{
			"class":     map[string]int{"earned": classEarned, "used": classUsed, "balance": classEarned - classUsed},
			"selfStudy": map[string]int{"earned": ssEarned, "used": ssUsed, "balance": ssEarned - ssUsed},
		})

	}
}
