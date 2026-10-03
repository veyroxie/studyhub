package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"studyhub/internal/core"
	"studyhub/internal/store"
)

func testDSN() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://stratum:stratum_dev@localhost:5432/studyhub_test?sslmode=disable"
}

// Build the REAL production router.
//
// Nothing else did. The handler suites each construct their own chi mux with
// just the routes they exercise, so server.go's own wiring was never executed
// by a test: `go build` and `go vet` both pass on a mux that panics the moment
// it is assembled. A route registered above a later r.Use in the same group
// does exactly that -- "chi: all middlewares must be defined before routes on
// a mux" -- and it took the site down on deploy rather than in CI.
//
// This test is cheap and catches the whole class: ordering, duplicate paths,
// and any nil handler passed to a route.
func TestProductionRouterAssembles(t *testing.T) {
	core.InitLogger()
	db := store.InitDB(testDSN())
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("server.Build panicked, so the API would not boot: %v", r)
		}
	}()
	if h := Build(db); h == nil {
		t.Fatal("server.Build returned no handler")
	}
}

// Technical endpoints on the real router: anonymous callers learn nothing.
func TestTechnicalEndpointsRefuseAnonymousCallers(t *testing.T) {
	core.InitLogger()
	db := store.InitDB(testDSN())
	defer db.Close()
	h := Build(db)
	for _, path := range []string{"/metrics", "/api/dev/health", "/api/dev/audit-logs", "/api/dev/failures", "/api/absence-reports/sessions?studentId=STU001"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("GET %s anonymously: %d, want 401", path, w.Code)
		}
	}
	// Writes without a session are refused before any handler runs (CSRF or auth, whichever is first).
	for _, path := range []string{"/api/absence-reports", "/api/absence-reports/ABS_X/decision"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(`{}`)))
		if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
			t.Errorf("POST %s anonymously: %d, want 401 or 403", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/health", nil))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "db_pool") {
		t.Errorf("public health: %d %s", w.Code, w.Body.String())
	}
}

// A deploy must reach an open browser: the shell names its version, versioned assets
// are cached for good, anything unversioned is revalidated, and every reply says
// which version is running.
func TestADeployAlwaysReachesTheBrowser(t *testing.T) {
	core.InitLogger()
	t.Chdir("../..") // the binary runs from backend/, next to ../frontend
	was := core.BuildVersion
	core.BuildVersion = "test-build-42"
	defer func() { core.BuildVersion = was }()
	db := store.InitDB(testDSN())
	defer db.Close()
	h := Build(db)
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	shell := get("/")
	if body := shell.Body.String(); !strings.Contains(body, "js/main.js?v=test-build-42") || strings.Contains(body, "__APP_VERSION__") {
		t.Errorf("shell is not stamped with the build version")
	}
	if cc := shell.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("shell Cache-Control %q, want no-store", cc)
	}
	if cc := get("/js/main.js?v=test-build-42").Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("versioned asset Cache-Control %q, want immutable", cc)
	}
	if cc := get("/sw.js").Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("unversioned asset Cache-Control %q, want no-cache", cc)
	}
	if v := get("/api/health").Header().Get("X-App-Version"); v != "test-build-42" {
		t.Errorf("X-App-Version %q", v)
	}
}
