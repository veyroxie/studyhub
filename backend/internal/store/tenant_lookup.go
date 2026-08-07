package store

import (
	"database/sql"

	"studyhub/internal/core"
)

// Tenant-resolution helpers for audit logging on paths that have no Claims in
// scope — webhooks (system actor) and pre-auth flows (email verify, password
// reset, self-registration). They resolve the tenant of the affected row so
// the audit_logs row is stamped with the correct tenant instead of defaulting
// to tenant 1. A missing row degrades to tenant 1 (historical behaviour); a
// query FAILURE is logged loudly — silently stamping tenant 1 on a DB error
// mis-attributes payment webhooks to the wrong tenant's audit trail.

func tenantOf(db *DB, table, id string) int {
	tid := 1
	err := db.QueryRow(`SELECT tenant_id FROM `+table+` WHERE id=?`, id).Scan(&tid)
	if err != nil && err != sql.ErrNoRows {
		core.Logger.Error("tenant lookup failed — audit row will be stamped tenant 1", "table", table, "id", id, "err", err)
	}
	return tid
}

// TenantOfUser returns the tenant_id of a user row, or 1 if not found.
func TenantOfUser(db *DB, userID int64) int {
	tid := 1
	err := db.QueryRow(`SELECT tenant_id FROM users WHERE id=?`, userID).Scan(&tid)
	if err != nil && err != sql.ErrNoRows {
		core.Logger.Error("tenant lookup failed — audit row will be stamped tenant 1", "table", "users", "id", userID, "err", err)
	}
	return tid
}

// TenantOfInvoice returns the tenant_id of an invoice row, or 1 if not found.
func TenantOfInvoice(db *DB, invoiceID string) int {
	return tenantOf(db, "invoices", invoiceID)
}

// TenantOfRegistration returns the tenant_id of a registration row, or 1.
func TenantOfRegistration(db *DB, regID string) int {
	return tenantOf(db, "registrations", regID)
}
