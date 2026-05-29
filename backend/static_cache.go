package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// staticCacheHandler wraps http.FileServer with:
//   - Cache-Control: public, max-age=300, must-revalidate
//   - Weak ETag based on (path, modtime, size). On If-None-Match → 304.
//
// Cache-Control is the load-time win: returning visitors don't re-download
// the JS/CSS bundle. The ETag is the deploy-correctness companion: a 5-min
// max-age means a fresh deploy could be invisible to a returning user for
// up to 5 minutes; ETag lets the browser revalidate cheaply when the file
// has actually changed.
//
// 5 minutes is short on purpose. Longer max-age (1 hour, 1 day) would need
// content-hashed filenames to avoid stale-bundle pain after a release.
func staticCacheHandler(root string) http.Handler {
	fs := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip cache-control for the bare path "/" (index.html) — that's the
		// shell document; we want every navigation to fetch fresh in case
		// the JS module list changed. The defer/lazy-load pattern in the
		// shell means index.html is small (~25KB) anyway.
		path := r.URL.Path
		if !shouldCacheStatic(path) {
			fs.ServeHTTP(w, r)
			return
		}
		full := filepath.Join(root, filepath.Clean(path))
		etag, ok := staticETag(full)
		if ok {
			w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
			w.Header().Set("ETag", etag)
			if match := r.Header.Get("If-None-Match"); match == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		fs.ServeHTTP(w, r)
	})
}

// shouldCacheStatic picks asset extensions that benefit from a browser
// cache. HTML shells deliberately fall through to the bare fileserver so a
// deploy is visible on the next navigation.
func shouldCacheStatic(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".css", ".ico", ".png", ".jpg", ".jpeg", ".svg", ".woff", ".woff2", ".ttf", ".webp":
		return true
	}
	return false
}

// staticETag returns a weak ETag derived from path + modtime + size.
// Cached so the typical "10 modules per page" load doesn't re-hash on
// every request — the modtime in the cache key invalidates automatically
// on file change.
type staticETagEntry struct {
	etag    string
	modTime time.Time
	size    int64
}

var (
	staticETagMu    sync.RWMutex
	staticETagCache = map[string]staticETagEntry{}
)

func staticETag(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	staticETagMu.RLock()
	e, ok := staticETagCache[path]
	staticETagMu.RUnlock()
	if ok && e.modTime.Equal(info.ModTime()) && e.size == info.Size() {
		return e.etag, true
	}
	h := sha256.New()
	h.Write([]byte(path))
	h.Write([]byte(info.ModTime().Format(time.RFC3339Nano)))
	tag := `W/"` + hex.EncodeToString(h.Sum(nil)[:12]) + `"`
	staticETagMu.Lock()
	staticETagCache[path] = staticETagEntry{etag: tag, modTime: info.ModTime(), size: info.Size()}
	staticETagMu.Unlock()
	return tag, true
}
