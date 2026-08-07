-- 0032_sessions_invalid_before.sql
--
-- Password reset/change revoked refresh tokens but left already-issued access
-- JWTs valid — a stolen "Remember me" cookie survived a reset for up to 30
-- days. The JWT middleware now rejects tokens whose iat predates this
-- timestamp; reset/change bump it to evict every live session at once.

ALTER TABLE users ADD COLUMN IF NOT EXISTS sessions_invalid_before TIMESTAMPTZ;
