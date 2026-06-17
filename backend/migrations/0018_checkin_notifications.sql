-- 0018_checkin_notifications.sql
--
-- Parent notifications for student check-in / check-out.
--
-- Two pieces:
--   1. push_subscriptions — one row per browser Push subscription a parent has
--      granted. A parent can have several (phone + laptop). Keyed by the unique
--      endpoint URL the browser's push service hands us; re-subscribing upserts.
--   2. users.notify_checkin_email — per-parent opt-in for the email channel.
--      Web push is always sent when a subscription exists; email is opt-in so
--      parents who don't want ~2 mails per child per class day can turn it off.
--      Defaults FALSE so we don't start mailing existing parents who never
--      asked for it — they opt in from the profile modal.

CREATE TABLE IF NOT EXISTS push_subscriptions (
    id           SERIAL PRIMARY KEY,
    tenant_id    INTEGER NOT NULL DEFAULT 1,
    parent_email TEXT    NOT NULL,
    endpoint     TEXT    NOT NULL UNIQUE,
    p256dh       TEXT    NOT NULL,
    auth         TEXT    NOT NULL,
    created_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_push_subs_parent ON push_subscriptions (tenant_id, parent_email);

-- Mirror the tenant_isolation backstop the other tenant-scoped tables use
-- (migrations 0004 + 0015). The app also filters by tenant_id in WHERE clauses;
-- this catches a forgotten filter at the DB layer.
DROP POLICY IF EXISTS tenant_isolation ON push_subscriptions;
CREATE POLICY tenant_isolation ON push_subscriptions
    USING (
        current_setting('app.tenant_id', true) IS NULL
        OR current_setting('app.tenant_id', true) = '0'
        OR tenant_id::text = current_setting('app.tenant_id', true)
    );
ALTER TABLE push_subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE push_subscriptions FORCE ROW LEVEL SECURITY;

ALTER TABLE users ADD COLUMN IF NOT EXISTS notify_checkin_email BOOLEAN DEFAULT FALSE;
