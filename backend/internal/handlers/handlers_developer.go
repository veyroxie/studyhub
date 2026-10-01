package handlers

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"studyhub/internal/core"
	"studyhub/internal/mailer"
	"studyhub/internal/store"
)

// Developer screens: technical history for whoever maintains StudyHub. Every
// route here sits behind auth.RequireDeveloper and only reads.

const (
	devAuditDefaultLimit = 200
	devAuditMaxLimit     = 500
	devFailureLimit      = 50
	devDateLayout        = "2006-01-02"
)

type devAuditEntry struct {
	ID         int       `json:"id"`
	ActorEmail string    `json:"actorEmail"`
	Action     string    `json:"action"`
	EntityType string    `json:"entityType"`
	EntityID   string    `json:"entityId"`
	Detail     string    `json:"detail"`
	CreatedAt  time.Time `json:"createdAt"`
}

type devAuditFilter struct {
	query, action string
	from, until   *time.Time // until is exclusive: the day after the "to" date
	limit         int
}

// parseDevAuditFilter reads ?q=&action=&from=YYYY-MM-DD&to=YYYY-MM-DD&limit=; the message names a bad value.
func parseDevAuditFilter(v url.Values) (devAuditFilter, string) {
	f := devAuditFilter{query: strings.TrimSpace(v.Get("q")), action: strings.TrimSpace(v.Get("action")), limit: devAuditDefaultLimit}
	if n, err := strconv.Atoi(v.Get("limit")); err == nil && n > 0 {
		f.limit = min(n, devAuditMaxLimit)
	}
	var ok bool
	if f.from, ok = parseDevDate(v.Get("from")); !ok {
		return f, "from must be YYYY-MM-DD"
	}
	if f.until, ok = parseDevDate(v.Get("to")); !ok {
		return f, "to must be YYYY-MM-DD"
	}
	if f.until != nil {
		next := f.until.AddDate(0, 0, 1)
		f.until = &next
	}
	return f, ""
}

// parseDevDate reads a local calendar date; blank means no bound.
func parseDevDate(s string) (*time.Time, bool) {
	if s == "" {
		return nil, true
	}
	t, err := time.ParseInLocation(devDateLayout, s, time.Local)
	if err != nil {
		return nil, false
	}
	return &t, true
}

func (f devAuditFilter) where() (string, []any) {
	clause, args := "", []any{}
	if f.query != "" {
		clause += ` AND (actor_email ILIKE ? OR entity_id ILIKE ? OR COALESCE(detail,'') ILIKE ?)`
		pattern := "%" + likeEscape(f.query) + "%"
		args = append(args, pattern, pattern, pattern)
	}
	if f.action != "" {
		clause += ` AND action=?`
		args = append(args, f.action)
	}
	if f.from != nil {
		clause += ` AND created_at >= ?`
		args = append(args, *f.from)
	}
	if f.until != nil {
		clause += ` AND created_at < ?`
		args = append(args, *f.until)
	}
	return clause, args
}

// likeEscape makes user text match literally inside a LIKE pattern (backslash is Postgres's default escape).
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// GET /api/dev/audit-logs
func HandleDevAuditLogs(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		f, msg := parseDevAuditFilter(r.URL.Query())
		if msg != "" {
			core.RespondError(w, msg, http.StatusBadRequest)
			return
		}
		tw, twArgs := store.ScopeTenant(c, "")
		where, whereArgs := f.where()
		args := append(append(append([]any{}, twArgs...), whereArgs...), f.limit)
		entries, err := scanDevAudit(db, `SELECT id,actor_email,action,entity_type,entity_id,COALESCE(detail,''),created_at FROM audit_logs WHERE 1=1`+tw+where+` ORDER BY created_at DESC, id DESC LIMIT ?`, args)
		if err != nil {
			core.LogFromReq(r).Error("dev audit query failed", "err", err)
			core.RespondError(w, "could not read the audit log", http.StatusInternalServerError)
			return
		}
		core.Respond(w, map[string]any{"entries": entries, "actions": devAuditActions(db, tw, twArgs)})
	}
}

func scanDevAudit(db *store.DB, query string, args []any) ([]devAuditEntry, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []devAuditEntry{}
	for rows.Next() {
		var e devAuditEntry
		if err := rows.Scan(&e.ID, &e.ActorEmail, &e.Action, &e.EntityType, &e.EntityID, &e.Detail, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// devAuditActions feeds the action filter; a failure only empties the dropdown.
func devAuditActions(db *store.DB, tw string, twArgs []any) []string {
	out := []string{}
	rows, err := db.Query(`SELECT DISTINCT action FROM audit_logs WHERE 1=1`+tw+` ORDER BY action`, twArgs...)
	if err != nil {
		core.Logger.Error("dev audit actions query failed", "err", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var a string
		if rows.Scan(&a) == nil {
			out = append(out, a)
		}
	}
	return out
}

type devFailedEmail struct {
	ID        int       `json:"id"`
	To        string    `json:"to"`
	Subject   string    `json:"subject"`
	Attempts  int       `json:"attempts"`
	LastError string    `json:"lastError"`
	CreatedAt time.Time `json:"createdAt"`
}

type devStuckJob struct {
	ID        int64     `json:"id"`
	Topic     string    `json:"topic"`
	Attempts  int       `json:"attempts"`
	LastError string    `json:"lastError"`
	CreatedAt time.Time `json:"createdAt"`
}

// GET /api/dev/failures: emails nothing will retry, and outbox jobs that keep failing.
// A stuck outbox job is an email that was never even queued, so it belongs beside them.
func HandleDevFailures(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tw, twArgs := store.ScopeTenant(core.ClaimsFrom(r), "")
		emails, err := devFailedEmails(db, tw, twArgs)
		if err != nil {
			core.LogFromReq(r).Error("dev failed emails query failed", "err", err)
			core.RespondError(w, "could not read the email queue", http.StatusInternalServerError)
			return
		}
		jobs, err := devStuckJobs(db, tw, twArgs)
		if err != nil {
			core.LogFromReq(r).Error("dev stuck jobs query failed", "err", err)
			core.RespondError(w, "could not read the outbox", http.StatusInternalServerError)
			return
		}
		core.Respond(w, map[string]any{"emails": emails, "jobs": jobs})
	}
}

func devFailedEmails(db *store.DB, tw string, twArgs []any) ([]devFailedEmail, error) {
	args := append(append([]any{}, twArgs...), devFailureLimit)
	rows, err := db.Query(`SELECT id,to_email,subject,attempts,COALESCE(last_error,''),created_at FROM email_queue WHERE status='failed'`+tw+` ORDER BY created_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []devFailedEmail{}
	for rows.Next() {
		var e devFailedEmail
		if err := rows.Scan(&e.ID, &e.To, &e.Subject, &e.Attempts, &e.LastError, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func devStuckJobs(db *store.DB, tw string, twArgs []any) ([]devStuckJob, error) {
	args := append(append([]any{}, twArgs...), devFailureLimit)
	rows, err := db.Query(`SELECT id,topic,attempts,last_error,created_at FROM outbox WHERE processed_at IS NULL AND last_error<>''`+tw+` ORDER BY created_at LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []devStuckJob{}
	for rows.Next() {
		var j devStuckJob
		if err := rows.Scan(&j.ID, &j.Topic, &j.Attempts, &j.LastError, &j.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// GET /api/dev/health: what this server is running with. Booleans and counts only;
// no secret, address or key ever leaves here.
func HandleDevHealth(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var migrations int
		var latest string
		var appliedAt *time.Time
		if err := db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(version),'') FROM schema_migrations`).Scan(&migrations, &latest); err != nil {
			core.LogFromReq(r).Error("dev health migrations query failed", "err", err)
		}
		if latest != "" {
			db.QueryRow(`SELECT applied_at FROM schema_migrations WHERE version=?`, latest).Scan(&appliedAt)
		}
		core.Respond(w, map[string]any{
			"version":        core.BuildVersion,
			"env":            core.AppEnv(),
			"uptimeSec":      int(time.Since(core.BootTime).Seconds()),
			"dbOK":           db.Ping() == nil,
			"jobsEnabled":    core.JobsEnabled(),
			"migrations":     map[string]any{"count": migrations, "latest": latest, "appliedAt": appliedAt},
			"emailLive":      mailer.IsLive(),
			"emailOnlyTo":    core.OutboundRestrictedTo(),
			"onlinePayments": onlinePaymentReady(),
			"emailQueue":     store.EmailQueueSummary(db),
		})
	}
}
