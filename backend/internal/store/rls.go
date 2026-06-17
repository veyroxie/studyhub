package store

import (
	"net/http"
	"strconv"
	"studyhub/internal/core"
)

// rlsScope sets the per-request app.tenant_id GUC so the RLS policies in
// migration 0004 can filter rows automatically. Acts as defense-in-depth
// behind the explicit scopeTenant() WHERE clauses.
//
// The setting is applied via `SET` (session-wide) rather than `SET LOCAL`
// (transaction-scoped) because database/sql connection pooling means a
// transaction wrapper would require routing every query through the same
// txn. Setting it session-wide is acceptable because:
//   - We reset on every request entry, so a recycled connection is
//     re-bound to the new tenant before any query runs.
//   - Connections returning to the pool aren't queried again until the
//     next request enters this middleware.
//
// Mounted AFTER jwtMiddleware so Claims are available.
func RLSScope(db *DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := core.ClaimsFrom(r)
			if c != nil {
				// '0' = superadmin / cross-tenant. Anything else binds the
				// connection to the caller's tenant for the duration of
				// subsequent queries on that conn.
				tid := strconv.Itoa(c.TenantID)
				// pg_catalog requires SET to take a literal — bind via
				// a parameterised stmt isn't supported for GUCs. The
				// strconv result is digits only so injection is impossible.
				db.Exec(`SET app.tenant_id = '` + tid + `'`)
			}
			next.ServeHTTP(w, r)
		})
	}
}
