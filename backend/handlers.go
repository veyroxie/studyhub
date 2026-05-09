package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

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
		logger.Error("audit log write failed", "err", err, "action", action, "entity_type", entityType, "entity_id", entityID)
	}
}

// listPendingUsers returns users with status=pending_verification.
// Admin-only — included in the snapshot for dashboard attention items.
func listPendingUsers(db *DB) []PendingUser {
	rows, err := db.Query(`SELECT id, email, name, role FROM users WHERE status='pending_verification' ORDER BY id DESC`)
	if err != nil {
		return []PendingUser{}
	}
	defer rows.Close()
	out := []PendingUser{}
	for rows.Next() {
		var u PendingUser
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

func parsePagination(r *http.Request) Pagination {
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

func handleSnapshot(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		isAdmin := c != nil && (c.Role == "admin" || c.Role == "superadmin")
		isParent := c != nil && c.Role != "admin" && c.Role != "superadmin" && c.Role != "teacher"

		var snap Snapshot
		var wg sync.WaitGroup

		run := func(fn func()) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				fn()
			}()
		}

		run(func() { snap.Students = listStudents(db, c) })
		run(func() { snap.Classes = listClasses(db, c) })
		run(func() { snap.Staff = listStaff(db, c) })
		run(func() { snap.Invoices = listInvoices(db, c) })
		run(func() { snap.Announcements = listAnnouncements(db, c) })
		run(func() { snap.Attendance = listAttendance(db, c) })
		run(func() { snap.Payroll = listPayroll(db, c) })
		run(func() { snap.Feedback = listFeedback(db, c) })
		run(func() { snap.Workshops = listWorkshops(db, c) })
		run(func() { snap.SelfStudySessions = listSelfStudy(db, c) })
		run(func() { snap.PerformanceReviews = listPerformanceReviews(db, c) })
		run(func() { snap.CancelledClasses = listCancelledClasses(db, c) })
		run(func() { snap.Holidays = listHolidays(db, c) })
		run(func() { snap.ReplacementCredits = listReplacementCredits(db, c) })
		run(func() { snap.Families = listFamilies(db, c) })
		run(func() { snap.FeedbackReplies = listFeedbackReplies(db, c) })
		run(func() { snap.ReferralRewards = listReferralRewards(db, c) })
		if isAdmin {
			run(func() { snap.Registrations = listRegistrations(db, c) })
			run(func() { snap.PendingUsers = listPendingUsers(db) })
		} else if c != nil && c.Role == "parent" {
			// Parents see only their own enrollment requests so the dashboard
			// can show pending enrolments and the "register your child" form.
			run(func() { snap.Registrations = listParentEnrollments(db, c) })
		}

		wg.Wait()

		// Parents: filter to their children's data only (post-load so the heavy
		// queries above can run in parallel).
		if isParent && c != nil {
			classIDs := parentClassIDs(db, c.Email)
			snap.Feedback = filterFeedbackForParent(snap.Feedback, classIDs)

			stuIDs := parentStudentIDs(db, c.Email)
			filtered := []SelfStudySession{}
			for _, s := range snap.SelfStudySessions {
				if stuIDs[s.StudentID] {
					filtered = append(filtered, s)
				}
			}
			snap.SelfStudySessions = filtered

			// Hide internal performance reviews from parents
			snap.PerformanceReviews = []PerformanceReview{}
		}
		respond(w, snap)
	}
}
