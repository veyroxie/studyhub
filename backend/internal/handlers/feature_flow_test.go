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
		"invoices", "referral_rewards", "email_tokens",
		"announcements", "registrations", "students", "families",
		"classes", "staff", "users",
	}
	for _, tbl := range tables {
		db.Exec("DELETE FROM " + tbl)
	}
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
		r.Get("/api/families", HandleFamilies(db))
		r.Post("/api/families", HandleFamilies(db))
		r.Get("/api/families/{id}/referral", HandleFamilyReferral(db))
		r.Get("/api/invoices", HandleInvoices(db))
		r.Post("/api/invoices", HandleInvoices(db))
		r.Put("/api/invoices/{id}/pay", HandleInvoicePay(db))
		r.Get("/api/attendance", HandleAttendance(db, hub))
		r.Post("/api/attendance", HandleAttendance(db, hub))
		r.Get("/api/referrals", HandleReferrals(db))
		r.Post("/api/referrals/{id}/earn", HandleReferralEarn(db))
		r.Post("/api/referrals/{id}/consume", HandleReferralConsume(db))
	})

	return r, db, func() { db.Close() }
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
		w := authedJSON(t, r, "POST", "/api/invoices", tok, map[string]any{
			"studentId":   stuID,
			"description": fmt.Sprintf("Month %d", i+1),
			"type":        "Monthly",
			"amount":      300.00,
			"dueDate":     "2026-04-30",
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
