-- 0050_audience_constraint.sql
--
-- Make an unreachable announcement impossible to store.
--
-- 0049 normalised the values and fixed the compose form, but an audit found
-- two more writers still emitting the display string: calendar.js (the "new
-- class added" notice) and seed.go. Patching call sites fixes the ones you
-- find; the fifth writer added next year has the same 50/50 odds.
--
-- The root cause is not the strings. It is that audience is free text whose
-- contract lives only in a SQL fragment in parent_scope.go, so a writer and a
-- reader can disagree and NOTHING fails -- the announcement is stored, shown
-- to nobody, and looks fine in the admin list. Same shape as a class with no
-- priced tier: a value that matches nothing, answered with silence.
--
-- A CHECK constraint is the backstop because it does not care which code path,
-- language, or hand-written SQL produced the row. Server-side validation gives
-- the readable error; this guarantees the invariant.
--
-- Cost, accepted deliberately: adding an audience value later needs a
-- migration. That is correct — it changes who can read things, and should be
-- a decision rather than a typo.

-- Re-run the normalisation: 0049 only saw rows existing at the time, and the
-- unfixed writers have been adding more since.
UPDATE announcements SET audience = 'parents' WHERE audience IN ('All Parents', 'My Class Parents');
UPDATE announcements SET audience = 'staff'   WHERE audience = 'All Staff';
UPDATE announcements SET audience = 'parents' WHERE COALESCE(audience, '') = '';

-- Anything still unrecognised becomes 'staff': visible to admins, invisible to
-- parents. Failing closed is right for an unknown audience -- showing a message
-- to the wrong people is worse than showing it to too few, and it stays
-- visible on the admin side where someone can fix it.
UPDATE announcements SET audience = 'staff'
 WHERE audience NOT IN ('all', 'parents', 'staff') AND audience NOT LIKE 'class:%';

ALTER TABLE announcements DROP CONSTRAINT IF EXISTS announcements_audience_known;
ALTER TABLE announcements ADD CONSTRAINT announcements_audience_known
    CHECK (audience IN ('all', 'parents', 'staff') OR audience LIKE 'class:%');
