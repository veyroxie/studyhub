package store

import (
	"net/http"
)

// RLSScope is a passthrough that records the request's tenant in context.
//
// Tenant isolation is enforced at the query layer by ScopeTenant() (applied to
// every tenant-scoped query and covered by TestRLSTenantIsolation). The DB-level
// RLS policies (migrations 0004/0015) are a SECOND line of defence that is
// currently DORMANT for two independent reasons, both documented in
// notes/rls-activation.md:
//
//  1. The app connects as a Postgres SUPERUSER, which bypasses RLS entirely
//     (even FORCE ROW LEVEL SECURITY). Activation requires connecting as the
//     non-superuser studyhub_app role.
//  2. RLS reads the app.tenant_id GUC, which must be set on the SAME connection
//     that serves the request's queries. database/sql hands each query an
//     arbitrary pooled connection, so the GUC must be bound via a per-request
//     pinned *sql.Conn — a deliberate refactor, not a middleware one-liner.
//
// The previous implementation ran `SET app.tenant_id` via db.Exec on a random
// pooled connection. That gave zero protection (wrong connection) AND left the
// GUC stuck on that pooled connection (session-level SET, never reset) — a
// latent cross-tenant hazard the moment RLS is activated. This passthrough
// removes that hazard; it intentionally does NOT touch the pool.
//
// Mounted AFTER jwtMiddleware so Claims are available.
func RLSScope(db *DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}
}
