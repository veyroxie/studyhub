-- 0024_payroll_unique_month.sql
--
-- Backstop for the payroll dedup preload (see cron.go generateMonthlyPayroll):
-- one staff member can only have a single payroll row per month per tenant. If a
-- transient error ever slips past the fail-closed preload, this unique index
-- turns the duplicate INSERT into a hard error instead of double pay.
--
-- Deliberately NOT added to invoices: a student can legitimately have two
-- Monthly invoices in one month (manual sibling invoice + admin adhoc), so the
-- fail-closed dedup in generateMonthlyInvoices is the correct remedy there.

CREATE UNIQUE INDEX IF NOT EXISTS ux_payroll_staff_month ON payroll(tenant_id, staff_id, month);
