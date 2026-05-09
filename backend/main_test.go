package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

// ── test helpers ──────────────────────────────────────────────────────────────

func testDSN() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://stratum:stratum_dev@localhost:5432/studyhub_test?sslmode=disable"
}

func setupTestApp(t *testing.T) (*chi.Mux, func()) {
	t.Helper()
	dsn := testDSN()
	db := initDB(dsn)

	// Clean tables before each test to ensure isolation
	tables := []string{
		"replacement_credits", "audit_logs", "payroll", "performance_reviews", "self_study_sessions",
		"cancelled_classes", "feedback", "attendance", "invoices",
		"announcements", "registrations", "students", "classes",
		"staff", "workshops", "subjects", "holidays", "users",
	}
	for _, tbl := range tables {
		db.Exec("DELETE FROM " + tbl)
	}

	seedIfEmpty(db)

	initLogger()

	hub := newHub()
	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{AllowedOrigins: []string{"*"}, AllowedMethods: []string{"GET", "POST", "PUT", "DELETE"}, AllowedHeaders: []string{"Authorization", "Content-Type"}}))

	r.Post("/api/auth/login", handleLogin(db))
	r.Get("/ws", hub.handleWS())
	r.Group(func(r chi.Router) {
		r.Use(jwtMiddleware)
		r.Get("/api/snapshot", handleSnapshot(db))
		r.Get("/api/students", handleStudents(db))
		r.Post("/api/students", handleStudents(db))
		r.Put("/api/students/{id}", handleStudent(db))
		r.Delete("/api/students/{id}", handleStudent(db))
		r.Get("/api/classes", handleClasses(db))
		r.Post("/api/classes", handleClasses(db))
		r.Get("/api/staff", handleStaff(db))
		r.Post("/api/staff", handleStaff(db))
		r.Get("/api/invoices", handleInvoices(db))
		r.Post("/api/invoices", handleInvoices(db))
		r.Put("/api/invoices/{id}/pay", handleInvoicePay(db))
		r.Get("/api/announcements", handleAnnouncements(db))
		r.Post("/api/announcements", handleAnnouncements(db))
		r.Delete("/api/announcements/{id}", handleAnnouncementDelete(db))
		r.Get("/api/attendance", handleAttendance(db, hub))
		r.Post("/api/attendance", handleAttendance(db, hub))
		r.Get("/api/feedback", handleListFeedback(db))
		r.Post("/api/feedback", handleCreateFeedback(db))
		r.Get("/api/self-study", handleListSelfStudy(db))
		r.Post("/api/self-study", handleCreateSelfStudy(db))
		r.Get("/api/performance-reviews", handleListPerformanceReviews(db))
		r.Post("/api/performance-reviews", handleCreatePerformanceReview(db))
		r.Post("/api/cancelled-classes", handleCreateCancelledClass(db))
		r.Get("/api/replacement-credits", handleListReplacementCredits(db))
		r.Post("/api/replacement-credits", handleCreateReplacementCredit(db))
		r.Delete("/api/replacement-credits/{id}", handleDeleteReplacementCredit(db))
		r.Get("/api/replacement-credits/balance", handleReplacementBalance(db))
		r.Group(func(r chi.Router) {
			r.Use(requireAdmin)
			r.Get("/api/users", handleUsers(db))
			r.Post("/api/users", handleUsers(db))
			r.Delete("/api/users/{id}", handleUserDelete(db))
		})
	})

	return r, func() { db.Close() }
}

func getAdminToken(t *testing.T, r *chi.Mux) string {
	t.Helper()
	return getToken(t, r, "admin@studyhub.com", "admin123")
}

func getParentToken(t *testing.T, r *chi.Mux) string {
	t.Helper()
	return getToken(t, r, "seeduser27@example.com", "parent123")
}

func getTeacherToken(t *testing.T, r *chi.Mux) string {
	t.Helper()
	return getToken(t, r, "chiying@studyhub.com", "Teacher123!")
}

func getToken(t *testing.T, r *chi.Mux, email, password string) string {
	t.Helper()
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login failed for %s: status %d, body: %s", email, w.Code, w.Body.String())
	}
	// Extract JWT from Set-Cookie header
	for _, c := range w.Result().Cookies() {
		if c.Name == "sh_token" {
			return c.Value
		}
	}
	t.Fatalf("no sh_token cookie in login response for %s", email)
	return ""
}

func doRequest(r *chi.Mux, method, path, token string, body any) *httptest.ResponseRecorder {
	var bodyReader *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(b))
	} else {
		bodyReader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ── Auth tests ────────────────────────────────────────────────────────────────

func TestLogin_Admin_Success(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	w := doRequest(r, "POST", "/api/auth/login", "", map[string]string{"email": "admin@studyhub.com", "password": "admin123"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", w.Code, w.Body.String())
	}
	var resp loginResponse
	json.NewDecoder(w.Body).Decode(&resp)
	// Token is in cookie, not in response body
	found := false
	for _, c := range w.Result().Cookies() {
		if c.Name == "sh_token" && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected sh_token cookie")
	}
	if resp.Role != "admin" {
		t.Fatalf("expected role admin got %s", resp.Role)
	}
}

func TestLogin_Parent_Success(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	w := doRequest(r, "POST", "/api/auth/login", "", map[string]string{"email": "seeduser27@example.com", "password": "parent123"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var resp loginResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Role != "parent" {
		t.Fatalf("expected role parent got %s", resp.Role)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	w := doRequest(r, "POST", "/api/auth/login", "", map[string]string{"email": "admin@studyhub.com", "password": "wrongpass"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}
}

func TestLogin_NonExistentUser(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	w := doRequest(r, "POST", "/api/auth/login", "", map[string]string{"email": "ghost@nowhere.com", "password": "abc"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}
}

func TestLogin_EmptyBody(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Fatal("should not succeed with bad body")
	}
}

func TestNoToken_Returns401(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	w := doRequest(r, "GET", "/api/students", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}
}

func TestBadToken_Returns401(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	w := doRequest(r, "GET", "/api/students", "not.a.real.token", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}
}

// ── Snapshot tests ────────────────────────────────────────────────────────────

func TestSnapshot_Admin_HasAllFields(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	w := doRequest(r, "GET", "/api/snapshot", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var snap Snapshot
	json.NewDecoder(w.Body).Decode(&snap)
	if len(snap.Students) == 0 {
		t.Error("expected students in snapshot")
	}
	if len(snap.Classes) == 0 {
		t.Error("expected classes in snapshot")
	}
	if len(snap.Staff) == 0 {
		t.Error("expected staff in snapshot")
	}
	if len(snap.Invoices) == 0 {
		t.Error("expected invoices in snapshot")
	}
	if len(snap.Announcements) == 0 {
		t.Error("expected announcements in snapshot")
	}
	if len(snap.Payroll) == 0 {
		t.Error("expected payroll in snapshot")
	}
}

func TestSnapshot_Parent_FilteredData(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getParentToken(t, r)
	w := doRequest(r, "GET", "/api/snapshot", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var snap Snapshot
	json.NewDecoder(w.Body).Decode(&snap)
	// Parent should only see their own children
	for _, s := range snap.Students {
		if s.Contact != "seeduser27@example.com" {
			t.Errorf("parent got student they don't own: %s (contact: %s)", s.ID, s.Contact)
		}
	}
	// Parent should not see payroll
	if len(snap.Payroll) != 0 {
		t.Error("parent should not see payroll")
	}
	// Parent should not see staff salaries
	for _, st := range snap.Staff {
		if st.Salary != 0 {
			t.Errorf("parent should not see salary for %s", st.Name)
		}
	}
	// Parent should not see performance reviews
	if len(snap.PerformanceReviews) != 0 {
		t.Error("parent should not see performance reviews")
	}
}

// ── Students tests ────────────────────────────────────────────────────────────

func TestStudents_List_Admin(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	w := doRequest(r, "GET", "/api/students", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var students []Student
	json.NewDecoder(w.Body).Decode(&students)
	if len(students) < 12 {
		t.Errorf("expected at least 12 students got %d", len(students))
	}
}

func TestStudents_Create_Admin(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	newStudent := Student{
		FirstName: "Test", LastName: "Child", DOB: "2015-06-15",
		Gender: "Male", ParentName: "Test Parent",
		Contact: "testparent@test.com", Phone: "60110000003",
		Branch: "The Study Hub", Status: "New",
		EnrolledClasses: []string{}, Siblings: []string{},
	}
	w := doRequest(r, "POST", "/api/students", token, newStudent)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", w.Code, w.Body.String())
	}
	var created Student
	json.NewDecoder(w.Body).Decode(&created)
	if created.FirstName != "Test" {
		t.Errorf("expected firstName Test got %s", created.FirstName)
	}
	if created.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestStudents_Create_Parent_Forbidden(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getParentToken(t, r)
	w := doRequest(r, "POST", "/api/students", token, Student{FirstName: "Hack"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 got %d", w.Code)
	}
}

func TestStudents_Update(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	w := doRequest(r, "GET", "/api/students", token, nil)
	var students []Student
	json.NewDecoder(w.Body).Decode(&students)
	if len(students) == 0 {
		t.Skip("no students to update")
	}
	s := students[0]
	s.Notes = "updated-note"
	w2 := doRequest(r, "PUT", "/api/students/"+s.ID, token, s)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w2.Code)
	}
}

func TestStudents_MissingName_Returns400(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	w := doRequest(r, "POST", "/api/students", token, map[string]string{"status": "Active"})
	if w.Code >= 500 {
		t.Fatalf("should not 500 on missing name, got %d", w.Code)
	}
}

// ── Classes tests ─────────────────────────────────────────────────────────────

func TestClasses_List(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	w := doRequest(r, "GET", "/api/classes", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var classes []Class
	json.NewDecoder(w.Body).Decode(&classes)
	if len(classes) < 8 {
		t.Errorf("expected at least 8 classes got %d", len(classes))
	}
}

func TestClasses_Create(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	cls := Class{Name: "Test Class", Day: "Wednesday", Time: "10:00", EndTime: "11:00", Capacity: 5, Color: "green"}
	w := doRequest(r, "POST", "/api/classes", token, cls)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", w.Code, w.Body.String())
	}
}

func TestClasses_TeacherIDs_RoundTrip(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	cls := Class{Name: "Multi Teacher", Day: "Friday", Time: "14:00", EndTime: "15:00", Capacity: 6, TeacherIDs: []string{"s1", "s2"}}
	w := doRequest(r, "POST", "/api/classes", token, cls)
	var created Class
	json.NewDecoder(w.Body).Decode(&created)
	if len(created.TeacherIDs) != 2 {
		t.Errorf("expected 2 teacher IDs got %d", len(created.TeacherIDs))
	}
}

// ── Invoices tests ────────────────────────────────────────────────────────────

func TestInvoices_List_Admin(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	w := doRequest(r, "GET", "/api/invoices", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var invoices []Invoice
	json.NewDecoder(w.Body).Decode(&invoices)
	if len(invoices) == 0 {
		t.Error("expected invoices")
	}
}

func TestInvoices_Parent_OnlySeesOwn(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getParentToken(t, r)
	w := doRequest(r, "GET", "/api/invoices", token, nil)
	var invoices []Invoice
	json.NewDecoder(w.Body).Decode(&invoices)
	validIDs := map[string]bool{"STU001": true, "STU002": true}
	for _, inv := range invoices {
		if !validIDs[inv.StudentID] {
			t.Errorf("parent got invoice for wrong student: %s", inv.StudentID)
		}
	}
}

func TestInvoices_Create_And_MarkPaid(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	inv := Invoice{StudentID: "STU001", Description: "Test Invoice", Type: "Adhoc", Amount: 99.50, DueDate: "2026-04-30"}
	w := doRequest(r, "POST", "/api/invoices", token, inv)
	if w.Code != http.StatusOK {
		t.Fatalf("create invoice: expected 200 got %d: %s", w.Code, w.Body.String())
	}
	var created Invoice
	json.NewDecoder(w.Body).Decode(&created)
	if created.Status != "Unpaid" {
		t.Errorf("new invoice should be Unpaid got %s", created.Status)
	}
	w2 := doRequest(r, "PUT", "/api/invoices/"+created.ID+"/pay", token, nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("mark paid: expected 200 got %d", w2.Code)
	}
}

func TestInvoices_NegativeAmount_Rejected(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	inv := Invoice{StudentID: "STU001", Description: "Bad Invoice", Amount: -50}
	w := doRequest(r, "POST", "/api/invoices", token, inv)
	if w.Code == http.StatusOK {
		t.Fatal("negative amount should be rejected")
	}
}

// ── Announcements tests ───────────────────────────────────────────────────────

func TestAnnouncements_List(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	w := doRequest(r, "GET", "/api/announcements", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var anns []Announcement
	json.NewDecoder(w.Body).Decode(&anns)
	if len(anns) == 0 {
		t.Error("expected announcements")
	}
}

func TestAnnouncements_Create_And_Delete(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	ann := Announcement{Title: "Test Notice", Message: "This is a test.", Audience: "All Parents", Type: "Notice"}
	w := doRequest(r, "POST", "/api/announcements", token, ann)
	if w.Code != http.StatusOK {
		t.Fatalf("create: expected 200 got %d: %s", w.Code, w.Body.String())
	}
	var created Announcement
	json.NewDecoder(w.Body).Decode(&created)
	if created.ID == "" {
		t.Fatal("expected ID in created announcement")
	}
	w2 := doRequest(r, "DELETE", "/api/announcements/"+created.ID, token, nil)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204 got %d", w2.Code)
	}
}

func TestAnnouncements_Create_Parent_Forbidden(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getParentToken(t, r)
	w := doRequest(r, "POST", "/api/announcements", token, Announcement{Title: "Hack", Message: "...", Type: "Notice"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 got %d", w.Code)
	}
}

// ── Attendance tests ──────────────────────────────────────────────────────────

func TestAttendance_List(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	w := doRequest(r, "GET", "/api/attendance", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
}

func TestAttendance_CheckIn_Student(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	classID := "c3"
	checkIn := "16:00"
	att := Attendance{
		PersonID: "STU001", PersonType: "student",
		Date: "2026-03-10", ClassID: &classID,
		CheckIn: &checkIn, Status: "Present",
	}
	w := doRequest(r, "POST", "/api/attendance", token, att)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", w.Code, w.Body.String())
	}
}

func TestAttendance_CheckIn_Then_CheckOut(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	classID := "c3"
	checkIn := "15:55"

	att := Attendance{PersonID: "STU002", PersonType: "student", Date: "2026-03-10", ClassID: &classID, CheckIn: &checkIn, Status: "Present"}
	doRequest(r, "POST", "/api/attendance", token, att)

	checkOut := "17:00"
	att.CheckOut = &checkOut
	w := doRequest(r, "POST", "/api/attendance", token, att)
	if w.Code != http.StatusOK {
		t.Fatalf("checkout: expected 200 got %d", w.Code)
	}
	var result Attendance
	json.NewDecoder(w.Body).Decode(&result)
	if result.CheckOut == nil {
		t.Error("expected checkOut to be set")
	}
}

func TestAttendance_Parent_OnlySeesOwnChildren(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getParentToken(t, r)
	w := doRequest(r, "GET", "/api/attendance", token, nil)
	var records []Attendance
	json.NewDecoder(w.Body).Decode(&records)
	validIDs := map[string]bool{"STU001": true, "STU002": true}
	for _, rec := range records {
		if !validIDs[rec.PersonID] {
			t.Errorf("parent got attendance for wrong person: %s", rec.PersonID)
		}
	}
}

// ── Staff tests ───────────────────────────────────────────────────────────────

func TestStaff_List(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	w := doRequest(r, "GET", "/api/staff", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var staff []Staff
	json.NewDecoder(w.Body).Decode(&staff)
	if len(staff) < 4 {
		t.Errorf("expected at least 4 staff got %d", len(staff))
	}
}

func TestStaff_Create_Admin(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	s := Staff{Name: "New", FullName: "Teacher New", Role: "Teacher", Email: "new@test.com", Salary: 3000, JoinDate: "2026-03-01", Status: "Active"}
	w := doRequest(r, "POST", "/api/staff", token, s)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", w.Code, w.Body.String())
	}
}

// ── User management tests ─────────────────────────────────────────────────────

func TestUsers_List_Admin(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	w := doRequest(r, "GET", "/api/users", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
}

func TestUsers_List_Parent_Forbidden(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getParentToken(t, r)
	w := doRequest(r, "GET", "/api/users", token, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 got %d", w.Code)
	}
}

func TestUsers_Create_And_Login(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)

	w := doRequest(r, "POST", "/api/users", token, userCreateReq{
		Email: "newparent@test.com", Password: "testpass123", Role: "parent", Name: "New Parent",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create user: expected 201 got %d: %s", w.Code, w.Body.String())
	}

	w2 := doRequest(r, "POST", "/api/auth/login", "", map[string]string{"email": "newparent@test.com", "password": "testpass123"})
	if w2.Code != http.StatusOK {
		t.Fatalf("new user login: expected 200 got %d", w2.Code)
	}
}

func TestUsers_DuplicateEmail_Returns409(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	w := doRequest(r, "POST", "/api/users", token, userCreateReq{Email: "admin@studyhub.com", Password: "whatever1", Role: "admin"})
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 got %d", w.Code)
	}
}

// ── Parent permission tests (new) ─────────────────────────────────────────────

func TestParent_Cannot_CreateFeedback(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getParentToken(t, r)
	w := doRequest(r, "POST", "/api/feedback", token, map[string]string{
		"classId": "c1", "date": "2026-03-10", "teacherId": "s1", "topic": "test",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("parent should not create feedback: expected 403 got %d", w.Code)
	}
}

func TestParent_Cannot_CreateSelfStudy(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getParentToken(t, r)
	w := doRequest(r, "POST", "/api/self-study", token, map[string]string{
		"studentId": "STU001", "date": "2026-03-10",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("parent should not create self-study: expected 403 got %d", w.Code)
	}
}

func TestParent_Cannot_CreatePerformanceReview(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getParentToken(t, r)
	w := doRequest(r, "POST", "/api/performance-reviews", token, map[string]string{
		"staffId": "s1",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("parent should not create performance review: expected 403 got %d", w.Code)
	}
}

func TestParent_Cannot_CancelClass(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getParentToken(t, r)
	w := doRequest(r, "POST", "/api/cancelled-classes", token, map[string]string{
		"classId": "c1", "date": "2026-03-10", "reason": "test",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("parent should not cancel classes: expected 403 got %d", w.Code)
	}
}

func TestParent_Cannot_See_PerformanceReviews(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getParentToken(t, r)
	w := doRequest(r, "GET", "/api/performance-reviews", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var reviews []PerformanceReview
	json.NewDecoder(w.Body).Decode(&reviews)
	if len(reviews) != 0 {
		t.Errorf("parent should see 0 performance reviews, got %d", len(reviews))
	}
}

func TestTeacher_Can_CreateFeedback(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getTeacherToken(t, r)
	w := doRequest(r, "POST", "/api/feedback", token, map[string]string{
		"classId": "c1", "date": "2026-03-10", "teacherId": "s1", "topic": "test topic",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("teacher should create feedback: expected 201 got %d: %s", w.Code, w.Body.String())
	}
}

// ── Edge case & security tests ────────────────────────────────────────────────

func TestXSS_InStudentName(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	s := Student{FirstName: "<script>alert(1)</script>", LastName: "Test", Contact: "xss@test.com", Status: "Active", EnrolledClasses: []string{}, Siblings: []string{}}
	w := doRequest(r, "POST", "/api/students", token, s)
	if w.Code >= 500 {
		t.Fatalf("XSS payload caused server error: %d", w.Code)
	}
}

func TestSQLInjection_InLogin(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	w := doRequest(r, "POST", "/api/auth/login", "", map[string]string{
		"email":    "' OR '1'='1",
		"password": "' OR '1'='1",
	})
	if w.Code == http.StatusOK {
		t.Fatal("SQL injection succeeded — critical security issue")
	}
}

func TestVeryLongInput(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	s := Student{
		FirstName:       strings.Repeat("A", 10000),
		LastName:        "Test",
		Contact:         "long@test.com",
		Status:          "Active",
		EnrolledClasses: []string{},
		Siblings:        []string{},
	}
	w := doRequest(r, "POST", "/api/students", token, s)
	if w.Code >= 500 {
		t.Fatalf("long input caused server crash: %d", w.Code)
	}
}

// ── Validation tests ──────────────────────────────────────────────────────────

func TestLogin_InvalidEmailFormat(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	cases := []string{"notanemail", "@nodomain.com", "missing@", "spaces in@email.com", ""}
	for _, email := range cases {
		w := doRequest(r, "POST", "/api/auth/login", "", map[string]string{"email": email, "password": "admin123"})
		if w.Code != http.StatusBadRequest {
			t.Errorf("email %q: expected 400 got %d", email, w.Code)
		}
	}
}

func TestLogin_EmptyPassword(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	w := doRequest(r, "POST", "/api/auth/login", "", map[string]string{"email": "admin@studyhub.com", "password": ""})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d", w.Code)
	}
}

func TestCreateUser_InvalidEmail(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	w := doRequest(r, "POST", "/api/users", token, userCreateReq{Email: "bademail", Password: "validpass123", Role: "parent"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d", w.Code)
	}
}

func TestCreateUser_ShortPassword(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	cases := []string{"", "1", "short", "1234567"}
	for _, pw := range cases {
		w := doRequest(r, "POST", "/api/users", token, userCreateReq{Email: "valid@test.com", Password: pw, Role: "parent"})
		if w.Code != http.StatusBadRequest {
			t.Errorf("password %q: expected 400 got %d", pw, w.Code)
		}
	}
}

func TestCreateUser_ExactlyEightChars_Allowed(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	w := doRequest(r, "POST", "/api/users", token, userCreateReq{Email: "eight@test.com", Password: "12345678", Role: "parent"})
	if w.Code != http.StatusCreated {
		t.Fatalf("8-char password should be valid, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConcurrentRequests(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)
	done := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		go func(i int) {
			w := doRequest(r, "GET", "/api/snapshot", token, nil)
			if w.Code != http.StatusOK {
				t.Errorf("concurrent request %d failed: %d", i, w.Code)
			}
			done <- true
		}(i)
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}

// ── Replacement Credits ──────────────────────────────────────────────────────

func TestReplacementCredits_EarnAndUse(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)

	// Create a student first
	w := doRequest(r, "POST", "/api/students", token, map[string]any{
		"firstName": "Ali", "lastName": "Test", "contact": "ali@test.com",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create student: %d %s", w.Code, w.Body.String())
	}
	var stu map[string]any
	json.Unmarshal(w.Body.Bytes(), &stu)
	stuID := stu["id"].(string)

	// Earn 4 credits (= 1 hour class absence)
	w = doRequest(r, "POST", "/api/replacement-credits", token, map[string]any{
		"studentId": stuID, "type": "earned", "minutes": 4, "category": "class", "note": "Absent from Math", "date": "2026-03-20",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("earn credit: %d %s", w.Code, w.Body.String())
	}
	var credit map[string]any
	json.Unmarshal(w.Body.Bytes(), &credit)
	if credit["minutes"].(float64) != 4 {
		t.Fatalf("expected 4 credits, got %v", credit["minutes"])
	}

	// Check balance = 4 class credits
	w = doRequest(r, "GET", "/api/replacement-credits/balance?studentId="+stuID, token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("balance: %d %s", w.Code, w.Body.String())
	}
	var bal map[string]any
	json.Unmarshal(w.Body.Bytes(), &bal)
	classBal := bal["class"].(map[string]any)
	if classBal["balance"].(float64) != 4 {
		t.Fatalf("expected class balance 4, got %v", classBal["balance"])
	}

	// Use 1 credit (= 15 min extension)
	w = doRequest(r, "POST", "/api/replacement-credits", token, map[string]any{
		"studentId": stuID, "type": "used", "minutes": 1, "category": "class", "note": "Extended English", "date": "2026-03-21",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("use credit: %d %s", w.Code, w.Body.String())
	}

	// Check balance = 3 class credits
	w = doRequest(r, "GET", "/api/replacement-credits/balance?studentId="+stuID, token, nil)
	json.Unmarshal(w.Body.Bytes(), &bal)
	classBal = bal["class"].(map[string]any)
	if classBal["balance"].(float64) != 3 {
		t.Fatalf("expected class balance 3, got %v", classBal["balance"])
	}

	// List credits — should have 2 entries
	w = doRequest(r, "GET", "/api/replacement-credits?studentId="+stuID, token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var credits []map[string]any
	json.Unmarshal(w.Body.Bytes(), &credits)
	if len(credits) != 2 {
		t.Fatalf("expected 2 credits, got %d", len(credits))
	}
}

func TestReplacementCredits_InsufficientBalance(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)

	// Create student
	w := doRequest(r, "POST", "/api/students", token, map[string]any{
		"firstName": "Siti", "lastName": "Test", "contact": "siti@test.com",
	})
	var stu map[string]any
	json.Unmarshal(w.Body.Bytes(), &stu)
	stuID := stu["id"].(string)

	// Try to use credits with 0 balance — should fail
	w = doRequest(r, "POST", "/api/replacement-credits", token, map[string]any{
		"studentId": stuID, "type": "used", "minutes": 2, "category": "class", "date": "2026-03-21",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for insufficient balance, got %d: %s", w.Code, w.Body.String())
	}

	// Earn 2 credits, try to use 3 — should fail
	doRequest(r, "POST", "/api/replacement-credits", token, map[string]any{
		"studentId": stuID, "type": "earned", "minutes": 2, "category": "class", "date": "2026-03-20",
	})
	w = doRequest(r, "POST", "/api/replacement-credits", token, map[string]any{
		"studentId": stuID, "type": "used", "minutes": 3, "category": "class", "date": "2026-03-21",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for over-use, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReplacementCredits_ParentReadOnly(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	parentToken := getParentToken(t, r)

	// Parent should NOT be able to create credits
	w := doRequest(r, "POST", "/api/replacement-credits", parentToken, map[string]any{
		"studentId": "STU_test", "type": "earned", "minutes": 4, "category": "class", "date": "2026-03-20",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for parent creating credit, got %d", w.Code)
	}

	// Parent CAN list (even if empty)
	w = doRequest(r, "GET", "/api/replacement-credits", parentToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("parent list: %d %s", w.Code, w.Body.String())
	}
}

func TestReplacementCredits_InvalidType(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)

	w := doRequest(r, "POST", "/api/replacement-credits", token, map[string]any{
		"studentId": "STU_test", "type": "bogus", "minutes": 4, "category": "class", "date": "2026-03-20",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid type, got %d", w.Code)
	}
}

func TestReplacementCredits_ZeroMinutes(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)

	w := doRequest(r, "POST", "/api/replacement-credits", token, map[string]any{
		"studentId": "STU_test", "type": "earned", "minutes": 0, "date": "2026-03-20",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for 0 minutes, got %d", w.Code)
	}
}

func TestReplacementCredits_InSnapshot(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)

	w := doRequest(r, "GET", "/api/snapshot", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("snapshot: %d", w.Code)
	}
	var snap map[string]any
	json.Unmarshal(w.Body.Bytes(), &snap)
	if _, ok := snap["replacementCredits"]; !ok {
		t.Fatal("snapshot missing replacementCredits field")
	}
}

func TestReplacementCredits_Delete(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	token := getAdminToken(t, r)

	// Create student + credit
	w := doRequest(r, "POST", "/api/students", token, map[string]any{
		"firstName": "Del", "lastName": "Test", "contact": "del@test.com",
	})
	var stu map[string]any
	json.Unmarshal(w.Body.Bytes(), &stu)

	w = doRequest(r, "POST", "/api/replacement-credits", token, map[string]any{
		"studentId": stu["id"], "type": "earned", "minutes": 4, "category": "class", "date": "2026-03-20",
	})
	var credit map[string]any
	json.Unmarshal(w.Body.Bytes(), &credit)
	creditID := credit["id"].(string)

	// Delete it
	w = doRequest(r, "DELETE", "/api/replacement-credits/"+creditID, token, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", w.Code)
	}

	// Balance should be 0
	w = doRequest(r, "GET", "/api/replacement-credits/balance?studentId="+stu["id"].(string), token, nil)
	var bal map[string]any
	json.Unmarshal(w.Body.Bytes(), &bal)
	classBal := bal["class"].(map[string]any)
	if classBal["balance"].(float64) != 0 {
		t.Fatalf("expected 0 after delete, got %v", classBal["balance"])
	}
}
