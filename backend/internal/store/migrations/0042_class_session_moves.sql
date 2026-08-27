-- 0042_class_session_moves.sql
--
-- Reschedule one dated session to another date, for every enrolled student
-- at once. The centre wants BOTH models (Nadine, 27/08): cancel+credit for
-- per-student make-ups, and a wholesale move when the whole class shifts.
--
-- Same exception-row shape as cancelled_classes and 0040's overrides, keyed
-- by (class_id, date-it-normally-occurs). Unlike cancellations this table is
-- soft-deleted, so a move can be undone; and a move grants NO replacement
-- credits -- the class still happens, just elsewhere in the week.

CREATE TABLE IF NOT EXISTS class_session_moves (
    id          TEXT PRIMARY KEY,
    tenant_id   INTEGER NOT NULL DEFAULT 1,
    class_id    TEXT NOT NULL,
    from_date   TEXT NOT NULL,
    to_date     TEXT NOT NULL,
    reason      TEXT NOT NULL DEFAULT '',
    moved_by    TEXT NOT NULL DEFAULT '',
    created_on  TEXT NOT NULL DEFAULT '',
    deleted_at  TIMESTAMPTZ
);

-- One live move per session; undoing (soft delete) frees the slot again.
CREATE UNIQUE INDEX IF NOT EXISTS idx_session_move_unique
    ON class_session_moves (tenant_id, class_id, from_date) WHERE deleted_at IS NULL;
