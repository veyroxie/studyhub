package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/jobs"
	"studyhub/internal/mailer"
	"studyhub/internal/store"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

// setupFeatureTestApp wires the routes needed by the billing/attendance/
// referral/family flow tests. It's a separate helper from setupTestApp and
// setupEmailTestApp because each route group has different middleware needs
// and we don't want one to leak into the others.
func setupFeatureTestApp(t *testing.T) (*chi.Mux, *store.DB, func()) {
	t.Helper()
	dsn := testDSN()
	db := store.InitDB(dsn)

	// Reset every table the feature tests touch.
	tables := []string{
		"replacement_credits", "audit_logs", "feedback", "attendance",
		"invoices", "referral_rewards", "email_tokens", "enrollments",
		"cancelled_classes", "class_session_moves", "class_schedule_history", "class_schedule_versions",
		"announcements", "registrations", "students", "families",
		"classes", "staff", "users",
	}
	for _, tbl := range tables {
		db.Exec("DELETE FROM " + tbl)
	}
	// Demo data is opt-in (SEED_DEMO); the tests want it.
	t.Setenv("SEED_DEMO_DATA", "1")
	jobs.SeedIfEmpty(db)

	t.Setenv("RESEND_API_KEY", "")
	core.InitLogger()
	mailer.Init()

	hub := NewHub()
	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE"},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
	}))
	r.Use(core.RequestID)

	// Public
	r.Post("/api/auth/login", auth.HandleLogin(db))
	r.Post("/api/register", HandleRegister(db))

	// Authenticated
	r.Group(func(r chi.Router) {
		r.Use(auth.JWTMiddleware(db))
		r.Get("/api/snapshot", HandleSnapshot(db))
		r.Get("/api/students", HandleStudents(db))
		r.Post("/api/students", HandleStudents(db))
		r.Put("/api/students/{id}", HandleStudent(db))
		r.Delete("/api/students/{id}", HandleStudent(db))
		r.Get("/api/families", HandleFamilies(db))
		r.Post("/api/families", HandleFamilies(db))
		r.Get("/api/families/{id}/referral", HandleFamilyReferral(db))
		r.Get("/api/invoices", HandleInvoices(db))
		r.Post("/api/invoices", HandleInvoices(db))
		r.Put("/api/invoices/{id}/pay", HandleInvoicePay(db))
		r.Get("/api/attendance", HandleAttendance(db, hub))
		r.Post("/api/attendance", HandleAttendance(db, hub))
		r.Delete("/api/attendance/{id}", HandleDeleteAttendance(db))
		r.Get("/api/referrals", HandleReferrals(db))
		r.Post("/api/referrals/{id}/earn", HandleReferralEarn(db))
		r.Post("/api/referrals/{id}/consume", HandleReferralConsume(db))
	})

	return r, db, func() { db.Close() }
}

// countRows runs a COUNT and fails the test if the query or scan errors.
// The bare form leaves the destination at its PREVIOUS value on failure, so a
// reused counter silently asserts against stale data -- an assertion that
// cannot fail correctly. Mirrors store.CountRow on the production side.
func countRows(t *testing.T, db *store.DB, query string, args ...any) int {
	t.Helper()
	n, err := store.CountRow(db, query, args...)
	if err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	return n
}

// authedJSON makes an authenticated JSON request and returns the recorder.
// `tok` is the JWT obtained from getToken().
func authedJSON(t *testing.T, r *chi.Mux, method, path, tok string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(b))
	} else {
		bodyReader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ── Failed login lockout ────────────────────────────────────────────────────

func TestLogin_LockoutAfter5FailedAttempts(t *testing.T) {
	r, _, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	body := map[string]string{
		"email":    "admin@studyhub.com",
		"password": "wrongpassword",
	}

	// 5 wrong attempts: each returns 401, the 5th locks the account.
	for i := 0; i < 5; i++ {
		w := post(t, r, "/api/auth/login", body)
		expected := http.StatusUnauthorized
		if i == 4 {
			expected = http.StatusForbidden // lock kicks in on the 5th
		}
		if w.Code != expected {
			t.Fatalf("attempt %d: expected %d got %d (%s)", i+1, expected, w.Code, w.Body.String())
		}
	}

	// 6th attempt with the CORRECT password should still be locked.
	w := post(t, r, "/api/auth/login", map[string]string{
		"email":    "admin@studyhub.com",
		"password": "admin123",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("locked account should reject correct password too: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(strings.ToLower(w.Body.String()), "lock") {
		t.Fatalf("expected lock-related error message, got %s", w.Body.String())
	}
}

func TestLogin_SuccessfulLoginResetsFailureCounter(t *testing.T) {
	r, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	// Two wrong attempts, then a successful one.
	for i := 0; i < 2; i++ {
		post(t, r, "/api/auth/login", map[string]string{
			"email":    "admin@studyhub.com",
			"password": "wrong",
		})
	}
	w := post(t, r, "/api/auth/login", map[string]string{
		"email":    "admin@studyhub.com",
		"password": "admin123",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("login should succeed: %d %s", w.Code, w.Body.String())
	}

	// Counter should be reset to 0.
	var count int
	db.QueryRow(`SELECT COALESCE(failed_login_count,0) FROM users WHERE email=?`, "admin@studyhub.com").Scan(&count)
	if count != 0 {
		t.Fatalf("expected failed_login_count=0 after successful login, got %d", count)
	}
}

// ── Family auto-creation on student add ─────────────────────────────────────

func TestFamilies_AutoCreatedOnStudentAdd(t *testing.T) {
	r, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	tok := getToken(t, r, "admin@studyhub.com", "admin123")
	w := authedJSON(t, r, "POST", "/api/students", tok, map[string]any{
		"firstName":  "Auto",
		"lastName":   "Family",
		"contact":    "autofamily@example.com",
		"parentName": "Auto Parent",
		"phone":      "60123456789",
		"branch":     "The Study Hub",
	})
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("create student failed: %d %s", w.Code, w.Body.String())
	}

	// A family row should now exist for this contact email.
	var famID, refCode string
	if err := db.QueryRow(`SELECT id, COALESCE(referral_code,'') FROM families WHERE contact=?`, "autofamily@example.com").Scan(&famID, &refCode); err != nil {
		t.Fatalf("family not auto-created: %v", err)
	}
	if famID == "" {
		t.Fatal("empty family id")
	}
	if refCode == "" {
		t.Fatal("auto-created family should have a referral code")
	}
}

func TestFamilies_ReferralEndpoint_ReturnsCode(t *testing.T) {
	r, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	// Seed a family directly so we can fetch it.
	db.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name,referral_code) VALUES(?,?,?,?,?,?,?)`,
		"FAM_test", 1, "Test Family", "famref@example.com", "", "Test Parent", "SH-TEST")

	tok := getToken(t, r, "admin@studyhub.com", "admin123")
	w := authedJSON(t, r, "GET", "/api/families/FAM_test/referral", tok, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("family referral endpoint failed: %d %s", w.Code, w.Body.String())
	}
	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	if body["referralCode"] != "SH-TEST" {
		t.Fatalf("expected referralCode=SH-TEST, got %v", body["referralCode"])
	}
}

// ── Invoice CRUD + mark paid + soft delete ──────────────────────────────────

func TestInvoices_CreateMarkPaidAndPersist(t *testing.T) {
	r, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	tok := getToken(t, r, "admin@studyhub.com", "admin123")

	// Find a seeded student to invoice.
	var studentID string
	db.QueryRow(`SELECT id FROM students LIMIT 1`).Scan(&studentID)
	if studentID == "" {
		t.Skip("no seeded students available")
	}

	// Create the invoice.
	w := authedJSON(t, r, "POST", "/api/invoices", tok, map[string]any{
		"studentId":   studentID,
		"description": "April Tuition",
		"type":        "Monthly",
		"amount":      350.00,
		"dueDate":     "2026-04-30",
		"status":      "Unpaid",
	})
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("create invoice failed: %d %s", w.Code, w.Body.String())
	}

	// Find the persisted row by description (server assigns its own ID).
	var invID string
	if err := db.QueryRow(`SELECT id FROM invoices WHERE description=? AND deleted_at IS NULL ORDER BY created_on DESC LIMIT 1`, "April Tuition").Scan(&invID); err != nil {
		t.Fatalf("invoice not persisted to DB: %v", err)
	}

	// Mark it paid.
	w = authedJSON(t, r, "PUT", "/api/invoices/"+invID+"/pay", tok, map[string]string{
		"paymentMethod": "Bank Transfer",
		"referenceNo":   "TXN-TEST-12345",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("mark paid failed: %d %s", w.Code, w.Body.String())
	}

	// Verify status flipped.
	var status, method string
	db.QueryRow(`SELECT status, COALESCE(payment_method,'') FROM invoices WHERE id=?`, invID).Scan(&status, &method)
	if status != "Paid" {
		t.Fatalf("expected status=Paid, got %q", status)
	}
	if method != "Bank Transfer" {
		t.Fatalf("expected method=Bank Transfer, got %q", method)
	}
}

func TestInvoices_NegativeAmount_Rejected_Feature(t *testing.T) {
	r, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	tok := getToken(t, r, "admin@studyhub.com", "admin123")
	var studentID string
	db.QueryRow(`SELECT id FROM students LIMIT 1`).Scan(&studentID)
	if studentID == "" {
		t.Skip("no seeded students")
	}

	w := authedJSON(t, r, "POST", "/api/invoices", tok, map[string]any{
		"studentId":   studentID,
		"description": "Bad invoice",
		"type":        "Monthly",
		"amount":      -100.00,
		"dueDate":     "2026-04-30",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative amount, got %d", w.Code)
	}
}

// ── Attendance check-in ─────────────────────────────────────────────────────

func TestAttendance_CheckInPersists(t *testing.T) {
	r, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	tok := getToken(t, r, "admin@studyhub.com", "admin123")
	var studentID string
	db.QueryRow(`SELECT id FROM students LIMIT 1`).Scan(&studentID)
	if studentID == "" {
		t.Skip("no seeded students")
	}

	now := time.Now().Format("15:04")
	today := time.Now().Format("2006-01-02")
	w := authedJSON(t, r, "POST", "/api/attendance", tok, map[string]any{
		"personId":   studentID,
		"personType": "student",
		"date":       today,
		"checkIn":    now,
		"status":     "Present",
	})
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("check-in failed: %d %s", w.Code, w.Body.String())
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM attendance WHERE person_id=? AND date=?`, studentID, today).Scan(&count)
	if count == 0 {
		t.Fatal("attendance not persisted")
	}
}

// ── Referral milestone full lifecycle ───────────────────────────────────────

// TestReferralMilestone_FullLifecycle exercises the entire referral system
// in one test:
//
//  1. Create referrer family with a known referral_code
//  2. Create referred student linked back via referred_by_family_id
//  3. Create + pay 3 monthly invoices for the referred student
//  4. Assert the referral_rewards row is now status='earned' with
//     credits_remaining=3 (the inline referralCheckMilestoneOnPay hook)
//  5. Consume one credit and assert credits_remaining=2
func TestReferralMilestone_FullLifecycle(t *testing.T) {
	r, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	tok := getToken(t, r, "admin@studyhub.com", "admin123")

	// Step 1: referrer family
	db.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name,referral_code) VALUES(?,?,?,?,?,?,?)`,
		"FAM_referrer", 1, "Referrer Family", "referrer@example.com", "", "Referrer Parent", "SH-REFER")

	// Step 2: referred student + a referral_rewards row in pending state
	stuID := "STU_referral_test"
	db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,parent_name,contact,branch,status,registered_on,referred_by_family_id) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		stuID, 1, "Referred", "Kid", "Other Parent", "other@example.com", "The Study Hub", "Active", core.Today(), "FAM_referrer")
	db.Exec(`INSERT INTO referral_rewards(id,tenant_id,referrer_family_id,referred_student_id,status) VALUES(?,?,?,?,'pending')`,
		"REF_test", 1, "FAM_referrer", stuID)

	// Step 3: create + pay 3 monthly invoices for the referred student.
	for i := 0; i < 3; i++ {
		// Distinct issue dates: these are three consecutive months, and one
		// monthly invoice per student per month is now enforced by the database
		// (migration 0039). Without this they all defaulted to today.
		w := authedJSON(t, r, "POST", "/api/invoices", tok, map[string]any{
			"studentId":   stuID,
			"description": fmt.Sprintf("Month %d", i+1),
			"type":        "Monthly",
			"amount":      300.00,
			"createdOn":   fmt.Sprintf("2026-%02d-01", i+1),
			"dueDate":     fmt.Sprintf("2026-%02d-07", i+1),
			"status":      "Unpaid",
		})
		if w.Code != http.StatusOK && w.Code != http.StatusCreated {
			t.Fatalf("invoice %d create failed: %d %s", i+1, w.Code, w.Body.String())
		}
		var invID string
		db.QueryRow(`SELECT id FROM invoices WHERE student_id=? AND description=? ORDER BY created_on DESC LIMIT 1`, stuID, fmt.Sprintf("Month %d", i+1)).Scan(&invID)
		if invID == "" {
			t.Fatalf("invoice %d not persisted", i+1)
		}
		w = authedJSON(t, r, "PUT", "/api/invoices/"+invID+"/pay", tok, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("mark paid %d failed: %d %s", i+1, w.Code, w.Body.String())
		}
	}

	// Step 4: assert milestone has been auto-detected by handleInvoicePay.
	var status string
	var creditsRemaining, paidCount int
	db.QueryRow(`SELECT status, credits_remaining, paid_invoice_count FROM referral_rewards WHERE id=?`, "REF_test").Scan(&status, &creditsRemaining, &paidCount)
	if status != "earned" {
		t.Fatalf("expected status=earned after 3 paid invoices, got %q (paid_count=%d)", status, paidCount)
	}
	if creditsRemaining != 3 {
		t.Fatalf("expected credits_remaining=3, got %d", creditsRemaining)
	}

	// Step 5: consume one credit via the API and assert it decremented.
	w := authedJSON(t, r, "POST", "/api/referrals/REF_test/consume", tok, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("consume failed: %d %s", w.Code, w.Body.String())
	}
	db.QueryRow(`SELECT credits_remaining, status FROM referral_rewards WHERE id=?`, "REF_test").Scan(&creditsRemaining, &status)
	if creditsRemaining != 2 {
		t.Fatalf("expected credits_remaining=2 after consume, got %d", creditsRemaining)
	}
	if status != "earned" {
		t.Fatalf("expected status to remain earned, got %q", status)
	}
}

func TestReferralMilestone_PendingBelowThreshold(t *testing.T) {
	r, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	tok := getToken(t, r, "admin@studyhub.com", "admin123")

	db.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name,referral_code) VALUES(?,?,?,?,?,?,?)`,
		"FAM_pending", 1, "Pending Family", "pendingref@example.com", "", "Parent", "SH-PEND")
	stuID := "STU_pending_referral"
	db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,parent_name,contact,branch,status,registered_on,referred_by_family_id) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		stuID, 1, "Pending", "Kid", "Other", "otherp@example.com", "The Study Hub", "Active", core.Today(), "FAM_pending")
	db.Exec(`INSERT INTO referral_rewards(id,tenant_id,referrer_family_id,referred_student_id,status) VALUES(?,?,?,?,'pending')`,
		"REF_pending", 1, "FAM_pending", stuID)

	// Pay only 2 invoices — below threshold.
	for i := 0; i < 2; i++ {
		authedJSON(t, r, "POST", "/api/invoices", tok, map[string]any{
			"studentId":   stuID,
			"description": fmt.Sprintf("M%d", i),
			"type":        "Monthly",
			"amount":      200.00,
			"dueDate":     "2026-04-30",
		})
		var invID string
		db.QueryRow(`SELECT id FROM invoices WHERE student_id=? AND description=? ORDER BY created_on DESC LIMIT 1`, stuID, fmt.Sprintf("M%d", i)).Scan(&invID)
		authedJSON(t, r, "PUT", "/api/invoices/"+invID+"/pay", tok, nil)
	}

	// Status should still be pending.
	var status string
	db.QueryRow(`SELECT status FROM referral_rewards WHERE id=?`, "REF_pending").Scan(&status)
	if status != "pending" {
		t.Fatalf("expected status=pending below threshold, got %q", status)
	}
}

func TestReferralMilestone_ConsumeWithoutEarned_Returns400(t *testing.T) {
	r, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	tok := getToken(t, r, "admin@studyhub.com", "admin123")
	db.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name,referral_code) VALUES(?,?,?,?,?,?,?)`,
		"FAM_noconsume", 1, "F", "nc@example.com", "", "P", "SH-NOPE")
	db.Exec(`INSERT INTO referral_rewards(id,tenant_id,referrer_family_id,referred_student_id,status) VALUES(?,?,?,?,'pending')`,
		"REF_noconsume", 1, "FAM_noconsume", "STU_doesnotexist")

	w := authedJSON(t, r, "POST", "/api/referrals/REF_noconsume/consume", tok, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 consuming non-earned reward, got %d %s", w.Code, w.Body.String())
	}
}

// TestAttendance_UndoDeletesRow locks the undo endpoint: admins can remove a
// mis-tapped record entirely; parents cannot.
func TestAttendance_UndoDeletesRow(t *testing.T) {
	r, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	tok := getToken(t, r, "admin@studyhub.com", "admin123")
	var studentID string
	db.QueryRow(`SELECT id FROM students LIMIT 1`).Scan(&studentID)
	if studentID == "" {
		t.Skip("no seeded students")
	}
	today := time.Now().Format("2006-01-02")
	w := authedJSON(t, r, "POST", "/api/attendance", tok, map[string]any{
		"personId": studentID, "personType": "student", "date": today,
		"checkIn": time.Now().Format("15:04"), "status": "Present",
	})
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("check-in failed: %d %s", w.Code, w.Body.String())
	}
	var recID string
	db.QueryRow(`SELECT id FROM attendance WHERE person_id=? AND date=? LIMIT 1`, studentID, today).Scan(&recID)
	if recID == "" {
		t.Fatal("attendance row not found after check-in")
	}

	parentTok := getToken(t, r, "seeduser27@example.com", "parent123")
	if w := authedJSON(t, r, "DELETE", "/api/attendance/"+recID, parentTok, nil); w.Code != http.StatusForbidden {
		t.Fatalf("parent delete should be 403, got %d", w.Code)
	}

	if w := authedJSON(t, r, "DELETE", "/api/attendance/"+recID, tok, nil); w.Code != http.StatusNoContent {
		t.Fatalf("admin delete failed: %d %s", w.Code, w.Body.String())
	}
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM attendance WHERE id=?`, recID).Scan(&count)
	if count != 0 {
		t.Fatal("attendance row still present after undo")
	}
	if w := authedJSON(t, r, "DELETE", "/api/attendance/"+recID, tok, nil); w.Code != http.StatusNotFound {
		t.Fatalf("second delete should be 404, got %d", w.Code)
	}
}

// TestSessionMove_FullLifecycle: a move notifies parents via a class-scoped
// announcement, grants NO credits, upserts on re-move, refuses a cancelled
// session, and undoing announces the reversal.
func TestSessionMove_FullLifecycle(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	r := chi.NewRouter()
	r.Post("/api/auth/login", auth.HandleLogin(db))
	r.Group(func(g chi.Router) {
		g.Use(auth.JWTMiddleware(db))
		g.Post("/api/session-moves", HandleCreateSessionMove(db))
		g.Delete("/api/session-moves/{id}", HandleDeleteSessionMove(db))
	})
	tok := getToken(t, r, "admin@studyhub.com", "admin123")

	classID := core.GenerateID("CLS")
	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time) VALUES(?,?,?,?,?,?)`,
		classID, 1, "Move Test Class", "Monday", "10:00", "11:00")
	from := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	to := time.Now().AddDate(0, 0, 9).Format("2006-01-02")

	var creditsBefore int
	db.QueryRow(`SELECT COUNT(*) FROM replacement_credits`).Scan(&creditsBefore)

	w := authedJSON(t, r, "POST", "/api/session-moves", tok, map[string]any{
		"classId": classID, "fromDate": from, "toDate": to, "reason": "Holiday make-up",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("move failed: %d %s", w.Code, w.Body.String())
	}
	var moveID string
	db.QueryRow(`SELECT id FROM class_session_moves WHERE class_id=? AND from_date=? AND deleted_at IS NULL`, classID, from).Scan(&moveID)
	if moveID == "" {
		t.Fatal("move row missing")
	}

	var annCount int
	db.QueryRow(`SELECT COUNT(*) FROM announcements WHERE audience=? AND type='Reschedule'`, "class:"+classID).Scan(&annCount)
	if annCount != 1 {
		t.Fatalf("expected 1 reschedule announcement, got %d", annCount)
	}
	var creditsAfter int
	db.QueryRow(`SELECT COUNT(*) FROM replacement_credits`).Scan(&creditsAfter)
	if creditsAfter != creditsBefore {
		t.Fatal("a move must not grant credits")
	}

	// Re-moving the same session replaces the target, no duplicate row.
	to2 := time.Now().AddDate(0, 0, 10).Format("2006-01-02")
	if w := authedJSON(t, r, "POST", "/api/session-moves", tok, map[string]any{
		"classId": classID, "fromDate": from, "toDate": to2,
	}); w.Code != http.StatusCreated {
		t.Fatalf("re-move failed: %d %s", w.Code, w.Body.String())
	}
	var live int
	db.QueryRow(`SELECT COUNT(*) FROM class_session_moves WHERE class_id=? AND from_date=? AND deleted_at IS NULL`, classID, from).Scan(&live)
	if live != 1 {
		t.Fatalf("expected 1 live move after upsert, got %d", live)
	}
	var gotTo string
	db.QueryRow(`SELECT to_date FROM class_session_moves WHERE class_id=? AND from_date=? AND deleted_at IS NULL`, classID, from).Scan(&gotTo)
	if gotTo != to2 {
		t.Fatalf("upsert did not replace target: %s", gotTo)
	}

	// A cancelled session cannot be rescheduled.
	cancelledDate := time.Now().AddDate(0, 0, 14).Format("2006-01-02")
	db.Exec(`INSERT INTO cancelled_classes(id,tenant_id,class_id,date,reason,cancelled_by,created_on) VALUES(?,?,?,?,?,?,?)`,
		core.GenerateID("CC"), 1, classID, cancelledDate, "test", "test", core.Today())
	if w := authedJSON(t, r, "POST", "/api/session-moves", tok, map[string]any{
		"classId": classID, "fromDate": cancelledDate, "toDate": to,
	}); w.Code != http.StatusConflict {
		t.Fatalf("moving a cancelled session should be 409, got %d", w.Code)
	}

	// Undo soft-deletes and announces the reversal.
	if w := authedJSON(t, r, "DELETE", "/api/session-moves/"+moveID, tok, nil); w.Code != http.StatusNoContent {
		t.Fatalf("undo failed: %d %s", w.Code, w.Body.String())
	}
	db.QueryRow(`SELECT COUNT(*) FROM class_session_moves WHERE class_id=? AND from_date=? AND deleted_at IS NULL`, classID, from).Scan(&live)
	if live != 0 {
		t.Fatal("move still live after undo")
	}
	db.QueryRow(`SELECT COUNT(*) FROM announcements WHERE audience=? AND type='Reschedule'`, "class:"+classID).Scan(&annCount)
	if annCount != 3 {
		t.Fatalf("expected 3 reschedule announcements (move, re-move, undo), got %d", annCount)
	}
}

// TestCancellation_IdempotentAndUndoable locks the F3 hardening: a duplicate
// cancel POST must not re-grant credits (409), a cancellation can be undone
// (credits clawed back, parents notified), and a moved session refuses to be
// cancelled until the move is undone.
func TestCancellation_IdempotentAndUndoable(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	r := chi.NewRouter()
	r.Post("/api/auth/login", auth.HandleLogin(db))
	r.Group(func(g chi.Router) {
		g.Use(auth.JWTMiddleware(db))
		g.Post("/api/cancelled-classes", HandleCreateCancelledClass(db))
		g.Delete("/api/cancelled-classes/{id}", HandleDeleteCancelledClass(db))
	})
	tok := getToken(t, r, "admin@studyhub.com", "admin123")

	classID := core.GenerateID("CLS")
	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time) VALUES(?,?,?,?,?,?)`,
		classID, 1, "Cancel Test Class", "Monday", "10:00", "11:00")
	stuID := core.GenerateID("STU")
	db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,contact,enrolled_classes) VALUES(?,?,?,?,?,?)`,
		stuID, 1, "Cancel", "Kid", "cancelkid@example.com", `["`+classID+`"]`)
	date := time.Now().AddDate(0, 0, 7).Format("2006-01-02")

	credits := func() int {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM replacement_credits WHERE student_id=?`, stuID).Scan(&n)
		return n
	}
	announcements := func() int {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM announcements WHERE audience=?`, "class:"+classID).Scan(&n)
		return n
	}

	w := authedJSON(t, r, "POST", "/api/cancelled-classes", tok, map[string]any{"classId": classID, "date": date})
	if w.Code != http.StatusCreated {
		t.Fatalf("cancel failed: %d %s", w.Code, w.Body.String())
	}
	var cc struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &cc)
	if credits() != 1 || announcements() != 1 {
		t.Fatalf("after cancel: want 1 credit + 1 announcement, got %d + %d", credits(), announcements())
	}

	// Duplicate POST: 409, and neither credits nor announcements grow.
	if w := authedJSON(t, r, "POST", "/api/cancelled-classes", tok, map[string]any{"classId": classID, "date": date}); w.Code != http.StatusConflict {
		t.Fatalf("duplicate cancel should be 409, got %d %s", w.Code, w.Body.String())
	}
	if credits() != 1 || announcements() != 1 {
		t.Fatalf("duplicate cancel re-granted: %d credits, %d announcements", credits(), announcements())
	}

	// Undo: credits clawed back, reversal announced, row soft-deleted.
	if w := authedJSON(t, r, "DELETE", "/api/cancelled-classes/"+cc.ID, tok, nil); w.Code != http.StatusNoContent {
		t.Fatalf("undo failed: %d %s", w.Code, w.Body.String())
	}
	if credits() != 0 {
		t.Fatalf("undo should claw back the credit, got %d", credits())
	}
	if announcements() != 2 {
		t.Fatalf("undo should announce the reversal, got %d announcements", announcements())
	}
	if w := authedJSON(t, r, "DELETE", "/api/cancelled-classes/"+cc.ID, tok, nil); w.Code != http.StatusNotFound {
		t.Fatalf("second undo should be 404, got %d", w.Code)
	}

	// After the undo the same date can be cancelled again (partial index).
	if w := authedJSON(t, r, "POST", "/api/cancelled-classes", tok, map[string]any{"classId": classID, "date": date}); w.Code != http.StatusCreated {
		t.Fatalf("re-cancel after undo should succeed, got %d %s", w.Code, w.Body.String())
	}

	// A session moved away cannot be cancelled until the move is undone.
	movedDate := time.Now().AddDate(0, 0, 14).Format("2006-01-02")
	db.Exec(`INSERT INTO class_session_moves(id,tenant_id,class_id,from_date,to_date,created_on) VALUES(?,?,?,?,?,?)`,
		core.GenerateID("MOV"), 1, classID, movedDate, time.Now().AddDate(0, 0, 16).Format("2006-01-02"), core.Today())
	if w := authedJSON(t, r, "POST", "/api/cancelled-classes", tok, map[string]any{"classId": classID, "date": movedDate}); w.Code != http.StatusConflict {
		t.Fatalf("cancelling a moved session should be 409, got %d %s", w.Code, w.Body.String())
	}
}

// TestJobHeartbeats_CatchAJobThatWentQuiet locks the 0048 mechanism that
// replaces per-symptom monitoring.
//
// Two 2026-09-01 outages were invisible for months: the nightly backup ran
// while its upload did nothing, and WebSocket upgrades failed continuously.
// Neither had a check, because checks were written per known symptom. A
// heartbeat inverts that — a job that stops reporting is caught whether or not
// anyone predicted that particular failure.
func TestJobHeartbeats_CatchAJobThatWentQuiet(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()
	db.Exec(`DELETE FROM job_heartbeats`)

	limits := map[string]time.Duration{
		"fresh-job":  time.Hour,
		"quiet-job":  time.Hour,
		"absent-job": time.Hour,
	}

	store.RecordJobSuccess(db, "fresh-job", "just ran")
	store.RecordJobSuccess(db, "quiet-job", "ran, then stopped")
	// Backdate the quiet one past its limit.
	if _, err := db.Exec(`UPDATE job_heartbeats SET last_success_at = NOW() - INTERVAL '5 hours' WHERE name='quiet-job'`); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	stale := map[string]store.StaleJob{}
	for _, sj := range store.StaleJobs(db, limits) {
		stale[sj.Name] = sj
	}

	if _, ok := stale["fresh-job"]; ok {
		t.Error("a job that reported within its limit must not alert")
	}
	if sj, ok := stale["quiet-job"]; !ok {
		t.Error("a job that stopped reporting must alert")
	} else if sj.Never {
		t.Error("quiet-job did report once; it should read as stale, not never-run")
	}
	// A job that never ran at all is the case a freshness check misses
	// entirely — there is no old record to look stale.
	if sj, ok := stale["absent-job"]; !ok {
		t.Error("a job that has NEVER reported must alert, not be silently absent")
	} else if !sj.Never {
		t.Error("absent-job should be reported as never-run")
	}

	// Recording again clears it, so a recovered job stops alerting.
	store.RecordJobSuccess(db, "quiet-job", "recovered")
	for _, sj := range store.StaleJobs(db, limits) {
		if sj.Name == "quiet-job" {
			t.Error("a job that resumed must stop alerting")
		}
	}
}
