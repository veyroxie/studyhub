-- 0011_perf_indexes.sql
--
-- Composite indexes covering hot snapshot queries. Every one matches a
-- WHERE/ORDER BY pair the planner currently satisfies via merge of
-- single-column indexes or a seq-scan. Safe to re-run.

CREATE INDEX IF NOT EXISTS idx_feedback_class_date_desc
    ON feedback(class_id, date DESC) WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_attendance_tenant_date_desc
    ON attendance(tenant_id, date DESC);

CREATE INDEX IF NOT EXISTS idx_invoices_tenant_status_created
    ON invoices(tenant_id, status, created_on DESC) WHERE deleted_at IS NULL;

-- Self-study overflow dedup query filters by student_id + month prefix.
CREATE INDEX IF NOT EXISTS idx_invoices_overflow_lookup
    ON invoices(student_id, type, created_on) WHERE deleted_at IS NULL;

-- Hot path for refresh-token rotation (token_hash is PK, no extra index
-- needed) — but the cleanup job filters by expires_at OR revoked_at,
-- a partial index keeps the daily purge cheap.
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_purge
    ON refresh_tokens(expires_at)
    WHERE used_at IS NULL AND revoked_at IS NULL;
