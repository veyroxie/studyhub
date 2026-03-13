package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var emailRe = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

func validateEmail(email string) bool {
	return emailRe.MatchString(email)
}

func validatePassword(password string) (bool, string) {
	if len(password) < 8 {
		return false, "password must be at least 8 characters"
	}
	return true, ""
}

// jwtSecret is set from JWT_SECRET env var in main.go — this default is only used in dev
var jwtSecret []byte

const tokenExpiry = 7 * 24 * time.Hour // 7 days — long enough to not annoy users

type Claims struct {
	UserID   int    `json:"userId"`
	TenantID int    `json:"tenantId"` // 0 = superadmin (cross-tenant access)
	Email    string `json:"email"`
	Role     string `json:"role"` // "superadmin" | "admin" | "teacher" | "parent"
	Name     string `json:"name"`
	jwt.RegisteredClaims
}

type contextKey string

const claimsKey contextKey = "claims"

// ── Login ─────────────────────────────────────────────────────────────────────

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// loginResponse never contains the token — it goes in an HttpOnly cookie instead
type loginResponse struct {
	Role  string `json:"role"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func handleLogin(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		req.Email = strings.ToLower(strings.TrimSpace(req.Email))

		if !validateEmail(req.Email) {
			http.Error(w, "invalid email format", http.StatusBadRequest)
			return
		}
		if len(req.Password) == 0 {
			http.Error(w, "password is required", http.StatusBadRequest)
			return
		}

		var (
			id       int
			hash     string
			role     string
			name     string
			tenantID int
		)
		err := db.QueryRow(
			`SELECT id, password_hash, role, name, tenant_id FROM users WHERE email = ?`, req.Email,
		).Scan(&id, &hash, &role, &name, &tenantID)

		if err == sql.ErrNoRows {
			// Use same error as wrong password — never reveal which one was wrong
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		if err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		token, err := makeToken(id, tenantID, req.Email, role, name)
		if err != nil {
			http.Error(w, "could not sign token", http.StatusInternalServerError)
			return
		}

		// ── Set HttpOnly cookie (JS cannot read this, prevents XSS token theft) ──
		secure := r.TLS != nil // only Secure flag when running over HTTPS
		http.SetCookie(w, &http.Cookie{
			Name:     "sh_token",
			Value:    token,
			Path:     "/",
			Expires:  time.Now().Add(tokenExpiry),
			HttpOnly: true,           // JavaScript cannot access this cookie
			Secure:   secure,         // HTTPS only in production
			SameSite: http.SameSiteStrictMode, // blocks cross-site request forgery
		})

		// Return role/name/email — NOT the token itself
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(loginResponse{Role: role, Name: name, Email: req.Email})
	}
}

// handleLogout clears the auth cookie
func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "sh_token",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleMe returns the current user's info from their cookie
func handleMe(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	if c == nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(loginResponse{Role: c.Role, Name: c.Name, Email: c.Email})
}

func makeToken(userID, tenantID int, email, role, name string) (string, error) {
	claims := Claims{
		UserID:   userID,
		TenantID: tenantID,
		Email:    email,
		Role:     role,
		Name:     name,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(tokenExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret)
}

// ── JWT Middleware ─────────────────────────────────────────────────────────────
// Reads the token from the HttpOnly cookie (not Authorization header).
// Falls back to Bearer header for API clients / testing.

func jwtMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr := ""

		// 1. Try HttpOnly cookie first (browser clients)
		if cookie, err := r.Cookie("sh_token"); err == nil {
			tokenStr = cookie.Value
		}

		// 2. Fall back to Authorization header (API clients, tests)
		if tokenStr == "" {
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				tokenStr = strings.TrimPrefix(auth, "Bearer ")
			}
		}

		if tokenStr == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return jwtSecret, nil
		})
		if err != nil || !token.Valid {
			// Clear the bad cookie
			http.SetCookie(w, &http.Cookie{Name: "sh_token", Value: "", Path: "/", Expires: time.Unix(0, 0), HttpOnly: true})
			http.Error(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func claimsFrom(r *http.Request) *Claims {
	c, _ := r.Context().Value(claimsKey).(*Claims)
	return c
}

func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			http.Error(w, "admin only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requireAdminOrTeacher(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || (c.Role != "admin" && c.Role != "teacher") {
			http.Error(w, "staff only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
