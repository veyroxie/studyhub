-- Links a class to a subject in the Subjects catalogue, which carries the
-- monthly_fee (by category/level). The monthly invoice cron prices each
-- student as the sum of their enrolled classes' subject fees.
ALTER TABLE classes ADD COLUMN IF NOT EXISTS subject_id TEXT DEFAULT '';
