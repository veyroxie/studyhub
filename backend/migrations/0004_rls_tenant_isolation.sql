-- 0004_rls_tenant_isolation.sql
--
-- Row-level security as defense-in-depth. The application still passes
-- tenant_id explicitly in WHERE clauses (via scopeTenant()); these
-- policies are a backstop so any forgotten filter is caught at the DB
-- layer rather than silently leaking cross-tenant data.
--
-- Enforcement model:
--   - Policies use `current_setting('app.tenant_id', true)` which returns
--     NULL when the setting is missing. The NULL branch is permissive —
--     so existing code paths that don't set the variable keep working.
--   - The app's jwt middleware calls `SET LOCAL app.tenant_id = '<id>'`
--     at the start of every authenticated request (see rls.go), inside
--     a per-request transaction. From that point on, queries are
--     automatically constrained to the caller's tenant — even if the
--     handler forgot to add the WHERE clause.
--   - Superadmin (tenantID=0) sets app.tenant_id to '0' which the policy
--     treats as full access via the explicit OR branch.
--
-- Safe to apply incrementally: enabling RLS without policies would block
-- all access, so we create the policy FIRST and only then enable RLS.

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
        -- Drop any pre-existing policy with the same name so this migration
        -- is idempotent across re-applies during dev.
        EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON %I', t);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I '
            'USING ('
            '    current_setting(''app.tenant_id'', true) IS NULL '
            '    OR current_setting(''app.tenant_id'', true) = ''0'' '
            '    OR tenant_id::text = current_setting(''app.tenant_id'', true)'
            ')',
            t
        );
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    END LOOP;
END $$;
