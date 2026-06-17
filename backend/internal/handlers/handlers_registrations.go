package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/mailer"
	"studyhub/internal/models"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
)

// ── Registrations ─────────────────────────────────────────────────────────────

// listParentEnrollments returns enrollment requests submitted by the given
// parent. Used by the snapshot to populate the parent dashboard's pending
// enrolments section.
func listParentEnrollments(db *store.DB, c *core.Claims) []models.Registration {
	if c == nil {
		return []models.Registration{}
	}
	tw, twArgs := store.ScopeTenant(c, "")
	args := append([]any{c.Email}, twArgs...)
	rows, err := db.Query(`SELECT id,parent_name,email,COALESCE(student_first_name,''),COALESCE(student_last_name,''),submitted_on,status,COALESCE(type,'enrollment') FROM registrations WHERE email=? AND type='enrollment'`+tw+` ORDER BY submitted_on DESC`, args...)
	if err != nil {
		return []models.Registration{}
	}
	defer rows.Close()
	out := []models.Registration{}
	for rows.Next() {
		var reg models.Registration
		if err := rows.Scan(&reg.ID, &reg.ParentName, &reg.Email, &reg.StudentFirstName, &reg.StudentLastName, &reg.SubmittedOn, &reg.Status, &reg.Type); err != nil {
			continue
		}
		out = append(out, reg)
	}
	return out
}

func listRegistrations(db *store.DB, c *core.Claims) []models.Registration {
	tw, twArgs := store.ScopeTenant(c, "")
	rows, err := db.Query(`SELECT id,parent_name,email,phone,COALESCE(emergency_name,''),COALESCE(emergency_phone,''),COALESCE(student_first_name,''),COALESCE(student_last_name,''),COALESCE(student_dob,''),COALESCE(student_gender,''),COALESCE(gender,''),COALESCE(school_name,''),COALESCE(year_grade,''),COALESCE(class_type_interest,''),COALESCE(subject_interest,''),COALESCE(school_fees,0),COALESCE(registration_date,''),COALESCE(workshop_interest,''),COALESCE(class_interest,''),COALESCE(notes,''),submitted_on,status,COALESCE(type,'student'),COALESCE(specialization,''),COALESCE(nric,''),COALESCE(display_name,''),COALESCE(employment_type,'Full-time'),COALESCE(experience,''),COALESCE(qualifications,''),COALESCE(bio,''),COALESCE(schedule,''),COALESCE(expected_salary,''),COALESCE(referral_code,''),COALESCE(email_verified_at::text,'') FROM registrations WHERE status='pending'`+tw+` ORDER BY submitted_on DESC`, twArgs...)
	if err != nil {
		return []models.Registration{}
	}
	defer rows.Close()
	out := []models.Registration{}
	for rows.Next() {
		var reg models.Registration
		if err := rows.Scan(&reg.ID, &reg.ParentName, &reg.Email, &reg.Phone, &reg.EmergencyName, &reg.EmergencyPhone,
			&reg.StudentFirstName, &reg.StudentLastName, &reg.StudentDOB, &reg.StudentGender,
			&reg.Gender, &reg.SchoolName, &reg.YearGrade, &reg.ClassTypeInterest, &reg.SubjectInterest,
			&reg.SchoolFees, &reg.RegistrationDate, &reg.WorkshopInterest,
			&reg.ClassInterest, &reg.Notes, &reg.SubmittedOn, &reg.Status, &reg.Type,
			&reg.Specialization, &reg.NRIC, &reg.DisplayName, &reg.EmploymentType, &reg.Experience, &reg.Qualifications, &reg.Bio, &reg.Schedule, &reg.ExpectedSalary, &reg.ReferralCode, &reg.EmailVerifiedAt); err != nil {
			continue
		}
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
func HandleRegister(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ParentName     string `json:"parentName"`
			Email          string `json:"email"`
			Password       string `json:"password"`
			Phone          string `json:"phone"`
			EmergencyName  string `json:"emergencyName"`
			EmergencyPhone string `json:"emergencyPhone"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			core.RespondError(w, "bad request", 400)
			return
		}
		if body.ParentName == "" || body.Email == "" {
			core.RespondError(w, "name and email are required", 400)
			return
		}
		if !auth.ValidateEmail(body.Email) {
			core.RespondError(w, "invalid email address", 400)
			return
		}
		if ok, msg := auth.ValidatePassword(body.Password); !ok {
			core.RespondError(w, msg, 400)
			return
		}

		email := strings.ToLower(strings.TrimSpace(body.Email))

		var existingID int
		_ = db.QueryRow(`SELECT id FROM users WHERE email=?`, email).Scan(&existingID)
		if existingID > 0 {
			core.RespondError(w, "An account with this email already exists. Try logging in or use forgot password.", 409)
			return
		}

		hash, err := auth.HashPassword(body.Password)
		if err != nil {
			core.RespondError(w, "server error", 500)
			return
		}

		tx, err := db.BeginTx(r.Context())
		if err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		defer tx.Rollback()

		// Create the parent user in pending_verification state.
		var userID int64
		if err := tx.QueryRow(`INSERT INTO users(tenant_id,email,password_hash,role,name,status) VALUES(?,?,?,?,?,?) RETURNING id`,
			1, email, hash, "parent", body.ParentName, "pending_verification").Scan(&userID); err != nil {
			core.RespondError(w, "could not create account", 500)
			return
		}

		// Create the family so the parent has a referral code immediately
		// and enrollment requests can link to it.
		famID := core.GenerateID("FAM")
		familyName := body.ParentName + " Family"
		if body.ParentName == "" {
			familyName = email
		}
		emergencyNote := ""
		if body.EmergencyName != "" {
			emergencyNote = "Emergency: " + body.EmergencyName
			if body.EmergencyPhone != "" {
				emergencyNote += " · " + body.EmergencyPhone
			}
		}
		if _, err := tx.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name,referral_code,notes) VALUES(?,?,?,?,?,?,?,?)`,
			famID, 1, familyName, email, body.Phone, body.ParentName, core.NewReferralCode(), emergencyNote); err != nil {
			core.RespondError(w, "could not create family", 500)
			return
		}

		// Record the self-registration in the registrations table so admin's
		// queue + audit reports can find it (the comment block at the top of
		// this handler advertised this row but it was never actually inserted
		// — admins had no view of self-registered parents).
		regID := core.GenerateID("REG")
		if _, err := tx.Exec(`INSERT INTO registrations(id,tenant_id,parent_name,email,phone,emergency_name,emergency_phone,submitted_on,status,type) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			regID, 1, body.ParentName, email, body.Phone, body.EmergencyName, body.EmergencyPhone, core.Today(), "self_registered", "parent"); err != nil {
			core.RespondError(w, "could not record registration", 500)
			return
		}

		if err := tx.Commit(); err != nil {
			core.RespondError(w, "server error", 500)
			return
		}

		// Token + email happen outside the transaction.
		token, terr := store.CreateEmailToken(db, email, store.TokenPurposeVerifyParent, &userID, nil, store.VerifyTokenTTL)
		if terr != nil {
			core.RespondError(w, "account created but verification email failed — please use the resend link", 500)
			return
		}
		verifyURL := mailer.AppURL() + "/verify.html?token=" + token
		if err := core.SendEmail(email, "Verify your Study Hub account", mailer.RenderVerifyParentEmail(body.ParentName, verifyURL)); err != nil {
			core.LogFromReq(r).Error("parent verify mail send failed", "err", err, "email", email)
		}

		core.LogAudit(db, email, "parent_self_registered", "user", fmt.Sprintf("%d", userID), "family_id="+famID)

		w.WriteHeader(http.StatusCreated)
		core.Respond(w, map[string]string{
			"status":  "pending_verification",
			"message": "Account created. Check your email for a verification link.",
		})

	}
}

// POST /api/registrations/{id}/approve — admin only
func HandleRegistrationApprove(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		id := chi.URLParam(r, "id")

		// Optional body: admin can pass classIds to assign during approval.
		var approveBody struct {
			ClassIds []string `json:"classIds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&approveBody); err != nil && err.Error() != "EOF" {
			core.RespondError(w, "bad request body", http.StatusBadRequest)
			return
		}

		regTw, regTwArgs := store.ScopeTenant(c, "")
		var reg models.Registration
		regSelArgs := append([]any{id}, regTwArgs...)
		err := db.QueryRow(`SELECT id,parent_name,email,phone,emergency_name,emergency_phone,student_first_name,student_last_name,student_dob,student_gender,class_interest,notes,COALESCE(type,'student'),COALESCE(specialization,''),COALESCE(nric,''),COALESCE(display_name,''),COALESCE(employment_type,'Full-time'),COALESCE(experience,''),COALESCE(qualifications,''),COALESCE(expected_salary,''),COALESCE(referral_code,'') FROM registrations WHERE id=?`+regTw, regSelArgs...).
			Scan(&reg.ID, &reg.ParentName, &reg.Email, &reg.Phone, &reg.EmergencyName, &reg.EmergencyPhone,
				&reg.StudentFirstName, &reg.StudentLastName, &reg.StudentDOB, &reg.StudentGender,
				&reg.ClassInterest, &reg.Notes, &reg.Type,
				&reg.Specialization, &reg.NRIC, &reg.DisplayName, &reg.EmploymentType, &reg.Experience, &reg.Qualifications, &reg.ExpectedSalary, &reg.ReferralCode)
		if err != nil {
			core.RespondError(w, "registration not found", 404)
			return
		}

		// Generate temp password before starting transaction
		tid := store.TenantID(c)
		rawBytes := make([]byte, 8)
		if _, err := rand.Read(rawBytes); err != nil {
			core.RespondError(w, "could not generate password", 500)
			return
		}
		tempPassword := "Sh-" + hex.EncodeToString(rawBytes)
		hash, err := auth.HashPassword(tempPassword)
		if err != nil {
			core.RespondError(w, "could not hash password", 500)
			return
		}

		// Start transaction
		tx, err := db.BeginTx(r.Context())
		if err != nil {
			core.RespondError(w, "server error", 500)
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

		if reg.Type == "enrollment" {
			// Enrollment request: the parent already has a user + family.
			// We only need to create the student and link to the family.
			parentEmail := strings.ToLower(strings.TrimSpace(reg.Email))
			var famID string
			famTw, famTwArgs := store.ScopeTenant(c, "")
			famArgs := append([]any{parentEmail}, famTwArgs...)
			tx.QueryRow(`SELECT id FROM families WHERE contact=?`+famTw+` AND deleted_at IS NULL`, famArgs...).Scan(&famID)
			if famID == "" {
				// Parent has no family record (e.g. admin-created account).
				// Create one so the student is linked and visible to the parent.
				famID = core.GenerateID("FAM")
				familyName := reg.ParentName + " Family"
				if reg.ParentName == "" {
					familyName = parentEmail
				}
				tx.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name,referral_code) VALUES(?,?,?,?,?,?,?)`,
					famID, tid, familyName, parentEmail, reg.Phone, reg.ParentName, core.NewReferralCode())
			}

			stuID := core.GenerateID("STU")
			// If admin picked classes during approval, enrol immediately.
			enrolledJSON := "[]"
			if len(approveBody.ClassIds) > 0 {
				b, _ := json.Marshal(approveBody.ClassIds)
				enrolledJSON = string(b)
			}
			if _, err := tx.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,family_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
				stuID, tid, reg.StudentFirstName, reg.StudentLastName, reg.StudentDOB, reg.StudentGender,
				reg.ParentName, parentEmail, "", "The Study Hub", "New", core.Today(), enrolledJSON, "[]", reg.Notes, famID); err != nil {
				core.RespondError(w, "could not create student record", 500)
				return
			}
			// Increment enrolled count on each assigned class, gated by capacity.
			// The atomic UPDATE … WHERE enrolled < capacity bumps the count
			// only if there's room — two concurrent approvals racing on the
			// last seat can't both succeed because Postgres serialises the
			// row update. Zero rows affected = class is full or missing;
			// abort the whole approval (tx rollback) so the parent isn't
			// left with a half-enrolled student.
			clsTw, clsTwArgs := store.ScopeTenant(c, "")
			for _, cid := range approveBody.ClassIds {
				clsArgs := append([]any{cid}, clsTwArgs...)
				res, err := tx.Exec(`UPDATE classes SET enrolled=enrolled+1 WHERE id=? AND deleted_at IS NULL AND enrolled < capacity`+clsTw, clsArgs...)
				if err != nil {
					core.RespondError(w, "could not enrol in class", 500)
					return
				}
				n, _ := res.RowsAffected()
				if n == 0 {
					core.RespondError(w, "class is full or unavailable: "+cid, http.StatusConflict)
					return
				}
			}

			// Referral validation (same pattern as the old combined flow).
			code := strings.ToUpper(strings.TrimSpace(reg.ReferralCode))
			if code != "" && famID != "" {
				var referrerFamID string
				refTw, refTwArgs := store.ScopeTenant(c, "")
				refArgs := append([]any{code}, refTwArgs...)
				tx.QueryRow(`SELECT id FROM families WHERE referral_code=?`+refTw+` AND deleted_at IS NULL`, refArgs...).Scan(&referrerFamID)
				if referrerFamID != "" && referrerFamID != famID {
					if _, err := tx.Exec(`INSERT INTO referral_rewards(id,tenant_id,referrer_family_id,referred_student_id,status) VALUES(?,?,?,?,'pending') ON CONFLICT(referred_student_id) DO NOTHING`,
						core.GenerateID("REF"), tid, referrerFamID, stuID); err == nil {
						stuUpdArgs := append([]any{referrerFamID, stuID}, clsTwArgs...)
						tx.Exec(`UPDATE students SET referred_by_family_id=? WHERE id=?`+clsTw, stuUpdArgs...)
						core.LogAudit(tx, c.Email, "referral_validated", "student", stuID, "code="+code+" referrer="+referrerFamID)
					}
				}
			}

			responseData = map[string]string{
				"studentId": stuID,
				"type":      "enrollment",
				"message":   "Student enrolled and linked to parent's family.",
			}

			// Build class list HTML for the approval email.
			var classListHTML string
			if len(approveBody.ClassIds) > 0 {
				classListHTML = `<ul style="margin:0;padding:0 0 0 18px;font-size:13px;color:#374151">`
				for _, cid := range approveBody.ClassIds {
					var cname, cday, ctime string
					cnameArgs := append([]any{cid}, clsTwArgs...)
					db.QueryRow(`SELECT name, day, time FROM classes WHERE id=?`+clsTw, cnameArgs...).Scan(&cname, &cday, &ctime)
					if cname != "" {
						classListHTML += `<li style="margin-bottom:4px">` + html.EscapeString(cname) + ` — ` + html.EscapeString(cday) + ` ` + html.EscapeString(ctime) + `</li>`
					}
				}
				classListHTML += `</ul>`
			}

			// Defer enrollment-approved email until after commit.
			enrollEmail := strings.ToLower(strings.TrimSpace(reg.Email))
			enrollParentName := reg.ParentName
			enrollStudentName := reg.StudentFirstName + " " + reg.StudentLastName
			enrollClassHTML := classListHTML
			defer func() {
				go func() {
					if err := core.SendEmail(enrollEmail, mailer.SafeName(enrollStudentName)+" has been enrolled at The Study Hub",
						mailer.RenderEnrollmentApprovedEmail(enrollParentName, enrollStudentName, enrollClassHTML)); err != nil {
						core.Logger.Error("enrollment approved email failed", "err", err, "email", enrollEmail)
					}
				}()
			}()
		} else if reg.Type == "teacher" {
			// Teacher approval: create staff + user records, then email a
			// "set your password" link instead of the legacy temp password.
			// The user account exists but is unusable until the link is
			// clicked because we hash a discarded random secret.
			staffID := core.GenerateID("stf")
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
				"Teacher", reg.Email, reg.Phone, 0, core.Today(), "Active",
				reg.Specialization, reg.NRIC,
				reg.EmergencyName, reg.EmergencyPhone, empType); err != nil {
				core.RespondError(w, "could not create staff record", 500)
				return
			}

			// Generate a new throw-away hash so the password column is set
			// (NOT NULL) but no one — including us — knows the password.
			placeholderBytes := make([]byte, 32)
			if _, err := rand.Read(placeholderBytes); err != nil {
				core.RespondError(w, "server error", 500)
				return
			}
			placeholderHash, perr := auth.HashPassword(hex.EncodeToString(placeholderBytes))
			if perr != nil {
				core.RespondError(w, "server error", 500)
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
				core.RespondError(w, "could not create teacher account", 500)
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
			stuID := core.GenerateID("STU")
			if _, err := tx.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
				stuID, tid, reg.StudentFirstName, reg.StudentLastName, reg.StudentDOB, reg.StudentGender,
				reg.ParentName, reg.Email, reg.Phone, "The Study Hub", "New", core.Today(), "[]", "[]", reg.Notes); err != nil {
				core.RespondError(w, "could not create student record", 500)
				return
			}

			parentEmail := strings.ToLower(strings.TrimSpace(reg.Email))
			var existingUserID int
			tx.QueryRow(`SELECT id FROM users WHERE email=?`, parentEmail).Scan(&existingUserID)
			parentSelfServed := existingUserID > 0

			if !parentSelfServed {
				if _, err := tx.Exec(`INSERT INTO users(tenant_id,email,password_hash,role,name) VALUES(?,?,?,?,?) ON CONFLICT(email) DO NOTHING`,
					tid, parentEmail, hash, "parent", reg.ParentName); err != nil {
					core.RespondError(w, "could not create parent account", 500)
					return
				}
			}

			// Find or create family for parent
			var famID string
			famTw, famTwArgs := store.ScopeTenant(c, "")
			famArgs := append([]any{strings.ToLower(strings.TrimSpace(reg.Email))}, famTwArgs...)
			tx.QueryRow(`SELECT id FROM families WHERE contact=?`+famTw+` AND deleted_at IS NULL`, famArgs...).Scan(&famID)
			if famID == "" {
				famID = core.GenerateID("FAM")
				familyName := reg.ParentName + " Family"
				if reg.ParentName == "" {
					familyName = reg.Email
				}
				tx.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name,referral_code) VALUES(?,?,?,?,?,?,?)`,
					famID, tid, familyName, reg.Email, reg.Phone, reg.ParentName, core.NewReferralCode())
			}
			stuFamArgs := append([]any{famID, stuID}, famTwArgs...)
			tx.Exec(`UPDATE students SET family_id=? WHERE id=?`+famTw, stuFamArgs...)

			// Referral validation: if a code was supplied, look up the referrer
			// family and create a pending referral_rewards row. Self-referral and
			// invalid codes are ignored (logged) so they don't block approval.
			code := strings.ToUpper(strings.TrimSpace(reg.ReferralCode))
			if code != "" {
				var referrerFamID string
				refTw, refTwArgs := store.ScopeTenant(c, "")
				refArgs := append([]any{code}, refTwArgs...)
				tx.QueryRow(`SELECT id FROM families WHERE referral_code=?`+refTw+` AND deleted_at IS NULL`, refArgs...).Scan(&referrerFamID)
				if referrerFamID != "" && referrerFamID != famID {
					if _, err := tx.Exec(`INSERT INTO referral_rewards(id,tenant_id,referrer_family_id,referred_student_id,status) VALUES(?,?,?,?,'pending') ON CONFLICT(referred_student_id) DO NOTHING`,
						core.GenerateID("REF"), tid, referrerFamID, stuID); err == nil {
						stuRefArgs := append([]any{referrerFamID, stuID}, refTwArgs...)
						tx.Exec(`UPDATE students SET referred_by_family_id=? WHERE id=?`+refTw, stuRefArgs...)
						core.LogAudit(tx, c.Email, "referral_validated", "student", stuID, "code="+code+" referrer="+referrerFamID)
					}
				} else {
					reason := "code_not_found"
					if referrerFamID == famID {
						reason = "self_referral"
					}
					core.LogAudit(tx, c.Email, "referral_rejected", "student", stuID, "code="+code+" reason="+reason)
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

		// Mark registration approved (tenant-scoped — same tw/args computed above).
		regUpdArgs := append([]any{id}, regTwArgs...)
		if _, err := tx.Exec(`UPDATE registrations SET status='approved' WHERE id=?`+regTw, regUpdArgs...); err != nil {
			core.RespondError(w, "could not update registration status", 500)
			return
		}

		if err := tx.Commit(); err != nil {
			core.RespondError(w, "server error", 500)
			return
		}

		// Post-commit: if we just approved a teacher, mint a set-password
		// token and email the welcome link. Failure here is logged but does
		// NOT roll back the approval — the admin can hit "resend" later.
		if pendingTeacherEmail != "" {
			store.InvalidateOldTokens(db, pendingTeacherEmail, store.TokenPurposeSetPassword)
			tok, terr := store.CreateEmailToken(db, pendingTeacherEmail, store.TokenPurposeSetPassword, &pendingTeacherUserID, nil, store.SetPasswordTokenTTL)
			l := core.LogFromReq(r).With("teacher_email", pendingTeacherEmail, "user_id", pendingTeacherUserID)
			if terr != nil {
				l.Error("set-password token create failed", "err", terr)
			} else {
				setURL := mailer.AppURL() + "/set-password.html?token=" + tok
				if mErr := core.SendEmail(pendingTeacherEmail, "Welcome to The Study Hub — set your password", mailer.RenderTeacherWelcomeEmail(pendingTeacherName, setURL)); mErr != nil {
					l.Error("set-password mail send failed", "err", mErr)
				} else {
					l.Info("teacher approved, set-password email sent")
				}
			}
			core.LogAudit(db, c.Email, "teacher_approved", "user", fmt.Sprintf("%d", pendingTeacherUserID), pendingTeacherEmail)
		}

		core.Respond(w, responseData)
	}
}

// POST /api/enrollment-requests — authenticated parent.
// Creates a registration row with type='enrollment' for a child the parent
// wants to enrol. Admin reviews and approves (handleRegistrationApprove
// handles this case by creating just the student record + linking to the
// existing family, since the parent user already exists).
func HandleEnrollmentRequest(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if c == nil || c.Role != "parent" {
			core.RespondError(w, "parents only", 403)
			return
		}

		var req struct {
			StudentFirstName  string `json:"studentFirstName"`
			StudentLastName   string `json:"studentLastName"`
			StudentDOB        string `json:"studentDob"`
			StudentGender     string `json:"studentGender"`
			SchoolName        string `json:"schoolName"`
			YearGrade         string `json:"yearGrade"`
			SubjectInterest   string `json:"subjectInterest"`
			ClassTypeInterest string `json:"classTypeInterest"`
			WorkshopInterest  string `json:"workshopInterest"`
			Notes             string `json:"notes"`
			ReferralCode      string `json:"referralCode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			core.RespondError(w, "bad request", 400)
			return
		}
		if req.StudentFirstName == "" {
			core.RespondError(w, "student first name is required", 400)
			return
		}

		// Look up the parent's family so we know which family to link the
		// student to when admin approves. Tenant-scoped so a parent whose
		// email exists in multiple tenants gets the right family ID.
		var famID string
		famTw, famTwArgs := store.ScopeTenant(c, "")
		famLookupArgs := append([]any{c.Email}, famTwArgs...)
		db.QueryRow(`SELECT id FROM families WHERE contact=? AND deleted_at IS NULL`+famTw, famLookupArgs...).Scan(&famID)

		// Dedup: refuse if this parent already has a pending registration for
		// the same child (matched on first+last+DOB so siblings with the same
		// first name aren't blocked). Saves admin from a queue of identical
		// rows when a parent double-clicks or re-submits.
		dupArgs := append([]any{c.Email, strings.TrimSpace(req.StudentFirstName), strings.TrimSpace(req.StudentLastName), strings.TrimSpace(req.StudentDOB)}, famTwArgs...)
		var dupID string
		db.QueryRow(`SELECT id FROM registrations
		             WHERE email=? AND status='pending' AND type='enrollment'
		               AND lower(student_first_name)=lower(?) AND lower(student_last_name)=lower(?)
		               AND COALESCE(student_dob,'')=?
		`+famTw, dupArgs...).Scan(&dupID)
		if dupID != "" {
			core.RespondError(w, "you already have a pending enrolment request for this child", http.StatusConflict)
			return
		}

		id := core.GenerateID("REG")
		tid := store.TenantID(c)
		_, err := db.Exec(`INSERT INTO registrations(id,tenant_id,parent_name,email,phone,student_first_name,student_last_name,student_dob,student_gender,school_name,year_grade,class_type_interest,subject_interest,workshop_interest,notes,submitted_on,status,type,referral_code,email_verified_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,NOW())`,
			id, tid, c.Name, c.Email, "", // phone not needed — it's on the family
			req.StudentFirstName, req.StudentLastName, req.StudentDOB, req.StudentGender,
			req.SchoolName, req.YearGrade, req.ClassTypeInterest, req.SubjectInterest,
			req.WorkshopInterest, req.Notes, core.Today(), "pending", "enrollment",
			strings.ToUpper(strings.TrimSpace(req.ReferralCode)))
		if err != nil {
			core.RespondError(w, "could not save enrollment request", 500)
			return
		}

		core.LogAudit(db, c.Email, "enrollment_requested", "registration", id, req.StudentFirstName+" "+req.StudentLastName)

		// Validate referral code at submission time. We DON'T block the
		// enrollment if the code is invalid — we just include a warning so
		// the parent knows to double-check before admin processes it.
		codeWarning := ""
		code := strings.ToUpper(strings.TrimSpace(req.ReferralCode))
		if code != "" {
			var referrerFamID string
			codeLookupArgs := append([]any{code}, famTwArgs...)
			db.QueryRow(`SELECT id FROM families WHERE referral_code=? AND deleted_at IS NULL`+famTw, codeLookupArgs...).Scan(&referrerFamID)
			if referrerFamID == "" {
				codeWarning = "The referral code '" + code + "' was not found. Your enrolment has been submitted anyway — please double-check the code with your friend."
			} else if referrerFamID == famID {
				codeWarning = "You can't use your own referral code. The enrolment has been submitted without a referral."
				// Clear the invalid self-referral from the row (tenant-scoped).
				clearArgs := append([]any{id}, famTwArgs...)
				if _, err := db.Exec(`UPDATE registrations SET referral_code='' WHERE id=?`+famTw, clearArgs...); err != nil {
					core.LogFromReq(r).Error("failed to clear self-referral code", "err", err, "registration_id", id)
				}
			}
		}

		msg := "Enrolment request submitted. Our team will review it shortly."
		if codeWarning != "" {
			msg = codeWarning
		}

		w.WriteHeader(http.StatusCreated)
		core.Respond(w, map[string]any{
			"id":          id,
			"status":      "pending",
			"message":     msg,
			"codeWarning": codeWarning,
		})

	}
}

// DELETE /api/registrations/{id} — admin only (reject)
func HandleRegistrationReject(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		regTw, regTwArgs := store.ScopeTenant(core.ClaimsFrom(r), "")
		regArgs := append([]any{id}, regTwArgs...)
		if _, err := db.Exec(`UPDATE registrations SET status='rejected' WHERE id=?`+regTw, regArgs...); err != nil {
			core.RespondError(w, "could not reject registration", 500)
			return
		}
		if c := core.ClaimsFrom(r); c != nil {
			core.LogAudit(db, c.Email, "registration_rejected", "registration", id, "")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func HandleRegisterTeacher(db *store.DB) http.HandlerFunc {
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
			core.RespondError(w, "bad request", 400)
			return
		}
		if reg.FullName == "" || reg.Email == "" {
			core.RespondError(w, "full name and email are required", 400)
			return
		}
		if !auth.ValidateEmail(reg.Email) {
			core.RespondError(w, "invalid email address", 400)
			return
		}
		if reg.DisplayName == "" {
			reg.DisplayName = reg.FullName
		}
		if reg.EmploymentType == "" {
			reg.EmploymentType = "Full-time"
		}

		id := core.GenerateID("REG")
		email := strings.ToLower(strings.TrimSpace(reg.Email))
		_, err := db.Exec(`INSERT INTO registrations(id,tenant_id,parent_name,email,phone,emergency_name,emergency_phone,student_first_name,student_last_name,student_dob,student_gender,gender,school_name,year_grade,class_type_interest,subject_interest,school_fees,registration_date,workshop_interest,class_interest,notes,submitted_on,status,type,specialization,nric,display_name,employment_type,experience,qualifications,bio,schedule,expected_salary) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, 1, reg.FullName, email, reg.Phone, reg.EmergencyName, reg.EmergencyPhone,
			reg.FullName, "", "", "", "", "", "", "", "", 0, "", "",
			"", reg.Bio, core.Today(), "pending", "teacher",
			reg.Specialty, reg.NRIC, reg.DisplayName, reg.EmploymentType, reg.Experience, reg.Qualifications, reg.Bio, reg.Schedule, reg.ExpectedSalary)
		if err != nil {
			core.RespondError(w, "could not save registration", 500)
			return
		}

		// Mint a verify_teacher token + send confirmation email. No user
		// account is created at this stage — that only happens after admin
		// approves the application via handleRegistrationApprove.
		token, terr := store.CreateEmailToken(db, email, store.TokenPurposeVerifyTeacher, nil, &id, store.VerifyTokenTTL)
		l := core.LogFromReq(r).With("registration_id", id, "email", email)
		if terr == nil {
			verifyURL := mailer.AppURL() + "/verify.html?token=" + token
			if mErr := core.SendEmail(email, "Confirm your Study Hub teacher application", mailer.RenderVerifyTeacherEmail(reg.FullName, verifyURL)); mErr != nil {
				l.Error("teacher register mail send failed", "err", mErr)
			} else {
				l.Info("teacher application received")
			}
		} else {
			l.Error("teacher register token create failed", "err", terr)
		}

		core.LogAudit(db, email, "teacher_self_registered", "registration", id, reg.FullName)

		w.WriteHeader(http.StatusCreated)
		core.Respond(w, map[string]string{
			"id":      id,
			"status":  "pending_verification",
			"type":    "teacher",
			"message": "Application received. Check your email for the confirmation link.",
		})

	}
}
