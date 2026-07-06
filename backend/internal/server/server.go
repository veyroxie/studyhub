package server

import (
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/handlers"
	"studyhub/internal/jobs"
	"studyhub/internal/pdf"
	"studyhub/internal/store"
)

// Build assembles the full HTTP router: global middleware, public routes,
// authenticated routes, and the static file server. Extracted from main.go so
// the entrypoint only handles process lifecycle.
func Build(db *store.DB) http.Handler {
	// CORS allowed origins. In production the Go server serves both the
	// frontend and API on the same origin, so the same-origin check in the
	// CSRF middleware handles most cases. This list is a fallback for
	// explicit cross-origin requests (e.g. local dev with separate frontend).
	allowedOrigins := []string{
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"https://studyhub.fit",
		"https://www.studyhub.fit",
	}
	if v := os.Getenv("ALLOWED_ORIGIN"); v != "" {
		allowedOrigins = append(allowedOrigins, v)
	}

	hub := handlers.NewHub()
	r := chi.NewRouter()

	// ── Global middleware ──────────────────────────────────────────────────────
	r.Use(middleware.Logger)
	r.Use(core.RequestID)
	r.Use(middleware.Recoverer)
	// Gzip JSON, HTML, CSS, JS responses. Snapshot payload (~500KB) compresses
	// to ~50KB; saves bandwidth and the bulk of TTFB on slow networks.
	r.Use(middleware.Compress(5))
	r.Use(core.SecurityHeaders) // CSP, X-Frame-Options, etc.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: true, // required for HttpOnly cookies to be sent cross-origin
	}))
	r.Use(core.RateLimitAPI)
	r.Use(core.MaxBodySize)
	r.Use(core.MetricsMiddleware)
	r.Use(auth.CSRFMiddleware)

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
						core.RespondError(w, "origin not allowed", http.StatusForbidden)
						return
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	})

	// ── Public routes (no auth needed) ───────────────────────────────────────
	r.Get("/api/health", handlers.HandleHealth(db))
	r.Get("/metrics", core.HandleMetrics)
	// OpenAPI spec — served from the binary's embedded copy so the schema
	// always matches the running version of the API.
	r.Get("/api/openapi.yaml", handlers.HandleOpenAPI)
	// Public branding — login / register pages fetch this before there's
	// any session to render the correct centre name + colour.
	r.Get("/api/branding", store.HandleBranding(db))
	// Public VAPID key — the browser needs it to create a push subscription.
	// The public key is not a secret.
	r.Get("/api/push/vapid-key", handlers.HandleVapidKey())
	// iCal feed is auth'd via a signed token in the URL — calendar apps
	// don't speak cookies, so this lives outside the JWT middleware group.
	r.Get("/api/calendar/{userID}/{token}", handlers.HandleParentCalendarFeed(db))
	r.With(core.RateLimitLogin).Post("/api/auth/login", auth.HandleLogin(db))
	r.With(core.RateLimitLogin).Post("/api/auth/mfa/verify", auth.HandleMFAVerify(db))
	r.With(core.RateLimitLogin).Post("/api/auth/refresh", store.HandleRefresh(db))
	r.Post("/api/auth/logout", auth.HandleLogout(db))
	r.With(core.RateLimitLogin).Post("/api/register", handlers.HandleRegister(db))
	r.With(core.RateLimitLogin).Post("/api/register-teacher", handlers.HandleRegisterTeacher(db))
	r.With(core.RateLimitLogin).Post("/api/forgot-password", handlers.HandleForgotPassword(db))
	r.With(core.RateLimitLogin).Post("/api/reset-password", handlers.HandleResetPassword(db))
	r.With(core.RateLimitLogin).Post("/api/set-password", handlers.HandleSetPassword(db))
	r.Get("/api/verify-email", handlers.HandleVerifyEmail(db))
	r.With(core.RateLimitLogin).Post("/api/resend-verification", handlers.HandleResendVerification(db))
	r.Get("/ws", hub.HandleWS())

	// Payment webhooks — public, signature-verified internally. No CSRF
	// origin check needed (Stripe/Billplz are not browsers).
	r.Post("/api/payments/webhook/billplz", handlers.HandleBillplzWebhook(db))
	r.Post("/api/payments/webhook/stripe", handlers.HandleStripeWebhook(db))

	// ── Authenticated routes ──────────────────────────────────────────────────
	r.Group(func(r chi.Router) {
		r.Use(auth.JWTMiddleware(db))
		// Bind app.tenant_id GUC per request so RLS policies filter rows
		// even when the handler forgets a tenant WHERE clause.
		r.Use(store.RLSScope(db))
		// Drop the tenant snapshot cache after every successful write so
		// admins / parents see their changes on the next dashboard load
		// instead of waiting up to snapshotCacheTTL.
		r.Use(store.SnapshotCacheInvalidator)

		r.Get("/api/auth/me", auth.HandleMe(db))
		r.Get("/api/account/export-my-data", handlers.HandleDSARExport(db))
		r.Get("/api/account/calendar-url", handlers.HandleParentCalendarURL(db))
		r.Get("/api/account/tos-status", handlers.HandleToSStatus(db))
		r.Post("/api/account/accept-tos", handlers.HandleToSAccept(db))
		r.Post("/api/auth/mfa/setup", auth.HandleMFASetup(db))
		r.Post("/api/auth/mfa/confirm", auth.HandleMFAConfirm(db))
		r.Post("/api/auth/mfa/disable", auth.HandleMFADisable(db))
		r.Get("/api/auth/profile", handlers.HandleProfile(db))
		r.Put("/api/auth/profile", handlers.HandleProfile(db))
		r.Post("/api/auth/change-password", handlers.HandleChangePassword(db))
		r.Post("/api/push/subscribe", handlers.HandlePushSubscribe(db))
		r.Get("/api/snapshot", handlers.HandleSnapshot(db))

		r.Route("/api/students", func(r chi.Router) {
			r.Get("/", handlers.HandleStudents(db))
			r.Post("/", handlers.HandleStudents(db))
			r.Put("/{id}", handlers.HandleStudent(db))
			r.Delete("/{id}", handlers.HandleStudent(db))
			r.Post("/{id}/subscription", handlers.HandleStudentSubscription(db))
			r.Post("/{id}/relink", handlers.HandleStudentRelink(db))
			r.Post("/{id}/note", handlers.HandleStudentNote(db))
		})

		r.Route("/api/classes", func(r chi.Router) {
			r.Get("/", handlers.HandleClasses(db))
			r.Post("/", handlers.HandleClasses(db))
			r.Put("/{id}", handlers.HandleClassByID(db))
			r.Delete("/{id}", handlers.HandleClassByID(db))
		})

		r.Route("/api/staff", func(r chi.Router) {
			r.Get("/", handlers.HandleStaff(db))
			r.Post("/", handlers.HandleStaff(db))
			r.Put("/{id}", handlers.HandleStaffByID(db))
			r.Delete("/{id}", handlers.HandleStaffByID(db))
		})

		r.Route("/api/invoices", func(r chi.Router) {
			r.Get("/", handlers.HandleInvoices(db))
			r.Post("/", handlers.HandleInvoices(db))
			r.Put("/{id}", handlers.HandleInvoiceUpdate(db))
			r.Put("/{id}/pay", handlers.HandleInvoicePay(db))
			r.Post("/{id}/checkout", handlers.HandlePaymentCheckout(db))
			r.Delete("/{id}", handlers.HandleInvoiceDelete(db))
			r.Get("/{id}/pdf", pdf.HandleInvoicePDF(db, false))
			r.Get("/{id}/receipt.pdf", pdf.HandleInvoicePDF(db, true))
		})

		// Manual trigger for the monthly invoice + payroll cron — admin-only.
		r.Post("/api/admin/cron/run-monthly-invoices", jobs.HandleRunMonthlyCron(db))
		r.Post("/api/admin/cron/regenerate-payroll", jobs.HandleRegeneratePayroll(db))

		// Admin hand-corrections to a payroll row (marks it manually_edited so
		// the cron refresh leaves it alone).
		r.Put("/api/payroll/{id}", handlers.HandlePayrollUpdate(db))

		r.Route("/api/announcements", func(r chi.Router) {
			r.Get("/", handlers.HandleAnnouncements(db))
			r.Post("/", handlers.HandleAnnouncements(db))
			r.Put("/{id}", handlers.HandleAnnouncementUpdate(db))
			r.Put("/{id}/approve", handlers.HandleAnnouncementApprove(db))
			r.Delete("/{id}", handlers.HandleAnnouncementDelete(db))
		})

		r.Route("/api/attendance", func(r chi.Router) {
			r.Get("/", handlers.HandleAttendance(db, hub))
			r.Post("/", handlers.HandleAttendance(db, hub))
		})

		r.Route("/api/feedback", func(r chi.Router) {
			r.Get("/", handlers.HandleListFeedback(db))
			r.Post("/", handlers.HandleCreateFeedback(db))
			r.Put("/{id}", handlers.HandleUpdateFeedback(db))
			r.Delete("/{id}", handlers.HandleDeleteFeedback(db))
		})

		r.Post("/api/feedback-replies", handlers.HandleCreateFeedbackReply(db))

		r.Route("/api/progress-reports", func(r chi.Router) {
			r.Get("/", handlers.HandleProgressReports(db))
			r.Post("/", handlers.HandleProgressReports(db))
			r.Put("/{id}", handlers.HandleProgressReportByID(db))
			r.Delete("/{id}", handlers.HandleProgressReportByID(db))
			r.Get("/{id}/pdf", handlers.HandleProgressReportPDF(db))
		})

		r.Route("/api/workshops", func(r chi.Router) {
			r.Get("/", handlers.HandleListWorkshops(db))
			r.Post("/", handlers.HandleCreateWorkshop(db))
			r.Put("/{id}", handlers.HandleUpdateWorkshop(db))
			r.Delete("/{id}", handlers.HandleDeleteWorkshop(db))
		})

		r.Put("/api/pricing/{id}", handlers.HandleUpdatePricingTier(db))

		r.Route("/api/self-study", func(r chi.Router) {
			r.Get("/", handlers.HandleListSelfStudy(db))
			r.Post("/", handlers.HandleCreateSelfStudy(db))
			r.Delete("/{id}", handlers.HandleDeleteSelfStudy(db))
		})

		r.Route("/api/performance-reviews", func(r chi.Router) {
			r.Get("/", handlers.HandleListPerformanceReviews(db))
			r.Post("/", handlers.HandleCreatePerformanceReview(db))
			r.Delete("/{id}", handlers.HandleDeletePerformanceReview(db))
		})

		r.Route("/api/cancelled-classes", func(r chi.Router) {
			r.Get("/", handlers.HandleListCancelledClasses(db))
			r.Post("/", handlers.HandleCreateCancelledClass(db))
		})

		r.Route("/api/replacement-credits", func(r chi.Router) {
			r.Get("/", handlers.HandleListReplacementCredits(db))
			r.Post("/", handlers.HandleCreateReplacementCredit(db))
			r.Delete("/{id}", handlers.HandleDeleteReplacementCredit(db))
			r.Get("/balance", handlers.HandleReplacementBalance(db))
		})

		r.Post("/api/enrollment-requests", handlers.HandleEnrollmentRequest(db))

		r.Route("/api/families", func(r chi.Router) {
			r.Get("/", handlers.HandleFamilies(db))
			r.Post("/", handlers.HandleFamilies(db))
			r.Put("/{id}", handlers.HandleFamilyByID(db))
			r.Delete("/{id}", handlers.HandleFamilyByID(db))
			r.Delete("/{id}/pdpa", handlers.HandleFamilyPDPADelete(db))
			r.Get("/{id}/referral", handlers.HandleFamilyReferral(db))
		})

		r.Route("/api/referrals", func(r chi.Router) {
			r.Get("/", handlers.HandleReferrals(db))
			r.Post("/{id}/earn", handlers.HandleReferralEarn(db))
			r.Post("/{id}/consume", handlers.HandleReferralConsume(db))
		})

		r.Route("/api/holidays", func(r chi.Router) {
			r.Get("/", handlers.HandleListHolidays(db))
			r.Post("/", handlers.HandleCreateHoliday(db))
			r.Put("/{id}", handlers.HandleUpdateHoliday(db))
			r.Delete("/{id}", handlers.HandleDeleteHoliday(db))
		})

		// Payment proof upload/serve
		r.Post("/api/upload-proof", handlers.HandleUploadProof(db))
		r.Get("/api/uploads/{filename}", handlers.HandleServeUpload(db))

		// Admin-only: user management + registration review + audit logs
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAdmin)
			r.Get("/api/users", handlers.HandleUsers(db))
			r.Post("/api/users", handlers.HandleUsers(db))
			r.Delete("/api/users/{id}", handlers.HandleUserDelete(db))
			r.Post("/api/users/{id}/verify", handlers.HandleUserVerify(db))
			r.Post("/api/users/{id}/unlock", handlers.HandleAdminUnlockUser(db))
			r.Get("/api/admin/settings", store.HandleAdminSettings(db))
			r.Put("/api/admin/settings", store.HandleAdminSettings(db))
			r.Post("/api/users/{id}/resend-verification", handlers.HandleUserResendVerification(db))
			r.Post("/api/admin/import", handlers.HandleImport(db))
			r.Post("/api/admin/clear-seed", handlers.HandleClearSeedData(db))
			r.Post("/api/registrations/{id}/approve", handlers.HandleRegistrationApprove(db))
			r.Delete("/api/registrations/{id}", handlers.HandleRegistrationReject(db))
			r.Get("/api/audit-logs", handlers.HandleAuditLogs(db))
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
	r.Handle("/*", handlers.StaticCacheHandler(frontendDir))

	return r
}
