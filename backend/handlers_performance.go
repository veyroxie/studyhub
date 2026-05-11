package main

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ── Performance Reviews ───────────────────────────────────────────────────────

func listPerformanceReviews(db *DB, c *Claims) []PerformanceReview {
	tw, twArgs := scopeTenant(c, "")
	rows, err := db.Query(`SELECT id,staff_id,reviewer_email,date,rating,parent_rating,notes FROM performance_reviews WHERE deleted_at IS NULL`+tw+` ORDER BY date DESC`, twArgs...)
	if err != nil {
		return []PerformanceReview{}
	}
	defer rows.Close()
	out := []PerformanceReview{}
	for rows.Next() {
		var p PerformanceReview
		if err := rows.Scan(&p.ID, &p.StaffID, &p.ReviewerEmail, &p.Date, &p.Rating, &p.ParentRating, &p.Notes); err != nil {
			continue
		}
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
			tw, twArgs := scopeTenant(c, "")
			args := append([]any{staffID}, twArgs...)
			rows, err := db.Query(`SELECT id,staff_id,reviewer_email,date,rating,parent_rating,notes FROM performance_reviews WHERE staff_id=? AND deleted_at IS NULL`+tw+` ORDER BY date DESC`, args...)
			if err != nil {
				respond(w, []PerformanceReview{})
				return
			}
			defer rows.Close()
			out := []PerformanceReview{}
			for rows.Next() {
				var pr PerformanceReview
				if err := rows.Scan(&pr.ID, &pr.StaffID, &pr.ReviewerEmail, &pr.Date, &pr.Rating, &pr.ParentRating, &pr.Notes); err != nil {
					continue
				}
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

		tw, twArgs := scopeTenant(c, "")
		var total int
		db.QueryRow(`SELECT COUNT(*) FROM performance_reviews WHERE deleted_at IS NULL`+tw, twArgs...).Scan(&total)
		pageArgs := append(append([]any{}, twArgs...), pg.Limit, pg.Offset)
		rows, err := db.Query(`SELECT id,staff_id,reviewer_email,date,rating,parent_rating,notes FROM performance_reviews WHERE deleted_at IS NULL`+tw+` ORDER BY date DESC LIMIT ? OFFSET ?`, pageArgs...)
		if err != nil {
			respond(w, PaginatedResponse{Data: []PerformanceReview{}, Total: total, Limit: pg.Limit, Offset: pg.Offset})
			return
		}
		defer rows.Close()
		out := []PerformanceReview{}
		for rows.Next() {
			var pr PerformanceReview
			if err := rows.Scan(&pr.ID, &pr.StaffID, &pr.ReviewerEmail, &pr.Date, &pr.Rating, &pr.ParentRating, &pr.Notes); err != nil {
				continue
			}
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
			logAudit(db, c.Email, "performance_review_created", "performance_review", p.ID, "staff="+p.StaffID)
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
		tw, twArgs := scopeTenant(c, "")
		args := append([]any{id}, twArgs...)
		db.Exec(`UPDATE performance_reviews SET deleted_at=NOW() WHERE id=?`+tw, args...)
		if c := claimsFrom(r); c != nil {
			logAudit(db, c.Email, "performance_review_deleted", "performance_review", id, "soft deleted")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
