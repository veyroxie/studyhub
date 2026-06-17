-- 0008_email_queue.sql
--
-- Persistent outbound email queue with retry + backoff. Replaces
-- fire-and-forget mailer.Send for paths where deliverability matters
-- (welcome, verify, reset, overdue reminders, payment confirmations).
--
-- Status:
--   pending  — queued, not yet attempted (or scheduled for retry)
--   sent     — delivered successfully
--   failed   — gave up after MAX_ATTEMPTS
--
-- Backoff: 1m → 5m → 30m → 2h → 12h, capped at MAX_ATTEMPTS=5. After
-- failure the row stays in the table for ops review (no auto-cleanup);
-- a background sweep prunes rows older than 90 days.

CREATE TABLE IF NOT EXISTS email_queue (
    id              SERIAL PRIMARY KEY,
    tenant_id       INTEGER DEFAULT 1,
    to_email        TEXT NOT NULL,
    subject         TEXT NOT NULL,
    body_html       TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at         TIMESTAMPTZ,
    last_error      TEXT,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_email_queue_pending ON email_queue(status, next_attempt_at) WHERE status='pending';
CREATE INDEX IF NOT EXISTS idx_email_queue_created ON email_queue(created_at);
