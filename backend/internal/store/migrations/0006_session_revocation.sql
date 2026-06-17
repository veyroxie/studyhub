-- 0006_session_revocation.sql
--
-- Server-side session revocation. JWT alone is bearer-only — once issued,
-- it's valid until expiry no matter what. This table lets us mark
-- specific tokens as revoked so logout, password change, and admin
-- suspension take effect immediately.
--
-- Strategy:
--   - Every issued JWT carries a 'jti' (JWT ID) claim — a random opaque id.
--   - On any revocation event we insert the jti here with the token's
--     original expiry. The JWT middleware checks the revoked set on every
--     request (cached in-memory with the user-status cache).
--   - Rows past expires_at are pruned by the existing background job
--     sweep — once the underlying JWT has timed out, the revocation row
--     is no longer needed.
--
-- This is a stepping-stone toward proper refresh tokens; the next phase
-- adds a row per session with rotation + reuse detection.

CREATE TABLE IF NOT EXISTS revoked_tokens (
    jti        TEXT PRIMARY KEY,
    user_id    INTEGER,
    revoked_at TIMESTAMPTZ DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    reason     TEXT
);

CREATE INDEX IF NOT EXISTS idx_revoked_tokens_expires ON revoked_tokens(expires_at);
