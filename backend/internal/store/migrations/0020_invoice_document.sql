-- 0020_invoice_document.sql
--
-- Malaysian-convention invoice / receipt support. Adds the centre's bank +
-- payment details to the tenants row (so the PDF letterhead and payment
-- section are fully configurable, never hardcoded) and a receipt number to
-- invoices. Receipt numbers are drawn from a dedicated sequence so they're
-- monotonic and human-friendly (RCPT-000001, RCPT-000002, ...).

ALTER TABLE tenants ADD COLUMN IF NOT EXISTS bank_name TEXT DEFAULT '';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS bank_account_no TEXT DEFAULT '';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS bank_account_holder TEXT DEFAULT '';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS payment_instructions TEXT DEFAULT '';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS invoice_terms TEXT DEFAULT '';

ALTER TABLE invoices ADD COLUMN IF NOT EXISTS receipt_no TEXT;

CREATE SEQUENCE IF NOT EXISTS receipt_no_seq START 1;
