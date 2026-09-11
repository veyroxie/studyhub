-- 0066_pricing_plan_effective_dates.sql
--
-- A price is only true for a stretch of time. pricing_plans held exactly one
-- live row per (category, tier, sessions_per_week), so the catalogue could
-- answer "what does this cost" and nothing else. Re-rating an earlier month --
-- which the invoice differ and any re-run of the monthly job both do -- priced
-- it at today's rates and said nothing. store.CatalogPrices already takes an
-- `asOf`, but it only dated the ENROLMENT windows; the price itself had no
-- history to read.
--
-- Versions are half-open [effective_from, effective_to), matching the rule
-- enrolments already use: the day a price ends is the first day it is not
-- charged, so a successor starting that same day tiles exactly with no gap and
-- no overlap.
--
-- Existing rows become open-ended from a date before the first invoice, so
-- every historical month keeps resolving to the price it resolves to today.
-- This migration changes no price and no invoice.

CREATE EXTENSION IF NOT EXISTS btree_gist;

ALTER TABLE pricing_plans
    ADD COLUMN IF NOT EXISTS effective_from DATE,
    ADD COLUMN IF NOT EXISTS effective_to   DATE;

UPDATE pricing_plans SET effective_from = DATE '2000-01-01' WHERE effective_from IS NULL;

ALTER TABLE pricing_plans ALTER COLUMN effective_from SET NOT NULL;
-- New rows default to "from today". A second version of a plan must therefore
-- close the first (set its effective_to) or trip the exclusion constraint
-- below -- a loud failure rather than two prices claiming the same day.
ALTER TABLE pricing_plans ALTER COLUMN effective_from SET DEFAULT CURRENT_DATE;

ALTER TABLE pricing_plans DROP CONSTRAINT IF EXISTS pricing_plans_window;
ALTER TABLE pricing_plans ADD CONSTRAINT pricing_plans_window
    CHECK (effective_to IS NULL OR effective_to > effective_from);

-- ux_pricing_plans_live permitted exactly one live row per plan key, which is
-- precisely what versioning needs to stop being true. The exclusion constraint
-- replaces it with the weaker, correct rule: many versions, no two overlapping.
-- Soft-deleted rows sit outside it, as they did outside the unique index.
DROP INDEX IF EXISTS ux_pricing_plans_live;

ALTER TABLE pricing_plans DROP CONSTRAINT IF EXISTS pricing_plans_no_overlap;
ALTER TABLE pricing_plans ADD CONSTRAINT pricing_plans_no_overlap
    EXCLUDE USING gist (
        tenant_id         WITH =,
        category_id       WITH =,
        tier_name         WITH =,
        sessions_per_week WITH =,
        daterange(effective_from, effective_to, '[)') WITH &&
    ) WHERE (deleted_at IS NULL);
