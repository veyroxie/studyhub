-- 0063_overflow_period_dedup.sql
--
-- The self-study overflow dedup keyed on the human month inside `description`
-- ("Aug 2026") -- a field the invoice edit modal lets an admin retype. Retitling
-- a cron-issued overflow invoice made the next run inside the 1-7 window miss it
-- and issue a second charge. Overflow invoices queue no email, so nothing
-- surfaced the duplicate; the parent simply saw two charges.
--
-- The dedup now keys on `period`, which the cron sets at insert time. Rows
-- created before that must be backfilled, or the first run after this migration
-- re-bills everyone who was already billed under the old scheme.
--
-- Overflow bills the PREVIOUS month and is issued during days 1-7 of the
-- following one, so created_on's month minus one is the billed month. That is
-- the same fact the description encoded, read from a column instead of prose.
-- The regex guard skips any row whose created_on is not a plain YYYY-MM-DD,
-- which would otherwise fail the cast.

UPDATE invoices
   SET period = to_char((created_on::date - INTERVAL '1 month'), 'YYYY-MM')
 WHERE type = 'Self-study Overflow'
   AND COALESCE(period, '') = ''
   AND created_on ~ '^\d{4}-\d{2}-\d{2}$';

-- NOT added here, deliberately: a partial UNIQUE index on
-- (tenant_id, student_id, period) WHERE type = 'Self-study Overflow'.
--
-- 0024 declined the equivalent index on Monthly invoices because a student can
-- legitimately hold two in one month (a manual sibling invoice alongside the
-- cron's). Whether the same is true of overflow -- whether an admin ever
-- hand-writes a second overflow charge for one month -- is a business question
-- that has not been answered, and a unique index is the wrong place to discover
-- the answer. The period dedup above closes the reachable bug; this index would
-- be the backstop behind it. See notes/code-review-2026-09-09.md T4.
