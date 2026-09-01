-- 0047_class_schedule_versions.sql
--
-- Replaces 0046's representation. class_schedule_history stored the slot that
-- applied BEFORE a date, so a row was only interpretable against the current
-- classes row. That produced four defects in one guard: inserting an
-- earlier-dated change was undefined (hence the guard), the guard's advice
-- named an undo that could not exist, a self-undone row was indistinguishable
-- from a live one and blocked earlier edits forever, and the check-then-insert
-- had no transaction.
--
-- Here a row states the slot that applies FROM a date. Resolution for date d:
-- the version with the greatest effective_from <= d. Out-of-order inserts are
-- ordinary, rows are self-describing so edit and delete are natural, and the
-- resolver returns time and end_time as well as day -- which is what duration
-- aware pricing (NEW-31) and historical iCal times need.
--
-- INVARIANT: the version with the greatest effective_from for a class always
-- mirrors that class's current day/time/end_time. The classes row stays the
-- source of truth for "now" so the ~dozen readers of classes.day keep working;
-- this table answers "what was it on date d".
--
-- 0046's table is left in place, unused, and dropped by a later migration once
-- this has run in production for a while. Migrations are append-only.

CREATE TABLE IF NOT EXISTS class_schedule_versions (
    id             TEXT PRIMARY KEY,
    tenant_id      INTEGER NOT NULL DEFAULT 1,
    class_id       TEXT NOT NULL,
    effective_from TEXT NOT NULL,
    day            TEXT NOT NULL,
    time           TEXT NOT NULL,
    end_time       TEXT NOT NULL,
    created_by     TEXT NOT NULL DEFAULT '',
    created_on     TEXT NOT NULL DEFAULT ''
);

-- One version per class per effective date; a same-date re-edit updates in
-- place rather than stacking (the intermediate never applied to a real date).
CREATE UNIQUE INDEX IF NOT EXISTS idx_class_schedule_versions_once
    ON class_schedule_versions (tenant_id, class_id, effective_from);
CREATE INDEX IF NOT EXISTS idx_class_schedule_versions_lookup
    ON class_schedule_versions (tenant_id, class_id, effective_from DESC);

-- Backfill, part 1: each 0046 snapshot describes the slot in force over
-- [previous changed_on, this changed_on). LAG gives the previous one; the
-- oldest snapshot reaches back to the sentinel epoch. TEXT dates compare
-- lexically, so '0001-01-01' is below every real date.
INSERT INTO class_schedule_versions (id, tenant_id, class_id, effective_from, day, time, end_time, created_by, created_on)
SELECT 'SV_' || h.id,
       h.tenant_id,
       h.class_id,
       COALESCE(LAG(h.changed_on) OVER (PARTITION BY h.tenant_id, h.class_id ORDER BY h.changed_on), '0001-01-01'),
       h.day,
       h.time,
       h.end_time,
       'backfill-0047',
       '2026-09-01'
FROM class_schedule_history h
ON CONFLICT (tenant_id, class_id, effective_from) DO NOTHING;

-- Backfill, part 2: the current classes row is the newest version, effective
-- from the latest recorded change (or the epoch for a class that never moved).
INSERT INTO class_schedule_versions (id, tenant_id, class_id, effective_from, day, time, end_time, created_by, created_on)
SELECT 'SVC_' || c.id,
       c.tenant_id,
       c.id,
       COALESCE((SELECT MAX(h.changed_on) FROM class_schedule_history h
                 WHERE h.class_id = c.id AND h.tenant_id = c.tenant_id), '0001-01-01'),
       COALESCE(c.day, ''),
       COALESCE(c.time, ''),
       COALESCE(c.end_time, ''),
       'backfill-0047',
       '2026-09-01'
FROM classes c
WHERE c.deleted_at IS NULL
ON CONFLICT (tenant_id, class_id, effective_from) DO NOTHING;
