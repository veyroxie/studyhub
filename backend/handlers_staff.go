package main

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

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

func handleStaffByID(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		id := chi.URLParam(r, "id")
		switch r.Method {
		case http.MethodPut:
			if c == nil || c.Role != "admin" {
				respondError(w, "admin only", 403)
				return
			}
			var s Staff
			if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			s.ID = id
			if s.EmploymentType == "" {
				s.EmploymentType = "Full-time"
			}
			tid := tenantID(c)
			db.Exec(`UPDATE staff SET name=?,full_name=?,role=?,email=?,phone=?,salary=?,join_date=?,status=?,specialization=?,nric=?,emergency_name=?,emergency_phone=?,employment_type=?,hourly_rate=? WHERE id=? AND (tenant_id=? OR ?=0)`,
				s.Name, s.FullName, s.Role, s.Email, s.Phone, s.Salary, s.JoinDate, s.Status, s.Specialization, s.NRIC, s.EmergencyName, s.EmergencyPhone, s.EmploymentType, s.HourlyRate, id, tid, tid)
			if c != nil {
				db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
					c.Email, "staff_updated", "staff", id, s.Name)
			}
			respond(w, s)

		case http.MethodDelete:
			if c == nil || c.Role != "admin" {
				respondError(w, "admin only", 403)
				return
			}
			tid := tenantID(c)
			db.Exec(`UPDATE staff SET deleted_at=NOW() WHERE id=? AND (tenant_id=? OR ?=0)`, id, tid, tid)
			if c != nil {
				db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
					c.Email, "staff_deleted", "staff", id, "soft deleted")
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}
}
