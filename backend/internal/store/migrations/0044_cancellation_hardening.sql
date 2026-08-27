-- 0044_cancellation_hardening.sql
--
-- F3: cancellations become money once session billing lands. Until now the
-- table had no soft delete (a mistaken cancellation could never be reversed
-- through the API) and no uniqueness (every duplicate POST re-granted every
-- enrolled student a make-up credit).
--
-- Order matters: add the column, soft-delete historical duplicates keeping
-- the earliest row, claw back the credits those duplicates granted, THEN
-- create the partial unique index -- creating it first would abort on any
-- production duplicate.

ALTER TABLE cancelled_classes ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

UPDATE cancelled_classes cc SET deleted_at = NOW()
 WHERE cc.deleted_at IS NULL
   AND EXISTS (
     SELECT 1 FROM cancelled_classes k
      WHERE k.tenant_id = cc.tenant_id
        AND k.class_id  = cc.class_id
        AND k.date      = cc.date
        AND k.deleted_at IS NULL
        AND (k.created_on, k.id) < (cc.created_on, cc.id)
   );

-- Each duplicate POST also duplicated the per-student credit grant (the
-- fingerprint 0041 established: earned / class / 'Class cancelled on <date>').
-- Keep one grant per (student, class, date), delete the rest.
DELETE FROM replacement_credits rc
 USING replacement_credits k
 WHERE rc.type = 'earned' AND rc.category = 'class'
   AND rc.note = 'Class cancelled on ' || rc.date
   AND k.tenant_id  = rc.tenant_id
   AND k.student_id = rc.student_id
   AND k.class_id   = rc.class_id
   AND k.date       = rc.date
   AND k.note       = rc.note
   AND k.type = 'earned' AND k.category = 'class'
   AND k.id < rc.id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_cancelled_classes_unique
    ON cancelled_classes (tenant_id, class_id, date)
    WHERE deleted_at IS NULL;
