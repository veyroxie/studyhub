-- 0058_mandarin_category.sql
--
-- Mandarin becomes its own pricing category, which is what unblocks the last
-- two unpriceable students.
--
-- WHY A CATEGORY AND NOT A TIER UNDER GROUP. Chase and Luther each hold two
-- live Group enrolments: "Level 1 & 2" and "Mandarin". The price resolver
-- groups enrolments by category and counts the group to decide the frequency
-- tier, so a Mandarin tier sitting inside Group would read them as
-- twice-weekly Level 1 & 2 students and charge them 450 instead of 240 + 240.
-- The subject IS the pricing axis here, which is exactly what ADR-003 says a
-- category is for: Group, Private, Self-Study, Mandarin, Phonics.
--
-- The resolver already caught this. It refused to price them rather than
-- guessing, reporting "no tier on the enrolment or the class" for a group that
-- held one tiered class and one untiered one.
--
-- TIERS ARE Group AND Private, NOT LEVELS. Nadine, 09-08: "No level for those
-- taking Mandarin and Phonics." Skooly agrees, listing Mandarin only as
-- "Mandarin - Group" (RM240/month, RM60/week) and "Mandarin - Private".
--
-- Only the Group tier is created. Skooly lists Mandarin Private at BOTH 320
-- and 400, nobody has said which is current, and no student is on it -- so
-- creating it would mean picking a price nobody has confirmed for a product
-- nobody is buying.

INSERT INTO pricing_categories (id, tenant_id, name, credit_covered, sort_order)
VALUES ('PC_mandarin', 1, 'Mandarin', FALSE, 4)
ON CONFLICT (tenant_id, name) DO NOTHING;

INSERT INTO pricing_plans (id, tenant_id, category_id, tier_name, sessions_per_week, monthly_fee, sort_order)
VALUES ('PP_mandarin_grp_1', 1, 'PC_mandarin', 'Group', 1, 240.00, 1)
ON CONFLICT (tenant_id, category_id, tier_name, sessions_per_week) DO NOTHING;

-- Both Mandarin classes move across: the Thursday one with three students, and
-- the dayless one with none. Matched on the exact name, not a pattern -- a
-- LIKE would eventually catch "Mandarin (trial)" and reprice it without anyone
-- deciding to.
UPDATE classes c SET pricing_category_id = pc.id, default_tier_name = 'Group'
  FROM pricing_categories pc
 WHERE pc.tenant_id = c.tenant_id AND pc.name = 'Mandarin'
   AND c.deleted_at IS NULL AND c.name = 'Mandarin';

-- Phonics is RM60 an hour, group only, no level (Nadine 08-17), and its class
-- runs 10:30-11:30, so a month is 240.00. The stored override of 239.96 is
-- four sen short of that -- invisible while Aleena was on a package, and now
-- billable since Aria was added to the class and has none.
UPDATE classes SET monthly_fee_override = 240.00
 WHERE deleted_at IS NULL AND monthly_fee_override = 239.96;
