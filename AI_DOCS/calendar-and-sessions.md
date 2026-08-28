# Classes, sessions, cancellations, holidays, iCal, attendance

Read this before touching `handlers_classes.go`, `handlers_session_overrides.go`,
`handlers_cancelled.go`, `handlers_replacement.go`, `handlers_holidays.go`,
`handlers_ical.go`, `handlers_attendance.go`, or `frontend/js/modules/calendar.js`.

## The central fact: there is no sessions table

**Classes are recurring templates. Concrete sessions are computed on read.**

The `classes` table stores a weekday *name* and start/end times as TEXT, with no date column
at all (`store/database.go:111-125`). Nothing materialises per-date session rows. A query
against a `sessions` table will find nothing, because there is none.

Per-date divergence is modelled as **exception rows**, all keyed `(tenant_id, class_id, date)`:

| Table | Models |
|---|---|
| `cancelled_classes` | this session did not happen (credits granted) |
| `class_session_overrides` | this session differs from the template (migration `0040`) |
| `class_session_moves` | this session runs on another date (migration `0042`) — soft-deletable so a move can be undone; NO credits (the class still happens); a cancelled session cannot be moved (409); move and undo each auto-announce to the class |
| `holidays` | this date is a holiday (display + reminder suppression only) |

Migration `0040_class_session_overrides.sql:5-14` states the reasoning: classes "carry a day
and a time but no date, so editing one to cover 'Ms Tan took Monday for Ms Lee this week'
rewrites every Monday, past and future" -- including payroll attribution. The override table
exists so "just this week" stops meaning "every week from now on".

Session identity is enforced by a partial unique index on `(tenant_id, class_id, date)`
`WHERE deleted_at IS NULL` (`0040:32-33`). Any new per-session table must key the same way.

**The canonical expander is `store.SessionsInPeriod` (F6, `store/sessions.go`).** It flattens
one class's weekly recurrence over a window and classifies every occurrence:
`held | cancelled | holiday | moved_out | moved_in`. It classifies rather than filters (iCal
needs STATUS:CANCELLED under the same UID), returns TEXT local dates (never `time.Time`),
and derives the tenant from the class row, not claims (the iCal caller has synthetic
claims). `ClassSession.Billable()` encodes the money rules: cancelled sessions stay billable
(credits compensate), holidays do not, and a moved session bills on its ORIGIN date only.
Any code that expands or counts a class's dated sessions MUST call it -- the iCal feed
already does; the future billing cron must too. Locked by
`TestSessionsInPeriod_ClassifiesEveryOccurrence`.

## Session overrides

Today an override can change **only `teacher_ids`** (plus a free-text note) --
`handlers_session_overrides.go:15-18`. There is no storage for a per-session time or room;
extend the table first if you need one.

Writes upsert via `ON CONFLICT` against the `0040` index, so a repeat swap replaces rather
than stacks. An override with zero teachers is rejected with a 400 -- reverting to the usual
teacher is done by **DELETE**, not by posting an empty `teacherIds`
(`handlers_session_overrides.go:60-79`).

**The API has no frontend consumer.** `/api/session-overrides` is wired in `server.go:269`,
but nothing under `frontend/js/` references it, and `calendar.js:57` pulls only
`{ classes, staff, students, cancelledClasses, holidays }` from the store. The calendar still
shows template teachers on swapped dates. Do not assume the UI already renders swaps.

## Cancellations

Cancelling a session synchronously does three things in one handler
(`handlers_cancelled.go:64-69, 104, 132-138`): inserts the `cancelled_classes` row, creates a
published class-scoped announcement, and grants each enrolled student a replacement credit.
Any new cancellation path (bulk cancel, holiday-driven) that inserts directly into the table
skips the announcement and the credits.

**F3 hardening (2026-08-28, migration `0044`):** `cancelled_classes` now has `deleted_at`
and a partial unique index on `(tenant_id, class_id, date) WHERE deleted_at IS NULL`. The
create is idempotent -- a duplicate POST hits `ON CONFLICT DO NOTHING` and returns 409 with
no re-grant (`handlers_cancelled.go`). `DELETE /api/cancelled-classes/{id}` undoes one:
soft-deletes the row, claws back the exact credit grants (fingerprint: earned / class /
`'Class cancelled on <date>'`), and announces the reversal. Cancelling a date with a live
session move returns 409 (undo the move first), mirroring the move handler's check in the
other direction. Every reader filters `deleted_at IS NULL` (list, iCal window, move-conflict
check). Locked by `TestCancellation_IdempotentAndUndoable`.

### Two verified conflicts here

**Credit magnitude -- FIXED 2026-08-26.** Both grant paths now credit the class's
duration at 1 credit = 15 minutes via `App.Utils.creditsForClass` (frontend) and
`creditsForDuration` in `handlers_cancelled.go` (backend) -- keep the two mirrors in
sync. Migration `0041` topped up historical flat-1 cancellation grants. Original
finding: **RESOLVED 2026-08-21, backend is wrong.** The unit was defined with
the centre on 2026-04-02 (WhatsApp, Nadine-confirmed): **1 credit = 15 minutes**, so a
1-hour class is 4 credits. The frontend absent flow granting `minutes = 4`
(`attendance.js:1129-1137`) matches the spec; the backend cancellation grant of a flat
`minutes = 1` (`handlers_cancelled.go:135`) is the bug -- it should grant class duration /
15 min. Filed as A16 in `V2_IDEAS.md`. Both flows share category `"class"` and one balance,
which is why the under-grant surfaced as the centre's "insufficient credit" report
(2026-08-19). Redemption asks for a credit count and checks it against the summed balance
(`students.js:1342-1360`); nothing anywhere treats the column as literal minutes.

**Parents get two cancellation announcements -- FIXED 2026-08-26.** The teacher-absent
flow no longer posts its own announcement; the backend cancellation handler's
auto-created one (`handlers_cancelled.go`) is the single source.

## Billing does not know about cancellations

`cron.go` never reads `cancelled_classes`, `holidays`, or `class_session_overrides`. Invoices
are not pro-rated for cancelled sessions; the compensation mechanism is the replacement
credits ledger. See `AI_DOCS/billing.md` for the pricing chain.

## Holidays are display-only

The calendar renders a badge/tint but **still shows the classes**
(`calendar.js:227-231`). The only backend behaviour tied to holidays is suppressing
overdue-invoice reminder emails (`jobs.go:329-345`). A holiday does not cancel sessions, grant
credits, or affect the iCal feed.

**F4 -- FIXED 2026-08-28: one canonical range predicate.** `core.HolidayCovers(date,
endDate, day)` (`core/respond.go`) and its mirror `App.Utils.holidayCovers` (`js/utils.js`)
are the only holiday range predicates; both treat empty or malformed `endDate` (before
`date`) as single-day. The old backend SQL treated empty `end_date` as open-ended, so one
past single-day holiday suppressed reminder emails forever -- `isHolidayToday` now filters
through the helper. Unit-locked on both sides (`core/respond_test.go`,
`tests/unit/utils.test.mjs`). Any new holiday consumer (the session expander) must call
these, never inline the comparison.

## iCal feed

The feed is a `store.SessionsInPeriod` consumer since F6: per relevant class it expands the
window and maps `held`/`holiday` to a normal event, `cancelled` to STATUS:CANCELLED,
`moved_out` to a same-UID event on the new date, and `moved_in` to an event only when the
origin lies outside the window (an in-window origin already emitted the moved event).


Each relevant class is flattened into individual VEVENTs across a **fixed window of 7 days
back to 42 days forward**, by scanning every calendar day and matching the weekday. There is
no RRULE (`handlers_ical.go:149-170`). Events beyond six weeks simply do not exist in the
feed -- check the window first when debugging "missing events".

**A15 -- FIXED 2026-08-26:** the feed now loads the window's `cancelled_classes` and emits
those occurrences under the **same UID** with `STATUS:CANCELLED` plus a "Cancelled: " summary
prefix (`cancelledDatesInWindow` in `handlers_ical.go`), so already-synced calendars update in
place. Holidays and `class_session_overrides` are still not consulted -- holidays are
display-only by design, and override rendering arrives with the session expander.

Moved sessions are emitted under their ORIGINAL date's UID with `DTSTART` on the new
date, so synced calendars relocate the event. UID format is `class-<classID>-<YYYYMMDD>@studyhub.fit` (`handlers_ical.go:189`) -- the
`(class_id, date)` key re-expressed for calendar clients. Changing it orphans every event
already synced into parents' calendar apps.

The feed token is `hex(HMAC-SHA256(JWT_SECRET, "ical:<userID>:<email>:<version>"))` where
version is `users.ical_token_version` (`handlers_ical.go:44-48`, `0027_ical_token_version.sql:6-10`).
Bumping the version revokes one user's feed without rotating `JWT_SECRET`; password reset does
this. Note the **email is part of the signed input**, so changing a user's email invalidates
their feed. No tokens are stored, so any new revocation flow must bump the version.

The route lives outside the JWT middleware on purpose -- calendar apps do not speak cookies --
and the handler builds synthetic parent Claims to reuse tenant-scoped queries
(`server.go:120-122`, `handlers_ical.go:115-122`). Anything added to `listClasses` /
`listStudents` flows out through this cookie-less endpoint.

Event times are written as **floating local times** (no TZID, no Z), relying on the
`X-WR-TIMEZONE:Asia/Kuala_Lumpur` header and the server's local zone. `DTSTAMP` alone is UTC,
so mixed-format edits are easy to get wrong (`handlers_ical.go:147, 178-210`).

## Dates and the weekday contract

All date columns in this subsystem are **TEXT compared lexicographically**: `attendance.date`,
`cancelled_classes.date`, `holidays.date`, `class_session_overrides.date`. Backend stamps come
from `core.Today()` = `time.Now().Format("2006-01-02")` in server-local time
(`core/respond.go:65`). Introducing `DATE`/`TIMESTAMPTZ` columns or ISO strings with a time
component breaks the string-equality joins between attendance, cancellations, and overrides.

On the frontend, `toISOString()` is banned for calendar dates -- in UTC+8 the UTC day is still
"yesterday" until 08:00 local. Use `App.Utils.localDate` (`utils.js:357-368`). `calendar.js`
repeats the warning inline at `275-278` and hand-builds the local date string at `408-413`,
because the week grid's cancellation strike-through depends on exact string equality with
`cancelled_classes.date`.

`classes.day` holds **English weekday names** (`"Monday"`), matched by string on both sides
(`calendar.js:89`, `handlers_ical.go:213-231`, `attendance.js:238` via
`toLocaleDateString('en-US', { weekday: 'long' })`). Localising or abbreviating those values
breaks scheduling, day-filtering, and iCal expansion at once. The calendar week starts Monday
(`calendar.js:4-12`).

**Naming collision:** migration `0032_sessions_invalid_before` and `0006`'s "session
revocation" are about **auth JWT sessions**, not class sessions (`0032:3-8`). Grepping
"session" for scheduling work hits auth first.

## Attendance

Rows upsert on `(person_id, date, class_id)` within tenant, serialized by
`pg_advisory_xact_lock` on that composite -- explicitly because duplicate rows double-count
part-time payroll hours (`handlers_attendance.go:205-237`). **There is no DB unique constraint
backing this**; the advisory lock is the only guard, so any write path bypassing this handler
can corrupt payroll.

The upsert overwrites `check_in` with whatever the client sends, so **every check-out call
must echo the existing check-in time back** or it is blanked. The frontend does this
deliberately in three places (`attendance.js:817-819, 453, 935`). A new client that posts only
`checkOut` erases the check-in.

Marking a student absent with credit is a two-step non-atomic flow: the absence POST must
succeed before the credit POST is attempted, because a swallowed absence failure previously
left "a credit for a class nobody was ever marked absent from" (`attendance.js:1114-1125`).
The reverse gap (absence saved, credit fails) still exists and only warns via toast.

Redeeming credits (`type: "used"`) is serialized per `(student, category)` with a
transaction-scoped advisory lock plus a balance check, so concurrent redemptions cannot both
pass the threshold; earned credits skip the transaction (`handlers_replacement.go:142-186`).

The kiosk flow is optimistic-first with rollback; the POST is what triggers the parent
WebSocket and push notification server-side. Status-only updates (marking absent) carry no
time and are deliberately skipped so a parent toast never reads "checked in at " with no time
(`attendance.js:463-486`, `handlers_attendance.go:262-289`).

## Classes

`classes.enrolled` is **deliberately not updated** by the class PUT handler. It is a derived
count owned by the student add/edit/delete and registration-approve paths; trusting the
client-supplied value "let a class edit silently overwrite the true count, drifting capacity
enforcement" (`handlers_classes.go:207-213`).

Clash detection uses half-open interval overlap (`time < other.end AND end_time > other.time`)
with teacher membership matched by **JSON-string LIKE** `'%"<id>"%'` against the `teacher_ids`
TEXT column (`handlers_classes.go:108-117`). The same LIKE-on-JSON-TEXT pattern is
load-bearing in the cancellation credit grant against `students.enrolled_classes`
(`handlers_cancelled.go:118`). Migrating these columns to real array types must update every
LIKE site at once.

The class edit modal must send **every** field -- the PUT replaces the whole row. Omitting
`subject` once "quietly cleared the invoice line name every time a class was edited"
(`calendar.js:1253-1256`). A new class column means updating that payload.

Parents see a deliberately different calendar: full classes are hidden, and week view is
forced (`calendar.js:91, 104-105`). A parent reporting a "missing class" may be seeing
capacity-hiding, not a bug.
