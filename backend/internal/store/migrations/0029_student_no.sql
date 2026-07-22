-- 0029_student_no.sql
--
-- Add a human-facing, editable "student number" separate from the internal
-- primary key (students.id, e.g. STU_...). The id is a stable surrogate key
-- referenced by invoices, attendance, credits, self-study, referrals and
-- progress reports, so it must never change. student_no lets admins assign
-- their own scheme (e.g. 2024-001) and edit it freely without touching any of
-- those relationships.
--
-- Partial unique index (per tenant, non-empty only) prevents accidental
-- duplicate numbers while leaving existing rows — which start blank — alone.

ALTER TABLE students ADD COLUMN IF NOT EXISTS student_no TEXT DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS ux_students_tenant_student_no
    ON students(tenant_id, student_no)
    WHERE student_no <> '' AND deleted_at IS NULL;
