package main

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// ── Rate Limiter ──────────────────────────────────────────────────────────────
// Simple in-memory token bucket: max 5 login attempts per IP per minute.

type ipBucket struct {
	count       int
	windowStart time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*ipBucket
}

var loginRateLimiter = &rateLimiter{buckets: make(map[string]*ipBucket)}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Clean up old entries every so often (simple GC)
	if len(rl.buckets) > 10000 {
		now := time.Now()
		for k, b := range rl.buckets {
			if now.Sub(b.windowStart) > 2*time.Minute {
				delete(rl.buckets, k)
			}
		}
	}

	b, ok := rl.buckets[ip]
	if !ok || time.Since(b.windowStart) > time.Minute {
		rl.buckets[ip] = &ipBucket{count: 1, windowStart: time.Now()}
		return true
	}
	b.count++
	return b.count <= 5
}

func realIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}

// rateLimitMiddleware wraps a handler with per-IP rate limiting
func rateLimitLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loginRateLimiter.allow(realIP(r)) {
			http.Error(w, "too many login attempts, please wait a minute", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ── Security Headers ──────────────────────────────────────────────────────────
// Applied to every response. Prevents common web attacks.

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// Prevent clickjacking
		h.Set("X-Frame-Options", "DENY")
		// Prevent MIME sniffing
		h.Set("X-Content-Type-Options", "nosniff")
		// XSS protection (legacy browsers)
		h.Set("X-XSS-Protection", "1; mode=block")
		// Only send referrer on same origin
		h.Set("Referrer-Policy", "same-origin")
		// Content Security Policy — adjust allowed sources as needed
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' https://cdn.tailwindcss.com https://cdn.jsdelivr.net 'unsafe-inline'; "+
				"style-src 'self' https://cdn.tailwindcss.com https://fonts.googleapis.com 'unsafe-inline'; "+
				"font-src 'self' https://fonts.gstatic.com; "+
				"connect-src 'self' ws: wss: https://cdn.jsdelivr.net; "+
				"img-src 'self' data:; "+
				"frame-ancestors 'none'")
		// HTTPS only (enable in production)
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		// Prevent caching of static assets so CSS/JS changes take effect immediately
		if strings.HasSuffix(r.URL.Path, ".css") || strings.HasSuffix(r.URL.Path, ".js") || strings.HasSuffix(r.URL.Path, ".html") || r.URL.Path == "/" {
			h.Set("Cache-Control", "no-cache, no-store, must-revalidate")
		}
		next.ServeHTTP(w, r)
	})
}
