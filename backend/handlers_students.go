package main

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

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
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,COALESCE(medical_info,''),COALESCE(allergies,''),COALESCE(family_id,''),COALESCE(referred_by_family_id,'') FROM students WHERE contact=? AND (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY registered_on`, c.Email, tid, tid)
	} else {
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,COALESCE(medical_info,''),COALESCE(allergies,''),COALESCE(family_id,''),COALESCE(referred_by_family_id,'') FROM students WHERE (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY registered_on`, tid, tid)
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
		rows.Scan(&s.ID, &s.FirstName, &s.LastName, &s.DOB, &s.Gender, &s.ParentName, &s.Contact, &s.Phone, &s.Branch, &s.Status, &s.RegisteredOn, &ec, &sib, &s.Notes, &e2name, &e2phone, &s.MedicalInfo, &s.Allergies, &s.FamilyID, &s.ReferredByFamilyID)
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
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,COALESCE(medical_info,''),COALESCE(allergies,''),COALESCE(family_id,''),COALESCE(referred_by_family_id,'') FROM students WHERE contact=? AND (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY registered_on LIMIT ? OFFSET ?`, c.Email, tid, tid, p.Limit, p.Offset)
	} else {
		db.QueryRow(`SELECT COUNT(*) FROM students WHERE (tenant_id=? OR ?=0) AND deleted_at IS NULL`, tid, tid).Scan(&total)
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,COALESCE(medical_info,''),COALESCE(allergies,''),COALESCE(family_id,''),COALESCE(referred_by_family_id,'') FROM students WHERE (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY registered_on LIMIT ? OFFSET ?`, tid, tid, p.Limit, p.Offset)
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
		rows.Scan(&s.ID, &s.FirstName, &s.LastName, &s.DOB, &s.Gender, &s.ParentName, &s.Contact, &s.Phone, &s.Branch, &s.Status, &s.RegisteredOn, &ec, &sib, &s.Notes, &e2name, &e2phone, &s.MedicalInfo, &s.Allergies, &s.FamilyID, &s.ReferredByFamilyID)
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
					db.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name,referral_code) VALUES(?,?,?,?,?,?,?)`,
						famID, tid, familyName, s.Contact, s.Phone, s.ParentName, newReferralCode())
				}
				s.FamilyID = famID
			}
			_, err := db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,medical_info,allergies,family_id,referred_by_family_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				s.ID, tid, s.FirstName, s.LastName, s.DOB, s.Gender, s.ParentName, s.Contact, s.Phone, s.Branch, s.Status, s.RegisteredOn, jsonArr(s.EnrolledClasses), jsonArr(s.Siblings), s.Notes, s.Emergency2Name, s.Emergency2Phone, s.MedicalInfo, s.Allergies, s.FamilyID, s.ReferredByFamilyID)
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
