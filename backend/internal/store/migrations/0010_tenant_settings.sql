-- 0010_tenant_settings.sql
--
-- Per-tenant branding + operational settings. Stored as columns on the
-- existing tenants table since each row is read together at request time.
--
-- Columns are designed to be NOT NULL with sane defaults so a freshly
-- inserted tenant works without any post-install steps — the operator
-- can refine later via /api/admin/settings.

ALTER TABLE tenants ADD COLUMN IF NOT EXISTS slug TEXT DEFAULT '';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS brand_name TEXT NOT NULL DEFAULT 'The Study Hub';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS brand_tagline TEXT DEFAULT '';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS logo_path TEXT DEFAULT '';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS primary_color TEXT DEFAULT '#C9A227';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS address_line1 TEXT DEFAULT '';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS address_line2 TEXT DEFAULT '';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS tax_id TEXT DEFAULT '';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS support_email TEXT DEFAULT '';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS support_phone TEXT DEFAULT '';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS locale TEXT DEFAULT 'en-MY';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS currency TEXT DEFAULT 'MYR';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS timezone TEXT DEFAULT 'Asia/Kuala_Lumpur';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS invoice_footer_html TEXT DEFAULT '';

-- Slug is used by the branding endpoint to resolve a tenant from the
-- request host or query param. Backfill the default tenant so existing
-- deployments work without manual SQL.
UPDATE tenants SET slug='default' WHERE id=1 AND (slug IS NULL OR slug='');
CREATE UNIQUE INDEX IF NOT EXISTS idx_tenants_slug ON tenants(slug) WHERE slug <> '';
