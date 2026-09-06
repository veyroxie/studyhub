-- 0049_bulletin_board.sql
--
-- Two things: the bulletin board columns, and a live bug fix.
--
-- THE BUG. Parent visibility is gated by
--   audience IN ('all','parents') OR audience LIKE 'class:%'
-- (store/parent_scope.go). The compose UI writes 'All Parents', 'All Staff'
-- and 'My Class Parents'. None of those match, so every manually written
-- announcement has been invisible to parents since the feature shipped. Only
-- the auto-generated 'class:<id>' notices (cancellations, reschedules) ever
-- reached anyone, which is why the feature looked like it worked.
--
-- Canonical values from here on: 'parents' | 'staff' | 'all' | 'class:<id>'.
-- 'My Class Parents' maps to 'parents' because its restriction is carried by
-- target_class_ids, which ParentAnnouncementFilter already enforces -- the
-- audience value was never what limited it.
--
-- THE BOARD. category separates a standing policy from a dated notice: they
-- have opposite lifecycles, and a policy that scrolls away under notices is
-- the exact complaint this is meant to fix. pinned is admin-only; teachers
-- post through the existing approval flow and set pin_requested, which the
-- admin sees when approving. updated_on is distinct from created_on so an
-- amended policy shows when it actually changed rather than when it was first
-- written -- otherwise a parent cannot tell whether they have read the
-- current version.

ALTER TABLE announcements ADD COLUMN IF NOT EXISTS category      TEXT    DEFAULT 'notice';
ALTER TABLE announcements ADD COLUMN IF NOT EXISTS pinned        BOOLEAN DEFAULT FALSE;
ALTER TABLE announcements ADD COLUMN IF NOT EXISTS pin_requested BOOLEAN DEFAULT FALSE;
ALTER TABLE announcements ADD COLUMN IF NOT EXISTS updated_on    TEXT    DEFAULT '';

-- Normalise the audience values so the visibility rule matches.
UPDATE announcements SET audience = 'parents' WHERE audience IN ('All Parents', 'My Class Parents');
UPDATE announcements SET audience = 'staff'   WHERE audience = 'All Staff';

-- Existing rows have never been edited, so updated_on starts at created_on.
UPDATE announcements SET updated_on = COALESCE(created_on, '') WHERE COALESCE(updated_on, '') = '';

-- The board reads pinned policies first; this keeps that ordered scan cheap
-- as announcements accumulate.
CREATE INDEX IF NOT EXISTS idx_announcements_board
    ON announcements (tenant_id, pinned DESC, created_on DESC)
    WHERE COALESCE(status, 'published') = 'published';
