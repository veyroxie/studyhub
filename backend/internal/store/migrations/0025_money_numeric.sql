-- 0025_money_numeric.sql
--
-- Convert the remaining money columns from DOUBLE PRECISION to NUMERIC(12,2).
-- Migration 0002 fixed the original currency columns, but three money columns
-- were added later as floats:
--   invoices.early_bird_discount  (0014) — participates in amount arithmetic
--   pricing_tiers.monthly_fee     (0016) — the source of every cron invoice
--   registrations.school_fees     (schema) — captured at registration
-- Floats accumulate rounding error in billing math (amount = amount +
-- early_bird_discount, monthly_fee * n), so all money must be exact decimal.
-- Idempotent: re-running the ALTER on an already-NUMERIC column is a no-op cast.

ALTER TABLE invoices      ALTER COLUMN early_bird_discount TYPE NUMERIC(12,2) USING early_bird_discount::numeric;
ALTER TABLE pricing_tiers ALTER COLUMN monthly_fee         TYPE NUMERIC(12,2) USING monthly_fee::numeric;
ALTER TABLE registrations ALTER COLUMN school_fees         TYPE NUMERIC(12,2) USING school_fees::numeric;
