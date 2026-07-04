-- 0022_payroll_manual_edit.sql
--
-- Payroll rows can now be hand-edited by admin (PUT /api/payroll/{id}) and the
-- cron recomputes stale rows during the 1-7 window so late check-ins are
-- captured. manually_edited marks a row the admin touched: the recompute skips
-- it (and any Paid row) so a hand-corrected or settled amount is never
-- overwritten by the automatic refresh.

ALTER TABLE payroll ADD COLUMN IF NOT EXISTS manually_edited BOOLEAN DEFAULT FALSE;
