package core

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"hash/fnv"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// MaskNRIC returns the PII-safe display form of a Malaysian IC / NRIC:
// the first part is replaced with asterisks and only the last 4 digits
// are visible. Empty input returns empty. Used in list/snapshot responses
// where the full value isn't required.
//
//	"901231-10-1234" → "************1234"
//	"901231101234"   → "********1234"
//	""               → ""
func MaskNRIC(nric string) string {
	if len(nric) <= 4 {
		return nric
	}
	masked := ""
	for i := 0; i < len(nric)-4; i++ {
		masked += "*"
	}
	return masked + nric[len(nric)-4:]
}

// AdvisoryLockKey hashes a label into the int8 Postgres pg_advisory_*_lock
// expects. FNV-1a is fast and avalanche-good enough that two distinct labels
// collide rarely in practice; collisions only serialise unrelated work, they
// don't corrupt anything.
func AdvisoryLockKey(label string) int64 {
	h := fnv.New64a()
	h.Write([]byte(label))
	return int64(h.Sum64())
}

// ── helpers ───────────────────────────────────────────────────────────────────

func Respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// RespondError writes a JSON error envelope including the request ID, which
// is read back from the response header (set by the requestID middleware).
// Including the ID lets support correlate "I got an error at 3pm" with the
// matching server log line in seconds.
func RespondError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{
		"error":      msg,
		"request_id": w.Header().Get("X-Request-Id"),
	})
}

func Today() string { return time.Now().Format("2006-01-02") }

// ParseDayName maps a classes.day value ("Monday") to time.Weekday's int
// (Sunday=0), or -1 when unrecognised.
func ParseDayName(name string) int {
	days := map[string]int{"sunday": 0, "monday": 1, "tuesday": 2, "wednesday": 3, "thursday": 4, "friday": 5, "saturday": 6}
	if d, ok := days[strings.ToLower(strings.TrimSpace(name))]; ok {
		return d
	}
	return -1
}

// HolidayCovers is THE holiday range predicate (F4) -- every consumer, Go or
// SQL-adjacent, must use it so billing and display can never disagree.
// Kept in sync with App.Utils.holidayCovers in frontend/js/utils.js.
// An empty or malformed endDate (before date) means a single-day holiday.
// Lexical compares are safe: all dates are TEXT YYYY-MM-DD.
func HolidayCovers(date, endDate, day string) bool {
	if endDate != "" && endDate >= date {
		return day >= date && day <= endDate
	}
	return day == date
}

// ValidationError returns a comma-joined list of missing/invalid field names, or "".
func ValidationError(checks ...string) string {
	var errs []string
	for i := 0; i+1 < len(checks); i += 2 {
		if strings.TrimSpace(checks[i+1]) == "" {
			errs = append(errs, checks[i])
		}
	}
	if len(errs) == 0 {
		return ""
	}
	return "missing required fields: " + strings.Join(errs, ", ")
}

// ValidAmount returns true if amount is a positive number.
func ValidAmount(a float64) bool { return a > 0 }

// Execer is satisfied by both *DB and *Tx, allowing LogAudit to work
// inside or outside a transaction.
type Execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// LogAudit inserts an audit_logs row and logs any error via slog.
// This replaces all bare db.Exec audit inserts — a failed audit write
// should never crash the request, but it must never be silently swallowed.
//
// tenantID stamps the row so audit trails stay tenant-isolated: HandleAuditLogs
// filters by tenant, so a row written without it would default to tenant 1 and
// leak across tenants (or vanish for tenant 2). Callers pass store.TenantID(c)
// for request-scoped actions, or the tenant of the affected row for system /
// webhook / pre-auth actions.
func LogAudit(db Execer, tenantID int, actorEmail, action, entityType, entityID, detail string) {
	if _, err := db.Exec(
		`INSERT INTO audit_logs(tenant_id,actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?,?)`,
		tenantID, actorEmail, action, entityType, entityID, detail,
	); err != nil {
		Logger.Error("audit log write failed", "err", err, "action", action, "entity_type", entityType, "entity_id", entityID)
	}
}

// ── Pagination ───────────────────────────────────────────────────────────────

type Pagination struct {
	Limit  int
	Offset int
	Active bool
}

type PaginatedResponse struct {
	Data   any `json:"data"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

func ParsePagination(r *http.Request) Pagination {
	q := r.URL.Query()
	ls, os := q.Get("limit"), q.Get("offset")
	if ls == "" && os == "" {
		return Pagination{}
	}
	p := Pagination{Active: true, Limit: 50, Offset: 0}
	if ls != "" {
		if v, err := strconv.Atoi(ls); err == nil && v > 0 {
			if v > 500 {
				v = 500
			}
			p.Limit = v
		}
	}
	if os != "" {
		if v, err := strconv.Atoi(os); err == nil && v >= 0 {
			p.Offset = v
		}
	}
	return p
}

// GenerateID returns a sortable, collision-resistant ID: a millisecond
// timestamp prefix (so IDs stay roughly time-ordered) plus a crypto-random
// suffix so two IDs minted in the same millisecond — e.g. inside the monthly
// invoice / payroll cron loops — never collide and silently drop a row.
func GenerateID(prefix string) string {
	ts := strings.ReplaceAll(time.Now().Format("20060102150405.000"), ".", "")
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		Logger.Error("GenerateID: crypto/rand failed", "err", err)
		return prefix + "_" + ts + strconv.FormatInt(time.Now().UnixNano()%1e6, 10)
	}
	return prefix + "_" + ts + hex.EncodeToString(b)
}

// NewReferralCode returns a short shareable code like "SH-7K3X".
// Excludes ambiguous characters (0/O, 1/I/L) so it's easy to read off WhatsApp.
func NewReferralCode() string {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// Fall back to time-based — extremely unlikely path.
		return "SH-" + strings.ToUpper(strings.ReplaceAll(time.Now().Format("0405.000"), ".", ""))[:4]
	}
	out := make([]byte, 4)
	for i, v := range b {
		out[i] = alphabet[int(v)%len(alphabet)]
	}
	return "SH-" + string(out)
}

// AppEnv returns the current environment ("development", "staging",
// "production", etc.). Defaults to "development" so local dev never needs
// the variable set.
func AppEnv() string {
	if v := os.Getenv("APP_ENV"); v != "" {
		return v
	}
	return "development"
}

// BuildVersion is overridden at link time via -ldflags="-X ...core.BuildVersion=..."
// during Docker builds. Defaults to "dev" so local builds always work.
var BuildVersion = "dev"

// MaxBodySize limits request body size globally (1MB). File upload has its own
// 5MB limit.
func MaxBodySize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" || r.Method == "PUT" {
			// Skip file upload endpoint (has its own limit)
			if r.URL.Path != "/api/upload-proof" {
				r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
			}
		}
		next.ServeHTTP(w, r)
	})
}

// CurrentToSVersion is the version of the Terms of Service the app currently
// requires. Bump this any time the ToS text materially changes; every user
// with a lower tos_accepted_version gets re-prompted on next login.
const CurrentToSVersion = 1
