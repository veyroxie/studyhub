package main

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"net"
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

// trustedProxyNets matches the loopback + private ranges that our reverse
// proxy (Caddy) is allowed to occupy in production and development. Any
// other peer is treated as untrusted and X-Real-IP / X-Forwarded-For are
// ignored — otherwise an attacker reaching the app directly (or via a
// misconfigured CDN) could spoof their source IP and bypass per-IP rate
// limits.
var trustedProxyNets = func() []*net.IPNet {
	cidrs := []string{
		"127.0.0.0/8",
		"::1/128",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"fc00::/7",
	}
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}()

func isTrustedProxy(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range trustedProxyNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func realIP(r *http.Request) string {
	if isTrustedProxy(r.RemoteAddr) {
		if ip := r.Header.Get("X-Real-IP"); ip != "" {
			return strings.TrimSpace(ip)
		}
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			// X-Forwarded-For is a comma-separated chain — the
			// left-most entry is the original client.
			if i := strings.IndexByte(fwd, ','); i >= 0 {
				return strings.TrimSpace(fwd[:i])
			}
			return strings.TrimSpace(fwd)
		}
	}
	return r.RemoteAddr
}

// rateLimitMiddleware wraps a handler with per-IP rate limiting
func rateLimitLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loginRateLimiter.allow(realIP(r)) {
			respondError(w, "too many login attempts, please wait a minute", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ── General API Rate Limiter ──────────────────────────────────────────────────
// 60 requests per minute per IP for writes, 120 for reads

var apiRateLimiter = &rateLimiter{buckets: make(map[string]*ipBucket)}

func rateLimitAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip rate limiting for OPTIONS
		if r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}
		limit := 120 // reads
		if r.Method != "GET" {
			limit = 60 // writes
		}
		ip := realIP(r)
		apiRateLimiter.mu.Lock()
		if len(apiRateLimiter.buckets) > 10000 {
			now := time.Now()
			for k, b := range apiRateLimiter.buckets {
				if now.Sub(b.windowStart) > 2*time.Minute {
					delete(apiRateLimiter.buckets, k)
				}
			}
		}
		b, ok := apiRateLimiter.buckets[ip]
		if !ok || time.Since(b.windowStart) > time.Minute {
			apiRateLimiter.buckets[ip] = &ipBucket{count: 1, windowStart: time.Now()}
			apiRateLimiter.mu.Unlock()
			next.ServeHTTP(w, r)
			return
		}
		b.count++
		allowed := b.count <= limit
		apiRateLimiter.mu.Unlock()
		if !allowed {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limit exceeded, please wait"}`))
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
				"script-src 'self' https://cdn.jsdelivr.net 'unsafe-inline'; "+
				"style-src 'self' https://fonts.googleapis.com 'unsafe-inline'; "+
				"font-src 'self' https://fonts.gstatic.com; "+
				"connect-src 'self' ws: wss: https://cdn.jsdelivr.net; "+
				"img-src 'self' data:; "+
				"frame-ancestors 'none'")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		// HTTPS only (enable in production)
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		// HTML and the SPA root: revalidate every load so the latest shell is
		// always served. CSS/JS: short browser-cache window (5 min) with
		// must-revalidate — repeat visits hit disk cache (zero bytes over the
		// wire, near-instant render) but a deploy propagates within minutes.
		path := r.URL.Path
		if strings.HasSuffix(path, ".html") || path == "/" {
			h.Set("Cache-Control", "no-cache, no-store, must-revalidate")
		} else if strings.HasSuffix(path, ".css") || strings.HasSuffix(path, ".js") {
			h.Set("Cache-Control", "public, max-age=300, must-revalidate")
		}
		next.ServeHTTP(w, r)
	})
}

// ── Request ID ───────────────────────────────────────────────────────────────
// Adds a unique X-Request-Id header to every response for log correlation.

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			b := make([]byte, 8)
			crand.Read(b)
			id = hex.EncodeToString(b)
		}
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), reqIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type ctxKey string

const reqIDKey ctxKey = "requestId"

