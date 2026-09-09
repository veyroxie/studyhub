package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The pricing catalogue (migrations 0051, 0054): categories own named tiers,
// and a (category, tier, sessions/week) triple carries one price.
//
// This file is the whole reason the catalogue is usable by anyone but a
// developer. Until it existed the tables held the right prices and had no API
// and no screen, so on the day billing switched over Nadine could not have
// changed a single price without a deploy -- the exact outcome the rework
// exists to end (ADR-004).
//
// Two rules are enforced here rather than left to the UI:
//
//   - A plan must be priced. The DB CHECK says monthly_fee > 0 OR hourly_rate
//     > 0; the handler refuses first so the operator gets a sentence instead
//     of a constraint violation.
//   - A category in use cannot be deleted. Soft-deleting one out from under
//     live classes would make them unpriceable in silence, which is the bug
//     the catalogue was built to close.

// isDuplicate distinguishes a UNIQUE violation from every other database
// failure. Reporting them all as "already exists" told an operator to go
// looking through their data for a row that was never the problem, and logged
// nothing, so the real cause was unrecoverable afterwards.
func isDuplicate(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

func listPricingCategories(db *store.DB, c *core.Claims) []models.PricingCategory {
	tw, twArgs := store.ScopeTenant(c, "")
	rows, err := db.Query(`SELECT id,name,COALESCE(credit_covered,FALSE),COALESCE(sort_order,0)
		FROM pricing_categories WHERE deleted_at IS NULL`+tw+` ORDER BY sort_order, name`, twArgs...)
	return store.CollectRows(rows, err, "PricingCategory", func(r *sql.Rows) (models.PricingCategory, error) {
		var p models.PricingCategory
		err := r.Scan(&p.ID, &p.Name, &p.CreditCovered, &p.SortOrder)
		return p, err
	})
}

func listPricingPlans(db *store.DB, c *core.Claims) []models.PricingPlan {
	tw, twArgs := store.ScopeTenant(c, "")
	rows, err := db.Query(`SELECT id,category_id,tier_name,COALESCE(sessions_per_week,1),
		COALESCE(monthly_fee,0),COALESCE(hourly_rate,0),COALESCE(sort_order,0)
		FROM pricing_plans WHERE deleted_at IS NULL`+tw+` ORDER BY category_id, sort_order, tier_name`, twArgs...)
	return store.CollectRows(rows, err, "PricingPlan", func(r *sql.Rows) (models.PricingPlan, error) {
		var p models.PricingPlan
		err := r.Scan(&p.ID, &p.CategoryID, &p.TierName, &p.SessionsPerWeek, &p.MonthlyFee, &p.HourlyRate, &p.SortOrder)
		return p, err
	})
}

// ── Categories ───────────────────────────────────────────────────────────────

func HandlePricingCategories(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if r.Method == http.MethodGet {
			core.Respond(w, listPricingCategories(db, c))
			return
		}
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		var body models.PricingCategory
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" {
			core.RespondError(w, "a category needs a name", http.StatusBadRequest)
			return
		}
		tid, tOK := writeTenant(w, c)
		if !tOK {
			return
		}
		id := core.GenerateID("PC")
		if _, err := db.Exec(`INSERT INTO pricing_categories(id,tenant_id,name,credit_covered,sort_order) VALUES(?,?,?,?,?)`,
			id, tid, body.Name, body.CreditCovered, body.SortOrder); err != nil {
			if isDuplicate(err) {
				core.RespondError(w, "a category called "+body.Name+" already exists", http.StatusConflict)
				return
			}
			core.LogFromReq(r).Error("catalogue: create category failed", "err", err, "name", body.Name)
			core.RespondError(w, "could not create the category", 500)
			return
		}
		core.LogAudit(db, tid, c.Email, "pricing_category_created", "pricing_category", id, body.Name)
		body.ID = id
		core.Respond(w, body)
	}
}

func HandlePricingCategoryByID(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")

		if r.Method == http.MethodDelete {
			// A category with classes pointing at it cannot go: those classes
			// would resolve to no price and be skipped in silence next run.
			var inUse int
			useArgs := append([]any{id}, twArgs...)
			if err := db.QueryRow(`SELECT COUNT(*) FROM classes WHERE pricing_category_id=? AND deleted_at IS NULL`+tw, useArgs...).Scan(&inUse); err != nil {
				core.RespondError(w, "server error", 500)
				return
			}
			if inUse > 0 {
				core.RespondError(w, "still used by "+itoa(inUse)+" class(es) -- move them to another category first", http.StatusConflict)
				return
			}
			// The category and its plans retire together or not at all. As two
			// separate statements, a failure between them left live plans under
			// a deleted category -- and the endpoint had already answered 200.
			tx, err := db.BeginTx(r.Context())
			if err != nil {
				core.RespondError(w, "server error", 500)
				return
			}
			defer tx.Rollback()

			args := append([]any{id}, twArgs...)
			res, err := tx.Exec(`UPDATE pricing_categories SET deleted_at=NOW() WHERE id=?`+tw+` AND deleted_at IS NULL`, args...)
			if err != nil {
				core.RespondError(w, "server error", 500)
				return
			}
			if n, _ := res.RowsAffected(); n == 0 {
				core.RespondError(w, "category not found", 404)
				return
			}
			// Its plans go with it, so a later category of the same name does
			// not inherit prices nobody set. Tenant-scoped like every other
			// query -- RLS is a documented passthrough, so the query layer is
			// the only thing enforcing isolation.
			planArgs := append([]any{id}, twArgs...)
			if _, perr := tx.Exec(`UPDATE pricing_plans SET deleted_at=NOW() WHERE category_id=?`+tw+` AND deleted_at IS NULL`, planArgs...); perr != nil {
				core.LogFromReq(r).Error("catalogue: retiring plans under a deleted category failed", "err", perr, "category", id)
				core.RespondError(w, "could not delete the category", 500)
				return
			}
			if err := tx.Commit(); err != nil {
				core.LogFromReq(r).Error("catalogue: category delete commit failed", "err", err, "category", id)
				core.RespondError(w, "could not delete the category", 500)
				return
			}
			core.LogAudit(db, store.TenantID(c), c.Email, "pricing_category_deleted", "pricing_category", id, "")
			core.Respond(w, map[string]string{"status": "deleted"})
			return
		}

		var body models.PricingCategory
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" {
			core.RespondError(w, "a category needs a name", http.StatusBadRequest)
			return
		}
		// Flipping credit_covered on a category in use is the same harm DELETE
		// is guarded against, reached through a checkbox. A credit-covered
		// category contributes 0 and reads as legitimate rather than flagged,
		// so turning one on under live tuition classes prices them all at zero
		// in silence -- the exact bug this catalogue exists to close.
		var wasCovered bool
		curArgs := append([]any{id}, twArgs...)
		if err := db.QueryRow(`SELECT COALESCE(credit_covered,FALSE) FROM pricing_categories WHERE id=?`+tw+` AND deleted_at IS NULL`, curArgs...).Scan(&wasCovered); err != nil {
			core.RespondError(w, "category not found", 404)
			return
		}
		if wasCovered != body.CreditCovered {
			var inUse int
			useArgs := append([]any{id}, twArgs...)
			if err := db.QueryRow(`SELECT COUNT(*) FROM classes WHERE pricing_category_id=? AND deleted_at IS NULL`+tw, useArgs...).Scan(&inUse); err != nil {
				core.LogFromReq(r).Error("catalogue: in-use check failed", "err", err, "category", id)
				core.RespondError(w, "server error", 500)
				return
			}
			if inUse > 0 {
				core.RespondError(w, "cannot change how this category bills while "+itoa(inUse)+" class(es) use it -- move them first", http.StatusConflict)
				return
			}
		}
		args := append([]any{body.Name, body.CreditCovered, body.SortOrder, id}, twArgs...)
		res, err := db.Exec(`UPDATE pricing_categories SET name=?,credit_covered=?,sort_order=? WHERE id=?`+tw+` AND deleted_at IS NULL`, args...)
		if err != nil {
			if isDuplicate(err) {
				core.RespondError(w, "a category called "+body.Name+" already exists", http.StatusConflict)
				return
			}
			core.LogFromReq(r).Error("catalogue: update category failed", "err", err, "category", id)
			core.RespondError(w, "could not update the category", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			core.RespondError(w, "category not found", 404)
			return
		}
		core.LogAudit(db, store.TenantID(c), c.Email, "pricing_category_updated", "pricing_category", id, body.Name)
		body.ID = id
		core.Respond(w, body)
	}
}

// ── Plans ────────────────────────────────────────────────────────────────────

// validPlan rejects the shapes the DB would reject anyway, with a sentence the
// operator can act on. The priced check mirrors 0054's CHECK constraint.
func validPlan(p models.PricingPlan) error {
	if strings.TrimSpace(p.TierName) == "" {
		return errors.New("a tier needs a name")
	}
	if p.CategoryID == "" {
		return errors.New("a tier must belong to a category")
	}
	if p.SessionsPerWeek < 1 || p.SessionsPerWeek > 7 {
		return errors.New("sessions per week must be between 1 and 7")
	}
	if p.MonthlyFee < 0 || p.HourlyRate < 0 {
		return errors.New("prices cannot be negative")
	}
	if p.MonthlyFee == 0 && p.HourlyRate == 0 {
		return errors.New("set a monthly fee or an hourly rate -- a tier with neither cannot price anything")
	}
	return nil
}

func HandlePricingPlans(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if r.Method == http.MethodGet {
			core.Respond(w, listPricingPlans(db, c))
			return
		}
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		var body models.PricingPlan
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		body.TierName = strings.TrimSpace(body.TierName)
		if body.SessionsPerWeek == 0 {
			body.SessionsPerWeek = 1
		}
		if err := validPlan(body); err != nil {
			core.RespondError(w, err.Error(), http.StatusBadRequest)
			return
		}
		tid, tOK := writeTenant(w, c)
		if !tOK {
			return
		}
		tw, twArgs := store.ScopeTenant(c, "")
		var catExists int
		catArgs := append([]any{body.CategoryID}, twArgs...)
		if err := db.QueryRow(`SELECT COUNT(*) FROM pricing_categories WHERE id=? AND deleted_at IS NULL`+tw, catArgs...).Scan(&catExists); err != nil || catExists == 0 {
			core.RespondError(w, "unknown category", http.StatusBadRequest)
			return
		}
		id := core.GenerateID("PP")
		if _, err := db.Exec(`INSERT INTO pricing_plans(id,tenant_id,category_id,tier_name,sessions_per_week,monthly_fee,hourly_rate,sort_order) VALUES(?,?,?,?,?,?,?,?)`,
			id, tid, body.CategoryID, body.TierName, body.SessionsPerWeek, body.MonthlyFee, body.HourlyRate, body.SortOrder); err != nil {
			if isDuplicate(err) {
				core.RespondError(w, body.TierName+" already exists in this category at "+itoa(body.SessionsPerWeek)+"x a week", http.StatusConflict)
				return
			}
			core.LogFromReq(r).Error("catalogue: write tier failed", "err", err, "tier", body.TierName)
			core.RespondError(w, "could not save the tier", 500)
			return
		}
		core.LogAudit(db, tid, c.Email, "pricing_plan_created", "pricing_plan", id, body.TierName)
		body.ID = id
		core.Respond(w, body)
	}
}

func HandlePricingPlanByID(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")

		if r.Method == http.MethodDelete {
			args := append([]any{id}, twArgs...)
			res, err := db.Exec(`UPDATE pricing_plans SET deleted_at=NOW() WHERE id=?`+tw+` AND deleted_at IS NULL`, args...)
			if err != nil {
				core.RespondError(w, "server error", 500)
				return
			}
			if n, _ := res.RowsAffected(); n == 0 {
				core.RespondError(w, "tier not found", 404)
				return
			}
			core.LogAudit(db, store.TenantID(c), c.Email, "pricing_plan_deleted", "pricing_plan", id, "")
			core.Respond(w, map[string]string{"status": "deleted"})
			return
		}

		var body models.PricingPlan
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		body.TierName = strings.TrimSpace(body.TierName)
		if body.SessionsPerWeek == 0 {
			body.SessionsPerWeek = 1
		}
		// CategoryID is not editable: moving a tier between categories would
		// silently reprice every class pointing at it. Delete and recreate.
		var currentCat string
		catArgs := append([]any{id}, twArgs...)
		if err := db.QueryRow(`SELECT category_id FROM pricing_plans WHERE id=?`+tw+` AND deleted_at IS NULL`, catArgs...).Scan(&currentCat); err != nil {
			core.RespondError(w, "tier not found", 404)
			return
		}
		body.CategoryID = currentCat
		if err := validPlan(body); err != nil {
			core.RespondError(w, err.Error(), http.StatusBadRequest)
			return
		}
		args := append([]any{body.TierName, body.SessionsPerWeek, body.MonthlyFee, body.HourlyRate, body.SortOrder, id}, twArgs...)
		res, err := db.Exec(`UPDATE pricing_plans SET tier_name=?,sessions_per_week=?,monthly_fee=?,hourly_rate=?,sort_order=? WHERE id=?`+tw+` AND deleted_at IS NULL`, args...)
		if err != nil {
			if isDuplicate(err) {
				core.RespondError(w, body.TierName+" already exists in this category at "+itoa(body.SessionsPerWeek)+"x a week", http.StatusConflict)
				return
			}
			core.LogFromReq(r).Error("catalogue: write tier failed", "err", err, "tier", body.TierName)
			core.RespondError(w, "could not save the tier", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			core.RespondError(w, "tier not found", 404)
			return
		}
		core.LogAudit(db, store.TenantID(c), c.Email, "pricing_plan_updated", "pricing_plan", id, body.TierName)
		body.ID = id
		core.Respond(w, body)
	}
}
