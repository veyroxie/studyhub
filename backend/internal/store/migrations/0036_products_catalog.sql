-- 0036_products_catalog.sql
--
-- A catalogue of the things the centre actually sells. Until now the only
-- priced concept was tuition (pricing_tiers, a 2x2 type x level matrix), so
-- registration and deposit had to be typed as free-text line items on every
-- invoice — the amount lived in the admin's memory and drifted between
-- invoices. A product row makes each sellable thing named, priced, and
-- selectable from a dropdown.
--
-- pricing_tiers is NOT dropped or replaced here. The monthly cron still prices
-- tuition from it, and the tuition products below are seeded FROM it, so the
-- two agree on day one. Collapsing them into one table is a later step: doing
-- it in the same migration as the introduction would move the live billing
-- run onto an untested read path.
CREATE TABLE IF NOT EXISTS products (
    id                  TEXT PRIMARY KEY,
    tenant_id           INTEGER NOT NULL DEFAULT 1,
    category            TEXT NOT NULL,
    name                TEXT NOT NULL,
    descriptor          TEXT NOT NULL DEFAULT '',
    default_unit_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    class_type          TEXT NOT NULL DEFAULT '',
    level_band          TEXT NOT NULL DEFAULT '',
    active              BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order          INTEGER NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ,
    CONSTRAINT products_category_check CHECK (category IN
        ('tuition','registration','deposit','material','selfstudy','addon'))
);

-- Names are what the admin picks from and what the parent reads on the PDF, so
-- a duplicate is a support ticket waiting to happen. Case-insensitive, and
-- scoped to live rows so a deleted name can be reused.
CREATE UNIQUE INDEX IF NOT EXISTS idx_products_tenant_name
    ON products (tenant_id, lower(name)) WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_products_tenant_active
    ON products (tenant_id, active) WHERE deleted_at IS NULL;

-- Seed tuition from the live pricing matrix rather than hardcoded values: those
-- rows are per-tenant and the centre has edited them, so hardcoding would
-- re-introduce prices they already changed. The derived id keeps re-apply
-- idempotent.
INSERT INTO products (id, tenant_id, category, name, default_unit_amount, class_type, level_band, sort_order)
SELECT 'PRD_' || pt.id,
       pt.tenant_id,
       'tuition',
       pt.class_type || ' Level ' || pt.level_band || ' Tuition',
       pt.monthly_fee,
       pt.class_type,
       pt.level_band,
       CASE WHEN pt.class_type = 'Group' THEN 10 ELSE 20 END
FROM pricing_tiers pt
WHERE pt.deleted_at IS NULL
ON CONFLICT (id) DO NOTHING;

-- RM250 is the centre's confirmed registration fee.
INSERT INTO products (id, tenant_id, category, name, default_unit_amount, sort_order)
SELECT 'PRD_registration_' || t.id, t.id, 'registration', 'Registration Fee', 250, 30
FROM tenants t
ON CONFLICT (id) DO NOTHING;

-- Deposit is level-based but the amounts have not been confirmed, so these seed
-- INACTIVE: an unpriced row that can be picked is how "RM 0" reaches a parent.
-- Setting a price and activating them is an admin action, not a guess here.
INSERT INTO products (id, tenant_id, category, name, default_unit_amount, level_band, active, sort_order)
SELECT 'PRD_deposit_' || band.level_band || '_' || t.id,
       t.id, 'deposit', 'Deposit (Level ' || band.level_band || ')', 0, band.level_band, FALSE, 40
FROM tenants t
CROSS JOIN (VALUES ('1-3'), ('4-6')) AS band(level_band)
ON CONFLICT (id) DO NOTHING;
