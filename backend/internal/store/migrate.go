package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strings"
)

// Migrations are numbered SQL files under backend/internal/store/migrations/, embedded into
// the binary at build time. On startup, runFileMigrations() applies any that
// haven't been recorded in the schema_migrations table yet.
//
// Why this lives alongside the existing runMigrations() in database.go:
//   - runMigrations() is the legacy ALTER-IF-NOT-EXISTS path that handles
//     production databases provisioned before this system existed. It's
//     idempotent and harmless to keep running.
//   - runFileMigrations() is the path forward: every NEW schema change goes
//     into backend/internal/store/migrations/NNNN_<name>.sql and is tracked properly.
//
// Why not the Atlas CLI? It requires installing a separate binary and shipping
// it in the Docker image. A pure-stdlib applier is simpler, has zero deps,
// and gives us the same versioned-migrations property.

//go:embed migrations/*.sql
var migrationFiles embed.FS

// runFileMigrations applies any unapplied .sql files from the embedded
// migrations directory in lexicographic (= numeric) order.
func runFileMigrations(db *DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		checksum   TEXT NOT NULL,
		applied_at TIMESTAMPTZ DEFAULT NOW()
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	// Acquire a PostgreSQL advisory lock so two instances starting
	// simultaneously don't race on the same migration. Session-level locks
	// are bound to ONE connection, so we must hold a dedicated *sql.Conn for
	// the lock's lifetime — acquiring via the pool (db.Exec) could acquire and
	// release on different pooled connections, leaking the lock. Closing the
	// connection releases the lock even if the explicit unlock is missed.
	// Key 20260411 is arbitrary — just needs to be unique across the app.
	ctx := context.Background()
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration lock conn: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(20260411)`); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}
	defer conn.ExecContext(ctx, `SELECT pg_advisory_unlock(20260411)`)

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		// No migrations folder yet — that's fine.
		return nil
	}

	// Collect .sql files and sort by filename. The naming convention
	// (NNNN_name.sql) means lexicographic order = correct order.
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	// Pull the set of already-applied versions in one query so we don't
	// hammer the DB on a cold start with 50 migrations.
	applied := map[string]string{} // version -> checksum
	rows, err := db.Query(`SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	for rows.Next() {
		var v, c string
		if err := rows.Scan(&v, &c); err != nil {
			rows.Close()
			return err
		}
		applied[v] = c
	}
	rows.Close()

	for _, file := range files {
		version := strings.TrimSuffix(file, ".sql")
		body, err := migrationFiles.ReadFile("migrations/" + file)
		if err != nil {
			return fmt.Errorf("read %s: %w", file, err)
		}
		sum := sha256.Sum256(body)
		checksum := hex.EncodeToString(sum[:])

		if existing, ok := applied[version]; ok {
			// Already applied — verify checksum hasn't drifted, which would
			// mean someone edited a historical migration in place. That's a
			// dangerous mistake (different envs end up at different states)
			// so we surface it loudly but don't crash on it.
			if existing != checksum {
				log.Printf("MIGRATIONS: WARNING — checksum mismatch on %s; the file has been edited after being applied. This is unsafe across environments.", version)
			}
			continue
		}

		log.Printf("MIGRATIONS: applying %s", version)
		// Each .sql file runs in its own transaction so a partial failure
		// rolls back cleanly. Migrations that need multiple statements should
		// just include them — pgx handles multi-statement Exec.
		tx, err := db.BeginTx(context.Background())
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", version, err)
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply %s: %w", version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, checksum) VALUES(?, ?)`, version, checksum); err != nil {
			tx.Rollback()
			return fmt.Errorf("record %s: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", version, err)
		}
	}
	return nil
}
