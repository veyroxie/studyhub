package handlers

import (
	"context"
	"studyhub/internal/store"
	"testing"
)

// TestRLSTenantIsolation proves the 0004 tenant_isolation policy + 0015 FORCE
// actually constrain reads by tenant — i.e. an UNSCOPED query (no WHERE
// tenant_id) cannot see another tenant's rows once app.tenant_id is set.
//
// IMPORTANT operational note this test encodes: RLS is bypassed for SUPERUSER
// roles and for a table's owner-without-FORCE. The app currently connects as a
// superuser (the default POSTGRES_USER), so in production these policies are
// INERT and isolation rests entirely on scopeTenant(). To activate the DB-level
// backstop, the app must connect as a NON-superuser role. This test simulates
// that correct setup via SET ROLE to a dedicated non-superuser role.
func TestRLSTenantIsolation(t *testing.T) {
	db := store.InitDB(testDSN())
	ctx := context.Background()

	// A dedicated non-superuser role to prove enforcement (the prod app role
	// must likewise be non-superuser for RLS to take effect).
	db.Exec(`DO $$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='rls_probe') THEN CREATE ROLE rls_probe NOSUPERUSER; END IF; END $$`)
	db.Exec(`GRANT USAGE ON SCHEMA public TO rls_probe`)
	db.Exec(`GRANT SELECT ON students TO rls_probe`)

	db.Exec(`DELETE FROM students WHERE id IN ('RLS_T1','RLS_T2')`)
	if _, err := db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name) VALUES(?,?,?,?)`, "RLS_T1", 1, "Tenant1", "Kid"); err != nil {
		t.Fatalf("seed tenant1: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name) VALUES(?,?,?,?)`, "RLS_T2", 2, "Tenant2", "Kid"); err != nil {
		t.Fatalf("seed tenant2: %v", err)
	}
	defer db.Exec(`DELETE FROM students WHERE id IN ('RLS_T1','RLS_T2')`)

	// Read inside a transaction with SET LOCAL role+GUC, then ROLLBACK — so the
	// role/GUC never leak back to the pooled connection (which would taint
	// later tests). SET LOCAL is scoped to the transaction.
	countAs := func(t *testing.T, tenant string) int {
		t.Helper()
		conn, err := db.DB.Conn(ctx)
		if err != nil {
			t.Fatalf("conn: %v", err)
		}
		defer conn.Close()
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, `SET LOCAL ROLE rls_probe`); err != nil {
			t.Fatalf("set role: %v", err)
		}
		if _, err := tx.ExecContext(ctx, `SET LOCAL app.tenant_id = '`+tenant+`'`); err != nil {
			t.Fatalf("set guc: %v", err)
		}
		var n int
		// UNSCOPED on purpose — RLS must do the filtering, not a WHERE clause.
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM students WHERE id IN ('RLS_T1','RLS_T2')`).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}

	if got := countAs(t, "1"); got != 1 {
		t.Errorf("tenant 1 should see only its row, got %d (RLS not enforcing)", got)
	}
	if got := countAs(t, "2"); got != 1 {
		t.Errorf("tenant 2 should see only its row, got %d", got)
	}
	if got := countAs(t, "0"); got != 2 {
		t.Errorf("superadmin (0) should see both rows, got %d", got)
	}
	// NOTE: the "no tenant context → see all" (NULL-permissive) branch is NOT
	// asserted here. RESET on a custom GUC yields '' (not NULL), and pooled
	// connections retain a prior session's app.tenant_id — the exact reason a
	// background job must explicitly scope (or run as a non-pooled/owner role)
	// before relying on the permissive branch. Documented in 0015_rls_force.sql.
}
