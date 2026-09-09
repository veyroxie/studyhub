-- 0060_level0_twice_weekly.sql
--
-- Twice-weekly Level 0, answered by Nadine 2026-09-09.
--
-- She priced Aria's week directly rather than giving a bundle:
--
--   RM80  Tuesday private
--   RM80  Saturday 09:00-10:00 private
--   RM60  Saturday 10:30-11:30 group with Aleena  (Phonics)
--   ----
--   RM220 per week
--
-- So there is NO twice-weekly discount at Level 0. It is simply RM80 a
-- session, twice, which is 640 a month. Every other level's twice-weekly price
-- is (2 x weekly - 30), and the pattern tempted an answer of 610 -- the
-- migration that added Level 0 deliberately refused to guess it, and refusing
-- was right, because the real answer is not on that pattern at all.
--
-- The RM60 group hour is Phonics, already priced by its own class override at
-- 240 a month (0058), so this migration adds only the private tier. Aria then
-- resolves to 640 + 240 = 880 a month, which is her 220 a week over four
-- weeks -- the check that this is the right number rather than merely a
-- number.
--
-- Aleena's RM90 a week is confirmed by the same message and needs nothing: her
-- package_amount of 360 is already exactly 4 x 90.

INSERT INTO pricing_plans (id, tenant_id, category_id, tier_name, sessions_per_week, monthly_fee, sort_order)
VALUES ('PP_prv_L0_2', 1, 'PC_private', 'Level 0', 2, 640.00, 0)
ON CONFLICT (tenant_id, category_id, tier_name, sessions_per_week) DO NOTHING;
