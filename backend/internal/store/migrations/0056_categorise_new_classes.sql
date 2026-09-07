-- 0056_categorise_new_classes.sql
--
-- Catches classes created between 0053 and the create-path fix.
--
-- 0053 backfilled `pricing_category_id` for every class that existed when it
-- ran, and stopped there. `handlers_classes.go` never wrote the column, so
-- every class created afterwards had none: three appeared within two days,
-- two of them with live students, each silently unpriceable. That is the hole
-- this whole rework exists to close, reopened through the create path.
--
-- The durable fix is `resolvePricingCategory` in the handler, which applies
-- this same rule on every create and on every edit. This migration only
-- repairs the rows created in the gap. Both must exist: without the handler
-- the next class is wrong again, and without this one the three already here
-- stay wrong until somebody edits them.
--
-- Tier is deliberately NOT derived. Category is structural and the class
-- already answers it; a tier is the priced thing, so a blank one stays blank
-- and shows up in the "needs a tier" list rather than being guessed into a
-- bill.

UPDATE classes c SET pricing_category_id = pc.id
  FROM pricing_categories pc
 WHERE pc.tenant_id = c.tenant_id AND pc.name = 'Self-Study'
   AND c.deleted_at IS NULL AND c.pricing_category_id IS NULL
   AND lower(trim(c.name)) = 'self-study';

UPDATE classes c SET pricing_category_id = pc.id
  FROM pricing_categories pc
 WHERE pc.tenant_id = c.tenant_id AND pc.name = 'Private'
   AND c.deleted_at IS NULL AND c.pricing_category_id IS NULL
   AND COALESCE(c.class_type, 'Group') = 'Private';

UPDATE classes c SET pricing_category_id = pc.id
  FROM pricing_categories pc
 WHERE pc.tenant_id = c.tenant_id AND pc.name = 'Group'
   AND c.deleted_at IS NULL AND c.pricing_category_id IS NULL;

-- Self-Study classes point at the overflow tier, matching 0054.
UPDATE classes c SET default_tier_name = 'Beyond included hours'
  FROM pricing_categories pc
 WHERE pc.id = c.pricing_category_id AND pc.credit_covered
   AND c.deleted_at IS NULL AND COALESCE(c.default_tier_name,'') = '';

-- Tier from the class NAME, exact matches only, same as 0053. A LIKE would
-- eventually catch "Level 3 & 4 (trial)" and price it without anyone deciding.
UPDATE classes SET default_tier_name = 'Level 1-2' WHERE deleted_at IS NULL AND COALESCE(default_tier_name,'') = '' AND name = 'Level 1 & 2';
UPDATE classes SET default_tier_name = 'Level 3-4' WHERE deleted_at IS NULL AND COALESCE(default_tier_name,'') = '' AND name = 'Level 3 & 4';
UPDATE classes SET default_tier_name = 'Level 5-6' WHERE deleted_at IS NULL AND COALESCE(default_tier_name,'') = '' AND name = 'Level 5 & 6';
