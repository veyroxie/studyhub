package main

import (
	"encoding/json"
	"net/http"
)

// handleVapidKey returns the public VAPID key the browser needs before it can
// create a push subscription. The public key is not a secret, so this route is
// unauthenticated. Empty string means push isn't configured — the frontend
// then skips the subscribe flow and falls back to email + in-app.
func handleVapidKey() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]string{"publicKey": pushCfg.publicKey})
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
func handlePushSubscribe(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil {
			respondError(w, "not authenticated", http.StatusUnauthorized)
			return
		}
		var body pushSubscribeBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			respondError(w, "bad body", http.StatusBadRequest)
			return
		}
		if body.Endpoint == "" || body.P256dh == "" || body.Auth == "" {
			respondError(w, "endpoint, p256dh and auth are required", http.StatusBadRequest)
			return
		}
		_, err := db.Exec(`INSERT INTO push_subscriptions(tenant_id,parent_email,endpoint,p256dh,auth)
			VALUES(?,?,?,?,?)
			ON CONFLICT(endpoint) DO UPDATE SET parent_email=EXCLUDED.parent_email, p256dh=EXCLUDED.p256dh, auth=EXCLUDED.auth`,
			tenantID(c), c.Email, body.Endpoint, body.P256dh, body.Auth)
		if err != nil {
			respondError(w, "could not save subscription", http.StatusInternalServerError)
			return
		}
		respond(w, map[string]string{"message": "subscribed"})
	}
}
