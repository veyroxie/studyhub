-- 0039_invoice_period_unique.sql
--
-- One monthly invoice per student per billing month, enforced by the database
-- rather than by the cron remembering to check.
--
-- The monthly run preloads a set of already-invoiced students and skips them.
-- That is a read followed by a write with a gap in between, so two overlapping
-- runs — the 1st-to-7th catch-up window colliding with a manual "run now", or
-- two API containers — can both read "not yet invoiced" and both insert. This
-- index closes the gap: the second insert hits ON CONFLICT DO NOTHING and the
-- parent is not billed twice.
--
-- Partial on purpose:
--   type='Monthly'      — a registration fee or self-study overflow line is not
--                         "for" a month and several may legitimately coexist.
--   deleted_at IS NULL  — a voided invoice must not block re-issuing.
--   period <> ''        — legacy rows the backfill could not date stay exempt
--                         rather than colliding with each other on ''.
--
-- Safe to apply: verified against production on 2026-08-20 with the grouped
-- duplicate check in V2_REBUILD_PLAN.md section 8.55, which returned no rows.
CREATE UNIQUE INDEX IF NOT EXISTS idx_invoices_monthly_unique
    ON invoices (tenant_id, student_id, period)
    WHERE type = 'Monthly' AND deleted_at IS NULL AND period <> '';
