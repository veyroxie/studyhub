package handlers

import (
	"testing"

	"studyhub/internal/core"
	"studyhub/internal/store"
)

// visibleClassIDs answers "all classes" with a separate flag rather than a nil
// map, because the set-builders return empty on failure. If emptiness and
// unrestricted shared one value, a DB error would hand a parent every session
// record in the centre — the disclosure the scoping exists to prevent.
func TestParentClassScopeFailsClosed(t *testing.T) {
	_, cleanup := setupTestApp(t)
	defer cleanup()

	dead := store.InitDB(testDSN())
	dead.Close()

	ids := store.ParentClassIDs(dead, &core.Claims{TenantID: 1, Role: "parent", Email: "parent@test.com"})
	if ids == nil {
		t.Fatal("ParentClassIDs returned nil on a failed query — a caller that reads nil as \"unrestricted\" would show every class")
	}
	if len(ids) != 0 {
		t.Errorf("failed lookup returned %d classes, want none", len(ids))
	}
}
