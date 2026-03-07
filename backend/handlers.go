package main

import (
	"database/sql"
	"encoding/json"
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

func generateID(prefix string) string {
	return prefix + "_" + strings.ReplaceAll(time.Now().Format("20060102150405.000"), ".", "")
}

// ── Snapshot (full data load) ─────────────────────────────────────────────────

func handleSnapshot(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		snap := Snapshot{
			Students:      listStudents(db, c),
			Classes:       listClasses(db),
			Staff:         listStaff(db, c),
			Invoices:      listInvoices(db, c),
			Announcements: listAnnouncements(db),
			Attendance:    listAttendance(db, c),
			Payroll:       listPayroll(db, c),
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

func listStudents(db *sql.DB, c *Claims) []Student {
	var rows *sql.Rows
	var err error
	tid := tenantID(c)
	if c != nil && c.Role == "parent" {
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes FROM students WHERE contact=? AND (tenant_id=? OR ?=0) ORDER BY registered_on`, c.Email, tid, tid)
	} else {
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes FROM students WHERE (tenant_id=? OR ?=0) ORDER BY registered_on`, tid, tid)
	}
	if err != nil {
		return []Student{}
	}
	defer rows.Close()
	out := []Student{}
	for rows.Next() {
		var s Student
		var ec, sib string
		rows.Scan(&s.ID, &s.FirstName, &s.LastName, &s.DOB, &s.Gender, &s.ParentName, &s.Contact, &s.Phone, &s.Branch, &s.Status, &s.RegisteredOn, &ec, &sib, &s.Notes)
		s.EnrolledClasses = parseArr(ec)
		s.Siblings = parseArr(sib)
		out = append(out, s)
	}
	return out
}

func handleStudents(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			respond(w, listStudents(db, c))
		case http.MethodPost:
			if c.Role != "admin" { http.Error(w, "admin only", 403); return }
			var s Student
			if err := json.NewDecoder(r.Body).Decode(&s); err != nil { http.Error(w, "bad body", 400); return }
			if s.ID == "" { s.ID = generateID("STU") }
			if s.RegisteredOn == "" { s.RegisteredOn = today() }
			_, err := db.Exec(`INSERT INTO students(id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				s.ID, s.FirstName, s.LastName, s.DOB, s.Gender, s.ParentName, s.Contact, s.Phone, s.Branch, s.Status, s.RegisteredOn, jsonArr(s.EnrolledClasses), jsonArr(s.Siblings), s.Notes)
			if err != nil { http.Error(w, err.Error(), 500); return }
			respond(w, s)
		}
	}
}

func handleStudent(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		switch r.Method {
		case http.MethodPut:
			var s Student
			json.NewDecoder(r.Body).Decode(&s)
			s.ID = id
			db.Exec(`UPDATE students SET first_name=?,last_name=?,dob=?,gender=?,parent_name=?,contact=?,phone=?,branch=?,status=?,enrolled_classes=?,siblings=?,notes=? WHERE id=?`,
				s.FirstName, s.LastName, s.DOB, s.Gender, s.ParentName, s.Contact, s.Phone, s.Branch, s.Status, jsonArr(s.EnrolledClasses), jsonArr(s.Siblings), s.Notes, id)
			respond(w, s)
		case http.MethodDelete:
			db.Exec(`DELETE FROM students WHERE id=?`, id)
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// ── Classes ───────────────────────────────────────────────────────────────────

func listClasses(db *sql.DB) []Class {
	rows, err := db.Query(`SELECT id,name,teacher_ids,classroom,day,time,end_time,capacity,enrolled,color FROM classes`)
	if err != nil { return []Class{} }
	defer rows.Close()
	out := []Class{}
	for rows.Next() {
		var c Class
		var tids string
		rows.Scan(&c.ID, &c.Name, &tids, &c.Classroom, &c.Day, &c.Time, &c.EndTime, &c.Capacity, &c.Enrolled, &c.Color)
		c.TeacherIDs = parseArr(tids)
		out = append(out, c)
	}
	return out
}

func handleClasses(db *sql.DB) http.HandlerFunc {
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
			json.NewDecoder(r.Body).Decode(&c)
			if c.ID == "" { c.ID = generateID("cls") }

			// ── Clash detection ────────────────────────────────────────────────
			// Two intervals [s1,e1) and [s2,e2) overlap when s1<e2 AND s2<e1
			for _, tid2 := range c.TeacherIDs {
				var cnt int
				db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND id!=? AND time<?  AND end_time>? AND teacher_ids LIKE '%'||?||'%'`,
					c.Day, c.ID, c.EndTime, c.Time, tid2).Scan(&cnt)
				if cnt > 0 {
					http.Error(w, "Conflict: teacher "+tid2+" is already booked at this time", http.StatusConflict)
					return
				}
			}
			if c.Classroom != "" {
				var cnt int
				db.QueryRow(`SELECT COUNT(*) FROM classes WHERE day=? AND classroom=? AND id!=? AND time<? AND end_time>?`,
					c.Day, c.Classroom, c.ID, c.EndTime, c.Time).Scan(&cnt)
				if cnt > 0 {
					http.Error(w, "Conflict: "+c.Classroom+" is already booked at this time", http.StatusConflict)
					return
				}
			}

			db.Exec(`INSERT INTO classes(id,name,teacher_ids,classroom,day,time,end_time,capacity,enrolled,color) VALUES(?,?,?,?,?,?,?,?,?,?)`,
				c.ID, c.Name, jsonArr(c.TeacherIDs), c.Classroom, c.Day, c.Time, c.EndTime, c.Capacity, c.Enrolled, c.Color)
			respond(w, c)
		}
	}
}

// ── Staff ─────────────────────────────────────────────────────────────────────

func listStaff(db *sql.DB, c *Claims) []Staff {
	rows, err := db.Query(`SELECT id,name,full_name,role,email,phone,salary,join_date,status FROM staff`)
	if err != nil { return []Staff{} }
	defer rows.Close()
	out := []Staff{}
	for rows.Next() {
		var s Staff
		rows.Scan(&s.ID, &s.Name, &s.FullName, &s.Role, &s.Email, &s.Phone, &s.Salary, &s.JoinDate, &s.Status)
		if c != nil && c.Role == "parent" {
			s.Salary = 0 // hide salary from parents
		}
		out = append(out, s)
	}
	return out
}

func handleStaff(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			respond(w, listStaff(db, c))
		case http.MethodPost:
			if c.Role != "admin" { http.Error(w, "admin only", 403); return }
			var s Staff
			json.NewDecoder(r.Body).Decode(&s)
			if s.ID == "" { s.ID = generateID("stf") }
			db.Exec(`INSERT INTO staff(id,name,full_name,role,email,phone,salary,join_date,status) VALUES(?,?,?,?,?,?,?,?,?)`,
				s.ID, s.Name, s.FullName, s.Role, s.Email, s.Phone, s.Salary, s.JoinDate, s.Status)
			respond(w, s)
		}
	}
}

// ── Invoices ──────────────────────────────────────────────────────────────────

func listInvoices(db *sql.DB, c *Claims) []Invoice {
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		rows, err = db.Query(`SELECT i.id,i.student_id,i.description,i.type,i.amount,i.due_date,i.status,i.created_on,i.paid_on FROM invoices i JOIN students s ON s.id=i.student_id WHERE s.contact=? ORDER BY i.created_on DESC`, c.Email)
	} else {
		rows, err = db.Query(`SELECT id,student_id,description,type,amount,due_date,status,created_on,paid_on FROM invoices ORDER BY created_on DESC`)
	}
	if err != nil { return []Invoice{} }
	defer rows.Close()
	out := []Invoice{}
	for rows.Next() {
		var inv Invoice
		var paidOn sql.NullString
		rows.Scan(&inv.ID, &inv.StudentID, &inv.Description, &inv.Type, &inv.Amount, &inv.DueDate, &inv.Status, &inv.CreatedOn, &paidOn)
		if paidOn.Valid { inv.PaidOn = &paidOn.String }
		out = append(out, inv)
	}
	return out
}

func handleInvoices(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			respond(w, listInvoices(db, c))
		case http.MethodPost:
			if c.Role != "admin" { http.Error(w, "admin only", 403); return }
			var inv Invoice
			json.NewDecoder(r.Body).Decode(&inv)
			if inv.ID == "" { inv.ID = generateID("INV") }
			if inv.CreatedOn == "" { inv.CreatedOn = today() }
			db.Exec(`INSERT INTO invoices(id,student_id,description,type,amount,due_date,status,created_on,paid_on) VALUES(?,?,?,?,?,?,?,?,?)`,
				inv.ID, inv.StudentID, inv.Description, inv.Type, inv.Amount, inv.DueDate, "Unpaid", inv.CreatedOn, nil)
			respond(w, inv)
		}
	}
}

func handleInvoicePay(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		t := today()
		db.Exec(`UPDATE invoices SET status='Paid', paid_on=? WHERE id=?`, t, id)
		respond(w, map[string]string{"status": "Paid", "paidOn": t})
	}
}

// ── Announcements ─────────────────────────────────────────────────────────────

func listAnnouncements(db *sql.DB) []Announcement {
	rows, err := db.Query(`SELECT id,title,message,audience,type,created_on,created_by FROM announcements ORDER BY created_on DESC`)
	if err != nil { return []Announcement{} }
	defer rows.Close()
	out := []Announcement{}
	for rows.Next() {
		var a Announcement
		rows.Scan(&a.ID, &a.Title, &a.Message, &a.Audience, &a.Type, &a.CreatedOn, &a.CreatedBy)
		out = append(out, a)
	}
	return out
}

func handleAnnouncements(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			respond(w, listAnnouncements(db))
		case http.MethodPost:
			if c.Role != "admin" { http.Error(w, "admin only", 403); return }
			var a Announcement
			json.NewDecoder(r.Body).Decode(&a)
			if a.ID == "" { a.ID = generateID("ANN") }
			if a.CreatedOn == "" { a.CreatedOn = today() }
			if a.CreatedBy == "" { a.CreatedBy = c.Name }
			db.Exec(`INSERT INTO announcements(id,title,message,audience,type,created_on,created_by) VALUES(?,?,?,?,?,?,?)`,
				a.ID, a.Title, a.Message, a.Audience, a.Type, a.CreatedOn, a.CreatedBy)
			respond(w, a)
		}
	}
}

func handleAnnouncementDelete(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		db.Exec(`DELETE FROM announcements WHERE id=?`, chi.URLParam(r, "id"))
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── Attendance ────────────────────────────────────────────────────────────────

func listAttendance(db *sql.DB, c *Claims) []Attendance {
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

func handleAttendance(db *sql.DB, hub *WSHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			respond(w, listAttendance(db, c))
		case http.MethodPost:
			var a Attendance
			json.NewDecoder(r.Body).Decode(&a)
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

func listPayroll(db *sql.DB, c *Claims) []Payroll {
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

func handleUsers(db *sql.DB) http.HandlerFunc {
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
			json.NewDecoder(r.Body).Decode(&req)
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
				if strings.Contains(err.Error(), "UNIQUE") {
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

func handleUserDelete(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		db.Exec(`DELETE FROM users WHERE id=?`, chi.URLParam(r, "id"))
		w.WriteHeader(http.StatusNoContent)
	}
}
