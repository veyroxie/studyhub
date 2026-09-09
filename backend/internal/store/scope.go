package store

import (
	"errors"

	"studyhub/internal/core"
)

// ErrNoWriteTenant means the caller has no single tenant to write into.
var ErrNoWriteTenant = errors.New("no tenant selected for this write")

// WriteTenantID resolves the tenant a NEW row belongs to, and refuses 0.
//
// TenantID returns 0 for a superadmin, which ScopeTenant reads as "no filter".
// That is right for a read and wrong for a write, because the 0 gets stored:
// on `users` it mints an account whose JWT then carries TenantID 0, so every
// scoped query in the app stops filtering for that person; everywhere else it
// writes a row the owning tenant can never see, which is how a superadmin's
// class cancellation granted make-up credits to nobody.
//
// A superadmin is a cross-tenant READER. To create rows they act inside a
// tenant, so callers surface this as a 400 rather than inventing a default --
// guessing tenant 1 would put the row in someone's data.
func WriteTenantID(c *core.Claims) (int, error) {
	tid := TenantID(c)
	if tid == 0 {
		return 0, ErrNoWriteTenant
	}
	return tid, nil
}

// TenantID resolves the tenant id used for scoping queries. Superadmin
// (TenantID==0) maps to 0 (cross-tenant); a nil caller defaults to tenant 1.
func TenantID(c *core.Claims) int {
	if c == nil {
		return 1
	}
	if c.TenantID == 0 {
		return 0
	} // 0 = superadmin, cross-tenant
	return c.TenantID
}

// ScopeTenant returns a SQL fragment that scopes a query to the caller's
// tenant, plus the args to thread through. For tenant-scoped users it
// appends "AND tenant_id = ?" (or "AND <alias>.tenant_id = ?"); for
// superadmin (tid=0) it returns the empty string and no args, granting
// cross-tenant visibility.
//
// Callers concatenate the clause directly into their SQL — preserving the
// `?` placeholder convention used throughout the project — and append
// twArgs to their existing args slice.
//
// Why this matters: the previous "(tenant_id=? OR ?=0)" pattern prevented
// PostgreSQL from using the composite (tenant_id, deleted_at) indexes
// because the planner couldn't pick a generic plan. The helper keeps the
// superadmin escape hatch while letting the common case use indexes.
func ScopeTenant(c *core.Claims, alias string) (string, []any) {
	tid := TenantID(c)
	if tid == 0 {
		return "", nil
	}
	if alias == "" {
		return " AND tenant_id = ?", []any{tid}
	}
	return " AND " + alias + ".tenant_id = ?", []any{tid}
}
