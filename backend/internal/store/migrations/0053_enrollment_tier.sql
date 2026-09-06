-- 0053_enrollment_tier.sql
--
-- Puts the level on the ENROLMENT, which is the change the whole rework rests
-- on. A student has no single level: Nadine's requirement is one child taking
-- Level 1 Mandarin and Level 2 Math, and `students.level_band` (0045) cannot
-- represent that. It is left in place and simply stops being read -- all 66
-- rows are empty, so nothing changes for anyone.
--
-- Three columns:
--
--   classes.pricing_category_id  which price catalogue this class belongs to.
--                                A property of the class, not the student.
--                                Named to avoid `classes.category`, which
--                                already exists and means Academic vs
--                                Non-academic -- a different idea entirely.
--   classes.default_tier_name    the tier to suggest when enrolling. A class
--                                called "Level 3 & 4" answers this for
--                                everyone in it.
--   enrollments.tier_name        the answer, per student per class. Overrides
--                                the class default.
--
-- WHY tier_name IS TEXT AND NOT A FOREIGN KEY. The tier vocabulary is
-- user-defined -- the point of 0051 is that Nadine can add a Music category
-- with tiers she names herself, without a migration. A CHECK listing valid
-- values would defeat that, and a foreign key needs a tiers table whose only
-- content would be `SELECT DISTINCT tier_name FROM pricing_plans`.
--
-- The hole that leaves -- a tier_name matching no plan -- is closed at SAVE
-- time in the handler and surfaced in the "needs a price" list, NOT left to be
-- discovered at month end. That is the deliberate trade: the constraint moves
-- from the database to the write path because the vocabulary is data. It is a
-- weaker guarantee than 0051's CHECK on price, and it is the reason the
-- save-time validation is not optional.
--
-- SESSIONS PER WEEK IS NOT STORED. It is COUNT(live enrolments in the same
-- category), because production shows twice-weekly students hold two
-- enrolments in two different classes, and idx_enrollments_live (0044) already
-- makes two rows mean two real slots.

ALTER TABLE classes     ADD COLUMN IF NOT EXISTS pricing_category_id TEXT REFERENCES pricing_categories(id);
ALTER TABLE classes     ADD COLUMN IF NOT EXISTS default_tier_name   TEXT NOT NULL DEFAULT '';
ALTER TABLE enrollments ADD COLUMN IF NOT EXISTS tier_name           TEXT NOT NULL DEFAULT '';

-- Category, joined on tenant so a second tenant's classes cannot be pointed at
-- tenant 1's catalogue.
UPDATE classes c SET pricing_category_id = pc.id
  FROM pricing_categories pc
 WHERE pc.tenant_id = c.tenant_id AND pc.name = 'Self-Study'
   AND c.deleted_at IS NULL AND c.name = 'Self-Study';

UPDATE classes c SET pricing_category_id = pc.id
  FROM pricing_categories pc
 WHERE pc.tenant_id = c.tenant_id AND pc.name = 'Private'
   AND c.deleted_at IS NULL AND c.pricing_category_id IS NULL
   AND COALESCE(c.class_type, 'Group') = 'Private';

UPDATE classes c SET pricing_category_id = pc.id
  FROM pricing_categories pc
 WHERE pc.tenant_id = c.tenant_id AND pc.name = 'Group'
   AND c.deleted_at IS NULL AND c.pricing_category_id IS NULL;

-- Tier from the class NAME, and ONLY where the name states it outright. The
-- production audit found the level was recorded in the name all along while
-- level_band sat empty on 36 of 37 classes.
--
-- Exact matches, not patterns. A LIKE would eventually catch something like
-- "Level 3 & 4 (trial)" and price it without anyone deciding to. The names
-- below are the ones actually in the database; anything else is left blank and
-- surfaced, because inventing a tier means inventing a bill.
UPDATE classes SET default_tier_name = 'Level 1-2' WHERE deleted_at IS NULL AND name = 'Level 1 & 2';
UPDATE classes SET default_tier_name = 'Level 3-4' WHERE deleted_at IS NULL AND name = 'Level 3 & 4';
UPDATE classes SET default_tier_name = 'Level 5-6' WHERE deleted_at IS NULL AND name = 'Level 5 & 6';

-- Seed live enrolments from their class's default. Only LIVE ones: an ended
-- enrolment belongs to a month already invoiced, and back-dating a tier onto
-- it would change what a past invoice would recompute to.
UPDATE enrollments e SET tier_name = c.default_tier_name
  FROM classes c
 WHERE c.id = e.class_id AND c.tenant_id = e.tenant_id
   AND e.ended_on IS NULL AND e.tier_name = ''
   AND c.default_tier_name <> '';

-- Still nothing reads any of this. `pricing_tiers` and the JSON class list
-- remain the live billing path until the switchover, so no invoice can change.
