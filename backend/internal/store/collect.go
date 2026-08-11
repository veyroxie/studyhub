package store

import (
	"database/sql"

	"studyhub/internal/core"
)

// CollectRows drains a list query into a slice, replacing the query-check /
// defer-Close / scan-loop block that was hand-copied at ~26 call sites.
//
// Beyond the duplication it closes two gaps every one of those copies shared:
// a failed Scan dropped the row with no signal at all, and rows.Err() was
// never checked anywhere in the codebase — so a connection dropped mid-
// iteration returned a short list indistinguishable from a complete one. On a
// billing or attendance list that is a silently wrong answer.
//
// kind names the model in log lines. Failures stay fail-soft (return what was
// read, log the rest) to match the behaviour callers already expect.
func CollectRows[T any](rows *sql.Rows, queryErr error, kind string, scan func(*sql.Rows) (T, error)) []T {
	out := []T{}
	if queryErr != nil {
		core.Logger.Error("list query failed", "err", queryErr, "type", kind)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			core.Logger.Warn("list row dropped — scan failed", "err", err, "type", kind)
			continue
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		core.Logger.Error("list truncated — row iteration failed", "err", err, "type", kind)
	}
	return out
}
