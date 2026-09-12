package server

import (
	"os"
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
