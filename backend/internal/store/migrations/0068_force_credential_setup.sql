-- 0068_force_credential_setup.sql
--
-- An admin handing out a temporary password has to be temporary. Without a
-- flag, "we will change it later" is the whole security model, and the seeded
-- teacher password that shipped in source for months is what that looks like
-- in practice.
--
-- must_change_credentials marks an account whose password was set BY SOMEONE
-- ELSE. Such a session can reach the setup endpoint and nothing else, so the
-- temporary password buys exactly one thing: the chance to replace it.
--
-- Existing accounts are untouched (FALSE). Turning it on for the three seeded
-- teacher logins is a deliberate act, not something a migration should do to a
-- live site mid-morning.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS must_change_credentials BOOLEAN NOT NULL DEFAULT FALSE;
