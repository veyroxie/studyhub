-- 0054_selfstudy_tier.sql
--
-- Self-Study has a price and always has: RM10 per hour beyond the included
-- quota, billed by the monthly cron. It lives as a Go constant --
-- `SelfStudyOverflowRatePerHour = 10.0` (cron.go:38) -- so changing what a
-- parent pays for extra self-study currently requires a developer, a build and
-- a deploy. That is the thing this rework exists to end.
--
-- 0051 got this wrong. Its comment claims a credit-covered category "charges
-- nothing" and therefore needs no plan rows. Wrong on the second half:
-- credit_covered describes how the CREDITS are consumed, not that the category
-- is free. Credits run out. What happens then has a price, and it belongs in
-- the catalogue with every other price.
--
-- HOURLY, NOT MONTHLY. Self-study overflow is billed per hour, so a plan needs
-- to be able to carry a rate rather than a subscription fee. `hourly_rate`
-- joins `monthly_fee`, and the CHECK becomes "at least one of them is set"
-- rather than "monthly_fee > 0". That keeps 0051's real guarantee -- a plan
-- can never be silently unpriced -- while admitting a shape it did not
-- anticipate.
--
-- NO BEHAVIOUR CHANGE. The rate seeded below is 10.00, the same number the
-- constant holds today. Nothing reads this table yet. The switchover replaces
-- the constant with a lookup, and on the day it does, the amount every parent
-- is charged stays exactly the same.

ALTER TABLE pricing_plans ADD COLUMN IF NOT EXISTS hourly_rate NUMERIC(12,2) NOT NULL DEFAULT 0;

-- Relax the price CHECK from "monthly_fee > 0" to "priced somehow". A plan
-- with neither is still refused: that is the silent zero the whole change
-- exists to make unstorable.
ALTER TABLE pricing_plans DROP CONSTRAINT IF EXISTS pricing_plans_priced;
ALTER TABLE pricing_plans ADD CONSTRAINT pricing_plans_priced
    CHECK (monthly_fee > 0 OR hourly_rate > 0);

-- monthly_fee must now be allowed to sit at 0 for an hourly-only plan.
ALTER TABLE pricing_plans ALTER COLUMN monthly_fee SET DEFAULT 0;

INSERT INTO pricing_plans (id, tenant_id, category_id, tier_name, sessions_per_week, monthly_fee, hourly_rate, sort_order) VALUES
    ('PP_self_overflow', 1, 'PC_selfstudy', 'Beyond included hours', 1, 0, 10.00, 1)
ON CONFLICT (tenant_id, category_id, tier_name, sessions_per_week) DO NOTHING;

-- Self-Study classes point at that tier so the lookup has somewhere to land.
UPDATE classes c SET default_tier_name = 'Beyond included hours'
  FROM pricing_categories pc
 WHERE pc.id = c.pricing_category_id AND pc.credit_covered
   AND c.deleted_at IS NULL AND COALESCE(c.default_tier_name,'') = '';
