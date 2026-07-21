-- 0028_recompute_enrolled.sql
--
-- One-time repair of drifted classes.enrolled counters. The counter is a
-- derived value maintained by the app's recompute paths (student
-- add/edit/delete, registration approve, import), but historical data and a
-- since-fixed class-edit path that trusted a client-supplied value left some
-- counts out of sync with the actual enrolments. Capacity enforcement reads
-- this counter, so drift means a full class can look open (or vice-versa).
--
-- Recompute every non-deleted class from the authoritative source: the number
-- of non-deleted students whose enrolled_classes JSON array contains the class
-- id. Runs once (tracked in schema_migrations); ongoing correctness is handled
-- in application code.

UPDATE classes c SET enrolled = (
    SELECT count(*) FROM students s
    WHERE s.deleted_at IS NULL
      AND s.enrolled_classes LIKE '%"' || c.id || '"%'
)
WHERE c.deleted_at IS NULL;
