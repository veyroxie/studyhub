-- 0031_announcement_targets.sql
--
-- Teacher "My Class Parents" announcements computed targetClassIds in the
-- client and then dropped them: no column existed, so the targeting silently
-- degraded to every parent in the tenant. JSON array of class ids, matching
-- the other id-array columns; '' = untargeted (all parents).

ALTER TABLE announcements ADD COLUMN IF NOT EXISTS target_class_ids TEXT DEFAULT '';
