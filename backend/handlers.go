package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
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
	if len(errs) == 0 { return "" }
	return "missing required fields: " + strings.Join(errs, ", ")
}

// validAmount returns true if amount is a positive number.
func validAmount(a float64) bool { return a > 0 }

func generateID(prefix string) string {
	return prefix + "_" + strings.ReplaceAll(time.Now().Format("20060102150405.000"), ".", "")
}

// ── Snapshot (full data load) ─────────────────────────────────────────────────

func handleSnapshot(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		snap := Snapshot{
			Students:           listStudents(db, c),
			Classes:            listClasses(db),
			Staff:              listStaff(db, c),
			Invoices:           listInvoices(db, c),
			Announcements:      listAnnouncements(db),
			Attendance:         listAttendance(db, c),
			Payroll:            listPayroll(db, c),
			Feedback:           listFeedback(db),
			Subjects:           listSubjects(db),
			Workshops:          listWorkshops(db),
			SelfStudySessions:  listSelfStudy(db),
			PerformanceReviews: listPerformanceReviews(db),
			CancelledClasses:   listCancelledClasses(db),
			Holidays:           listHolidays(db),
		}
		if c != nil && (c.Role == "admin" || c.Role == "superadmin") {
			snap.Registrations = listRegistrations(db)
		}
		respond(w, snap)
	}
}

// ── Students ──────────────────────────────────────────────────────────────────

func tenantID(c *Claims) int {
	if c == nil { return 1 }
	if c.TenantID == 0 { return 0 } // 0 = superadmin, cross-tenant
	return c.TenantID
}

func listStudents(db *DB, c *Claims) []Student {
	var rows *sql.Rows
	var err error
	tid := tenantID(c)
	if c != nil && c.Role == "parent" {
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,COALESCE(medical_info,''),COALESCE(allergies,'') FROM students WHERE contact=? AND (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY registered_on`, c.Email, tid, tid)
	} else {
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,COALESCE(medical_info,''),COALESCE(allergies,'') FROM students WHERE (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY registered_on`, tid, tid)
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
		rows.Scan(&s.ID, &s.FirstName, &s.LastName, &s.DOB, &s.Gender, &s.ParentName, &s.Contact, &s.Phone, &s.Branch, &s.Status, &s.RegisteredOn, &ec, &sib, &s.Notes, &e2name, &e2phone, &s.MedicalInfo, &s.Allergies)
		s.EnrolledClasses = parseArr(ec)
		s.Siblings = parseArr(sib)
		s.Emergency2Name = nullStr(e2name)
		s.Emergency2Phone = nullStr(e2phone)
		out = append(out, s)
	}
	return out
}

func handleStudents(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			respond(w, listStudents(db, c))
		case http.MethodPost:
			if c.Role != "admin" { http.Error(w, "admin only", 403); return }
			var s Student
			if err := json.NewDecoder(r.Body).Decode(&s); err != nil { http.Error(w, "bad body", 400); return }
			if msg := validationError("firstName", s.FirstName, "lastName", s.LastName, "contact", s.Contact); msg != "" {
				http.Error(w, msg, 400); return
			}
			if s.Status == "" { s.Status = "New" }
			if s.ID == "" { s.ID = generateID("STU") }
			if s.RegisteredOn == "" { s.RegisteredOn = today() }
			_, err := db.Exec(`INSERT INTO students(id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,medical_info,allergies) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				s.ID, s.FirstName, s.LastName, s.DOB, s.Gender, s.ParentName, s.Contact, s.Phone, s.Branch, s.Status, s.RegisteredOn, jsonArr(s.EnrolledClasses), jsonArr(s.Siblings), s.Notes, s.Emergency2Name, s.Emergency2Phone, s.MedicalInfo, s.Allergies)
			if err != nil { http.Error(w, err.Error(), 500); return }
			respond(w, s)
		}
	}
}

func handleStudent(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			http.Error(w, "admin only", 403); return
		}
		id := chi.URLParam(r, "id")
		switch r.Method {
		case http.MethodPut:
			var s Student
			if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
				http.Error(w, "bad body", 400); return
			}
			s.ID = id
			if _, err := db.Exec(`UPDATE students SET first_name=?,last_name=?,dob=?,gender=?,parent_name=?,contact=?,phone=?,branch=?,status=?,enrolled_classes=?,siblings=?,notes=?,emergency2_name=?,emergency2_phone=?,medical_info=?,allergies=? WHERE id=?`,
				s.FirstName, s.LastName, s.DOB, s.Gender, s.ParentName, s.Contact, s.Phone, s.Branch, s.Status, jsonArr(s.EnrolledClasses), jsonArr(s.Siblings), s.Notes, s.Emergency2Name, s.Emergency2Phone, s.MedicalInfo, s.Allergies, id); err != nil {
				http.Error(w, "could not update student", 500); return
			}
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "student_updated", "student", id, s.FirstName+" "+s.LastName)
			respond(w, s)
		case http.MethodDelete:
			if _, err := db.Exec(`UPDATE students SET deleted_at=NOW() WHERE id=?`, id); err != nil {
				http.Error(w, "could not delete student", 500); return
			}
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "student_deleted", "student", id, "soft deleted")
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// ── Classes ───────────────────────────────────────────────────────────────────

func listClasses(db *DB) []Class {
	rows, err := db.Query(`SELECT id,name,teacher_ids,classroom,day,time,end_time,capacity,enrolled,color,category FROM classes WHERE deleted_at IS NULL`)
	if err != nil { return []Class{} }
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
		switch r.Method {
		case http.MethodGet:
			respond(w, listClasses(db))
		case http.MethodPost:
			cl := claimsFrom(r)
			if cl == nil || (cl.Role != "admin" && cl.Role != "superadmin") {
				http.Error(w, "admin only", 403); return
			}
			var c Class
			if err := json.NewDecoder(r.Body).Decode(&c); err != nil { http.Error(w, "bad body", 400); return }
			if msg := validationError("name", c.Name, "day", c.Day, "time", c.Time, "endTime", c.EndTime); msg != "" {
				http.Error(w, msg, 400); return
			}
			if c.Capacity < 1 { c.Capacity = 5 }
			if c.Time >= c.EndTime { http.Error(w, "end time must be after start time", 400); return }
			if c.ID == "" { c.ID = generateID("cls") }

			// ── Clash detection ────────────────────────────────────────────────
			// Two intervals [s1,e1) and [s2,e2) overlap when s1<e2 AND s2<e1
			for _, tid2 := range c.TeacherIDs {
				var cnt int
				db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND id!=? AND time<?  AND end_time>? AND teacher_ids LIKE '%'||?||'%' AND deleted_at IS NULL`,
					c.Day, c.ID, c.EndTime, c.Time, tid2).Scan(&cnt)
				if cnt > 0 {
					http.Error(w, "Conflict: teacher "+tid2+" is already booked at this time", http.StatusConflict)
					return
				}
			}
			if c.Classroom != "" {
				var cnt int
				db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND classroom=? AND id!=? AND time<? AND end_time>? AND deleted_at IS NULL`,
					c.Day, c.Classroom, c.ID, c.EndTime, c.Time).Scan(&cnt)
				if cnt > 0 {
					http.Error(w, "Conflict: "+c.Classroom+" is already booked at this time", http.StatusConflict)
					return
				}
			}

			if c.Category == "" { c.Category = "Academic" }
			db.Exec(`INSERT INTO classes(id,name,teacher_ids,classroom,day,time,end_time,capacity,enrolled,color,category) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
				c.ID, c.Name, jsonArr(c.TeacherIDs), c.Classroom, c.Day, c.Time, c.EndTime, c.Capacity, c.Enrolled, c.Color, c.Category)
			respond(w, c)
		}
	}
}

// ── Staff ─────────────────────────────────────────────────────────────────────

func listStaff(db *DB, c *Claims) []Staff {
	rows, err := db.Query(`SELECT id,name,full_name,role,email,phone,salary,join_date,status,specialization,nric,emergency_name,emergency_phone,employment_type,hourly_rate FROM staff`)
	if err != nil { return []Staff{} }
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
		if hourlyRate.Valid { s.HourlyRate = hourlyRate.Float64 }
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
			if c.Role != "admin" { http.Error(w, "admin only", 403); return }
			var s Staff
			if err := json.NewDecoder(r.Body).Decode(&s); err != nil { http.Error(w, "bad body", 400); return }
			if msg := validationError("name", s.Name, "fullName", s.FullName, "role", s.Role, "email", s.Email); msg != "" {
				http.Error(w, msg, 400); return
			}
			if s.Status == "" { s.Status = "Active" }
			if s.ID == "" { s.ID = generateID("stf") }
			if s.EmploymentType == "" { s.EmploymentType = "Full-time" }
			db.Exec(`INSERT INTO staff(id,name,full_name,role,email,phone,salary,join_date,status,specialization,nric,emergency_name,emergency_phone,employment_type,hourly_rate) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				s.ID, s.Name, s.FullName, s.Role, s.Email, s.Phone, s.Salary, s.JoinDate, s.Status, s.Specialization, s.NRIC, s.EmergencyName, s.EmergencyPhone, s.EmploymentType, s.HourlyRate)
			respond(w, s)
		}
	}
}

// ── Invoices ──────────────────────────────────────────────────────────────────

func listInvoices(db *DB, c *Claims) []Invoice {
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		rows, err = db.Query(`SELECT i.id,i.student_id,i.description,i.type,i.amount,i.due_date,i.status,i.created_on,i.paid_on,i.payment_proof FROM invoices i JOIN students s ON s.id=i.student_id WHERE s.contact=? AND i.deleted_at IS NULL ORDER BY i.created_on DESC`, c.Email)
	} else {
		rows, err = db.Query(`SELECT id,student_id,description,type,amount,due_date,status,created_on,paid_on,payment_proof FROM invoices WHERE deleted_at IS NULL ORDER BY created_on DESC`)
	}
	if err != nil { return []Invoice{} }
	defer rows.Close()
	out := []Invoice{}
	for rows.Next() {
		var inv Invoice
		var paidOn, proof sql.NullString
		rows.Scan(&inv.ID, &inv.StudentID, &inv.Description, &inv.Type, &inv.Amount, &inv.DueDate, &inv.Status, &inv.CreatedOn, &paidOn, &proof)
		if paidOn.Valid { inv.PaidOn = &paidOn.String }
		inv.PaymentProof = nullStr(proof)
		out = append(out, inv)
	}
	return out
}

func handleInvoices(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			respond(w, listInvoices(db, c))
		case http.MethodPost:
			if c.Role != "admin" { http.Error(w, "admin only", 403); return }
			var inv Invoice
			if err := json.NewDecoder(r.Body).Decode(&inv); err != nil { http.Error(w, "bad body", 400); return }
			if msg := validationError("studentId", inv.StudentID, "description", inv.Description, "dueDate", inv.DueDate); msg != "" {
				http.Error(w, msg, 400); return
			}
			if !validAmount(inv.Amount) { http.Error(w, "amount must be greater than 0", 400); return }
			if inv.ID == "" { inv.ID = generateID("INV") }
			if inv.CreatedOn == "" { inv.CreatedOn = today() }
			db.Exec(`INSERT INTO invoices(id,student_id,description,type,amount,due_date,status,created_on,paid_on) VALUES(?,?,?,?,?,?,?,?,?)`,
				inv.ID, inv.StudentID, inv.Description, inv.Type, inv.Amount, inv.DueDate, "Unpaid", inv.CreatedOn, nil)
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
			http.Error(w, "invoice not found", 404); return
		}
		if c != nil && c.Role == "parent" {
			var ownerEmail string
			db.QueryRow(`SELECT contact FROM students WHERE id=?`, studentID).Scan(&ownerEmail)
			if ownerEmail != c.Email {
				http.Error(w, "not your invoice", 403); return
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
		if _, err := db.Exec(`UPDATE invoices SET status=?, paid_on=? WHERE id=?`, newStatus, t, id); err != nil {
			http.Error(w, "could not update invoice", 500); return
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
		rows, err := db.Query(`SELECT id,actor_email,action,entity_type,entity_id,detail,created_at FROM audit_logs ORDER BY created_at DESC LIMIT 200`)
		if err != nil { respond(w, []any{}); return }
		defer rows.Close()
		type LogEntry struct {
			ID         int    `json:"id"`
			ActorEmail string `json:"actorEmail"`
			Action     string `json:"action"`
			EntityType string `json:"entityType"`
			EntityID   string `json:"entityId"`
			Detail     string `json:"detail"`
			CreatedAt  string `json:"createdAt"`
		}
		out := []LogEntry{}
		for rows.Next() {
			var e LogEntry
			rows.Scan(&e.ID, &e.ActorEmail, &e.Action, &e.EntityType, &e.EntityID, &e.Detail, &e.CreatedAt)
			out = append(out, e)
		}
		respond(w, out)
	}
}

// ── Announcements ─────────────────────────────────────────────────────────────

func listAnnouncements(db *DB) []Announcement {
	rows, err := db.Query(`SELECT id,title,message,audience,type,created_on,created_by,status,archive_on FROM announcements ORDER BY created_on DESC`)
	if err != nil { return []Announcement{} }
	defer rows.Close()
	out := []Announcement{}
	for rows.Next() {
		var a Announcement
		var status, archiveOn sql.NullString
		rows.Scan(&a.ID, &a.Title, &a.Message, &a.Audience, &a.Type, &a.CreatedOn, &a.CreatedBy, &status, &archiveOn)
		a.Status = nullStr(status)
		if a.Status == "" { a.Status = "published" }
		a.ArchiveOn = nullStr(archiveOn)
		out = append(out, a)
	}
	return out
}

func handleAnnouncements(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			respond(w, listAnnouncements(db))
		case http.MethodPost:
			if c.Role != "admin" { http.Error(w, "admin only", 403); return }
			var a Announcement
			if err := json.NewDecoder(r.Body).Decode(&a); err != nil { http.Error(w, "bad body", 400); return }
			if msg := validationError("title", a.Title, "message", a.Message); msg != "" {
				http.Error(w, msg, 400); return
			}
			if a.ID == "" { a.ID = generateID("ANN") }
			if a.CreatedOn == "" { a.CreatedOn = today() }
			if a.CreatedBy == "" { a.CreatedBy = c.Name }
			if a.Status == "" { a.Status = "published" }
			db.Exec(`INSERT INTO announcements(id,title,message,audience,type,created_on,created_by,status,archive_on) VALUES(?,?,?,?,?,?,?,?,?)`,
				a.ID, a.Title, a.Message, a.Audience, a.Type, a.CreatedOn, a.CreatedBy, a.Status, a.ArchiveOn)
			respond(w, a)
		}
	}
}

func handleAnnouncementDelete(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			http.Error(w, "admin only", 403); return
		}
		id := chi.URLParam(r, "id")
		if _, err := db.Exec(`DELETE FROM announcements WHERE id=?`, id); err != nil {
			http.Error(w, "could not delete announcement", 500); return
		}
		db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
			c.Email, "announcement_deleted", "announcement", id, "hard deleted")
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── Attendance ────────────────────────────────────────────────────────────────

func listAttendance(db *DB, c *Claims) []Attendance {
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		rows, err = db.Query(`SELECT a.id,a.person_id,a.person_type,a.date,a.class_id,a.check_in,a.check_out,a.status FROM attendance a JOIN students s ON s.id=a.person_id WHERE a.person_type='student' AND s.contact=? ORDER BY a.date DESC`, c.Email)
	} else {
		rows, err = db.Query(`SELECT id,person_id,person_type,date,class_id,check_in,check_out,status FROM attendance ORDER BY date DESC`)
	}
	if err != nil { return []Attendance{} }
	defer rows.Close()
	out := []Attendance{}
	for rows.Next() {
		var a Attendance
		var classID, checkIn, checkOut sql.NullString
		rows.Scan(&a.ID, &a.PersonID, &a.PersonType, &a.Date, &classID, &checkIn, &checkOut, &a.Status)
		if classID.Valid { a.ClassID = &classID.String }
		if checkIn.Valid { a.CheckIn = &checkIn.String }
		if checkOut.Valid { a.CheckOut = &checkOut.String }
		out = append(out, a)
	}
	return out
}

func handleAttendance(db *DB, hub *WSHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			respond(w, listAttendance(db, c))
		case http.MethodPost:
			var a Attendance
			if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
				http.Error(w, "bad body", 400); return
			}
			if a.ID == "" { a.ID = generateID("ATT") }
			if a.Date == "" { a.Date = today() }

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
			if a.ClassID != nil { classID = *a.ClassID }
			if a.CheckIn != nil { checkIn = *a.CheckIn }
			if a.CheckOut != nil { checkOut = *a.CheckOut }

			if existingID != "" {
				a.ID = existingID
				db.Exec(`UPDATE attendance SET check_in=?,check_out=?,status=? WHERE id=?`, checkIn, checkOut, a.Status, existingID)
			} else {
				db.Exec(`INSERT INTO attendance(id,person_id,person_type,date,class_id,check_in,check_out,status) VALUES(?,?,?,?,?,?,?,?)`,
					a.ID, a.PersonID, a.PersonType, a.Date, classID, checkIn, checkOut, a.Status)
			}

			// Broadcast check-in/out event to WebSocket clients
			if hub != nil && a.PersonType == "student" {
				eventType := "CHECK_IN"
				if a.CheckOut != nil { eventType = "CHECK_OUT" }
				hub.broadcast(map[string]any{
					"type":      eventType,
					"personId":  a.PersonID,
					"checkIn":   a.CheckIn,
					"checkOut":  a.CheckOut,
					"date":      a.Date,
				})
			}
			respond(w, a)
		}
	}
}

// ── Payroll ───────────────────────────────────────────────────────────────────

func listPayroll(db *DB, c *Claims) []Payroll {
	if c != nil && c.Role == "parent" { return []Payroll{} }
	rows, err := db.Query(`SELECT id,staff_id,month,base_salary,bonus,deductions,total,status,paid_on FROM payroll ORDER BY month DESC`)
	if err != nil { return []Payroll{} }
	defer rows.Close()
	out := []Payroll{}
	for rows.Next() {
		var p Payroll
		var paidOn sql.NullString
		rows.Scan(&p.ID, &p.StaffID, &p.Month, &p.BaseSalary, &p.Bonus, &p.Deductions, &p.Total, &p.Status, &paidOn)
		if paidOn.Valid { p.PaidOn = &paidOn.String }
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
			rows, _ := db.Query(`SELECT id,email,role,name FROM users ORDER BY role,name`)
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
				http.Error(w, "bad body", 400); return
			}
			req.Email = strings.ToLower(strings.TrimSpace(req.Email))
			if !validateEmail(req.Email) {
				http.Error(w, "invalid email format", http.StatusBadRequest)
				return
			}
			if ok, msg := validatePassword(req.Password); !ok {
				http.Error(w, msg, http.StatusBadRequest)
				return
			}
			if req.Role == "" { req.Role = "parent" }
			hash, err := hashPassword(req.Password)
			if err != nil { http.Error(w, "hash error", 500); return }
			_, err = db.Exec(`INSERT INTO users(email,password_hash,role,name) VALUES(?,?,?,?)`, req.Email, hash, req.Role, req.Name)
			if err != nil {
				if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "duplicate key") {
					http.Error(w, "email already exists", 409)
					return
				}
				http.Error(w, err.Error(), 500)
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
		if _, err := db.Exec(`DELETE FROM users WHERE id=?`, id); err != nil {
			http.Error(w, "could not delete user", 500); return
		}
		if c := claimsFrom(r); c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "user_deleted", "user", id, "hard deleted")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── Registrations ─────────────────────────────────────────────────────────────

func listRegistrations(db *DB) []Registration {
	rows, err := db.Query(`SELECT id,parent_name,email,phone,emergency_name,emergency_phone,student_first_name,student_last_name,student_dob,student_gender,gender,school_name,year_grade,class_type_interest,subject_interest,school_fees,registration_date,workshop_interest,class_interest,notes,submitted_on,status FROM registrations WHERE status='pending' ORDER BY submitted_on DESC`)
	if err != nil { return []Registration{} }
	defer rows.Close()
	out := []Registration{}
	for rows.Next() {
		var reg Registration
		rows.Scan(&reg.ID, &reg.ParentName, &reg.Email, &reg.Phone, &reg.EmergencyName, &reg.EmergencyPhone,
			&reg.StudentFirstName, &reg.StudentLastName, &reg.StudentDOB, &reg.StudentGender,
			&reg.Gender, &reg.SchoolName, &reg.YearGrade, &reg.ClassTypeInterest, &reg.SubjectInterest,
			&reg.SchoolFees, &reg.RegistrationDate, &reg.WorkshopInterest,
			&reg.ClassInterest, &reg.Notes, &reg.SubmittedOn, &reg.Status)
		out = append(out, reg)
	}
	return out
}

// POST /api/register — public, no auth required
func handleRegister(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var reg Registration
		if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
			http.Error(w, "bad request", 400); return
		}
		if reg.ParentName == "" || reg.Email == "" || reg.StudentFirstName == "" {
			http.Error(w, "parent name, email and student first name are required", 400); return
		}
		if !validateEmail(reg.Email) {
			http.Error(w, "invalid email address", 400); return
		}
		reg.ID = generateID("REG")
		reg.SubmittedOn = today()
		reg.Status = "pending"
		_, err := db.Exec(`INSERT INTO registrations(id,parent_name,email,phone,emergency_name,emergency_phone,student_first_name,student_last_name,student_dob,student_gender,gender,school_name,year_grade,class_type_interest,subject_interest,school_fees,registration_date,workshop_interest,class_interest,notes,submitted_on,status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			reg.ID, reg.ParentName, reg.Email, reg.Phone, reg.EmergencyName, reg.EmergencyPhone,
			reg.StudentFirstName, reg.StudentLastName, reg.StudentDOB, reg.StudentGender,
			reg.Gender, reg.SchoolName, reg.YearGrade, reg.ClassTypeInterest, reg.SubjectInterest,
			reg.SchoolFees, reg.RegistrationDate, reg.WorkshopInterest,
			reg.ClassInterest, reg.Notes, reg.SubmittedOn, reg.Status)
		if err != nil { http.Error(w, "could not save registration", 500); return }
		w.WriteHeader(http.StatusCreated)
		respond(w, map[string]string{"id": reg.ID, "status": "pending"})
	}
}

// POST /api/registrations/{id}/approve — admin only
func handleRegistrationApprove(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var reg Registration
		err := db.QueryRow(`SELECT id,parent_name,email,phone,emergency_name,emergency_phone,student_first_name,student_last_name,student_dob,student_gender,class_interest,notes FROM registrations WHERE id=?`, id).
			Scan(&reg.ID, &reg.ParentName, &reg.Email, &reg.Phone, &reg.EmergencyName, &reg.EmergencyPhone,
				&reg.StudentFirstName, &reg.StudentLastName, &reg.StudentDOB, &reg.StudentGender,
				&reg.ClassInterest, &reg.Notes)
		if err != nil { http.Error(w, "registration not found", 404); return }

		// Create student record
		stuID := generateID("STU")
		if _, err := db.Exec(`INSERT INTO students(id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
			stuID, reg.StudentFirstName, reg.StudentLastName, reg.StudentDOB, reg.StudentGender,
			reg.ParentName, reg.Email, reg.Phone, "The Study Hub", "New", today(), "[]", "[]", reg.Notes); err != nil {
			http.Error(w, "could not create student record", 500); return
		}

		// Create parent user account with cryptographically random temp password
		rawBytes := make([]byte, 8)
		if _, err := rand.Read(rawBytes); err != nil {
			http.Error(w, "could not generate password", 500); return
		}
		tempPassword := "Sh-" + hex.EncodeToString(rawBytes) // e.g. Sh-a3f8c2d1e4b59067
		hash, err := hashPassword(tempPassword)
		if err != nil { http.Error(w, "could not hash password", 500); return }
		if _, err := db.Exec(`INSERT INTO users(email,password_hash,role,name) VALUES(?,?,?,?) ON CONFLICT(email) DO NOTHING`,
			strings.ToLower(strings.TrimSpace(reg.Email)), hash, "parent", reg.ParentName); err != nil {
			http.Error(w, "could not create parent account", 500); return
		}

		// Mark registration approved
		if _, err := db.Exec(`UPDATE registrations SET status='approved' WHERE id=?`, id); err != nil {
			http.Error(w, "could not update registration status", 500); return
		}

		respond(w, map[string]string{
			"studentId":    stuID,
			"tempPassword": tempPassword,
			"message":      "Student created. Share temp password with parent.",
		})
	}
}

// DELETE /api/registrations/{id} — admin only (reject)
func handleRegistrationReject(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if _, err := db.Exec(`UPDATE registrations SET status='rejected' WHERE id=?`, id); err != nil {
			http.Error(w, "could not reject registration", 500); return
		}
		if c := claimsFrom(r); c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "registration_rejected", "registration", id, "")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
