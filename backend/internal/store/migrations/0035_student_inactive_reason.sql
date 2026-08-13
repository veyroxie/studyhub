-- 0035_student_inactive_reason.sql
--
-- When a student goes inactive the centre wants to know why and when, so
-- churn can actually be analysed ("migrated overseas" vs "cost" vs "schedule"
-- are very different problems). A fixed vocabulary is enforced in the UI
-- rather than free text — free text cannot be grouped in a report.
--
-- inactive_on is the date they stopped, which is not the same as the row's
-- updated timestamp: an admin often records the departure days later.
ALTER TABLE students ADD COLUMN IF NOT EXISTS inactive_reason TEXT DEFAULT '';
ALTER TABLE students ADD COLUMN IF NOT EXISTS inactive_on     TEXT DEFAULT '';
