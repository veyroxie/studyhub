# Session model hardening — plan

Status: proposed, 2026-09-01. Supersedes nothing; complements
`V2_REBUILD_PLAN.md` section 8.7 and the `NEW-31`/`NEW-32` entries in
`V2_IDEAS.md`.

Origin: `/code-review high` over `dad555c..94d8b3e` plus manual verification.
Every claim below carries a `file:line` so it can be re-checked; a claim with
no citation is a judgement call, marked as such.

---

## 1. Why this exists

Migration `0046` shipped dated schedule changes so a class can move to a new
day from a chosen date without rewriting the past. It works for the case it was
built for. But the review found four defects in one six-line guard, and all
four trace to a single representation choice.

`class_schedule_history` stores **the slot that applied BEFORE `changed_on`**.
A row is therefore only interpretable relative to the current `classes` row.
Consequences, all observed rather than theorised:

- Inserting a change dated *earlier* than an existing one is undefined, because
  the `classes` row already reflects the later edit. Hence a guard
  (`handlers_classes.go:92-97`) that bans it.
- The guard's advice ("undo the later change") names an operation that does not
  exist anywhere in the codebase.
- A self-undone row (edit back with the same effective date) is byte-identical
  to a live one, so it silently blocks all earlier-dated edits forever.
- The guard reads then writes without a transaction.

The alternative shape — store **the slot that applies FROM a date** — makes all
four disappear rather than requiring four patches. It also returns `time` and
`end_time`, not just `day`, which is precisely what `NEW-31` needs.

This was a known trade at the time: the inverted form was chosen so existing
readers kept working with no backfill. That was a defensible call for shipping
in a day. It is not the right long-term shape.

---

## 2. Full inventory

Severity: **S1** = wrong money or lost data; **S2** = wrong behaviour a user
will notice; **S3** = latent, edge-case, or hygiene.

**Closed so far (2026-09-01).** Keep this list current; the group tables below
describe the defect, not its status.

| Item | Commit | Note |
|---|---|---|
| A1-A4 | `67357ec` | All four guards on `store.CountRow`; they now 500 instead of passing silently |
| E1 | `09759aa` | `pricing_tiers` PUT takes pointer fields. **E2 (the class PUT) is still open** |
| G1 | `67357ec` | Stale-count assertions fixed via the `countRows` helper. **G2 is still open** — `email_flow_test.go:304,318,529` and `feature_enrollments_test.go:115` still drop `Scan` errors |
| K2 | `09759aa` | Fixed in the one test that mutates a seeded row. **K1 is still open** — the harness still does not reset seeded tables |
| B1 | `3eb836a` | Schedule history takes its tenant from the class row. **B2/B3 reassessed, see below** |
| C1 | `ca0c936` | Cancellation on a move destination now visible; `Billable()` untouched on purpose |
| C3 / `NEW-31` | Phase 3 | `store.SessionRateOn` prices a session at the duration it actually ran; iCal stamps each event with its own date's times. A month whose sessions differ in length is FLAGGED, not blended — `Qty x UnitPrice` cannot express it |
| 2.3 differ | `36bd2de` | Mutation-checked; run against a production copy before deploying 0047 |
| C2 | `2967f4b` | Move onto an occupied date rejected with a 409 |
| D1/D2 | `97e8b1d` | `App.Utils.runsOnDate` is now the one client-side predicate; six divergent day checks removed |
| G2 | `2f78e9d` | Remaining dropped `Scan` errors fixed in the email and enrolment tests |
| K1 | `2f78e9d` | Harness resets seeded tables and UPSERTS the pricing matrix (migration data cannot be reset by deletion — deleting it broke every rate test) |
| H1 | `a5b0f87` | Batch cancellation reports per-class outcomes instead of implying nothing happened |
| Phase 4 rosters | `745d16f` | `App.Utils.enrolledOn`; unenrolling no longer hides past attendance |
| Seed safety | `745d16f` | Demo data now needs `SEED_DEMO_DATA=1` everywhere — the old guard was fail-open because `APP_ENV` defaults to development |
| E2 | Phase 1.4b | Class PUT decodes OVER the stored row, so omitted fields keep their values |
| B2/B3 | closed by evidence | Nothing in the codebase creates a superadmin account — the role is only ever checked, never assigned. Confirmed with Ely 2026-09-01 that the centre runs on plain admin accounts, so the tenancy group was theoretical. The B1 fix stands as correctness. |

**B2/B3 reassessed 2026-09-01 (downgraded).** The ~20 `store.TenantID(c)`
write sites do not share one failure mode. A site whose lookup is scoped with a
literal `tenant_id=?` simply returns 404 for a superadmin — a lockout, and safe.
Only a site that *reads* with `ScopeTenant` (a no-op for superadmin) and then
*writes* with `TenantID(c)` orphans a row. `handlers_session_moves.go` is the
lockout shape, not the corrupting one. So B2 is S3 (a superadmin cannot create
session moves), and B3 is a convention to apply as those handlers are touched,
not a 20-site sweep to schedule.

Everything else in this document is open. Nothing here is deployed until a
`make ship` restarts the API — check `uptime_sec` on `/api/health`.

Naming: the group codes below (`A1`, `B2`, ...) are local to this document.
They are **not** the `F1`-`F8` items of `V2_REBUILD_PLAN.md` section 8.7 —
those are always cited with a file reference, e.g. "F2
(`V2_REBUILD_PLAN.md:530`)". Group F here happens to contain rows `F1`/`F2`;
they are unrelated to the plan's F1 (credits-only) and F2 (frozen record).

### Group A — fail-open guards

| # | Site | Severity | What |
|---|---|---|---|
| A1 | `handlers_classes.go:93` | S3 | `Scan` error discarded; on a DB error `later` stays 0 and the out-of-order guard passes silently |
| A2 | `handlers_cancelled.go:63` | S2 | Same shape: cancel-with-live-move guard passes on error |
| A3 | `handlers_session_moves.go:73` | S2 | Same shape: move-on-cancelled-session guard passes on error |
| A4 | `handlers_referrals.go:102` | S3 | Same shape on the paid-invoice recount. **Corrected 2026-09-01 from S1:** the count does not gate the reward — `status='earned'` is set unconditionally and the count is written via `GREATEST(paid_invoice_count, ?)`, so a dropped error just fails to advance it. Wrong, not dangerous. |

Same failure mode as the RM 0 pricing incident (2026-07-31): a failed query
returns an empty answer indistinguishable from a valid one.

### Group B — tenancy

| # | Site | Severity | What |
|---|---|---|---|
| B1 | `handlers_classes.go:93,101` | S1 (conditional) | Uses `store.TenantID(c)` for a WHERE and an INSERT. `TenantID` returns **0** for superadmin (`store/scope.go:11-13`), so a superadmin's schedule change writes `tenant_id = 0` — a row no reader can find, because `classScheduleChanges` scopes to the class row's tenant and `listScheduleChanges` scopes to the caller's. The feature then silently does nothing and the past *is* rewritten. |
| B2 | `handlers_session_moves.go:64,86` | S1 (conditional) | Identical pattern for session moves |
| B3 | codebase-wide | S1 (conditional) | The rule, not the instances: `TenantID(claims)` is correct for reads (cross-tenant visibility) and wrong for writes. A superadmin editing tenant 5's class must write `tenant_id = 5`. |

Conditional on whether anyone actually operates as superadmin. **Verify this
first** — it decides whether B is urgent or theoretical. The fix is correct
either way.

### Group C — the session expander

| # | Site | Severity | What |
|---|---|---|---|
| C1 | `store/sessions.go:87-89` | S2 | Moved-in destinations are appended **unclassified**. A cancellation or holiday on a destination date is silently ignored, so a cancelled relocated session still renders and still bills. |
| C2 | `handlers_session_moves.go:60` | S2 | Only `FromDate == ToDate` is rejected. Moving a Monday session to the *following* Monday puts two sessions of one class on one date. Billing is arguably right (two sessions do happen), but `attendance` is keyed `(person, date, class)` so only one can be recorded, the calendar dedupes to one card, and iCal emits two VEVENTs. The data model cannot represent it — so reject the move rather than try to support it. |
| C3 | `store/sessions.go` | S2 | Resolves `day` only. `time`/`end_time` still come from the current class row, so a schedule change that alters class *length* misprices earlier months and stamps wrong iCal times. This is `NEW-31`. |

### Group D — client-side schedule resolution

One question — "which classes run on date D?" — is answered independently in
roughly a dozen places, each with a different rule:

| Site | Honours `scheduleChanges` | Honours `sessionMoves` |
|---|---|---|
| `calendar.js` week (`classesByDay`) | yes | yes |
| `calendar.js` month (`dayCls`) | yes | yes |
| `calendar.js` `_movedInCards` | yes | yes |
| `attendance.js` `_studentTab` | yes | yes |
| `attendance.js` `_staffTab` | yes | **no** |
| `attendance.js` `_setDate` | yes | **no** |
| `attendance.js` `_markStaff` | yes | yes (fixed `94d8b3e`) |
| `dashboard.js` (5 sites) | yes | **no** |
| `notifs.js:134` | yes | **no** |
| `store/sessions.go` (canonical) | yes | yes |

| # | Severity | What |
|---|---|---|
| D1 | S2 | A rescheduled class still appears under "Today's classes", still fires the not-checked-in teacher alert on the old date, and never appears on the new one |
| D2 | S3 | Extracting one `App.Utils.classesOnDate` fixes today but creates a *seventh* implementation to keep in sync with `classifyOccurrence`. The repo already carries three "keep the two in sync" comments; that pattern is what failed here. |

### Group E — full-row PUT clobber

| # | Site | Severity | What |
|---|---|---|---|
| E1 | `handlers_pricing.go:37` | S1 | `hourly_rate` written from a bare `float64`. A body omitting `hourlyRate` silently writes 0, wiping migration `0045`'s backfill and zeroing the rate F8 billing reads. `p.HourlyRate < 0` does not catch it. |
| E2 | family | S2 | Third instance of this bug shape (`subject` on the class PUT is documented at `calendar.js:1377`). `session_rate`, `level_band` and `monthly_fee_override` have the same exposure. |

### Group F — enrolment dates and billing

| # | Site | Severity | What |
|---|---|---|---|
| F1 | `jobs/session_preview.go:74` | S1 | Reads `s.enrolled_classes` (the JSON column), **not** the `enrollments` table migration `0043` created for exactly this. Mid-month joiners are billed a full month; mid-month leavers are billed as if still enrolled. |
| F2 | — | S1 | Hard blocker for the 8.7 cron switchover. Also the direct answer to Nadine's 31/08 question about students starting halfway through the month. |

### Group K — shared seeded state in tests

Found 2026-09-01 while fixing E1.

| # | Severity | What |
|---|---|---|
| K1 | S2 | `setupFeatureTestApp` resets fifteen tables but **not** the seeded reference data (`pricing_tiers`, `holidays`, `products`). A test that edits a seeded row corrupts every later test in the package, and because the test database is a long-lived container, it stays corrupted across runs — a failure that looks exactly like a flake. |
| K2 | S3 | `t.Cleanup` is the wrong tool for restoring shared rows here: cleanup callbacks run **after** the test's deferred calls, and the harness's `defer cleanup()` closes the DB. A restore registered with `t.Cleanup` runs against a closed connection. Use `defer` registered after the harness's, and check the error. |

Fix direction: either add the seeded tables to the reset list and re-seed, or
have the suite refuse to run against a database it did not create. Until then,
any test mutating seeded data must restore it via `defer` with a checked error.

### Group G — tests

| # | Site | Severity | What |
|---|---|---|---|
| G1 | `feature_sessions_test.go:174,186` | S2 | `Scan` errors discarded. On failure `count` retains the *previous* assertion's value, so the test passes on stale data — an assertion that cannot fail correctly. |
| G2 | family | S3 | Same shape in `email_flow_test.go:304,318,529` and `feature_enrollments_test.go:115` |

### Group H — partial failure

| # | Site | Severity | What |
|---|---|---|---|
| H1 | `attendance.js` `_doCancelClasses` | S2 | N independent POSTs in `Promise.all`; one 409 discards the outcome of siblings whose parents were already notified |

### Group I — the backstop

| # | Severity | What |
|---|---|---|
| I1 | S1 | Tenant isolation rests entirely on reviewer discipline (`CLAUDE.md` invariant; `notes/rls-activation.md`). B1 is a tenancy hole in code written *specifically* to be careful about tenancy. That is the argument for the database enforcing it. |

### Group J — the invoice is not yet a frozen record (F2)

Not a review finding: `V2_REBUILD_PLAN.md:530-537` registered this as **F2,
HIGH, and it is still open**. Recorded here because it addresses the same root
cause as Phase 2 and must be sequenced against it.

| # | Severity | What |
|---|---|---|
| J1 | S1 | Invoice lines carry `Qty x rate` but not the session **dates** they were computed from. Once the cron bills from the schedule, a later schedule edit makes every past month's count unreproducible — a dispute six weeks on cannot be answered. |

F2's remedy is to freeze the actual session dates onto the line item at issue,
the same reasoning as B5 freezing lines. **That is a different fix from Phase 2,
and both are wanted:**

- J1 answers "what did we bill, and for which dates" — the invoice is the record.
- Phase 2 answers "which day did this class meet in August" — the calendar,
  attendance and iCal, which no invoice can answer.

Judgement call: **J1 should land before Phase 2.** It is cheaper, and it is a
safety net that holds even if schedule history is later found to be wrong. Phase
2 without J1 means the correctness of every past invoice depends on the version
table being right; J1 without Phase 2 means past invoices stay answerable while
the calendar is still wrong. J1 is the better first move.

---

## 3. Sequence

Ordered so each phase makes the next safer, and so nothing depends on a
decision that has not been made yet.

### Phase 0 — Make the bad shapes unwriteable (no behaviour change) — DONE 2026-09-01

Small, additive, reversible. Do these first: they are the primitives the rest
of the work will use.

**0.1 `store.CountRow(db, query, args...) (int, error)`**
Fixes A1-A4 as a category rather than four times. The point is not brevity —
it is that you cannot obtain the count without also obtaining the error, so the
safe form becomes the shortest form. Mirror of the reasoning behind
`store.CollectRows` (`store/collect.go:9-19`), which exists for the same reason.

Why not four `if err != nil` blocks: fixes today, not the habit. A fifth guard
written next month has the same 50/50 odds.

**0.2 `countRows(t, db, query, args...) int` test helper**
Fixes G1-G2. Fails the test on a `Scan` error, so a DB assertion cannot be
written in the silently-passing shape.

Verify: `go test ./...` unchanged; deliberately break a guard query and confirm
the test now fails.

### Phase 1 — Correct the live bugs that need no restructure

**1.1 Write-tenant derives from the resource (B1-B3)**
Rule: for a tenant-owned resource, the write tenant comes from the row being
edited, not from the caller's claims. The class row already knows its tenant.

Scope: `handlers_classes.go`, `handlers_session_moves.go`, then a sweep for
`INSERT ... store.TenantID(c)` on tenant-owned tables.

Verify: a superadmin-authenticated PUT against a tenant-1 class writes
`tenant_id = 1`, and the change is visible to a tenant-1 admin. Lock with a test.

**1.2 Classify moved-in dates through the same pipeline (C1)**
Build the complete occurrence set (natural plus moved-in) *first*, then run
every element through `classifyOccurrence`. Cheap, and inside the canonical
expander, so every consumer benefits at once.

**1.3 Reject a move onto an existing occurrence (C2)**
Validation, not support. `attendance` is keyed `(person, date, class)`, so two
sessions of one class on one date is unrepresentable downstream. Reject with a
409 naming the clash.

**1.4 Partial-update PUT for pricing (E1-E2) — DONE 2026-09-01 for `pricing_tiers`; the class PUT (E2) still open**
Pointer fields plus a SET clause built only from present keys. Removes the
family, not the instance. Start with `pricing_tiers` because it is money and
live; the class PUT can follow.

Verify: a PUT omitting `hourlyRate` leaves the stored rate untouched. Test it.

### Phase 1.5 — Freeze session dates on the invoice line (J1 / F2)

**Revised 2026-09-01 after checking the code.** This cannot be done as its own
step: the live cron bills a flat monthly fee (`cron.go:345`) and produces no
session-derived line, so today there is nothing to freeze. The freeze belongs
*inside* the F5 switchover, not before it.

What was independently useful and is DONE: the dry run now reports
`BilledDates` per line -- the dates that produced the charge, moved sessions
listed under their origin to match `Billable()`. Two payoffs: the centre's
comparison gate becomes checkable against the real schedule instead of being a
bare count, and the generator that F5 will build already has the exact data F2
wants frozen.

`InvoiceLineItem` needs no schema change when that lands -- it already carries
`Details []string` alongside `Qty`/`UnitPrice`, and the PDF renders them
(`V2_REBUILD_PLAN.md:436-441`).

Consequence for the sequence: **Phase 2 is no longer gated on this.** The
argument for doing J1 first was that it is a safety net holding even if the
version backfill is wrong. That still holds, but it can only be built with F5,
so Phase 2 now precedes it.

### Phase 2 — Schedule versions (migration `0047`)

The structural fix. Deletes the guard that A1 and B1 live in, and removes C3's
blocker.

**2.1 Table**
`class_schedule_versions(id, tenant_id, class_id, effective_from, day, time,
end_time, created_by, created_on)`. Resolution: the version with the greatest
`effective_from <= date`. The current `classes` row remains the newest version
(or is migrated in as one — see decision D1).

**2.2 Backfill**
Mechanical and verifiable. For snapshot rows `D1 < D2 < ... < Dn`,
`snapshot(Di)` is the slot in force over `[D(i-1), Di)`, and the current
`classes` row is the slot from `Dn` onward.

**2.3 Dry-run differ before cutover — BUILT 2026-09-01**
`TestScheduleBackfill_MatchesLegacyResolver` reimplements 0046's rule and
asserts the migrated versions answer identically for every class, weekly across
two years back to one year forward. Point it at a restored production copy
before deploying 0047:

    TEST_DATABASE_URL=postgres://...prod_copy... go test ./internal/handlers/ \
      -run TestScheduleBackfill_MatchesLegacyResolver -count=1

A divergence names the class and the date. Mutation-checked: replacing the
backfill's `LAG` with the row's own `changed_on` produces 79 reported
divergences, so the test genuinely fails on a wrong backfill rather than
passing vacuously. It also seeds its own three-change class, so CI exercises
the `LAG` path and not just the no-history case.

**2.3 (original) Dry-run differ before cutover**
A command that, for every existing class and every date in a wide window,
compares the old resolver's answer to the new one and reports any divergence.
Non-negotiable: this rewrites the timeline that attendance and invoices are
date-anchored to.

**2.4 Resolvers**
Go and JS both return `{day, time, endTime}`. Delete `weekdayOn`,
`App.Utils.scheduleOn`, the out-of-order guard, `errScheduleOutOfOrder`, its
409, and the test that locks it.

**2.5 Edit and delete a version**
Now a natural operation, because rows are self-describing. This is what makes
the error message from `90a79b6` honest — or rather, makes it unnecessary.

Migration `0046` has shipped: never edit it. `0047` only.

### Phase 3 — Duration-aware pricing and historical iCal times — DONE 2026-09-01

Depends on Phase 2's resolver.

- 3.1 `ClassSession` carries resolved `time`/`endTime`
- 3.2 `session_preview` prices from resolved duration — closes `NEW-31`
- 3.3 iCal stamps historical times — closes the documented gap in
  `AI_DOCS/calendar-and-sessions.md`

### Phase 4 — Enrolment dates in billing (Group F) — BILLING HALF DONE 2026-09-02

Done: the dry run reads `store.EnrollmentWindowsIn` instead of
`students.enrolled_classes`, so a joiner is billed from their start date, a
leaver to their last day, and someone who never started gets no invoice.
Enrolment is HALF-OPEN — `ended_on` is not billed — so removing a student on
the 15th bills through the 14th rather than charging for a day they had left.

**Drift guard, found by the tests rather than designed in.** Switching the read
path created a new way to bill someone nothing in silence: a student still
carrying classes in the legacy JSON with NO enrolment rows produces no lines
and quietly drops off the invoice run. That is the RM 0 pricing incident's
shape. Such a student is now FLAGGED. The distinction that matters and that the
first version got wrong: no windows *this month* is a legitimate skip (joined
later, left earlier); no enrolment records *at all* is drift. They look
identical on a report and mean opposite things.

Still open: rosters. `attendance.js` and the other readers still use the JSON
list, so unenrolling a student with real history hides their past attendance
from the class roster. Needs enrolment windows in the snapshot.

### Phase 4 (original) — Enrolment dates in billing (Group F)

Hard blocker for the 8.7 switchover.

- 4.1 `session_preview` reads `enrollments` with
  `started_on <= D AND (ended_on IS NULL OR ended_on > D)` instead of the JSON
- 4.2 Prorate mid-month joiners and leavers — **needs decision D2**
- 4.3 Sweep remaining `enrolled_classes` readers; retire the JSON column last

### Phase 5 — One occurrence source for the frontend (D1-D2)

`GET /api/sessions?from&to` returning occurrences already classified
`scheduled` / `cancelled` / `moved_out` / `moved_in` / `holiday`. Every module
renders from it; the client-side resolvers get deleted as each module moves.

Land it *alongside* the existing snapshot payload and migrate module by module.
Drop `scheduleChanges` and `sessionMoves` from the payload last. Big-bang swap
here would put every screen at risk at once for no benefit.

Judgement call: this is the largest item and the least urgent. `V2_IDEAS.md`
already sequences F-tier work alongside the invoice UI rebuild; this belongs
there rather than as its own project.

### Phase 6 — RLS activation (I1)

Per `notes/rls-activation.md`. Its own change, small steps, explicit
confirmation at each: a mistake locks the app out of its own data. Needs a
non-superuser DB role, `docker-compose.yml`, and droplet `.env` changes.

### Phase 7 — Batch cancellation (H1)

`POST /api/cancelled-classes/batch` returning per-item results, so partial
success is representable. Cheaper after Phase 5, because the UI stops having to
mirror backend eligibility rules to guess what will be rejected.

### Cross-cutting — a drift tripwire

The chosen architecture's failure mode is silent: new code comparing `c.day`
directly just ignores history. Add a CI check that fails on new bare
`c.day ===` comparisons in date-anchored modules. Documentation does not fail
builds. Retire it once Phase 5 removes client-side resolution entirely.

---

## 4. Decisions needed

| # | Question | Recommendation |
|---|---|---|
| D1 | Replace `class_schedule_history` outright, or shadow it during transition? | **Replace.** Shadowing doubles the write paths, which is the exact class of problem Phase 5 exists to remove. Safety comes from the 2.3 differ, not from keeping two tables. |
| D2 | Does a mid-month joiner prorate by session, or pay the full month? | **DECIDED 2026-09-01 (Ely): bill only the sessions inside the student's enrolment window.** A joiner pays from their start date, a leaver up to their end date. A student who was on the schedule but never started owes nothing — with no billable lines the F5 rules already skip the invoice entirely, so this needs no new code beyond Phase 4 reading `enrollments` instead of the JSON. An *absence* does **not** reduce the bill (confirmed 2026-09-01): the count is of sessions **scheduled** inside the enrolment window, minus holidays, and the replacement credit remains the sole compensation. This is F1 (`V2_REBUILD_PLAN.md:499`) unchanged, and it is what makes advance billing on the 1st possible at all. |
| D3 | RLS now or after Phase 4? | After. It is a backstop, not a fix, and Phase 1.1 closes the actual hole. |
| D4 | Superadmin — does anyone operate as one in production? | Answer decides whether Group B is urgent or theoretical. Check before scheduling Phase 1. |

---

## 5. Definition of done

- No production guard passes silently on a query error
- A superadmin write lands in the resource's tenant, provably
- One resolver per language, both returning day, time and end time
- Session pricing derives duration and enrolment window from dated records
- One implementation of "which classes run on date D" reachable from the client
- `0046`, `0047` and this document agree with `AI_DOCS/calendar-and-sessions.md`

## 6. Alignment with `V2_REBUILD_PLAN.md`

Checked 2026-09-01. Most of this plan is not new work — it is the unfinished
half of things 8.7 already registered.

| This plan | v2 plan | Status |
|---|---|---|
| Phase 4 (enrolment dates in billing) | Risk 3 (`:470-473`) and B6 stage 2 (`:624-632`) | Same item. The plan calls the join table "a prerequisite, not an optional cleanup". Stage 1 shipped; readers still on the JSON. |
| Group C3 / `NEW-31` (duration-blind pricing) | F8 original reasoning (`:663-669`) | **Predicted.** F8 chose per-session storage precisely so that "editing a class's times silently changes its price" could not happen. See the tension below. |
| Groups C1-C2 (expander classification) | F6 (`:585-590`), shipped | Extension of a shipped item. |
| Phase 5 (server-owned occurrences) | "one function, three callers" (`:456-459`) | Same principle, extended to the client. |
| Phase 6 (RLS) | `notes/rls-activation.md` | Pre-existing. |
| Group J | F2 (`:530-537`), **still open** | Now sequenced as Phase 1.5. |
| Groups A, B, E, G, H | absent | Genuinely new; found by the 01/09 review. |

**Tension to resolve before Phase 3.** F8's original reasoning rejected
rate-per-hour x duration exactly because deriving duration from `time`/`end_time`
lets a schedule edit rewrite a past price. As built, migration `0045` shipped
**both** an hourly matrix (`pricing_tiers.hourly_rate`) and a per-session
override (`classes.session_rate`), so any class priced off the matrix still
derives duration and still has the bug F8 wrote itself to avoid. Phase 3 either
resolves duration through schedule history (Phase 2's resolver) or moves those
classes onto stored per-session rates. Decide which before writing it.

**Overlap to expect.** `handlers_pricing.go:37` (Group E1) is listed in F7's
change surface as "becomes editing a session rate", so that file is slated for
rework during `pricing_tiers` retirement. Fixing the clobber now is still right —
it is live and it is money — and the partial-update pattern carries over.

## 6.4 Backups — findings 2026-09-01

Checked on the droplet while answering a question about data safety, not part
of the review:

- Nightly `backup.sh` cron IS installed and working (dumps present for every
  recent night, pruning correctly, restores cleanly).
- **RESOLVED 2026-09-02:** off-site upload now works to Cloudflare R2 (free
  tier). Verified end to end with `make backup-verify-remote`: the newest
  object downloads and restores with data in every core table (77 students,
  100 invoices, 206 attendance, 42 classes, 51 users). Remaining gap: the
  hourly self-check alerts when S3 is UNCONFIGURED but not when a configured
  upload FAILS (expired token, revoked key). Close that by having backup.sh
  write a success marker and the self-check watch its freshness.
- **Off-site upload was never enabled.** Every log line ends "S3 not configured
  — local backup only", so all backups lived on the droplet they protect.
  `docker-compose.yml` already forwards `S3_BUCKET`/`S3_ACCESS_KEY`/
  `S3_SECRET_KEY` and `s3cmd` is in the runtime image, so the only missing
  piece is the values in the droplet's `.env`. **Owner: Ely** (credentials).
- `backup_verify.sh` cron was never installed — INSTALLED 2026-09-01, and run
  once manually: the newest dump restores cleanly (51 users, 77 students, 100
  invoices, 0 orphans).
- The self-check only tested backup FRESHNESS, which is why nothing alerted.
  It now also alerts when `S3_BUCKET` is unset in production, so this cannot
  go quiet again.

## 6.45 Two outages nothing in the codebase could have found

Both surfaced on 2026-09-01 by looking at the RUNNING system, not by reading
code. Recording them because the lesson generalises: every audit to date --
including the multi-agent review that produced this document -- read source and
reasoned about it, and neither of these is visible that way.

- **WebSockets dead since June** (`89249be`). Every upgrade failed with
  "response does not implement http.Hijacker" because `statusRecorder` wrapped
  the writer and hid the interface. `AI_DOCS/jobs-and-outbound.md` describes
  the WebSocket auth, per-parent event scoping and write deadlines correctly --
  documentation of a feature that had not run in production for months.
  `V2_IDEAS.md` C2 asks for *better auth* on the upgrade, taking for granted
  that upgrades happen. Found in the production log, not in a file.
- **Backups never left the droplet** (`b8392b5`). Script correct, cron
  installed, freshness self-check passing, dumps restore cleanly -- and every
  run logged "S3 not configured". Found by reading the log.

Practical consequence for this plan: after each phase deploys, look at what
production is DOING (logs, `/api/health`, a real page) rather than only at
whether the tests pass. Green tests said nothing about either of these.

## 6.5 Follow-on feature: the student summary (requested 2026-09-01)

Ely's decision on mixed-duration months settles a wider principle: **the invoice
stays simple and predictable, and the detail lives somewhere else.** Billing
follows the ordinary weekly schedule; replacements, credits and schedule changes
must not make a parent's bill harder to read.

The place for that detail is a per-student summary, visible to both the centre
and the parent, showing:

- replacement credits earned and used, with what each came from
- self-study hours used against the package, and any overflow
- schedule changes and rescheduled sessions affecting that student

Not a defect, so it is not in the inventory. Sequence it AFTER the cron
switchover: the same resolved-session data feeds it, and building it first would
mean building it twice. It also answers the credits questions the centre has
raised repeatedly (see `studyhub-open-questions-for-nadine`).

## 7. What is deliberately not here

- Materialising a sessions table. The exception-row model
  (`AI_DOCS/calendar-and-sessions.md`) is coherent and works; replacing it is a
  rewrite with no user-visible gain.
- Splitting a class into two on a schedule change (the calendar-app "this and
  following events" pattern). A StudyHub class is a foreign-key hub for
  attendance, enrolments, rates and cancellations; splitting fragments every one
  of those histories across two IDs.
