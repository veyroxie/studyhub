-- 0005_tos_acceptance.sql
--
-- Versioned Terms of Service acceptance. The app bumps currentToSVersion
-- (in code) whenever the ToS changes; users with a lower tos_accepted_version
-- are forced through a click-through accept screen on their next login.
--
-- Default 0 means "never accepted" — newly created users and pre-existing
-- users alike land on the accept screen until they confirm.

ALTER TABLE users ADD COLUMN IF NOT EXISTS tos_accepted_version INTEGER DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS tos_accepted_at TIMESTAMPTZ;
