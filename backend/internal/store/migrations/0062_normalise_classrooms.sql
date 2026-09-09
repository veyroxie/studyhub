-- 0062_normalise_classrooms.sql
--
-- Four rooms were stored as six spellings, and the clash check compares the
-- string.
--
--   Classroom 1  x12      classroom 1  x1
--   Classroom 2  x14      2            x2
--   Classroom 3  x12      Classroom 4  x1      (blank) x1
--
-- checkClassClash and checkWorkshopClash both match on `classroom=?`, so a
-- class in "2" and a class in "Classroom 2" at the same hour do not collide
-- and the double-booking guard misses them silently.
--
-- THIS IS NOT THEORETICAL. Three real conflicts exist in production right now,
-- all of them Tuesday in Classroom 2, all invisible for exactly this reason:
--
--   15:30-16:30  Teacher Chiying (Aria)  vs  Level 3 & 4
--   16:30-17:30  Level 1 & 2             vs  Level 3 & 4
--   16:30-17:30  Level 3 & 4             vs  Level 3 & 4
--
-- Normalising does NOT resolve those. They are already saved, and the guard
-- only runs on create and update. What it does is make them visible, and stop
-- the fourth one being created. Someone has to move a class.
--
-- IT ALSO MEANS EDITING ONE OF THOSE THREE WILL NOW 409. That is the guard
-- doing its job on a conflict that already existed, not a new fault -- but it
-- will look like one to whoever hits it first, so it is written down here.
--
-- Exact matches only. A LIKE or a regex would eventually rewrite a room named
-- "Classroom 2 (annex)" into a collision rather than out of one.
--
-- The blank is left alone: one class has no room recorded and inventing one
-- would put it in a room it may not use.

UPDATE classes SET classroom = 'Classroom 1'
 WHERE deleted_at IS NULL AND classroom IN ('classroom 1', 'CLASSROOM 1', '1');

UPDATE classes SET classroom = 'Classroom 2'
 WHERE deleted_at IS NULL AND classroom IN ('classroom 2', 'CLASSROOM 2', '2');

UPDATE classes SET classroom = 'Classroom 3'
 WHERE deleted_at IS NULL AND classroom IN ('classroom 3', 'CLASSROOM 3', '3');

UPDATE classes SET classroom = 'Classroom 4'
 WHERE deleted_at IS NULL AND classroom IN ('classroom 4', 'CLASSROOM 4', '4');

-- Trailing and leading space is the same fault wearing a quieter face: it
-- stores happily, matches nothing, and reads identically in the UI.
UPDATE classes SET classroom = btrim(classroom)
 WHERE deleted_at IS NULL AND classroom <> btrim(classroom);

UPDATE workshops SET classroom = btrim(classroom)
 WHERE deleted_at IS NULL AND classroom IS NOT NULL AND classroom <> btrim(classroom);
