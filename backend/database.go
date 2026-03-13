package main

import (
	"database/sql"
	"log"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func initDB(dsn string) *DB {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	if err := db.Ping(); err != nil {
		log.Fatalf("ping db: %v", err)
	}
	if err := createSchema(db); err != nil {
		log.Fatalf("create schema: %v", err)
	}
	runMigrations(db)
	return &DB{db}
}

func createSchema(db *sql.DB) error {
	_, err := db.Exec(`
	-- Tenants: each tutoring center is a tenant (multi-SaaS foundation)
	CREATE TABLE IF NOT EXISTS tenants (
		id                  SERIAL PRIMARY KEY,
		name                TEXT NOT NULL,
		subscription_status TEXT DEFAULT 'active',
		plan                TEXT DEFAULT 'basic',
		created_at          TIMESTAMPTZ DEFAULT NOW()
	);

	-- Legacy centers table (kept for backwards compatibility)
	CREATE TABLE IF NOT EXISTS centers (
		id         SERIAL PRIMARY KEY,
		name       TEXT NOT NULL,
		created_at TIMESTAMPTZ DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS users (
		id            SERIAL PRIMARY KEY,
		tenant_id     INTEGER NOT NULL DEFAULT 1,
		email         TEXT    UNIQUE NOT NULL,
		password_hash TEXT    NOT NULL,
		role          TEXT    NOT NULL DEFAULT 'parent',
		name          TEXT,
		created_at    TIMESTAMPTZ DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS students (
		id               TEXT PRIMARY KEY,
		tenant_id        INTEGER NOT NULL DEFAULT 1,
		first_name       TEXT NOT NULL,
		last_name        TEXT NOT NULL,
		dob              TEXT,
		gender           TEXT,
		parent_name      TEXT,
		contact          TEXT,
		phone            TEXT,
		branch           TEXT,
		status           TEXT DEFAULT 'Active',
		registered_on    TEXT,
		enrolled_classes TEXT DEFAULT '[]',
		siblings         TEXT DEFAULT '[]',
		notes            TEXT DEFAULT '',
		deleted_at       TEXT,
		emergency2_name  TEXT,
		emergency2_phone TEXT,
		medical_info     TEXT DEFAULT '',
		allergies        TEXT DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS classes (
		id          TEXT PRIMARY KEY,
		tenant_id   INTEGER NOT NULL DEFAULT 1,
		name        TEXT NOT NULL,
		teacher_ids TEXT DEFAULT '[]',
		classroom   TEXT,
		day         TEXT,
		time        TEXT,
		end_time    TEXT,
		capacity    INTEGER DEFAULT 6,
		enrolled    INTEGER DEFAULT 0,
		color       TEXT DEFAULT 'blue',
		category    TEXT DEFAULT 'Academic',
		deleted_at  TEXT
	);

	CREATE TABLE IF NOT EXISTS staff (
		id              TEXT PRIMARY KEY,
		tenant_id       INTEGER NOT NULL DEFAULT 1,
		name            TEXT NOT NULL,
		full_name       TEXT NOT NULL,
		role            TEXT,
		email           TEXT,
		phone           TEXT,
		salary          DOUBLE PRECISION DEFAULT 0,
		join_date       TEXT,
		status          TEXT DEFAULT 'Active',
		specialization  TEXT,
		nric            TEXT,
		emergency_name  TEXT,
		emergency_phone TEXT,
		employment_type TEXT DEFAULT 'Full-time',
		hourly_rate     DOUBLE PRECISION DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS invoices (
		id            TEXT PRIMARY KEY,
		tenant_id     INTEGER NOT NULL DEFAULT 1,
		student_id    TEXT NOT NULL,
		description   TEXT,
		type          TEXT DEFAULT 'Monthly',
		amount        DOUBLE PRECISION NOT NULL,
		due_date      TEXT,
		status        TEXT DEFAULT 'Unpaid',
		created_on    TEXT,
		paid_on       TEXT,
		deleted_at    TEXT,
		payment_proof TEXT DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS audit_logs (
		id          SERIAL PRIMARY KEY,
		tenant_id   INTEGER NOT NULL DEFAULT 1,
		actor_email TEXT NOT NULL,
		action      TEXT NOT NULL,
		entity_type TEXT NOT NULL,
		entity_id   TEXT NOT NULL,
		detail      TEXT,
		created_at  TIMESTAMPTZ DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS announcements (
		id          TEXT PRIMARY KEY,
		tenant_id   INTEGER NOT NULL DEFAULT 1,
		title       TEXT NOT NULL,
		message     TEXT NOT NULL,
		audience    TEXT DEFAULT 'All Parents',
		type        TEXT DEFAULT 'Notice',
		created_on  TEXT,
		created_by  TEXT,
		status      TEXT DEFAULT 'published',
		archive_on  TEXT
	);

	CREATE TABLE IF NOT EXISTS attendance (
		id          TEXT PRIMARY KEY,
		tenant_id   INTEGER NOT NULL DEFAULT 1,
		person_id   TEXT NOT NULL,
		person_type TEXT NOT NULL,
		date        TEXT NOT NULL,
		class_id    TEXT,
		check_in    TEXT,
		check_out   TEXT,
		status      TEXT DEFAULT 'Present'
	);

	CREATE TABLE IF NOT EXISTS payroll (
		id          TEXT PRIMARY KEY,
		tenant_id   INTEGER NOT NULL DEFAULT 1,
		staff_id    TEXT NOT NULL,
		month       TEXT NOT NULL,
		base_salary DOUBLE PRECISION DEFAULT 0,
		bonus       DOUBLE PRECISION DEFAULT 0,
		deductions  DOUBLE PRECISION DEFAULT 0,
		total       DOUBLE PRECISION DEFAULT 0,
		status      TEXT DEFAULT 'Pending',
		paid_on     TEXT
	);

	CREATE TABLE IF NOT EXISTS feedback (
		id            TEXT PRIMARY KEY,
		tenant_id     INTEGER NOT NULL DEFAULT 1,
		class_id      TEXT NOT NULL,
		teacher_id    TEXT NOT NULL,
		date          TEXT NOT NULL,
		topic         TEXT,
		mood          TEXT,
		notes         TEXT,
		student_notes TEXT DEFAULT '[]',
		created_at    TIMESTAMPTZ DEFAULT NOW(),
		deleted_at    TEXT
	);

	CREATE TABLE IF NOT EXISTS subjects (
		id          TEXT PRIMARY KEY,
		tenant_id   INTEGER NOT NULL DEFAULT 1,
		name        TEXT NOT NULL,
		category    TEXT DEFAULT 'Academic',
		level       TEXT,
		description TEXT,
		monthly_fee DOUBLE PRECISION DEFAULT 0,
		color       TEXT DEFAULT 'blue'
	);

	CREATE TABLE IF NOT EXISTS workshops (
		id          TEXT PRIMARY KEY,
		tenant_id   INTEGER NOT NULL DEFAULT 1,
		name        TEXT NOT NULL,
		description TEXT,
		date        TEXT,
		time        TEXT,
		end_time    TEXT,
		classroom   TEXT,
		capacity    INTEGER DEFAULT 10,
		enrolled    INTEGER DEFAULT 0,
		fee         DOUBLE PRECISION DEFAULT 0,
		teacher_ids TEXT DEFAULT '[]',
		status      TEXT DEFAULT 'upcoming'
	);

	CREATE TABLE IF NOT EXISTS self_study_sessions (
		id           TEXT PRIMARY KEY,
		tenant_id    INTEGER NOT NULL DEFAULT 1,
		student_id   TEXT NOT NULL,
		date         TEXT NOT NULL,
		start_time   TEXT,
		end_time     TEXT,
		duration_min INTEGER DEFAULT 0,
		notes        TEXT
	);

	CREATE TABLE IF NOT EXISTS performance_reviews (
		id             TEXT PRIMARY KEY,
		tenant_id      INTEGER NOT NULL DEFAULT 1,
		staff_id       TEXT NOT NULL,
		reviewer_email TEXT,
		date           TEXT NOT NULL,
		rating         DOUBLE PRECISION DEFAULT 0,
		parent_rating  DOUBLE PRECISION DEFAULT 0,
		notes          TEXT
	);

	CREATE TABLE IF NOT EXISTS cancelled_classes (
		id           TEXT PRIMARY KEY,
		tenant_id    INTEGER NOT NULL DEFAULT 1,
		class_id     TEXT NOT NULL,
		date         TEXT NOT NULL,
		reason       TEXT,
		cancelled_by TEXT,
		created_on   TEXT
	);

	CREATE TABLE IF NOT EXISTS registrations (
		id                   TEXT PRIMARY KEY,
		tenant_id            INTEGER NOT NULL DEFAULT 1,
		parent_name          TEXT NOT NULL,
		email                TEXT NOT NULL,
		phone                TEXT,
		emergency_name       TEXT,
		emergency_phone      TEXT,
		student_first_name   TEXT NOT NULL,
		student_last_name    TEXT NOT NULL,
		student_dob          TEXT,
		student_gender       TEXT,
		gender               TEXT,
		school_name          TEXT,
		year_grade           TEXT,
		class_type_interest  TEXT,
		subject_interest     TEXT,
		school_fees          DOUBLE PRECISION DEFAULT 0,
		registration_date    TEXT,
		workshop_interest    TEXT,
		class_interest       TEXT,
		notes                TEXT,
		submitted_on         TEXT,
		status               TEXT DEFAULT 'pending',
		type                 TEXT DEFAULT 'student'
	);

	CREATE TABLE IF NOT EXISTS holidays (
		id         TEXT PRIMARY KEY,
		tenant_id  INTEGER NOT NULL DEFAULT 1,
		name       TEXT NOT NULL,
		date       TEXT NOT NULL,
		end_date   TEXT,
		type       TEXT DEFAULT 'holiday',
		notes      TEXT,
		created_by TEXT
	);
	`)
	return err
}

// runMigrations applies best-effort schema updates for existing databases.
// PostgreSQL raises errors on duplicate columns, which we safely ignore.
func runMigrations(db *sql.DB) {
	migrations := []string{
		// Seed default tenant if missing
		`INSERT INTO tenants(id,name,subscription_status,plan) VALUES(1,'The Study Hub','active','basic') ON CONFLICT(id) DO NOTHING`,

		// Indexes on frequently queried columns
		`CREATE INDEX IF NOT EXISTS idx_attendance_date ON attendance(date)`,
		`CREATE INDEX IF NOT EXISTS idx_invoices_status ON invoices(status)`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_date ON feedback(date)`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_class ON feedback(class_id)`,
		`CREATE INDEX IF NOT EXISTS idx_students_parent ON students(contact)`,
		`CREATE INDEX IF NOT EXISTS idx_holidays_date ON holidays(date)`,
	}
	for _, m := range migrations {
		db.Exec(m) // intentionally ignore errors (index/row already exists = OK)
	}
}
