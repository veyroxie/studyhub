package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func respondError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
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
		}
		if c != nil && (c.Role == "admin" || c.Role == "superadmin") {
			snap.Registrations = listRegistrations(db, c)
		}
		respond(w, snap)
	}
}

// ── Students ──────────────────────────────────────────────────────────────────

func tenantID(c *Claims) int {
	if c == nil {
		return 1
	}
	if c.TenantID == 0 {
		return 0
	} // 0 = superadmin, cross-tenant
	return c.TenantID
}

func listStudents(db *DB, c *Claims) []Student {
	var rows *sql.Rows
	var err error
	tid := tenantID(c)
	if c != nil && c.Role == "parent" {
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,COALESCE(medical_info,''),COALESCE(allergies,''),COALESCE(family_id,'') FROM students WHERE contact=? AND (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY registered_on`, c.Email, tid, tid)
	} else {
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,COALESCE(medical_info,''),COALESCE(allergies,''),COALESCE(family_id,'') FROM students WHERE (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY registered_on`, tid, tid)
	}
	if err != nil {
		return []Student{}
	}
	defer rows.Close()
	out := []Student{}
	for rows.Next() {
		var s Student
		var ec, sib string
		var e2name, e2phone sql.NullString
		rows.Scan(&s.ID, &s.FirstName, &s.LastName, &s.DOB, &s.Gender, &s.ParentName, &s.Contact, &s.Phone, &s.Branch, &s.Status, &s.RegisteredOn, &ec, &sib, &s.Notes, &e2name, &e2phone, &s.MedicalInfo, &s.Allergies, &s.FamilyID)
		s.EnrolledClasses = parseArr(ec)
		s.Siblings = parseArr(sib)
		s.Emergency2Name = nullStr(e2name)
		s.Emergency2Phone = nullStr(e2phone)
		out = append(out, s)
	}
	return out
}

func listStudentsPaged(db *DB, c *Claims, p Pagination) ([]Student, int) {
	tid := tenantID(c)
	var total int
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		db.QueryRow(`SELECT COUNT(*) FROM students WHERE contact=? AND (tenant_id=? OR ?=0) AND deleted_at IS NULL`, c.Email, tid, tid).Scan(&total)
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,COALESCE(medical_info,''),COALESCE(allergies,''),COALESCE(family_id,'') FROM students WHERE contact=? AND (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY registered_on LIMIT ? OFFSET ?`, c.Email, tid, tid, p.Limit, p.Offset)
	} else {
		db.QueryRow(`SELECT COUNT(*) FROM students WHERE (tenant_id=? OR ?=0) AND deleted_at IS NULL`, tid, tid).Scan(&total)
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,COALESCE(medical_info,''),COALESCE(allergies,''),COALESCE(family_id,'') FROM students WHERE (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY registered_on LIMIT ? OFFSET ?`, tid, tid, p.Limit, p.Offset)
	}
	if err != nil {
		return []Student{}, total
	}
	defer rows.Close()
	out := []Student{}
	for rows.Next() {
		var s Student
		var ec, sib string
		var e2name, e2phone sql.NullString
		rows.Scan(&s.ID, &s.FirstName, &s.LastName, &s.DOB, &s.Gender, &s.ParentName, &s.Contact, &s.Phone, &s.Branch, &s.Status, &s.RegisteredOn, &ec, &sib, &s.Notes, &e2name, &e2phone, &s.MedicalInfo, &s.Allergies, &s.FamilyID)
		s.EnrolledClasses = parseArr(ec)
		s.Siblings = parseArr(sib)
		s.Emergency2Name = nullStr(e2name)
		s.Emergency2Phone = nullStr(e2phone)
		out = append(out, s)
	}
	return out, total
}

func handleStudents(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			p := parsePagination(r)
			if !p.Active {
				respond(w, listStudents(db, c))
				return
			}
			data, total := listStudentsPaged(db, c, p)
			respond(w, PaginatedResponse{Data: data, Total: total, Limit: p.Limit, Offset: p.Offset})
		case http.MethodPost:
			if c.Role != "admin" {
				respondError(w, "admin only", 403)
				return
			}
			var s Student
			if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			if msg := validationError("firstName", s.FirstName, "lastName", s.LastName, "contact", s.Contact); msg != "" {
				respondError(w, msg, 400)
				return
			}
			if s.Status == "" {
				s.Status = "New"
			}
			if s.ID == "" {
				s.ID = generateID("STU")
			}
			if s.RegisteredOn == "" {
				s.RegisteredOn = today()
			}
			tid := tenantID(c)
			// Auto-find or create family for this student
			if s.FamilyID == "" && s.Contact != "" {
				var famID string
				db.QueryRow(`SELECT id FROM families WHERE contact=? AND (tenant_id=? OR ?=0) AND deleted_at IS NULL`, s.Contact, tid, tid).Scan(&famID)
				if famID == "" {
					famID = generateID("FAM")
					familyName := s.ParentName + " Family"
					if s.ParentName == "" {
						familyName = s.Contact
					}
					db.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name) VALUES(?,?,?,?,?,?)`,
						famID, tid, familyName, s.Contact, s.Phone, s.ParentName)
				}
				s.FamilyID = famID
			}
			_, err := db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,medical_info,allergies,family_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				s.ID, tid, s.FirstName, s.LastName, s.DOB, s.Gender, s.ParentName, s.Contact, s.Phone, s.Branch, s.Status, s.RegisteredOn, jsonArr(s.EnrolledClasses), jsonArr(s.Siblings), s.Notes, s.Emergency2Name, s.Emergency2Phone, s.MedicalInfo, s.Allergies, s.FamilyID)
			if err != nil {
				respondError(w, "server error", 500)
				return
			}
			respond(w, s)
		}
	}
}

func handleStudent(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		switch r.Method {
		case http.MethodPut:
			var s Student
			if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			s.ID = id
			tid := tenantID(c)
			if _, err := db.Exec(`UPDATE students SET first_name=?,last_name=?,dob=?,gender=?,parent_name=?,contact=?,phone=?,branch=?,status=?,enrolled_classes=?,siblings=?,notes=?,emergency2_name=?,emergency2_phone=?,medical_info=?,allergies=?,family_id=? WHERE id=? AND (tenant_id=? OR ?=0)`,
				s.FirstName, s.LastName, s.DOB, s.Gender, s.ParentName, s.Contact, s.Phone, s.Branch, s.Status, jsonArr(s.EnrolledClasses), jsonArr(s.Siblings), s.Notes, s.Emergency2Name, s.Emergency2Phone, s.MedicalInfo, s.Allergies, s.FamilyID, id, tid, tid); err != nil {
				respondError(w, "could not update student", 500)
				return
			}
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "student_updated", "student", id, s.FirstName+" "+s.LastName)
			respond(w, s)
		case http.MethodDelete:
			tid := tenantID(c)
			if _, err := db.Exec(`UPDATE students SET deleted_at=NOW() WHERE id=? AND (tenant_id=? OR ?=0)`, id, tid, tid); err != nil {
				respondError(w, "could not delete student", 500)
				return
			}
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "student_deleted", "student", id, "soft deleted")
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// ── Classes ───────────────────────────────────────────────────────────────────

func listClasses(db *DB, c *Claims) []Class {
	tid := tenantID(c)
	rows, err := db.Query(`SELECT id,name,teacher_ids,classroom,day,time,end_time,capacity,enrolled,color,category FROM classes WHERE deleted_at IS NULL AND (tenant_id=? OR ?=0)`, tid, tid)
	if err != nil {
		return []Class{}
	}
	defer rows.Close()
	out := []Class{}
	for rows.Next() {
		var c Class
		var tids string
		rows.Scan(&c.ID, &c.Name, &tids, &c.Classroom, &c.Day, &c.Time, &c.EndTime, &c.Capacity, &c.Enrolled, &c.Color, &c.Category)
		c.TeacherIDs = parseArr(tids)
		out = append(out, c)
	}
	return out
}

func handleClasses(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			respond(w, listClasses(db, cl))
		case http.MethodPost:
			if cl == nil || (cl.Role != "admin" && cl.Role != "superadmin") {
				respondError(w, "admin only", 403)
				return
			}
			var c Class
			if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			if msg := validationError("name", c.Name, "day", c.Day, "time", c.Time, "endTime", c.EndTime); msg != "" {
				respondError(w, msg, 400)
				return
			}
			if c.Capacity < 1 {
				c.Capacity = 5
			}
			if c.Time >= c.EndTime {
				respondError(w, "end time must be after start time", 400)
				return
			}
			if c.ID == "" {
				c.ID = generateID("cls")
			}

			// ── Clash detection ────────────────────────────────────────────────
			// Two intervals [s1,e1) and [s2,e2) overlap when s1<e2 AND s2<e1
			for _, tid2 := range c.TeacherIDs {
				var cnt int
				db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND id!=? AND time<?  AND end_time>? AND teacher_ids LIKE '%'||?||'%' AND deleted_at IS NULL`,
					c.Day, c.ID, c.Time, c.EndTime, tid2).Scan(&cnt)
				if cnt > 0 {
					respondError(w, "Conflict: teacher "+tid2+" is already booked at this time", http.StatusConflict)
					return
				}
			}
			if c.Classroom != "" {
				var cnt int
				db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND classroom=? AND id!=? AND time<? AND end_time>? AND deleted_at IS NULL`,
					c.Day, c.Classroom, c.ID, c.Time, c.EndTime).Scan(&cnt)
				if cnt > 0 {
					respondError(w, "Conflict: "+c.Classroom+" is already booked at this time", http.StatusConflict)
					return
				}
			}

			if c.Category == "" {
				c.Category = "Academic"
			}
			tid := tenantID(cl)
			db.Exec(`INSERT INTO classes(id,tenant_id,name,teacher_ids,classroom,day,time,end_time,capacity,enrolled,color,category) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
				c.ID, tid, c.Name, jsonArr(c.TeacherIDs), c.Classroom, c.Day, c.Time, c.EndTime, c.Capacity, c.Enrolled, c.Color, c.Category)
			respond(w, c)
		}
	}
}

// ── Staff ─────────────────────────────────────────────────────────────────────

func listStaff(db *DB, c *Claims) []Staff {
	tid := tenantID(c)
	rows, err := db.Query(`SELECT id,name,full_name,role,email,phone,salary,join_date,status,specialization,nric,emergency_name,emergency_phone,employment_type,hourly_rate FROM staff WHERE deleted_at IS NULL AND (tenant_id=? OR ?=0)`, tid, tid)
	if err != nil {
		return []Staff{}
	}
	defer rows.Close()
	out := []Staff{}
	for rows.Next() {
		var s Staff
		var spec, nric, eName, ePhone, empType sql.NullString
		var hourlyRate sql.NullFloat64
		rows.Scan(&s.ID, &s.Name, &s.FullName, &s.Role, &s.Email, &s.Phone, &s.Salary, &s.JoinDate, &s.Status, &spec, &nric, &eName, &ePhone, &empType, &hourlyRate)
		s.Specialization = nullStr(spec)
		s.NRIC = nullStr(nric)
		s.EmergencyName = nullStr(eName)
		s.EmergencyPhone = nullStr(ePhone)
		s.EmploymentType = nullStr(empType)
		if hourlyRate.Valid {
			s.HourlyRate = hourlyRate.Float64
		}
		if c != nil && c.Role == "parent" {
			s.Salary = 0 // hide salary from parents
			s.HourlyRate = 0
			s.NRIC = ""
		}
		out = append(out, s)
	}
	return out
}

func handleStaff(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			respond(w, listStaff(db, c))
		case http.MethodPost:
			if c.Role != "admin" {
				respondError(w, "admin only", 403)
				return
			}
			var s Staff
			if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			if msg := validationError("name", s.Name, "fullName", s.FullName, "role", s.Role, "email", s.Email); msg != "" {
				respondError(w, msg, 400)
				return
			}
			if s.Status == "" {
				s.Status = "Active"
			}
			if s.ID == "" {
				s.ID = generateID("stf")
			}
			if s.EmploymentType == "" {
				s.EmploymentType = "Full-time"
			}
			tid := tenantID(c)
			db.Exec(`INSERT INTO staff(id,tenant_id,name,full_name,role,email,phone,salary,join_date,status,specialization,nric,emergency_name,emergency_phone,employment_type,hourly_rate) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				s.ID, tid, s.Name, s.FullName, s.Role, s.Email, s.Phone, s.Salary, s.JoinDate, s.Status, s.Specialization, s.NRIC, s.EmergencyName, s.EmergencyPhone, s.EmploymentType, s.HourlyRate)
			respond(w, s)
		}
	}
}

// ── Invoices ──────────────────────────────────────────────────────────────────

func listInvoices(db *DB, c *Claims) []Invoice {
	var rows *sql.Rows
	var err error
	tid := tenantID(c)
	if c != nil && c.Role == "parent" {
		rows, err = db.Query(`SELECT i.id,i.student_id,i.description,i.type,i.amount,i.due_date,i.status,i.created_on,i.paid_on,COALESCE(i.payment_proof,''),COALESCE(i.payment_method,''),COALESCE(i.discount_pct,0),COALESCE(i.submitted_by_parent,false),COALESCE(i.sibling_ids,''),COALESCE(i.sibling_discount,0) FROM invoices i JOIN students s ON s.id=i.student_id WHERE s.contact=? AND i.deleted_at IS NULL AND (i.tenant_id=? OR ?=0) ORDER BY i.created_on DESC`, c.Email, tid, tid)
	} else {
		rows, err = db.Query(`SELECT id,student_id,description,type,amount,due_date,status,created_on,paid_on,COALESCE(payment_proof,''),COALESCE(payment_method,''),COALESCE(discount_pct,0),COALESCE(submitted_by_parent,false),COALESCE(sibling_ids,''),COALESCE(sibling_discount,0) FROM invoices WHERE deleted_at IS NULL AND (tenant_id=? OR ?=0) ORDER BY created_on DESC`, tid, tid)
	}
	if err != nil {
		return []Invoice{}
	}
	defer rows.Close()
	out := []Invoice{}
	for rows.Next() {
		var inv Invoice
		var paidOn sql.NullString
		rows.Scan(&inv.ID, &inv.StudentID, &inv.Description, &inv.Type, &inv.Amount, &inv.DueDate, &inv.Status, &inv.CreatedOn, &paidOn, &inv.PaymentProof, &inv.PaymentMethod, &inv.DiscountPct, &inv.SubmittedByParent, &inv.SiblingIds, &inv.SiblingDiscount)
		if paidOn.Valid {
			inv.PaidOn = &paidOn.String
		}
		out = append(out, inv)
	}
	return out
}

func listInvoicesPaged(db *DB, c *Claims, p Pagination) ([]Invoice, int) {
	tid := tenantID(c)
	var total int
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		db.QueryRow(`SELECT COUNT(*) FROM invoices i JOIN students s ON s.id=i.student_id WHERE s.contact=? AND i.deleted_at IS NULL AND (i.tenant_id=? OR ?=0)`, c.Email, tid, tid).Scan(&total)
		rows, err = db.Query(`SELECT i.id,i.student_id,i.description,i.type,i.amount,i.due_date,i.status,i.created_on,i.paid_on,COALESCE(i.payment_proof,''),COALESCE(i.payment_method,''),COALESCE(i.discount_pct,0),COALESCE(i.submitted_by_parent,false),COALESCE(i.sibling_ids,''),COALESCE(i.sibling_discount,0) FROM invoices i JOIN students s ON s.id=i.student_id WHERE s.contact=? AND i.deleted_at IS NULL AND (i.tenant_id=? OR ?=0) ORDER BY i.created_on DESC LIMIT ? OFFSET ?`, c.Email, tid, tid, p.Limit, p.Offset)
	} else {
		db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE deleted_at IS NULL AND (tenant_id=? OR ?=0)`, tid, tid).Scan(&total)
		rows, err = db.Query(`SELECT id,student_id,description,type,amount,due_date,status,created_on,paid_on,COALESCE(payment_proof,''),COALESCE(payment_method,''),COALESCE(discount_pct,0),COALESCE(submitted_by_parent,false),COALESCE(sibling_ids,''),COALESCE(sibling_discount,0) FROM invoices WHERE deleted_at IS NULL AND (tenant_id=? OR ?=0) ORDER BY created_on DESC LIMIT ? OFFSET ?`, tid, tid, p.Limit, p.Offset)
	}
	if err != nil {
		return []Invoice{}, total
	}
	defer rows.Close()
	out := []Invoice{}
	for rows.Next() {
		var inv Invoice
		var paidOn sql.NullString
		rows.Scan(&inv.ID, &inv.StudentID, &inv.Description, &inv.Type, &inv.Amount, &inv.DueDate, &inv.Status, &inv.CreatedOn, &paidOn, &inv.PaymentProof, &inv.PaymentMethod, &inv.DiscountPct, &inv.SubmittedByParent, &inv.SiblingIds, &inv.SiblingDiscount)
		if paidOn.Valid {
			inv.PaidOn = &paidOn.String
		}
		out = append(out, inv)
	}
	return out, total
}

func handleInvoices(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			p := parsePagination(r)
			if !p.Active {
				respond(w, listInvoices(db, c))
				return
			}
			data, total := listInvoicesPaged(db, c, p)
			respond(w, PaginatedResponse{Data: data, Total: total, Limit: p.Limit, Offset: p.Offset})
		case http.MethodPost:
			if c.Role != "admin" {
				respondError(w, "admin only", 403)
				return
			}
			var inv Invoice
			if err := json.NewDecoder(r.Body).Decode(&inv); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			if msg := validationError("studentId", inv.StudentID, "description", inv.Description, "dueDate", inv.DueDate); msg != "" {
				respondError(w, msg, 400)
				return
			}
			if !validAmount(inv.Amount) {
				respondError(w, "amount must be greater than 0", 400)
				return
			}
			if inv.ID == "" {
				inv.ID = generateID("INV")
			}
			if inv.CreatedOn == "" {
				inv.CreatedOn = today()
			}
			inv.Status = "Unpaid"
			tid := tenantID(c)
			db.Exec(`INSERT INTO invoices(id,tenant_id,student_id,description,type,amount,due_date,status,created_on,paid_on,payment_method,discount_pct,submitted_by_parent,sibling_ids,sibling_discount) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				inv.ID, tid, inv.StudentID, inv.Description, inv.Type, inv.Amount, inv.DueDate, inv.Status, inv.CreatedOn, nil, inv.PaymentMethod, inv.DiscountPct, inv.SubmittedByParent, inv.SiblingIds, inv.SiblingDiscount)
			respond(w, inv)
		}
	}
}

func handleInvoicePay(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		id := chi.URLParam(r, "id")

		// Verify invoice exists and check ownership for parents
		var studentID string
		var amount float64
		if err := db.QueryRow(`SELECT student_id, amount FROM invoices WHERE id=? AND deleted_at IS NULL`, id).Scan(&studentID, &amount); err != nil {
			respondError(w, "invoice not found", 404)
			return
		}
		if c != nil && c.Role == "parent" {
			var ownerEmail string
			db.QueryRow(`SELECT contact FROM students WHERE id=?`, studentID).Scan(&ownerEmail)
			if ownerEmail != c.Email {
				respondError(w, "not your invoice", 403)
				return
			}
		}

		// Decode optional body (status override, payment method)
		var body struct {
			Status        string `json:"status"`
			PaymentMethod string `json:"paymentMethod"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		newStatus := "Paid"
		if body.Status != "" {
			newStatus = body.Status
		}

		t := today()
		tid := tenantID(c)
		if _, err := db.Exec(`UPDATE invoices SET status=?, paid_on=?, payment_method=COALESCE(NULLIF(?,''),payment_method) WHERE id=? AND (tenant_id=? OR ?=0)`, newStatus, t, body.PaymentMethod, id, tid, tid); err != nil {
			respondError(w, "could not update invoice", 500)
			return
		}
		if c != nil {
			detail := fmt.Sprintf(`{"studentId":"%s","amount":%.2f,"paidOn":"%s","method":"%s"}`, studentID, amount, t, body.PaymentMethod)
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "invoice_paid", "invoice", id, detail)
		}
		respond(w, map[string]string{"status": newStatus, "paidOn": t})
	}
}

func handleAuditLogs(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type LogEntry struct {
			ID         int    `json:"id"`
			ActorEmail string `json:"actorEmail"`
			Action     string `json:"action"`
			EntityType string `json:"entityType"`
			EntityID   string `json:"entityId"`
			Detail     string `json:"detail"`
			CreatedAt  string `json:"createdAt"`
		}

		p := parsePagination(r)
		if !p.Active {
			rows, err := db.Query(`SELECT id,actor_email,action,entity_type,entity_id,detail,created_at FROM audit_logs ORDER BY created_at DESC LIMIT 200`)
			if err != nil {
				respond(w, []any{})
				return
			}
			defer rows.Close()
			out := []LogEntry{}
			for rows.Next() {
				var e LogEntry
				rows.Scan(&e.ID, &e.ActorEmail, &e.Action, &e.EntityType, &e.EntityID, &e.Detail, &e.CreatedAt)
				out = append(out, e)
			}
			respond(w, out)
			return
		}

		var total int
		db.QueryRow(`SELECT COUNT(*) FROM audit_logs`).Scan(&total)
		rows, err := db.Query(`SELECT id,actor_email,action,entity_type,entity_id,detail,created_at FROM audit_logs ORDER BY created_at DESC LIMIT ? OFFSET ?`, p.Limit, p.Offset)
		if err != nil {
			respond(w, PaginatedResponse{Data: []LogEntry{}, Total: total, Limit: p.Limit, Offset: p.Offset})
			return
		}
		defer rows.Close()
		out := []LogEntry{}
		for rows.Next() {
			var e LogEntry
			rows.Scan(&e.ID, &e.ActorEmail, &e.Action, &e.EntityType, &e.EntityID, &e.Detail, &e.CreatedAt)
			out = append(out, e)
		}
		respond(w, PaginatedResponse{Data: out, Total: total, Limit: p.Limit, Offset: p.Offset})
	}
}

// ── Announcements ─────────────────────────────────────────────────────────────

func listAnnouncements(db *DB, c *Claims) []Announcement {
	tid := tenantID(c)
	rows, err := db.Query(`SELECT id,title,message,audience,type,created_on,created_by,status,archive_on FROM announcements WHERE (tenant_id=? OR ?=0) ORDER BY created_on DESC`, tid, tid)
	if err != nil {
		return []Announcement{}
	}
	defer rows.Close()
	out := []Announcement{}
	for rows.Next() {
		var a Announcement
		var status, archiveOn sql.NullString
		rows.Scan(&a.ID, &a.Title, &a.Message, &a.Audience, &a.Type, &a.CreatedOn, &a.CreatedBy, &status, &archiveOn)
		a.Status = nullStr(status)
		if a.Status == "" {
			a.Status = "published"
		}
		a.ArchiveOn = nullStr(archiveOn)
		out = append(out, a)
	}
	return out
}

func listAnnouncementsPaged(db *DB, c *Claims, p Pagination) ([]Announcement, int) {
	tid := tenantID(c)
	var total int
	db.QueryRow(`SELECT COUNT(*) FROM announcements WHERE (tenant_id=? OR ?=0)`, tid, tid).Scan(&total)
	rows, err := db.Query(`SELECT id,title,message,audience,type,created_on,created_by,status,archive_on FROM announcements WHERE (tenant_id=? OR ?=0) ORDER BY created_on DESC LIMIT ? OFFSET ?`, tid, tid, p.Limit, p.Offset)
	if err != nil {
		return []Announcement{}, total
	}
	defer rows.Close()
	out := []Announcement{}
	for rows.Next() {
		var a Announcement
		var status, archiveOn sql.NullString
		rows.Scan(&a.ID, &a.Title, &a.Message, &a.Audience, &a.Type, &a.CreatedOn, &a.CreatedBy, &status, &archiveOn)
		a.Status = nullStr(status)
		if a.Status == "" {
			a.Status = "published"
		}
		a.ArchiveOn = nullStr(archiveOn)
		out = append(out, a)
	}
	return out, total
}

func handleAnnouncements(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			p := parsePagination(r)
			if !p.Active {
				respond(w, listAnnouncements(db, c))
				return
			}
			data, total := listAnnouncementsPaged(db, c, p)
			respond(w, PaginatedResponse{Data: data, Total: total, Limit: p.Limit, Offset: p.Offset})
		case http.MethodPost:
			if c.Role != "admin" && c.Role != "teacher" {
				respondError(w, "admin or teacher only", 403)
				return
			}
			var a Announcement
			if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			if msg := validationError("title", a.Title, "message", a.Message); msg != "" {
				respondError(w, msg, 400)
				return
			}
			if a.ID == "" {
				a.ID = generateID("ANN")
			}
			if a.CreatedOn == "" {
				a.CreatedOn = today()
			}
			if a.CreatedBy == "" {
				a.CreatedBy = c.Name
			}
			if a.Status == "" {
				a.Status = "published"
			}
			tid := tenantID(c)
			db.Exec(`INSERT INTO announcements(id,tenant_id,title,message,audience,type,created_on,created_by,status,archive_on) VALUES(?,?,?,?,?,?,?,?,?,?)`,
				a.ID, tid, a.Title, a.Message, a.Audience, a.Type, a.CreatedOn, a.CreatedBy, a.Status, a.ArchiveOn)
			respond(w, a)
		}
	}
}

func handleAnnouncementDelete(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tid := tenantID(c)
		if _, err := db.Exec(`DELETE FROM announcements WHERE id=? AND (tenant_id=? OR ?=0)`, id, tid, tid); err != nil {
			respondError(w, "could not delete announcement", 500)
			return
		}
		db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
			c.Email, "announcement_deleted", "announcement", id, "deleted")
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAnnouncementUpdate(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var a Announcement
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		tid := tenantID(c)
		db.Exec(`UPDATE announcements SET title=?,message=?,type=?,archive_on=? WHERE id=? AND (tenant_id=? OR ?=0)`,
			a.Title, a.Message, a.Type, a.ArchiveOn, id, tid, tid)
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAnnouncementApprove(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var body struct {
			Status string `json:"status"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Status == "" {
			body.Status = "published"
		}
		tid := tenantID(c)
		db.Exec(`UPDATE announcements SET status=? WHERE id=? AND (tenant_id=? OR ?=0)`, body.Status, id, tid, tid)
		db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
			c.Email, "announcement_"+body.Status, "announcement", id, "status changed to "+body.Status)
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── Attendance ────────────────────────────────────────────────────────────────

func listAttendance(db *DB, c *Claims) []Attendance {
	var rows *sql.Rows
	var err error
	tid := tenantID(c)
	if c != nil && c.Role == "parent" {
		rows, err = db.Query(`SELECT a.id,a.person_id,a.person_type,a.date,a.class_id,a.check_in,a.check_out,a.status FROM attendance a JOIN students s ON s.id=a.person_id WHERE a.person_type='student' AND s.contact=? AND (a.tenant_id=? OR ?=0) ORDER BY a.date DESC`, c.Email, tid, tid)
	} else {
		rows, err = db.Query(`SELECT id,person_id,person_type,date,class_id,check_in,check_out,status FROM attendance WHERE (tenant_id=? OR ?=0) ORDER BY date DESC`, tid, tid)
	}
	if err != nil {
		return []Attendance{}
	}
	defer rows.Close()
	out := []Attendance{}
	for rows.Next() {
		var a Attendance
		var classID, checkIn, checkOut sql.NullString
		rows.Scan(&a.ID, &a.PersonID, &a.PersonType, &a.Date, &classID, &checkIn, &checkOut, &a.Status)
		if classID.Valid {
			a.ClassID = &classID.String
		}
		if checkIn.Valid {
			a.CheckIn = &checkIn.String
		}
		if checkOut.Valid {
			a.CheckOut = &checkOut.String
		}
		out = append(out, a)
	}
	return out
}

func listAttendancePaged(db *DB, c *Claims, p Pagination) ([]Attendance, int) {
	tid := tenantID(c)
	var total int
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		db.QueryRow(`SELECT COUNT(*) FROM attendance a JOIN students s ON s.id=a.person_id WHERE a.person_type='student' AND s.contact=? AND (a.tenant_id=? OR ?=0)`, c.Email, tid, tid).Scan(&total)
		rows, err = db.Query(`SELECT a.id,a.person_id,a.person_type,a.date,a.class_id,a.check_in,a.check_out,a.status FROM attendance a JOIN students s ON s.id=a.person_id WHERE a.person_type='student' AND s.contact=? AND (a.tenant_id=? OR ?=0) ORDER BY a.date DESC LIMIT ? OFFSET ?`, c.Email, tid, tid, p.Limit, p.Offset)
	} else {
		db.QueryRow(`SELECT COUNT(*) FROM attendance WHERE (tenant_id=? OR ?=0)`, tid, tid).Scan(&total)
		rows, err = db.Query(`SELECT id,person_id,person_type,date,class_id,check_in,check_out,status FROM attendance WHERE (tenant_id=? OR ?=0) ORDER BY date DESC LIMIT ? OFFSET ?`, tid, tid, p.Limit, p.Offset)
	}
	if err != nil {
		return []Attendance{}, total
	}
	defer rows.Close()
	out := []Attendance{}
	for rows.Next() {
		var a Attendance
		var classID, checkIn, checkOut sql.NullString
		rows.Scan(&a.ID, &a.PersonID, &a.PersonType, &a.Date, &classID, &checkIn, &checkOut, &a.Status)
		if classID.Valid {
			a.ClassID = &classID.String
		}
		if checkIn.Valid {
			a.CheckIn = &checkIn.String
		}
		if checkOut.Valid {
			a.CheckOut = &checkOut.String
		}
		out = append(out, a)
	}
	return out, total
}

func handleAttendance(db *DB, hub *WSHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			p := parsePagination(r)
			if !p.Active {
				respond(w, listAttendance(db, c))
				return
			}
			data, total := listAttendancePaged(db, c, p)
			respond(w, PaginatedResponse{Data: data, Total: total, Limit: p.Limit, Offset: p.Offset})
		case http.MethodPost:
			var a Attendance
			if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			if a.ID == "" {
				a.ID = generateID("ATT")
			}
			if a.Date == "" {
				a.Date = today()
			}

			// Upsert: update existing record for same person+class+date, or insert
			var existingID string
			q := `SELECT id FROM attendance WHERE person_id=? AND date=?`
			args := []any{a.PersonID, a.Date}
			if a.ClassID != nil {
				q = `SELECT id FROM attendance WHERE person_id=? AND date=? AND class_id=?`
				args = append(args, *a.ClassID)
			}
			db.QueryRow(q, args...).Scan(&existingID)

			var classID, checkIn, checkOut any
			if a.ClassID != nil {
				classID = *a.ClassID
			}
			if a.CheckIn != nil {
				checkIn = *a.CheckIn
			}
			if a.CheckOut != nil {
				checkOut = *a.CheckOut
			}

			if existingID != "" {
				a.ID = existingID
				db.Exec(`UPDATE attendance SET check_in=?,check_out=?,status=? WHERE id=?`, checkIn, checkOut, a.Status, existingID)
			} else {
				tid := tenantID(c)
				db.Exec(`INSERT INTO attendance(id,tenant_id,person_id,person_type,date,class_id,check_in,check_out,status) VALUES(?,?,?,?,?,?,?,?,?)`,
					a.ID, tid, a.PersonID, a.PersonType, a.Date, classID, checkIn, checkOut, a.Status)
			}

			// Broadcast check-in/out event to WebSocket clients
			if hub != nil && a.PersonType == "student" {
				eventType := "CHECK_IN"
				if a.CheckOut != nil {
					eventType = "CHECK_OUT"
				}
				hub.broadcast(map[string]any{
					"type":     eventType,
					"personId": a.PersonID,
					"checkIn":  a.CheckIn,
					"checkOut": a.CheckOut,
					"date":     a.Date,
				})
			}
			respond(w, a)
		}
	}
}

// ── Payroll ───────────────────────────────────────────────────────────────────

func listPayroll(db *DB, c *Claims) []Payroll {
	if c != nil && c.Role == "parent" {
		return []Payroll{}
	}
	tid := tenantID(c)
	rows, err := db.Query(`SELECT id,staff_id,month,base_salary,bonus,deductions,total,status,paid_on FROM payroll WHERE (tenant_id=? OR ?=0) ORDER BY month DESC`, tid, tid)
	if err != nil {
		return []Payroll{}
	}
	defer rows.Close()
	out := []Payroll{}
	for rows.Next() {
		var p Payroll
		var paidOn sql.NullString
		rows.Scan(&p.ID, &p.StaffID, &p.Month, &p.BaseSalary, &p.Bonus, &p.Deductions, &p.Total, &p.Status, &paidOn)
		if paidOn.Valid {
			p.PaidOn = &paidOn.String
		}
		out = append(out, p)
	}
	return out
}

// ── User management (admin) ───────────────────────────────────────────────────

type userCreateReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
	Name     string `json:"name"`
}

func handleUsers(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			c := claimsFrom(r)
			tid := tenantID(c)
			rows, _ := db.Query(`SELECT id,email,role,name FROM users WHERE (tenant_id=? OR ?=0) ORDER BY role,name`, tid, tid)
			defer rows.Close()
			type userRow struct {
				ID    int    `json:"id"`
				Email string `json:"email"`
				Role  string `json:"role"`
				Name  string `json:"name"`
			}
			out := []userRow{}
			for rows.Next() {
				var u userRow
				rows.Scan(&u.ID, &u.Email, &u.Role, &u.Name)
				out = append(out, u)
			}
			respond(w, out)
		case http.MethodPost:
			var req userCreateReq
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			req.Email = strings.ToLower(strings.TrimSpace(req.Email))
			if !validateEmail(req.Email) {
				respondError(w, "invalid email format", http.StatusBadRequest)
				return
			}
			if ok, msg := validatePassword(req.Password); !ok {
				respondError(w, msg, http.StatusBadRequest)
				return
			}
			if req.Role == "" {
				req.Role = "parent"
			}
			hash, err := hashPassword(req.Password)
			if err != nil {
				respondError(w, "server error", 500)
				return
			}
			c := claimsFrom(r)
			tid := tenantID(c)
			_, err = db.Exec(`INSERT INTO users(tenant_id,email,password_hash,role,name) VALUES(?,?,?,?,?)`, tid, req.Email, hash, req.Role, req.Name)
			if err != nil {
				if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "duplicate key") {
					respondError(w, "email already exists", 409)
					return
				}
				respondError(w, "server error", 500)
				return
			}
			w.WriteHeader(http.StatusCreated)
			respond(w, map[string]string{"email": req.Email, "role": req.Role})
		}
	}
}

func handleUserDelete(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		tid := tenantID(claimsFrom(r))
		if _, err := db.Exec(`DELETE FROM users WHERE id=? AND (tenant_id=? OR ?=0)`, id, tid, tid); err != nil {
			respondError(w, "could not delete user", 500)
			return
		}
		if c := claimsFrom(r); c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "user_deleted", "user", id, "hard deleted")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── Registrations ─────────────────────────────────────────────────────────────

func listRegistrations(db *DB, c *Claims) []Registration {
	tid := tenantID(c)
	rows, err := db.Query(`SELECT id,parent_name,email,phone,emergency_name,emergency_phone,student_first_name,student_last_name,student_dob,student_gender,gender,school_name,year_grade,class_type_interest,subject_interest,school_fees,registration_date,workshop_interest,class_interest,notes,submitted_on,status,COALESCE(type,'student'),COALESCE(specialization,''),COALESCE(nric,''),COALESCE(display_name,''),COALESCE(employment_type,'Full-time'),COALESCE(experience,''),COALESCE(qualifications,''),COALESCE(bio,''),COALESCE(schedule,''),COALESCE(expected_salary,'') FROM registrations WHERE status='pending' AND (tenant_id=? OR ?=0) ORDER BY submitted_on DESC`, tid, tid)
	if err != nil {
		return []Registration{}
	}
	defer rows.Close()
	out := []Registration{}
	for rows.Next() {
		var reg Registration
		rows.Scan(&reg.ID, &reg.ParentName, &reg.Email, &reg.Phone, &reg.EmergencyName, &reg.EmergencyPhone,
			&reg.StudentFirstName, &reg.StudentLastName, &reg.StudentDOB, &reg.StudentGender,
			&reg.Gender, &reg.SchoolName, &reg.YearGrade, &reg.ClassTypeInterest, &reg.SubjectInterest,
			&reg.SchoolFees, &reg.RegistrationDate, &reg.WorkshopInterest,
			&reg.ClassInterest, &reg.Notes, &reg.SubmittedOn, &reg.Status, &reg.Type,
			&reg.Specialization, &reg.NRIC, &reg.DisplayName, &reg.EmploymentType, &reg.Experience, &reg.Qualifications, &reg.Bio, &reg.Schedule, &reg.ExpectedSalary)
		out = append(out, reg)
	}
	return out
}

// POST /api/register — public, no auth required
func handleRegister(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var reg Registration
		if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
			respondError(w, "bad request", 400)
			return
		}
		if reg.ParentName == "" || reg.Email == "" || reg.StudentFirstName == "" {
			respondError(w, "parent name, email and student first name are required", 400)
			return
		}
		if !validateEmail(reg.Email) {
			respondError(w, "invalid email address", 400)
			return
		}
		reg.ID = generateID("REG")
		reg.SubmittedOn = today()
		reg.Status = "pending"
		reg.Type = "student"
		_, err := db.Exec(`INSERT INTO registrations(id,parent_name,email,phone,emergency_name,emergency_phone,student_first_name,student_last_name,student_dob,student_gender,gender,school_name,year_grade,class_type_interest,subject_interest,school_fees,registration_date,workshop_interest,class_interest,notes,submitted_on,status,type) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			reg.ID, reg.ParentName, reg.Email, reg.Phone, reg.EmergencyName, reg.EmergencyPhone,
			reg.StudentFirstName, reg.StudentLastName, reg.StudentDOB, reg.StudentGender,
			reg.Gender, reg.SchoolName, reg.YearGrade, reg.ClassTypeInterest, reg.SubjectInterest,
			reg.SchoolFees, reg.RegistrationDate, reg.WorkshopInterest,
			reg.ClassInterest, reg.Notes, reg.SubmittedOn, reg.Status, reg.Type)
		if err != nil {
			respondError(w, "could not save registration", 500)
			return
		}
		w.WriteHeader(http.StatusCreated)
		respond(w, map[string]string{"id": reg.ID, "status": "pending"})
	}
}

// POST /api/registrations/{id}/approve — admin only
func handleRegistrationApprove(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		id := chi.URLParam(r, "id")
		var reg Registration
		err := db.QueryRow(`SELECT id,parent_name,email,phone,emergency_name,emergency_phone,student_first_name,student_last_name,student_dob,student_gender,class_interest,notes,COALESCE(type,'student'),COALESCE(specialization,''),COALESCE(nric,''),COALESCE(display_name,''),COALESCE(employment_type,'Full-time'),COALESCE(experience,''),COALESCE(qualifications,''),COALESCE(expected_salary,'') FROM registrations WHERE id=?`, id).
			Scan(&reg.ID, &reg.ParentName, &reg.Email, &reg.Phone, &reg.EmergencyName, &reg.EmergencyPhone,
				&reg.StudentFirstName, &reg.StudentLastName, &reg.StudentDOB, &reg.StudentGender,
				&reg.ClassInterest, &reg.Notes, &reg.Type,
				&reg.Specialization, &reg.NRIC, &reg.DisplayName, &reg.EmploymentType, &reg.Experience, &reg.Qualifications, &reg.ExpectedSalary)
		if err != nil {
			respondError(w, "registration not found", 404)
			return
		}

		// Generate temp password before starting transaction
		tid := tenantID(c)
		rawBytes := make([]byte, 8)
		if _, err := rand.Read(rawBytes); err != nil {
			respondError(w, "could not generate password", 500)
			return
		}
		tempPassword := "Sh-" + hex.EncodeToString(rawBytes)
		hash, err := hashPassword(tempPassword)
		if err != nil {
			respondError(w, "could not hash password", 500)
			return
		}

		// Start transaction
		tx, err := db.BeginTx(r.Context())
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		defer tx.Rollback()

		var responseData map[string]string

		if reg.Type == "teacher" {
			// Teacher registration: create staff record + teacher user account
			staffID := generateID("stf")
			displayName := reg.DisplayName
			if displayName == "" {
				displayName = reg.ParentName
			}
			shortName := strings.Split(displayName, " ")[0]
			empType := reg.EmploymentType
			if empType == "" {
				empType = "Full-time"
			}

			if _, err := tx.Exec(`INSERT INTO staff(id,tenant_id,name,full_name,role,email,phone,salary,join_date,status,specialization,nric,emergency_name,emergency_phone,employment_type) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				staffID, tid, shortName, displayName,
				"Teacher", reg.Email, reg.Phone, 0, today(), "Active",
				reg.Specialization, reg.NRIC,
				reg.EmergencyName, reg.EmergencyPhone, empType); err != nil {
				respondError(w, "could not create staff record", 500)
				return
			}
			if _, err := tx.Exec(`INSERT INTO users(tenant_id,email,password_hash,role,name) VALUES(?,?,?,?,?) ON CONFLICT(email) DO NOTHING`,
				tid, strings.ToLower(strings.TrimSpace(reg.Email)), hash, "teacher", displayName); err != nil {
				respondError(w, "could not create teacher account", 500)
				return
			}
			responseData = map[string]string{
				"staffId":      staffID,
				"tempPassword": tempPassword,
				"type":         "teacher",
				"message":      "Teacher added to staff. Share temp password.",
			}
		} else {
			// Student registration: create student + parent user account
			stuID := generateID("STU")
			if _, err := tx.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
				stuID, tid, reg.StudentFirstName, reg.StudentLastName, reg.StudentDOB, reg.StudentGender,
				reg.ParentName, reg.Email, reg.Phone, "The Study Hub", "New", today(), "[]", "[]", reg.Notes); err != nil {
				respondError(w, "could not create student record", 500)
				return
			}
			if _, err := tx.Exec(`INSERT INTO users(tenant_id,email,password_hash,role,name) VALUES(?,?,?,?,?) ON CONFLICT(email) DO NOTHING`,
				tid, strings.ToLower(strings.TrimSpace(reg.Email)), hash, "parent", reg.ParentName); err != nil {
				respondError(w, "could not create parent account", 500)
				return
			}

			// Find or create family for parent
			var famID string
			tx.QueryRow(`SELECT id FROM families WHERE contact=? AND (tenant_id=? OR ?=0) AND deleted_at IS NULL`, strings.ToLower(strings.TrimSpace(reg.Email)), tid, tid).Scan(&famID)
			if famID == "" {
				famID = generateID("FAM")
				familyName := reg.ParentName + " Family"
				if reg.ParentName == "" {
					familyName = reg.Email
				}
				tx.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name) VALUES(?,?,?,?,?,?)`,
					famID, tid, familyName, reg.Email, reg.Phone, reg.ParentName)
			}
			tx.Exec(`UPDATE students SET family_id=? WHERE id=?`, famID, stuID)

			responseData = map[string]string{
				"studentId":    stuID,
				"tempPassword": tempPassword,
				"type":         "student",
				"message":      "Student created. Share temp password with parent.",
			}
		}

		// Mark registration approved
		if _, err := tx.Exec(`UPDATE registrations SET status='approved' WHERE id=?`, id); err != nil {
			respondError(w, "could not update registration status", 500)
			return
		}

		if err := tx.Commit(); err != nil {
			respondError(w, "server error", 500)
			return
		}

		respond(w, responseData)
	}
}

// DELETE /api/registrations/{id} — admin only (reject)
func handleRegistrationReject(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		tid := tenantID(claimsFrom(r))
		if _, err := db.Exec(`UPDATE registrations SET status='rejected' WHERE id=? AND (tenant_id=? OR ?=0)`, id, tid, tid); err != nil {
			respondError(w, "could not reject registration", 500)
			return
		}
		if c := claimsFrom(r); c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "registration_rejected", "registration", id, "")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
