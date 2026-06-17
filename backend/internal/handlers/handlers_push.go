package handlers

import (
	"encoding/json"
	"net/http"
	"studyhub/internal/core"
	"studyhub/internal/notify"
	"studyhub/internal/store"
)

// handleVapidKey returns the public VAPID key the browser needs before it can
// create a push subscription. The public key is not a secret, so this route is
// unauthenticated. Empty string means push isn't configured — the frontend
// then skips the subscribe flow and falls back to email + in-app.
func HandleVapidKey() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		core.Respond(w, map[string]string{"publicKey": notify.VapidPublicKey()})
	}
}

type pushSubscribeBody struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

// handlePushSubscribe stores (or refreshes) a browser push subscription for the
// authenticated parent. Upsert on the unique endpoint so re-subscribing the
// same browser updates its keys instead of failing the UNIQUE constraint.
func HandlePushSubscribe(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if c == nil {
			core.RespondError(w, "not authenticated", http.StatusUnauthorized)
			return
		}
		var body pushSubscribeBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			core.RespondError(w, "bad body", http.StatusBadRequest)
			return
		}
		if body.Endpoint == "" || body.P256dh == "" || body.Auth == "" {
			core.RespondError(w, "endpoint, p256dh and auth are required", http.StatusBadRequest)
			return
		}
		_, err := db.Exec(`INSERT INTO push_subscriptions(tenant_id,parent_email,endpoint,p256dh,auth)
			VALUES(?,?,?,?,?)
			ON CONFLICT(endpoint) DO UPDATE SET tenant_id=EXCLUDED.tenant_id, parent_email=EXCLUDED.parent_email, p256dh=EXCLUDED.p256dh, auth=EXCLUDED.auth`,
			store.TenantID(c), c.Email, body.Endpoint, body.P256dh, body.Auth)
		if err != nil {
			core.RespondError(w, "could not save subscription", http.StatusInternalServerError)
			return
		}
		core.Respond(w, map[string]string{"message": "subscribed"})
	}
}
