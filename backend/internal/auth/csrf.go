package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"studyhub/internal/core"
)

// CSRF double-submit token. Defense-in-depth on top of the existing
// Origin-header check (which has a bypass when the Origin header is
// missing — non-browser clients).
//
// How it works:
//   1. `sh_csrf` cookie (NOT HttpOnly — JS needs to read it) is set on
//      every response whenever the request didn't already carry one.
//      Random 32 bytes hex-encoded. Lifetime = the JWT cookie lifetime.
//   2. Frontend reads document.cookie, copies the value into the
//      X-CSRF-Token header on every state-changing API call.
//   3. This middleware compares header to cookie value with constant-time
//      compare. Mismatch → 403.
//
// Why this stops CSRF:
//   - Attacker on another origin can't read sh_csrf from their JS context
//     (Same-Origin Policy on document.cookie).
//   - Attacker can't include the cookie in a top-level form POST because
//     of SameSite=Lax — but even if they could, they can't read the value
//     to set X-CSRF-Token.
//   - A request without both cookie AND matching header is rejected.
//
// Exempt paths: payment webhooks (signature-verified separately), login,
// register, forgot-password (no session yet). These remain protected by
// rate limits + signature validation.

const csrfCookieName = "sh_csrf"
const csrfHeaderName = "X-CSRF-Token"

// csrfExempt routes that legitimately have no session cookie to compare
// against. Webhooks verify signatures; auth bootstrap endpoints are
// rate-limited at the API gateway.
var csrfExemptPaths = []string{
	"/api/auth/login",
	"/api/auth/mfa/verify",
	"/api/auth/refresh", // exchanges its own cookie; no session cookie pair
	"/api/register",
	"/api/register-teacher",
	"/api/forgot-password",
	"/api/reset-password",
	"/api/set-password",
	"/api/resend-verification",
	"/api/payments/webhook/",
}

func csrfIsExempt(path string) bool {
	for _, p := range csrfExemptPaths {
		if path == p || strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// csrfMiddleware enforces the double-submit cookie check on state-changing
// requests. Always emits a fresh cookie on the response when one is
// missing — that gives the first authenticated GET a chance to set the
// token before the user starts mutating.
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always make sure the response carries a token. The frontend
		// reads it on next request.
		ensureCSRFCookie(w, r)

		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if csrfIsExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie(csrfCookieName)
		if err != nil || cookie.Value == "" {
			core.RespondError(w, "missing CSRF token", http.StatusForbidden)
			return
		}
		header := r.Header.Get(csrfHeaderName)
		if header == "" {
			core.RespondError(w, "missing CSRF header", http.StatusForbidden)
			return
		}
		if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) != 1 {
			core.RespondError(w, "CSRF token mismatch", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ensureCSRFCookie issues a fresh sh_csrf cookie when the request doesn't
// already carry one. Idempotent for clients that already have a token.
func ensureCSRFCookie(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(csrfCookieName); err == nil && cookie.Value != "" {
		return
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return
	}
	token := hex.EncodeToString(b)
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false, // intentionally JS-readable — frontend copies to header
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}
