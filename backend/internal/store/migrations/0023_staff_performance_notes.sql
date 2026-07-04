-- 0023_staff_performance_notes.sql
--
-- Internal performance notes for a staff member, edited from the Staff modal's
-- performance tab. Previously the frontend PUT only { performanceNotes } which
-- the full-row staff UPDATE dropped (and blanked every other column). This adds
-- the backing column so the note persists.

ALTER TABLE staff ADD COLUMN IF NOT EXISTS performance_notes TEXT DEFAULT '';
