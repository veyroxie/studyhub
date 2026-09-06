package store

import (
	"fmt"
	"time"

	"studyhub/internal/core"
)

// Job heartbeats (migration 0048). A job records that it finished; the health
// self-check alerts on any job whose last success is older than its interval
// allows. Recording lives in the shared job runner, so a job added later is
// covered without anyone remembering to add a check for it.
//
// Shell jobs (backup.sh, backup_verify.sh) record their own heartbeat with
// psql, and only on genuine success -- for the backup that means the upload
// completed, not merely that a dump was written. That distinction is the whole
// point: the local dump was always fine while the off-site copy did nothing.

// StaleJob is a job that has not reported success recently enough.
type StaleJob struct {
	Name  string
	Age   time.Duration
	Limit time.Duration
	Never bool // never recorded a success at all
}

func (s StaleJob) String() string {
	if s.Never {
		return fmt.Sprintf("%s has never reported a successful run", s.Name)
	}
	return fmt.Sprintf("%s last succeeded %s ago (expected within %s)",
		s.Name, s.Age.Round(time.Minute), s.Limit.Round(time.Minute))
}

// RecordJobSuccess stamps a job as having completed. Failures are logged, never
// returned: a heartbeat write must not be able to break the job it observes.
func RecordJobSuccess(db *DB, name, detail string) {
	if _, err := db.Exec(`INSERT INTO job_heartbeats(name,last_success_at,detail) VALUES(?,NOW(),?)
		ON CONFLICT (name) DO UPDATE SET last_success_at=NOW(), detail=EXCLUDED.detail`, name, detail); err != nil {
		core.Logger.Warn("heartbeat write failed", "err", err, "job", name)
	}
}

// StaleJobs returns every job in `limits` whose last success is older than its
// limit, plus any that has never reported one. A job absent from `limits` is
// not checked -- the caller owns the expectations.
//
// `since` is when this process started. A job that has NEVER reported is only
// stale once it has had a full interval to run: on a cold start nothing has
// reported yet, and a nightly backup legitimately has no heartbeat for hours
// after a deploy. Without this the first health check after every deploy
// alerts on every job at once -- which it did, in production, immediately.
func StaleJobs(db *DB, limits map[string]time.Duration, since time.Time) []StaleJob {
	seen := map[string]time.Time{}
	rows, err := db.Query(`SELECT name, last_success_at FROM job_heartbeats`)
	if err != nil {
		core.Logger.Error("heartbeat read failed", "err", err)
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var at time.Time
		if rows.Scan(&name, &at) == nil {
			seen[name] = at
		}
	}

	var out []StaleJob
	for name, limit := range limits {
		at, ok := seen[name]
		if !ok {
			// Not yet had a chance to run since this process started.
			if time.Since(since) < limit {
				continue
			}
			out = append(out, StaleJob{Name: name, Limit: limit, Never: true})
			continue
		}
		if age := time.Since(at); age > limit {
			out = append(out, StaleJob{Name: name, Age: age, Limit: limit})
		}
	}
	return out
}
