package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// ── Registrations ─────────────────────────────────────────────────────────────

func listRegistrations(db *DB, c *Claims) []Registration {
	tid := tenantID(c)
	rows, err := db.Query(`SELECT id,parent_name,email,phone,emergency_name,emergency_phone,student_first_name,student_last_name,student_dob,student_gender,gender,school_name,year_grade,class_type_interest,subject_interest,school_fees,registration_date,workshop_interest,class_interest,notes,submitted_on,status,COALESCE(type,'student'),COALESCE(specialization,''),COALESCE(nric,''),COALESCE(display_name,''),COALESCE(employment_type,'Full-time'),COALESCE(experience,''),COALESCE(qualifications,''),COALESCE(bio,''),COALESCE(schedule,''),COALESCE(expected_salary,''),COALESCE(referral_code,''),COALESCE(email_verified_at::text,'') FROM registrations WHERE status='pending' AND (tenant_id=? OR ?=0) ORDER BY submitted_on DESC`, tid, tid)
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
			&reg.Specialization, &reg.NRIC, &reg.DisplayName, &reg.EmploymentType, &reg.Experience, &reg.Qualifications, &reg.Bio, &reg.Schedule, &reg.ExpectedSalary, &reg.ReferralCode, &reg.EmailVerifiedAt)
		out = append(out, reg)
	}
	return out
}

// POST /api/register — public, no auth required.
//
// Self-serve flow:
//  1. Parent submits the form with their chosen password.
//  2. We create a `users` row immediately in `pending_verification` state and
//     a `registrations` row for admin's records.
//  3. We mint an email_tokens row (purpose=verify_parent) and email the link.
//  4. The parent clicks the link → /api/verify-email activates the account
//     and issues an auth cookie. They land in the dashboard.
//  5. Admin's only remaining job is "link the child's profile to this parent",
//     which they do via the existing approval flow.
//
// Backwards compat: the existing handleRegistrationApprove still works and
// will skip user creation if a user already exists for the email.
func handleRegister(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Registration
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			respondError(w, "bad request", 400)
			return
		}
		reg := body.Registration

		if reg.ParentName == "" || reg.Email == "" || reg.StudentFirstName == "" {
			respondError(w, "parent name, email and student first name are required", 400)
			return
		}
		if !validateEmail(reg.Email) {
			respondError(w, "invalid email address", 400)
			return
		}
		if ok, msg := validatePassword(body.Password); !ok {
			respondError(w, msg, 400)
			return
		}

		email := strings.ToLower(strings.TrimSpace(reg.Email))

		// Reject if a user already exists. Don't reveal *why* in too much
		// detail — just point them to the right next action.
		var existingID int
		_ = db.QueryRow(`SELECT id FROM users WHERE email=?`, email).Scan(&existingID)
		if existingID > 0 {
			respondError(w, "An account with this email already exists. Try logging in or use forgot password.", 409)
			return
		}

		hash, err := hashPassword(body.Password)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}

		reg.ID = generateID("REG")
		reg.SubmittedOn = today()
		reg.Status = "pending"
		reg.Type = "student"

		tx, err := db.BeginTx(r.Context())
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		defer tx.Rollback()

		// Create the registration row first so we have the ID for the token.
		if _, err := tx.Exec(`INSERT INTO registrations(id,parent_name,email,phone,emergency_name,emergency_phone,student_first_name,student_last_name,student_dob,student_gender,gender,school_name,year_grade,class_type_interest,subject_interest,school_fees,registration_date,workshop_interest,class_interest,notes,submitted_on,status,type,referral_code) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			reg.ID, reg.ParentName, email, reg.Phone, reg.EmergencyName, reg.EmergencyPhone,
			reg.StudentFirstName, reg.StudentLastName, reg.StudentDOB, reg.StudentGender,
			reg.Gender, reg.SchoolName, reg.YearGrade, reg.ClassTypeInterest, reg.SubjectInterest,
			reg.SchoolFees, reg.RegistrationDate, reg.WorkshopInterest,
			reg.ClassInterest, reg.Notes, reg.SubmittedOn, reg.Status, reg.Type,
			strings.ToUpper(strings.TrimSpace(reg.ReferralCode))); err != nil {
			respondError(w, "could not save registration", 500)
			return
		}

		// Create the parent user in pending_verification state. The login handler
		// rejects this status until the email is verified.
		var userID int64
		if err := tx.QueryRow(`INSERT INTO users(tenant_id,email,password_hash,role,name,status) VALUES(?,?,?,?,?,?) RETURNING id`,
			1, email, hash, "parent", reg.ParentName, "pending_verification").Scan(&userID); err != nil {
			respondError(w, "could not create account", 500)
			return
		}

		if err := tx.Commit(); err != nil {
			respondError(w, "server error", 500)
			return
		}

		// Token + email happen outside the transaction so a transient SES /
		// Resend hiccup doesn't roll back the user record. Worst case the user
		// re-requests the link via "Resend verification email".
		regID := reg.ID
		token, terr := createEmailToken(db, email, tokenPurposeVerifyParent, &userID, &regID, verifyTokenTTL)
		if terr != nil {
			// Account exists, just no email sent. Surface a useful error so
			// the parent can retry rather than getting a silent broken state.
			respondError(w, "account created but verification email failed — please use the resend link", 500)
			return
		}
		verifyURL := appURL() + "/verify.html?token=" + token
		if err := mailer.Send(email, "Verify your Study Hub account", renderVerifyParentEmail(reg.ParentName, verifyURL)); err != nil {
			// Same fallback — don't 500 because email is the only failed step.
			logFromReq(r).Error("parent verify mail send failed", "err", err, "email", email)
		}

		db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
			email, "parent_self_registered", "user", fmt.Sprintf("%d", userID), "registration_id="+reg.ID)

		w.WriteHeader(http.StatusCreated)
		respond(w, map[string]string{
			"id":      reg.ID,
			"status":  "pending_verification",
			"message": "Account created. Check your email for a verification link.",
		})
	}
}

// POST /api/registrations/{id}/approve — admin only
func handleRegistrationApprove(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		id := chi.URLParam(r, "id")
		var reg Registration
		err := db.QueryRow(`SELECT id,parent_name,email,phone,emergency_name,emergency_phone,student_first_name,student_last_name,student_dob,student_gender,class_interest,notes,COALESCE(type,'student'),COALESCE(specialization,''),COALESCE(nric,''),COALESCE(display_name,''),COALESCE(employment_type,'Full-time'),COALESCE(experience,''),COALESCE(qualifications,''),COALESCE(expected_salary,''),COALESCE(referral_code,'') FROM registrations WHERE id=?`, id).
			Scan(&reg.ID, &reg.ParentName, &reg.Email, &reg.Phone, &reg.EmergencyName, &reg.EmergencyPhone,
				&reg.StudentFirstName, &reg.StudentLastName, &reg.StudentDOB, &reg.StudentGender,
				&reg.ClassInterest, &reg.Notes, &reg.Type,
				&reg.Specialization, &reg.NRIC, &reg.DisplayName, &reg.EmploymentType, &reg.Experience, &reg.Qualifications, &reg.ExpectedSalary, &reg.ReferralCode)
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

		// Variables populated during the teacher branch and consumed after
		// the transaction commits. Sending the email inside the transaction
		// would mean a transient mailer outage rolls back the staff record,
		// which we don't want — the staff exists, we just need to retry the
		// email (admin can use the resend flow).
		var (
			pendingTeacherEmail  string
			pendingTeacherName   string
			pendingTeacherUserID int64
		)

		if reg.Type == "teacher" {
			// Teacher approval: create staff + user records, then email a
			// "set your password" link instead of the legacy temp password.
			// The user account exists but is unusable until the link is
			// clicked because we hash a discarded random secret.
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

			// Generate a new throw-away hash so the password column is set
			// (NOT NULL) but no one — including us — knows the password.
			placeholderBytes := make([]byte, 32)
			if _, err := rand.Read(placeholderBytes); err != nil {
				respondError(w, "server error", 500)
				return
			}
			placeholderHash, perr := hashPassword(hex.EncodeToString(placeholderBytes))
			if perr != nil {
				respondError(w, "server error", 500)
				return
			}

			teacherEmail := strings.ToLower(strings.TrimSpace(reg.Email))
			// Create the user with status='pending_verification' so it can't
			// log in until set-password completes (which flips to active).
			var teacherUserID int64
			if err := tx.QueryRow(`INSERT INTO users(tenant_id,email,password_hash,role,name,status) VALUES(?,?,?,?,?,?)
			                       ON CONFLICT(email) DO UPDATE SET status='pending_verification'
			                       RETURNING id`,
				tid, teacherEmail, placeholderHash, "teacher", displayName, "pending_verification").Scan(&teacherUserID); err != nil {
				respondError(w, "could not create teacher account", 500)
				return
			}

			responseData = map[string]string{
				"staffId": staffID,
				"type":    "teacher",
				"message": "Teacher added. A set-password email has been sent — they'll choose their own password and sign in directly.",
			}

			// Defer the token + email send until after the transaction commits,
			// using a closure stashed in the response data via a side channel.
			// Simpler: do it after commit by remembering the values here.
			pendingTeacherEmail = teacherEmail
			pendingTeacherName = displayName
			pendingTeacherUserID = teacherUserID
		} else {
			// Student registration: create student row, then ensure a parent
			// user account exists. With the self-serve flow, the parent's
			// account was already created at /api/register time — in that
			// case we skip user creation entirely and the temp password is
			// irrelevant. Legacy / admin-driven flow still creates the user
			// with a temp password to share manually.
			stuID := generateID("STU")
			if _, err := tx.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
				stuID, tid, reg.StudentFirstName, reg.StudentLastName, reg.StudentDOB, reg.StudentGender,
				reg.ParentName, reg.Email, reg.Phone, "The Study Hub", "New", today(), "[]", "[]", reg.Notes); err != nil {
				respondError(w, "could not create student record", 500)
				return
			}

			parentEmail := strings.ToLower(strings.TrimSpace(reg.Email))
			var existingUserID int
			tx.QueryRow(`SELECT id FROM users WHERE email=?`, parentEmail).Scan(&existingUserID)
			parentSelfServed := existingUserID > 0

			if !parentSelfServed {
				if _, err := tx.Exec(`INSERT INTO users(tenant_id,email,password_hash,role,name) VALUES(?,?,?,?,?) ON CONFLICT(email) DO NOTHING`,
					tid, parentEmail, hash, "parent", reg.ParentName); err != nil {
					respondError(w, "could not create parent account", 500)
					return
				}
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
				tx.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name,referral_code) VALUES(?,?,?,?,?,?,?)`,
					famID, tid, familyName, reg.Email, reg.Phone, reg.ParentName, newReferralCode())
			}
			tx.Exec(`UPDATE students SET family_id=? WHERE id=?`, famID, stuID)

			// Referral validation: if a code was supplied, look up the referrer
			// family and create a pending referral_rewards row. Self-referral and
			// invalid codes are ignored (logged) so they don't block approval.
			code := strings.ToUpper(strings.TrimSpace(reg.ReferralCode))
			if code != "" {
				var referrerFamID string
				tx.QueryRow(`SELECT id FROM families WHERE referral_code=? AND (tenant_id=? OR ?=0) AND deleted_at IS NULL`, code, tid, tid).Scan(&referrerFamID)
				if referrerFamID != "" && referrerFamID != famID {
					if _, err := tx.Exec(`INSERT INTO referral_rewards(id,tenant_id,referrer_family_id,referred_student_id,status) VALUES(?,?,?,?,'pending') ON CONFLICT(referred_student_id) DO NOTHING`,
						generateID("REF"), tid, referrerFamID, stuID); err == nil {
						tx.Exec(`UPDATE students SET referred_by_family_id=? WHERE id=?`, referrerFamID, stuID)
						tx.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
							c.Email, "referral_validated", "student", stuID, "code="+code+" referrer="+referrerFamID)
					}
				} else {
					reason := "code_not_found"
					if referrerFamID == famID {
						reason = "self_referral"
					}
					tx.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
						c.Email, "referral_rejected", "student", stuID, "code="+code+" reason="+reason)
				}
			}

			if parentSelfServed {
				responseData = map[string]string{
					"studentId": stuID,
					"type":      "student",
					"message":   "Student linked to existing parent account. No temp password needed — they already chose their own.",
				}
			} else {
				responseData = map[string]string{
					"studentId":    stuID,
					"tempPassword": tempPassword,
					"type":         "student",
					"message":      "Student created. Share temp password with parent.",
				}
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

		// Post-commit: if we just approved a teacher, mint a set-password
		// token and email the welcome link. Failure here is logged but does
		// NOT roll back the approval — the admin can hit "resend" later.
		if pendingTeacherEmail != "" {
			invalidateOldTokens(db, pendingTeacherEmail, tokenPurposeSetPassword)
			tok, terr := createEmailToken(db, pendingTeacherEmail, tokenPurposeSetPassword, &pendingTeacherUserID, nil, setPasswordTokenTTL)
			l := logFromReq(r).With("teacher_email", pendingTeacherEmail, "user_id", pendingTeacherUserID)
			if terr != nil {
				l.Error("set-password token create failed", "err", terr)
			} else {
				setURL := appURL() + "/set-password.html?token=" + tok
				if mErr := mailer.Send(pendingTeacherEmail, "Welcome to The Study Hub — set your password", renderTeacherWelcomeEmail(pendingTeacherName, setURL)); mErr != nil {
					l.Error("set-password mail send failed", "err", mErr)
				} else {
					l.Info("teacher approved, set-password email sent")
				}
			}
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "teacher_approved", "user", fmt.Sprintf("%d", pendingTeacherUserID), pendingTeacherEmail)
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

func handleRegisterTeacher(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var reg struct {
			FullName       string `json:"fullName"`
			DisplayName    string `json:"displayName"`
			Email          string `json:"email"`
			Phone          string `json:"phone"`
			Specialty      string `json:"specialty"`
			EmploymentType string `json:"employmentType"`
			Experience     string `json:"experience"`
			Qualifications string `json:"qualifications"`
			Bio            string `json:"bio"`
			Schedule       string `json:"schedule"`
			ExpectedSalary string `json:"expectedSalary"`
			EmergencyName  string `json:"emergencyName"`
			EmergencyPhone string `json:"emergencyPhone"`
			NRIC           string `json:"nric"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
			respondError(w, "bad request", 400)
			return
		}
		if reg.FullName == "" || reg.Email == "" {
			respondError(w, "full name and email are required", 400)
			return
		}
		if !validateEmail(reg.Email) {
			respondError(w, "invalid email address", 400)
			return
		}
		if reg.DisplayName == "" {
			reg.DisplayName = reg.FullName
		}
		if reg.EmploymentType == "" {
			reg.EmploymentType = "Full-time"
		}

		id := generateID("REG")
		email := strings.ToLower(strings.TrimSpace(reg.Email))
		_, err := db.Exec(`INSERT INTO registrations(id,parent_name,email,phone,emergency_name,emergency_phone,student_first_name,student_last_name,student_dob,student_gender,gender,school_name,year_grade,class_type_interest,subject_interest,school_fees,registration_date,workshop_interest,class_interest,notes,submitted_on,status,type,specialization,nric,display_name,employment_type,experience,qualifications,bio,schedule,expected_salary) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, reg.FullName, email, reg.Phone, reg.EmergencyName, reg.EmergencyPhone,
			reg.FullName, "", "", "", "", "", "", "", "", 0, "", "",
			"", reg.Bio, today(), "pending", "teacher",
			reg.Specialty, reg.NRIC, reg.DisplayName, reg.EmploymentType, reg.Experience, reg.Qualifications, reg.Bio, reg.Schedule, reg.ExpectedSalary)
		if err != nil {
			respondError(w, "could not save registration", 500)
			return
		}

		// Mint a verify_teacher token + send confirmation email. No user
		// account is created at this stage — that only happens after admin
		// approves the application via handleRegistrationApprove.
		token, terr := createEmailToken(db, email, tokenPurposeVerifyTeacher, nil, &id, verifyTokenTTL)
		l := logFromReq(r).With("registration_id", id, "email", email)
		if terr == nil {
			verifyURL := appURL() + "/verify.html?token=" + token
			if mErr := mailer.Send(email, "Confirm your Study Hub teacher application", renderVerifyTeacherEmail(reg.FullName, verifyURL)); mErr != nil {
				l.Error("teacher register mail send failed", "err", mErr)
			} else {
				l.Info("teacher application received")
			}
		} else {
			l.Error("teacher register token create failed", "err", terr)
		}

		db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
			email, "teacher_self_registered", "registration", id, reg.FullName)

		w.WriteHeader(http.StatusCreated)
		respond(w, map[string]string{
			"id":      id,
			"status":  "pending_verification",
			"type":    "teacher",
			"message": "Application received. Check your email for the confirmation link.",
		})
	}
}
