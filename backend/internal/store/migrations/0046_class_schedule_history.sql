-- 0046_class_schedule_history.sql
--
-- Classes are recurring templates (see 0040): editing day/time rewrites every
-- week, past and future. This table records the schedule that applied BEFORE
-- a dated change, so "the Friday 3pm class moves to Thursday 4pm from
-- September" keeps August rendering, billing and exporting as Friday 3pm
-- while the class keeps its id (attendance, enrolments, rates all stay put).
--
-- Each row stores the OLD day/time/end_time; changed_on is the first date the
-- NEW schedule applies. Resolution for a date d: the row with the smallest
-- changed_on > d wins; no such row means the current classes row applies.
-- Two edits with the same effective date keep the FIRST row -- the
-- intermediate schedule never applied to any real date -- hence the unique
-- index and ON CONFLICT DO NOTHING at the write site. That same property
-- makes a mistaken change self-undoing: editing back with the same effective
-- date restores the class row and the stale snapshot row becomes a no-op.

CREATE TABLE IF NOT EXISTS class_schedule_history (
    id         TEXT PRIMARY KEY,
    tenant_id  INTEGER NOT NULL DEFAULT 1,
    class_id   TEXT NOT NULL,
    day        TEXT NOT NULL,
    time       TEXT NOT NULL,
    end_time   TEXT NOT NULL,
    changed_on TEXT NOT NULL,
    created_by TEXT NOT NULL DEFAULT '',
    created_on TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_class_schedule_history_once
    ON class_schedule_history (tenant_id, class_id, changed_on);
