package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
)

// ── Cancelled Classes ─────────────────────────────────────────────────────────

func listCancelledClasses(db *store.DB, c *core.Claims) []models.CancelledClass {
	tw, twArgs := store.ScopeTenant(c, "")
	rows, err := db.Query(`SELECT id,class_id,date,reason,cancelled_by,created_on FROM cancelled_classes WHERE deleted_at IS NULL`+tw+` ORDER BY date DESC LIMIT 5000`, twArgs...)
	return store.CollectRows(rows, err, "CancelledClass", func(r *sql.Rows) (models.CancelledClass, error) {
		var cc models.CancelledClass
		err := r.Scan(&cc.ID, &cc.ClassID, &cc.Date, &cc.Reason, &cc.CancelledBy, &cc.CreatedOn)
		return cc, err
	})
}

func HandleListCancelledClasses(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		core.Respond(w, listCancelledClasses(db, c))
	}
}

func HandleCreateCancelledClass(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		var cc models.CancelledClass
		if err := json.NewDecoder(r.Body).Decode(&cc); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		if msg := validationError("classId", cc.ClassID, "date", cc.Date); msg != "" {
			core.RespondError(w, msg, 400)
			return
		}
		if cc.ID == "" {
			cc.ID = core.GenerateID("CC")
		}
		if cc.CreatedOn == "" {
			cc.CreatedOn = core.Today()
		}
		if cc.CancelledBy == "" && c != nil {
			cc.CancelledBy = c.Email
		}
		tid := store.TenantID(c)
		var moved int
		db.QueryRow(`SELECT COUNT(*) FROM class_session_moves WHERE tenant_id=? AND class_id=? AND from_date=? AND deleted_at IS NULL`, tid, cc.ClassID, cc.Date).Scan(&moved)
		if moved > 0 {
			core.RespondError(w, "session was rescheduled to another date — undo the move first", http.StatusConflict)
			return
		}
		res, err := db.Exec(`INSERT INTO cancelled_classes(id,tenant_id,class_id,date,reason,cancelled_by,created_on) VALUES(?,?,?,?,?,?,?)
			ON CONFLICT (tenant_id, class_id, date) WHERE deleted_at IS NULL DO NOTHING`,
			cc.ID, tid, cc.ClassID, cc.Date, cc.Reason, cc.CancelledBy, cc.CreatedOn)
		if err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		// Idempotency: a duplicate POST must not re-announce or re-grant
		// credits — before 0044 every retry double-granted every student.
		if n, _ := res.RowsAffected(); n == 0 {
			core.RespondError(w, "this session is already cancelled", http.StatusConflict)
			return
		}
		// Side-effects: announce the cancellation to parents and grant each
		// enrolled student a class replacement credit. Both run in the same
		// request handler (not async) so the admin sees them in the next
		// snapshot reload — a teacher missing class without these would
		// silently bill a missed lesson.
		applyCancelledClassSideEffects(db, c, cc, tid)

		if c != nil {
			core.LogAudit(db, store.TenantID(c), c.Email, "class_cancelled", "cancelled_class", cc.ID, "class="+cc.ClassID+" date="+cc.Date)
		}
		w.WriteHeader(http.StatusCreated)
		core.Respond(w, cc)
	}
}

// applyCancelledClassSideEffects fans out the announce + credit-grant work
// triggered by a class cancellation. Errors are logged but not surfaced —
// the cancellation row is already committed and the admin will see the
// outcome in the next snapshot.
func applyCancelledClassSideEffects(db *store.DB, c *core.Claims, cc models.CancelledClass, tid int) {
	var className, classStart, classEnd string
	db.QueryRow(`SELECT name, COALESCE(time,''), COALESCE(end_time,'') FROM classes WHERE id=? AND tenant_id=?`, cc.ClassID, tid).Scan(&className, &classStart, &classEnd)
	if className == "" {
		className = cc.ClassID
	}
	credits := creditsForDuration(classStart, classEnd)

	// Announcement — created as published, audience scoped to the class so
	// only enrolled parents get the notification.
	annID := core.GenerateID("ANN")
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
		annID, tid, title, message, "class:"+cc.ClassID, "Cancellation", core.Today(), actor, "published",
	); err != nil {
		core.Logger.Error("cancellation announcement insert failed", "err", err, "class_id", cc.ClassID)
	} else if c != nil {
		// Mirror the direct-create audit hook so "list every announcement
		// mutation" queries don't skip the auto-generated ones.
		core.LogAudit(db, store.TenantID(c), c.Email, "announcement_created", "announcement", annID, "auto: class cancellation "+cc.ClassID)
	}

	// Replacement credits — the class's duration in 15-min credits, per
	// enrolled student. We rely on the JSON-string LIKE match used elsewhere
	// in the codebase; enrolled_classes is TEXT '["<id>",...]'.
	rows, err := db.Query(
		`SELECT id FROM students WHERE tenant_id=? AND deleted_at IS NULL AND enrolled_classes LIKE '%"'||?||'"%'`,
		tid, cc.ClassID,
	)
	if err != nil {
		core.Logger.Error("cancellation enrolled-student lookup failed", "err", err, "class_id", cc.ClassID)
		return
	}
	defer rows.Close()
	note := "Class cancelled on " + cc.Date
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			continue
		}
		rcID := core.GenerateID("RC")
		if _, err := db.Exec(
			`INSERT INTO replacement_credits(id,tenant_id,student_id,type,minutes,note,class_id,date,created_by,category) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			rcID, tid, sid, "earned", credits, note, cc.ClassID, cc.Date, actor, "class",
		); err != nil {
			core.Logger.Error("cancellation credit insert failed", "err", err, "student_id", sid)
		}
	}
}

func HandleDeleteCancelledClass(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tid := store.TenantID(c)
		var classID, date string
		if err := db.QueryRow(`SELECT class_id, date FROM cancelled_classes WHERE id=? AND tenant_id=? AND deleted_at IS NULL`, id, tid).Scan(&classID, &date); err != nil {
			core.RespondError(w, "not found", http.StatusNotFound)
			return
		}
		if _, err := db.Exec(`UPDATE cancelled_classes SET deleted_at=NOW() WHERE id=? AND tenant_id=?`, id, tid); err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		// Claw back the grants this cancellation created. A credit already
		// spent leaves the ledger negative — visible, and right for undo-a-mistake.
		if _, err := db.Exec(`DELETE FROM replacement_credits WHERE tenant_id=? AND class_id=? AND date=? AND type='earned' AND category='class' AND note=?`,
			tid, classID, date, "Class cancelled on "+date); err != nil {
			core.Logger.Error("cancellation credit claw-back failed", "err", err, "class_id", classID)
		}
		var className string
		db.QueryRow(`SELECT name FROM classes WHERE id=? AND tenant_id=?`, classID, tid).Scan(&className)
		if className == "" {
			className = classID
		}
		announceMove(db, c, tid, className, classID, "Cancellation",
			"Class back on — "+className,
			className+" on "+date+" will run as scheduled after all; the cancellation has been reversed and the make-up credit added for it has been removed.")
		if c != nil {
			core.LogAudit(db, tid, c.Email, "cancellation_undone", "cancelled_class", id, "class="+classID+" date="+date)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// creditsForDuration converts a class's HH:MM start/end into replacement
// credits at the agreed unit of 1 credit = 15 minutes (a 1-hour class = 4).
// Unparsable or missing times fall back to 4, the standard 1-hour class.
func creditsForDuration(start, end string) int {
	s, errS := time.Parse("15:04", start)
	e, errE := time.Parse("15:04", end)
	if errS != nil || errE != nil {
		return 4
	}
	mins := int(e.Sub(s).Minutes())
	if mins < 15 {
		return 4
	}
	return mins / 15
}
