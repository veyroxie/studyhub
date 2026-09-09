-- 0061_catalog_unique_ignores_deleted.sql
--
-- A deleted category or tier permanently reserved its own name.
--
-- 0051 declared `UNIQUE (tenant_id, name)` on categories and
-- `UNIQUE (tenant_id, category_id, tier_name, sessions_per_week)` on plans.
-- Both are plain constraints, so they span soft-deleted rows -- and the
-- catalogue editor soft-deletes. Delete the "Level 1-2 @ 1x" tier, try to add
-- it back, and the API answers "Level 1-2 already exists in this category at 1x
-- a week", naming a row that is invisible in the UI with no way to recover it.
-- Same for a category name, and for renaming a category onto a deleted one.
--
-- The fix is the pattern the rest of the schema already uses: a partial UNIQUE
-- INDEX over live rows only, exactly like idx_enrollments_live (0044) and the
-- cancelled_classes index (0044) and the invoice period index (0039).
--
-- The existing lookup index stays -- it is partial but NOT unique, so it never
-- enforced anything and does not conflict.

ALTER TABLE pricing_categories DROP CONSTRAINT IF EXISTS pricing_categories_tenant_id_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS ux_pricing_categories_live
    ON pricing_categories (tenant_id, name)
    WHERE deleted_at IS NULL;

ALTER TABLE pricing_plans DROP CONSTRAINT IF EXISTS pricing_plans_tenant_id_category_id_tier_name_sessions_per__key;
CREATE UNIQUE INDEX IF NOT EXISTS ux_pricing_plans_live
    ON pricing_plans (tenant_id, category_id, tier_name, sessions_per_week)
    WHERE deleted_at IS NULL;

-- NOTE for whoever writes the next catalogue migration. Earlier migrations use
-- `ON CONFLICT (tenant_id, name) DO NOTHING`, which infers the plain
-- constraint this file drops. They are safe because they run BEFORE it and
-- never run again. Anything written AFTER this must spell out the predicate to
-- infer a partial index:
--
--   ON CONFLICT (tenant_id, name) WHERE deleted_at IS NULL DO NOTHING
--
-- Omitting it fails at apply time with "no unique or exclusion constraint
-- matching the ON CONFLICT specification", which is a loud, safe failure --
-- but only if someone reads this first.
