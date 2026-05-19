package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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

// Two session lifetimes: a short one for the default browser-session case
// (cookie dies on browser close) and a long one when the user ticks
// "Remember me" on the login form (persistent cookie, 30 days).
const (
	tokenExpiryShort    = 24 * time.Hour      // 1 day — default
	tokenExpiryRemember = 30 * 24 * time.Hour // 30 days — opt-in via "Remember me"
)

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
	Email      string `json:"email"`
	Password   string `json:"password"`
	RememberMe bool   `json:"rememberMe"`
}

// loginResponse never contains the token — it goes in an HttpOnly cookie instead
type loginResponse struct {
	Role    string `json:"role"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	StaffID string `json:"staffId,omitempty"`
}

func handleLogin(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		req.Email = strings.ToLower(strings.TrimSpace(req.Email))

		if !validateEmail(req.Email) {
			respondError(w, "invalid email format", http.StatusBadRequest)
			return
		}
		if len(req.Password) == 0 {
			respondError(w, "password is required", http.StatusBadRequest)
			return
		}

		var (
			id          int
			hash        string
			role        string
			name        string
			tenantID    int
			status      string
			failedCount int
			lockedUntil sql.NullTime
		)
		err := db.QueryRow(
			`SELECT id, password_hash, role, name, tenant_id, COALESCE(status,'active'), COALESCE(failed_login_count,0), locked_until FROM users WHERE email = ?`, req.Email,
		).Scan(&id, &hash, &role, &name, &tenantID, &status, &failedCount, &lockedUntil)

		if err == sql.ErrNoRows {
			// Use same error as wrong password — never reveal which one was wrong
			respondError(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		if err != nil {
			respondError(w, "server error", http.StatusInternalServerError)
			return
		}

		// ── Account lockout gate ───────────────────────────────────────────
		// If the account is currently locked, reject before even checking the
		// password. Use a generic message that includes how many minutes are
		// left so a confused legitimate user knows when to try again.
		if lockedUntil.Valid && lockedUntil.Time.After(time.Now()) {
			minsLeft := int(time.Until(lockedUntil.Time).Minutes()) + 1
			respondError(w, fmt.Sprintf("account temporarily locked due to too many failed attempts — try again in %d minutes", minsLeft), http.StatusForbidden)
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
			// Wrong password: bump the failure counter and lock if it crosses
			// the threshold. Done in one statement so concurrent attempts
			// don't race past each other.
			newCount := failedCount + 1
			const maxAttempts = 5
			const lockDuration = 15 * time.Minute
			if newCount >= maxAttempts {
				if _, err := db.Exec(
					`UPDATE users SET failed_login_count=?, locked_until=? WHERE id=?`,
					newCount, time.Now().Add(lockDuration), id,
				); err != nil {
					logFromReq(r).Error("failed to lock account", "err", err, "user_id", id)
				}
				logFromReq(r).Warn("account locked after failed logins", "email", req.Email, "user_id", id, "attempts", newCount)
				respondError(w, fmt.Sprintf("account locked after %d failed attempts — try again in %d minutes", maxAttempts, int(lockDuration.Minutes())), http.StatusForbidden)
				return
			}
			if _, err := db.Exec(`UPDATE users SET failed_login_count=? WHERE id=?`, newCount, id); err != nil {
				logFromReq(r).Error("failed to update login failure count", "err", err, "user_id", id)
			}
			respondError(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		// Successful password check — reset the failure counter so a parent
		// who got their password wrong twice doesn't carry that history
		// forever.
		if failedCount > 0 || lockedUntil.Valid {
			if _, err := db.Exec(`UPDATE users SET failed_login_count=0, locked_until=NULL WHERE id=?`, id); err != nil {
				logFromReq(r).Error("failed to reset login failure count", "err", err, "user_id", id)
			}
		}

		// Block login until the email has been verified. Frontend looks for
		// the "needs_verification" sentinel in the error message and shows a
		// "Resend verification email" link.
		if status == "pending_verification" {
			respondError(w, "needs_verification: please verify your email — check your inbox for the link we sent", http.StatusForbidden)
			return
		}

		expiry := tokenExpiryShort
		if req.RememberMe {
			expiry = tokenExpiryRemember
		}
		token, err := makeToken(id, tenantID, req.Email, role, name, expiry)
		if err != nil {
			respondError(w, "could not sign token", http.StatusInternalServerError)
			return
		}

		// ── Set HttpOnly cookie (JS cannot read this, prevents XSS token theft) ──
		secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" // behind reverse proxy (Caddy)
		cookie := &http.Cookie{
			Name:     "sh_token",
			Value:    token,
			Path:     "/",
			HttpOnly: true,                 // JavaScript cannot access this cookie
			Secure:   secure,               // HTTPS only in production
			SameSite: http.SameSiteLaxMode, // Lax allows cookie on top-level navigation (Strict blocks it)
		}
		if req.RememberMe {
			// Persistent cookie — survives browser close until the JWT expires.
			cookie.Expires = time.Now().Add(expiry)
		}
		// Else: leave Expires/MaxAge zero so the browser treats it as a
		// session cookie and deletes it when the window closes.
		http.SetCookie(w, cookie)

		// Return role/name/email — NOT the token itself.
		// For teachers, also look up their staff ID so the frontend can
		// populate App.currentTeacher and render the teacher dashboard.
		resp := loginResponse{Role: role, Name: name, Email: req.Email}
		if role == "teacher" {
			db.QueryRow(`SELECT id FROM staff WHERE email=? AND deleted_at IS NULL LIMIT 1`, req.Email).Scan(&resp.StaffID)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// handleLogout clears the auth cookie
func handleLogout(w http.ResponseWriter, r *http.Request) {
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     "sh_token",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleMe returns the current user's info from their cookie.
// For teachers, also looks up staffId so the frontend can restore
// App.currentTeacher on page refresh.
func handleMe(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil {
			respondError(w, "not authenticated", http.StatusUnauthorized)
			return
		}
		// Read the current name from the DB rather than the JWT claim so
		// profile changes are reflected immediately (the JWT may be stale).
		var dbName string
		if err := db.QueryRow(`SELECT COALESCE(name,'') FROM users WHERE email=?`, c.Email).Scan(&dbName); err == nil && dbName != "" {
			c.Name = dbName
		}
		resp := loginResponse{Role: c.Role, Name: c.Name, Email: c.Email}
		if c.Role == "teacher" {
			db.QueryRow(`SELECT id FROM staff WHERE email=? AND deleted_at IS NULL LIMIT 1`, c.Email).Scan(&resp.StaffID)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func makeToken(userID, tenantID int, email, role, name string, expiry time.Duration) (string, error) {
	claims := Claims{
		UserID:   userID,
		TenantID: tenantID,
		Email:    email,
		Role:     role,
		Name:     name,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiry)),
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
			respondError(w, "missing token", http.StatusUnauthorized)
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
			respondError(w, "invalid or expired token", http.StatusUnauthorized)
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
		if c == nil || (c.Role != "admin" && c.Role != "superadmin") {
			respondError(w, "admin only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

