package main

import (
	"context"
	"crypto/rand"
	"flag"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/joho/godotenv"
)

// Limit request body size globally (1MB). File upload has its own 5MB limit.
func maxBodySize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" || r.Method == "PUT" {
			// Skip file upload endpoint (has its own limit)
			if r.URL.Path != "/api/upload-proof" {
				r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
			}
		}
		next.ServeHTTP(w, r)
	})
}

// appEnv returns the current environment ("development", "staging",
// "production", etc.). Defaults to "development" so local dev never needs
// the variable set.
func appEnv() string {
	if v := os.Getenv("APP_ENV"); v != "" {
		return v
	}
	return "development"
}

func main() {
	// Load .env files in order: .env first (legacy / shared defaults), then
	// .env.${APP_ENV} on top so per-environment overrides win. Both are
	// silently optional — production typically gets env vars from the
	// orchestrator (Docker Compose, systemd) instead of files.
	env := appEnv()
	_ = godotenv.Load(".env")
	_ = godotenv.Load(".env." + env)
	initLogger()
	logger.Info("starting", "env", env, "version", buildVersion)

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
		jwtSecret = []byte(v)
	} else {
		// Generate a random secret for development — in production, JWT_SECRET must be set
		b := make([]byte, 32)
		rand.Read(b)
		jwtSecret = b
		logger.Warn("JWT_SECRET not set — using random secret (sessions won't survive restarts)")
	}
	if len(jwtSecret) < 16 {
		logger.Error("JWT_SECRET must be at least 16 characters")
		os.Exit(1)
	}

	// CORS allowed origins. In production the Go server serves both the
	// frontend and API on the same origin, so the same-origin check in the
	// CSRF middleware handles most cases. This list is a fallback for
	// explicit cross-origin requests (e.g. local dev with separate frontend).
	allowedOrigins := []string{
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"https://studyhub.fit",
		"http://studyhub.fit",
	}
	if v := os.Getenv("ALLOWED_ORIGIN"); v != "" {
		allowedOrigins = append(allowedOrigins, v)
	}

	db := initDB(*dbDSN)
	defer db.Close()
	seedIfEmpty(db)
	initMailer()
	initEmailBrand(db)
	initUploads()

	// Background jobs and cron share a single cancel context with the HTTP
	// server. On SIGTERM we cancel, wait for each ticker loop to drain its
	// current tick via the WaitGroup, then exit — no goroutines killed
	// mid-write.
	bgCtx, cancelBG := context.WithCancel(context.Background())
	var bgWG sync.WaitGroup
	startJobs(bgCtx, &bgWG, db)
	startCron(bgCtx, &bgWG, db)

	hub := newHub()
	r := chi.NewRouter()

	// ── Global middleware ──────────────────────────────────────────────────────
	r.Use(middleware.Logger)
	r.Use(requestID)
	r.Use(middleware.Recoverer)
	// Gzip JSON, HTML, CSS, JS responses. Snapshot payload (~500KB) compresses
	// to ~50KB; saves bandwidth and the bulk of TTFB on slow networks.
	r.Use(middleware.Compress(5))
	r.Use(securityHeaders) // CSP, X-Frame-Options, etc.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: true, // required for HttpOnly cookies to be sent cross-origin
	}))
	r.Use(rateLimitAPI)
	r.Use(maxBodySize)
	r.Use(metricsMiddleware)
	r.Use(csrfMiddleware)

	// Origin validation for state-changing requests (extra CSRF protection).
	// Same-origin requests (frontend served by this same Go server) are
	// always allowed — the check only blocks cross-origin POST/PUT/DELETE
	// from domains we don't recognise. Compare the parsed Origin host
	// exactly against the request host (and proxy-supplied X-Forwarded-Host)
	// — substring matching is unsafe because "studyhub.fit.attacker.com"
	// contains "studyhub.fit".
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Payment webhooks aren't browsers and don't carry Origin —
			// signature verification inside the handler is the auth.
			if strings.HasPrefix(r.URL.Path, "/api/payments/webhook/") {
				next.ServeHTTP(w, r)
				return
			}
			if r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE" {
				origin := r.Header.Get("Origin")
				if origin != "" {
					ok := false
					originURL, err := url.Parse(origin)
					if err == nil && originURL.Host != "" {
						host := r.Host
						if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
							host = fwd
						}
						if originURL.Host == host {
							ok = true
						}
						if !ok {
							for _, ao := range allowedOrigins {
								if origin == ao {
									ok = true
									break
								}
							}
						}
					}
					if !ok {
						respondError(w, "origin not allowed", http.StatusForbidden)
						return
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	})

	// ── Public routes (no auth needed) ───────────────────────────────────────
	r.Get("/api/health", handleHealth(db))
	r.Get("/metrics", handleMetrics)
	// OpenAPI spec — served from the binary's embedded copy so the schema
	// always matches the running version of the API.
	r.Get("/api/openapi.yaml", handleOpenAPI)
	// Public branding — login / register pages fetch this before there's
	// any session to render the correct centre name + colour.
	r.Get("/api/branding", handleBranding(db))
	// iCal feed is auth'd via a signed token in the URL — calendar apps
	// don't speak cookies, so this lives outside the JWT middleware group.
	r.Get("/api/calendar/{userID}/{token}", handleParentCalendarFeed(db))
	r.With(rateLimitLogin).Post("/api/auth/login", handleLogin(db))
	r.With(rateLimitLogin).Post("/api/auth/mfa/verify", handleMFAVerify(db))
	r.With(rateLimitLogin).Post("/api/auth/refresh", handleRefresh(db))
	r.Post("/api/auth/logout", handleLogout(db))
	r.With(rateLimitLogin).Post("/api/register", handleRegister(db))
	r.With(rateLimitLogin).Post("/api/register-teacher", handleRegisterTeacher(db))
	r.With(rateLimitLogin).Post("/api/forgot-password", handleForgotPassword(db))
	r.With(rateLimitLogin).Post("/api/reset-password", handleResetPassword(db))
	r.With(rateLimitLogin).Post("/api/set-password", handleSetPassword(db))
	r.Get("/api/verify-email", handleVerifyEmail(db))
	r.With(rateLimitLogin).Post("/api/resend-verification", handleResendVerification(db))
	r.Get("/ws", hub.handleWS())

	// Payment webhooks — public, signature-verified internally. No CSRF
	// origin check needed (Stripe/Billplz are not browsers).
	r.Post("/api/payments/webhook/billplz", handleBillplzWebhook(db))
	r.Post("/api/payments/webhook/stripe", handleStripeWebhook(db))

	// ── Authenticated routes ──────────────────────────────────────────────────
	r.Group(func(r chi.Router) {
		r.Use(jwtMiddleware(db))
		// Bind app.tenant_id GUC per request so RLS policies filter rows
		// even when the handler forgets a tenant WHERE clause.
		r.Use(rlsScope(db))
		// Drop the tenant snapshot cache after every successful write so
		// admins / parents see their changes on the next dashboard load
		// instead of waiting up to snapshotCacheTTL.
		r.Use(snapshotCacheInvalidator)

		r.Get("/api/auth/me", handleMe(db))
		r.Get("/api/account/export-my-data", handleDSARExport(db))
		r.Get("/api/account/calendar-url", handleParentCalendarURL(db))
		r.Get("/api/account/tos-status", handleToSStatus(db))
		r.Post("/api/account/accept-tos", handleToSAccept(db))
		r.Post("/api/auth/mfa/setup", handleMFASetup(db))
		r.Post("/api/auth/mfa/confirm", handleMFAConfirm(db))
		r.Post("/api/auth/mfa/disable", handleMFADisable(db))
		r.Get("/api/auth/profile", handleProfile(db))
		r.Put("/api/auth/profile", handleProfile(db))
		r.Post("/api/auth/change-password", handleChangePassword(db))
		r.Get("/api/snapshot", handleSnapshot(db))

		r.Route("/api/students", func(r chi.Router) {
			r.Get("/", handleStudents(db))
			r.Post("/", handleStudents(db))
			r.Put("/{id}", handleStudent(db))
			r.Delete("/{id}", handleStudent(db))
			r.Post("/{id}/subscription", handleStudentSubscription(db))
			r.Post("/{id}/relink", handleStudentRelink(db))
		})

		r.Route("/api/classes", func(r chi.Router) {
			r.Get("/", handleClasses(db))
			r.Post("/", handleClasses(db))
			r.Put("/{id}", handleClassByID(db))
			r.Delete("/{id}", handleClassByID(db))
		})

		r.Route("/api/staff", func(r chi.Router) {
			r.Get("/", handleStaff(db))
			r.Post("/", handleStaff(db))
			r.Put("/{id}", handleStaffByID(db))
			r.Delete("/{id}", handleStaffByID(db))
		})

		r.Route("/api/invoices", func(r chi.Router) {
			r.Get("/", handleInvoices(db))
			r.Post("/", handleInvoices(db))
			r.Put("/{id}", handleInvoiceUpdate(db))
			r.Put("/{id}/pay", handleInvoicePay(db))
			r.Post("/{id}/checkout", handlePaymentCheckout(db))
			r.Delete("/{id}", handleInvoiceDelete(db))
			r.Get("/{id}/pdf", handleInvoicePDF(db, false))
			r.Get("/{id}/receipt.pdf", handleInvoicePDF(db, true))
		})

		// Manual trigger for the monthly invoice + payroll cron — admin-only.
		r.Post("/api/admin/cron/run-monthly-invoices", handleRunMonthlyCron(db))
		r.Post("/api/admin/cron/regenerate-payroll", handleRegeneratePayroll(db))

		r.Route("/api/announcements", func(r chi.Router) {
			r.Get("/", handleAnnouncements(db))
			r.Post("/", handleAnnouncements(db))
			r.Put("/{id}", handleAnnouncementUpdate(db))
			r.Put("/{id}/approve", handleAnnouncementApprove(db))
			r.Delete("/{id}", handleAnnouncementDelete(db))
		})

		r.Route("/api/attendance", func(r chi.Router) {
			r.Get("/", handleAttendance(db, hub))
			r.Post("/", handleAttendance(db, hub))
		})

		r.Route("/api/feedback", func(r chi.Router) {
			r.Get("/", handleListFeedback(db))
			r.Post("/", handleCreateFeedback(db))
			r.Put("/{id}", handleUpdateFeedback(db))
			r.Delete("/{id}", handleDeleteFeedback(db))
		})

		r.Post("/api/feedback-replies", handleCreateFeedbackReply(db))

		r.Route("/api/progress-reports", func(r chi.Router) {
			r.Get("/", handleProgressReports(db))
			r.Post("/", handleProgressReports(db))
			r.Put("/{id}", handleProgressReportByID(db))
			r.Delete("/{id}", handleProgressReportByID(db))
			r.Get("/{id}/pdf", handleProgressReportPDF(db))
		})

		r.Route("/api/workshops", func(r chi.Router) {
			r.Get("/", handleListWorkshops(db))
			r.Post("/", handleCreateWorkshop(db))
			r.Put("/{id}", handleUpdateWorkshop(db))
			r.Delete("/{id}", handleDeleteWorkshop(db))
		})

		r.Put("/api/pricing/{id}", handleUpdatePricingTier(db))

		r.Route("/api/self-study", func(r chi.Router) {
			r.Get("/", handleListSelfStudy(db))
			r.Post("/", handleCreateSelfStudy(db))
			r.Delete("/{id}", handleDeleteSelfStudy(db))
		})

		r.Route("/api/performance-reviews", func(r chi.Router) {
			r.Get("/", handleListPerformanceReviews(db))
			r.Post("/", handleCreatePerformanceReview(db))
			r.Delete("/{id}", handleDeletePerformanceReview(db))
		})

		r.Route("/api/cancelled-classes", func(r chi.Router) {
			r.Get("/", handleListCancelledClasses(db))
			r.Post("/", handleCreateCancelledClass(db))
		})

		r.Route("/api/replacement-credits", func(r chi.Router) {
			r.Get("/", handleListReplacementCredits(db))
			r.Post("/", handleCreateReplacementCredit(db))
			r.Delete("/{id}", handleDeleteReplacementCredit(db))
			r.Get("/balance", handleReplacementBalance(db))
		})

		r.Post("/api/enrollment-requests", handleEnrollmentRequest(db))

		r.Route("/api/families", func(r chi.Router) {
			r.Get("/", handleFamilies(db))
			r.Post("/", handleFamilies(db))
			r.Put("/{id}", handleFamilyByID(db))
			r.Delete("/{id}", handleFamilyByID(db))
			r.Delete("/{id}/pdpa", handleFamilyPDPADelete(db))
			r.Get("/{id}/referral", handleFamilyReferral(db))
		})

		r.Route("/api/referrals", func(r chi.Router) {
			r.Get("/", handleReferrals(db))
			r.Post("/{id}/earn", handleReferralEarn(db))
			r.Post("/{id}/consume", handleReferralConsume(db))
		})

		r.Route("/api/holidays", func(r chi.Router) {
			r.Get("/", handleListHolidays(db))
			r.Post("/", handleCreateHoliday(db))
			r.Put("/{id}", handleUpdateHoliday(db))
			r.Delete("/{id}", handleDeleteHoliday(db))
		})

		// Payment proof upload/serve
		r.Post("/api/upload-proof", handleUploadProof(db))
		r.Get("/api/uploads/{filename}", handleServeUpload(db))

		// Admin-only: user management + registration review + audit logs
		r.Group(func(r chi.Router) {
			r.Use(requireAdmin)
			r.Get("/api/users", handleUsers(db))
			r.Post("/api/users", handleUsers(db))
			r.Delete("/api/users/{id}", handleUserDelete(db))
			r.Post("/api/users/{id}/verify", handleUserVerify(db))
			r.Post("/api/users/{id}/unlock", handleAdminUnlockUser(db))
			r.Get("/api/admin/settings", handleAdminSettings(db))
			r.Put("/api/admin/settings", handleAdminSettings(db))
			r.Post("/api/users/{id}/resend-verification", handleUserResendVerification(db))
			r.Post("/api/admin/import", handleImport(db))
			r.Post("/api/admin/clear-seed", handleClearSeedData(db))
			r.Post("/api/registrations/{id}/approve", handleRegistrationApprove(db))
			r.Delete("/api/registrations/{id}", handleRegistrationReject(db))
			r.Get("/api/audit-logs", handleAuditLogs(db))
		})
	})

	// ── Serve frontend static files ───────────────────────────────────────────
	// Dev: binary runs from backend/, frontend is at ../frontend/
	// Docker: binary runs from /app/, frontend is copied to /app/frontend/
	frontendDir := "../frontend"
	if _, err := os.Stat(frontendDir); os.IsNotExist(err) {
		frontendDir = "./frontend"
	}
	// Static assets get Cache-Control + ETag. HTML shells fall through to
	// the bare fileserver so deploys are immediately visible.
	r.Handle("/*", staticCacheHandler(frontendDir))

	logger.Info("server ready", "addr", ":"+*port, "db", "postgresql")
	server := &http.Server{
		Addr:         ":" + *port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	// Graceful shutdown
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("listen failed", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("forced shutdown", "err", err)
		os.Exit(1)
	}

	// Signal background loops to exit at their next select, then wait for
	// each in-flight tick to finish so we don't kill a goroutine mid-tx.
	cancelBG()
	bgDone := make(chan struct{})
	go func() { bgWG.Wait(); close(bgDone) }()
	select {
	case <-bgDone:
		logger.Info("background jobs stopped cleanly")
	case <-time.After(15 * time.Second):
		logger.Warn("background jobs did not stop within 15s — exiting anyway")
	}
	logger.Info("server stopped")
}
