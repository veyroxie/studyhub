# Database layer, migrations, schema conventions

Read this before writing SQL, adding a column, or creating a migration. Raw SQL via pgx,
no ORM, no repository pattern.

## The `?` placeholder trap

Every query through `store.DB` / `store.Tx` is rewritten from `?` to `$1..$N` by
`convertPlaceholders`, which tracks only single-quote state (`store/db.go:16-18`).

**It corrupts a literal `?` outside a single-quoted string** -- jsonb operators (`?`, `?|`,
`?&`) -- and its quote tracking is defeated by an apostrophe inside a SQL comment. Migration
bodies deliberately bypass it via the embedded `tx.Tx.Exec` for exactly this reason
(`migrate.go:125-128`).

So: use `?` everywhere in handler SQL, never `$N`, never string concatenation of values -- but
if you need a jsonb `?` operator, you cannot go through the wrapper.

## Migrations

`.sql` files under `migrations/`, embedded with `go:embed`, discovered by directory read and
ordered by **lexicographic filename sort** (`migrate.go:29-30, 67-75`). Latest is `0040`.

- Applied migrations are tracked in `schema_migrations` (version, checksum, applied_at). A
  checksum mismatch on an already-applied file is a **fatal boot error**, not a warning:
  "an edited historical migration means..." (`migrate.go:35-39, 108-112`). Editing an applied
  migration bricks every deploy. The fix is always a new migration.
- Each file runs in **its own transaction** (body plus tracking insert), rolled back on
  failure; a failure aborts `InitDB` via `log.Fatalf` (`migrate.go:118-131`,
  `database.go:38-40`). DDL that cannot run in a transaction (`CREATE INDEX CONCURRENTLY`)
  will fail here.
- Runs are serialized across instances by a session-level advisory lock (key `20260411`) held
  on a dedicated `*sql.Conn`, because pool-issued lock/unlock could land on different
  connections (`migrate.go:43-59`).
- **Land a risky constraint in its own migration.** `0038` added `invoices.period` and
  explicitly deferred the unique index to `0039` because "a pre-existing duplicate would make
  this migration fail, and a failed migration stops the API booting"
  (`0038_invoice_period.sql:14-17`).

### Boot order and the createSchema/migrations split

```
createSchema()          -> applyColumnBackfills() -> runFileMigrations() -> data backfills
(database.go:31-44)
```

On a **fresh** database `createSchema` runs first, so a `CREATE TABLE IF NOT EXISTS` in a
migration becomes a no-op and `createSchema`'s definition wins for tables it defines. On an
**existing** database, columns present only in migrations exist only because the numbered
files ran.

**Every new schema change goes in `migrations/NNNN_*.sql`** (`migrate.go:22-23`). Adding a
column only to `createSchema` leaves production without it.

`applyColumnBackfills` replaced a legacy `runMigrations` "whose statements were fired with
their errors discarded -- so a failure was invisible and the app served traffic against a
schema it merely assumed" (`database.go:400-405`). Never revive the ignore-errors pattern.

`0033_retire_boot_alters.sql:8-10` deliberately did NOT carry over 13 `ALTER COLUMN ... TYPE
NUMERIC` statements, because re-issuing them every boot took an ACCESS EXCLUSIVE lock on the
money tables. Do not "restore completeness" there.

### House style

```sql
-- NNNN_short_lower_snake.sql
--
-- One line on what this adds.
--
-- Why: the problem observed (often a concrete production failure), why this shape and
-- not the alternative, and any deliberate omission with its reason.

CREATE TABLE IF NOT EXISTS t (
    id          TEXT PRIMARY KEY,
    tenant_id   INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);

-- One line justifying the index.
CREATE UNIQUE INDEX IF NOT EXISTS idx_t_something
    ON t (tenant_id, ...) WHERE deleted_at IS NULL;
```

Zero-padded 4-digit prefix; the name describes the change, not the table. Every statement
idempotent (`IF NOT EXISTS`, `ON CONFLICT DO NOTHING`). No explicit `BEGIN`/`COMMIT` -- the
applier wraps it. Backfills derive ids deterministically (`0036`: `'PRD_' || pt.id`) so
re-apply is a no-op. Derived from `0036`-`0040` and `migrations/README.md`.

## Schema conventions

**Primary keys are split.** `tenants`, `centers`, `users`, `audit_logs` use `SERIAL`. All
domain tables (`students`, `families`, `classes`, `staff`, `invoices`, `announcements`,
`attendance`, `payroll`, ...) use `id TEXT PRIMARY KEY` with prefixed generated ids
(`database.go:51, 66, 76, 99, 171`).

Mint text ids with **`core.GenerateID(prefix)`** -- timestamp plus 4 crypto-random hex bytes,
specifically "so two IDs minted in the same millisecond -- e.g. inside the monthly invoice /
payroll cron loops -- never collide and silently drop a row" (`core/respond.go:146-158`). The
older `handlers.generateID` is timestamp-only with no random suffix (`handlers.go:176-178`);
using it inside a loop can produce duplicate primary keys.

`students.id` (`STU_...`) is an **immutable surrogate key** referenced by invoices,
attendance, credits, self-study, referrals, and progress reports. The human-facing editable
number is the separate `student_no` column with a partial unique index per tenant
(`0029_student_no.sql:4-17`).

**Soft deletes are inconsistent by type.** `deleted_at` is TEXT on `students`, `families`,
`classes`, `invoices`, `feedback`, `subjects`, `workshops`, `self_study_sessions`,
`performance_reviews`, `holidays`; TIMESTAMPTZ on `staff`, `progress_reports`, `products`,
`class_session_overrides`. New tables follow the TIMESTAMPTZ style. Always filter
`deleted_at IS NULL` regardless of type -- but never cast or compare it across tables.

**Several tables have no `deleted_at` at all**: `attendance`, `payroll`, `cancelled_classes`,
`registrations`, `replacement_credits`, `feedback_replies`, `referral_rewards`,
`announcements`, `users`, `audit_logs`. Adding the filter to their queries errors on a
nonexistent column; deleting from them is a hard delete.

**Many date/time fields are TEXT, not date types** -- `students.dob`, `invoices.due_date` /
`created_on` / `paid_on`, `attendance.date`, `payroll.month`, `holidays.date` -- and window
filters compare them lexicographically in `YYYY-MM-DD` form (`snapshot_bounded.go:31-33, 51`).
Postgres date functions or non-ISO formats break those comparisons. `0038` even dates invoices
by `substr(created_on, 1, 7)`.

**JSON-in-TEXT arrays are the house pattern**: `classes.teacher_ids`,
`students.enrolled_classes`, `invoices.line_items` (`0040:11-17`). They are matched with
`LIKE '%"<id>"%'` in several places. Normalizing them into join tables means updating every
reader at once.

**`enrolled_classes` has a shadow join table** (`enrollments`, migration `0043`): dual-written
by `store.SyncEnrollments` / `EndAllEnrollments` (`store/enrollments.go`) at every mutation
site, with `started_on` / `ended_on` history and a partial unique index on live rows
(`ended_on IS NULL`). The JSON column is still the ONLY read path -- any new enrolment
mutation MUST call `SyncEnrollments` after writing the JSON, or the tables drift.

## Tenant scoping

Covered in full in `AI_DOCS/auth-and-tenancy.md`. The short version: `store.ScopeTenant`
returns a fragment starting with `" AND "`, so base queries start `WHERE 1=1` or
`WHERE deleted_at IS NULL`; tenant 0 is superadmin and gets an empty fragment
(`store/scope.go:27-40`). There is no database-level backstop.

## Handler plumbing

- `respond(w, v)` writes JSON with Content-Type only.
- `respondError(w, msg, code)` writes the status plus `{"error": msg, "request_id": ...}`,
  reading the id back from the `X-Request-Id` header set by middleware
  (`handlers.go:71-87`). Do not invent a different error shape -- the frontend parses this one.
- `logFromReq(r)` returns the global slog logger pre-populated with `request_id`
  (`core/logger.go:61-75`). `core.Logger` is JSON in production, text in dev, and is nil until
  `InitLogger()` runs.

**Duplicate helpers exist.** `handlers` carries near-copies of `core`'s `respond`,
`respondError`, `maskNRIC`, `advisoryLockKey`, `validationError`, `newReferralCode`, and
pagination. Fixing a bug in one copy leaves the other divergent -- check both before editing
either.

Use **`core.LogAudit`**, which requires an explicit tenantID: a row written without it
"would default to tenant 1 and leak across tenants" (`core/respond.go:94-98`). The older
`handlers.logAudit` omits `tenant_id` entirely (`handlers.go:117-120`).

## Listing rows

Drain through **`store.CollectRows`**. It was extracted because ~26 hand-copied loops never
checked `rows.Err()`, so "a connection dropped mid-iteration returned a short list
indistinguishable from a complete one" (`store/collect.go:9-16, 35-37`). Note
`snapshot_bounded.go` still uses the old hand-rolled loops.

## Caching

**Snapshot cache**: in-process map keyed `tenant|role|email`, 10-second TTL, holding the
marshalled `/api/snapshot` body (`snapshot_cache.go:26, 100-105`). The email component exists
because parents see post-filtered data scoped to their own children -- a tenant-only key would
leak.

Invalidation is **middleware-based**: `SnapshotCacheInvalidator` drops the requester's whole
tenant cache after any 2xx non-GET, so write handlers need no bookkeeping
(`snapshot_cache.go:153-160`). **Writes that bypass HTTP -- cron jobs, direct DB writes -- do
not trigger it** and must call `SnapshotCacheInvalidateAll()` themselves.

Builds use a hand-rolled singleflight so concurrent cold-cache requests run the ~18 fan-out
queries once; a follower whose leader failed falls through and builds itself
(`snapshot_cache.go:37-44`, `handlers.go:210-217`). You must call `SnapshotSingleflightDone`
on **every** exit path after becoming leader, including the marshal-error path
(`handlers.go:327`), or followers block forever.

Cached responses carry a weak ETag (sha256 of the body) and return 304 on `If-None-Match`, so
a 30s poll gets 5 bytes instead of ~500KB (`snapshot_cache.go:183-201`). Nondeterministic
field ordering breaks ETag stability.

Expiry is lazy only -- a prior O(n) full-map sweep on every Put was removed as wasted hot-path
work (`snapshot_cache.go:122-125`).

**Tenant settings** cache for 10 minutes, and a generic DB error is deliberately **not**
cached -- only a real row or `ErrNoRows` -- so a transient blip does not pin the default
fallback (branding, bank details) for the whole TTL (`tenant_settings.go:52, 100-110`). Writes
must call `invalidateTenantSettings(tid)`.

## Connection pool

Pinned at 40 open / 10 idle, sized to snapshot fan-out times concurrent sessions against
Postgres `max_connections=60` in compose. Both prior values (50/10 and 150) caused documented
failures (`database.go:18-26`). Raising `MaxOpenConns` past ~60 to "fix pool exhaustion"
recreates the "too many clients" outage.

## Snapshot bounds

Constants in `snapshot_bounded.go:11-29`: 90 days for attendance/feedback/self-study,
unpaid-or-24-months for invoices, 6 months-or-unarchived for announcements. **Each `_Recent`
function must mirror its unbounded sibling exactly except for the WHERE clause** -- adding a
column to one without the other desynchronises dashboard and detail views.

`ListInvoicesRecent` hard-gates access: teachers get an empty slice, parents get invoices
joined through `students.contact = claims.Email` (`snapshot_bounded.go:110-119`). Parent
identity is that email link, not a foreign key -- changing a parent's email without updating
`students.contact` orphans their billing view.
