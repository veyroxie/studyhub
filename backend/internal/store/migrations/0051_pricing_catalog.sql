-- 0051_pricing_catalog.sql
--
-- Replaces the fixed 2x2 price grid with a catalogue Nadine can edit herself.
--
-- WHY NOT JUST WIDEN THE GRID. `pricing_tiers` is (class_type, level_band) ->
-- fee, seeded with Group/Private x 1-3/4-6. Going to three bands would be one
-- more column of the same shape, and the production audit says the shape is
-- what is wrong: of 37 classes, 34 cannot be priced by it. Self-Study has no
-- level, Mandarin has none recorded, and adding a subject like Music needs a
-- migration and a developer. A wider grid leaves the same holes in different
-- places.
--
-- THE MODEL. A category owns named tiers. A (category, tier, sessions/week)
-- triple carries one price. A class belongs to a category; an enrolment picks
-- the tier -- which is what lets one child be Level 1 Mandarin and Level 2
-- Math, the requirement that makes a level on the STUDENT unrepresentable.
--
-- FREQUENCY IS A PRICE DIMENSION BUT NOT A STORED FIELD. Production shows
-- twice-a-week students hold two live enrolments in two different classes
-- (Lucy Lee and Jiho Choi, both "Level 3 & 4"). So sessions/week is the COUNT
-- of live enrolments in a category, and `idx_enrollments_live` (0044) already
-- guarantees two rows mean two real slots rather than a duplicate. Storing it
-- as well would create a second source of truth that can drift.
--
-- This migration is ADDITIVE ONLY. Nothing reads these tables yet, so it
-- cannot change a single invoice. `pricing_tiers` stays exactly as it is until
-- the pricing paths switch over in a later migration.

CREATE TABLE IF NOT EXISTS pricing_categories (
    id             TEXT PRIMARY KEY,
    tenant_id      INTEGER NOT NULL DEFAULT 1,
    name           TEXT NOT NULL,
    -- Self-Study bills nothing WHILE the student has credits (Ely, 09-06);
    -- it is not unconditionally free, and the balance check lives in the
    -- scheduling path where it can warn someone in time to act.
    credit_covered BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order     INTEGER NOT NULL DEFAULT 0,
    deleted_at     TEXT,
    UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS pricing_plans (
    id                TEXT PRIMARY KEY,
    tenant_id         INTEGER NOT NULL DEFAULT 1,
    category_id       TEXT NOT NULL REFERENCES pricing_categories(id),
    tier_name         TEXT NOT NULL,
    sessions_per_week INTEGER NOT NULL DEFAULT 1,
    monthly_fee       NUMERIC(12,2) NOT NULL,
    sort_order        INTEGER NOT NULL DEFAULT 0,
    deleted_at        TEXT,
    UNIQUE (tenant_id, category_id, tier_name, sessions_per_week),
    -- A plan priced at 0 is the bug this whole change exists to close: a class
    -- with no resolvable price was skipped in silence for a month. A category
    -- that legitimately charges nothing is marked credit_covered and has no
    -- plan rows at all, so 0 never needs to be storable.
    CONSTRAINT pricing_plans_priced CHECK (monthly_fee > 0),
    CONSTRAINT pricing_plans_frequency CHECK (sessions_per_week BETWEEN 1 AND 7)
);

CREATE INDEX IF NOT EXISTS idx_pricing_plans_lookup
    ON pricing_plans (tenant_id, category_id, tier_name, sessions_per_week)
    WHERE deleted_at IS NULL;

-- Seed the centre's current catalogue. Deterministic ids so a re-run is a
-- no-op rather than a second set of rows.
INSERT INTO pricing_categories (id, tenant_id, name, credit_covered, sort_order) VALUES
    ('PC_group',      1, 'Group',      FALSE, 1),
    ('PC_private',    1, 'Private',    FALSE, 2),
    ('PC_selfstudy',  1, 'Self-Study', TRUE,  3)
ON CONFLICT (tenant_id, name) DO NOTHING;

-- Prices confirmed by Nadine 02/09 (weekly) and 02/09 (twice weekly), mapped
-- onto the three new bands by the rule Ely confirmed 09-06: the new 1-2 takes
-- the old 1-3 price; 3-4 and 5-6 both take the old 4-6 price. Nadine adds the
-- Level 3 discount by hand for now.
--
-- Every twice-weekly figure is exactly (2 x weekly - 30), and Ying Quah
-- confirmed the RM30. It is stored as six prices rather than as that formula
-- deliberately: the discount is a fact about today's numbers, not a policy,
-- and encoding it would silently move every 2x price the next time a weekly
-- rate changes.
INSERT INTO pricing_plans (id, tenant_id, category_id, tier_name, sessions_per_week, monthly_fee, sort_order) VALUES
    ('PP_grp_12_1', 1, 'PC_group',   'Level 1-2', 1,  240.00, 1),
    ('PP_grp_12_2', 1, 'PC_group',   'Level 1-2', 2,  450.00, 2),
    ('PP_grp_34_1', 1, 'PC_group',   'Level 3-4', 1,  260.00, 3),
    ('PP_grp_34_2', 1, 'PC_group',   'Level 3-4', 2,  490.00, 4),
    ('PP_grp_56_1', 1, 'PC_group',   'Level 5-6', 1,  260.00, 5),
    ('PP_grp_56_2', 1, 'PC_group',   'Level 5-6', 2,  490.00, 6),
    ('PP_prv_12_1', 1, 'PC_private', 'Level 1-2', 1,  480.00, 1),
    ('PP_prv_12_2', 1, 'PC_private', 'Level 1-2', 2,  930.00, 2),
    ('PP_prv_34_1', 1, 'PC_private', 'Level 3-4', 1,  520.00, 3),
    ('PP_prv_34_2', 1, 'PC_private', 'Level 3-4', 2, 1010.00, 4),
    ('PP_prv_56_1', 1, 'PC_private', 'Level 5-6', 1,  520.00, 5),
    ('PP_prv_56_2', 1, 'PC_private', 'Level 5-6', 2, 1010.00, 6)
ON CONFLICT (tenant_id, category_id, tier_name, sessions_per_week) DO NOTHING;

-- Self-Study deliberately has no plan rows: credit_covered says it bills
-- nothing while credits remain, and a 0-priced plan would be indistinguishable
-- from a class someone forgot to price.

-- Standardise the Self-Study name while we are here (Ely, 09-06). Safe: it is
-- a display name, nothing keys on it, and 'Self-study' (2 rows) vs
-- 'Self-Study' (8) is only going to get worse as more are added.
UPDATE classes SET name = 'Self-Study'
 WHERE deleted_at IS NULL AND name <> 'Self-Study' AND lower(name) = 'self-study';
