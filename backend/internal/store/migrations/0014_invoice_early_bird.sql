-- Early-bird discount that is kept only if the invoice is paid by the cutoff
-- (the 10th). The invoice is issued at the discounted amount; if still unpaid
-- after early_bird_cutoff, a job adds early_bird_discount back to amount
-- (full price) and clears these fields. Mutating amount keeps it the single
-- source of truth for every payment path.
ALTER TABLE invoices ADD COLUMN IF NOT EXISTS early_bird_cutoff TEXT DEFAULT '';
ALTER TABLE invoices ADD COLUMN IF NOT EXISTS early_bird_discount DOUBLE PRECISION DEFAULT 0;
