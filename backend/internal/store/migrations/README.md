# Migrations

Numbered SQL files applied in order on every server boot. Tracked in the
`schema_migrations` table by version + checksum so each one runs exactly once.

## Adding a new migration

1. Pick the next number after the highest existing file (e.g. `0002_*.sql`).
2. Create `NNNN_short_name.sql` in this folder.
3. Write idempotent SQL — prefer `IF NOT EXISTS` / `IF EXISTS` clauses where
   possible so the file is safe to re-run during local dev resets.
4. Each file runs in its own transaction. Don't span DDL across multiple files
   that need to commit together.
5. Commit the file. On the next deploy the new migration is applied
   automatically; the version + checksum is recorded in `schema_migrations`.

## Naming convention

`NNNN_short_lower_snake.sql`

- `NNNN` is a zero-padded 4-digit sequence number, lexicographic = chronological.
- The name should describe the *change*, not the *table*: `0007_add_referral_code_to_families.sql`, not `0007_families.sql`.

## Don't edit applied migrations

If you edit a file after it's been applied, the checksum changes and you'll get
a `WARNING — checksum mismatch` log on every boot. **Don't do this.** Different
environments will have applied the original version, leaving them out of sync
in ways that are very hard to debug. Instead, add a new migration that fixes
or replaces what the old one did.

## Why this isn't using the Atlas CLI

Atlas the CLI is great but requires installing a separate binary on every
developer machine *and* in the production Docker image. This applier is pure
Go stdlib (`embed` + `database/sql`), so the migration files ship inside the
binary itself and there's nothing extra to install or manage. The trade-off:
no fancy schema diffing — you write the SQL by hand. For a project this size
that's the right call.
