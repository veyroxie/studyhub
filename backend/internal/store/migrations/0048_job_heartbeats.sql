-- 0048_job_heartbeats.sql
--
-- Every scheduled job in this system can stop without anyone noticing. Two
-- real outages on 2026-09-01 were exactly that shape: the nightly backup ran
-- for months while its upload silently did nothing, and WebSocket upgrades
-- failed every nine seconds since June. Both were found by reading production
-- logs during an unrelated investigation, not by any check.
--
-- The existing self-check watched two specific symptoms (disk, local backup
-- age). That is a per-symptom approach: it covers what someone thought of, and
-- says nothing about the other nine scheduled jobs.
--
-- A heartbeat inverts it. Each job records that it completed; the self-check
-- alerts on any job whose last success is older than its own interval allows.
-- A job added later is covered automatically, because the recording happens in
-- the shared runner rather than in each job. That is the difference between
-- fixing an instance and closing the category.

CREATE TABLE IF NOT EXISTS job_heartbeats (
    name            TEXT PRIMARY KEY,
    last_success_at TIMESTAMPTZ NOT NULL,
    detail          TEXT NOT NULL DEFAULT ''
);
