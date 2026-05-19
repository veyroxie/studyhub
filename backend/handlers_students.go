package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

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

// scopeTenant returns a SQL fragment that scopes a query to the caller's
// tenant, plus the args to thread through. For tenant-scoped users it
// appends "AND tenant_id = ?" (or "AND <alias>.tenant_id = ?"); for
// superadmin (tid=0) it returns the empty string and no args, granting
// cross-tenant visibility.
//
// Callers concatenate the clause directly into their SQL — preserving the
// `?` placeholder convention used throughout the project — and append
// twArgs to their existing args slice.
//
// Why this matters: the previous "(tenant_id=? OR ?=0)" pattern prevented
// PostgreSQL from using the composite (tenant_id, deleted_at) indexes
// because the planner couldn't pick a generic plan. The helper keeps the
// superadmin escape hatch while letting the common case use indexes.
func scopeTenant(c *Claims, alias string) (string, []any) {
	tid := tenantID(c)
	if tid == 0 {
		return "", nil
	}
	if alias == "" {
		return " AND tenant_id = ?", []any{tid}
	}
	return " AND " + alias + ".tenant_id = ?", []any{tid}
}

// teacherClassIDSet returns the set of class IDs a teacher (identified by
// claims email) is currently assigned to. Used to scope student lists so a
// teacher can only see kids enrolled in their own classes.
func teacherClassIDSet(db *DB, c *Claims) map[string]bool {
	out := map[string]bool{}
	if c == nil || c.Role != "teacher" {
		return out
	}
	var staffID string
	db.QueryRow(`SELECT id FROM staff WHERE email=? AND deleted_at IS NULL LIMIT 1`, c.Email).Scan(&staffID)
	if staffID == "" {
		return out
	}
	tw, twArgs := scopeTenant(c, "")
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

func studentInClassSet(s Student, classIDs map[string]bool) bool {
	for _, cid := range s.EnrolledClasses {
		if classIDs[cid] {
			return true
		}
	}
	return false
}

func listStudents(db *DB, c *Claims) []Student {
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		// Parents are always tenant-scoped — drop the OR pattern.
		tid := tenantID(c)
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,COALESCE(medical_info,''),COALESCE(allergies,''),COALESCE(family_id,''),COALESCE(referred_by_family_id,''),COALESCE(package_amount,0),COALESCE(package_self_study_hours,4),COALESCE(subscription_status,'active'),paused_at,resumed_at FROM students WHERE contact=? AND tenant_id=? AND deleted_at IS NULL ORDER BY registered_on`, c.Email, tid)
	} else {
		tw, twArgs := scopeTenant(c, "")
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,COALESCE(medical_info,''),COALESCE(allergies,''),COALESCE(family_id,''),COALESCE(referred_by_family_id,''),COALESCE(package_amount,0),COALESCE(package_self_study_hours,4),COALESCE(subscription_status,'active'),paused_at,resumed_at FROM students WHERE deleted_at IS NULL`+tw+` ORDER BY registered_on`, twArgs...)
	}
	if err != nil {
		return []Student{}
	}
	defer rows.Close()
	var classIDs map[string]bool
	isTeacher := c != nil && c.Role == "teacher"
	if isTeacher {
		classIDs = teacherClassIDSet(db, c)
	}
	out := []Student{}
	for rows.Next() {
		var s Student
		var ec, sib string
		var e2name, e2phone, pausedAt, resumedAt sql.NullString
		if err := rows.Scan(&s.ID, &s.FirstName, &s.LastName, &s.DOB, &s.Gender, &s.ParentName, &s.Contact, &s.Phone, &s.Branch, &s.Status, &s.RegisteredOn, &ec, &sib, &s.Notes, &e2name, &e2phone, &s.MedicalInfo, &s.Allergies, &s.FamilyID, &s.ReferredByFamilyID, &s.PackageAmount, &s.PackageSelfStudyHours, &s.SubscriptionStatus, &pausedAt, &resumedAt); err != nil {
			continue
		}
		s.EnrolledClasses = parseArr(ec)
		s.Siblings = parseArr(sib)
		s.Emergency2Name = nullStr(e2name)
		s.Emergency2Phone = nullStr(e2phone)
		if pausedAt.Valid {
			s.PausedAt = &pausedAt.String
		}
		if resumedAt.Valid {
			s.ResumedAt = &resumedAt.String
		}
		if isTeacher && !studentInClassSet(s, classIDs) {
			continue
		}
		out = append(out, s)
	}
	return out
}

func listStudentsPaged(db *DB, c *Claims, p Pagination) ([]Student, int) {
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
		tid := tenantID(c)
		db.QueryRow(`SELECT COUNT(*) FROM students WHERE contact=? AND tenant_id=? AND deleted_at IS NULL`, c.Email, tid).Scan(&total)
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,COALESCE(medical_info,''),COALESCE(allergies,''),COALESCE(family_id,''),COALESCE(referred_by_family_id,''),COALESCE(package_amount,0),COALESCE(package_self_study_hours,4),COALESCE(subscription_status,'active'),paused_at,resumed_at FROM students WHERE contact=? AND tenant_id=? AND deleted_at IS NULL ORDER BY registered_on LIMIT ? OFFSET ?`, c.Email, tid, p.Limit, p.Offset)
	} else {
		tw, twArgs := scopeTenant(c, "")
		db.QueryRow(`SELECT COUNT(*) FROM students WHERE deleted_at IS NULL`+tw, twArgs...).Scan(&total)
		pageArgs := append(append([]any{}, twArgs...), p.Limit, p.Offset)
		rows, err = db.Query(`SELECT id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,COALESCE(medical_info,''),COALESCE(allergies,''),COALESCE(family_id,''),COALESCE(referred_by_family_id,''),COALESCE(package_amount,0),COALESCE(package_self_study_hours,4),COALESCE(subscription_status,'active'),paused_at,resumed_at FROM students WHERE deleted_at IS NULL`+tw+` ORDER BY registered_on LIMIT ? OFFSET ?`, pageArgs...)
	}
	if err != nil {
		return []Student{}, total
	}
	defer rows.Close()
	out := []Student{}
	for rows.Next() {
		var s Student
		var ec, sib string
		var e2name, e2phone, pausedAt, resumedAt sql.NullString
		if err := rows.Scan(&s.ID, &s.FirstName, &s.LastName, &s.DOB, &s.Gender, &s.ParentName, &s.Contact, &s.Phone, &s.Branch, &s.Status, &s.RegisteredOn, &ec, &sib, &s.Notes, &e2name, &e2phone, &s.MedicalInfo, &s.Allergies, &s.FamilyID, &s.ReferredByFamilyID, &s.PackageAmount, &s.PackageSelfStudyHours, &s.SubscriptionStatus, &pausedAt, &resumedAt); err != nil {
			continue
		}
		s.EnrolledClasses = parseArr(ec)
		s.Siblings = parseArr(sib)
		s.Emergency2Name = nullStr(e2name)
		s.Emergency2Phone = nullStr(e2phone)
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
			// Normalise contact to lowercase/trimmed so it matches the
			// parent's login email exactly. Without this, an admin who
			// types "John@Email.com" would silently hide the kid from a
			// parent who logs in as "john@email.com".
			s.Contact = strings.ToLower(strings.TrimSpace(s.Contact))
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
				famTw, famTwArgs := scopeTenant(c, "")
				famArgs := append([]any{s.Contact}, famTwArgs...)
				db.QueryRow(`SELECT id FROM families WHERE contact=?`+famTw+` AND deleted_at IS NULL`, famArgs...).Scan(&famID)
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
			if s.SubscriptionStatus == "" {
				s.SubscriptionStatus = "active"
			}
			if s.PackageSelfStudyHours == 0 {
				s.PackageSelfStudyHours = 4
			}
			_, err := db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes,emergency2_name,emergency2_phone,medical_info,allergies,family_id,referred_by_family_id,package_amount,package_self_study_hours,subscription_status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				s.ID, tid, s.FirstName, s.LastName, s.DOB, s.Gender, s.ParentName, s.Contact, s.Phone, s.Branch, s.Status, s.RegisteredOn, jsonArr(s.EnrolledClasses), jsonArr(s.Siblings), s.Notes, s.Emergency2Name, s.Emergency2Phone, s.MedicalInfo, s.Allergies, s.FamilyID, s.ReferredByFamilyID, s.PackageAmount, s.PackageSelfStudyHours, s.SubscriptionStatus)
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
			// Same normalisation as create — keep contact matchable against
			// the lowercased login email parents authenticate with.
			s.Contact = strings.ToLower(strings.TrimSpace(s.Contact))
			tw, twArgs := scopeTenant(c, "")
			args := append([]any{s.FirstName, s.LastName, s.DOB, s.Gender, s.ParentName, s.Contact, s.Phone, s.Branch, s.Status, jsonArr(s.EnrolledClasses), jsonArr(s.Siblings), s.Notes, s.Emergency2Name, s.Emergency2Phone, s.MedicalInfo, s.Allergies, s.FamilyID, s.PackageAmount, s.PackageSelfStudyHours, id}, twArgs...)
			if _, err := db.Exec(`UPDATE students SET first_name=?,last_name=?,dob=?,gender=?,parent_name=?,contact=?,phone=?,branch=?,status=?,enrolled_classes=?,siblings=?,notes=?,emergency2_name=?,emergency2_phone=?,medical_info=?,allergies=?,family_id=?,package_amount=?,package_self_study_hours=? WHERE id=?`+tw, args...); err != nil {
				respondError(w, "could not update student", 500)
				return
			}
			logAudit(db, c.Email, "student_updated", "student", id, s.FirstName+" "+s.LastName)
			respond(w, s)
		case http.MethodDelete:
			tw, twArgs := scopeTenant(c, "")
			args := append([]any{id}, twArgs...)
			if _, err := db.Exec(`UPDATE students SET deleted_at=NOW() WHERE id=?`+tw, args...); err != nil {
				respondError(w, "could not delete student", 500)
				return
			}
			logAudit(db, c.Email, "student_deleted", "student", id, "soft deleted")
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// handleStudentSubscription pauses, resumes, or freezes a student's monthly
// subscription. Pausing skips them in the monthly invoice cron and hides them
// from rosters; resuming restores them; freeze is a separate flag with the
// same effect that's surfaced differently in reports.
func handleStudentSubscription(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var body struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			respondError(w, "bad body", 400)
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
			respondError(w, "action must be pause, freeze, or resume", 400)
			return
		}
		tw, twArgs := scopeTenant(c, "")
		now := time.Now().UTC().Format(time.RFC3339)
		if newStatus == "active" {
			args := append([]any{newStatus, now, id}, twArgs...)
			if _, err := db.Exec(`UPDATE students SET subscription_status=?, resumed_at=? WHERE id=?`+tw, args...); err != nil {
				respondError(w, "could not update subscription", 500)
				return
			}
		} else {
			args := append([]any{newStatus, now, id}, twArgs...)
			if _, err := db.Exec(`UPDATE students SET subscription_status=?, paused_at=? WHERE id=?`+tw, args...); err != nil {
				respondError(w, "could not update subscription", 500)
				return
			}
		}
		logAudit(db, c.Email, "subscription_"+body.Action, "student", id, newStatus)
		respond(w, map[string]string{"subscriptionStatus": newStatus})
	}
}

// resolveFamilyForContact finds the family row matching the given parent
// email; if none exists it creates one with the supplied parent name and a
// fresh referral code. Returns the family id and a flag indicating whether a
// new row was created. An empty contact yields ("", false, nil).
func resolveFamilyForContact(db *DB, c *Claims, contact, parentName, phone string) (string, bool, error) {
	if contact == "" {
		return "", false, nil
	}
	tid := tenantID(c)
	famTw, famTwArgs := scopeTenant(c, "")
	var famID string
	args := append([]any{contact}, famTwArgs...)
	db.QueryRow(`SELECT id FROM families WHERE contact=?`+famTw+` AND deleted_at IS NULL`, args...).Scan(&famID)
	if famID != "" {
		return famID, false, nil
	}
	famID = generateID("FAM")
	familyName := parentName + " Family"
	if parentName == "" {
		familyName = contact
	}
	if _, err := db.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name,referral_code) VALUES(?,?,?,?,?,?,?)`,
		famID, tid, familyName, contact, phone, parentName, newReferralCode()); err != nil {
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
	StudentID    string `json:"studentId"`
	Contact      string `json:"contact"`
	ParentName   string `json:"parentName"`
	FamilyID     string `json:"familyId"`
	FamilyName   string `json:"familyName"`
	IsNewFamily  bool   `json:"isNewFamily"`
}

func handleStudentRelink(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		var req relinkRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		req.Contact = strings.ToLower(strings.TrimSpace(req.Contact))
		if req.Contact != "" && !validateEmail(req.Contact) {
			respondError(w, "invalid email", 400)
			return
		}

		tw, twArgs := scopeTenant(c, "")
		var oldContact string
		oldArgs := append([]any{id}, twArgs...)
		if err := db.QueryRow(`SELECT COALESCE(contact,'') FROM students WHERE id=?`+tw+` AND deleted_at IS NULL`, oldArgs...).Scan(&oldContact); err != nil {
			respondError(w, "student not found", 404)
			return
		}

		famID, isNew, err := resolveFamilyForContact(db, c, req.Contact, req.ParentName, req.Phone)
		if err != nil {
			respondError(w, "could not resolve family", 500)
			return
		}

		updArgs := append([]any{req.Contact, req.ParentName, famID, id}, twArgs...)
		if _, err := db.Exec(`UPDATE students SET contact=?, parent_name=?, family_id=? WHERE id=?`+tw, updArgs...); err != nil {
			respondError(w, "could not relink student", 500)
			return
		}

		detail := oldContact + " -> " + req.Contact
		if req.Contact == "" {
			detail = oldContact + " -> (unlinked)"
		}
		logAudit(db, c.Email, "student_relinked", "student", id, detail)

		var familyName string
		if famID != "" {
			famTw, famTwArgs := scopeTenant(c, "")
			famArgs := append([]any{famID}, famTwArgs...)
			db.QueryRow(`SELECT name FROM families WHERE id=?`+famTw, famArgs...).Scan(&familyName)
		}
		respond(w, relinkResponse{
			StudentID:   id,
			Contact:     req.Contact,
			ParentName:  req.ParentName,
			FamilyID:    famID,
			FamilyName:  familyName,
			IsNewFamily: isNew,
		})
	}
}
