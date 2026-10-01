package handlers

import (
	"net/http"
	"studyhub/internal/core"
	"studyhub/internal/store"
	"time"
)

// The build stamp lives in core.BuildVersion, which is what the Dockerfile's
// -ldflags actually sets. This package used to declare its own buildVersion
// that nothing ever wrote, so /api/health reported "dev" on every production
// build and could not answer "is my fix live?".

// HandleHealth is the public liveness answer for the Docker healthcheck, make verify and
// the deploy workflow: ok, db and uptime only. 503 when the database is unreachable.
// Pool, queue and runtime details are on the developer page (/api/dev/health); they
// told anyone on the internet how busy and how configured the server was.
func HandleHealth(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbStatus := "ok"
		var one int
		if err := db.QueryRow(`SELECT 1`).Scan(&one); err != nil {
			dbStatus = "down"
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		core.Respond(w, map[string]any{
			"ok":         dbStatus == "ok",
			"db":         dbStatus,
			"uptime_sec": int(time.Since(core.BootTime).Seconds()),
		})
	}
}
