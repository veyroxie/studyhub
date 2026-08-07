-- 0030_audit_index_and_fees_numeric.sql
--
-- audit_logs is queried as (tenant_id, ORDER BY created_at DESC) by the audit
-- viewer but had no composite index, so it degraded to a sequential scan plus
-- a sort as the table grew. Every other hot table already has this shape.
--
-- (An earlier draft also re-ran the registrations.school_fees NUMERIC
-- conversion here; dropped — 0025 already converted it, and re-issuing the
-- ALTER takes an ACCESS EXCLUSIVE lock on registrations for a no-op.)

CREATE INDEX IF NOT EXISTS idx_audit_logs_tenant_created
    ON audit_logs(tenant_id, created_at DESC);
