-- 0067_invoice_issue_and_numbering.sql
--
-- An invoice is a document, not a row you keep editing. This adds the state it
-- was missing: when it was issued, the number it was issued under, and whether
-- it was later voided.
--
-- Numbering is GAPLESS and resets each year (ADR-016). A Postgres sequence
-- cannot do that -- nextval is never rolled back, so an abandoned transaction
-- burns a number permanently. A counter row can: the allocation below is an
-- ordinary UPDATE inside the issuing transaction, so if the issue rolls back
-- the number comes back with it. Gaplessness normally costs write throughput
-- because it serialises number assignment; at one operator issuing once a month
-- that cost is nil.
--
-- Existing invoices are NOT back-numbered. They were issued without numbers and
-- inventing a sequence for them now would fabricate a history that never
-- existed; they keep their INV_<timestamp> ids and issued_at is backfilled from
-- created_on. Numbering starts at 1 with the first invoice issued from here on.

ALTER TABLE invoices
    ADD COLUMN IF NOT EXISTS invoice_no    TEXT,
    ADD COLUMN IF NOT EXISTS issued_at     TEXT,
    ADD COLUMN IF NOT EXISTS voided_at     TEXT,
    -- The reissue that replaces a voided invoice, so the pair can be read as
    -- one correction rather than two unrelated documents.
    ADD COLUMN IF NOT EXISTS superseded_by TEXT;

UPDATE invoices SET issued_at = created_on WHERE issued_at IS NULL;

CREATE TABLE IF NOT EXISTS invoice_number_counters (
    tenant_id INTEGER NOT NULL,
    year      INTEGER NOT NULL,
    next_no   INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (tenant_id, year)
);

-- A number, once issued, belongs to exactly one invoice. Partial so the many
-- rows without one (everything issued before this migration) do not collide.
CREATE UNIQUE INDEX IF NOT EXISTS ux_invoices_number
    ON invoices (tenant_id, invoice_no)
    WHERE invoice_no IS NOT NULL;

-- 0039 stops a student being billed twice for one month. A voided invoice
-- still occupied that slot, so voiding and reissuing -- the only correction
-- mechanism we support (ADR-016) -- would have been refused by the very index
-- protecting against double billing, as a 409 nobody could explain.
DROP INDEX IF EXISTS idx_invoices_monthly_unique;
CREATE UNIQUE INDEX IF NOT EXISTS idx_invoices_monthly_unique
    ON invoices (tenant_id, student_id, period)
    WHERE type = 'Monthly' AND deleted_at IS NULL AND period <> '' AND status <> 'Void';
