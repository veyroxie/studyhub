-- 0038_invoice_period.sql
--
-- The billing month an invoice belongs to, as an explicit column.
--
-- Until now "which month is this invoice for" was inferred from the created_on
-- prefix, which conflates when the invoice was made with what it covers. The
-- monthly cron dedups on `type='Monthly' AND created_on LIKE '2026-08%'`, so an
-- invoice raised late for an earlier month is indistinguishable from one raised
-- on time, and generating for an arbitrary month is impossible.
--
-- The backfill deliberately uses substr(created_on,1,7), which is exactly what
-- the dedup matched on, so no invoice changes which month it counts as.
--
-- The uniqueness constraint that makes the monthly run race-free is NOT created
-- here. A pre-existing duplicate would make this migration fail, and a failed
-- migration stops the API booting. It lands in its own migration once the
-- duplicate check has been run against production.
ALTER TABLE invoices ADD COLUMN IF NOT EXISTS period TEXT NOT NULL DEFAULT '';

UPDATE invoices
   SET period = substr(created_on, 1, 7)
 WHERE type = 'Monthly'
   AND period = ''
   AND created_on IS NOT NULL
   AND length(created_on) >= 7;

CREATE INDEX IF NOT EXISTS idx_invoices_tenant_student_period
    ON invoices (tenant_id, student_id, period)
    WHERE type = 'Monthly' AND deleted_at IS NULL;
