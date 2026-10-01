package store

import (
	"database/sql"
	"errors"
	"fmt"

	"studyhub/internal/core"
)

// SessionCredit is a replacement credit owed for one student missing one session.
type SessionCredit struct {
	TenantID  int
	StudentID string
	ClassID   string
	Date      string // the session's local YYYY-MM-DD
	Credits   int    // 15-minute units, stored in replacement_credits.minutes
	Note      string
	CreatedBy string
}

// GrantSessionCredit is THE way a missed session earns a credit: a cancellation, a
// teacher's "Absent + credit" and an approved absence report all come here, so a
// student is credited at most once per session. Reports false when one already exists.
// tx must be the caller's transaction: the advisory lock lasts until it ends.
func GrantSessionCredit(tx *Tx, g SessionCredit) (string, bool, error) {
	lock := core.AdvisoryLockKey(fmt.Sprintf("session-credit|%d|%s|%s|%s", g.TenantID, g.StudentID, g.ClassID, g.Date))
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(?)`, lock); err != nil {
		return "", false, fmt.Errorf("lock session credit: %w", err)
	}
	var existing string
	err := tx.QueryRow(`SELECT id FROM replacement_credits WHERE tenant_id=? AND student_id=? AND class_id=? AND date=? AND type='earned' AND category='class' LIMIT 1`,
		g.TenantID, g.StudentID, g.ClassID, g.Date).Scan(&existing)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("check session credit: %w", err)
	}
	id := core.GenerateID("RC")
	if _, err := tx.Exec(`INSERT INTO replacement_credits(id,tenant_id,student_id,type,minutes,note,class_id,date,created_by,category) VALUES(?,?,?,'earned',?,?,?,?,?,'class')`,
		id, g.TenantID, g.StudentID, g.Credits, g.Note, g.ClassID, g.Date, g.CreatedBy); err != nil {
		return "", false, fmt.Errorf("insert session credit: %w", err)
	}
	return id, true, nil
}
