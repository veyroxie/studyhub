package main

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Pricing tiers are a fixed 2×2 matrix (Group/Private × Level 1-3/4-6) seeded by
// migration 0016. Admins edit the fees; the set of tiers itself isn't created or
// deleted, so only an update endpoint is exposed. listPricingTiers (in
// handlers_classes.go) feeds the snapshot.

func handleUpdatePricingTier(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if !isAdminRole(c) {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var p PricingTier
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		if p.MonthlyFee < 0 {
			respondError(w, "monthly fee cannot be negative", 400)
			return
		}
		tw, twArgs := scopeTenant(c, "")
		args := append([]any{p.MonthlyFee, id}, twArgs...)
		res, err := db.Exec(`UPDATE pricing_tiers SET monthly_fee=? WHERE id=?`+tw+` AND deleted_at IS NULL`, args...)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			respondError(w, "pricing tier not found", 404)
			return
		}
		p.ID = id
		logAudit(db, c.Email, "pricing_tier_updated", "pricing_tier", id, p.ClassType+" "+p.LevelBand)
		respond(w, p)
	}
}
