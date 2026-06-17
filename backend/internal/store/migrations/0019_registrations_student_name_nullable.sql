-- 0019_registrations_student_name_nullable.sql
--
-- Parent self-registration (/api/register) records a `type='parent'` row in
-- `registrations` with no student attached — the student is enrolled later.
-- But the table declared student_first_name / student_last_name NOT NULL, so
-- that insert failed and the whole signup transaction rolled back.
-- Drop the NOT NULL: those fields are only meaningful for enrollment-type rows.

ALTER TABLE registrations ALTER COLUMN student_first_name DROP NOT NULL;
ALTER TABLE registrations ALTER COLUMN student_last_name  DROP NOT NULL;
