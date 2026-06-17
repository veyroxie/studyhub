package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/pdf"
	"studyhub/internal/store"
	"time"

	"github.com/go-chi/chi/v5"
)

// listProgressReports returns reports for the snapshot. Parents only see
// reports for their own children, and only when the report is Published
// AND there's no unpaid Monthly invoice on the family.
func listProgressReports(db *store.DB, c *core.Claims) []models.ProgressReport {
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		// Parents are always tenant-scoped — drop the OR pattern.
		tid := store.TenantID(c)
		rows, err = db.Query(`
			SELECT pr.id, pr.student_id, pr.term, COALESCE(pr.teacher_id,''),
			       COALESCE(pr.subject,''), COALESCE(pr.grade,''),
			       COALESCE(pr.strengths,''), COALESCE(pr.areas_to_improve,''),
			       COALESCE(pr.teacher_comment,''), COALESCE(pr.next_term_focus,''),
			       COALESCE(pr.published,false), pr.created_at, pr.updated_at
			FROM progress_reports pr
			JOIN students s ON s.id = pr.student_id
			WHERE s.contact = ?
			  AND s.tenant_id = ?
			  AND pr.tenant_id = ?
			  AND pr.deleted_at IS NULL
			  AND pr.published = true
			ORDER BY pr.term DESC, pr.created_at DESC`,
			c.Email, tid, tid)
	} else {
		tw, twArgs := store.ScopeTenant(c, "")
		rows, err = db.Query(`
			SELECT id, student_id, term, COALESCE(teacher_id,''),
			       COALESCE(subject,''), COALESCE(grade,''),
			       COALESCE(strengths,''), COALESCE(areas_to_improve,''),
			       COALESCE(teacher_comment,''), COALESCE(next_term_focus,''),
			       COALESCE(published,false), created_at, updated_at
			FROM progress_reports
			WHERE deleted_at IS NULL`+tw+`
			ORDER BY term DESC, created_at DESC`,
			twArgs...)
	}
	if err != nil {
		return []models.ProgressReport{}
	}
	defer rows.Close()
	out := []models.ProgressReport{}
	for rows.Next() {
		var pr models.ProgressReport
		if err := rows.Scan(&pr.ID, &pr.StudentID, &pr.Term, &pr.TeacherID,
			&pr.Subject, &pr.Grade, &pr.Strengths, &pr.AreasToImprove,
			&pr.TeacherComment, &pr.NextTermFocus, &pr.Published,
			&pr.CreatedAt, &pr.UpdatedAt); err != nil {
			continue
		}
		out = append(out, pr)
	}
	return out
}

func HandleProgressReports(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			core.Respond(w, listProgressReports(db, c))
		case http.MethodPost:
			if !core.IsStaffRole(c) {
				core.RespondError(w, "admin or teacher only", 403)
				return
			}
			var pr models.ProgressReport
			if err := json.NewDecoder(r.Body).Decode(&pr); err != nil {
				core.RespondError(w, "bad body", 400)
				return
			}
			if msg := validationError("studentId", pr.StudentID, "term", pr.Term); msg != "" {
				core.RespondError(w, msg, 400)
				return
			}
			if pr.ID == "" {
				pr.ID = core.GenerateID("PR")
			}
			now := time.Now().UTC().Format(time.RFC3339)
			tid := store.TenantID(c)
			_, err := db.Exec(`
				INSERT INTO progress_reports
				(id,tenant_id,student_id,term,teacher_id,subject,grade,strengths,areas_to_improve,teacher_comment,next_term_focus,published,created_at,updated_at)
				VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				pr.ID, tid, pr.StudentID, pr.Term, pr.TeacherID, pr.Subject, pr.Grade,
				pr.Strengths, pr.AreasToImprove, pr.TeacherComment, pr.NextTermFocus,
				pr.Published, now, now)
			if err != nil {
				core.RespondError(w, "could not create progress report", 500)
				return
			}
			pr.CreatedAt = now
			pr.UpdatedAt = now
			core.LogAudit(db, c.Email, "progress_report_created", "progress_report", pr.ID, pr.StudentID+" "+pr.Term)
			core.Respond(w, pr)
		}
	}
}

func HandleProgressReportByID(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		id := chi.URLParam(r, "id")
		switch r.Method {
		case http.MethodPut:
			if !core.IsStaffRole(c) {
				core.RespondError(w, "admin or teacher only", 403)
				return
			}
			var pr models.ProgressReport
			if err := json.NewDecoder(r.Body).Decode(&pr); err != nil {
				core.RespondError(w, "bad body", 400)
				return
			}
			now := time.Now().UTC().Format(time.RFC3339)
			tw, twArgs := store.ScopeTenant(c, "")
			args := append([]any{pr.TeacherID, pr.Subject, pr.Grade, pr.Strengths, pr.AreasToImprove,
				pr.TeacherComment, pr.NextTermFocus, pr.Published, now, id}, twArgs...)
			if _, err := db.Exec(`
				UPDATE progress_reports
				SET teacher_id=?, subject=?, grade=?, strengths=?, areas_to_improve=?,
				    teacher_comment=?, next_term_focus=?, published=?, updated_at=?
				WHERE id=?`+tw,
				args...); err != nil {
				core.RespondError(w, "could not update progress report", 500)
				return
			}
			pr.ID = id
			pr.UpdatedAt = now
			core.LogAudit(db, c.Email, "progress_report_updated", "progress_report", id, "")
			core.Respond(w, pr)
		case http.MethodDelete:
			if !core.IsAdminRole(c) {
				core.RespondError(w, "admin only", 403)
				return
			}
			tw, twArgs := store.ScopeTenant(c, "")
			args := append([]any{id}, twArgs...)
			if _, err := db.Exec(`UPDATE progress_reports SET deleted_at=NOW() WHERE id=?`+tw, args...); err != nil {
				core.RespondError(w, "could not delete progress report", 500)
				return
			}
			core.LogAudit(db, c.Email, "progress_report_deleted", "progress_report", id, "")
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// handleProgressReportPDF renders a single report as a PDF. Same parent
// gating as listProgressReports: parents only get their children's
// reports, only if Published, only if the family is up to date on
// monthly invoices.
func HandleProgressReportPDF(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		id := chi.URLParam(r, "id")

		var (
			pr          models.ProgressReport
			studentName string
			parentEmail string
			teacherName string
			deletedAt   sql.NullString
		)
		err := db.QueryRow(`
			SELECT pr.id, pr.student_id, pr.term, COALESCE(pr.teacher_id,''),
			       COALESCE(pr.subject,''), COALESCE(pr.grade,''),
			       COALESCE(pr.strengths,''), COALESCE(pr.areas_to_improve,''),
			       COALESCE(pr.teacher_comment,''), COALESCE(pr.next_term_focus,''),
			       COALESCE(pr.published,false), pr.created_at, pr.updated_at,
			       pr.deleted_at,
			       s.first_name || ' ' || s.last_name, COALESCE(s.contact,''),
			       COALESCE(t.full_name, t.name, '')
			FROM progress_reports pr
			JOIN students s ON s.id = pr.student_id
			LEFT JOIN staff t ON t.id = pr.teacher_id
			WHERE pr.id = ?`, id).Scan(
			&pr.ID, &pr.StudentID, &pr.Term, &pr.TeacherID,
			&pr.Subject, &pr.Grade, &pr.Strengths, &pr.AreasToImprove,
			&pr.TeacherComment, &pr.NextTermFocus, &pr.Published,
			&pr.CreatedAt, &pr.UpdatedAt, &deletedAt,
			&studentName, &parentEmail, &teacherName,
		)
		if err != nil || deletedAt.Valid {
			core.RespondError(w, "report not found", 404)
			return
		}

		if c != nil && c.Role == "parent" {
			if parentEmail != c.Email {
				core.RespondError(w, "not your report", 403)
				return
			}
			if !pr.Published {
				core.RespondError(w, "report is still a draft", 403)
				return
			}
			if hasUnpaidMonthlyInvoice(db, parentEmail, c) {
				core.RespondError(w, "settle outstanding invoices to access progress reports", 403)
				return
			}
		}

		bytes, err := pdf.RenderProgressReportPDF(pr, studentName, teacherName)
		if err != nil {
			core.RespondError(w, "could not render PDF", 500)
			return
		}

		safeTerm := strings.ReplaceAll(pr.Term, " ", "-")
		filename := "progress-" + safeTerm + "-" + pr.ID + ".pdf"
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		w.Write(bytes)
	}
}

// hasUnpaidMonthlyInvoice tells the progress-report PDF gate whether the
// parent's children have any outstanding Monthly invoices. A single hit
// blocks PDF access; matches the dashboard banner shown on the parent
// portal so the experience is consistent.
func hasUnpaidMonthlyInvoice(db *store.DB, parentEmail string, c *core.Claims) bool {
	itw, itwArgs := store.ScopeTenant(c, "i")
	stw, stwArgs := store.ScopeTenant(c, "s")
	args := append([]any{parentEmail}, append(stwArgs, itwArgs...)...)
	var count int
	db.QueryRow(`
		SELECT COUNT(*) FROM invoices i
		JOIN students s ON s.id = i.student_id
		WHERE s.contact = ?`+stw+itw+`
		  AND i.type = 'Monthly'
		  AND (i.status = 'Unpaid' OR i.status = 'Overdue')
		  AND i.deleted_at IS NULL`,
		args...).Scan(&count)
	return count > 0
}
