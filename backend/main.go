package main

import (
	"context"
	"crypto/rand"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
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

func main() {
	// Load .env file if present (silently ignored in production where env vars are set directly)
	_ = godotenv.Load()

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
		log.Printf("WARNING: JWT_SECRET not set — using random secret (sessions won't survive restarts)")
	}
	if len(jwtSecret) < 16 {
		log.Fatal("JWT_SECRET must be at least 16 characters")
	}

	// CORS allowed origins — use env var in production (e.g. https://yourdomain.com)
	allowedOrigins := []string{"http://localhost:8080", "http://127.0.0.1:8080"}
	if v := os.Getenv("ALLOWED_ORIGIN"); v != "" {
		allowedOrigins = []string{v}
	}

	db := initDB(*dbDSN)
	defer db.Close()
	seedIfEmpty(db)

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

	// Origin validation for state-changing requests (extra CSRF protection)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE" {
				origin := r.Header.Get("Origin")
				if origin != "" {
					ok := false
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
	r.With(rateLimitLogin).Post("/api/auth/login", handleLogin(db))
	r.Post("/api/auth/logout", handleLogout)
	r.With(rateLimitLogin).Post("/api/register", handleRegister(db))
	r.With(rateLimitLogin).Post("/api/register-teacher", handleRegisterTeacher(db))
	r.With(rateLimitLogin).Post("/api/forgot-password", handleForgotPassword(db))
	r.Get("/ws", hub.handleWS())

	// ── Authenticated routes ──────────────────────────────────────────────────
	r.Group(func(r chi.Router) {
		r.Use(jwtMiddleware)

		r.Get("/api/auth/me", handleMe)
		r.Get("/api/snapshot", handleSnapshot(db))

		r.Route("/api/students", func(r chi.Router) {
			r.Get("/", handleStudents(db))
			r.Post("/", handleStudents(db))
			r.Put("/{id}", handleStudent(db))
			r.Delete("/{id}", handleStudent(db))
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
		})

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

		r.Route("/api/subjects", func(r chi.Router) {
			r.Get("/", handleListSubjects(db))
			r.Post("/", handleCreateSubject(db))
			r.Put("/{id}", handleUpdateSubject(db))
			r.Delete("/{id}", handleDeleteSubject(db))
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

	log.Printf("StudyHub running on http://localhost:%s", *port)
	log.Printf("Database: PostgreSQL")
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
			log.Fatal(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}
	log.Println("Server stopped")
}
