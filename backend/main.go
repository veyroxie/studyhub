package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env file if present (silently ignored in production where env vars are set directly)
	_ = godotenv.Load()

	dbPath := flag.String("db", "studyhub.db", "path to SQLite database file")
	port := flag.String("port", "8080", "HTTP port")
	flag.Parse()

	// Override with environment variables if set (useful for deployment)
	if v := os.Getenv("DB_PATH"); v != "" { *dbPath = v }
	if v := os.Getenv("PORT"); v != "" { *port = v }
	if v := os.Getenv("JWT_SECRET"); v != "" { jwtSecret = []byte(v) }

	// CORS allowed origins — use env var in production (e.g. https://yourdomain.com)
	allowedOrigins := []string{"http://localhost:8080", "http://127.0.0.1:8080"}
	if v := os.Getenv("ALLOWED_ORIGIN"); v != "" {
		allowedOrigins = []string{v}
	}

	db := initDB(*dbPath)
	defer db.Close()
	seedIfEmpty(db)

	hub := newHub()
	r := chi.NewRouter()

	// ── Global middleware ──────────────────────────────────────────────────────
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders) // CSP, X-Frame-Options, etc.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: true, // required for HttpOnly cookies to be sent cross-origin
	}))

	// ── Public routes (no auth needed) ───────────────────────────────────────
	r.With(rateLimitLogin).Post("/api/auth/login", handleLogin(db))
	r.Post("/api/auth/logout", handleLogout)
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
		})

		r.Route("/api/staff", func(r chi.Router) {
			r.Get("/", handleStaff(db))
			r.Post("/", handleStaff(db))
		})

		r.Route("/api/invoices", func(r chi.Router) {
			r.Get("/", handleInvoices(db))
			r.Post("/", handleInvoices(db))
			r.Put("/{id}/pay", handleInvoicePay(db))
		})

		r.Route("/api/announcements", func(r chi.Router) {
			r.Get("/", handleAnnouncements(db))
			r.Post("/", handleAnnouncements(db))
			r.Delete("/{id}", handleAnnouncementDelete(db))
		})

		r.Route("/api/attendance", func(r chi.Router) {
			r.Get("/", handleAttendance(db, hub))
			r.Post("/", handleAttendance(db, hub))
		})

		// Admin-only: user management
		r.Group(func(r chi.Router) {
			r.Use(requireAdmin)
			r.Get("/api/users", handleUsers(db))
			r.Post("/api/users", handleUsers(db))
			r.Delete("/api/users/{id}", handleUserDelete(db))
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
	log.Printf("Database: %s", *dbPath)
	log.Printf("Login: admin@studyhub.com / admin123")
	if err := http.ListenAndServe(":"+*port, r); err != nil {
		log.Fatal(err)
	}
}
