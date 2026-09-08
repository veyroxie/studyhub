-- 0059_standing_discount.sql
--
-- A per-student monthly discount that the system can SEE.
--
-- The first real differ run (notes/pricing-bands.md section 15) found five
-- students invoiced below the catalogue with every discount column at zero:
-- Gareth by 40, Utaha by 30, Jiho Choi, Rui Xiang and Sukie Ren by 10 each.
-- The reduction existed only as a smaller number typed into the amount, so
-- switching billing to the catalogue would have RAISED those five bills with
-- nothing in the data to explain why -- to Nadine or to a parent who asked.
--
-- WHY NOT REUSE AN EXISTING DISCOUNT COLUMN. early_bird_discount is a mutation
-- with a clawback: applyEarlyBirdExpiry adds back the exact RM removed, so a
-- standing arrangement stored there would be wiped on the 7th of every month.
-- sibling_discount and referral_credit are derived from family and referral
-- state and are recomputed. This is none of those -- it is an arrangement with
-- one student that persists until someone changes it.
--
-- WHY NOT A LOWER TIER PRICE. The tier is what the centre charges for that
-- tier; this is what one family was given. Folding it into the tier reprices
-- everyone on it.
--
-- The reason is NOT optional in spirit even though the column allows empty: a
-- discount nobody can explain is how five of them ended up invisible in the
-- first place. The save path asks for it.
--
-- Additive. Nothing reads these columns until the cron switches over, so no
-- invoice can change.

ALTER TABLE students ADD COLUMN IF NOT EXISTS standing_discount        NUMERIC(12,2) NOT NULL DEFAULT 0;
ALTER TABLE students ADD COLUMN IF NOT EXISTS standing_discount_reason TEXT          NOT NULL DEFAULT '';

-- A discount cannot be negative -- that would be a surcharge wearing the wrong
-- name, and the invoice line would read as money off while adding money on.
ALTER TABLE students DROP CONSTRAINT IF EXISTS students_standing_discount_positive;
ALTER TABLE students ADD CONSTRAINT students_standing_discount_positive
    CHECK (standing_discount >= 0);
