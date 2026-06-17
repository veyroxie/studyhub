package main

import (
	"encoding/json"
	"net/http"
	"os"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Web push delivery (VAPID). Parents subscribe a browser via the frontend;
// rows live in push_subscriptions. We push on student check-in/out so a parent
// is notified even when the app is closed — unlike the in-app toast, which only
// fires while StudyHub is open in a tab.

const (
	// pushTTLSeconds: how long the push service holds an undelivered message.
	// 10 min — a check-in alert older than that is no longer "immediate" and
	// not worth waking the device for.
	pushTTLSeconds = 600
	// pushSubscriber identifies us to the push service per the VAPID spec.
	pushSubscriber = "mailto:hello@studyhub.fit"
)

// pushConfig is the VAPID key pair, loaded once at startup. When unset the
// whole web-push path is a silent no-op so the feature degrades to email +
// in-app rather than erroring on every check-in.
type pushConfig struct {
	publicKey  string
	privateKey string
}

var pushCfg pushConfig

// initPush loads the VAPID keys. Called from main() alongside initMailer.
func initPush() {
	pushCfg = pushConfig{
		publicKey:  os.Getenv("VAPID_PUBLIC_KEY"),
		privateKey: os.Getenv("VAPID_PRIVATE_KEY"),
	}
	if !isPushConfigured() {
		logger.Warn("web push disabled — VAPID_PUBLIC_KEY / VAPID_PRIVATE_KEY not set; parents get email + in-app only")
		return
	}
	logger.Info("web push initialised")
}

func isPushConfigured() bool {
	return pushCfg.publicKey != "" && pushCfg.privateKey != ""
}

// pushPayload is the JSON the service worker receives in its `push` event.
type pushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag"`
}

// sendPushToParent delivers one notification to every browser the parent has
// subscribed (phone + laptop, say). Best-effort: it logs and continues, never
// blocking the attendance write that triggered it. Dead endpoints are pruned.
func sendPushToParent(db *DB, tenantID int, parentEmail string, payload pushPayload) {
	if !isPushConfigured() {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		logger.Error("push payload marshal failed", "err", err)
		return
	}
	subs := listParentSubs(db, tenantID, parentEmail)
	for i := range subs {
		deliverPush(db, tenantID, &subs[i], body)
	}
}

// listParentSubs returns every stored push subscription for one parent.
func listParentSubs(db *DB, tenantID int, parentEmail string) []webpush.Subscription {
	rows, err := db.Query(`SELECT endpoint,p256dh,auth FROM push_subscriptions WHERE tenant_id=? AND parent_email=?`, tenantID, parentEmail)
	if err != nil {
		logger.Error("push subscription lookup failed", "err", err, "email", parentEmail)
		return nil
	}
	defer rows.Close()
	subs := []webpush.Subscription{}
	for rows.Next() {
		var s webpush.Subscription
		if err := rows.Scan(&s.Endpoint, &s.Keys.P256dh, &s.Keys.Auth); err != nil {
			continue
		}
		subs = append(subs, s)
	}
	return subs
}

// deliverPush sends to a single subscription and prunes it if the push service
// reports it gone (404/410) — browsers rotate endpoints on reinstall.
func deliverPush(db *DB, tenantID int, sub *webpush.Subscription, body []byte) {
	resp, err := webpush.SendNotification(body, sub, &webpush.Options{
		Subscriber:      pushSubscriber,
		VAPIDPublicKey:  pushCfg.publicKey,
		VAPIDPrivateKey: pushCfg.privateKey,
		TTL:             pushTTLSeconds,
	})
	if err != nil {
		logger.Error("push send failed", "err", err, "endpoint", sub.Endpoint)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		if _, derr := db.Exec(`DELETE FROM push_subscriptions WHERE tenant_id=? AND endpoint=?`, tenantID, sub.Endpoint); derr != nil {
			logger.Error("push subscription prune failed", "err", derr, "endpoint", sub.Endpoint)
		}
		return
	}
	if resp.StatusCode >= 300 {
		logger.Warn("push service rejected", "status", resp.StatusCode, "endpoint", sub.Endpoint)
	}
}
