package store

import (
	"database/sql"
	"fmt"
	"log"
	"studyhub/internal/core"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func InitDB(dsn string) *DB {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	// Pool tuned for snapshot fan-out (~18 concurrent queries per build)
	// × ~10 concurrent dashboard sessions before we hit the ceiling. The
	// previous 50/10 sat under that load and caused pool exhaustion when
	// admins refreshed simultaneously.
	// 40 open × work_mem=8MB fits the 768M Postgres container; the previous 150
	// exceeded Postgres's max_connections (now pinned to 60 in compose), so
	// load spikes hit "too many clients" instead of briefly queueing here.
	db.SetMaxOpenConns(40)
	db.SetMaxIdleConns(10)
	db.SetConnMaxIdleTime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		log.Fatalf("ping db: %v", err)
	}
	if err := createSchema(db); err != nil {
		log.Fatalf("create schema: %v", err)
	}
	if err := applyColumnBackfills(db); err != nil {
		log.Fatalf("column backfills: %v", err)
	}
	wrapped := &DB{db}
	if err := runFileMigrations(wrapped); err != nil {
		log.Fatalf("file migrations: %v", err)
	}
	// Data backfills run last, against the fully converged schema.
	migrateStudentsToFamilies(db)
	backfillFamilyReferralCodes(db)
	return wrapped
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

	CREATE TABLE IF NOT EXISTS families (
		id          TEXT PRIMARY KEY,
		tenant_id   INTEGER NOT NULL DEFAULT 1,
		name        TEXT NOT NULL,
		contact     TEXT NOT NULL,
		phone       TEXT DEFAULT '',
		parent_name TEXT DEFAULT '',
		address     TEXT DEFAULT '',
		notes       TEXT DEFAULT '',
		created_at  TIMESTAMPTZ DEFAULT NOW(),
		deleted_at  TEXT
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
		hourly_rate     DOUBLE PRECISION DEFAULT 0,
		performance_notes TEXT DEFAULT '',
		deleted_at      TIMESTAMPTZ
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
		paid_on               TEXT,
		deleted_at            TEXT,
		payment_proof         TEXT DEFAULT '',
		payment_method        TEXT DEFAULT '',
		discount_pct          REAL DEFAULT 0,
		submitted_by_parent   BOOLEAN DEFAULT FALSE,
		sibling_ids           TEXT DEFAULT '[]',
		sibling_discount      REAL DEFAULT 0,
		receipt_no            TEXT,
		line_items            TEXT DEFAULT '[]'
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
		paid_on     TEXT,
		manually_edited BOOLEAN DEFAULT FALSE
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
		color       TEXT DEFAULT 'blue',
		deleted_at  TEXT
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
		status      TEXT DEFAULT 'upcoming',
		deleted_at  TEXT
	);

	CREATE TABLE IF NOT EXISTS self_study_sessions (
		id           TEXT PRIMARY KEY,
		tenant_id    INTEGER NOT NULL DEFAULT 1,
		student_id   TEXT NOT NULL,
		date         TEXT NOT NULL,
		start_time   TEXT,
		end_time     TEXT,
		duration_min INTEGER DEFAULT 0,
		notes        TEXT,
		deleted_at   TEXT
	);

	CREATE TABLE IF NOT EXISTS performance_reviews (
		id             TEXT PRIMARY KEY,
		tenant_id      INTEGER NOT NULL DEFAULT 1,
		staff_id       TEXT NOT NULL,
		reviewer_email TEXT,
		date           TEXT NOT NULL,
		rating         DOUBLE PRECISION DEFAULT 0,
		parent_rating  DOUBLE PRECISION DEFAULT 0,
		notes          TEXT,
		deleted_at     TEXT
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
		student_first_name   TEXT,
		student_last_name    TEXT,
		student_dob          TEXT,
		student_gender       TEXT,
		gender               TEXT,
		school_name          TEXT,
		year_grade           TEXT,
		class_type_interest  TEXT,
		subject_interest     TEXT,
		school_fees          NUMERIC(12,2) DEFAULT 0,
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
		created_by TEXT,
		deleted_at TEXT
	);

	CREATE TABLE IF NOT EXISTS replacement_credits (
		id          TEXT PRIMARY KEY,
		tenant_id   INTEGER NOT NULL DEFAULT 1,
		student_id  TEXT NOT NULL,
		type        TEXT NOT NULL,
		minutes     INTEGER NOT NULL,
		note        TEXT DEFAULT '',
		class_id    TEXT DEFAULT '',
		date        TEXT NOT NULL,
		created_by  TEXT DEFAULT '',
		created_at  TIMESTAMPTZ DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS feedback_replies (
		id           TEXT PRIMARY KEY,
		tenant_id    INTEGER NOT NULL DEFAULT 1,
		feedback_id  TEXT NOT NULL,
		author_email TEXT NOT NULL,
		author_name  TEXT DEFAULT '',
		message      TEXT NOT NULL,
		created_at   TIMESTAMPTZ DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS referral_rewards (
		id                  TEXT PRIMARY KEY,
		tenant_id           INTEGER NOT NULL DEFAULT 1,
		referrer_family_id  TEXT NOT NULL,
		referred_student_id TEXT NOT NULL UNIQUE,
		status              TEXT NOT NULL DEFAULT 'pending',
		paid_invoice_count  INTEGER NOT NULL DEFAULT 0,
		credits_remaining   INTEGER NOT NULL DEFAULT 0,
		milestone_met_on    TEXT,
		created_at          TIMESTAMPTZ DEFAULT NOW()
	);

	-- MFA intermediate tokens: 5-minute-TTL handles between password
	-- verification and TOTP verification when MFA is enabled.
	CREATE TABLE IF NOT EXISTS mfa_intermediate (
		token       TEXT PRIMARY KEY,
		user_id     INTEGER NOT NULL,
		tenant_id   INTEGER NOT NULL,
		email       TEXT NOT NULL,
		role        TEXT NOT NULL,
		name        TEXT,
		remember_me BOOLEAN DEFAULT FALSE,
		expires_at  TIMESTAMPTZ NOT NULL,
		created_at  TIMESTAMPTZ DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS email_tokens (
		token            TEXT PRIMARY KEY,
		tenant_id        INTEGER NOT NULL DEFAULT 1,
		email            TEXT NOT NULL,
		purpose          TEXT NOT NULL,
		user_id          INTEGER,
		registration_id  TEXT,
		expires_at       TIMESTAMPTZ NOT NULL,
		used_at          TIMESTAMPTZ,
		created_at       TIMESTAMPTZ DEFAULT NOW()
	);
	`)
	return err
}

// applyColumnBackfills brings an existing database up to the column set the
// code expects, before the numbered migrations run. It replaces runMigrations,
// whose statements were fired with their errors discarded — so a failure was
// invisible and the app served traffic against a schema it merely assumed.
// Every statement is IF NOT EXISTS, so this is a cheap no-op once converged.
func applyColumnBackfills(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS progress_reports (
			id                 TEXT PRIMARY KEY,
			tenant_id          INTEGER NOT NULL DEFAULT 1,
			student_id         TEXT NOT NULL,
			term               TEXT NOT NULL,
			teacher_id         TEXT,
			subject            TEXT,
			grade              TEXT,
			strengths          TEXT,
			areas_to_improve   TEXT,
			teacher_comment    TEXT,
			next_term_focus    TEXT,
			published          BOOLEAN DEFAULT FALSE,
			created_at         TIMESTAMPTZ DEFAULT NOW(),
			updated_at         TIMESTAMPTZ DEFAULT NOW(),
			deleted_at         TIMESTAMPTZ
		)`,
		`ALTER TABLE invoices ADD COLUMN IF NOT EXISTS payment_method TEXT DEFAULT ''`,
		`ALTER TABLE invoices ADD COLUMN IF NOT EXISTS discount_pct REAL DEFAULT 0`,
		`ALTER TABLE invoices ADD COLUMN IF NOT EXISTS submitted_by_parent BOOLEAN DEFAULT FALSE`,
		`ALTER TABLE invoices ADD COLUMN IF NOT EXISTS sibling_ids TEXT DEFAULT '[]'`,
		`ALTER TABLE invoices ADD COLUMN IF NOT EXISTS sibling_discount REAL DEFAULT 0`,
		`ALTER TABLE registrations ADD COLUMN IF NOT EXISTS type TEXT DEFAULT 'student'`,
		`ALTER TABLE staff ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ`,
		`ALTER TABLE subjects ADD COLUMN IF NOT EXISTS deleted_at TEXT`,
		`ALTER TABLE workshops ADD COLUMN IF NOT EXISTS deleted_at TEXT`,
		`ALTER TABLE self_study_sessions ADD COLUMN IF NOT EXISTS deleted_at TEXT`,
		`ALTER TABLE performance_reviews ADD COLUMN IF NOT EXISTS deleted_at TEXT`,
		`ALTER TABLE holidays ADD COLUMN IF NOT EXISTS deleted_at TEXT`,
		`ALTER TABLE registrations ADD COLUMN IF NOT EXISTS specialization TEXT DEFAULT ''`,
		`ALTER TABLE registrations ADD COLUMN IF NOT EXISTS nric TEXT DEFAULT ''`,
		`ALTER TABLE registrations ADD COLUMN IF NOT EXISTS display_name TEXT DEFAULT ''`,
		`ALTER TABLE registrations ADD COLUMN IF NOT EXISTS employment_type TEXT DEFAULT 'Full-time'`,
		`ALTER TABLE registrations ADD COLUMN IF NOT EXISTS experience TEXT DEFAULT ''`,
		`ALTER TABLE registrations ADD COLUMN IF NOT EXISTS qualifications TEXT DEFAULT ''`,
		`ALTER TABLE registrations ADD COLUMN IF NOT EXISTS bio TEXT DEFAULT ''`,
		`ALTER TABLE registrations ADD COLUMN IF NOT EXISTS schedule TEXT DEFAULT ''`,
		`ALTER TABLE registrations ADD COLUMN IF NOT EXISTS expected_salary TEXT DEFAULT ''`,
		`ALTER TABLE students ADD COLUMN IF NOT EXISTS family_id TEXT DEFAULT ''`,
		`ALTER TABLE replacement_credits ADD COLUMN IF NOT EXISTS category TEXT DEFAULT 'class'`,
		`ALTER TABLE users         ADD COLUMN IF NOT EXISTS status TEXT DEFAULT 'active'`,
		`ALTER TABLE users         ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMPTZ`,
		`ALTER TABLE users         ADD COLUMN IF NOT EXISTS failed_login_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users         ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ`,
		`ALTER TABLE registrations ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMPTZ`,
		`ALTER TABLE families      ADD COLUMN IF NOT EXISTS referral_code TEXT DEFAULT ''`,
		`ALTER TABLE students      ADD COLUMN IF NOT EXISTS referred_by_family_id TEXT DEFAULT ''`,
		`ALTER TABLE registrations ADD COLUMN IF NOT EXISTS referral_code TEXT DEFAULT ''`,
		`ALTER TABLE invoices      ADD COLUMN IF NOT EXISTS referral_credit DOUBLE PRECISION DEFAULT 0`,
		`ALTER TABLE invoices      ADD COLUMN IF NOT EXISTS reminder_sent_on TEXT`,
		`ALTER TABLE students ADD COLUMN IF NOT EXISTS package_amount NUMERIC DEFAULT 0`,
		`ALTER TABLE students ADD COLUMN IF NOT EXISTS package_self_study_hours INTEGER DEFAULT 4`,
		`ALTER TABLE students ADD COLUMN IF NOT EXISTS subscription_status TEXT DEFAULT 'active'`,
		`ALTER TABLE students ADD COLUMN IF NOT EXISTS paused_at TIMESTAMPTZ`,
		`ALTER TABLE students ADD COLUMN IF NOT EXISTS resumed_at TIMESTAMPTZ`,
		`ALTER TABLE invoices ADD COLUMN IF NOT EXISTS reference_no TEXT DEFAULT ''`,
		`ALTER TABLE families ADD COLUMN IF NOT EXISTS referral_credits_remaining INTEGER DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_secret TEXT DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_enabled BOOLEAN DEFAULT FALSE`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_recovery_codes TEXT DEFAULT '[]'`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("column backfill %.60q: %w", s, err)
		}
	}
	return nil
}

// backfillFamilyReferralCodes assigns a unique SH-XXXX code to every family
// that does not already have one. Safe to run repeatedly.
func backfillFamilyReferralCodes(db *sql.DB) {
	rows, err := db.Query(`SELECT id FROM families WHERE (referral_code IS NULL OR referral_code = '') AND deleted_at IS NULL`)
	if err != nil {
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
	if len(ids) == 0 {
		return
	}
	log.Printf("Backfilling referral codes for %d families...", len(ids))
	for _, id := range ids {
		// Try a few times in case of code collisions.
		for attempt := 0; attempt < 5; attempt++ {
			code := core.NewReferralCode()
			_, err := db.Exec(`UPDATE families SET referral_code=$1 WHERE id=$2 AND (referral_code IS NULL OR referral_code='')`, code, id)
			if err == nil {
				break
			}
		}
	}
}

func migrateStudentsToFamilies(db *sql.DB) {
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM students WHERE (family_id = '' OR family_id IS NULL) AND contact != '' AND deleted_at IS NULL`).Scan(&count)
	if count == 0 {
		return
	}
	log.Printf("Migrating %d students into families...", count)

	// COALESCE: parent_name and phone are nullable, and a NULL failed the scan
	// below — which skipped that family silently, so a student with no phone
	// was never migrated.
	rows, err := db.Query(`SELECT DISTINCT contact, COALESCE(parent_name,''), COALESCE(phone,'') FROM students WHERE contact != '' AND deleted_at IS NULL ORDER BY contact`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var contact, parentName, phone string
		if err := rows.Scan(&contact, &parentName, &phone); err != nil {
			core.Logger.Error("failed to scan family migration row", "err", err)
			continue
		}

		var existingID string
		db.QueryRow(`SELECT id FROM families WHERE contact = $1`, contact).Scan(&existingID)

		famID := existingID
		if famID == "" {
			famID = "FAM_" + time.Now().Format("20060102150405") + fmt.Sprintf("%04d", time.Now().Nanosecond()/1000000)
			familyName := parentName + " Family"
			if parentName == "" {
				familyName = contact
			}
			db.Exec(`INSERT INTO families(id, tenant_id, name, contact, phone, parent_name) VALUES($1, 1, $2, $3, $4, $5)`,
				famID, familyName, contact, phone, parentName)
			time.Sleep(time.Millisecond) // ensure unique IDs
		}

		db.Exec(`UPDATE students SET family_id = $1 WHERE contact = $2 AND (family_id = '' OR family_id IS NULL) AND deleted_at IS NULL`, famID, contact)
	}
	log.Println("Family migration complete.")
}
