-- 0045_session_rates.sql
--
-- F8: session billing prices by the HOUR, per student band. Three additive
-- columns; the monthly cron is untouched until the F5 switchover.
--
-- pricing_tiers.hourly_rate: the matrix converts from monthly to hourly.
-- Backfill divides monthly_fee by 4 (a month is four weekly 1-hour
-- sessions), which reproduces the centre's quoted rates exactly:
-- 240/260/480/520 monthly -> 60/65/120/130 per hour (WhatsApp, 11/06).
--
-- classes.session_rate: per-session override for classes the matrix cannot
-- price (Phonics has no band, the 30-min class runs below Level 1,
-- negotiated rates). 0 = unset, same convention as 0037's
-- monthly_fee_override.
--
-- students.level_band: a student's own band, for mixed-level classes that
-- straddle the 1-3 / 4-6 boundary (an L4 student in an L3&4 class pays the
-- 4-6 rate). '' = unset, falls back to the class's band.

ALTER TABLE pricing_tiers ADD COLUMN IF NOT EXISTS hourly_rate NUMERIC(12,2) NOT NULL DEFAULT 0;

UPDATE pricing_tiers
   SET hourly_rate = ROUND(monthly_fee / 4.0, 2)
 WHERE hourly_rate = 0 AND monthly_fee > 0;

ALTER TABLE classes ADD COLUMN IF NOT EXISTS session_rate NUMERIC(12,2) NOT NULL DEFAULT 0;

ALTER TABLE students ADD COLUMN IF NOT EXISTS level_band TEXT NOT NULL DEFAULT '';
