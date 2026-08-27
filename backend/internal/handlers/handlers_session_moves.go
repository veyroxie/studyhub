package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
)

// ── Session moves ─────────────────────────────────────────────────────────────
// Reschedules ONE dated session of a recurring class to another date, for all
// students at once. The counterpart to cancel+credit: a move grants no
// credits because the class still happens. See migration 0042.

var isoDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func listSessionMoves(db *store.DB, c *core.Claims) []models.SessionMove {
	tw, twArgs := store.ScopeTenant(c, "")
	rows, err := db.Query(`SELECT id,class_id,from_date,to_date,reason,moved_by,created_on FROM class_session_moves WHERE deleted_at IS NULL`+tw+` ORDER BY from_date DESC LIMIT 5000`, twArgs...)
	return store.CollectRows(rows, err, "SessionMove", func(r *sql.Rows) (models.SessionMove, error) {
		var m models.SessionMove
		err := r.Scan(&m.ID, &m.ClassID, &m.FromDate, &m.ToDate, &m.Reason, &m.MovedBy, &m.CreatedOn)
		return m, err
	})
}

func HandleListSessionMoves(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		core.Respond(w, listSessionMoves(db, core.ClaimsFrom(r)))
	}
}

func HandleCreateSessionMove(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		var m models.SessionMove
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			core.RespondError(w, "bad body", http.StatusBadRequest)
			return
		}
		if msg := validationError("classId", m.ClassID, "fromDate", m.FromDate, "toDate", m.ToDate); msg != "" {
			core.RespondError(w, msg, http.StatusBadRequest)
			return
		}
		if !isoDate.MatchString(m.FromDate) || !isoDate.MatchString(m.ToDate) {
			core.RespondError(w, "dates must be YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		if m.FromDate == m.ToDate {
			core.RespondError(w, "the new date must differ from the original", http.StatusBadRequest)
			return
		}
		tid := store.TenantID(c)
		var className string
		if err := db.QueryRow(`SELECT name FROM classes WHERE id=? AND tenant_id=? AND deleted_at IS NULL`, m.ClassID, tid).Scan(&className); err != nil {
			core.RespondError(w, "class not found", http.StatusNotFound)
			return
		}
		// A cancelled session has already told parents "no class, credit
		// added" — silently turning that into a move would contradict it.
		var cancelled int
		db.QueryRow(`SELECT COUNT(*) FROM cancelled_classes WHERE tenant_id=? AND class_id=? AND date=?`, tid, m.ClassID, m.FromDate).Scan(&cancelled)
		if cancelled > 0 {
			core.RespondError(w, "this session is cancelled; a cancelled session cannot be rescheduled", http.StatusConflict)
			return
		}
		if m.ID == "" {
			m.ID = core.GenerateID("MOV")
		}
		m.CreatedOn = core.Today()
		if c != nil {
			m.MovedBy = c.Email
		}
		// Upsert: re-moving the same session replaces the previous target.
		_, err := db.Exec(`INSERT INTO class_session_moves(id,tenant_id,class_id,from_date,to_date,reason,moved_by,created_on)
			VALUES(?,?,?,?,?,?,?,?)
			ON CONFLICT (tenant_id,class_id,from_date) WHERE deleted_at IS NULL
			DO UPDATE SET to_date=EXCLUDED.to_date, reason=EXCLUDED.reason, moved_by=EXCLUDED.moved_by, created_on=EXCLUDED.created_on`,
			m.ID, tid, m.ClassID, m.FromDate, m.ToDate, m.Reason, m.MovedBy, m.CreatedOn)
		if err != nil {
			core.Logger.Error("session move insert failed", "err", err, "class_id", m.ClassID)
			core.RespondError(w, "server error", http.StatusInternalServerError)
			return
		}
		announceMove(db, c, tid, className, m.ClassID,
			"Class rescheduled — "+className,
			buildMoveMessage(className, m.FromDate, m.ToDate, m.Reason))
		core.LogAudit(db, tid, m.MovedBy, "session_moved", "session_move", m.ID, "class="+m.ClassID+" "+m.FromDate+" -> "+m.ToDate)
		w.WriteHeader(http.StatusCreated)
		core.Respond(w, m)
	}
}

func HandleDeleteSessionMove(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		id := chi.URLParam(r, "id")
		tid := store.TenantID(c)
		var classID, fromDate, toDate string
		if err := db.QueryRow(`SELECT class_id, from_date, to_date FROM class_session_moves WHERE id=? AND tenant_id=? AND deleted_at IS NULL`, id, tid).Scan(&classID, &fromDate, &toDate); err != nil {
			core.RespondError(w, "not found", http.StatusNotFound)
			return
		}
		if _, err := db.Exec(`UPDATE class_session_moves SET deleted_at=NOW() WHERE id=? AND tenant_id=?`, id, tid); err != nil {
			core.RespondError(w, "server error", http.StatusInternalServerError)
			return
		}
		var className string
		db.QueryRow(`SELECT name FROM classes WHERE id=? AND tenant_id=?`, classID, tid).Scan(&className)
		if className == "" {
			className = classID
		}
		announceMove(db, c, tid, className, classID,
			"Class back to schedule — "+className,
			className+" will run on "+fromDate+" as usual after all; the move to "+toDate+" is cancelled.")
		actor := ""
		if c != nil {
			actor = c.Email
		}
		core.LogAudit(db, tid, actor, "session_move_undone", "session_move", id, "class="+classID)
		w.WriteHeader(http.StatusNoContent)
	}
}

func buildMoveMessage(className, from, to, reason string) string {
	msg := className + " on " + from + " is rescheduled to " + to + "."
	if strings.TrimSpace(reason) != "" {
		msg += " Reason: " + reason + "."
	}
	return msg + " No action needed; the class runs as normal on the new date."
}

// announceMove mirrors the cancellation flow's parent notification: a
// published, class-scoped announcement that lands in enrolled parents'
// in-app notifications on their next load.
func announceMove(db *store.DB, c *core.Claims, tid int, className, classID, title, message string) {
	annID := core.GenerateID("ANN")
	actor := ""
	if c != nil {
		actor = c.Email
	}
	if _, err := db.Exec(
		`INSERT INTO announcements(id,tenant_id,title,message,audience,type,created_on,created_by,status) VALUES(?,?,?,?,?,?,?,?,?)`,
		annID, tid, title, message, "class:"+classID, "Reschedule", core.Today(), actor, "published",
	); err != nil {
		core.Logger.Error("session move announcement failed", "err", err, "class_id", classID)
		return
	}
	if c != nil {
		core.LogAudit(db, tid, c.Email, "announcement_created", "announcement", annID, "auto: session move "+classID)
	}
}
