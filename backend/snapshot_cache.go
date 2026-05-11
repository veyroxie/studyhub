package main

import (
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
)

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
	if len(snapshotCache) > 1024 {
		now := time.Now()
		for k, v := range snapshotCache {
			if now.After(v.expires) {
				delete(snapshotCache, k)
			}
		}
	}
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
func writeCachedSnapshot(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

// marshalAndCacheSnapshot serializes the snapshot, stores it in the cache
// under the given key, and writes it to the response.
func marshalAndCacheSnapshot(w http.ResponseWriter, key string, snap any) {
	body, err := json.Marshal(snap)
	if err != nil {
		respondError(w, "snapshot serialization failed", http.StatusInternalServerError)
		return
	}
	snapshotCachePut(key, body)
	writeCachedSnapshot(w, body)
}
