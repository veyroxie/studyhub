package core

import (
	"context"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the JWT payload carried in the auth cookie. It lives in core
// because every layer needs to read it: the auth middleware SETS it on the
// request context, handlers and store helpers READ it. Keeping the type +
// context key + getter here (rather than in auth) avoids an import cycle.
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

// ClaimsFrom returns the authenticated user's claims from the request context,
// or nil when the request is unauthenticated.
func ClaimsFrom(r *http.Request) *Claims {
	c, _ := r.Context().Value(claimsKey).(*Claims)
	return c
}

// WithClaims returns a copy of r whose context carries the given claims. Used
// by the auth middleware after it validates the token.
func WithClaims(r *http.Request, c *Claims) *http.Request {
	ctx := context.WithValue(r.Context(), claimsKey, c)
	return r.WithContext(ctx)
}

// IsAdminRole returns true for both "admin" and "superadmin" claims. Used by
// handlers that mutate tenant-level data — the bare check `c.Role != "admin"`
// would lock superadmins out of routine work like creating an invoice or
// editing a holiday, which was a recurring drift across handlers.
func IsAdminRole(c *Claims) bool {
	if c == nil {
		return false
	}
	return c.Role == "admin" || c.Role == "superadmin"
}

// IsStaffRole returns true for any role allowed to mutate teaching-side
// records: admin, superadmin, or teacher. Use this for endpoints like
// feedback / progress reports / replacement credits where teachers
// legitimately participate.
func IsStaffRole(c *Claims) bool {
	if c == nil {
		return false
	}
	return c.Role == "admin" || c.Role == "superadmin" || c.Role == "teacher"
}
