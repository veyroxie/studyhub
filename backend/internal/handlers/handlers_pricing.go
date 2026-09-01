package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
)

// Pricing tiers are a fixed 2×2 matrix (Group/Private × Level 1-3/4-6) seeded by
// migration 0016. Admins edit the fees; the set of tiers itself isn't created or
// deleted, so only an update endpoint is exposed. listPricingTiers (in
// handlers_classes.go) feeds the snapshot.

// pricingTierPatch takes POINTERS deliberately. A bare float64 cannot tell
// "field absent" from "field is 0", so a client sending only monthlyFee used to
// silently zero hourly_rate -- wiping migration 0045's backfill and the rate
// session billing reads. Third instance of that bug shape in this codebase.
type pricingTierPatch struct {
	MonthlyFee *float64 `json:"monthlyFee"`
	HourlyRate *float64 `json:"hourlyRate"`
}

// updates builds the SET clause from the fields actually present.
func (p pricingTierPatch) updates() ([]string, []any, error) {
	sets, vals := []string{}, []any{}
	add := func(col string, v *float64) error {
		if v == nil {
			return nil
		}
		if *v < 0 {
			return errors.New("fees cannot be negative")
		}
		sets, vals = append(sets, col+"=?"), append(vals, *v)
		return nil
	}
	if err := add("monthly_fee", p.MonthlyFee); err != nil {
		return nil, nil, err
	}
	if err := add("hourly_rate", p.HourlyRate); err != nil {
		return nil, nil, err
	}
	if len(sets) == 0 {
		return nil, nil, errors.New("nothing to update")
	}
	return sets, vals, nil
}

// storedTier re-reads the row so the response reflects what was persisted
// rather than echoing a partial request body back.
func storedTier(db *store.DB, c *core.Claims, id string) models.PricingTier {
	tw, twArgs := store.ScopeTenant(c, "")
	var t models.PricingTier
	db.QueryRow(`SELECT id,class_type,level_band,COALESCE(monthly_fee,0),COALESCE(hourly_rate,0) FROM pricing_tiers WHERE id=?`+tw,
		append([]any{id}, twArgs...)...).Scan(&t.ID, &t.ClassType, &t.LevelBand, &t.MonthlyFee, &t.HourlyRate)
	return t
}

func HandleUpdatePricingTier(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var p pricingTierPatch
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		sets, vals, err := p.updates()
		if err != nil {
			core.RespondError(w, err.Error(), http.StatusBadRequest)
			return
		}
		tw, twArgs := store.ScopeTenant(c, "")
		args := append(append(vals, id), twArgs...)
		res, err := db.Exec(`UPDATE pricing_tiers SET `+strings.Join(sets, ", ")+` WHERE id=?`+tw+` AND deleted_at IS NULL`, args...)
		if err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			core.RespondError(w, "pricing tier not found", 404)
			return
		}
		core.LogAudit(db, store.TenantID(c), c.Email, "pricing_tier_updated", "pricing_tier", id, strings.Join(sets, " "))
		core.Respond(w, storedTier(db, c, id))
	}
}
