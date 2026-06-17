package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/jobs"
	"studyhub/internal/mailer"
	"studyhub/internal/store"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

// setupEmailTestApp wires the routes that exercise the parent self-serve
// registration, email verification, password reset, and set-password flows.
// It's a separate setup helper from the legacy setupTestApp because those
// flows run on the public router (no JWT middleware) and need the mailer
// initialised in dev mode (logs to stdout, doesn't actually call Resend).
func setupEmailTestApp(t *testing.T) (*chi.Mux, *store.DB, func()) {
	t.Helper()
	dsn := testDSN()
	db := store.InitDB(dsn)

	// Reset every table the email flow touches so each test starts clean.
	tables := []string{
		"email_tokens", "audit_logs", "registrations", "users",
		"families", "students", "invoices", "referral_rewards",
	}
	for _, tbl := range tables {
		db.Exec("DELETE FROM " + tbl)
	}
	jobs.SeedIfEmpty(db)

	// Force dev-mode mailer so tests don't try to hit Resend.
	t.Setenv("RESEND_API_KEY", "")
	core.InitLogger()
	mailer.Init()

	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE"},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
	}))
	r.Use(core.RequestID)

	// Public endpoints — no auth required.
	r.Get("/api/health", HandleHealth(db))
	r.Post("/api/auth/login", auth.HandleLogin(db))
	r.Post("/api/register", HandleRegister(db))
	r.Post("/api/register-teacher", HandleRegisterTeacher(db))
	r.Post("/api/forgot-password", HandleForgotPassword(db))
	r.Post("/api/reset-password", HandleResetPassword(db))
	r.Post("/api/set-password", HandleSetPassword(db))
	r.Get("/api/verify-email", HandleVerifyEmail(db))
	r.Post("/api/resend-verification", HandleResendVerification(db))

	return r, db, func() { db.Close() }
}

// post is a small helper for sending JSON requests against the email flow
// router. Returns the response recorder for the caller to assert on.
func post(t *testing.T, r *chi.Mux, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func get(t *testing.T, r *chi.Mux, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ── Health endpoint ─────────────────────────────────────────────────────────

func TestHealth_OK(t *testing.T) {
	r, _, cleanup := setupEmailTestApp(t)
	defer cleanup()

	w := get(t, r, "/api/health")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["ok"] != true {
		t.Fatalf("expected ok=true, got %v", body["ok"])
	}
	if body["db"] != "ok" {
		t.Fatalf("expected db=ok, got %v", body["db"])
	}
}

// ── Parent self-serve registration ──────────────────────────────────────────

func TestRegister_Parent_CreatesPendingUser(t *testing.T) {
	r, db, cleanup := setupEmailTestApp(t)
	defer cleanup()

	w := post(t, r, "/api/register", map[string]any{
		"parentName":       "Test Parent",
		"email":            "newparent@example.com",
		"password":         "supersecret123",
		"phone":            "60123456789",
		"studentFirstName": "Kid",
		"studentLastName":  "One",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d: %s", w.Code, w.Body.String())
	}

	// User row should exist with status='pending_verification'
	var status string
	if err := db.QueryRow(`SELECT COALESCE(status,'') FROM users WHERE email=?`, "newparent@example.com").Scan(&status); err != nil {
		t.Fatalf("user not created: %v", err)
	}
	if status != "pending_verification" {
		t.Fatalf("expected status pending_verification, got %q", status)
	}

	// Login should be blocked with the needs_verification sentinel.
	w = post(t, r, "/api/auth/login", map[string]string{
		"email":    "newparent@example.com",
		"password": "supersecret123",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unverified login, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "needs_verification") {
		t.Fatalf("expected needs_verification in error, got %s", w.Body.String())
	}
}

func TestRegister_Parent_DuplicateEmail_Returns409(t *testing.T) {
	r, _, cleanup := setupEmailTestApp(t)
	defer cleanup()

	body := map[string]any{
		"parentName":       "Test Parent",
		"email":            "dupe@example.com",
		"password":         "supersecret123",
		"phone":            "60123456789",
		"studentFirstName": "Kid",
		"studentLastName":  "One",
	}
	w := post(t, r, "/api/register", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("first register should succeed: %d %s", w.Code, w.Body.String())
	}
	w = post(t, r, "/api/register", body)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 on duplicate, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegister_Parent_ShortPassword_Rejected(t *testing.T) {
	r, _, cleanup := setupEmailTestApp(t)
	defer cleanup()

	w := post(t, r, "/api/register", map[string]any{
		"parentName":       "Test Parent",
		"email":            "shortpw@example.com",
		"password":         "short",
		"phone":            "60123456789",
		"studentFirstName": "Kid",
		"studentLastName":  "One",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for short password, got %d: %s", w.Code, w.Body.String())
	}
}

// ── Email verification end-to-end ───────────────────────────────────────────

// TestVerifyEmail_Parent_ActivatesAndLogsIn registers a parent, fetches the
// freshly-created token directly from the DB (simulating the parent clicking
// the link in their email), hits /api/verify-email, and asserts that the
// user is now active and an auth cookie was issued.
func TestVerifyEmail_Parent_ActivatesAndLogsIn(t *testing.T) {
	r, db, cleanup := setupEmailTestApp(t)
	defer cleanup()

	w := post(t, r, "/api/register", map[string]any{
		"parentName":       "Verify Parent",
		"email":            "verify@example.com",
		"password":         "supersecret123",
		"phone":            "60123456789",
		"studentFirstName": "Kid",
		"studentLastName":  "Two",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("register failed: %d %s", w.Code, w.Body.String())
	}

	// Pull the verification token from the DB. In real life the user gets
	// it via email, but for tests we read it directly.
	var token string
	if err := db.QueryRow(
		`SELECT token FROM email_tokens WHERE email=? AND purpose=? AND used_at IS NULL`,
		"verify@example.com", store.TokenPurposeVerifyParent,
	).Scan(&token); err != nil {
		t.Fatalf("token not created: %v", err)
	}

	w = get(t, r, "/api/verify-email?token="+token)
	if w.Code != http.StatusOK {
		t.Fatalf("verify failed: %d %s", w.Code, w.Body.String())
	}

	// Cookie should be set
	hasCookie := false
	for _, c := range w.Result().Cookies() {
		if c.Name == "sh_token" && c.Value != "" {
			hasCookie = true
		}
	}
	if !hasCookie {
		t.Fatalf("expected sh_token cookie after verification")
	}

	// User should now be active
	var status string
	db.QueryRow(`SELECT status FROM users WHERE email=?`, "verify@example.com").Scan(&status)
	if status != "active" {
		t.Fatalf("expected status=active after verify, got %q", status)
	}

	// Login should now succeed
	w = post(t, r, "/api/auth/login", map[string]string{
		"email":    "verify@example.com",
		"password": "supersecret123",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("login after verify failed: %d %s", w.Code, w.Body.String())
	}
}

func TestVerifyEmail_BadToken_Returns400(t *testing.T) {
	r, _, cleanup := setupEmailTestApp(t)
	defer cleanup()

	w := get(t, r, "/api/verify-email?token=not-a-real-token")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyEmail_Token_CannotBeReused(t *testing.T) {
	r, db, cleanup := setupEmailTestApp(t)
	defer cleanup()

	post(t, r, "/api/register", map[string]any{
		"parentName":       "Reuse Parent",
		"email":            "reuse@example.com",
		"password":         "supersecret123",
		"phone":            "60123456789",
		"studentFirstName": "Kid",
		"studentLastName":  "Three",
	})

	var token string
	db.QueryRow(`SELECT token FROM email_tokens WHERE email=?`, "reuse@example.com").Scan(&token)

	if w := get(t, r, "/api/verify-email?token="+token); w.Code != http.StatusOK {
		t.Fatalf("first verify should succeed: %d %s", w.Code, w.Body.String())
	}
	if w := get(t, r, "/api/verify-email?token="+token); w.Code != http.StatusBadRequest {
		t.Fatalf("reused token should be rejected: %d %s", w.Code, w.Body.String())
	}
}

// ── Teacher application + verification ─────────────────────────────────────

func TestRegisterTeacher_CreatesPendingRegistration(t *testing.T) {
	r, db, cleanup := setupEmailTestApp(t)
	defer cleanup()

	w := post(t, r, "/api/register-teacher", map[string]any{
		"fullName":  "Test Teacher",
		"email":     "teacher@example.com",
		"phone":     "60123456789",
		"specialty": "Mathematics",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("teacher register failed: %d %s", w.Code, w.Body.String())
	}

	// No user row should exist yet
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM users WHERE email=?`, "teacher@example.com").Scan(&count)
	if count != 0 {
		t.Fatalf("expected no user row for unverified teacher, got %d", count)
	}

	// Registration row should exist with type=teacher, status=pending
	var status, regType string
	db.QueryRow(`SELECT status, type FROM registrations WHERE email=?`, "teacher@example.com").Scan(&status, &regType)
	if status != "pending" || regType != "teacher" {
		t.Fatalf("expected pending teacher registration, got status=%s type=%s", status, regType)
	}

	// Verify token should exist
	var tokenCount int
	db.QueryRow(`SELECT COUNT(*) FROM email_tokens WHERE email=? AND purpose=?`, "teacher@example.com", store.TokenPurposeVerifyTeacher).Scan(&tokenCount)
	if tokenCount == 0 {
		t.Fatal("expected verify_teacher token to be created")
	}
}

func TestVerifyEmail_Teacher_MarksRegistrationVerified_NoLogin(t *testing.T) {
	r, db, cleanup := setupEmailTestApp(t)
	defer cleanup()

	post(t, r, "/api/register-teacher", map[string]any{
		"fullName":  "Verified Teacher",
		"email":     "vteacher@example.com",
		"phone":     "60123456789",
		"specialty": "Mathematics",
	})

	var token string
	db.QueryRow(`SELECT token FROM email_tokens WHERE email=? AND purpose=?`, "vteacher@example.com", store.TokenPurposeVerifyTeacher).Scan(&token)

	w := get(t, r, "/api/verify-email?token="+token)
	if w.Code != http.StatusOK {
		t.Fatalf("teacher verify failed: %d %s", w.Code, w.Body.String())
	}

	// Response should say type=teacher and have no redirectTo
	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	if body["type"] != "teacher" {
		t.Fatalf("expected type=teacher, got %v", body["type"])
	}

	// No cookie should be set for teachers
	for _, c := range w.Result().Cookies() {
		if c.Name == "sh_token" && c.Value != "" {
			t.Fatalf("teacher verification should NOT set auth cookie")
		}
	}

	// Registration row should now have email_verified_at set
	var verifiedAt *string
	db.QueryRow(`SELECT email_verified_at::text FROM registrations WHERE email=?`, "vteacher@example.com").Scan(&verifiedAt)
	if verifiedAt == nil || *verifiedAt == "" {
		t.Fatal("expected email_verified_at to be set on registration")
	}
}

// ── Password reset flow ─────────────────────────────────────────────────────

func TestForgotPassword_GenericResponse_NoEnumeration(t *testing.T) {
	r, _, cleanup := setupEmailTestApp(t)
	defer cleanup()

	// Both existing and non-existing emails should return the same shape.
	w1 := post(t, r, "/api/forgot-password", map[string]string{"email": "admin@studyhub.com"})
	w2 := post(t, r, "/api/forgot-password", map[string]string{"email": "nobody@nowhere.com"})

	if w1.Code != http.StatusOK || w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for both, got %d / %d", w1.Code, w2.Code)
	}
	// Response bodies should be identical (modulo whitespace) — that's the
	// enumeration-prevention guarantee.
	if w1.Body.String() != w2.Body.String() {
		t.Fatalf("forgot-password leaks user existence: %q vs %q", w1.Body.String(), w2.Body.String())
	}
}

func TestResetPassword_HappyPath(t *testing.T) {
	r, db, cleanup := setupEmailTestApp(t)
	defer cleanup()

	// Trigger forgot-password against the seeded admin so a token is created.
	post(t, r, "/api/forgot-password", map[string]string{"email": "admin@studyhub.com"})

	var token string
	db.QueryRow(`SELECT token FROM email_tokens WHERE email=? AND purpose=?`, "admin@studyhub.com", store.TokenPurposeResetPassword).Scan(&token)
	if token == "" {
		t.Fatal("expected reset token to be created")
	}

	w := post(t, r, "/api/reset-password", map[string]string{
		"token":       token,
		"newPassword": "brandnew123",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("reset failed: %d %s", w.Code, w.Body.String())
	}

	// Old password should now fail
	w = post(t, r, "/api/auth/login", map[string]string{
		"email":    "admin@studyhub.com",
		"password": "admin123",
	})
	if w.Code == http.StatusOK {
		t.Fatal("old password still works after reset")
	}

	// New password should succeed
	w = post(t, r, "/api/auth/login", map[string]string{
		"email":    "admin@studyhub.com",
		"password": "brandnew123",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("new password should work: %d %s", w.Code, w.Body.String())
	}
}

// ── Set-password flow (for newly approved teachers) ────────────────────────

// TestSetPassword_HappyPath manually creates a user in pending_verification
// state and a set_password token, then exercises the endpoint and asserts
// the user is activated and can log in.
func TestSetPassword_HappyPath(t *testing.T) {
	r, db, cleanup := setupEmailTestApp(t)
	defer cleanup()

	// Hand-build the "approved teacher" state: user row + token. We don't
	// run the full approval flow here because that requires admin auth and
	// a pre-existing teacher registration.
	hash, _ := auth.HashPassword("placeholder-discarded")
	var userID int64
	db.QueryRow(
		`INSERT INTO users(tenant_id,email,password_hash,role,name,status) VALUES(?,?,?,?,?,?) RETURNING id`,
		1, "newteacher@example.com", hash, "teacher", "New Teacher", "pending_verification",
	).Scan(&userID)

	tok, err := store.CreateEmailToken(db, "newteacher@example.com", store.TokenPurposeSetPassword, &userID, nil, store.SetPasswordTokenTTL)
	if err != nil {
		t.Fatalf("token create: %v", err)
	}

	w := post(t, r, "/api/set-password", map[string]string{
		"token":       tok,
		"newPassword": "teacherpass1",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("set-password failed: %d %s", w.Code, w.Body.String())
	}

	// Cookie should be issued
	hasCookie := false
	for _, c := range w.Result().Cookies() {
		if c.Name == "sh_token" && c.Value != "" {
			hasCookie = true
		}
	}
	if !hasCookie {
		t.Fatal("expected sh_token cookie after set-password")
	}

	// User should now be active and the new password should work
	var status string
	db.QueryRow(`SELECT status FROM users WHERE id=?`, userID).Scan(&status)
	if status != "active" {
		t.Fatalf("expected status=active, got %q", status)
	}

	w = post(t, r, "/api/auth/login", map[string]string{
		"email":    "newteacher@example.com",
		"password": "teacherpass1",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("login with new teacher password failed: %d %s", w.Code, w.Body.String())
	}
}

func TestSetPassword_BadToken(t *testing.T) {
	r, _, cleanup := setupEmailTestApp(t)
	defer cleanup()

	w := post(t, r, "/api/set-password", map[string]string{
		"token":       "fake",
		"newPassword": "doesntmatter1",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad token, got %d: %s", w.Code, w.Body.String())
	}
}

// ── Resend verification ────────────────────────────────────────────────────

func TestResendVerification_PendingParent_RotatesToken(t *testing.T) {
	r, db, cleanup := setupEmailTestApp(t)
	defer cleanup()

	post(t, r, "/api/register", map[string]any{
		"parentName":       "Resend Parent",
		"email":            "resend@example.com",
		"password":         "supersecret123",
		"phone":            "60123456789",
		"studentFirstName": "Kid",
		"studentLastName":  "Four",
	})

	var firstToken string
	db.QueryRow(`SELECT token FROM email_tokens WHERE email=? AND used_at IS NULL`, "resend@example.com").Scan(&firstToken)

	w := post(t, r, "/api/resend-verification", map[string]string{"email": "resend@example.com"})
	if w.Code != http.StatusOK {
		t.Fatalf("resend failed: %d %s", w.Code, w.Body.String())
	}

	// The original token should now be marked used (invalidated) and a new
	// one should exist.
	var usedAt *string
	db.QueryRow(`SELECT used_at::text FROM email_tokens WHERE token=?`, firstToken).Scan(&usedAt)
	if usedAt == nil || *usedAt == "" {
		t.Fatal("old token should have been invalidated by resend")
	}

	var newCount int
	db.QueryRow(`SELECT COUNT(*) FROM email_tokens WHERE email=? AND used_at IS NULL`, "resend@example.com").Scan(&newCount)
	if newCount != 1 {
		t.Fatalf("expected exactly 1 active token after resend, got %d", newCount)
	}
}

func TestResendVerification_UnknownEmail_GenericResponse(t *testing.T) {
	r, _, cleanup := setupEmailTestApp(t)
	defer cleanup()

	w := post(t, r, "/api/resend-verification", map[string]string{"email": "nobody@nowhere.com"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected generic 200, got %d", w.Code)
	}
}
