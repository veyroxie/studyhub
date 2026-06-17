package handlers

import (
	"net/http"
	"runtime"
	"studyhub/internal/core"
	"studyhub/internal/store"
	"time"
)

// buildVersion is overridden at link time via -ldflags="-X main.buildVersion=..."
// during Docker builds. Defaults to "dev" so local builds always work.
var buildVersion = "dev"

// handleHealth returns a JSON status payload usable by uptime monitors and
// container orchestrators. Probes:
//   - DB liveness (Ping) AND a real round-trip query for latency
//   - DB connection pool saturation (in-use / open / max)
//   - Email queue depth + oldest stuck row
//   - Process uptime + goroutine count
//
// The HTTP status is 503 only when something is genuinely broken (DB down
// or queue critically backed up); soft signals (latency, modest queue
// depth) come back in the JSON so dashboards can decide.
//
// Public route (no auth) — that's intentional. Monitoring agents shouldn't
// need credentials.
func HandleHealth(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbStatus := "ok"
		var dbLatencyMs float64
		t := time.Now()
		dbErr := db.Ping()
		dbLatencyMs = float64(time.Since(t).Microseconds()) / 1000.0
		if dbErr != nil {
			dbStatus = "down"
		}

		// Real round-trip query — Ping might just check the pool without
		// touching Postgres. A SELECT 1 forces an actual query.
		var one int
		queryT := time.Now()
		queryErr := db.QueryRow(`SELECT 1`).Scan(&one)
		queryLatencyMs := float64(time.Since(queryT).Microseconds()) / 1000.0
		if queryErr != nil {
			dbStatus = "query_failed"
		}

		stats := db.DB.Stats()

		emailQ := store.EmailQueueSummary(db)

		ok := dbErr == nil && queryErr == nil
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		core.Respond(w, map[string]any{
			"ok":         ok,
			"db":         dbStatus,
			"version":    buildVersion,
			"go_version": runtime.Version(),
			"env":        core.AppEnv(),
			"uptime_sec": int(time.Since(core.BootTime).Seconds()),
			"db_pool": map[string]any{
				"open":          stats.OpenConnections,
				"in_use":        stats.InUse,
				"idle":          stats.Idle,
				"wait_count":    stats.WaitCount,
				"wait_duration": stats.WaitDuration.String(),
			},
			"db_latency_ms":    dbLatencyMs,
			"query_latency_ms": queryLatencyMs,
			"goroutines":       runtime.NumGoroutine(),
			"email_queue":      emailQ,
		})

	}
}
