-- 0069_applied_discounts_and_outbox.sql
--
-- Two tables the invoice workflow needs before Nadine can review a month and
-- issue it.
--
-- applied_discounts: what actually came off an invoice, with provenance. Two of
-- our discounts are both a flat RM10 -- the early bird and the referral credit
-- -- so an amount alone cannot say which one an invoice carries. The early-bird
-- clawback currently identifies its own discount by MATCHING THE LINE NAME,
-- which breaks the day anyone renames what prints on an invoice.
--
-- There is deliberately NO discount_types table. The engine already owns the
-- type identities (rating.TypeEarlyBird and friends) and nothing would read a
-- table that merely repeated them -- a table with no reader is the inert
-- mechanism this rebuild exists to stop. type_id here is that stable identity.
--
-- outbox: issuing an invoice wants side effects (an email now, a MyInvois
-- submission if the centre ever crosses the RM3m threshold). Calling an
-- external system inside the issuing transaction is how events are lost on a
-- partial failure: the invoice commits and the email never sends, or worse the
-- email sends and the invoice rolls back. The row is written in the SAME
-- transaction as the issue, and a relay performs the effect afterwards.

CREATE TABLE IF NOT EXISTS applied_discounts (
    id         TEXT PRIMARY KEY,
    tenant_id  INTEGER NOT NULL,
    invoice_id TEXT NOT NULL,
    -- rating.Type* -- stable, so renaming what PRINTS never changes what it IS.
    type_id    TEXT NOT NULL,
    name       TEXT NOT NULL,
    -- Why this student qualified: which reward row, which siblings.
    source     TEXT NOT NULL DEFAULT '',
    sequence   INTEGER NOT NULL DEFAULT 0,
    -- Positive: what was actually taken off AFTER clamping, which is not always
    -- what was offered. The clawback restores exactly this.
    amount     NUMERIC(12,2) NOT NULL,
    -- applied | pending | earned | forfeited. The early bird is the only
    -- conditional one: granted on issue, resolved after the cutoff.
    state      TEXT NOT NULL DEFAULT 'applied',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ix_applied_discounts_invoice
    ON applied_discounts (tenant_id, invoice_id);

-- One discount of a given type per invoice. Two early birds on one invoice is a
-- double-grant, and the clawback would only ever restore one of them.
CREATE UNIQUE INDEX IF NOT EXISTS ux_applied_discounts_invoice_type
    ON applied_discounts (invoice_id, type_id);

CREATE TABLE IF NOT EXISTS outbox (
    id           BIGSERIAL PRIMARY KEY,
    tenant_id    INTEGER NOT NULL,
    topic        TEXT NOT NULL,
    payload      TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,
    attempts     INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT NOT NULL DEFAULT ''
);

-- The relay's only query: the unprocessed backlog, oldest first. Partial, so it
-- stays small as processed rows accumulate.
CREATE INDEX IF NOT EXISTS ix_outbox_pending
    ON outbox (created_at) WHERE processed_at IS NULL;
