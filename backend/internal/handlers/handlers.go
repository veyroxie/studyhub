package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"hash/fnv"
	"net/http"
	"strconv"
	"strings"
	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"
	"sync"
	"time"
)

// isAdminRole returns true for both "admin" and "superadmin" claims. Used by
// handlers that mutate tenant-level data — the bare check `c.Role != "admin"`
// would lock superadmins out of routine work like creating an invoice or
// editing a holiday, which was a recurring drift across handlers.
func isAdminRole(c *core.Claims) bool {
	if c == nil {
		return false
	}
	return c.Role == "admin" || c.Role == "superadmin"
}

// maskNRIC returns the PII-safe display form of a Malaysian IC / NRIC:
// the first part is replaced with asterisks and only the last 4 digits
// are visible. Empty input returns empty. Used in list/snapshot responses
// where the full value isn't required.
//
//	"901231-10-1234" → "************1234"
//	"901231101234"   → "********1234"
//	""               → ""
func maskNRIC(nric string) string {
	if len(nric) <= 4 {
		return nric
	}
	masked := ""
	for i := 0; i < len(nric)-4; i++ {
		masked += "*"
	}
	return masked + nric[len(nric)-4:]
}

// isStaffRole returns true for any role allowed to mutate teaching-side
// records: admin, superadmin, or teacher. Use this for endpoints like
// feedback / progress reports / replacement credits where teachers
// legitimately participate.
func isStaffRole(c *core.Claims) bool {
	if c == nil {
		return false
	}
	return c.Role == "admin" || c.Role == "superadmin" || c.Role == "teacher"
}

// advisoryLockKey hashes a label into the int8 Postgres pg_advisory_*_lock
// expects. FNV-1a is fast and avalanche-good enough that two distinct labels
// collide rarely in practice; collisions only serialise unrelated work, they
// don't corrupt anything.
func advisoryLockKey(label string) int64 {
	h := fnv.New64a()
	h.Write([]byte(label))
	return int64(h.Sum64())
}

// ── helpers ───────────────────────────────────────────────────────────────────

func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// respondError writes a JSON error envelope including the request ID, which
// is read back from the response header (set by the requestID middleware).
// Including the ID lets support correlate "I got an error at 3pm" with the
// matching server log line in seconds.
func respondError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{
		"error":      msg,
		"request_id": w.Header().Get("X-Request-Id"),
	})
}

func today() string { return time.Now().Format("2006-01-02") }

// validationError returns a comma-joined list of missing/invalid field names, or "".
func validationError(checks ...string) string {
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

// validAmount returns true if amount is a positive number.
func validAmount(a float64) bool { return a > 0 }

// execer is satisfied by both *DB and *Tx, allowing logAudit to work
// inside or outside a transaction.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// logAudit inserts an audit_logs row and logs any error via slog.
// This replaces all bare db.Exec audit inserts — a failed audit write
// should never crash the request, but it must never be silently swallowed.
func logAudit(db execer, actorEmail, action, entityType, entityID, detail string) {
	if _, err := db.Exec(
		`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
		actorEmail, action, entityType, entityID, detail,
	); err != nil {
		core.Logger.Error("audit log write failed", "err", err, "action", action, "entity_type", entityType, "entity_id", entityID)
	}
}

// listPendingUsers returns users with status=pending_verification.
// Admin-only — included in the snapshot for dashboard attention items.
func listPendingUsers(db *store.DB, c *core.Claims) []models.PendingUser {
	tw, twArgs := store.ScopeTenant(c, "")
	rows, err := db.Query(`SELECT id, email, name, role FROM users WHERE status='pending_verification'`+tw+` ORDER BY id DESC`, twArgs...)
	if err != nil {
		return []models.PendingUser{}
	}
	defer rows.Close()
	out := []models.PendingUser{}
	for rows.Next() {
		var u models.PendingUser
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			continue
		}
		out = append(out, u)
	}
	return out
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

func parsePagination(r *http.Request) core.Pagination {
	q := r.URL.Query()
	ls, os := q.Get("limit"), q.Get("offset")
	if ls == "" && os == "" {
		return core.Pagination{}
	}
	p := core.Pagination{Active: true, Limit: 50, Offset: 0}
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

func generateID(prefix string) string {
	return prefix + "_" + strings.ReplaceAll(time.Now().Format("20060102150405.000"), ".", "")
}

// newReferralCode returns a short shareable code like "SH-7K3X".
// Excludes ambiguous characters (0/O, 1/I/L) so it's easy to read off WhatsApp.
func newReferralCode() string {
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

// ── Snapshot (full data load) ─────────────────────────────────────────────────

func HandleSnapshot(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		cacheKey := store.SnapshotCacheKey(c)
		if body, ok := store.SnapshotCacheGet(cacheKey); ok {
			store.WriteCachedSnapshot(w, r, body)
			return
		}
		// Singleflight: if another goroutine is already building the same
		// snapshot, wait for its result instead of running the fan-out
		// queries again. Solves the "10 tabs auto-refresh on TTL expiry"
		// thundering-herd that previously hammered Postgres.
		flight, isLeader := store.SnapshotSingleflight(cacheKey)
		if !isLeader {
			if body, ok := flight.Wait(); ok {
				store.WriteCachedSnapshot(w, r, body)
				return
			}
			// Leader failed — fall through and try ourselves.
		}
		isAdmin := c != nil && (c.Role == "admin" || c.Role == "superadmin")
		isParent := c != nil && c.Role != "admin" && c.Role != "superadmin" && c.Role != "teacher"

		var snap models.Snapshot
		var wg sync.WaitGroup

		run := func(fn func()) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				fn()
			}()
		}

		// Snapshot bounds: for tables that grow linearly with time, only
		// load a recent window. A dashboard never renders 5 years of
		// attendance at once — the per-endpoint paginated GETs serve deep
		// history. These bounds keep the snapshot constant-size as the
		// centre ages.
		run(func() { snap.Students = listStudents(db, c) })
		run(func() { snap.Classes = listClasses(db, c) })
		run(func() { snap.Staff = listStaff(db, c) })
		run(func() { snap.Invoices = store.ListInvoicesRecent(db, c) })
		run(func() { snap.Announcements = store.ListAnnouncementsRecent(db, c) })
		run(func() { snap.Attendance = store.ListAttendanceRecent(db, c) })
		run(func() { snap.Payroll = listPayroll(db, c) })
		run(func() { snap.Feedback = store.ListFeedbackRecent(db, c) })
		run(func() { snap.Workshops = listWorkshops(db, c) })
		run(func() { snap.PricingTiers = listPricingTiers(db, c) })
		run(func() { snap.SelfStudySessions = store.ListSelfStudyRecent(db, c) })
		run(func() { snap.PerformanceReviews = listPerformanceReviews(db, c) })
		run(func() { snap.CancelledClasses = listCancelledClasses(db, c) })
		run(func() { snap.Holidays = listHolidays(db, c) })
		run(func() { snap.ReplacementCredits = listReplacementCredits(db, c) })
		run(func() { snap.Families = listFamilies(db, c) })
		run(func() { snap.FeedbackReplies = listFeedbackReplies(db, c) })
		run(func() { snap.ReferralRewards = listReferralRewards(db, c) })
		run(func() { snap.ProgressReports = listProgressReports(db, c) })
		if isAdmin {
			run(func() { snap.Registrations = listRegistrations(db, c) })
			run(func() { snap.PendingUsers = listPendingUsers(db, c) })
		} else if c != nil && c.Role == "parent" {
			// Parents see only their own enrollment requests so the dashboard
			// can show pending enrolments and the "register your child" form.
			run(func() { snap.Registrations = listParentEnrollments(db, c) })
		}

		wg.Wait()

		// Parents: filter to their children's data only (post-load so the heavy
		// queries above can run in parallel).
		if isParent && c != nil {
			classIDs := store.ParentClassIDs(db, c)
			snap.Feedback = filterFeedbackForParent(snap.Feedback, classIDs)

			stuIDs := parentStudentIDs(db, c)
			filtered := []models.SelfStudySession{}
			for _, s := range snap.SelfStudySessions {
				if stuIDs[s.StudentID] {
					filtered = append(filtered, s)
				}
			}
			snap.SelfStudySessions = filtered

			// Hide internal performance reviews from parents
			snap.PerformanceReviews = []models.PerformanceReview{}
		}
		body, err := json.Marshal(snap)
		if err != nil {
			store.SnapshotSingleflightDone(cacheKey, nil, err)
			core.RespondError(w, "snapshot serialization failed", http.StatusInternalServerError)
			return
		}
		store.SnapshotCachePut(cacheKey, body)
		// Publish to any followers waiting on the singleflight.
		store.SnapshotSingleflightDone(cacheKey, body, nil)
		store.WriteCachedSnapshot(w, r, body)
	}
}
