-- Marks casual pay-per-session self-study students (not on the monthly package).
-- Only these are billable via the manual self-study invoice option; package
-- students are auto-billed for overflow by the monthly cron, so excluding them
-- from the manual picker prevents double-charging.
ALTER TABLE students ADD COLUMN IF NOT EXISTS dropin_self_study BOOLEAN DEFAULT false;
