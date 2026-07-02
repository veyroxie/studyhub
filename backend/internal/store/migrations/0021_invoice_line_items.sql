-- 0021_invoice_line_items.sql
--
-- Multi-line invoices. Each invoice carries a JSON array of line items
-- (tuition per class, self-study, and discount lines such as an included
-- self-study FOC line). invoices.amount stays the authoritative total, derived
-- server-side from the sum of the item amounts at creation. Legacy flat
-- invoices keep '[]' and render via a synthesized fallback line, so no backfill
-- is needed. Read as a whole with the parent invoice — never queried in SQL —
-- so a JSON column fits (mirrors the existing sibling_ids convention).

ALTER TABLE invoices ADD COLUMN IF NOT EXISTS line_items TEXT DEFAULT '[]';
