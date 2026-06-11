-- 0016_pricing_matrix.sql
--
-- Tuition is priced by a 2×2 matrix: class type (Group/Private) × level band
-- (Level 1–3 / Level 4–6). This replaces the per-"subject" fee model — the
-- centre doesn't have subjects, it has levels. A class now carries its type and
-- level band; the monthly cron derives the fee from pricing_tiers, so the price
-- lives in ONE place (no per-class fee to mis-enter).

ALTER TABLE classes ADD COLUMN IF NOT EXISTS class_type TEXT DEFAULT 'Group';
ALTER TABLE classes ADD COLUMN IF NOT EXISTS level_band TEXT DEFAULT '';

CREATE TABLE IF NOT EXISTS pricing_tiers (
    id          TEXT PRIMARY KEY,
    tenant_id   INTEGER NOT NULL DEFAULT 1,
    class_type  TEXT NOT NULL,   -- 'Group' | 'Private'
    level_band  TEXT NOT NULL,   -- '1-3'   | '4-6'
    monthly_fee DOUBLE PRECISION NOT NULL DEFAULT 0,
    deleted_at  TEXT,
    UNIQUE (tenant_id, class_type, level_band)
);

-- Seed the four tiers for the default tenant with the centre's current prices.
-- ON CONFLICT DO NOTHING keeps re-applies + existing edits safe.
INSERT INTO pricing_tiers (id, tenant_id, class_type, level_band, monthly_fee) VALUES
    ('PT_group_1_3',  1, 'Group',   '1-3', 240),
    ('PT_group_4_6',  1, 'Group',   '4-6', 260),
    ('PT_private_1_3',1, 'Private', '1-3', 480),
    ('PT_private_4_6',1, 'Private', '4-6', 520)
ON CONFLICT (tenant_id, class_type, level_band) DO NOTHING;
