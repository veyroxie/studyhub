package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// skooly student record extracted from Skooly screenshots (Apr 2026).
type skoolyStudent struct {
	Name         string `json:"name"`
	DOB          string `json:"dob"`
	RegisteredOn string `json:"registeredOn"`
	Status       string `json:"status"`
	ParentName   string `json:"parentName"`
	ParentEmail  string `json:"parentEmail"`
	ParentPhone  string `json:"parentPhone"`
	ParentRole   string `json:"parentRole"`
	Siblings     string `json:"siblings"`
	Batches      string `json:"batches"` // comma-separated batch names
}

// handleImport is a one-shot admin endpoint that imports the Skooly data
// into StudyHub. Idempotent — skips students/families/users that already exist.
//
// POST /api/admin/import
func handleImport(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		l := logFromReq(r)

		students := skoolyData()

		// Optional: accept JSON body to override/extend the hardcoded data
		var bodyStudents []skoolyStudent
		if err := json.NewDecoder(r.Body).Decode(&bodyStudents); err == nil && len(bodyStudents) > 0 {
			students = bodyStudents
		}

		tid := tenantID(c)

		// ── Pass 1: deduplicate parents by email, create families + users ──
		type parentInfo struct {
			email    string
			name     string
			phone    string
			familyID string
			userID   int64
		}
		parents := map[string]*parentInfo{} // keyed by lowercase email

		for _, s := range students {
			email := strings.ToLower(strings.TrimSpace(s.ParentEmail))
			if email == "" {
				continue
			}
			if _, exists := parents[email]; !exists {
				parents[email] = &parentInfo{
					email: email,
					name:  s.ParentName,
					phone: s.ParentPhone,
				}
			}
		}

		tw, twArgs := scopeTenant(c, "")

		familiesCreated := 0
		usersCreated := 0
		for _, p := range parents {
			// users.email is globally unique — lookup needs no tenant scope.
			var existingID int64
			if err := db.QueryRow(`SELECT id FROM users WHERE email=?`, p.email).Scan(&existingID); err == nil {
				p.userID = existingID
				// Family lookup must be tenant-scoped — contact email is
				// not unique across tenants.
				var fid string
				famArgs := append([]any{p.email}, twArgs...)
				db.QueryRow(`SELECT id FROM families WHERE contact=? AND deleted_at IS NULL`+tw, famArgs...).Scan(&fid)
				p.familyID = fid
				continue
			}

			// Create user with random password
			randomPw := make([]byte, 12)
			rand.Read(randomPw)
			pwStr := hex.EncodeToString(randomPw)
			hash, err := bcrypt.GenerateFromPassword([]byte(pwStr), bcrypt.DefaultCost)
			if err != nil {
				l.Error("import: bcrypt failed", "err", err, "email", p.email)
				continue
			}

			err = db.QueryRow(
				`INSERT INTO users(tenant_id,email,password_hash,role,name,status) VALUES(?,?,?,?,?,?) RETURNING id`,
				tid, p.email, string(hash), "parent", p.name, "active",
			).Scan(&p.userID)
			if err != nil {
				l.Error("import: user create failed", "err", err, "email", p.email)
				continue
			}
			usersCreated++

			// Create family
			famID := generateID("FAM")
			familyName := p.name + " Family"
			if p.name == "" {
				familyName = p.email
			}
			if _, err := db.Exec(
				`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name,referral_code) VALUES(?,?,?,?,?,?,?)`,
				famID, tid, familyName, p.email, p.phone, p.name, newReferralCode(),
			); err != nil {
				l.Error("import: family create failed", "err", err, "email", p.email)
				continue
			}
			p.familyID = famID
			familiesCreated++
		}

		// ── Pass 2: create students ──────────────────────────────────────────
		studentsCreated := 0
		studentsSkipped := 0

		for _, s := range students {
			// Split name into first + last
			parts := strings.Fields(s.Name)
			firstName := parts[0]
			lastName := ""
			if len(parts) > 1 {
				lastName = strings.Join(parts[1:], " ")
			}

			// Check for existing student by case-insensitive normalised name
			// match — scoped to caller's tenant. Without lower() / TRIM the
			// re-import of a row with stray whitespace silently duplicated
			// the student.
			var existingCount int
			existsArgs := append([]any{firstName, lastName}, twArgs...)
			db.QueryRow(`SELECT COUNT(*) FROM students WHERE lower(trim(first_name))=lower(trim(?)) AND lower(trim(last_name))=lower(trim(?)) AND deleted_at IS NULL`+tw, existsArgs...).Scan(&existingCount)
			if existingCount > 0 {
				studentsSkipped++
				continue
			}

			email := strings.ToLower(strings.TrimSpace(s.ParentEmail))
			famID := ""
			contact := ""
			parentName := s.ParentName
			phone := s.ParentPhone
			if p, ok := parents[email]; ok {
				famID = p.familyID
				contact = p.email
			}

			stuID := generateID("STU")

			// Parse DOB: "18 Jan 2016" → "2016-01-18"
			dob := parseSkolyDate(s.DOB)
			regDate := parseSkolyDate(s.RegisteredOn)
			if regDate == "" {
				regDate = today()
			}

			status := s.Status
			if status == "" {
				status = "Active"
			}

			// Parse batches into enrolled class IDs (matched later)
			enrolledClasses := "[]"

			if _, err := db.Exec(
				`INSERT INTO students(id,tenant_id,first_name,last_name,dob,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,family_id,gender,notes) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				stuID, tid, firstName, lastName, dob, parentName, contact, phone,
				"The Study Hub", status, regDate, enrolledClasses, famID, "", "",
			); err != nil {
				l.Error("import: student create failed", "err", err, "name", s.Name)
				continue
			}
			studentsCreated++
		}

		// ── Pass 3: create Skooly batches as classes ─────────────────────────
		batchNames := []string{"Level 1 & 2", "Level 3 & 4", "Level 5 & 6", "Mandarin", "TSH Members"}
		classesCreated := 0

		// Map batch → student names for enrollment
		batchStudents := map[string][]string{
			"Level 1 & 2": {"Aria Threw Xin Yu", "Blake Liu", "Carina Poh", "Chase James Gan", "Gareth Lee", "Joy Kim", "Luther James Gan", "Rui Xiang", "Taiho Nishioka", "Utaha"},
			"Level 3 & 4": {"Ari Singh Gill", "Blake Liu", "Elijah Shi", "Geneva Ruytinx", "Jiho Choi", "Kota Inoue", "Lee Ya Shan", "Lewis Liew", "Lucas Lin Yumo", "Luda", "Minjae Kim", "Minsung Kim", "Riku Abe", "Stella Kim", "Stephanie Jin", "Toryu Nishioka", "Valerie Liu", "Zhang Zhan He"},
			"Level 5 & 6": {"Carolina Cho", "Dylan Cho", "Geneva Ruytinx"},
			"Mandarin":    {"Chase James Gan", "Luther James Gan"},
			"TSH Members": {"Ari Singh Gill", "Aria Threw Xin Yu", "Blake Liu", "Carina Poh", "Carolina Cho", "Chase James Gan", "Dylan Cho", "Elijah Shi", "Gareth Lee", "Geneva Ruytinx", "Jiho Choi", "Joy Kim", "Kota Inoue", "Lee Ya Shan", "Lewis Liew", "Lucas Lin Yumo", "Luda", "Luther James Gan", "Minjae Kim", "Minsung Kim", "Riku Abe", "Rui Xiang", "Stella Kim", "Stephanie Jin", "Taiho Nishioka", "Toryu Nishioka", "Utaha", "Valerie Liu", "Zhang Zhan He"},
		}

		classIDs := map[string]string{} // batch name → class ID
		for _, batchName := range batchNames {
			// Tenant-scoped existence check.
			var existingID string
			clsArgs := append([]any{batchName}, twArgs...)
			if err := db.QueryRow(`SELECT id FROM classes WHERE name=? AND deleted_at IS NULL`+tw, clsArgs...).Scan(&existingID); err == nil {
				classIDs[batchName] = existingID
				continue
			}

			clsID := generateID("cls")
			if _, err := db.Exec(
				`INSERT INTO classes(id,tenant_id,name,teacher_ids,classroom,day,time,end_time,capacity,enrolled,color,category) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
				clsID, tid, batchName, "[]", "", "", "", "", 30, len(batchStudents[batchName]), "#3b82f6", "Academic",
			); err != nil {
				l.Error("import: class create failed", "err", err, "name", batchName)
				continue
			}
			classIDs[batchName] = clsID
			classesCreated++
		}

		// ── Pass 4: enroll students in classes ───────────────────────────────
		enrollments := 0
		for batchName, stuNames := range batchStudents {
			clsID, ok := classIDs[batchName]
			if !ok {
				continue
			}
			for _, stuName := range stuNames {
				parts := strings.Fields(stuName)
				firstName := parts[0]
				lastName := ""
				if len(parts) > 1 {
					lastName = strings.Join(parts[1:], " ")
				}

				var currentClasses string
				var stuID string
				lookupArgs := append([]any{firstName, lastName}, twArgs...)
				if err := db.QueryRow(
					`SELECT id, COALESCE(enrolled_classes,'[]') FROM students WHERE first_name=? AND last_name=? AND deleted_at IS NULL`+tw+` LIMIT 1`,
					lookupArgs...,
				).Scan(&stuID, &currentClasses); err != nil {
					continue
				}

				// Parse existing enrolled classes
				var classList []string
				json.Unmarshal([]byte(currentClasses), &classList)

				// Add class if not already enrolled
				found := false
				for _, c := range classList {
					if c == clsID {
						found = true
						break
					}
				}
				if !found {
					classList = append(classList, clsID)
					classJSON, _ := json.Marshal(classList)
					updArgs := append([]any{string(classJSON), stuID}, twArgs...)
					db.Exec(`UPDATE students SET enrolled_classes=? WHERE id=?`+tw, updArgs...)
					enrollments++
				}
			}
		}

		// ── Pass 5: send set-password emails to new parents ──────────────────
		emailsSent := 0
		emailsFailed := 0
		for _, p := range parents {
			if p.userID == 0 {
				continue
			}
			// Only send to newly created users (check if they already have a
			// verified status — if so, skip)
			var status string
			db.QueryRow(`SELECT COALESCE(status,'active') FROM users WHERE id=?`, p.userID).Scan(&status)
			// Only send set-password if we just created them
			// (we set status='active' on import, so check if they have email_verified_at)
			var verifiedAt interface{}
			db.QueryRow(`SELECT email_verified_at FROM users WHERE id=?`, p.userID).Scan(&verifiedAt)
			if verifiedAt != nil {
				continue // already set up
			}

			token, err := createEmailToken(db, p.email, tokenPurposeSetPassword, &p.userID, nil, verifyTokenTTL)
			if err != nil {
				l.Error("import: token create failed", "err", err, "email", p.email)
				emailsFailed++
				continue
			}
			setURL := appURL() + "/set-password.html?token=" + token
			if err := mailer.Send(p.email, "Welcome to The Study Hub — set your password", renderParentWelcomeEmail(p.name, setURL)); err != nil {
				l.Error("import: email failed", "err", err, "email", p.email)
				emailsFailed++
			} else {
				emailsSent++
			}
		}

		// Recount enrolled per class — pass-2 student creation didn't touch
		// classes.enrolled, and pass-4 mutated students.enrolled_classes
		// without bumping the counter. Without this the count drifts every
		// re-import.
		recomputedClassIDs := make([]string, 0, len(classIDs))
		for _, cid := range classIDs {
			recomputedClassIDs = append(recomputedClassIDs, cid)
		}
		recomputeClassEnrollment(db, c, recomputedClassIDs)

		logAudit(db, c.Email, "skooly_import", "system", "import", fmt.Sprintf("students=%d families=%d users=%d classes=%d emails=%d", studentsCreated, familiesCreated, usersCreated, classesCreated, emailsSent))

		respond(w, map[string]any{
			"studentsCreated":  studentsCreated,
			"studentsSkipped":  studentsSkipped,
			"familiesCreated":  familiesCreated,
			"usersCreated":     usersCreated,
			"classesCreated":   classesCreated,
			"enrollments":      enrollments,
			"emailsSent":       emailsSent,
			"emailsFailed":     emailsFailed,
		})
	}
}

// parseSkolyDate converts "18 Jan 2016" → "2016-01-18"
func parseSkolyDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	months := map[string]string{
		"Jan": "01", "Feb": "02", "Mar": "03", "Apr": "04",
		"May": "05", "Jun": "06", "Jul": "07", "Aug": "08",
		"Sep": "09", "Oct": "10", "Nov": "11", "Dec": "12",
	}
	parts := strings.Fields(s)
	if len(parts) != 3 {
		return s // return as-is if unparseable
	}
	day := parts[0]
	if len(day) == 1 {
		day = "0" + day
	}
	month, ok := months[parts[1]]
	if !ok {
		return s
	}
	year := parts[2]
	return year + "-" + month + "-" + day
}

// handleClearSeedData removes the seed/test data that was inserted by seedIfEmpty().
// Only deletes records with the known seed IDs (STU001-STU012, c1-c8, INV*, etc.)
// so real imported data is untouched.
//
// POST /api/admin/clear-seed
func handleClearSeedData(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		l := logFromReq(r)

		seedStudentIDs := []string{"STU001", "STU002", "STU003", "STU004", "STU005", "STU006", "STU007", "STU008", "STU009", "STU010", "STU011", "STU012"}
		seedClassIDs := []string{"c1", "c2", "c3", "c4", "c5", "c6", "c7", "c8"}
		seedInvoiceIDs := []string{"INV001", "INV005", "INV006", "INV007", "INV012", "INV016", "INV017", "INV022", "INV026", "INV030"}
		seedAttIDs := []string{"ATT013", "ATT014", "ATT015", "ATT016", "ATT017", "ATT018", "ATT019", "ATT020", "ATT021", "ATT022", "ATT023", "ATT024", "ATT025", "ATT026", "ATT027", "ATT028", "ATT029"}
		seedFeedbackIDs := []string{"FB001", "FB002", "FB003", "FB004", "FB005"}
		seedAnnIDs := []string{"ANN001", "ANN002", "ANN004"}
		seedPayrollIDs := []string{"PAY001", "PAY002", "PAY003", "PAY004", "PAY009", "PAY010", "PAY011", "PAY012"}
		seedSubjectIDs := []string{"sub1", "sub2", "sub3", "sub4", "sub5"}
		seedWorkshopIDs := []string{"ws1", "ws2", "ws3"}
		// Seed parent emails (these have test users + families)
		seedParentEmails := []string{
			"seeduser27@example.com", "seeduser10@example.com", "seeduser13@example.com",
			"seeduser2@example.com", "seeduser32@example.com", "seeduser20@example.com",
			"seeduser7@example.com", "seeduser35@example.com", "jihopark@korea.com",
			"seeduser17@example.com", "seeduser6@example.com",
		}

		deleted := map[string]int{}
		tw, twArgs := scopeTenant(c, "")

		deleteByIDs := func(table string, ids []string) {
			for _, id := range ids {
				args := append([]any{id}, twArgs...)
				res, err := db.Exec(`DELETE FROM `+table+` WHERE id=?`+tw, args...)
				if err != nil {
					l.Error("clear-seed: delete failed", "table", table, "id", id, "err", err)
					continue
				}
				if n, _ := res.RowsAffected(); n > 0 {
					deleted[table] += int(n)
				}
			}
		}

		deleteByIDs("students", seedStudentIDs)
		deleteByIDs("invoices", seedInvoiceIDs)
		deleteByIDs("attendance", seedAttIDs)
		deleteByIDs("feedback", seedFeedbackIDs)
		deleteByIDs("announcements", seedAnnIDs)
		deleteByIDs("payroll", seedPayrollIDs)
		deleteByIDs("subjects", seedSubjectIDs)
		deleteByIDs("workshops", seedWorkshopIDs)
		deleteByIDs("classes", seedClassIDs)

		// Delete seed parent users + families (but not admin/teacher users).
		// users.email is globally unique so tenant scope is irrelevant there,
		// but families.contact can collide across tenants — must be scoped.
		for _, email := range seedParentEmails {
			res, _ := db.Exec(`DELETE FROM users WHERE email=? AND role='parent'`, email)
			if n, _ := res.RowsAffected(); n > 0 {
				deleted["users"] += int(n)
			}
			famArgs := append([]any{email}, twArgs...)
			res, _ = db.Exec(`DELETE FROM families WHERE contact=?`+tw, famArgs...)
			if n, _ := res.RowsAffected(); n > 0 {
				deleted["families"] += int(n)
			}
		}

		logAudit(db, c.Email, "seed_data_cleared", "system", "clear-seed", fmt.Sprintf("%v", deleted))

		respond(w, map[string]any{
			"message": "Seed data cleared",
			"deleted": deleted,
		})
	}
}

// skoolyData returns the hardcoded student data extracted from Skooly screenshots.
func skoolyData() []skoolyStudent {
	return []skoolyStudent{
		// ── Active students ──────────────────────────────────────────────────
		{Name: "Zhang Zhan He", DOB: "18 Jan 2016", RegisteredOn: "07 Mar 2026", Status: "Active", ParentName: "syz012345", ParentEmail: "seeduser31@example.com", ParentPhone: "60110000009", Batches: "Level 3 & 4"},
		{Name: "Luda", DOB: "08 Feb 2026", RegisteredOn: "08 Feb 2026", Status: "Active", Batches: "Level 3 & 4"},
		{Name: "Jiho Choi", DOB: "19 Nov 2016", RegisteredOn: "24 Jan 2026", Status: "Active", ParentName: "cindykhpark", ParentEmail: "seeduser8@example.com", Batches: "Level 3 & 4"},
		{Name: "Joy Kim", DOB: "08 Jun 2019", RegisteredOn: "24 Jan 2026", Status: "Active", ParentName: "gellen0707", ParentEmail: "seeduser12@example.com", Siblings: "Stella Kim", Batches: "TSH Members"},
		{Name: "Stella Kim", DOB: "08 May 2017", RegisteredOn: "24 Jan 2026", Status: "Active", ParentName: "gellen0707", ParentEmail: "seeduser12@example.com", ParentPhone: "60110000035", Siblings: "Joy Kim", Batches: "Level 3 & 4"},
		{Name: "Chase James Gan", DOB: "17 Feb 2020", RegisteredOn: "07 Jan 2026", Status: "Active", ParentName: "Esther Ng", ParentPhone: "60110000025", Siblings: "Luther James Gan", Batches: "Mandarin"},
		{Name: "Minsung Kim", DOB: "18 Apr 2017", RegisteredOn: "07 Jan 2026", Status: "Active", ParentName: "Doori Jo", ParentEmail: "seeduser10@example.com", ParentPhone: "60110000031", Siblings: "Minjae Kim", Batches: "Level 3 & 4"},
		{Name: "Aria Threw Xin Yu", DOB: "04 Jan 2021", RegisteredOn: "03 Nov 2025", Status: "Active", ParentName: "agnesseowyean", ParentEmail: "seeduser4@example.com", ParentPhone: "60110000030", Batches: "TSH Members"},
		{Name: "Taiho Nishioka", DOB: "16 Oct 2018", RegisteredOn: "02 Sep 2025", Status: "Active", ParentName: "Yui Nishioka", ParentEmail: "seeduser16@example.com", ParentPhone: "60110000032", Siblings: "Toryu Nishioka", Batches: "TSH Members"},
		{Name: "Ari Singh Gill", DOB: "15 Mar 2016", RegisteredOn: "25 Aug 2025", Status: "Active", ParentName: "Nupur", ParentEmail: "seeduser25@example.com", ParentPhone: "60110000023", Batches: "Level 3 & 4"},
		{Name: "Carina Poh", DOB: "22 Mar 2019", RegisteredOn: "27 Oct 2024", Status: "Active", ParentName: "Thuy Ha Minh", ParentEmail: "seeduser14@example.com", ParentPhone: "60110000024", Batches: "TSH Members"},
		{Name: "Lee Ya Shan", DOB: "18 Aug 2016", RegisteredOn: "27 Oct 2024", Status: "Active", ParentName: "mervistang", ParentEmail: "seeduser21@example.com", ParentPhone: "60110000042", Batches: "Level 3 & 4"},
		{Name: "Elijah Shi", DOB: "24 Jul 2017", RegisteredOn: "26 Oct 2024", Status: "Active", ParentName: "Connie Liang", ParentEmail: "seeduser9@example.com", ParentPhone: "60110000008", Batches: "Level 3 & 4"},
		{Name: "Lewis Liew", DOB: "22 Nov 2016", RegisteredOn: "26 Oct 2024", Status: "Active", ParentName: "Soo", ParentEmail: "seeduser29@example.com", ParentPhone: "60110000018", Batches: "Level 3 & 4"},
		{Name: "Luther James Gan", DOB: "09 May 2018", RegisteredOn: "23 Oct 2024", Status: "Active", ParentName: "Esther", ParentPhone: "60110000025", Siblings: "Chase James Gan", Batches: "Mandarin"},
		{Name: "Carolina Cho", DOB: "15 Oct 2015", RegisteredOn: "10 Sep 2024", Status: "Active", ParentName: "mira moon", ParentEmail: "seeduser22@example.com", ParentPhone: "60110000020", Siblings: "Dylan Cho", Batches: "Level 5 & 6"},
		{Name: "Dylan Cho", DOB: "22 Oct 2013", RegisteredOn: "10 Sep 2024", Status: "Active", ParentName: "mira moon", ParentEmail: "seeduser22@example.com", ParentPhone: "60110000020", Siblings: "Carolina Cho", Batches: "Level 5 & 6"},
		{Name: "Geneva Ruytinx", DOB: "08 Aug 2016", RegisteredOn: "16 Nov 2024", Status: "Active", ParentName: "Ms.Sunny", ParentEmail: "seeduser30@example.com", ParentPhone: "60110000011", Batches: "Level 3 & 4,Level 5 & 6"},
		{Name: "Blake Liu", DOB: "31 Oct 2016", RegisteredOn: "09 Nov 2024", Status: "Active", ParentName: "Alice Chen", ParentEmail: "seeduser5@example.com", ParentPhone: "60110000006", Siblings: "Valerie Liu", Batches: "Level 3 & 4"},
		{Name: "Valerie Liu", DOB: "05 Dec 2014", RegisteredOn: "09 Nov 2024", Status: "Active", ParentName: "Alice Chen", ParentEmail: "seeduser5@example.com", ParentPhone: "60110000006", Siblings: "Blake Liu", Batches: "Level 3 & 4"},
		{Name: "Lucas Lin Yumo", DOB: "22 Jan 2017", RegisteredOn: "08 Nov 2024", Status: "Active", ParentName: "Elaine", ParentEmail: "seeduser11@example.com", ParentPhone: "60110000007", Batches: "Level 3 & 4"},
		{Name: "Riku Abe", DOB: "29 Mar 2014", RegisteredOn: "26 Nov 2024", Status: "Active", ParentName: "Riku Father", ParentEmail: "seeduser24@example.com", ParentPhone: "60110000019", ParentRole: "Father", Batches: "Level 3 & 4"},
		{Name: "Minjae Kim", DOB: "18 Apr 2017", RegisteredOn: "07 Jan 2026", Status: "Active", ParentName: "Doori Jo", ParentEmail: "seeduser10@example.com", ParentPhone: "60110000031", Siblings: "Minsung Kim", Batches: "Level 3 & 4"},
		// Rui Xiang, Utaha, Kota Inoue, Stephanie Jin, Gareth Lee — from blurry screenshot
		{Name: "Rui Xiang", DOB: "22 Oct 2016", RegisteredOn: "04 Feb 2025", Status: "Active", ParentName: "Lin", ParentEmail: "seeduser18@example.com", Batches: "TSH Members"},
		{Name: "Utaha", DOB: "24 Jan 2017", RegisteredOn: "02 Feb 2025", Status: "Active", Batches: "TSH Members"},
		{Name: "Kota Inoue", DOB: "19 Jan 2017", RegisteredOn: "02 Feb 2025", Status: "Active", ParentEmail: "seeduser15@example.com", Batches: "Level 3 & 4"},
		{Name: "Stephanie Jin", DOB: "14 Feb 2017", RegisteredOn: "15 Feb 2025", Status: "Active", Batches: "Level 3 & 4"},
		{Name: "Gareth Lee", DOB: "25 Sep 2016", RegisteredOn: "13 Oct 2024", Status: "Active", Batches: "TSH Members"},
		{Name: "Toryu Nishioka", DOB: "16 Oct 2015", RegisteredOn: "02 Sep 2025", Status: "Active", ParentName: "Yui Nishioka", ParentEmail: "seeduser16@example.com", ParentPhone: "60110000032", Siblings: "Taiho Nishioka", Batches: "Level 3 & 4"},
		{Name: "Jiho Yoo", DOB: "31 Mar 2026", RegisteredOn: "31 Mar 2026", Status: "Active"},

		// ── New students ─────────────────────────────────────────────────────
		{Name: "Koki", DOB: "31 Mar 2026", RegisteredOn: "31 Mar 2026", Status: "New"},
		{Name: "Siyoon", DOB: "31 Mar 2026", RegisteredOn: "31 Mar 2026", Status: "New"},
		{Name: "Ryan", DOB: "31 Mar 2026", RegisteredOn: "31 Mar 2026", Status: "New"},
		{Name: "Jiyu", DOB: "31 Mar 2026", RegisteredOn: "31 Mar 2026", Status: "New"},
		{Name: "Lucas", DOB: "31 Mar 2026", RegisteredOn: "31 Mar 2026", Status: "New"},

		// ── Inactive students ────────────────────────────────────────────────
		{Name: "Averie", DOB: "20 Nov 2025", RegisteredOn: "20 Nov 2025", Status: "Inactive"},
		{Name: "Sydney", DOB: "20 Nov 2025", RegisteredOn: "20 Nov 2025", Status: "Inactive"},
		{Name: "Josef Langkoe Low", DOB: "31 Jan 2015", RegisteredOn: "31 Oct 2025", Status: "Inactive", ParentName: "lowsjo", ParentEmail: "seeduser19@example.com", ParentPhone: "60110000012", ParentRole: "Father"},
		{Name: "Pierce Wilson", DOB: "13 Sep 2025", RegisteredOn: "13 Sep 2025", Status: "Inactive"},
		{Name: "Eita Sawauchi", DOB: "20 Oct 2013", RegisteredOn: "22 Mar 2025", Status: "Inactive", ParentName: "Kaho Sawauchi", ParentEmail: "seeduser26@example.com", ParentPhone: "60110000027", Siblings: "Kaho Sawauchi"},
		{Name: "Kaho Sawauchi", DOB: "20 Jul 2015", RegisteredOn: "22 Mar 2025", Status: "Inactive", ParentName: "Kaho Sawauchi", ParentEmail: "seeduser26@example.com", ParentPhone: "60110000027", Siblings: "Eita Sawauchi"},
		{Name: "Cynthia", DOB: "15 Sep 2016", RegisteredOn: "09 Dec 2024", Status: "Inactive", ParentName: "Nicole Wang", ParentEmail: "seeduser34@example.com", ParentPhone: "60110000022"},
		{Name: "Ashley", DOB: "15 Nov 2012", RegisteredOn: "03 Dec 2024", Status: "Inactive", ParentName: "Ms Zhang", ParentEmail: "seeduser1@example.com", ParentPhone: "60110000039", Siblings: "Jane"},
		{Name: "Jane", DOB: "02 Oct 2016", RegisteredOn: "03 Dec 2024", Status: "Inactive", ParentName: "Zhang", ParentEmail: "seeduser1@example.com", ParentPhone: "60110000039", Siblings: "Ashley"},
		{Name: "Jayden Wong", DOB: "12 Jul 2019", RegisteredOn: "19 Nov 2024", Status: "Inactive", ParentName: "Shuehyean", ParentEmail: "seeduser28@example.com", ParentPhone: "60110000041"},
		{Name: "Adam Henry Rajudin", DOB: "15 Sep 1991", RegisteredOn: "27 Oct 2024", Status: "Inactive", ParentName: "Serena.azizuddin", ParentEmail: "seeduser3@example.com", ParentPhone: "60110000010"},
		{Name: "Chloe Choe", DOB: "23 Mar 2013", RegisteredOn: "10 Sep 2024", Status: "Inactive", ParentName: "Amy", ParentEmail: "seeduser23@example.com", ParentPhone: "60110000001"},
		{Name: "Eason Ling", DOB: "10 Sep 2024", RegisteredOn: "10 Sep 2024", Status: "Inactive", ParentName: "Karen", ParentPhone: "60110000005"},
		{Name: "Mina", DOB: "21 Dec 2013", RegisteredOn: "10 Sep 2024", Status: "Inactive", ParentName: "Wendy", ParentPhone: "60110000034", Siblings: "Serena"},
		{Name: "Serena", DOB: "13 Apr 2016", RegisteredOn: "10 Sep 2024", Status: "Inactive", ParentName: "Wendy", ParentPhone: "60110000034", Siblings: "Mina"},
		{Name: "Ouwen Hao", DOB: "23 Nov 2015", RegisteredOn: "10 Sep 2024", Status: "Inactive", ParentName: "Jessie", ParentPhone: "60110000036"},
		{Name: "Wen Hang", DOB: "31 May 2014", RegisteredOn: "10 Sep 2024", Status: "Inactive", ParentName: "Wang", ParentPhone: "60110000004"},
		{Name: "Henry", DOB: "10 Sep 2024", RegisteredOn: "10 Sep 2024", Status: "Inactive", ParentName: "Amelia", ParentPhone: "60110000033", Siblings: "Xin Bei"},
		{Name: "Xin Bei", DOB: "10 Sep 2024", RegisteredOn: "10 Sep 2024", Status: "Inactive", ParentName: "Amelia", ParentPhone: "60110000033", Siblings: "Henry"},
		{Name: "Kher Ern", DOB: "12 Dec 2011", RegisteredOn: "10 Sep 2024", Status: "Inactive", ParentName: "Kher Ern mummy", ParentPhone: "60110000021", Siblings: "Zi Heng"},
		{Name: "Zi Heng", DOB: "10 Sep 2024", RegisteredOn: "10 Sep 2024", Status: "Inactive", ParentName: "Kher Ern mummy", ParentPhone: "60110000021", Siblings: "Kher Ern"},
	}
}
