// Command migrate applies pending migrations to the database named by
// DATABASE_URL, then exits.
//
// It exists for the migration dry run. That script restores a copy of
// production and needs the migrations applied to it — but the only other way
// to run them was the test harness, and setupFeatureTestApp DELETEs every
// table before it starts (feature_flow_test.go:41). So the dry run restored
// production, deleted it, seeded eight demo classes, and reported on those
// while appearing to validate the real estate.
//
// Booting cmd/api would also work and would also start jobs, an HTTP listener
// and the mailer. Applying migrations should not require any of that.
package main

import (
	"log"
	"os"

	"studyhub/internal/store"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set")
	}
	store.InitDB(dsn)
	log.Println("migrations applied")
}
