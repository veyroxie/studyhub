-- 0041_cancellation_credit_topup.sql
--
-- Top up replacement credits granted by class cancellations from a flat 1 to
-- the agreed unit of 1 credit = 15 minutes (WhatsApp 02/04/2026: a 1-hour
-- class = 4 credits). The cancellation handler hardcoded minutes=1, which
-- undercompensated every cancelled class 4x and surfaced as the centre's
-- "insufficient credit" report on 2026-08-19 (V2_IDEAS A16).
--
-- Rows are identified by the handler's fingerprint: earned, category class,
-- minutes=1, and the auto-generated note prefix. Manual grants use free-text
-- notes and are untouched. Idempotent: touched rows leave minutes=1, so a
-- re-run matches nothing (a 15-minute class would recompute to the same 1).

-- Classes with parsable HH:MM times: credit the actual duration / 15.
UPDATE replacement_credits rc
SET minutes = (EXTRACT(EPOCH FROM (c.end_time::time - c.time::time)) / 900)::int
FROM classes c
WHERE c.id = rc.class_id AND c.tenant_id = rc.tenant_id
  AND rc.type = 'earned' AND rc.category = 'class' AND rc.minutes = 1
  AND rc.note LIKE 'Class cancelled on %'
  AND c.time ~ '^[0-2]?[0-9]:[0-5][0-9]$' AND c.end_time ~ '^[0-2]?[0-9]:[0-5][0-9]$'
  AND (EXTRACT(EPOCH FROM (c.end_time::time - c.time::time)) / 900)::int >= 1;

-- Anything left at 1 with the fingerprint (deleted class, blank times):
-- standard 1-hour class.
UPDATE replacement_credits
SET minutes = 4
WHERE type = 'earned' AND category = 'class' AND minutes = 1
  AND note LIKE 'Class cancelled on %';
