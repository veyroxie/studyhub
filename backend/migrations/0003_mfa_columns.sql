-- 0003_mfa_columns.sql
--
-- Admin/superadmin MFA (TOTP RFC 6238) enrolment columns and the
-- intermediate-token table used between password verification and
-- TOTP verification at login.

ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_secret TEXT DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_enabled BOOLEAN DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_recovery_codes TEXT DEFAULT '[]';

CREATE TABLE IF NOT EXISTS mfa_intermediate (
    token       TEXT PRIMARY KEY,
    user_id     INTEGER NOT NULL,
    tenant_id   INTEGER NOT NULL,
    email       TEXT NOT NULL,
    role        TEXT NOT NULL,
    name        TEXT,
    remember_me BOOLEAN DEFAULT FALSE,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mfa_intermediate_expires ON mfa_intermediate(expires_at);
