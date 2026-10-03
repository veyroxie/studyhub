package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"studyhub/internal/models"
	"studyhub/internal/store"
)

// allowClashParam is how an admin says "add it anyway": some rooms are shared on
// purpose (Self-Study runs beside lessons), so a clash is a warning, not a rule.
const allowClashParam = "allowClash"

func clashOverridden(r *http.Request) bool {
	return r.URL.Query().Get(allowClashParam) == "1"
}

// classClash describes the first booking cl's slot overlaps, room first then each
// teacher, or "" when the slot is free. Intervals [s1,e1) and [s2,e2) overlap when
// s1<e2 AND s2<e1. tw scopes to the caller's tenant.
func classClash(db *store.DB, tw string, twArgs []any, cl models.Class) (string, error) {
	if cl.Classroom != "" {
		name, start, end, err := firstOverlap(db, `classroom=?`, cl.Classroom, tw, twArgs, cl)
		if err != nil || name != "" {
			return roomClashText(cl, name, start, end), err
		}
	}
	for _, teacherID := range cl.TeacherIDs {
		name, start, end, err := firstOverlap(db, `teacher_ids LIKE '%"'||?||'"%'`, teacherID, tw, twArgs, cl)
		if err != nil || name != "" {
			return teacherClashText(db, teacherID, name, start, end), err
		}
	}
	return "", nil
}

func firstOverlap(db *store.DB, match string, value string, tw string, twArgs []any, cl models.Class) (name, start, end string, err error) {
	args := append([]any{cl.Day, cl.ID, cl.EndTime, cl.Time, value}, twArgs...)
	err = db.QueryRow(`SELECT name, time, end_time FROM classes WHERE day=? AND id!=? AND time<? AND end_time>? AND `+match+` AND deleted_at IS NULL`+tw+` ORDER BY time LIMIT 1`,
		args...).Scan(&name, &start, &end)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", nil
	}
	if err != nil {
		return "", "", "", fmt.Errorf("class clash check: %w", err)
	}
	return name, start, end, nil
}

func roomClashText(cl models.Class, name, start, end string) string {
	if name == "" {
		return ""
	}
	return fmt.Sprintf("%s already has %s on %s, %s to %s", cl.Classroom, name, cl.Day, start, end)
}

func teacherClashText(db *store.DB, teacherID, name, start, end string) string {
	if name == "" {
		return ""
	}
	teacher := teacherID
	db.QueryRow(`SELECT COALESCE(NULLIF(full_name,''), name) FROM staff WHERE id=?`, teacherID).Scan(&teacher)
	return fmt.Sprintf("%s already teaches %s then, %s to %s", teacher, name, start, end)
}
