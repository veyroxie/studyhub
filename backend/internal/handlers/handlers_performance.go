package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
)

// ── Performance Reviews ───────────────────────────────────────────────────────

func listPerformanceReviews(db *store.DB, c *core.Claims) []models.PerformanceReview {
	tw, twArgs := store.ScopeTenant(c, "")
	// Admins see every review; a teacher sees only their own; everyone else
	// (parents) gets nothing. This feeds the snapshot, which previously handed
	// teachers every colleague's rating and comments.
	if c == nil || (!core.IsAdminRole(c) && c.Role != "teacher") {
		return []models.PerformanceReview{}
	}
	ownFilter := ""
	args := twArgs
	if c.Role == "teacher" {
		var myStaffID string
		staffArgs := append([]any{c.Email}, twArgs...)
		db.QueryRow(`SELECT id FROM staff WHERE email=? AND deleted_at IS NULL`+tw, staffArgs...).Scan(&myStaffID)
		if myStaffID == "" {
			return []models.PerformanceReview{}
		}
		ownFilter = ` AND staff_id=?`
		args = append(append([]any{}, twArgs...), myStaffID)
	}
	rows, err := db.Query(`SELECT id,staff_id,reviewer_email,date,rating,parent_rating,notes FROM performance_reviews WHERE deleted_at IS NULL`+tw+ownFilter+` ORDER BY date DESC LIMIT 5000`, args...)
	return store.CollectRows(rows, err, "PerformanceReview", func(r *sql.Rows) (models.PerformanceReview, error) {
		var p models.PerformanceReview
		err := r.Scan(&p.ID, &p.StaffID, &p.ReviewerEmail, &p.Date, &p.Rating, &p.ParentRating, &p.Notes)
		return p, err
	})
}

func HandleListPerformanceReviews(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		// Parents cannot view staff performance reviews
		if c == nil || (!core.IsAdminRole(c) && c.Role != "teacher") {
			core.Respond(w, []models.PerformanceReview{})
			return
		}
		staffID := r.URL.Query().Get("staffId")
		// A teacher may read only their OWN reviews. Pin the filter to their staff
		// row and ignore any requested staffId, otherwise they can read every
		// rating and comment written about a colleague.
		if c.Role == "teacher" {
			tw, twArgs := store.ScopeTenant(c, "")
			var myStaffID string
			staffArgs := append([]any{c.Email}, twArgs...)
			db.QueryRow(`SELECT id FROM staff WHERE email=? AND deleted_at IS NULL`+tw, staffArgs...).Scan(&myStaffID)
			if myStaffID == "" {
				core.Respond(w, []models.PerformanceReview{})
				return
			}
			staffID = myStaffID
		}
		if staffID != "" {
			// Filtered by staffId — small dataset, no pagination
			tw, twArgs := store.ScopeTenant(c, "")
			args := append([]any{staffID}, twArgs...)
			rows, err := db.Query(`SELECT id,staff_id,reviewer_email,date,rating,parent_rating,notes FROM performance_reviews WHERE staff_id=? AND deleted_at IS NULL`+tw+` ORDER BY date DESC LIMIT 5000`, args...)
			if err != nil {
				core.Respond(w, []models.PerformanceReview{})
				return
			}
			defer rows.Close()
			out := []models.PerformanceReview{}
			for rows.Next() {
				var pr models.PerformanceReview
				if err := rows.Scan(&pr.ID, &pr.StaffID, &pr.ReviewerEmail, &pr.Date, &pr.Rating, &pr.ParentRating, &pr.Notes); err != nil {
					continue
				}
				out = append(out, pr)
			}
			core.Respond(w, out)
			return
		}

		pg := core.ParsePagination(r)
		if !pg.Active {
			core.Respond(w, listPerformanceReviews(db, c))
			return
		}

		tw, twArgs := store.ScopeTenant(c, "")
		var total int
		db.QueryRow(`SELECT COUNT(*) FROM performance_reviews WHERE deleted_at IS NULL`+tw, twArgs...).Scan(&total)
		pageArgs := append(append([]any{}, twArgs...), pg.Limit, pg.Offset)
		rows, err := db.Query(`SELECT id,staff_id,reviewer_email,date,rating,parent_rating,notes FROM performance_reviews WHERE deleted_at IS NULL`+tw+` ORDER BY date DESC LIMIT ? OFFSET ?`, pageArgs...)
		if err != nil {
			core.Respond(w, core.PaginatedResponse{Data: []models.PerformanceReview{}, Total: total, Limit: pg.Limit, Offset: pg.Offset})
			return
		}
		defer rows.Close()
		out := []models.PerformanceReview{}
		for rows.Next() {
			var pr models.PerformanceReview
			if err := rows.Scan(&pr.ID, &pr.StaffID, &pr.ReviewerEmail, &pr.Date, &pr.Rating, &pr.ParentRating, &pr.Notes); err != nil {
				continue
			}
			out = append(out, pr)
		}
		core.Respond(w, core.PaginatedResponse{Data: out, Total: total, Limit: pg.Limit, Offset: pg.Offset})
	}
}

func HandleCreatePerformanceReview(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		// Admin-only: a teacher must never be able to author an evaluation about
		// a colleague (staff_id is client-supplied, so IsStaffRole let any
		// teacher fabricate a review for anyone).
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		var p models.PerformanceReview
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		if msg := validationError("staffId", p.StaffID); msg != "" {
			core.RespondError(w, msg, 400)
			return
		}
		if p.ID == "" {
			p.ID = core.GenerateID("PR")
		}
		if p.Date == "" {
			p.Date = core.Today()
		}
		if p.ReviewerEmail == "" && c != nil {
			p.ReviewerEmail = c.Email
		}
		tid := store.TenantID(c)
		_, err := db.Exec(`INSERT INTO performance_reviews(id,tenant_id,staff_id,reviewer_email,date,rating,parent_rating,notes) VALUES(?,?,?,?,?,?,?,?)`,
			p.ID, tid, p.StaffID, p.ReviewerEmail, p.Date, p.Rating, p.ParentRating, p.Notes)
		if err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		if c != nil {
			core.LogAudit(db, store.TenantID(c), c.Email, "performance_review_created", "performance_review", p.ID, "staff="+p.StaffID)
		}
		w.WriteHeader(http.StatusCreated)
		core.Respond(w, p)
	}
}

func HandleDeletePerformanceReview(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")
		args := append([]any{id}, twArgs...)
		db.Exec(`UPDATE performance_reviews SET deleted_at=NOW() WHERE id=?`+tw, args...)
		if c := core.ClaimsFrom(r); c != nil {
			core.LogAudit(db, store.TenantID(c), c.Email, "performance_review_deleted", "performance_review", id, "soft deleted")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
