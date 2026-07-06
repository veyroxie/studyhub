package store

// Tenant-resolution helpers for audit logging on paths that have no Claims in
// scope — webhooks (system actor) and pre-auth flows (email verify, password
// reset, self-registration). They resolve the tenant of the affected row so
// the audit_logs row is stamped with the correct tenant instead of defaulting
// to tenant 1. All return 1 (the default tenant) when the row is missing, so a
// lookup failure degrades to the historical behaviour rather than a bad stamp.

// TenantOfUser returns the tenant_id of a user row, or 1 if not found.
func TenantOfUser(db *DB, userID int64) int {
	tid := 1
	db.QueryRow(`SELECT tenant_id FROM users WHERE id=?`, userID).Scan(&tid)
	return tid
}

// TenantOfInvoice returns the tenant_id of an invoice row, or 1 if not found.
func TenantOfInvoice(db *DB, invoiceID string) int {
	tid := 1
	db.QueryRow(`SELECT tenant_id FROM invoices WHERE id=?`, invoiceID).Scan(&tid)
	return tid
}

// TenantOfRegistration returns the tenant_id of a registration row, or 1.
func TenantOfRegistration(db *DB, regID string) int {
	tid := 1
	db.QueryRow(`SELECT tenant_id FROM registrations WHERE id=?`, regID).Scan(&tid)
	return tid
}
