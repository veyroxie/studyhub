-- 0070_invoice_payment_note.sql
--
-- Why a payment was not accepted, for the parent to read. Rejecting a parent's
-- "I've paid" used to say nothing, so the parent saw their invoice go back to
-- unpaid with no reason and had to ask on WhatsApp. Written only when a payment
-- moves back to Unpaid; any later claim or confirmation clears it, so a stale
-- reason never sits beside a fresh claim.
ALTER TABLE invoices ADD COLUMN IF NOT EXISTS payment_note TEXT NOT NULL DEFAULT '';
