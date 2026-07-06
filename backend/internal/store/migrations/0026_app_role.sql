-- 0026_app_role.sql
--
-- Stage (but do NOT activate) the non-superuser role that DB-level RLS needs.
--
-- The RLS policies in 0004/0015 are bypassed while the app connects as a
-- superuser. This creates the studyhub_app role a future activation can switch
-- to. It is deliberately NOLOGIN: connecting as it before the per-request GUC
-- binding exists would make FORCE RLS block every write. Activation steps are
-- in notes/rls-activation.md.
--
-- Wrapped in a DO block that tolerates environments where role creation is
-- restricted (e.g. some managed Postgres) — the app keeps working either way.

DO $$
BEGIN
	IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'studyhub_app') THEN
		CREATE ROLE studyhub_app NOSUPERUSER NOLOGIN;
	END IF;

	GRANT USAGE ON SCHEMA public TO studyhub_app;
	GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO studyhub_app;
	GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO studyhub_app;
	-- Future tables/sequences created by the migration owner become accessible
	-- automatically, so activation doesn't require re-granting per release.
	ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO studyhub_app;
	ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO studyhub_app;
EXCEPTION
	WHEN insufficient_privilege THEN
		RAISE NOTICE 'skipping studyhub_app role setup: insufficient privilege (managed DB?) — RLS activation will need manual role provisioning';
END $$;
