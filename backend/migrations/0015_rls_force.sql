-- 0015_rls_force.sql
--
-- Activate RLS enforcement for the table OWNER as well.
--
-- 0004 ran `ENABLE ROW LEVEL SECURITY`, but PostgreSQL exempts a table's
-- OWNER from RLS unless FORCE is also set. The application connects as the
-- role that owns these tables, so the 0004 tenant_isolation policies were
-- effectively INERT for the app — isolation rested entirely on the app-level
-- scopeTenant() WHERE clauses. FORCE makes the policies apply to the owner
-- too, so a forgotten WHERE clause is caught at the DB layer.
--
-- Safe for current code paths: the 0004 policy's `app.tenant_id IS NULL`
-- branch is permissive, so startup migrations, seeds, and background jobs
-- (cron/email queue) — which never SET app.tenant_id — keep full access.
-- Authenticated HTTP requests set the GUC (rls.go) and are constrained.
--
-- Multi-tenant caveat (documented, not yet needed — StudyHub is single-tenant):
-- rls.go uses session-level `SET` (not `SET LOCAL` in a txn) on a pooled
-- connection, so a background job could inherit a prior request's tenant GUC.
-- At one tenant that's harmless (the only id is 1). BEFORE onboarding a second
-- tenant, switch background-job/cron queries to RESET app.tenant_id (or run
-- them with app.tenant_id='0') so cross-tenant sweeps aren't constrained.

DO $$
DECLARE
    t TEXT;
    tables TEXT[] := ARRAY[
        'students','families','classes','staff','invoices','attendance',
        'payroll','feedback','feedback_replies','announcements','subjects',
        'workshops','self_study_sessions','performance_reviews',
        'cancelled_classes','registrations','holidays','replacement_credits',
        'referral_rewards','progress_reports','audit_logs'
    ];
BEGIN
    FOREACH t IN ARRAY tables LOOP
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    END LOOP;
END $$;
