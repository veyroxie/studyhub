package main

import (
	"database/sql"
	"log"

	_ "modernc.org/sqlite"
)

func initDB(path string) *sql.DB {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1) // SQLite only supports one writer at a time
	if err := createSchema(db); err != nil {
		log.Fatalf("create schema: %v", err)
	}
	runMigrations(db)
	return db
}

func createSchema(db *sql.DB) error {
	_, err := db.Exec(`
	PRAGMA journal_mode=WAL;
	PRAGMA foreign_keys=ON;

	-- Tenants: each tutoring center is a tenant (multi-SaaS foundation)
	CREATE TABLE IF NOT EXISTS tenants (
		id                  INTEGER PRIMARY KEY AUTOINCREMENT,
		name                TEXT NOT NULL,
		subscription_status TEXT DEFAULT 'active', -- 'active' | 'trial' | 'suspended'
		plan                TEXT DEFAULT 'basic',
		created_at          DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Legacy centers table (kept for backwards compatibility)
	CREATE TABLE IF NOT EXISTS centers (
		id   INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS users (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id     INTEGER NOT NULL DEFAULT 1,
		email         TEXT    UNIQUE NOT NULL,
		password_hash TEXT    NOT NULL,
		role          TEXT    NOT NULL DEFAULT 'parent', -- 'superadmin' | 'admin' | 'teacher' | 'parent'
		name          TEXT,
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
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
		enrolled_classes TEXT DEFAULT '[]', -- JSON array of class IDs
		siblings         TEXT DEFAULT '[]', -- JSON array of student IDs
		notes            TEXT DEFAULT '',
		deleted_at       TEXT -- soft delete: NULL = active
	);

	CREATE TABLE IF NOT EXISTS classes (
		id          TEXT PRIMARY KEY,
		tenant_id   INTEGER NOT NULL DEFAULT 1,
		name        TEXT NOT NULL,
		teacher_ids TEXT DEFAULT '[]', -- JSON array of staff IDs
		classroom   TEXT,
		day         TEXT,
		time        TEXT,
		end_time    TEXT,
		capacity    INTEGER DEFAULT 6,
		enrolled    INTEGER DEFAULT 0,
		color       TEXT DEFAULT 'blue',
	category    TEXT DEFAULT 'Academic'
	);

	CREATE TABLE IF NOT EXISTS staff (
		id         TEXT PRIMARY KEY,
		tenant_id  INTEGER NOT NULL DEFAULT 1,
		name       TEXT NOT NULL,
		full_name  TEXT NOT NULL,
		role       TEXT,
		email      TEXT,
		phone      TEXT,
		salary     REAL DEFAULT 0,
		join_date  TEXT,
		status     TEXT DEFAULT 'Active'
	);

	CREATE TABLE IF NOT EXISTS invoices (
		id          TEXT PRIMARY KEY,
		tenant_id   INTEGER NOT NULL DEFAULT 1,
		student_id  TEXT NOT NULL,
		description TEXT,
		type        TEXT DEFAULT 'Monthly',
		amount      REAL NOT NULL,
		due_date    TEXT,
		status      TEXT DEFAULT 'Unpaid',
		created_on  TEXT,
		paid_on     TEXT,
		deleted_at  TEXT -- soft delete: NULL = active
	);

	CREATE TABLE IF NOT EXISTS audit_logs (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id   INTEGER NOT NULL DEFAULT 1,
		actor_email TEXT NOT NULL,
		action      TEXT NOT NULL, -- 'invoice_paid' | 'student_deleted' | 'invoice_deleted' | etc.
		entity_type TEXT NOT NULL, -- 'invoice' | 'student'
		entity_id   TEXT NOT NULL,
		detail      TEXT,          -- JSON or free-text description
		created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS announcements (
		id          TEXT PRIMARY KEY,
		tenant_id   INTEGER NOT NULL DEFAULT 1,
		title       TEXT NOT NULL,
		message     TEXT NOT NULL,
		audience    TEXT DEFAULT 'All Parents',
		type        TEXT DEFAULT 'Notice',
		created_on  TEXT,
		created_by  TEXT
	);

	CREATE TABLE IF NOT EXISTS attendance (
		id          TEXT PRIMARY KEY,
		tenant_id   INTEGER NOT NULL DEFAULT 1,
		person_id   TEXT NOT NULL,
		person_type TEXT NOT NULL, -- 'staff' | 'student'
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
		base_salary REAL DEFAULT 0,
		bonus       REAL DEFAULT 0,
		deductions  REAL DEFAULT 0,
		total       REAL DEFAULT 0,
		status      TEXT DEFAULT 'Pending',
		paid_on     TEXT
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
		school_fees          REAL DEFAULT 0,
		registration_date    TEXT,
		workshop_interest    TEXT,
		class_interest       TEXT,
		notes                TEXT,
		submitted_on         TEXT,
		status               TEXT DEFAULT 'pending'
	);
	`)
	return err
}

// runMigrations applies best-effort schema updates for existing databases.
// SQLite ignores errors on duplicate column additions.
func runMigrations(db *sql.DB) {
	migrations := []string{
		// Rename center_id → tenant_id for existing tables (add new column, keep old as fallback)
		`ALTER TABLE users ADD COLUMN tenant_id INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE students ADD COLUMN tenant_id INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE classes ADD COLUMN tenant_id INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE staff ADD COLUMN tenant_id INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE invoices ADD COLUMN tenant_id INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE announcements ADD COLUMN tenant_id INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE attendance ADD COLUMN tenant_id INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE payroll ADD COLUMN tenant_id INTEGER NOT NULL DEFAULT 1`,
		// Seed default tenant if missing
		`INSERT OR IGNORE INTO tenants(id,name,subscription_status,plan) VALUES(1,'The Study Hub','active','basic')`,
		// Soft deletes
		`ALTER TABLE students ADD COLUMN deleted_at TEXT`,
		`ALTER TABLE invoices ADD COLUMN deleted_at TEXT`,
		// Audit log
		`CREATE TABLE IF NOT EXISTS audit_logs (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL DEFAULT 1, actor_email TEXT NOT NULL, action TEXT NOT NULL, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL, detail TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
		// Category field for classes
		`ALTER TABLE classes ADD COLUMN category TEXT DEFAULT 'Academic'`,
		// New registration fields
		`ALTER TABLE registrations ADD COLUMN gender TEXT`,
		`ALTER TABLE registrations ADD COLUMN school_name TEXT`,
		`ALTER TABLE registrations ADD COLUMN year_grade TEXT`,
		`ALTER TABLE registrations ADD COLUMN class_type_interest TEXT`,
		`ALTER TABLE registrations ADD COLUMN subject_interest TEXT`,
		`ALTER TABLE registrations ADD COLUMN school_fees REAL DEFAULT 0`,
		`ALTER TABLE registrations ADD COLUMN registration_date TEXT`,
		`ALTER TABLE registrations ADD COLUMN workshop_interest TEXT`,
	}
	for _, m := range migrations {
		db.Exec(m) // intentionally ignore errors (column already exists = OK)
	}
}
