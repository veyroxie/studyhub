-- 0007_refresh_tokens.sql
--
-- Refresh tokens for silent access-token rotation. The access JWT remains
-- the bearer credential on every API call (short or long lived per
-- deployment); the refresh token lives in a separate HttpOnly cookie
-- scoped to /api/auth/refresh and is single-use + rotating.
--
-- Reuse detection:
--   - Each refresh token belongs to a "family" (token_family TEXT). When
--     a token is exchanged, the old row is marked used_at=NOW() and a new
--     row is inserted with the same family.
--   - If a request presents an already-used token, every token in that
--     family is revoked (replaced_by != NULL means it's been rotated;
--     re-presenting it is a strong signal the cookie was stolen).
--
-- Storage:
--   - token_hash is the SHA-256 of the random token (the cleartext lives
--     only in the cookie). Compromising the DB doesn't compromise live
--     sessions.

CREATE TABLE IF NOT EXISTS refresh_tokens (
    token_hash   TEXT PRIMARY KEY,
    token_family TEXT NOT NULL,
    user_id      INTEGER NOT NULL,
    tenant_id    INTEGER NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    used_at      TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    user_agent   TEXT,
    ip           TEXT,
    created_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_family  ON refresh_tokens(token_family);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user    ON refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires ON refresh_tokens(expires_at);
