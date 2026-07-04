package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"studyhub/internal/auth"
	"studyhub/internal/core"
	"studyhub/internal/mailer"
	"studyhub/internal/models"
	"studyhub/internal/store"
	"time"

	"github.com/go-chi/chi/v5"
)

// ── Students ──────────────────────────────────────────────────────────────────

// teacherClassIDSet returns the set of class IDs a teacher (identified by
// claims email) is currently assigned to. Used to scope student lists so a
// teacher can only see kids enrolled in their own classes.
func teacherClassIDSet(db *store.DB, c *core.Claims) map[string]bool {
	out := map[string]bool{}
	if c == nil || c.Role != "teacher" {
		return out
	}
	var staffID string
	db.QueryRow(`SELECT id FROM staff WHERE email=? AND deleted_at IS NULL LIMIT 1`, c.Email).Scan(&staffID)
	if staffID == "" {
		return out
	}
	tw, twArgs := store.ScopeTenant(c, "")
	args := append(append([]any{}, twArgs...), staffID)
	rows, err := db.Query(`SELECT id FROM classes WHERE deleted_at IS NULL`+tw+` AND teacher_ids LIKE '%"'||?||'"%'`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			out[id] = true
		}
	}
	return out
}

func studentInClassSet(s models.Student, classIDs map[string]bool) bool {
	for _, cid := range s.EnrolledClasses {
		if classIDs[cid] {
			return true
		}
	}
	return false
}

func listStudents(db *store.DB, c *core.Claims) []models.Student {
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		// Parents are always tenant-scoped — drop the OR pattern.
		tid := store.TenantID(c)
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,COALESCE(medical_info,''),COALESCE(allergies,''),COALESCE(family_id,''),COALESCE(referred_by_family_id,''),COALESCE(package_amount,0),COALESCE(package_self_study_hours,4),COALESCE(subscription_status,'active'),paused_at,resumed_at,COALESCE(dropin_self_study,false) FROM students WHERE contact=? AND tenant_id=? AND deleted_at IS NULL ORDER BY registered_on`, c.Email, tid)
	} else {
		tw, twArgs := store.ScopeTenant(c, "")
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,COALESCE(medical_info,''),COALESCE(allergies,''),COALESCE(family_id,''),COALESCE(referred_by_family_id,''),COALESCE(package_amount,0),COALESCE(package_self_study_hours,4),COALESCE(subscription_status,'active'),paused_at,resumed_at,COALESCE(dropin_self_study,false) FROM students WHERE deleted_at IS NULL`+tw+` ORDER BY registered_on`, twArgs...)
	}
	if err != nil {
		return []models.Student{}
	}
	defer rows.Close()
	var classIDs map[string]bool
	isTeacher := c != nil && c.Role == "teacher"
	if isTeacher {
		classIDs = teacherClassIDSet(db, c)
	}
	out := []models.Student{}
	for rows.Next() {
		var s models.Student
		var ec, sib string
		var e2name, e2phone, pausedAt, resumedAt sql.NullString
		if err := rows.Scan(&s.ID, &s.FirstName, &s.LastName, &s.DOB, &s.Gender, &s.ParentName, &s.Contact, &s.Phone, &s.Branch, &s.Status, &s.RegisteredOn, &ec, &sib, &s.Notes, &e2name, &e2phone, &s.MedicalInfo, &s.Allergies, &s.FamilyID, &s.ReferredByFamilyID, &s.PackageAmount, &s.PackageSelfStudyHours, &s.SubscriptionStatus, &pausedAt, &resumedAt, &s.DropinSelfStudy); err != nil {
			continue
		}
		s.EnrolledClasses = models.ParseArr(ec)
		s.Siblings = models.ParseArr(sib)
		s.Emergency2Name = models.NullStr(e2name)
		s.Emergency2Phone = models.NullStr(e2phone)
		if pausedAt.Valid {
			s.PausedAt = &pausedAt.String
		}
		if resumedAt.Valid {
			s.ResumedAt = &resumedAt.String
		}
		if isTeacher {
			if !studentInClassSet(s, classIDs) {
				continue
			}
			redactContactForTeacher(&s)
		}
		out = append(out, s)
	}
	return out
}

// redactContactForTeacher strips contact + internal-notes fields a teacher must
// not see. Teachers keep health (medical/allergies), DOB, classes and status;
// parent name/email/phone, emergency contact and admin notes are cleared. This
// runs in listStudents, which feeds BOTH /api/students and the snapshot, so the
// data never reaches a teacher's browser.
func redactContactForTeacher(s *models.Student) {
	s.ParentName = ""
	s.Contact = ""
	s.Phone = ""
	s.Emergency2Name = ""
	s.Emergency2Phone = ""
	s.Notes = ""
}

func listStudentsPaged(db *store.DB, c *core.Claims, p core.Pagination) ([]models.Student, int) {
	// Teachers see only students in their own classes — a class-set filter
	// that doesn't fit cleanly in SQL with the existing JSON-string columns,
	// so list-then-slice in Go. Teacher rosters stay small enough that this
	// is fine.
	if c != nil && c.Role == "teacher" {
		all := listStudents(db, c)
		total := len(all)
		start := p.Offset
		if start > total {
			start = total
		}
		end := start + p.Limit
		if end > total {
			end = total
		}
		return all[start:end], total
	}
	var total int
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		// Parents are always tenant-scoped — drop the OR pattern.
		tid := store.TenantID(c)
		db.QueryRow(`SELECT COUNT(*) FROM students WHERE contact=? AND tenant_id=? AND deleted_at IS NULL`, c.Email, tid).Scan(&total)
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,COALESCE(medical_info,''),COALESCE(allergies,''),COALESCE(family_id,''),COALESCE(referred_by_family_id,''),COALESCE(package_amount,0),COALESCE(package_self_study_hours,4),COALESCE(subscription_status,'active'),paused_at,resumed_at,COALESCE(dropin_self_study,false) FROM students WHERE contact=? AND tenant_id=? AND deleted_at IS NULL ORDER BY registered_on LIMIT ? OFFSET ?`, c.Email, tid, p.Limit, p.Offset)
	} else {
		tw, twArgs := store.ScopeTenant(c, "")
		db.QueryRow(`SELECT COUNT(*) FROM students WHERE deleted_at IS NULL`+tw, twArgs...).Scan(&total)
		pageArgs := append(append([]any{}, twArgs...), p.Limit, p.Offset)
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,COALESCE(medical_info,''),COALESCE(allergies,''),COALESCE(family_id,''),COALESCE(referred_by_family_id,''),COALESCE(package_amount,0),COALESCE(package_self_study_hours,4),COALESCE(subscription_status,'active'),paused_at,resumed_at,COALESCE(dropin_self_study,false) FROM students WHERE deleted_at IS NULL`+tw+` ORDER BY registered_on LIMIT ? OFFSET ?`, pageArgs...)
	}
	if err != nil {
		return []models.Student{}, total
	}
	defer rows.Close()
	out := []models.Student{}
	for rows.Next() {
		var s models.Student
		var ec, sib string
		var e2name, e2phone, pausedAt, resumedAt sql.NullString
		if err := rows.Scan(&s.ID, &s.FirstName, &s.LastName, &s.DOB, &s.Gender, &s.ParentName, &s.Contact, &s.Phone, &s.Branch, &s.Status, &s.RegisteredOn, &ec, &sib, &s.Notes, &e2name, &e2phone, &s.MedicalInfo, &s.Allergies, &s.FamilyID, &s.ReferredByFamilyID, &s.PackageAmount, &s.PackageSelfStudyHours, &s.SubscriptionStatus, &pausedAt, &resumedAt, &s.DropinSelfStudy); err != nil {
			continue
		}
		s.EnrolledClasses = models.ParseArr(ec)
		s.Siblings = models.ParseArr(sib)
		s.Emergency2Name = models.NullStr(e2name)
		s.Emergency2Phone = models.NullStr(e2phone)
		if pausedAt.Valid {
			s.PausedAt = &pausedAt.String
		}
		if resumedAt.Valid {
			s.ResumedAt = &resumedAt.String
		}
		out = append(out, s)
	}
	return out, total
}

func HandleStudents(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			p := core.ParsePagination(r)
			if !p.Active {
				core.Respond(w, listStudents(db, c))
				return
			}
			data, total := listStudentsPaged(db, c, p)
			core.Respond(w, core.PaginatedResponse{Data: data, Total: total, Limit: p.Limit, Offset: p.Offset})
		case http.MethodPost:
			if !core.IsAdminRole(c) {
				core.RespondError(w, "admin only", 403)
				return
			}
			var s models.Student
			if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
				core.RespondError(w, "bad body", 400)
				return
			}
			// Normalise contact to lowercase/trimmed so it matches the
			// parent's login email exactly. Without this, an admin who
			// types "John@Email.com" would silently hide the kid from a
			// parent who logs in as "john@email.com".
			s.Contact = strings.ToLower(strings.TrimSpace(s.Contact))
			if msg := validationError("firstName", s.FirstName, "lastName", s.LastName, "contact", s.Contact); msg != "" {
				core.RespondError(w, msg, 400)
				return
			}
			if s.Status == "" {
				s.Status = "New"
			}
			if s.ID == "" {
				s.ID = core.GenerateID("STU")
			}
			if s.RegisteredOn == "" {
				s.RegisteredOn = core.Today()
			}
			tid := store.TenantID(c)
			// Auto-find or create family for this student
			if s.FamilyID == "" && s.Contact != "" {
				var famID string
				famTw, famTwArgs := store.ScopeTenant(c, "")
				famArgs := append([]any{s.Contact}, famTwArgs...)
				db.QueryRow(`SELECT id FROM families WHERE contact=?`+famTw+` AND deleted_at IS NULL`, famArgs...).Scan(&famID)
				if famID == "" {
					famID = core.GenerateID("FAM")
					familyName := s.ParentName + " Family"
					if s.ParentName == "" {
						familyName = s.Contact
					}
					db.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name,referral_code) VALUES(?,?,?,?,?,?,?)`,
						famID, tid, familyName, s.Contact, s.Phone, s.ParentName, core.NewReferralCode())
				}
				s.FamilyID = famID
			}
			if s.SubscriptionStatus == "" {
				s.SubscriptionStatus = "active"
			}
			if s.PackageSelfStudyHours == 0 {
				s.PackageSelfStudyHours = 4
			}
			// Siblings is derived from family membership — ignore any
			// client-supplied value, persist an empty placeholder, then
			// recompute the JSON for every member of this family in one pass.
			_, err := db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,medical_info,allergies,family_id,referred_by_family_id,package_amount,package_self_study_hours,subscription_status,dropin_self_study) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				s.ID, tid, s.FirstName, s.LastName, s.DOB, s.Gender, s.ParentName, s.Contact, s.Phone, s.Branch, s.Status, s.RegisteredOn, models.JSONArr(s.EnrolledClasses), "[]", s.Notes, s.Emergency2Name, s.Emergency2Phone, s.MedicalInfo, s.Allergies, s.FamilyID, s.ReferredByFamilyID, s.PackageAmount, s.PackageSelfStudyHours, s.SubscriptionStatus, s.DropinSelfStudy)
			if err != nil {
				core.RespondError(w, "server error", 500)
				return
			}
			recomputeFamilySiblings(db, c, s.FamilyID)
			recomputeClassEnrollment(db, c, s.EnrolledClasses)
			// Ensure the parent (matched by contact email) has a login account.
			// If not, create one in pending_verification status and email a
			// set-password link so the parent can claim the account.
			ensureParentUserAccount(db, r, tid, s.Contact, s.ParentName)
			core.Respond(w, s)
		}
	}
}

func HandleStudent(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		switch r.Method {
		case http.MethodPut:
			var s models.Student
			if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
				core.RespondError(w, "bad body", 400)
				return
			}
			s.ID = id
			// Same normalisation as create — keep contact matchable against
			// the lowercased login email parents authenticate with.
			s.Contact = strings.ToLower(strings.TrimSpace(s.Contact))
			tw, twArgs := store.ScopeTenant(c, "")
			// Read previous family_id and enrolled_classes so we can recompute
			// siblings + class counts on both sides when membership changes.
			// siblings is derived — client input is ignored.
			var oldFamilyID, oldEnrolledRaw string
			oldArgs := append([]any{id}, twArgs...)
			db.QueryRow(`SELECT COALESCE(family_id,''), COALESCE(enrolled_classes,'[]') FROM students WHERE id=?`+tw, oldArgs...).Scan(&oldFamilyID, &oldEnrolledRaw)
			oldEnrolled := models.ParseArr(oldEnrolledRaw)
			// Capacity gate: any class the student is being ADDED to must
			// still have room. Existing enrolments are not re-checked.
			oldSet := map[string]bool{}
			for _, c := range oldEnrolled {
				oldSet[c] = true
			}
			for _, cid := range s.EnrolledClasses {
				if oldSet[cid] {
					continue
				}
				var enrolled, capacity int
				capArgs := append([]any{cid}, twArgs...)
				if err := db.QueryRow(`SELECT COALESCE(enrolled,0), COALESCE(capacity,0) FROM classes WHERE id=? AND deleted_at IS NULL`+tw, capArgs...).Scan(&enrolled, &capacity); err != nil {
					core.RespondError(w, "class not found: "+cid, http.StatusBadRequest)
					return
				}
				if capacity > 0 && enrolled >= capacity {
					core.RespondError(w, "class is full: "+cid, http.StatusConflict)
					return
				}
			}
			args := append([]any{s.FirstName, s.LastName, s.DOB, s.Gender, s.ParentName, s.Contact, s.Phone, s.Branch, s.Status, models.JSONArr(s.EnrolledClasses), s.Notes, s.Emergency2Name, s.Emergency2Phone, s.MedicalInfo, s.Allergies, s.FamilyID, s.PackageAmount, s.PackageSelfStudyHours, s.DropinSelfStudy, id}, twArgs...)
			res, err := db.Exec(`UPDATE students SET first_name=?,last_name=?,dob=?,gender=?,parent_name=?,contact=?,phone=?,branch=?,status=?,enrolled_classes=?,notes=?,emergency2_name=?,emergency2_phone=?,medical_info=?,allergies=?,family_id=?,package_amount=?,package_self_study_hours=?,dropin_self_study=? WHERE id=?`+tw, args...)
			if err != nil {
				core.RespondError(w, "could not update student", 500)
				return
			}
			if n, _ := res.RowsAffected(); n == 0 {
				core.RespondError(w, "student not found", http.StatusNotFound)
				return
			}
			recomputeFamilySiblings(db, c, s.FamilyID)
			if oldFamilyID != "" && oldFamilyID != s.FamilyID {
				recomputeFamilySiblings(db, c, oldFamilyID)
			}
			recomputeClassEnrollment(db, c, append(append([]string{}, oldEnrolled...), s.EnrolledClasses...))
			core.LogAudit(db, c.Email, "student_updated", "student", id, s.FirstName+" "+s.LastName)
			core.Respond(w, s)
		case http.MethodDelete:
			tw, twArgs := store.ScopeTenant(c, "")
			// Read enrolled_classes and family_id before soft-delete so we can
			// keep the class-enrollment counter and sibling JSON in sync.
			var enrolledRaw, famID string
			readArgs := append([]any{id}, twArgs...)
			db.QueryRow(`SELECT COALESCE(enrolled_classes,'[]'), COALESCE(family_id,'') FROM students WHERE id=?`+tw, readArgs...).Scan(&enrolledRaw, &famID)
			classIDs := models.ParseArr(enrolledRaw)
			args := append([]any{id}, twArgs...)
			res, err := db.Exec(`UPDATE students SET deleted_at=NOW() WHERE id=?`+tw+` AND deleted_at IS NULL`, args...)
			if err != nil {
				core.RespondError(w, "could not delete student", 500)
				return
			}
			if n, _ := res.RowsAffected(); n == 0 {
				core.RespondError(w, "student not found", http.StatusNotFound)
				return
			}
			recomputeClassEnrollment(db, c, classIDs)
			recomputeFamilySiblings(db, c, famID)
			core.LogAudit(db, c.Email, "student_deleted", "student", id, "soft deleted")
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// handleStudentSubscription pauses, resumes, or freezes a student's monthly
// subscription. Pausing skips them in the monthly invoice cron and hides them
// from rosters; resuming restores them; freeze is a separate flag with the
// same effect that's surfaced differently in reports.
func HandleStudentSubscription(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var body struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		var newStatus string
		switch body.Action {
		case "pause":
			newStatus = "paused"
		case "freeze":
			newStatus = "frozen"
		case "resume":
			newStatus = "active"
		default:
			core.RespondError(w, "action must be pause, freeze, or resume", 400)
			return
		}
		tw, twArgs := store.ScopeTenant(c, "")
		now := time.Now().UTC().Format(time.RFC3339)
		if newStatus == "active" {
			args := append([]any{newStatus, now, id}, twArgs...)
			if _, err := db.Exec(`UPDATE students SET subscription_status=?, resumed_at=? WHERE id=?`+tw, args...); err != nil {
				core.RespondError(w, "could not update subscription", 500)
				return
			}
		} else {
			args := append([]any{newStatus, now, id}, twArgs...)
			if _, err := db.Exec(`UPDATE students SET subscription_status=?, paused_at=? WHERE id=?`+tw, args...); err != nil {
				core.RespondError(w, "could not update subscription", 500)
				return
			}
		}
		core.LogAudit(db, c.Email, "subscription_"+body.Action, "student", id, newStatus)
		core.Respond(w, map[string]string{"subscriptionStatus": newStatus})
	}
}

// ensureParentUserAccount creates a parent user for the given contact email
// if one doesn't already exist. The user is placed in pending_verification
// status with a discarded random password; we mint a set_password token and
// email the link so the parent picks their own. Failures only log — the
// student record is already created, so the admin can hit "resend" later
// without retrying the whole operation.
func ensureParentUserAccount(db *store.DB, r *http.Request, tid int, contact, parentName string) {
	if contact == "" {
		return
	}
	var existing int
	db.QueryRow(`SELECT id FROM users WHERE email=?`, contact).Scan(&existing)
	if existing > 0 {
		return
	}
	placeholderBytes := make([]byte, 32)
	if _, err := rand.Read(placeholderBytes); err != nil {
		core.Logger.Error("ensureParentUserAccount: rand failed", "err", err)
		return
	}
	placeholderHash, err := auth.HashPassword(hex.EncodeToString(placeholderBytes))
	if err != nil {
		core.Logger.Error("ensureParentUserAccount: hash failed", "err", err)
		return
	}
	var userID int64
	if err := db.QueryRow(`INSERT INTO users(tenant_id,email,password_hash,role,name,status) VALUES(?,?,?,?,?,?) ON CONFLICT(email) DO NOTHING RETURNING id`,
		tid, contact, placeholderHash, "parent", parentName, "pending_verification").Scan(&userID); err != nil {
		return // existing row (race) or insert failed — nothing to follow up
	}
	tok, terr := store.CreateEmailToken(db, contact, store.TokenPurposeSetPassword, &userID, nil, store.SetPasswordTokenTTL)
	if terr != nil {
		core.LogFromReq(r).Error("ensureParentUserAccount: token create failed", "err", terr, "email", contact)
		return
	}
	setURL := mailer.AppURL() + "/set-password.html?token=" + tok
	go func() {
		if err := core.SendEmail(contact, "Welcome to The Study Hub — set your password", mailer.RenderParentWelcomeEmail(parentName, setURL)); err != nil {
			core.Logger.Error("parent welcome email failed", "err", err, "email", contact)
		}
	}()
	core.LogAudit(db, "system", "parent_user_created", "user", fmt.Sprintf("%d", userID), "via add-student")
}

// recomputeClassEnrollment recounts students enrolled in each given class and
// writes the result back to classes.enrolled. The counter drifted whenever a
// student PUT changed enrolled_classes without bumping classes.enrolled —
// callers that mutate enrolled_classes should call this with the union of old
// and new class IDs. Tenant-scoped so cross-tenant id collisions can't taint
// the count.
func recomputeClassEnrollment(db *store.DB, c *core.Claims, classIDs []string) {
	if len(classIDs) == 0 {
		return
	}
	tw, twArgs := store.ScopeTenant(c, "")
	seen := map[string]bool{}
	for _, cid := range classIDs {
		if cid == "" || seen[cid] {
			continue
		}
		seen[cid] = true
		var count int
		countArgs := append([]any{cid}, twArgs...)
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM students WHERE deleted_at IS NULL AND enrolled_classes LIKE '%"'||?||'"%'`+tw,
			countArgs...,
		).Scan(&count); err != nil {
			core.Logger.Error("recompute class enrollment count failed", "err", err, "class_id", cid)
			continue
		}
		updArgs := append([]any{count, cid}, twArgs...)
		if _, err := db.Exec(`UPDATE classes SET enrolled=? WHERE id=?`+tw, updArgs...); err != nil {
			core.Logger.Error("recompute class enrollment update failed", "err", err, "class_id", cid)
		}
	}
}

// recomputeFamilySiblings rewrites the students.siblings JSON array for every
// active student in the given family so each row reflects current membership.
// This is the single source of truth: any handler that mutates family
// membership (POST student, PUT student, relink) calls this to keep the JSON
// in lockstep with family_id. Without it the siblings column drifted whenever
// a new kid joined a family. Empty familyID is a no-op.
func recomputeFamilySiblings(db *store.DB, c *core.Claims, familyID string) {
	if familyID == "" {
		return
	}
	tw, twArgs := store.ScopeTenant(c, "")
	args := append([]any{familyID}, twArgs...)
	rows, err := db.Query(`SELECT id FROM students WHERE family_id=? AND deleted_at IS NULL`+tw, args...)
	if err != nil {
		core.Logger.Error("recompute siblings query failed", "err", err, "family_id", familyID)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		others := make([]string, 0, len(ids)-1)
		for _, other := range ids {
			if other != id {
				others = append(others, other)
			}
		}
		updArgs := append([]any{models.JSONArr(others), id}, twArgs...)
		if _, err := db.Exec(`UPDATE students SET siblings=? WHERE id=?`+tw, updArgs...); err != nil {
			core.Logger.Error("recompute siblings update failed", "err", err, "student_id", id)
		}
	}
}

// resolveFamilyForContact finds the family row matching the given parent
// email; if none exists it creates one with the supplied parent name and a
// fresh referral code. Returns the family id and a flag indicating whether a
// new row was created. An empty contact yields ("", false, nil).
func resolveFamilyForContact(db *store.DB, c *core.Claims, contact, parentName, phone string) (string, bool, error) {
	if contact == "" {
		return "", false, nil
	}
	tid := store.TenantID(c)
	famTw, famTwArgs := store.ScopeTenant(c, "")
	var famID string
	args := append([]any{contact}, famTwArgs...)
	db.QueryRow(`SELECT id FROM families WHERE contact=?`+famTw+` AND deleted_at IS NULL`, args...).Scan(&famID)
	if famID != "" {
		return famID, false, nil
	}
	famID = core.GenerateID("FAM")
	familyName := parentName + " Family"
	if parentName == "" {
		familyName = contact
	}
	if _, err := db.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name,referral_code) VALUES(?,?,?,?,?,?,?)`,
		famID, tid, familyName, contact, phone, parentName, core.NewReferralCode()); err != nil {
		return "", false, err
	}
	return famID, true, nil
}

// handleStudentRelink lets an admin change which parent / family a student is
// attached to. Body: {contact, parentName?, phone?}. An empty contact unlinks
// the student (clears contact, parent_name, family_id) — useful when the
// original parent email was wrong and a fresh link is being set up.
type relinkRequest struct {
	Contact    string `json:"contact"`
	ParentName string `json:"parentName"`
	Phone      string `json:"phone"`
}

type relinkResponse struct {
	StudentID   string `json:"studentId"`
	Contact     string `json:"contact"`
	ParentName  string `json:"parentName"`
	FamilyID    string `json:"familyId"`
	FamilyName  string `json:"familyName"`
	IsNewFamily bool   `json:"isNewFamily"`
}

func HandleStudentRelink(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var req relinkRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		req.Contact = strings.ToLower(strings.TrimSpace(req.Contact))
		if req.Contact != "" && !auth.ValidateEmail(req.Contact) {
			core.RespondError(w, "invalid email", 400)
			return
		}

		tw, twArgs := store.ScopeTenant(c, "")
		var oldContact string
		oldArgs := append([]any{id}, twArgs...)
		if err := db.QueryRow(`SELECT COALESCE(contact,'') FROM students WHERE id=?`+tw+` AND deleted_at IS NULL`, oldArgs...).Scan(&oldContact); err != nil {
			core.RespondError(w, "student not found", 404)
			return
		}

		famID, isNew, err := resolveFamilyForContact(db, c, req.Contact, req.ParentName, req.Phone)
		if err != nil {
			core.RespondError(w, "could not resolve family", 500)
			return
		}

		// Read old family to recompute siblings on both sides.
		var oldFamilyID string
		oldFamArgs := append([]any{id}, twArgs...)
		db.QueryRow(`SELECT COALESCE(family_id,'') FROM students WHERE id=?`+tw, oldFamArgs...).Scan(&oldFamilyID)

		updArgs := append([]any{req.Contact, req.ParentName, famID, id}, twArgs...)
		if _, err := db.Exec(`UPDATE students SET contact=?, parent_name=?, family_id=? WHERE id=?`+tw, updArgs...); err != nil {
			core.RespondError(w, "could not relink student", 500)
			return
		}
		recomputeFamilySiblings(db, c, famID)
		if oldFamilyID != "" && oldFamilyID != famID {
			recomputeFamilySiblings(db, c, oldFamilyID)
		}

		detail := oldContact + " -> " + req.Contact
		if req.Contact == "" {
			detail = oldContact + " -> (unlinked)"
		}
		core.LogAudit(db, c.Email, "student_relinked", "student", id, detail)

		var familyName string
		if famID != "" {
			famTw, famTwArgs := store.ScopeTenant(c, "")
			famArgs := append([]any{famID}, famTwArgs...)
			db.QueryRow(`SELECT name FROM families WHERE id=?`+famTw, famArgs...).Scan(&familyName)
		}
		core.Respond(w, relinkResponse{
			StudentID:   id,
			Contact:     req.Contact,
			ParentName:  req.ParentName,
			FamilyID:    famID,
			FamilyName:  familyName,
			IsNewFamily: isNew,
		})

	}
}

// noteMaxLen caps a single appended note so a teacher can't balloon the
// students.notes column with one request.
const noteMaxLen = 2000

type studentNoteRequest struct {
	Note string `json:"note"`
}

// HandleStudentNote appends a single note to a student's admin-facing notes.
// Admin and teacher may call it; a teacher may only note a student enrolled in
// one of their own classes. The note is appended server-side (never a full-row
// PUT) so a teacher can't overwrite other student fields.
func HandleStudentNote(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsStaffRole(c) {
			core.RespondError(w, "staff only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var req studentNoteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		note := strings.TrimSpace(req.Note)
		if note == "" {
			core.RespondError(w, "note is required", 400)
			return
		}
		if len(note) > noteMaxLen {
			note = note[:noteMaxLen]
		}
		// Teachers can only note a student in one of their own classes.
		if c.Role == "teacher" && !teacherOwnsStudent(db, c, id) {
			core.RespondError(w, "not your student", 403)
			return
		}
		tw, twArgs := store.ScopeTenant(c, "")
		args := append([]any{note, note, id}, twArgs...)
		res, err := db.Exec(`UPDATE students SET notes = CASE WHEN COALESCE(notes,'')='' THEN ? ELSE notes || E'\n' || ? END WHERE id=?`+tw+` AND deleted_at IS NULL`, args...)
		if err != nil {
			core.RespondError(w, "could not save note", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			core.RespondError(w, "student not found", 404)
			return
		}
		core.LogAudit(db, c.Email, "student_note_added", "student", id, note)
		core.Respond(w, map[string]any{"ok": true})
	}
}

// teacherOwnsStudent reports whether the student is enrolled in one of the
// teacher's classes — the same class-set check listStudents uses.
func teacherOwnsStudent(db *store.DB, c *core.Claims, studentID string) bool {
	classIDs := teacherClassIDSet(db, c)
	if len(classIDs) == 0 {
		return false
	}
	tw, twArgs := store.ScopeTenant(c, "")
	args := append([]any{studentID}, twArgs...)
	var enrolledRaw string
	if err := db.QueryRow(`SELECT COALESCE(enrolled_classes,'[]') FROM students WHERE id=?`+tw+` AND deleted_at IS NULL`, args...).Scan(&enrolledRaw); err != nil {
		return false
	}
	for _, cid := range models.ParseArr(enrolledRaw) {
		if classIDs[cid] {
			return true
		}
	}
	return false
}
