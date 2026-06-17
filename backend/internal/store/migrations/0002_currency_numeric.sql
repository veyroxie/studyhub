-- 0002_currency_numeric.sql
--
-- Currency columns to NUMERIC. DOUBLE PRECISION introduces float rounding
-- that compounds across discount/credit calculations; NUMERIC is exact
-- decimal. ALTER ... USING ::numeric preserves existing values.
--
-- Safe to re-run: if a column is already NUMERIC the ALTER is a no-op.

ALTER TABLE invoices  ALTER COLUMN amount           TYPE NUMERIC(12,2) USING amount::numeric;
ALTER TABLE invoices  ALTER COLUMN discount_pct     TYPE NUMERIC(5,2)  USING discount_pct::numeric;
ALTER TABLE invoices  ALTER COLUMN sibling_discount TYPE NUMERIC(12,2) USING sibling_discount::numeric;
ALTER TABLE invoices  ALTER COLUMN referral_credit  TYPE NUMERIC(12,2) USING referral_credit::numeric;
ALTER TABLE staff     ALTER COLUMN salary           TYPE NUMERIC(12,2) USING salary::numeric;
ALTER TABLE staff     ALTER COLUMN hourly_rate      TYPE NUMERIC(8,2)  USING hourly_rate::numeric;
ALTER TABLE payroll   ALTER COLUMN base_salary      TYPE NUMERIC(12,2) USING base_salary::numeric;
ALTER TABLE payroll   ALTER COLUMN bonus            TYPE NUMERIC(12,2) USING bonus::numeric;
ALTER TABLE payroll   ALTER COLUMN deductions       TYPE NUMERIC(12,2) USING deductions::numeric;
ALTER TABLE payroll   ALTER COLUMN total            TYPE NUMERIC(12,2) USING total::numeric;
ALTER TABLE subjects  ALTER COLUMN monthly_fee      TYPE NUMERIC(12,2) USING monthly_fee::numeric;
ALTER TABLE workshops ALTER COLUMN fee              TYPE NUMERIC(12,2) USING fee::numeric;
ALTER TABLE students  ALTER COLUMN package_amount   TYPE NUMERIC(12,2) USING package_amount::numeric;
