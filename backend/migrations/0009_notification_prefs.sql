-- 0009_notification_prefs.sql
--
-- Per-user notification preferences. Default all true (opt-out, not
-- opt-in) — parents and teachers expect to be reachable; explicit toggle
-- is for the small minority who want quieter accounts.

ALTER TABLE users ADD COLUMN IF NOT EXISTS notify_invoice_reminders BOOLEAN DEFAULT TRUE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS notify_announcements     BOOLEAN DEFAULT TRUE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS notify_payment_receipts  BOOLEAN DEFAULT TRUE;
