package main

import (
	"context"
	"crypto/rand"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
	startJobs(db)
	startCron(db)

	hub := newHub()
	r := chi.NewRouter()

	// ── Global middleware ──────────────────────────────────────────────────────
	r.Use(middleware.Logger)
	r.Use(requestID)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders) // CSP, X-Frame-Options, etc.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: true, // required for HttpOnly cookies to be sent cross-origin
	}))
	r.Use(rateLimitAPI)
	r.Use(maxBodySize)

	// Origin validation for state-changing requests (extra CSRF protection).
	// Same-origin requests (frontend served by this same Go server) are
	// always allowed — the check only blocks cross-origin POST/PUT/DELETE
	// from domains we don't recognise.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE" {
				origin := r.Header.Get("Origin")
				if origin != "" {
					ok := false
					// Allow same-origin: check Origin against Host and
					// X-Forwarded-Host (set by Caddy/nginx reverse proxy).
					// Behind a proxy, r.Host is "localhost:8080" but the
					// real origin is "https://studyhub.fit".
					host := r.Host
					if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
						host = fwd
					}
					if strings.Contains(origin, host) {
						ok = true
					}
					for _, ao := range allowedOrigins {
						if origin == ao {
							ok = true
							break
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
	r.With(rateLimitLogin).Post("/api/auth/login", handleLogin(db))
	r.Post("/api/auth/logout", handleLogout)
	r.With(rateLimitLogin).Post("/api/register", handleRegister(db))
	r.With(rateLimitLogin).Post("/api/register-teacher", handleRegisterTeacher(db))
	r.With(rateLimitLogin).Post("/api/forgot-password", handleForgotPassword(db))
	r.With(rateLimitLogin).Post("/api/reset-password", handleResetPassword(db))
	r.With(rateLimitLogin).Post("/api/set-password", handleSetPassword(db))
	r.Get("/api/verify-email", handleVerifyEmail(db))
	r.With(rateLimitLogin).Post("/api/resend-verification", handleResendVerification(db))
	r.Get("/ws", hub.handleWS())

	// ── Authenticated routes ──────────────────────────────────────────────────
	r.Group(func(r chi.Router) {
		r.Use(jwtMiddleware)

		r.Get("/api/auth/me", handleMe(db))
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
			r.Put("/{id}/pay", handleInvoicePay(db))
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
		r.Get("/api/uploads/{filename}", handleServeUpload())

		// Admin-only: user management + registration review + audit logs
		r.Group(func(r chi.Router) {
			r.Use(requireAdmin)
			r.Get("/api/users", handleUsers(db))
			r.Post("/api/users", handleUsers(db))
			r.Delete("/api/users/{id}", handleUserDelete(db))
			r.Post("/api/users/{id}/verify", handleUserVerify(db))
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
	fs := http.FileServer(http.Dir(frontendDir))
	r.Handle("/*", fs)

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
	logger.Info("server stopped")
}
