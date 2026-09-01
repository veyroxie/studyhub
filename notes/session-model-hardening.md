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
| A4 | `handlers_referrals.go:102` | S1 | Same shape on a **paid-invoice count** that gates a referral reward |

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

### Phase 0 — Make the bad shapes unwriteable (no behaviour change)

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

**1.4 Partial-update PUT for pricing (E1-E2)**
Pointer fields plus a SET clause built only from present keys. Removes the
family, not the instance. Start with `pricing_tiers` because it is money and
live; the class PUT can follow.

Verify: a PUT omitting `hourlyRate` leaves the stored rate untouched. Test it.

### Phase 1.5 — Freeze session dates on the invoice line (J1 / F2)

Before Phase 2, not after. The invoice line item gains the session dates it was
computed from, so a past month is answerable from the record rather than by
replaying a schedule that may since have changed. `InvoiceLineItem` already
carries `Qty` and `UnitPrice` and the PDF already renders them
(`V2_REBUILD_PLAN.md:436-441`), so this is a descriptor change, not a schema
redesign.

Why first: it is the cheaper of the two history fixes and it holds even if the
version table is later found to have a backfill error. It also means Phase 2 can
be shipped without every past invoice depending on it being correct.

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

**2.3 Dry-run differ before cutover**
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

### Phase 3 — Duration-aware pricing and historical iCal times

Depends on Phase 2's resolver.

- 3.1 `ClassSession` carries resolved `time`/`endTime`
- 3.2 `session_preview` prices from resolved duration — closes `NEW-31`
- 3.3 iCal stamps historical times — closes the documented gap in
  `AI_DOCS/calendar-and-sessions.md`

### Phase 4 — Enrolment dates in billing (Group F)

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

## 7. What is deliberately not here

- Materialising a sessions table. The exception-row model
  (`AI_DOCS/calendar-and-sessions.md`) is coherent and works; replacing it is a
  rewrite with no user-visible gain.
- Splitting a class into two on a schedule change (the calendar-app "this and
  following events" pattern). A StudyHub class is a foreign-key hub for
  attendance, enrolments, rates and cancellations; splitting fragments every one
  of those histories across two IDs.
