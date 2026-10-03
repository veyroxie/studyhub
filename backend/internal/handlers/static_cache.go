package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"studyhub/internal/core"
)

// StaticCacheHandler serves the frontend so a deploy always reaches the browser:
//   - index.html is never cached and carries the build version (shellVersionToken);
//   - every asset it links has ?v=<version>, so a versioned URL is a new URL per
//     deploy and can be cached for a year without going stale;
//   - an unversioned asset (sw.js, a lazy import) is revalidated on every use.
//
// Before this, JS was cached for 5 minutes and the shell mixed old and new
// modules after a deploy until someone pressed Ctrl+Shift+R.
func StaticCacheHandler(root string) http.Handler {
	fs := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" || path == "/index.html" {
			serveVersionedShell(w, r, filepath.Join(root, "index.html"))
			return
		}
		if !shouldCacheStatic(path) {
			fs.ServeHTTP(w, r)
			return
		}
		full := filepath.Join(root, filepath.Clean(path))
		etag, ok := staticETag(full)
		if ok {
			w.Header().Set("Cache-Control", assetCacheControl(r))
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

// shellVersionToken in index.html is replaced with core.BuildVersion at serve time.
const shellVersionToken = "__APP_VERSION__"

func assetCacheControl(r *http.Request) string {
	if r.URL.Query().Get("v") != "" {
		return "public, max-age=31536000, immutable"
	}
	return "no-cache"
}

var (
	shellMu    sync.Mutex
	shellCache struct {
		modTime time.Time
		body    []byte
	}
)

func serveVersionedShell(w http.ResponseWriter, r *http.Request, file string) {
	body, err := versionedShell(file)
	if err != nil {
		core.LogFromReq(r).Error("index.html unreadable", "err", err)
		http.Error(w, "the app could not be loaded, please try again", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write(body)
}

func versionedShell(file string) ([]byte, error) {
	info, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	shellMu.Lock()
	defer shellMu.Unlock()
	if shellCache.body != nil && shellCache.modTime.Equal(info.ModTime()) {
		return shellCache.body, nil
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	shellCache.modTime, shellCache.body = info.ModTime(), bytes.ReplaceAll(raw, []byte(shellVersionToken), []byte(core.BuildVersion))
	return shellCache.body, nil
}
