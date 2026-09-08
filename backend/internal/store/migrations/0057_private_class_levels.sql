-- 0057_private_class_levels.sql
--
-- Nadine's levels for the private students (WhatsApp, 2026-09-08), plus the
-- Level 0 tier they need.
--
-- Private classes are one-to-one and named after the student, so the level
-- lands on the CLASS as its default tier rather than on each enrolment. The
-- enrolment can still override it, which is what makes a group class able to
-- hold two levels; for a class with one child in it there is nothing to
-- override.
--
-- Levels map to bands by the rule agreed in section 3c: 1 and 2 -> Level 1-2,
-- 3 and 4 -> Level 3-4, 5 and 6 -> Level 5-6.
--
--   Level 0   Aria                      -> new tier, see below
--   Level 2   Koki        -> Level 1-2  (480, and his package is exactly 480)
--   Level 3   Gareth      -> Level 3-4  (520, discounted to 480 by hand)
--   Level 4   Luda        -> Level 3-4
--   Level 4   Jiho Yoo    -> Level 3-4  (520, and his package is exactly 520)
--   Level 5   Geneva      -> Level 5-6
--   Level 5   Zia         -> Level 5-6
--   Level 5   Carolina    -> Level 5-6  (her session_rate is 130, = 520/4)
--   Level 4   Lucas Lin   -> Level 3-4  INFERRED, see below
--
-- Koki, Jiho Yoo and Carolina landing on figures already stored against them
-- independently is the check that this mapping is right.
--
-- GARETH IS BILLED 520 AND DISCOUNTED BY HAND, deliberately. Nadine prices
-- Level 3 at 480 but the three-tier split is not decided yet (ADR-007), so the
-- catalogue keeps the band price and the difference is recorded as a discount
-- per ADR-013 rather than folded into a tier nobody has agreed.
--
-- LUCAS LIN YUMO IS INFERRED, not answered. He was never on the list Ely sent
-- Nadine, so she has not skipped him. Two signals agree: he turned 9 in
-- January 2026, which is around Level 3-4, and he is invoiced 520, which on
-- Nadine's per-level list excludes Level 3 (480) and Level 1-2 (480).
--
-- This is safe to set precisely because it cannot move his bill: Private
-- Level 3-4 and Level 5-6 are BOTH 520, so the only thing at stake is the
-- label. It is a guess where a guess is free, and Nadine can change it in the
-- pricing screen without a migration. That is the whole point of her having
-- one.
--
-- NOT INCLUDED, and why:
--   Amelia and Aileen -- new private students on packages, not on Nadine's
--     list. Their package prices them; the tier can wait for her.
--   Mandarin -- needs its own category, not a tier under Group. A "Mandarin"
--     tier inside Group would make every Mandarin student look like a
--     twice-weekly Group student. Its own migration.
--
-- Nothing here changes an invoice: the monthly cron still prices from
-- pricing_tiers. This only makes the catalogue able to answer.

-- Level 0 Private. RM320/month, RM80/week in Skooly, and RM80 is already the
-- session_rate on Aria's Saturday class -- Nadine confirmed that figure 09-08.
INSERT INTO pricing_plans (id, tenant_id, category_id, tier_name, sessions_per_week, monthly_fee, sort_order)
VALUES ('PP_prv_L0_1', 1, 'PC_private', 'Level 0', 1, 320.00, 0)
ON CONFLICT (tenant_id, category_id, tier_name, sessions_per_week) DO NOTHING;

-- There is deliberately NO twice-weekly Level 0 price. Aria holds two live
-- Private slots and Skooly has never had a 2x Level 0 rate, so after this she
-- is still unpriceable -- but for the right reason, and the differ now says
-- "no price for Level 0 at 2x a week" instead of "no tier". Every other 2x
-- price is (2 x weekly - 30), which would make it 610; that pattern is not a
-- policy anyone has confirmed, and inventing it would be inventing a bill.

UPDATE classes SET default_tier_name = 'Level 0'
 WHERE deleted_at IS NULL AND COALESCE(default_tier_name,'') = ''
   AND id IN ('c5', 'c_1788026654869_db1zc');

UPDATE classes SET default_tier_name = 'Level 1-2'
 WHERE deleted_at IS NULL AND COALESCE(default_tier_name,'') = ''
   AND id = 'c1';

UPDATE classes SET default_tier_name = 'Level 3-4'
 WHERE deleted_at IS NULL AND COALESCE(default_tier_name,'') = ''
   AND id IN ('c_1783268683613_ek4tl', 'c_1782960582226_8zuee', 'c_1777045138678_oujjt',
              'c_1787124175256_ejxwf');

UPDATE classes SET default_tier_name = 'Level 5-6'
 WHERE deleted_at IS NULL AND COALESCE(default_tier_name,'') = ''
   AND id IN ('c_1782957082709_k6hn8', 'c_1782960866549_bm1b5', 'c_1782960759773_h3thr');
