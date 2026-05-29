package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// Snapshot cache — short-TTL in-memory cache for /api/snapshot.
//
// /api/snapshot fans out to ~18 parallel DB queries on every dashboard load
// and refreshes on every navigation. With a 10-second TTL most navigations
// served from cache, eliminating the bulk of DB load. Admin writes propagate
// to the UI on the next cache miss (≤10s) — acceptable staleness.
//
// Key includes (tenant, role, email) because parents see post-filtered data
// scoped to their own children.

const snapshotCacheTTL = 10 * time.Second

type snapshotCacheEntry struct {
	body    []byte
	expires time.Time
}

var (
	snapshotCacheMu sync.RWMutex
	snapshotCache   = map[string]snapshotCacheEntry{}

	// snapshotInFlight tracks builds currently running per cache key. The
	// singleflight pattern: only one goroutine actually runs the ~18 fan-out
	// queries, all other concurrent requests for the same key wait on the
	// shared result channel. Without this, a cold cache + a thundering herd
	// (e.g., 10 admin tabs auto-refreshing at the same second) runs 10×
	// duplicate work against Postgres.
	snapshotFlightsMu sync.Mutex
	snapshotFlights   = map[string]*snapshotFlight{}
)

type snapshotFlight struct {
	done chan struct{}
	body []byte
	err  error
}

// snapshotSingleflight returns the existing in-flight build for this key, or
// registers a new one and returns it with isLeader=true so the caller
// performs the actual work. Followers wait on f.done.
func snapshotSingleflight(key string) (f *snapshotFlight, isLeader bool) {
	snapshotFlightsMu.Lock()
	defer snapshotFlightsMu.Unlock()
	if existing, ok := snapshotFlights[key]; ok {
		return existing, false
	}
	f = &snapshotFlight{done: make(chan struct{})}
	snapshotFlights[key] = f
	return f, true
}

// snapshotSingleflightDone is called by the leader after the build completes
// (success or error). It publishes the result and removes the entry so the
// next request rebuilds.
func snapshotSingleflightDone(key string, body []byte, err error) {
	snapshotFlightsMu.Lock()
	f, ok := snapshotFlights[key]
	if ok {
		delete(snapshotFlights, key)
	}
	snapshotFlightsMu.Unlock()
	if !ok {
		return
	}
	f.body = body
	f.err = err
	close(f.done)
}

func snapshotCacheKey(c *Claims) string {
	if c == nil {
		return "anon"
	}
	return strconv.Itoa(c.TenantID) + "|" + c.Role + "|" + c.Email
}

func snapshotCacheGet(key string) ([]byte, bool) {
	snapshotCacheMu.RLock()
	defer snapshotCacheMu.RUnlock()
	e, ok := snapshotCache[key]
	if !ok || time.Now().After(e.expires) {
		return nil, false
	}
	return e.body, true
}

func snapshotCachePut(key string, body []byte) {
	snapshotCacheMu.Lock()
	defer snapshotCacheMu.Unlock()
	// Lazy expiry: Get already filters by expires, and the map churn is
	// bounded by (tenants × roles × admins online) which is small enough
	// that the previous O(n) full-map scan on every Put past 1024 entries
	// was wasted work in the hot write path. If the map ever grows large
	// in pathological cases, this can be revisited with a time-wheel.
	snapshotCache[key] = snapshotCacheEntry{body: body, expires: time.Now().Add(snapshotCacheTTL)}
}

// snapshotCacheInvalidateTenant drops every cached snapshot for a tenant.
// Called from write paths that materially change tenant-scoped data so
// admins see their changes reflected immediately (rather than waiting up
// to snapshotCacheTTL for natural expiry).
func snapshotCacheInvalidateTenant(tenantID int) {
	prefix := strconv.Itoa(tenantID) + "|"
	snapshotCacheMu.Lock()
	defer snapshotCacheMu.Unlock()
	for k := range snapshotCache {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(snapshotCache, k)
		}
	}
}

// snapshotCacheInvalidateAll drops every cached snapshot regardless of
// tenant. Used by cron jobs that touch all tenants in one pass.
func snapshotCacheInvalidateAll() {
	snapshotCacheMu.Lock()
	defer snapshotCacheMu.Unlock()
	for k := range snapshotCache {
		delete(snapshotCache, k)
	}
}

// snapshotCacheInvalidator is HTTP middleware that drops the requester's
// tenant cache after any successful (2xx) non-GET request. This way every
// write handler — students, invoices, classes, attendance, etc. — gets
// cache invalidation for free without per-handler bookkeeping. The 10s
// TTL is the safety net; this middleware makes admin writes feel instant.
//
// Must be registered AFTER jwtMiddleware so claims are available.
func snapshotCacheInvalidator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodOptions || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		status := ww.Status()
		if status >= 200 && status < 300 {
			c := claimsFrom(r)
			if c == nil {
				return
			}
			if c.TenantID == 0 {
				snapshotCacheInvalidateAll()
				return
			}
			snapshotCacheInvalidateTenant(c.TenantID)
		}
	})
}

// writeCachedSnapshot writes an already-marshalled snapshot to the response.
// Adds a weak ETag from the body hash so clients polling on a 30s interval
// receive a 5-byte 304 instead of the full ~500KB body when nothing has
// changed.
func writeCachedSnapshot(w http.ResponseWriter, r *http.Request, body []byte) {
	etag := snapshotETag(body)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Write(body)
}

func snapshotETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `W/"` + hex.EncodeToString(sum[:16]) + `"`
}

// marshalAndCacheSnapshot was the all-in-one helper before the singleflight
// rewrite; handleSnapshot now does marshal + cache + ETag-aware write
// inline so the leader can publish the body to followers before the
// response is sent. Kept as a thin wrapper for legacy callers and tests.
func marshalAndCacheSnapshot(w http.ResponseWriter, r *http.Request, key string, snap any) {
	body, err := json.Marshal(snap)
	if err != nil {
		respondError(w, "snapshot serialization failed", http.StatusInternalServerError)
		return
	}
	snapshotCachePut(key, body)
	writeCachedSnapshot(w, r, body)
}
