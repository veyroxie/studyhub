package main

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
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
		isParent := c != nil && c.Role != "admin" && c.Role != "superadmin" && c.Role != "teacher"

		feedback := listFeedback(db, c)
		selfStudy := listSelfStudy(db, c)
		perfReviews := listPerformanceReviews(db, c)

		// Parents: filter to their children's data only
		if isParent {
			classIDs := parentClassIDs(db, c.Email)
			feedback = filterFeedbackForParent(feedback, classIDs)

			stuIDs := parentStudentIDs(db, c.Email)
			filtered := []SelfStudySession{}
			for _, s := range selfStudy {
				if stuIDs[s.StudentID] {
					filtered = append(filtered, s)
				}
			}
			selfStudy = filtered

			// Hide internal performance reviews from parents
			perfReviews = []PerformanceReview{}
		}

		snap := Snapshot{
			Students:           listStudents(db, c),
			Classes:            listClasses(db, c),
			Staff:              listStaff(db, c),
			Invoices:           listInvoices(db, c),
			Announcements:      listAnnouncements(db, c),
			Attendance:         listAttendance(db, c),
			Payroll:            listPayroll(db, c),
			Feedback:           feedback,
			Subjects:           listSubjects(db, c),
			Workshops:          listWorkshops(db, c),
			SelfStudySessions:  selfStudy,
			PerformanceReviews: perfReviews,
			CancelledClasses:   listCancelledClasses(db, c),
			Holidays:           listHolidays(db, c),
			ReplacementCredits: listReplacementCredits(db, c),
			Families:           listFamilies(db, c),
			FeedbackReplies:    listFeedbackReplies(db, c),
			ReferralRewards:    listReferralRewards(db, c),
		}
		if c != nil && (c.Role == "admin" || c.Role == "superadmin") {
			snap.Registrations = listRegistrations(db, c)
		} else if c != nil && c.Role == "parent" {
			// Parents see only their own enrollment requests so the dashboard
			// can show pending enrolments and the "register your child" form.
			snap.Registrations = listParentEnrollments(db, c)
		}
		respond(w, snap)
	}
}
