# StudyHub full-code review — fix tickets (2026-09-09)

28 findings across six subsystems, reviewed at `main`. Every line cited below was
read at its stated location. Nothing in the repo was changed by the review.

Companion to [code-review-2026-07-04.md](./code-review-2026-07-04.md); see
*Continuity with the July review* for what recurred and why.

## Rules for the implementer (read first)

- Work ticket by ticket, in order. One Conventional Commit per ticket, e.g. `fix(cron): ...` — do NOT batch unrelated tickets into one commit.
- Follow project conventions: raw SQL with `?` placeholders, tenant scoping via `store.ScopeTenant`, `deleted_at IS NULL` on soft-deleted tables, `core.Respond/RespondError`, frontend strings escaped with `App.Utils.esc()`, no build step, IIFE modules on `window.App`.
- NEVER edit an already-shipped migration file (0001–0062 are checksum-tracked). New DDL = new numbered file. **Migration numbers are pre-assigned below: 0063 and 0064. Do not renumber.**
- After each backend ticket: `cd backend && go build ./... && go vet ./...` must pass. After each frontend ticket: `node --check frontend/js/<changed file>` must pass.
- Frontend unit tests need the glob form, not the directory form: `TZ=Asia/Kuala_Lumpur node --test frontend/tests/unit/*.test.mjs`. The directory form in `CLAUDE.md` fails on Node 22 with `MODULE_NOT_FOUND` (T28 corrects the doc).
- Do NOT refactor beyond what a ticket says. Do NOT "fix" anything in the *Verified clean* list at the bottom, or anything in *Claims rejected*.

## The deadline that orders this list

The monthly cron runs on days 1–7 and on every boot. Every P0 ticket either loses a
month of invoices or bills a parent twice, and gets its next opportunity to do so on
**1 October 2026**.

---

## Three patterns, not twenty-eight bugs

Most of the list collapses into three root causes. Each is already diagnosed in a
comment somewhere in the repo and fixed in exactly one place — the analysis was done,
it was never swept.

**1. An overloaded sentinel: `tenant_id = 0`.** `store.TenantID` returns `0` for a
superadmin and `ScopeTenant` reads `0` as "apply no filter". Correct for reads. For
writes it stores a value meaning *every tenant*, and it is also the value a column
lands on when nobody thought about it. Two opposite failures follow: a created user
silently gains cross-tenant reach (T10), and a cancellation silently reaches nobody
(T10). `handlers_classes.go:150` already states the rule and fixes one site.

**2. Fail-open beside fail-closed.** Six preloads open the monthly run. Four log
`ERROR` and `return 0`, each with a comment explaining that failing closed is
mandatory. Two do not, and one of those decides prices (T1).

**3. Identity carried by editable free text.** An invoice dedup key on a description
an admin can retype (T4), announcement ownership on a display name that falls back to
the literal `'Teacher'` (T25), and family membership on a contact field that is blank
for 21 students (T24).

## Continuity with the July review

Two tickets from [code-review-2026-07-04.md](./code-review-2026-07-04.md) came back in
adjacent form. Neither is a regression; both are the unswept remainder of a fix that
was scoped narrowly.

- **T4 (July) fixed every preload that answers "does this already exist"** — monthly,
  overflow, rollover, payroll. T1 below covers the two that answer *"what does this
  cost"* and *"what is owed"*. Identical fail-open shape, outside that ticket's stated
  scope.
- **T8 (July) guarded the `role` on the user-create INSERT.** The allowlist is there and
  works. The `tenant_id` on the same statement is still wrong (T10).

July's T4 also recorded a deliberate decision — *do not* add a unique index on
`invoices`, because a student can legitimately hold two Monthly invoices in one month
(manual sibling invoice plus admin adhoc). That decision was later relaxed for the
`Monthly` type via `idx_invoices_monthly_unique`. T4 below asks you to weigh it again
for the overflow type rather than assuming the index is safe.

---

## P0 — actively destructive or money-wrong today

### T1. Three cron preloads still fail open — the ones July's T4 did not cover
**Files:** `backend/internal/jobs/cron.go` — class/fee preload (`:345`), sibling roster preload (`:435`), `loadFamilyReferralCredits` (`:701-703`).
**Amended during implementation:** the sibling roster preload was found by T1's own
Accept criterion and was not in the original list. Same shape, same function; on a
query error `siblingsByFamily` stays empty, `len(sibs) >= 2` never holds at `:532`, and
every multi-child family is billed without its sibling discount.
**Problem:** both use the pre-T4 shape. `:345` is `if frows, ferr := db.Query(...); ferr == nil {` with no `else` — `ferr` is never logged and never aborts. `:701` is a bare `continue` on a per-tenant query error, also unlogged. The four sibling preloads at `:189`, `:213`, `:334` and `:846` each log `ERROR` and `return 0`, with comments saying fail-closed is mandatory.
**Failure:** a statement timeout on `:345` leaves `classByID` empty; every student with `package_amount = 0` then finds `m.fee <= 0` for every enrolled class, each line is skipped, `base` stays 0, and they fall through the `base <= 0` skip at `:501`. The run creates **zero invoices for the entire roster**, logs nothing at ERROR, and because `created > 0` is false it does not even emit its summary line. The `:701` variant silently strips every referral discount for one tenant's families: invoices go out RM10 per child too high *and* the credits are not consumed, so balances and billing disagree from then on.
**Fix:**
1. `:345` — on `ferr != nil`, `core.Logger.Error("monthly cron: class fee preload failed", "err", ferr)` and `return 0`.
2. `:435` — same treatment for the sibling roster preload.
3. `:701` — same treatment; `loadFamilyReferralCredits` returns `(map[string]int, error)` to match `loadExistingMonthlyInvoiceStudentIDs`, and the caller logs and returns 0.
**Accept:** `go build/vet` pass; `grep -n "err == nil {" backend/internal/jobs/cron.go` returns no preload path that proceeds with a partially-filled map.

### T2. Billing goroutines have no panic recovery and no heartbeat
**Files:** `backend/internal/jobs/cron.go:103`, `:778`, `:1100`; `backend/internal/jobs/jobs.go:67-70` (`jobLimits`).
**Problem:** `grep -c "recover()\|safeRun\|goSafe" cron.go` returns **0**. Every 30-second email tick goes through `safeRun` (`jobs.go:355`), and `handlers/background.go:11` defines `goSafe` with the rationale written out — chi's `Recoverer` only wraps the request goroutine, so an unrecovered background panic takes down the whole API process. The three entry points that create invoices are the only ones without it, and `:1100` is reachable from an admin button.
**Failure:** any nil dereference or index panic under `generateMonthlyInvoices` kills the process. `docker-compose.yml:89` is `restart: unless-stopped` and `StartCron` (`cron.go:105`) runs the cycle immediately on boot, so the container restarts, panics on the same student row, and crash-loops the API for every user — on the 1st of the month. Compounding it, `jobLimits` is built only from the `jobs` slice in `StartJobs`, which does not contain the monthly cron, so `store.StaleJobs` never watches it: if billing stops, nothing alerts.
**Fix:**
1. Wrap all three goroutines in the existing `safeRun` / `goSafe` helper.
2. Register the monthly cron in the `jobs` slice so it gets a `jobLimits` entry and a heartbeat.
**Accept:** `grep -c "safeRun\|goSafe" backend/internal/jobs/cron.go` returns 3; the monthly cron appears in `StaleJobs` output after a run.

### T3. Invoice update has no status guard — Paid invoices are repriceable, and back-dating double-bills
**Files:** `backend/internal/handlers/handlers_invoices.go:256` (both UPDATE branches); `frontend/js/modules/billing.js:390` (Edit renders for every status).
**Problem:** both branches filter only on `id`, tenant and `deleted_at IS NULL` — no status condition anywhere. Both also recompute `period` by passing `monthlyPeriod(inv.Type, inv.CreatedOn)` into the statement.
**Failure:** two distinct outcomes.
(a) An invoice paid RM800 through the gateway, with receipt `RCPT-000123` already downloaded by the parent, is edited to RM650. Receipt assignment is idempotent, so `/receipt.pdf` now serves that receipt number against a figure the gateway never collected.
(b) Back-dating `created_on` moves the row out of the current month's dedup set, so the next cron tick inside the day-1-to-7 window issues a **second Monthly invoice** for the same student. Per [invoices-often-hand-made](./adr.md) practice, back-dating is routine here, so this is reachable in normal use.
**Fix:**
1. Reject the update with 409 when `status = 'Paid'`, or restrict it to description-only. Decide which — a centre may legitimately need to correct a typo on a paid invoice.
2. Stop recomputing `period` from an edited `created_on` on a `Monthly` invoice. `period` is the dedup key; it should be set at creation and left alone.
**Accept:** editing a Paid invoice's amount is refused; back-dating a Monthly invoice's `created_on` leaves `period` unchanged, and a subsequent cron run creates nothing for that student.

### T4. Self-study overflow dedup keys on an admin-editable description
**Files:** `backend/internal/jobs/cron.go:195` (preload), `:279` (INSERT); `backend/internal/handlers/handlers_invoices.go:256`; `frontend/js/modules/billing.js:1187`, `:1230`. New migration `0063`.
**Problem:** the only thing preventing a second overflow invoice is that the previous one's `description` still contains the literal `"Aug 2026"`:
```
WHERE type='Self-study Overflow' AND description LIKE '%'||monthHuman||'%' AND deleted_at IS NULL
```
There is no database backstop: `idx_invoices_monthly_unique` is scoped `WHERE type = 'Monthly'`, and the overflow INSERT at `:279` never sets `period` at all.
**Failure:** the cron creates `"Self-study overflow — Aug 2026 — <student> (2 hr over quota)"`. An admin retitles it to `"Self-study overflow (August)"` through the free-text Description input, which `handlers_invoices.go:256` writes unconditionally with no protection for system-generated types. The next day-1-to-7 tick misses the `LIKE`, and a second charge is inserted. Overflow invoices queue **no** email, so nothing surfaces it — the parent simply sees two charges.
**Fix:**
1. Set `period` on the overflow INSERT at `:279` and dedup on `period`, not on `description`. This is the substance of the ticket and is safe on its own.
2. **Decision required** before adding a unique index. July's T4 deliberately declined one on `invoices` because a student can legitimately hold two `Monthly` invoices in a month; that reasoning was later judged not to apply to `Monthly` itself. Confirm whether a student can ever legitimately hold two `Self-study Overflow` invoices for one month. If not, add:
   ```sql
   -- backend/internal/store/migrations/0063_overflow_period_unique.sql
   CREATE UNIQUE INDEX IF NOT EXISTS ux_invoices_overflow_period
       ON invoices (tenant_id, student_id, period)
       WHERE type = 'Self-study Overflow' AND deleted_at IS NULL AND period <> '';
   ```
   If yes, skip the index and rely on the `period` dedup alone.
3. Note for `AI_DOCS/billing.md`: it currently states the frontend guards the invoice *type* "because reclassifying a system type would corrupt the overflow dedup". The guard is on the wrong field. Correct the sentence once the dedup moves to `period`.
**Accept:** retitling an overflow invoice's description does not cause a second one on the next run.

### T5. Cancellation credits are computed as of now, not as of the cancelled date
**Files:** `backend/internal/handlers/handlers_cancelled.go:106` (duration), `:140` (recipients). Helpers already exist: `store.EnrollmentWindowsIn` (`store/enrollments.go:98`), `store.ScheduleOn` (`store/sessions.go:170`).
**Problem:** `applyCancelledClassSideEffects` picks recipients with `SELECT id FROM students ... enrolled_classes LIKE '%"<classID>"%'` — the *current* JSON list, with no reference to `enrollments.started_on` / `ended_on`. It then computes `credits := creditsForDuration(classStart, classEnd)` from `classes.time` / `end_time` as they stand now, rather than the schedule version in force on `cc.Date`. Migration `0047_class_schedule_versions.sql` exists precisely so historical times resolve on a date, and `store/sessions.go:32-36` documents that times must be "the schedule AS IT WAS on Date".
**Failure:** on 2026-09-09 an admin records that the 2026-08-19 session was cancelled. A student who joined that class on 2026-09-01 receives a 4-credit make-up for a lesson they were never enrolled in; a student who left on 2026-08-25 — who actually missed it — receives nothing. Separately, a class shortened from 2h to 1h effective 2026-09-01 grants 4 credits instead of 8 for that same back-dated cancellation. Credits are money: 1 credit = 15 minutes.
**Fix:**
1. `:140` — resolve recipients through `store.EnrollmentWindowsIn` against `cc.Date`, under the half-open `[started_on, ended_on)` rule.
2. `:106` — resolve the duration through `store.ScheduleOn(versions, current, cc.Date)`.
**Accept:** a back-dated cancellation credits exactly the students enrolled on that date, at the duration the class had on that date.

### T6. Import rollback deletes users across every tenant
**Files:** `backend/internal/handlers/handlers_import.go:425`.
**Problem:** the statement carries no tenant scope and discards its result, while both DELETEs bracketing it do carry `+tw`:
```go
res, err := db.Exec(`DELETE FROM `+table+` WHERE id=?`+tw, args...)          // :400 — scoped
res, _   := db.Exec(`DELETE FROM users WHERE email=? AND role='parent'`, email) // :425 — NOT scoped
res, _    = db.Exec(`DELETE FROM families WHERE contact=?`+tw, famArgs...)   // :430 — scoped
```
Tenant isolation is enforced at the query layer only — `store.RLSScope` is a deliberate passthrough per [rls-activation.md](./rls-activation.md) — so nothing catches the omission.
**Failure:** an admin in tenant A rolls back a bad import. Any parent account in tenant B whose email appears in that import is deleted outright, with no error surfaced (the result is discarded into `res, _`) and no audit trail of the cross-tenant reach.
**Fix:** append `+tw` with `twArgs`, and check the error and row count rather than discarding them.
**Accept:** `go build/vet` pass; a rollback in one tenant leaves another tenant's identically-addressed parent intact.

### T7. Any teacher can write attendance for any student in the tenant
**Files:** `backend/internal/handlers/handlers_attendance.go:148` (staff gate), `:166` (tenant-membership check), `:244` (upsert branch), `:267` (notification branch). Helper: `handlers_students.go:62`.
**Problem:** the POST path gates on `core.IsStaffRole(c)` and then verifies only that `person_id` exists *in the caller's tenant*. `teacherMayActOnStudent` is never called in this file, though every sibling handler uses it — `handlers_selfstudy.go:175`, `:217`, `handlers_replacement.go:104`, `:230`. That asymmetry makes it an omission, not a policy.
**Failure:** teacher T, who teaches only Class A, POSTs `{personId: "STU_x", personType: "student", date: today, checkIn: "15:00"}` for a child in Class B. The row is created, and because `a.PersonType == "student" && a.CheckIn != nil` the handler fires `hub.broadcastCheckIn` and `notify.NotifyParentOnCheck` — an unrelated family receives a real-time toast, a web push and an email saying their child checked in at 3pm. The same call with `{status: "absent"}` hits the upsert branch and **overwrites** the row authored by the child's real teacher, changing payroll hours and the absence record.
**Fix:** add the `teacherMayActOnStudent` guard on the POST path, matching the four sibling call sites.
**Accept:** a teacher writing attendance for a student outside their classes receives 403; an admin is unaffected.

### T8. Teacher check-in files the shift under whatever date the page was last left on
**Files:** `frontend/js/modules/attendance.js:5` (declaration), `:103-104` (init), write paths `:950` (`_teacherCheckIn`) and `:983` (`_teacherCheckOut`), kiosk path `:444`; render order `:1032` vs `:1058`. Consumer: `frontend/js/modules/staff.js:277`.
**Problem:** `_attDate` is a module-level `let` initialised only when falsy — `if (!_attDate) { _attDate = App.Utils.today(); }` — and thereafter changed only by `_setDate` (`:690`). It is never re-derived on a later render. Six sites read `_attDate || App.Utils.today()`: `:444`, `:903`, `:950`, `:983`, `:1087`, `:1275`. `_renderTeacherSelfCheckIn` renders **above** the date picker that sets it.
**Failure:** a teacher opens Attendance, scrolls the picker back to last Tuesday to check who was absent, scrolls back up and taps "Check In" for herself. The row is written with `date: 2026-09-01`, not today. `staff.js:277` exposes "Recalculate from check-ins", so payroll is rebuilt from exactly these rows — the shift is credited to the wrong day and today's shows as never worked. Second path, same root cause: the front-desk kiosk is left open overnight, so every barcode scan the next morning is filed under yesterday's date with a correct local time.
**Fix:** re-derive `_attDate` from `App.Utils.today()` on each render, or have the write paths read `App.Utils.today()` directly rather than through the picker's state. Do not use `toISOString()` — see the UTC+8 invariant in `CLAUDE.md`.
**Accept:** `node --check` passes; a check-in performed after browsing an earlier date is filed under today.

### T9. One transaction for the whole monthly run — a single row failure loses the month
**Files:** `backend/internal/jobs/cron.go:436` (`BeginTx`), `:576-630` (loop), `:582` (`continue`), `:617-626` (referral decrement), `:632` (`Commit`); `backend/internal/store/db.go:39-53`.
**Problem:** `db.BeginTx` returns a plain `*sql.Tx` wrapper with no savepoints. In Postgres the first failed statement aborts the block, so every later `tx.Exec` returns `current transaction is aborted` — and the loop's error handling is `continue`, which keeps iterating inside a dead transaction. The referral decrement at `:617` logs its error and **falls through** with no `continue`, so `familyCredits[creditKey]--` and `created++` still run, inflating the count of a run that cannot commit.
**Failure:** an admin edits an invoice from the billing screen while the cron is mid-loop; the two transactions deadlock and Postgres kills the cron's statement. Every remaining student's insert fails. `tx.Commit()` returns `ErrTxCommitRollback`, so the function returns 0 and correctly skips the email loop — but **nobody is billed**. Recovery depends on a later tick landing inside the day-1-to-7 window; a manual run on the 20th re-bills with `isLateRun` semantics (full price, no early bird), which is not what those parents were owed.
**Fix:** isolate per-student failures — either a savepoint per student, or one transaction per student rather than one for the run. Prefer the latter unless cross-student atomicity is actually required; it also removes the `continue`-into-a-dead-transaction hazard entirely.
**Accept:** a forced failure on one student's insert leaves every other student invoiced, and the reported `created` count matches rows actually committed.

---

## P1 — auth gaps and shipped-but-broken features

### T10. `tenant_id = 0` is written to rows — the cross-tenant marker, stored
**Files:** `backend/internal/handlers/handlers_users.go:83-84`; `backend/internal/handlers/handlers_cancelled.go:88` → `:106`, `:140`; the same `tid := store.TenantID(c)` insert shape in ~20 handlers including `handlers_attendance.go:213`, `handlers_selfstudy.go:181`, `handlers_replacement.go:170`/`:181`, `handlers_session_moves.go:104`, `handlers_session_overrides.go:68`. Reference fix: `handlers_classes.go:150`. Optional migration `0063`.
**Problem:** `store.TenantID` returns `0` for a superadmin (`store/scope.go:11-13`) and `ScopeTenant` reads `tid == 0` as "return the empty clause" (`:34-35`). Right for reads, wrong for writes. `handlers_classes.go:150` already spells this out: *"Tenant comes from the CLASS, not the caller: store.TenantID returns 0 for a superadmin, which is right for reads and wrong for writes."* That fix landed only there.
**Failure:** two opposite outcomes from one cause.
(a) **Excess privilege.** A superadmin creates an ordinary `admin` for tenant 2. The role allowlist at `:73` blocks `"superadmin"`, so it looks safe — but the row lands with `tenant_id = 0`. `HandleLogin` reads `tenant_id` off that row into `MakeToken` (`auth/auth.go:113-119`, `:249`), so their JWT carries `TenantID: 0`, and from then on every `ScopeTenant` call returns an empty clause for them: they read and write every tenant's students, invoices, families and audit logs, and `WSHub.deliver` (`ws.go:107`) forwards them every tenant's broadcasts. `users.tenant_id` is `INTEGER NOT NULL DEFAULT 1` with no CHECK and no FK (`store/database.go:67`), so `0` inserts cleanly.
(b) **Silent no-op.** A superadmin cancels a class. The `cancelled_classes` row commits with `tenant_id = 0`, so the tenant's own admin never sees it in `listCancelledClasses`. Inside `applyCancelledClassSideEffects` the class lookup at `:106` filters `tenant_id = 0`, returns nothing, and `creditsForDuration("","")` falls back to 4; the student query at `:140` matches zero rows. **Not one student receives a credit**, and no error is surfaced.
**Fix:**
1. Sweep the insert sites. Take the tenant from the row being written, not from the caller — copy `recordScheduleChange` in `handlers_classes.go:150`. The judgement per site is *which row supplies the tenant*, so do this by hand rather than mechanically.
2. Consider a backstop, since the DB currently accepts `0` without complaint:
   ```sql
   -- backend/internal/store/migrations/0064_tenant_id_positive.sql
   ALTER TABLE users ADD CONSTRAINT users_tenant_id_positive CHECK (tenant_id > 0);
   ```
   Apply to `users` first; widening it to the other ~20 tables is a separate decision, and any existing `tenant_id = 0` rows must be reassigned before the constraint will validate.
**Preconditions worth stating:** this requires a superadmin account to exist and, for the cross-tenant reach in (a) to matter, more than one tenant. The code path is unconditional; the exploitation is not.
**Accept:** a superadmin-created user's row carries the intended tenant, not 0; a superadmin-recorded cancellation credits the right students.

### T11. PDPA erasure leaves the subject's PII in five tables, and re-logs the erased email
**Files:** `backend/internal/handlers/handlers_families.go:135` (transaction), `:136` (`families`), `:144` (`students`), `:157` (`users`), `:169` (`LogAudit`), `:170` (`Logger.Info`).
**Problem:** the transaction redacts `families`, `students` and `users`, and nothing else. Confirmed against the schema, all of the following survive untouched:
- `registrations` — keeps `parent_name`, `email`, `phone`, `emergency_name`, `emergency_phone`, `student_first_name`, `student_last_name`, `student_dob`, `school_name` (`store/database.go`).
- `push_subscriptions.parent_email` — the subscription stays live, so the notify path can still push to the erased parent's device (migration `0018`).
- `email_queue.to_email` — the sender at `email_queue.go:67` dispatches on `status IN ('pending','sending') AND next_attempt_at <= NOW()` with no recipient-still-exists check, so a queued message is still delivered to an address the centre has just certified as erased (migration `0008`).
- `email_tokens.email` — outstanding reset/verify tokens keyed on the real address survive.
- `feedback_replies.author_email`.

Then the handler's own last two statements re-introduce the address it just erased: `:169` writes `"contact="+contact` into `audit_logs.detail`, and `:170` writes it to the application log.
**Failure:** a parent exercises their PDPA erasure right; the admin runs the delete; the API returns *"Account and associated data have been anonymised."* Their name, phone, child's date of birth and school remain queryable in `registrations`, a queued email still reaches them, and their email is preserved in the audit record of the erasure itself.
**Fix:**
1. Extend the transaction to redact or delete the five tables above.
2. Decide the audit record's shape before writing it. An erasure needs *an* audit trail, but it must not be the erased identifier — record the family id and the acting admin, not the contact.
**Accept:** after an erasure, no query against those five tables returns the subject's identifiers, and `audit_logs.detail` for the `pdpa_account_deleted` row contains no email address.
**Note:** this is the only ticket on the list with a statutory deadline attached.

### T12. Password-reset and set-password tokens are stored in plaintext
**Files:** `backend/internal/store/email_tokens.go:69` (`CreateEmailToken`), `ConsumeEmailToken` (~`:85`).
**Problem:** `CreateEmailToken` inserts the raw 256-bit token straight into `email_tokens.token`.
**Failure:** anyone who can read that table — a leaked nightly backup to DigitalOcean Spaces, a read-only DB grant, a future SQL injection elsewhere — holds working account-takeover links for every user with an outstanding token. Bounded by the TTL: 1h for `reset`, 24h for `set_password`.
**Fix:** store `sha256(token)` and look up by the hash. Lookup is already by exact token, so the change is confined to the two functions and costs nothing at runtime. Everything else in this flow is sound and must not be disturbed — see *Verified clean*.
**Accept:** `SELECT token FROM email_tokens` returns no value that works as a reset link; an end-to-end reset still succeeds.

### T13. The WebSocket upgrade validates the JWT signature and nothing else
**Files:** `backend/internal/handlers/ws.go:140-156`; route mounted at `internal/server/server.go:133`; the gate it skips is `auth/auth.go:452-478`.
**Problem:** `HandleWS` is mounted outside the authenticated group and re-implements auth itself — `jwt.ParseWithClaims` with HMAC pinning, then straight to `upgrader.Upgrade`. It never calls `auth.isTokenRevoked`, `lookupUserGate`, or the `sessions_invalid_before` comparison that `JWTMiddleware` applies.
**Failure:** an attacker holds a stolen "Remember me" cookie (30-day TTL, `auth.go:57`). The victim logs out, resets their password, or an admin suspends the account. All three close the REST surface — `HandleLogout` revokes the jti, `HandleResetPassword` bumps `sessions_invalid_before`, suspension flips `status`. None closes the WebSocket: the attacker connects to `/ws` with the same cookie and keeps receiving live attendance check-ins, announcements and invoice events for the tenant until the JWT's natural expiry. Connections established *before* the revocation are never evicted either — the hub drops a client only on write failure.
**Fix:**
1. Apply the same revocation, account-status and `sessions_invalid_before` checks before upgrading.
2. Evict live sockets for a principal when their session is invalidated.
**Accept:** a socket opened with a revoked token is refused, and an open socket is closed when its user logs out or resets their password.

### T14. Session overrides, moves and cancellations are readable by parents
**Files:** `backend/internal/handlers/handlers_session_overrides.go:33` (and `listSessionOverrides` at `:22`), `handlers_session_moves.go:34`, `handlers_cancelled.go:29`; routes at `internal/server/server.go:293`.
**Problem:** all three list handlers apply only `store.ScopeTenant`. The routes sit in the JWT group with no role middleware and the handlers carry no role check. Every other list in this area scopes by role — feedback, progress, self-study, replacement credits, announcements.
**Failure:** a parent hits `GET /api/session-overrides` and receives every teacher swap for every class in the centre, including the free-text `note` (in practice, things like "Nadine out — family emergency") and the `created_by` admin email. `GET /api/session-moves` similarly exposes the `reason` field for classes their children are not in.
**Fix:** scope all three by role, following the pattern already used by the feedback and progress lists.
**Accept:** a parent's request returns only rows touching their own children's classes; admin and teacher behaviour is unchanged.

### T15. No way back from Paid — `"Unpaid"` is missing from the status whitelist
**Files:** `backend/internal/handlers/handlers_invoices.go:419-428`; callers `frontend/js/modules/billing.js:386` ("Reject (Mark Unpaid)") and `:1062` ("Mark Unpaid").
**Problem:** the whitelist is exactly `{Paid, Pending Verification, Pending, Overdue}`. `"Unpaid"` is absent, so both buttons that send it receive `400 invalid status`. There is no other route back — `HandleInvoiceUpdate` deliberately never touches status.
**Failure:** a parent submits a bogus payment claim, or an admin marks the wrong invoice Paid. Neither can be undone. The receipt number is already burned from `receipt_no_seq` by that point.
**Fix:**
1. Add `"Unpaid"` to the whitelist, keeping the existing parent restriction (parents may still only submit `Pending Verification`).
2. Widening the whitelist alone is insufficient: `:480` never clears `paid_on`, and nothing clears `receipt_no`. Clear both when leaving `Paid`, and decide whether the burned receipt number should be reusable or permanently retired — retiring it is the safer default for a gapless-receipt expectation.
**Accept:** an admin can move a Paid invoice back to Unpaid; `paid_on` is null afterwards and the receipt PDF no longer renders as a valid receipt.

### T16. None of the eight payment env vars is forwarded — online payment cannot activate
**Files:** `docker-compose.yml:33-83` (the `api` `environment:` block); readers in `backend/internal/handlers/payments.go` — `PAYMENT_PROVIDER` (`:49`), `BILLPLZ_API_BASE` (`:126`), `BILLPLZ_API_KEY` (`:133`), `BILLPLZ_COLLECTION_ID` (`:134`), `BILLPLZ_X_SIGNATURE` (`:187`), `STRIPE_API_BASE` (`:272`), `STRIPE_SECRET_KEY` (`:279`), `STRIPE_WEBHOOK_SECRET` (`:332`). Templates: `backend/.env.example:42-53` versus the root `.env.example`.
**Problem:** grepping `docker-compose.yml` for all eight returns **zero matches**. What makes it a live defect rather than an unshipped feature is the divergence between the two templates: `backend/.env.example` documents every one in a full "Payment gateway" section, while the root `.env.example` — the file compose actually reads — omits all of them. `defaultPaymentProvider()` returns `"billplz"` when unset, so the checkout route at `server.go:198` is live and defaults to a provider whose credentials can never arrive.
**Failure:** an operator follows `backend/.env.example`, sets `BILLPLZ_API_KEY` and `BILLPLZ_COLLECTION_ID` in the droplet `.env`, and deploys. Compose never forwards them, `os.Getenv` returns empty inside the container, and every parent who clicks "Pay online" gets a 500 from `payments.go:136` saying the key is unset while `.env` visibly contains it. This is the `CLAUDE.md` "fail safe on config" invariant firing exactly as documented.
**Fix:** **product decision first.** If online payment is meant to be live, forward all eight in `docker-compose.yml` and add them to the root `.env.example` with empty values and comments. If it is not meant to be live yet, say so in the root `.env.example` and leave the vars unforwarded — the defect is the disagreement between the two files, not the absence of the values.
**Not a vulnerability:** both webhook paths fail *closed* — `payments.go:189` and `:334` log and reject when the signature secret is absent, so an unsigned webhook cannot mark an invoice Paid.
**Accept:** the two `.env.example` files agree, and whichever state is chosen is the state the deployment actually produces.

### T17. `UPLOAD_STORE` / `S3_ENDPOINT` unforwarded — payment proofs sit outside the backup
**Files:** `backend/internal/handlers/uploads.go:50` (`UPLOAD_STORE` gate), `:55` (`S3_ENDPOINT`), `:62` (the warning that never fires); `docker-compose.yml:76-83`; `backend/.env.example:59-60`.
**Problem:** `InitUploads()` gates on `os.Getenv("UPLOAD_STORE") != "s3"` and reads `os.Getenv("S3_ENDPOINT")`. Neither is forwarded. Compose forwards `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_REGION`, plus `S3_HOST` (`:80`) and `S3_HOST_BUCKET` (`:82`) — those last two being the **s3cmd** names used by `backup.sh`, not the name the Go code reads.
**Failure:** an operator sets `UPLOAD_STORE=s3` and `S3_ENDPOINT=sgp1.digitaloceanspaces.com` per `backend/.env.example`. Neither reaches the container, so `:50` returns early and logs `uploads: local disk` — not even the "endpoint/bucket/key not set" warning at `:62`, because the gate never opens. Payment proofs stay on the droplet-local `./uploads` bind mount, which is outside the nightly Postgres dump to Spaces and therefore has **no offsite copy at all**. Losing the droplet loses every uploaded payment proof, silently, while the config reads as though S3 is on.
**Fix:** forward `UPLOAD_STORE` and `S3_ENDPOINT` in `docker-compose.yml`, and add both to the root `.env.example`. Reconcile the naming so the Go code and `backup.sh` are not reading differently-named variables for the same endpoint.
**Accept:** with `UPLOAD_STORE=s3` set, the container logs S3 mode; with it unset or misconfigured, the warning at `:62` actually fires.

---

## P2 — latent risks

### T18. Login and API rate limiting are bypassable with a client-supplied header
**Files:** `backend/internal/core/middleware.go:93-95` (`RealIP`), `:99-103` (`X-Forwarded-For`), used by `RateLimitLogin:113` and `RateLimitAPI:146`; `docker-compose.yml:29`; `infra/Caddyfile.recommended:38-40`.
**Problem:** `RealIP` trusts `X-Real-IP` whenever `r.RemoteAddr` is loopback or private. `AI_DOCS/auth-and-tenancy.md:128-131` justifies this on the grounds that an attacker reaching the app directly cannot spoof it — but the app is *only* reachable through the proxy (`127.0.0.1:8080:8080`), so `RemoteAddr` is **always** loopback and `isTrustedProxy` is **always** true. The in-repo Caddyfile is a bare `reverse_proxy localhost:8080` with no `header_up` directives, so it neither sets nor strips the header and a client-supplied value is forwarded verbatim. `X-Forwarded-For` has the same problem — Caddy appends to the incoming chain and `:99-103` takes the left-most, attacker-controlled entry.
**Failure:** an attacker POSTs `/api/auth/login` with `X-Real-IP: 1.2.3.<n>` incrementing per request. Every request lands in a fresh bucket, so neither the 5/min login limiter nor the 60–120/min API limiter ever fires. The per-account lockout (`auth/auth.go:146-160`) still caps guessing against any one account, so the damage is: unmetered account enumeration, lockout-DoS (5 requests locks any known account for 15 minutes, repeatable across the whole roster), unmetered `/api/forgot-password` and `/api/resend-verification` — mail-bombing plus Resend free-tier exhaustion — and unmetered load on `/api/snapshot`.
**Fix:** have Caddy overwrite both headers with the true remote address (`header_up X-Real-IP {remote_host}`, and replace rather than append `X-Forwarded-For`), so the app's trust is well-founded. This is a Caddyfile change, not a Go one.
**Verify first:** this is the one finding that could not be confirmed from the repo — it depends on the deployed `/etc/caddy/Caddyfile` matching `infra/Caddyfile.recommended`. Check the droplet before acting.
**Accept:** a request carrying a forged `X-Real-IP` is rate-limited against its real source address.

### T19. Blank student name panics the importer and orphans parent accounts
**Files:** `backend/internal/handlers/handlers_import.go:141`.
**Problem:** `parts := strings.Fields(s.Name)` followed immediately by `firstName := parts[0]`, with the length check guarding only `parts[1:]`. A name that is empty or whitespace-only yields a zero-length slice and panics. The import does not run in a transaction.
**Failure:** the panic aborts the request mid-import. Parent accounts created before it survive, so the operator fixes nothing, retries the same file, and panics on the same row again — accumulating orphaned parents on each attempt. With T2 unfixed, a panic on a background path is not contained at all.
**Fix:** guard `len(parts) == 0` before indexing, and wrap the import in a transaction so a mid-file failure rolls back cleanly.
**Accept:** importing a row with a blank name returns a validation error naming the row, and creates nothing.

### T20. Optimistic rollback replaces the whole array, discarding concurrent updates
**Files:** `frontend/js/modules/attendance.js:734`, and the same shape at `:501`, `:858`, `:893`, `:975`, `:1009`; `frontend/js/store.js:126-131`; `frontend/js/api.js:306`.
**Problem:** each site captures `var prevAtt = state.attendance;` *before* the POST and, in the `.catch`, does `App.Store.set({ attendance: prevAtt })`. `App.Store.set` is `Object.assign(_state, deepClone(patch))` — whole-key replacement, not a merge — so the rollback restores the array as it looked before the request rather than "the current array minus my optimistic row". `api.js:306` calls `loadSnapshot()` on **every** WebSocket `CHECK_IN`/`CHECK_OUT`, so in a centre running a kiosk the window is hit routinely.
**Failure:** an admin marks staff member S absent. While that POST is in flight a kiosk scan broadcasts `CHECK_IN`, `loadSnapshot()` lands and replaces `attendance` with the server's copy including three new scans. S's POST then fails, the `.catch` writes `prevAtt` back, and the three scanned check-ins vanish from the admin's screen until the next snapshot.
**Fix:** roll back by re-reading current state and removing the optimistic row — the pattern the success branch at `:730` already uses (`App.Store.get().attendance.map(...)`).
**Accept:** `node --check` passes; a failed write during a concurrent snapshot leaves the snapshot's rows intact.

### T21. The WebSocket is never torn down — each login in a tab adds another socket
**Files:** `frontend/js/api.js:271-321` (`connectWS`), `:319` (reconnect), `:264-268` (`_handle401`); `frontend/js/main.js:232` (connect), `:484` (logout).
**Problem:** `connectWS()` keeps no handle to the socket it creates, has no idempotence guard, and `ws.onclose` unconditionally schedules a reconnect after 5s. Nothing anywhere calls `close()`. Logout is `App.Api.logout().then(() => App.Login.show())` — it shows the login screen *without reloading the page*, and `_handle401` does the same.
**Failure:** on the shared front-desk browser, an admin logs in (socket A). She logs out; the page is not reloaded, so A's `onclose` starts a reconnect loop nothing can stop. A teacher logs in in the same tab and `connectWS()` opens socket B. There are now two `onmessage` handlers: each student check-in produces two toasts and two `loadSnapshot()` round trips, and the count grows by one per login cycle for the life of the tab.
**Fix:** keep a module-level handle, `close()` it on logout and on 401, and guard `connectWS` against opening a second socket.
**Accept:** `node --check` passes; after three logout/login cycles in one tab, a single check-in produces one toast.

### T22. Cascade plan-delete is still not atomic
**Files:** `backend/internal/handlers/handlers_catalog.go:135` (category), `:151` (plans). Reference pattern: `handlers_families.go` (`BeginTx` plus deferred `Rollback`).
**Problem:** residual from the previous review round. Tenant scoping and error logging both landed — `:151` now carries `tw` and logs through `core.LogFromReq(r)` — but `grep -c BeginTx handlers_catalog.go` returns **0**. The two statements still run outside any transaction.
**Failure:** a crash or connection loss between them leaves live plans under a deleted category.
**Fix:** wrap both in one transaction.
**Accept:** `grep -c BeginTx backend/internal/handlers/handlers_catalog.go` returns at least 1; a forced failure on the plan update leaves the category undeleted.

### T23. `enrolled_classes` is not deduplicated, so a repeated id bills twice
**Files:** `backend/internal/jobs/cron.go:484` (`models.ParseArr`); `backend/internal/handlers/handlers_students.go:443` (writes the client array verbatim via `models.JSONArr`); `store.SyncEnrollments` (dedups into a `want` map).
**Problem:** the invoice path does not deduplicate; the enrolments path does. The two therefore disagree.
**Failure:** a repeated class id in a student PUT body bills that class twice on the monthly invoice while the enrollments table records it once, with no error anywhere. The current UI does not produce such an array, which is why this is P2 rather than P0.
**Fix:** deduplicate on read in the cron, or reject duplicates on write in `handlers_students.go:443`. Prefer rejecting on write — it keeps the stored data honest.
**Accept:** a PUT containing a duplicated class id is either rejected or stored deduplicated; the invoice and `enrollments` agree.

---

## P3 — hygiene / consistency

### T24. Sibling-invoice family picker matches every blank-contact student
**Files:** `frontend/js/modules/billing.js:1602` (`_updateSiblingChildren`), select at `:1321`, guard at `:1644`. Helper: `App.Utils.childrenOf` (`js/utils.js:387-390`).
**Problem:** the raw comparison the `childrenOf` helper exists to prevent — `students.filter(function(s) { return s.contact === email; })` — with no empty-string guard. The `<select>` carries `<option value="">Select family...</option>` and `onchange` fires when the admin returns to it.
**Failure:** an admin opens Create Invoice → Sibling, picks a family, changes her mind and re-selects the placeholder. `email` is `''`, so the filter matches all 21 production students with a blank `contact`; the list is rebuilt with those 21 names, all pre-checked, and `_updateSiblingTotal()` prices them as one household. Bounded — `_doSiblingInvoice` guards on empty email at `:1644`, so no invoice can be created from that state. The damage is 21 unrelated children displayed as one family with a wrong total.
**Fix:** use `App.Utils.childrenOf`, which already guards the empty case.
**Accept:** re-selecting the placeholder clears the child list rather than filling it.

### T25. Announcement ownership is keyed on a display name, not a staff id
**Files:** `frontend/js/modules/communication.js:198` (`byline`), `:250` (`createdBy`), `:83` (comparison), `:273-276` (`_getTeacherName` fallback); `frontend/js/modules/notifs.js:176`.
**Problem:** `createdBy` is written as the teacher's display name. `notifs.js:176` compares it against `App.currentTeacher`, which is a staff *id*; `communication.js:83` compares it against the name.
**Failure:** the "N announcements pending approval" notification can never fire, because an id is compared to a name. Two teachers sharing a `fullName` see each other's pending submissions — as does everyone, whenever `_getTeacherName()` falls back to the literal `'Teacher'`.
**Fix:** write the staff id as `createdBy` and compare on it in both consumers. Keep the display name as a separate presentational field if the byline needs it.
**Accept:** `node --check` passes; the pending-approval count renders for the submitting teacher.

### T26. `hasUnpaidMonthly` joins students without the soft-delete filter
**Files:** `backend/internal/notify/checkin_notify.go:105-109`; student soft-delete at `handlers_students.go:505-517`.
**Problem:** the join carries no `deleted_at IS NULL`, and student soft-delete does not cascade to invoices.
**Failure:** a departed child's unpaid Monthly invoice survives, so the check returns true indefinitely and permanently mutes check-in push and email for that parent's remaining children. Recoverable rather than silent — the admin invoice list (`handlers_invoices.go:36`) does not join students, so the orphaned row is still visible and payable.
**Fix:** add `deleted_at IS NULL` to the students join.
**Accept:** soft-deleting a student with an unpaid invoice does not mute notifications for their siblings.

### T27. Test cleanup carries a hardcoded seed allowlist and discards its results
**Files:** `backend/internal/handlers/feature_flow_test.go:76`; leftover class row at `feature_rates_test.go` (~`:336`).
**Problem:** residual from the previous round. The FK poisoning was fixed — the class reference is now broken before the DELETE, with a comment naming the failure mode — but the allowlist is still a hardcoded set of seed ids, extended by hand for migrations 0057, 0058 and 0060, and the `db.Exec` results are still discarded.
**Failure:** any future seeded plan not added to the list is silently deleted by test setup.
**Fix:** derive the allowlist from the seed data rather than hardcoding it, and check the cleanup's error.
**Accept:** adding a new seeded pricing row does not require editing the test allowlist.

### T28. Two documentation corrections found while verifying
**Files:** `CLAUDE.md`, `AI_DOCS/jobs-and-outbound.md`.
**Problem and fix:**
1. `CLAUDE.md` gives the frontend test command as `TZ=Asia/Kuala_Lumpur node --test frontend/tests/unit/`. The directory form fails on Node 22 with `MODULE_NOT_FOUND`. Correct it to the glob form: `frontend/tests/unit/*.test.mjs`.
2. `AI_DOCS/jobs-and-outbound.md` overstates the email retry schedule by one tier. With `emailMaxAttempts = 5` and the guard written as `if nextAttempts >= emailMaxAttempts`, `emailRetryDelays[4]` (12h) is never reached — the real schedule is 1m, 5m, 30m, 2h.
**Accept:** the documented command runs; the documented retry schedule matches the code.

---

## Claims rejected

Recorded so a later review does not re-raise them. Both were reported during this
review and did not survive verification.

**Referral endpoint missing its role gate.** It has one. `handlers_referrals.go:174`
returns 403 on `c != nil && c.Role == "parent" && !strings.EqualFold(contact, c.Email)`,
immediately after the family lookup.

**`sibling_discount` stored as a percent, read as ringgit.** Not supported.
`handlers_invoices.go:138` enforces only non-negative — the 0–100 bound directly above
it belongs to `DiscountPct`, a different field. The cron sets
`siblingDiscount = SiblingMonthlyRM` (`cron.go:523`) and subtracts it as currency
(`:537`). Both sides treat it as ringgit.

---

## Verified clean

Do not "fix" these. Each was checked directly and holds.

**Payments and webhooks.** Both webhook paths are sound: `hmac.Equal` at
`payments.go:266` and `:436`, fail-closed on an unset secret at `:187-192` and
`:332-337`, Stripe's 5-minute replay window at `:425-431`, Billplz's `status <> 'Paid'`
idempotency. No forged or replayed webhook can mark an invoice Paid. Parent ownership
is enforced on all four money routes including both PDF endpoints; totals are
recomputed server-side on every write; no value concatenation into SQL.

**Migrations.** All 61 files present at review time verified append-only at blob level
across full git history, using `--follow` — which surfaced a directory rename that a
naive check would have misread as edits. Every file has exactly one distinct blob hash.
No duplicate prefixes, no gaps in 0001–0061. (`0062_normalise_classrooms.sql` landed
while this review was running and was not covered by that check.) Migration `0061`'s
`DROP CONSTRAINT` was confirmed
against a real `postgres:16-alpine` to match Postgres's 63-character identifier
truncation exactly, so it is not a silent no-op.

**Monthly invoice idempotency.** Holds through three independent layers: the `period`
preload (`loadExistingMonthlyInvoiceStudentIDs`), the partial unique index
`idx_invoices_monthly_unique`, and `ON CONFLICT DO NOTHING` at `:579` with the
`RowsAffected() == 0` branch correctly skipping both the count and the email. A
same-month re-run creates nothing.

**Advisory locks.** Cron-versus-manual is genuinely covered — both paths use
`core.AdvisoryLockKey("monthly_cron")` on a dedicated `*sql.Conn`, not from the pool.
Attendance upsert holds `pg_advisory_xact_lock` inside its transaction; replacement
credit redemption holds one and reads the balance in the same transaction. No
double-spend.

**Dates and enrolment windows.** `store.EnrollmentWindowsIn` uses the correct half-open
`[started_on, ended_on)` predicate and is correct at both month edges; `catalog_price.go:158`
now matches it. `Month()+1` normalises correctly through December in all three
generators. No `time.UTC` date arithmetic anywhere in the reviewed slices.

**SQL translation.** The `?` → `$N` rewriter in `store/db.go` was checked by AST scan of
every SQL string literal in non-test Go files: zero literals with an odd apostrophe
count, so no fragment can leave the quote tracker inverted — including under the
`+tw+` concatenation pattern and the `teacher_ids LIKE '%"'||?||'"%'` shape. No jsonb
`?` operators exist that the rewriter would eat.

**Frontend XSS.** Essentially clean. `esc()` (`js/utils.js:413`) escapes both `"` and
`'`, so quoted attribute contexts **are** protected — the caveat in `CLAUDE.md` about
attributes is stricter than the code requires. All 49 JS-string-literal interpolations
inside `on*` handlers carry a system-generated id or an enumerated constant. Toasts
build with `textContent`; `showConfirm` escapes its inputs; `sw.js` never caches
`/api/*`.

**Auth hygiene.** No bare `c.Role == "admin"` comparison exists anywhere. Algorithm
confusion is blocked by the `*jwt.SigningMethodHMAC` type assertion in both
`JWTMiddleware` and `HandleWS`. Refresh cookies are SameSite=Strict, path-scoped,
SHA-256 at rest, and rotate with family-wide reuse detection. MFA has no skippable
step, and its single-use intermediate token makes TOTP replay unexploitable.
`HandleChangePassword` correctly revokes the refresh family and bumps
`sessions_invalid_before`.

**iCal.** The token is `hex(HMAC-SHA256(JWT_SECRET, "ical:<userID>:<email>:<version>"))`,
compared with `hmac.Equal`, per-user, and revocable via `users.ical_token_version`
without rotating the global secret. The feed is scoped to the token holder's own
children; an admin's or teacher's token yields an empty calendar rather than a
tenant-wide one.

**Uploads.** The serve path's charset whitelist excludes `/`, so `filepath.Join`
traversal is unreachable. Both upload and serve verify tenant *and* parent ownership
of the invoice, and magic-byte MIME validation backs the extension check.

**Snapshot cache.** Key is `tenantID|role|email`; the endpoint reads no query params,
headers or body into the response, so nothing varies the body without varying the key.
Tenant invalidation prefixes with `|`, so tenant 1 does not match tenant 12.
Invalidation covers DELETE.

**Infrastructure.** Only `api` is published, host-scoped to `127.0.0.1`; `analytics`
and `postgres` use `expose:` only. The analytics service holds no database access and
no SQL — it receives already-fetched rows — so it has no injection surface. No secrets
in any tracked file.

---

## Caveats and coverage limits

- **The reachable Postgres is behind the repo.** It reports `max(version) = 0046` while
  the repo is at 0061, so every claim about migrations 0047 onward is static analysis,
  not verified against a database that has applied them.
- **T18 depends on the deployed Caddyfile** matching `infra/Caddyfile.recommended`,
  which cannot be checked from the repo. Verify on the droplet before acting.
- **T4 and T9 could not be measured for production incidence** — the reachable database
  holds seed data only, so how often they have actually fired is unknown.
- **`internal/store/`, `internal/jobs/` and `internal/pdf/`** were outside the
  `ScopeTenant` sweep that covered `internal/handlers/`. A missing tenant filter in
  those packages would not have been caught.
- **CI does not run the frontend unit tests.** `.github/workflows/ci.yml` runs
  `node --check` only; the 61 assertions in `frontend/tests/unit/` must be run locally.
  Note also that `security.yml:88` pins `securego/gosec@master` — a mutable third-party
  ref in a workflow holding `security-events: write`, while `trivy-action` and
  `appleboy/ssh-action` in the same repo are SHA-pinned.

## Not a bug, but bears on current work

The monthly cron still joins `pricing_tiers` (`cron.go:345`), not the
`pricing_categories` / `pricing_plans` catalogue that the admin pricing screen writes
to. `store/catalog_price.go:16` documents this as the pending switchover and states the
intent plainly — there is deliberately one implementation of "what does this student
cost", and the cron must adopt it. Until that lands, **editing a price in the new
pricing screen does not change what any parent is invoiced.**

T5 above is the same class of problem inside the cancellation path: two answers to
"what was this worth", one of which ignores the date. Fixing T5 and completing the
catalogue switchover are the same argument applied twice.
