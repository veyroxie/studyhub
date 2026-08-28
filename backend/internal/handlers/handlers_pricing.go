package handlers

import (
	"encoding/json"
	"net/http"
	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
)

// Pricing tiers are a fixed 2×2 matrix (Group/Private × Level 1-3/4-6) seeded by
// migration 0016. Admins edit the fees; the set of tiers itself isn't created or
// deleted, so only an update endpoint is exposed. listPricingTiers (in
// handlers_classes.go) feeds the snapshot.

func HandleUpdatePricingTier(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var p models.PricingTier
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		if p.MonthlyFee < 0 || p.HourlyRate < 0 {
			core.RespondError(w, "fees cannot be negative", 400)
			return
		}
		tw, twArgs := store.ScopeTenant(c, "")
		args := append([]any{p.MonthlyFee, p.HourlyRate, id}, twArgs...)
		res, err := db.Exec(`UPDATE pricing_tiers SET monthly_fee=?, hourly_rate=? WHERE id=?`+tw+` AND deleted_at IS NULL`, args...)
		if err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			core.RespondError(w, "pricing tier not found", 404)
			return
		}
		p.ID = id
		core.LogAudit(db, store.TenantID(c), c.Email, "pricing_tier_updated", "pricing_tier", id, p.ClassType+" "+p.LevelBand)
		core.Respond(w, p)
	}
}
