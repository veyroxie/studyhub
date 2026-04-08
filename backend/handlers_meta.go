package main

import (
	"net/http"
	"runtime"
)

// buildVersion is overridden at link time via -ldflags="-X main.buildVersion=..."
// during Docker builds. Defaults to "dev" so local builds always work.
var buildVersion = "dev"

// handleHealth returns a JSON status payload usable by uptime monitors and
// container orchestrators. It pings the database so a stuck or disconnected
// DB shows up as 503 instead of silently degrading the app.
//
// Public route (no auth) — that's intentional. Monitoring agents shouldn't
// need credentials.
func handleHealth(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbStatus := "ok"
		dbErr := db.Ping()
		if dbErr != nil {
			dbStatus = "down"
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		respond(w, map[string]any{
			"ok":         dbErr == nil,
			"db":         dbStatus,
			"version":    buildVersion,
			"go_version": runtime.Version(),
			"env":        appEnv(),
		})
	}
}
