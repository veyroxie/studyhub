package store

import (
	"net/http"
	"strconv"
	"studyhub/internal/core"
)

// rlsScope sets the app.tenant_id GUC that the RLS policies in migration 0004
// read.
//
// IMPORTANT — this is NOT a reliable per-request safety net. `db.Exec` borrows
// an arbitrary connection from the pool, runs the `SET` on it, and returns it.
// The request's subsequent queries are served by *other* pooled connections
// where the GUC is unset, so RLS does not filter them. RLS therefore only
// actually guards queries that run on the same connection that executed the
// SET (e.g. a superadmin tool that pins one conn). For normal request handling
// the ONLY thing enforcing tenant isolation is the explicit ScopeTenant() WHERE
// clauses — treat those as mandatory, not belt-and-braces.
//
// TODO(rls-binding): bind the GUC to the request by routing all of a request's
// queries through a single *sql.Conn (or a per-request tx). Needs a dedicated
// plan — do not attempt a piecemeal conn-routing change here.
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
