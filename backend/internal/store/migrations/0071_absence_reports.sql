-- 0071_absence_reports.sql
--
-- A parent tells the centre, in the app, that a child will miss one session.
-- The replacement-credit rule is "told us at least 3 hours before class", and
-- until now the only record of when a parent told us was a teacher's memory of
-- a WhatsApp message. reported_at is that record, stamped by the server.
--
-- Told in time (in_time) waits for an admin or the class's teacher to approve
-- before any credit is granted (Ely, 2026-10-01). Told late is recorded as
-- 'late' and never earns one. A decided report is not reopened.
--
-- The session's date and times are copied in at report time, from the
-- canonical session expander, so the credit is sized by the class as it was
-- scheduled that day, not as it is when someone gets round to approving.
--
-- credit_id points at the replacement_credits row an approval created. Blank
-- when the session had already been credited another way (a cancellation, or a
-- teacher's "Absent + credit"): one credit per student per session, enforced in
-- store.GrantSessionCredit.

CREATE TABLE IF NOT EXISTS absence_reports (
    id             TEXT PRIMARY KEY,
    tenant_id      INTEGER NOT NULL,
    student_id     TEXT NOT NULL,
    class_id       TEXT NOT NULL,
    session_date   TEXT NOT NULL,
    session_time   TEXT NOT NULL,
    session_end    TEXT NOT NULL DEFAULT '',
    reported_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    reported_by    TEXT NOT NULL,
    reason         TEXT NOT NULL DEFAULT '',
    in_time        BOOLEAN NOT NULL,
    status         TEXT NOT NULL,
    decided_by     TEXT NOT NULL DEFAULT '',
    decided_at     TIMESTAMPTZ,
    decision_note  TEXT NOT NULL DEFAULT '',
    credit_id      TEXT NOT NULL DEFAULT '',
    deleted_at     TIMESTAMPTZ,
    CONSTRAINT absence_reports_status_check CHECK (status IN ('pending', 'approved', 'declined', 'late'))
);

-- One report per child per session.
CREATE UNIQUE INDEX IF NOT EXISTS uq_absence_reports_session
    ON absence_reports (tenant_id, student_id, class_id, session_date)
    WHERE deleted_at IS NULL;

-- The review queue: pending reports, oldest session first.
CREATE INDEX IF NOT EXISTS idx_absence_reports_pending
    ON absence_reports (tenant_id, session_date)
    WHERE status = 'pending' AND deleted_at IS NULL;
