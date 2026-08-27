-- 0043_enrollments_table.sql
--
-- B6: enrolment becomes a real join table with a start and end date. The
-- students.enrolled_classes JSON stays AUTHORITATIVE for reads during the
-- dual-write phase; this table shadows every mutation from now on and is
-- what session billing (plan 8.7) needs to prorate mid-month joiners and
-- leavers -- the JSON list cannot say WHEN a student joined a class.
--
-- ended_on NULL = currently enrolled. Unenrolling ENDS the row (history is
-- the point); rows are never deleted.

CREATE TABLE IF NOT EXISTS enrollments (
    id          TEXT PRIMARY KEY,
    tenant_id   INTEGER NOT NULL DEFAULT 1,
    student_id  TEXT NOT NULL,
    class_id    TEXT NOT NULL,
    started_on  TEXT NOT NULL,
    ended_on    TEXT,
    created_by  TEXT NOT NULL DEFAULT '',
    created_on  TEXT NOT NULL DEFAULT ''
);

-- One live enrolment per student per class; re-enrolling after an end date
-- inserts a fresh row, preserving both stints.
CREATE UNIQUE INDEX IF NOT EXISTS idx_enrollments_live
    ON enrollments (tenant_id, student_id, class_id) WHERE ended_on IS NULL;
CREATE INDEX IF NOT EXISTS idx_enrollments_class
    ON enrollments (tenant_id, class_id) WHERE ended_on IS NULL;

-- Backfill from the JSON column. started_on is approximated with the
-- student's registration date (the JSON records no join date -- the exact
-- reason this table exists). Deterministic ids make a re-run a no-op.
INSERT INTO enrollments (id, tenant_id, student_id, class_id, started_on, created_by, created_on)
SELECT 'ENR_' || s.id || '_' || cls.class_id,
       s.tenant_id,
       s.id,
       cls.class_id,
       COALESCE(NULLIF(s.registered_on, ''), '2026-08-28'),
       'backfill-0043',
       '2026-08-28'
FROM students s,
     LATERAL json_array_elements_text(s.enrolled_classes::json) AS cls(class_id)
WHERE s.deleted_at IS NULL
  AND s.enrolled_classes IS NOT NULL
  AND s.enrolled_classes ~ '^\[.*\]$'
ON CONFLICT (tenant_id, student_id, class_id) WHERE ended_on IS NULL DO NOTHING;
