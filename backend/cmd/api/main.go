package main

import (
	"context"
	"crypto/rand"
	"flag"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/handlers"
	"studyhub/internal/jobs"
	"studyhub/internal/mailer"
	"studyhub/internal/notify"
	"studyhub/internal/server"
	"studyhub/internal/store"
)

func main() {
	// Load .env files in order: .env first (legacy / shared defaults), then
	// .env.${APP_ENV} on top so per-environment overrides win. Both are
	// silently optional — production typically gets env vars from the
	// orchestrator (Docker Compose, systemd) instead of files.
	env := core.AppEnv()
	_ = godotenv.Load(".env")
	_ = godotenv.Load(".env." + env)
	core.InitLogger()
	core.Logger.Info("starting", "env", env, "version", core.BuildVersion)

	dbDSN := flag.String("db", "postgres://studyhub:studyhub@localhost:5432/studyhub?sslmode=disable", "PostgreSQL connection string")
	port := flag.String("port", "8080", "HTTP port")
	flag.Parse()

	// Override with environment variables if set (useful for deployment)
	if v := os.Getenv("DATABASE_URL"); v != "" {
		*dbDSN = v
	}
	if v := os.Getenv("PORT"); v != "" {
		*port = v
	}
	if v := os.Getenv("JWT_SECRET"); v != "" {
		auth.SetJWTSecret([]byte(v))
	} else {
		// Generate a random secret for development — in production, JWT_SECRET must be set
		b := make([]byte, 32)
		rand.Read(b)
		auth.SetJWTSecret(b)
		core.Logger.Warn("JWT_SECRET not set — using random secret (sessions won't survive restarts)")
	}
	if auth.JWTSecretLen() < 16 {
		core.Logger.Error("JWT_SECRET must be at least 16 characters")
		os.Exit(1)
	}

	guardEnvDBCombo(env, *dbDSN)

	db := store.InitDB(*dbDSN)
	defer db.Close()
	jobs.SeedIfEmpty(db)
	mailer.Init()
	notify.InitPush()
	mailer.InitBrand(db)
	handlers.InitUploads()

	// Background jobs and cron share a single cancel context with the HTTP
	// server. On SIGTERM we cancel, wait for each ticker loop to drain its
	// current tick via the WaitGroup, then exit — no goroutines killed
	// mid-write.
	bgCtx, cancelBG := context.WithCancel(context.Background())
	var bgWG sync.WaitGroup
	// Jobs fire within milliseconds of boot (reminders, email queue, billing
	// cron). A dev box must never run them against whatever DB it's pointed
	// at — production only, or explicit ENABLE_JOBS=1 for local testing.
	if env == "production" || os.Getenv("ENABLE_JOBS") == "1" {
		jobs.StartJobs(bgCtx, &bgWG, db)
		jobs.StartCron(bgCtx, &bgWG, db)
	} else {
		core.Logger.Info("background jobs disabled outside production (set ENABLE_JOBS=1 to enable)")
	}

	r := server.Build(db)

	core.Logger.Info("server ready", "addr", ":"+*port, "db", "postgresql")
	srv := &http.Server{
		Addr:         ":" + *port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	// Graceful shutdown
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			core.Logger.Error("listen failed", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	core.Logger.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		core.Logger.Error("forced shutdown", "err", err)
		os.Exit(1)
	}

	// Signal background loops to exit at their next select, then wait for
	// each in-flight tick to finish so we don't kill a goroutine mid-tx.
	cancelBG()
	bgDone := make(chan struct{})
	go func() { bgWG.Wait(); close(bgDone) }()
	select {
	case <-bgDone:
		core.Logger.Info("background jobs stopped cleanly")
	case <-time.After(15 * time.Second):
		core.Logger.Warn("background jobs did not stop within 15s — exiting anyway")
	}
	core.Logger.Info("server stopped")
}

// guardEnvDBCombo refuses the environment/database combination behind the
// 2026-07-31 incident: a non-production box pointed at a remote database (a
// shell-exported prod DATABASE_URL silently overriding the compose default).
// Local hosts cover bare-metal dev and the compose service name. Override for
// a deliberate remote-DB session (e.g. a restore drill) with ALLOW_REMOTE_DB=1.
func guardEnvDBCombo(env, dsn string) {
	localHosts := map[string]bool{"localhost": true, "127.0.0.1": true, "postgres": true, "::1": true}
	host := ""
	if u, err := url.Parse(dsn); err == nil {
		host = u.Hostname()
	}
	core.Logger.Info("effective config", "env", env, "db_host", host,
		"outbound_enabled", os.Getenv("OUTBOUND_ENABLED") == "1" && env == "production")
	if env == "production" || localHosts[host] || os.Getenv("ALLOW_REMOTE_DB") == "1" {
		return
	}
	core.Logger.Error("refusing to start: non-production env pointed at a remote database — unset DATABASE_URL or set ALLOW_REMOTE_DB=1 if deliberate", "env", env, "db_host", host)
	os.Exit(1)
}
