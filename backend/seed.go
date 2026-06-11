package main

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
)

// hashPassword is the canonical entry point for new password storage.
// Delegates to Argon2id (passwords.go) so all new accounts use the modern
// hash. Existing bcrypt hashes verify transparently and are upgraded on
// the user's next login.
func hashPassword(password string) (string, error) {
	return hashPasswordArgon2id(password)
}

// seedAdminPassword resolves the password to assign to the bootstrap admin.
// In production, SEED_ADMIN_PASSWORD must be set explicitly — we never ship a
// hardcoded credential to a live deployment. In dev (APP_ENV unset or not
// "production"), we fall back to "admin123" so local boot-up is friction-free.
// If SEED_ADMIN_PASSWORD is missing in production we generate a one-shot
// random password and print it to the log; the operator must capture it.
func seedAdminPassword() string {
	if v := os.Getenv("SEED_ADMIN_PASSWORD"); v != "" {
		return v
	}
	if appEnv() == "production" {
		b := make([]byte, 12)
		rand.Read(b)
		generated := "sh-" + hex.EncodeToString(b)
		log.Printf("WARNING: SEED_ADMIN_PASSWORD not set; generated one-time admin password: %s", generated)
		return generated
	}
	return "admin123"
}

// seedIfEmpty populates the database on first run.
// It only runs when tables are empty.
//
// Demo data (sample parents, students, classes, invoices, etc.) is only
// seeded outside production OR when SEED_DEMO_DATA=1. Production deployments
// get JUST the bootstrap admin row.
func seedIfEmpty(db *DB) {
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	if count > 0 {
		return // already seeded
	}

	log.Println("First run — seeding database...")

	// ── Users ──────────────────────────────────────────────────────────────────
	adminHash, _ := hashPassword(seedAdminPassword())
	db.Exec(`INSERT INTO users(email,password_hash,role,name,status) VALUES(?,?,?,?,?) ON CONFLICT(email) DO NOTHING`,
		"admin@studyhub.com", adminHash, "admin", "Admin", "active")

	seedDemoData := appEnv() != "production" || os.Getenv("SEED_DEMO_DATA") == "1"
	if !seedDemoData {
		log.Println("Seed complete (admin only — set SEED_DEMO_DATA=1 to include sample data).")
		return
	}

	parentHash, _ := hashPassword("parent123")
	teacherHash, _ := hashPassword("Teacher123!")

	users := []struct{ email, hash, role, name string }{
		{"chiying@studyhub.com", teacherHash, "teacher", "Teacher Chiying"},
		{"nadine@studyhub.com", teacherHash, "teacher", "Teacher Nadine"},
		{"rose@studyhub.com", teacherHash, "teacher", "Teacher Rose"},
		{"seeduser27@example.com", parentHash, "parent", "Sawauchi Family"},
		{"seeduser10@example.com", parentHash, "parent", "Kim Duri"},
		{"seeduser13@example.com", parentHash, "parent", "In Young Gu"},
		{"seeduser2@example.com", parentHash, "parent", "Lee Family"},
		{"seeduser32@example.com", parentHash, "parent", "Tan Wei Ming"},
		{"seeduser20@example.com", parentHash, "parent", "Maria Martinez"},
		{"seeduser7@example.com", parentHash, "parent", "Chong Family"},
		{"seeduser35@example.com", parentHash, "parent", "Wong Kai"},
		{"jihopark@korea.com", parentHash, "parent", "Park Ji Ho"},
		{"seeduser17@example.com", parentHash, "parent", "Lim Siew"},
		{"seeduser6@example.com", parentHash, "parent", "Chen Wei"},
	}
	for _, u := range users {
		db.Exec(`INSERT INTO users(email,password_hash,role,name) VALUES(?,?,?,?) ON CONFLICT(email) DO NOTHING`, u.email, u.hash, u.role, u.name)
	}

	// ── Staff ──────────────────────────────────────────────────────────────────
	staff := [][]any{
		{"s1", "Chiying", "Teacher Chiying", "Teacher", "chiying@studyhub.com", "60110000014", 3500, "2024-01-15", "Active"},
		{"s2", "Nadine", "Teacher Nadine", "Teacher", "nadine@studyhub.com", "60110000015", 3200, "2024-03-01", "Active"},
		{"s3", "Rose", "Teacher Rose", "Senior Teacher", "rose@studyhub.com", "60110000016", 3800, "2023-08-01", "Active"},
		{"s4", "Yuki", "Admin Yuki", "Admin", "yuki@studyhub.com", "60110000017", 2800, "2024-06-01", "Active"},
	}
	for _, s := range staff {
		db.Exec(`INSERT INTO staff(id,name,full_name,role,email,phone,salary,join_date,status) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, s...)
	}

	// ── Classes ────────────────────────────────────────────────────────────────
	classes := [][]any{
		{"c1", "Level 1 & 2", `["s1"]`, "Classroom 2", "Saturday", "09:30", "10:30", 6, 2, "green", "Academic"},
		{"c2", "English", `["s3"]`, "Classroom 2", "Monday", "15:00", "16:00", 6, 3, "blue", "Academic"},
		{"c3", "Level 3 & 4", `["s3","s1"]`, "Classroom 2", "Monday", "16:00", "17:00", 6, 4, "teal", "Academic"},
		{"c4", "TSH Members", `["s3"]`, "Classroom 1", "Tuesday", "15:00", "16:00", 6, 6, "orange", "Non-academic"},
		{"c5", "Level 3 & 4", `["s2"]`, "Classroom 2", "Tuesday", "15:30", "16:30", 6, 3, "teal", "Academic"},
		{"c6", "Level 5 & 6", `["s1"]`, "Classroom 1", "Wednesday", "16:00", "17:00", 6, 2, "purple", "Academic"},
		{"c7", "Math Special", `["s2"]`, "Classroom 2", "Thursday", "16:00", "17:00", 4, 2, "blue", "Academic"},
		{"c8", "Writing Workshop", `["s3"]`, "Classroom 1", "Friday", "15:00", "16:30", 8, 5, "green", "Academic"},
	}
	for _, c := range classes {
		db.Exec(`INSERT INTO classes(id,name,teacher_ids,classroom,day,time,end_time,capacity,enrolled,color,category) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, c...)
	}

	// ── Students ───────────────────────────────────────────────────────────────
	students := [][]any{
		{"STU001", "Eita", "Sawauchi", "2013-10-20", "Male", "Sawauchi Family", "seeduser27@example.com", "60110000027", "The Study Hub", "Active", "2025-09-15", `["c3"]`, `["STU002"]`, ""},
		{"STU002", "Kaho", "Sawauchi", "2015-07-20", "Female", "Sawauchi Family", "seeduser27@example.com", "60110000027", "The Study Hub", "Active", "2025-09-15", `["c3"]`, `["STU001"]`, ""},
		{"STU003", "Minjae", "Kim", "2016-03-08", "Male", "Kim Duri", "seeduser10@example.com", "60110000031", "The Study Hub", "Active", "2025-10-08", `["c5"]`, `[]`, ""},
		{"STU004", "Stephanie", "Jin", "2016-11-11", "Female", "In Young Gu", "seeduser13@example.com", "60110000038", "The Study Hub", "Active", "2025-10-20", `["c3","c5"]`, `[]`, ""},
		{"STU005", "Janice", "Lee", "2012-12-06", "Female", "Lee Family", "seeduser2@example.com", "60110000029", "The Study Hub", "Active", "2025-11-05", `["c4"]`, `[]`, ""},
		{"STU006", "Ryan", "Tan", "2017-04-15", "Male", "Tan Wei Ming", "seeduser32@example.com", "60123456789", "The Study Hub", "Active", "2025-11-18", `["c1"]`, `[]`, ""},
		{"STU007", "Sofia", "Martinez", "2014-08-22", "Female", "Maria Martinez", "seeduser20@example.com", "60110000037", "The Study Hub", "Active", "2025-12-01", `["c5"]`, `[]`, ""},
		{"STU008", "Alex", "Chong", "2013-02-14", "Male", "Chong Family", "seeduser7@example.com", "60111234567", "The Study Hub", "Active", "2025-12-15", `["c6"]`, `[]`, ""},
		{"STU009", "Mei Lin", "Wong", "2017-09-30", "Female", "Wong Kai", "seeduser35@example.com", "60110000040", "The Study Hub", "Active", "2026-01-10", `["c1"]`, `[]`, ""},
		{"STU010", "James", "Park", "2013-06-18", "Male", "Park Ji Ho", "jihopark@korea.com", "60110000028", "The Study Hub", "Active", "2026-01-22", `["c4"]`, `[]`, ""},
		{"STU011", "Hannah", "Lim", "2015-03-25", "Female", "Lim Siew", "seeduser17@example.com", "60110000026", "The Study Hub", "New", "2026-02-05", `["c3"]`, `[]`, ""},
		{"STU012", "Tom", "Chen", "2012-11-08", "Male", "Chen Wei", "seeduser6@example.com", "60110000002", "The Study Hub", "New", "2026-02-20", `["c6"]`, `[]`, ""},
	}
	for _, s := range students {
		db.Exec(`INSERT INTO students(id,first_name,last_name,dob,gender,parent_name,contact,phone,branch,status,registered_on,enrolled_classes,siblings,notes) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, s...)
	}

	// ── Invoices (sample — first 10 only for brevity) ─────────────────────────
	invoices := [][]any{
		{"INV001", "STU001", "Oct 2025 Tuition", "Monthly", 150, "2025-10-31", "Paid", "2025-10-01", "2025-10-15"},
		{"INV005", "STU001", "Feb 2026 Tuition", "Monthly", 150, "2026-02-28", "Paid", "2026-02-01", "2026-02-12"},
		{"INV006", "STU001", "Mar 2026 Tuition", "Monthly", 150, "2026-03-31", "Unpaid", "2026-03-01", nil},
		{"INV007", "STU002", "Oct 2025 Tuition", "Monthly", 150, "2025-10-31", "Paid", "2025-10-01", "2025-10-15"},
		{"INV012", "STU002", "Mar 2026 Tuition", "Monthly", 150, "2026-03-31", "Unpaid", "2026-03-01", nil},
		{"INV016", "STU003", "Feb 2026 Tuition", "Monthly", 150, "2026-02-28", "Overdue", "2026-02-01", nil},
		{"INV017", "STU003", "Mar 2026 Tuition", "Monthly", 150, "2026-03-31", "Unpaid", "2026-03-01", nil},
		{"INV022", "STU004", "Mar 2026 Tuition", "Monthly", 200, "2026-03-31", "Unpaid", "2026-03-01", nil},
		{"INV026", "STU005", "Mar 2026 Tuition", "Monthly", 180, "2026-03-31", "Unpaid", "2026-03-01", nil},
		{"INV030", "STU006", "Feb 2026 Tuition", "Monthly", 150, "2026-02-28", "Overdue", "2026-02-01", nil},
	}
	for _, inv := range invoices {
		db.Exec(`INSERT INTO invoices(id,student_id,description,type,amount,due_date,status,created_on,paid_on) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, inv...)
	}

	// ── Attendance (students) ──────────────────────────────────────────────────
	studentAtt := [][]any{
		{"ATT013", "STU001", "student", "2026-03-02", "c3", "15:55", "17:05", "Present"},
		{"ATT014", "STU002", "student", "2026-03-02", "c3", "16:05", "17:00", "Late"},
		{"ATT015", "STU004", "student", "2026-03-02", "c3", "15:58", "17:02", "Present"},
		{"ATT016", "STU011", "student", "2026-03-02", "c3", nil, nil, "Absent"},
		{"ATT017", "STU005", "student", "2026-03-03", "c4", "14:55", "16:05", "Present"},
		{"ATT018", "STU010", "student", "2026-03-03", "c4", "15:00", "16:00", "Present"},
		{"ATT019", "STU001", "student", "2026-02-23", "c3", "16:00", "17:00", "Present"},
		{"ATT020", "STU002", "student", "2026-02-23", "c3", "16:00", "17:00", "Present"},
		{"ATT021", "STU003", "student", "2026-02-25", "c5", "15:30", "16:30", "Present"},
		{"ATT022", "STU004", "student", "2026-02-23", "c3", nil, nil, "Absent"},
		{"ATT023", "STU001", "student", "2026-02-16", "c3", "16:00", "17:00", "Present"},
		{"ATT024", "STU002", "student", "2026-02-16", "c3", "16:10", "17:00", "Late"},
		{"ATT025", "STU006", "student", "2026-03-01", "c1", "09:30", "10:30", "Present"},
		{"ATT026", "STU009", "student", "2026-03-01", "c1", "09:32", "10:28", "Present"},
		{"ATT027", "STU007", "student", "2026-02-25", "c5", "15:35", "16:30", "Present"},
		{"ATT028", "STU008", "student", "2026-02-26", "c6", "16:00", "17:00", "Present"},
		{"ATT029", "STU012", "student", "2026-02-26", "c6", nil, nil, "Absent"},
	}
	for _, a := range studentAtt {
		db.Exec(`INSERT INTO attendance(id,person_id,person_type,date,class_id,check_in,check_out,status) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, a...)
	}

	// ── Announcements ──────────────────────────────────────────────────────────
	anns := [][]any{
		{"ANN001", "March Holiday Schedule", "Classes will be suspended from March 15–17 for public holidays.", "All Parents", "Notice", "2026-03-05", "Admin Yuki"},
		{"ANN002", "March Fee Payment Reminder", "Monthly tuition for March 2026 is now due. Please pay by March 31.", "All Parents", "Reminder", "2026-03-01", "Admin Yuki"},
		{"ANN004", "Attendance Policy Update", "Parents must notify us at least 2 hours before class if child cannot attend.", "All Parents", "Urgent", "2026-02-15", "Admin Yuki"},
	}
	for _, a := range anns {
		db.Exec(`INSERT INTO announcements(id,title,message,audience,type,created_on,created_by) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, a...)
	}

	// ── Payroll ────────────────────────────────────────────────────────────────
	payroll := [][]any{
		{"PAY001", "s1", "2026-02", 3500, 0, 0, 3500, "Paid", "2026-02-28"},
		{"PAY002", "s2", "2026-02", 3200, 0, 0, 3200, "Paid", "2026-02-28"},
		{"PAY003", "s3", "2026-02", 3800, 300, 0, 4100, "Paid", "2026-02-28"},
		{"PAY004", "s4", "2026-02", 2800, 0, 0, 2800, "Paid", "2026-02-28"},
		{"PAY009", "s1", "2026-03", 3500, 0, 0, 3500, "Pending", nil},
		{"PAY010", "s2", "2026-03", 3200, 0, 0, 3200, "Pending", nil},
		{"PAY011", "s3", "2026-03", 3800, 0, 0, 3800, "Pending", nil},
		{"PAY012", "s4", "2026-03", 2800, 0, 0, 2800, "Pending", nil},
	}
	for _, p := range payroll {
		db.Exec(`INSERT INTO payroll(id,staff_id,month,base_salary,bonus,deductions,total,status,paid_on) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, p...)
	}

	// ── Feedback ──────────────────────────────────────────────────────────────
	feedbacks := [][]any{
		{"FB001", "c3", "s3", "2026-03-10", "Particle は vs が", "Great", "Students showed strong understanding of topic particles. Good class engagement overall.", `[{"studentId":"STU001","note":"Eita answered confidently — great progress."},{"studentId":"STU004","note":"Stephanie needs more practice with が usage."}]`},
		{"FB002", "c1", "s1", "2026-03-08", "Hiragana Row 3 (さしすせそ)", "Good", "Covered sa-row hiragana. Most students keeping up well.", `[{"studentId":"STU006","note":"Ryan writes neatly, good retention."},{"studentId":"STU009","note":"Mei Lin a bit distracted today, gentle reminder helped."}]`},
		{"FB003", "c5", "s2", "2026-03-11", "Te-form verbs review", "Good", "Revised te-form conjugation. Class was attentive but some struggled with irregular verbs.", `[{"studentId":"STU003","note":"Minjae did well on regular verbs, needs help with irregulars."},{"studentId":"STU007","note":"Sofia very strong — helped classmates."}]`},
		{"FB004", "c6", "s1", "2026-03-12", "Kanji compound words", "Needs Work", "Difficult session — compound kanji readings are challenging. Will revisit next week with more practice sheets.", `[{"studentId":"STU008","note":"Alex got frustrated, encouraged him to keep trying."},{"studentId":"STU012","note":"Tom absent today."}]`},
		{"FB005", "c4", "s3", "2026-03-11", "Cultural games day", "Great", "Fun session with Japanese card games (karuta). Students loved it, great energy.", `[]`},
	}
	for _, f := range feedbacks {
		db.Exec(`INSERT INTO feedback(id,class_id,teacher_id,date,topic,mood,notes,student_notes) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, f...)
	}

	// Subjects removed — pricing is now the type×level matrix (pricing_tiers),
	// seeded by migration 0016.

	// ── Workshops ─────────────────────────────────────────────────────────────
	workshops := [][]any{
		{"ws1", "Hiragana & Katakana Bootcamp", "Intensive one-day workshop to master both Japanese syllabaries.", "2026-03-22", "09:00", "13:00", "Classroom 1", 12, 8, 80.0, `["s1"]`, "upcoming"},
		{"ws2", "JLPT N5 Mock Exam", "Full mock examination under timed conditions with review session.", "2026-04-05", "10:00", "14:00", "Classroom 2", 15, 5, 50.0, `["s3"]`, "upcoming"},
		{"ws3", "Kanji Writing Workshop", "Hands-on practice of the first 80 kanji with brush-pen exercises.", "2026-02-08", "14:00", "16:00", "Classroom 1", 10, 10, 60.0, `["s2"]`, "completed"},
	}
	for _, w := range workshops {
		db.Exec(`INSERT INTO workshops(id,name,description,date,time,end_time,classroom,capacity,enrolled,fee,teacher_ids,status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, w...)
	}

	log.Println("Seed complete.")
}
