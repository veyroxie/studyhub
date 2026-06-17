package store

import (
	"encoding/json"

	"studyhub/internal/core"
	"studyhub/internal/models"
)

// ParentClassIDs returns the set of class IDs any of a parent's children are
// enrolled in. Used to scope announcements + feedback to a parent's own
// classes. Lives in store because both the snapshot queries and the feedback
// handlers need it.
func ParentClassIDs(db *DB, c *core.Claims) map[string]bool {
	if c == nil {
		return map[string]bool{}
	}
	tw, twArgs := ScopeTenant(c, "")
	args := append([]any{c.Email}, twArgs...)
	rows, err := db.Query(`SELECT enrolled_classes FROM students WHERE contact=? AND deleted_at IS NULL`+tw, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	ids := map[string]bool{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		var arr []string
		json.Unmarshal([]byte(raw), &arr)
		for _, id := range arr {
			ids[id] = true
		}
	}
	return ids
}

// AnnounceVisibilityClause returns a WHERE-clause fragment + args that hide
// drafts and pending-approval announcements from non-staff callers, plus
// restrict the audience pool to "all" / "parents" for parents. Staff
// (admin, superadmin, teacher) get the unfiltered set.
func AnnounceVisibilityClause(c *core.Claims) (string, []any) {
	if c != nil && (c.Role == "admin" || c.Role == "superadmin" || c.Role == "teacher") {
		return "", nil
	}
	// Parents see broadcast announcements ('all', 'parents') AND targeted
	// class announcements ('class:<id>') for classes any of their children
	// is enrolled in. Per-class targeting is used by the cancelled-class
	// auto-announcement so the affected parents actually get notified.
	// The class-id allow-list is built per-request below so each parent
	// only sees their own children's classes.
	return ` AND COALESCE(status,'published')='published' AND (audience IN ('all','parents') OR audience LIKE 'class:%')`, nil
}

// ParentAnnouncementFilter post-filters the broad SQL result to drop
// class-targeted rows whose class is not in the parent's enrollment set.
// We can't easily filter in SQL because enrolled_classes is JSON text on
// students, but the row count for a parent is small so this is cheap.
func ParentAnnouncementFilter(rows []models.Announcement, classIDs map[string]bool) []models.Announcement {
	out := make([]models.Announcement, 0, len(rows))
	for _, a := range rows {
		if len(a.Audience) > 6 && a.Audience[:6] == "class:" {
			if !classIDs[a.Audience[6:]] {
				continue
			}
		}
		out = append(out, a)
	}
	return out
}
