-- 0017_drop_subject_pricing.sql
--
-- Pricing moved to the type×level matrix (0016). The old per-"subject" pricing
-- is dead: clear the seeded subject rows and drop the now-unused
-- classes.subject_id column. The subjects TABLE itself is intentionally kept
-- (empty) — the RLS policies in 0004/0015 reference it, and dropping it would
-- break a fresh-DB migration run for no real benefit.
DELETE FROM subjects;
ALTER TABLE classes DROP COLUMN IF EXISTS subject_id;
