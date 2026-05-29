package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ensureTeacherUserAccount creates a teacher login row + email if the staff
// member doesn't already have one. Lets Add Staff produce a usable login
// without a separate self-register-then-approve dance.
func ensureTeacherUserAccount(db *DB, r *http.Request, tid int, email, fullName string) {
	if email == "" {
		return
	}
	var existing int
	db.QueryRow(`SELECT id FROM users WHERE email=?`, email).Scan(&existing)
	if existing > 0 {
		return
	}
	placeholderBytes := make([]byte, 32)
	if _, err := rand.Read(placeholderBytes); err != nil {
		logger.Error("ensureTeacherUserAccount: rand failed", "err", err)
		return
	}
	placeholderHash, err := hashPassword(hex.EncodeToString(placeholderBytes))
	if err != nil {
		logger.Error("ensureTeacherUserAccount: hash failed", "err", err)
		return
	}
	var userID int64
	if err := db.QueryRow(`INSERT INTO users(tenant_id,email,password_hash,role,name,status) VALUES(?,?,?,?,?,?) ON CONFLICT(email) DO NOTHING RETURNING id`,
		tid, email, placeholderHash, "teacher", fullName, "pending_verification").Scan(&userID); err != nil {
		return
	}
	tok, terr := createEmailToken(db, email, tokenPurposeSetPassword, &userID, nil, setPasswordTokenTTL)
	if terr != nil {
		logFromReq(r).Error("ensureTeacherUserAccount: token create failed", "err", terr, "email", email)
		return
	}
	setURL := appURL() + "/set-password.html?token=" + tok
	go func() {
		if err := mailer.Send(email, "Welcome to The Study Hub — set your password", renderTeacherWelcomeEmail(fullName, setURL)); err != nil {
			logger.Error("teacher welcome email failed", "err", err, "email", email)
		}
	}()
	logAudit(db, "system", "teacher_user_created", "user", fmt.Sprintf("%d", userID), "via add-staff")
}

// ── Staff ─────────────────────────────────────────────────────────────────────

func listStaff(db *DB, c *Claims) []Staff {
	tw, twArgs := scopeTenant(c, "")
	rows, err := db.Query(`SELECT id,name,full_name,role,email,phone,salary,join_date,status,specialization,nric,emergency_name,emergency_phone,employment_type,hourly_rate FROM staff WHERE deleted_at IS NULL`+tw, twArgs...)
	if err != nil {
		return []Staff{}
	}
	defer rows.Close()
	out := []Staff{}
	for rows.Next() {
		var s Staff
		var spec, nric, eName, ePhone, empType sql.NullString
		var hourlyRate sql.NullFloat64
		if err := rows.Scan(&s.ID, &s.Name, &s.FullName, &s.Role, &s.Email, &s.Phone, &s.Salary, &s.JoinDate, &s.Status, &spec, &nric, &eName, &ePhone, &empType, &hourlyRate); err != nil {
			continue
		}
		s.Specialization = nullStr(spec)
		s.NRIC = nullStr(nric)
		s.EmergencyName = nullStr(eName)
		s.EmergencyPhone = nullStr(ePhone)
		s.EmploymentType = nullStr(empType)
		if hourlyRate.Valid {
			s.HourlyRate = hourlyRate.Float64
		}
		// Hide salary, hourly rate and NRIC from anyone who isn't admin or
		// superadmin. Teachers should not see each other's pay or ID; the
		// snapshot is shared so the cleanup happens at this projection.
		if c != nil && c.Role != "admin" && c.Role != "superadmin" {
			s.Salary = 0
			s.HourlyRate = 0
			s.NRIC = ""
		} else if c != nil && c.Role != "superadmin" {
			// Admins (non-super) see a masked NRIC — last 4 only. Full value
			// is reserved for superadmin / DPO-equivalent access. Reduces
			// blast radius if an admin account is compromised.
			s.NRIC = maskNRIC(s.NRIC)
		}
		// Parents see only what they need to recognise the teacher on
		// the schedule: name + role. Strip personal phone, personal
		// email, emergency contact, join date and employment metadata.
		if c != nil && c.Role == "parent" {
			s.Phone = ""
			s.Email = ""
			s.EmergencyName = ""
			s.EmergencyPhone = ""
			s.JoinDate = ""
			s.EmploymentType = ""
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
			if !isAdminRole(c) {
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
			if _, err := db.Exec(`INSERT INTO staff(id,tenant_id,name,full_name,role,email,phone,salary,join_date,status,specialization,nric,emergency_name,emergency_phone,employment_type,hourly_rate) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				s.ID, tid, s.Name, s.FullName, s.Role, s.Email, s.Phone, s.Salary, s.JoinDate, s.Status, s.Specialization, s.NRIC, s.EmergencyName, s.EmergencyPhone, s.EmploymentType, s.HourlyRate); err != nil {
				respondError(w, "could not create staff", 500)
				return
			}
			logAudit(db, c.Email, "staff_created", "staff", s.ID, s.FullName)
			// If staff is a teacher (default), create login + welcome email.
			// Admin role is created without a login here — admin users are
			// managed via /api/users (separate flow with explicit role assignment).
			if s.Role == "Teacher" || s.Role == "Senior Teacher" {
				ensureTeacherUserAccount(db, r, tid, s.Email, s.FullName)
			}
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
			if !isAdminRole(c) {
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
			tw, twArgs := scopeTenant(c, "")
			args := append([]any{s.Name, s.FullName, s.Role, s.Email, s.Phone, s.Salary, s.JoinDate, s.Status, s.Specialization, s.NRIC, s.EmergencyName, s.EmergencyPhone, s.EmploymentType, s.HourlyRate, id}, twArgs...)
			if _, err := db.Exec(`UPDATE staff SET name=?,full_name=?,role=?,email=?,phone=?,salary=?,join_date=?,status=?,specialization=?,nric=?,emergency_name=?,emergency_phone=?,employment_type=?,hourly_rate=? WHERE id=?`+tw, args...); err != nil {
				respondError(w, "could not update staff", 500)
				return
			}
			if c != nil {
				logAudit(db, c.Email, "staff_updated", "staff", id, s.Name)
			}
			respond(w, s)

		case http.MethodDelete:
			if !isAdminRole(c) {
				respondError(w, "admin only", 403)
				return
			}
			tw, twArgs := scopeTenant(c, "")
			args := append([]any{id}, twArgs...)
			if _, err := db.Exec(`UPDATE staff SET deleted_at=NOW() WHERE id=?`+tw, args...); err != nil {
				respondError(w, "could not delete staff", 500)
				return
			}
			if c != nil {
				logAudit(db, c.Email, "staff_deleted", "staff", id, "soft deleted")
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}
}
