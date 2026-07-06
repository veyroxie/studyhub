-- 0027_ical_token_version.sql
--
-- Make the per-parent iCal feed URL revocable. The feed token is an HMAC over
-- the user id + email, so before this it was permanent: a leaked subscription
-- URL (browser history, a calendar app's sync logs) granted read access to a
-- child's schedule forever, revocable only by rotating JWT_SECRET (which logs
-- everyone out). Folding this version counter into the HMAC input lets us
-- invalidate one user's outstanding feed by bumping it (done on password reset).

ALTER TABLE users ADD COLUMN IF NOT EXISTS ical_token_version INTEGER NOT NULL DEFAULT 0;
