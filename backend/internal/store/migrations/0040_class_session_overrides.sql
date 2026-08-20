-- 0040_class_session_overrides.sql
--
-- A single dated session that differs from its class.
--
-- Classes are recurring templates: they carry a day and a time but no date, so
-- editing one to cover "Ms Tan took Monday for Ms Lee this week" rewrites every
-- Monday, past and future. The centre has been living with that, and the change
-- silently reaches sessions that already happened, which also misreports who
-- taught them for payroll.
--
-- This is the same shape as cancelled_classes, which already models a per-date
-- exception to a recurring class. A row here means "on this date, this class had
-- these teachers instead"; no row means the class template applies, so existing
-- sessions are untouched and nothing needs backfilling.
--
-- teacher_ids is a JSON array to match classes.teacher_ids, so the two are read
-- and written the same way rather than needing a second representation.
CREATE TABLE IF NOT EXISTS class_session_overrides (
    id          TEXT PRIMARY KEY,
    tenant_id   INTEGER NOT NULL DEFAULT 1,
    class_id    TEXT NOT NULL,
    date        TEXT NOT NULL,
    teacher_ids TEXT NOT NULL DEFAULT '[]',
    note        TEXT NOT NULL DEFAULT '',
    created_by  TEXT NOT NULL DEFAULT '',
    created_on  TEXT NOT NULL DEFAULT '',
    deleted_at  TIMESTAMPTZ
);

-- One override per class per date: a second swap for the same session replaces
-- the first rather than stacking, so "who taught this" always has one answer.
CREATE UNIQUE INDEX IF NOT EXISTS idx_session_override_unique
    ON class_session_overrides (tenant_id, class_id, date) WHERE deleted_at IS NULL;

-- The calendar loads a date range, so this is the access path.
CREATE INDEX IF NOT EXISTS idx_session_override_tenant_date
    ON class_session_overrides (tenant_id, date) WHERE deleted_at IS NULL;
