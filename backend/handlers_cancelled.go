package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// ── Cancelled Classes ─────────────────────────────────────────────────────────

func listCancelledClasses(db *DB, c *Claims) []CancelledClass {
	tw, twArgs := scopeTenant(c, "")
	rows, err := db.Query(`SELECT id,class_id,date,reason,cancelled_by,created_on FROM cancelled_classes WHERE 1=1`+tw+` ORDER BY date DESC LIMIT 5000`, twArgs...)
	if err != nil {
		return []CancelledClass{}
	}
	defer rows.Close()
	out := []CancelledClass{}
	for rows.Next() {
		var cc CancelledClass
		if err := rows.Scan(&cc.ID, &cc.ClassID, &cc.Date, &cc.Reason, &cc.CancelledBy, &cc.CreatedOn); err != nil {
			continue
		}
		out = append(out, cc)
	}
	return out
}

func handleListCancelledClasses(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		respond(w, listCancelledClasses(db, c))
	}
}

func handleCreateCancelledClass(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if !isAdminRole(c) {
			respondError(w, "admin only", 403)
			return
		}
		var cc CancelledClass
		if err := json.NewDecoder(r.Body).Decode(&cc); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		if msg := validationError("classId", cc.ClassID, "date", cc.Date); msg != "" {
			respondError(w, msg, 400)
			return
		}
		if cc.ID == "" {
			cc.ID = generateID("CC")
		}
		if cc.CreatedOn == "" {
			cc.CreatedOn = today()
		}
		if cc.CancelledBy == "" && c != nil {
			cc.CancelledBy = c.Email
		}
		tid := tenantID(c)
		_, err := db.Exec(`INSERT INTO cancelled_classes(id,tenant_id,class_id,date,reason,cancelled_by,created_on) VALUES(?,?,?,?,?,?,?)`,
			cc.ID, tid, cc.ClassID, cc.Date, cc.Reason, cc.CancelledBy, cc.CreatedOn)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		// Side-effects: announce the cancellation to parents and grant each
		// enrolled student a class replacement credit. Both run in the same
		// request handler (not async) so the admin sees them in the next
		// snapshot reload — a teacher missing class without these would
		// silently bill a missed lesson.
		applyCancelledClassSideEffects(db, c, cc, tid)

		if c != nil {
			logAudit(db, c.Email, "class_cancelled", "cancelled_class", cc.ID, "class="+cc.ClassID+" date="+cc.Date)
		}
		w.WriteHeader(http.StatusCreated)
		respond(w, cc)
	}
}

// applyCancelledClassSideEffects fans out the announce + credit-grant work
// triggered by a class cancellation. Errors are logged but not surfaced —
// the cancellation row is already committed and the admin will see the
// outcome in the next snapshot.
func applyCancelledClassSideEffects(db *DB, c *Claims, cc CancelledClass, tid int) {
	var className string
	db.QueryRow(`SELECT name FROM classes WHERE id=? AND tenant_id=?`, cc.ClassID, tid).Scan(&className)
	if className == "" {
		className = cc.ClassID
	}

	// Announcement — created as published, audience scoped to the class so
	// only enrolled parents get the notification.
	annID := generateID("ANN")
	title := "Class cancelled — " + className
	message := className + " on " + cc.Date + " has been cancelled."
	if strings.TrimSpace(cc.Reason) != "" {
		message += " Reason: " + cc.Reason + "."
	}
	message += " A make-up credit has been added to your account."
	actor := ""
	if c != nil {
		actor = c.Email
	}
	if _, err := db.Exec(
		`INSERT INTO announcements(id,tenant_id,title,message,audience,type,created_on,created_by,status) VALUES(?,?,?,?,?,?,?,?,?)`,
		annID, tid, title, message, "class:"+cc.ClassID, "Cancellation", today(), actor, "published",
	); err != nil {
		logger.Error("cancellation announcement insert failed", "err", err, "class_id", cc.ClassID)
	} else if c != nil {
		// Mirror the direct-create audit hook so "list every announcement
		// mutation" queries don't skip the auto-generated ones.
		logAudit(db, c.Email, "announcement_created", "announcement", annID, "auto: class cancellation "+cc.ClassID)
	}

	// Replacement credits — one "class" credit per enrolled student. We
	// rely on the JSON-string LIKE match used elsewhere in the codebase;
	// the schema stores enrolled_classes as TEXT '["<id>",...]'.
	rows, err := db.Query(
		`SELECT id FROM students WHERE tenant_id=? AND deleted_at IS NULL AND enrolled_classes LIKE '%"'||?||'"%'`,
		tid, cc.ClassID,
	)
	if err != nil {
		logger.Error("cancellation enrolled-student lookup failed", "err", err, "class_id", cc.ClassID)
		return
	}
	defer rows.Close()
	note := "Class cancelled on " + cc.Date
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			continue
		}
		rcID := generateID("RC")
		if _, err := db.Exec(
			`INSERT INTO replacement_credits(id,tenant_id,student_id,type,minutes,note,class_id,date,created_by,category) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			rcID, tid, sid, "earned", 1, note, cc.ClassID, cc.Date, actor, "class",
		); err != nil {
			logger.Error("cancellation credit insert failed", "err", err, "student_id", sid)
		}
	}
}
