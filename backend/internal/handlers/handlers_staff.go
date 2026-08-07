package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/mailer"
	"studyhub/internal/models"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
)

// ensureTeacherUserAccount creates a teacher login row + email if the staff
// member doesn't already have one. Lets Add Staff produce a usable login
// without a separate self-register-then-approve dance.
func ensureTeacherUserAccount(db *store.DB, r *http.Request, tid int, email, fullName string) {
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
		core.Logger.Error("ensureTeacherUserAccount: rand failed", "err", err)
		return
	}
	placeholderHash, err := auth.HashPassword(hex.EncodeToString(placeholderBytes))
	if err != nil {
		core.Logger.Error("ensureTeacherUserAccount: hash failed", "err", err)
		return
	}
	var userID int64
	if err := db.QueryRow(`INSERT INTO users(tenant_id,email,password_hash,role,name,status) VALUES(?,?,?,?,?,?) ON CONFLICT(email) DO NOTHING RETURNING id`,
		tid, email, placeholderHash, "teacher", fullName, "pending_verification").Scan(&userID); err != nil {
		return
	}
	tok, terr := store.CreateEmailToken(db, email, store.TokenPurposeSetPassword, &userID, nil, store.SetPasswordTokenTTL)
	if terr != nil {
		core.LogFromReq(r).Error("ensureTeacherUserAccount: token create failed", "err", terr, "email", email)
		return
	}
	setURL := mailer.AppURL() + "/set-password.html?token=" + tok
	go func() {
		if err := core.SendEmail(email, "Welcome to The Study Hub — set your password", mailer.RenderTeacherWelcomeEmail(fullName, setURL)); err != nil {
			core.Logger.Error("teacher welcome email failed", "err", err, "email", email)
		}
	}()
	core.LogAudit(db, tid, "system", "teacher_user_created", "user", fmt.Sprintf("%d", userID), "via add-staff")
}

// ── Staff ─────────────────────────────────────────────────────────────────────

func listStaff(db *store.DB, c *core.Claims) []models.Staff {
	tw, twArgs := store.ScopeTenant(c, "")
	rows, err := db.Query(`SELECT id,name,full_name,role,email,phone,salary,join_date,status,specialization,nric,emergency_name,emergency_phone,employment_type,hourly_rate,performance_notes FROM staff WHERE deleted_at IS NULL`+tw, twArgs...)
	if err != nil {
		core.Logger.Error("list query failed", "err", err, "type", "Staff")
		return []models.Staff{}
	}
	defer rows.Close()
	out := []models.Staff{}
	for rows.Next() {
		var s models.Staff
		var spec, nric, eName, ePhone, empType, perfNotes sql.NullString
		var hourlyRate sql.NullFloat64
		if err := rows.Scan(&s.ID, &s.Name, &s.FullName, &s.Role, &s.Email, &s.Phone, &s.Salary, &s.JoinDate, &s.Status, &spec, &nric, &eName, &ePhone, &empType, &hourlyRate, &perfNotes); err != nil {
			continue
		}
		s.Specialization = models.NullStr(spec)
		s.NRIC = models.NullStr(nric)
		s.EmergencyName = models.NullStr(eName)
		s.EmergencyPhone = models.NullStr(ePhone)
		s.EmploymentType = models.NullStr(empType)
		s.PerformanceNotes = models.NullStr(perfNotes)
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
			// Internal performance notes are admin-facing only.
			s.PerformanceNotes = ""
		} else if c != nil && c.Role != "superadmin" {
			// Admins (non-super) see a masked NRIC — last 4 only. Full value
			// is reserved for superadmin / DPO-equivalent access. Reduces
			// blast radius if an admin account is compromised.
			s.NRIC = core.MaskNRIC(s.NRIC)
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

func HandleStaff(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			core.Respond(w, listStaff(db, c))
		case http.MethodPost:
			if !core.IsAdminRole(c) {
				core.RespondError(w, "admin only", 403)
				return
			}
			var s models.Staff
			if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
				core.RespondError(w, "bad body", 400)
				return
			}
			if msg := validationError("name", s.Name, "fullName", s.FullName, "role", s.Role, "email", s.Email); msg != "" {
				core.RespondError(w, msg, 400)
				return
			}
			if s.Status == "" {
				s.Status = "Active"
			}
			if s.ID == "" {
				s.ID = core.GenerateID("stf")
			}
			if s.EmploymentType == "" {
				s.EmploymentType = "Full-time"
			}
			tid := store.TenantID(c)
			if _, err := db.Exec(`INSERT INTO staff(id,tenant_id,name,full_name,role,email,phone,salary,join_date,status,specialization,nric,emergency_name,emergency_phone,employment_type,hourly_rate) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				s.ID, tid, s.Name, s.FullName, s.Role, s.Email, s.Phone, s.Salary, s.JoinDate, s.Status, s.Specialization, s.NRIC, s.EmergencyName, s.EmergencyPhone, s.EmploymentType, s.HourlyRate); err != nil {
				core.RespondError(w, "could not create staff", 500)
				return
			}
			core.LogAudit(db, store.TenantID(c), c.Email, "staff_created", "staff", s.ID, s.FullName)
			// If staff is a teacher (default), create login + welcome email.
			// Admin role is created without a login here — admin users are
			// managed via /api/users (separate flow with explicit role assignment).
			if s.Role == "Teacher" || s.Role == "Senior Teacher" {
				ensureTeacherUserAccount(db, r, tid, s.Email, s.FullName)
			}
			core.Respond(w, s)
		}
	}
}

func HandleStaffByID(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		id := chi.URLParam(r, "id")
		switch r.Method {
		case http.MethodPut:
			if !core.IsAdminRole(c) {
				core.RespondError(w, "admin only", 403)
				return
			}
			var s models.Staff
			if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
				core.RespondError(w, "bad body", 400)
				return
			}
			s.ID = id
			if s.EmploymentType == "" {
				s.EmploymentType = "Full-time"
			}
			tw, twArgs := store.ScopeTenant(c, "")
			args := append([]any{s.Name, s.FullName, s.Role, s.Email, s.Phone, s.Salary, s.JoinDate, s.Status, s.Specialization, s.NRIC, s.EmergencyName, s.EmergencyPhone, s.EmploymentType, s.HourlyRate, s.PerformanceNotes, id}, twArgs...)
			if _, err := db.Exec(`UPDATE staff SET name=?,full_name=?,role=?,email=?,phone=?,salary=?,join_date=?,status=?,specialization=?,nric=?,emergency_name=?,emergency_phone=?,employment_type=?,hourly_rate=?,performance_notes=? WHERE id=?`+tw+` AND deleted_at IS NULL`, args...); err != nil {
				core.RespondError(w, "could not update staff", 500)
				return
			}
			if c != nil {
				core.LogAudit(db, store.TenantID(c), c.Email, "staff_updated", "staff", id, s.Name)
			}
			core.Respond(w, s)

		case http.MethodDelete:
			if !core.IsAdminRole(c) {
				core.RespondError(w, "admin only", 403)
				return
			}
			tw, twArgs := store.ScopeTenant(c, "")
			args := append([]any{id}, twArgs...)
			if _, err := db.Exec(`UPDATE staff SET deleted_at=NOW() WHERE id=?`+tw, args...); err != nil {
				core.RespondError(w, "could not delete staff", 500)
				return
			}
			if c != nil {
				core.LogAudit(db, store.TenantID(c), c.Email, "staff_deleted", "staff", id, "soft deleted")
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}
}
