# Changelog

All notable user-visible or operationally-significant changes to StudyHub.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
but kept loose. Add an entry to **Unreleased** on every PR; promote to a
dated section when you cut a deploy.

## [Unreleased]

### Added — Class assignment, PDPA deletion, parent profile, enrollment email

- **Class assignment dropdown on enrollment approval** — when admin approves
  a child enrollment, the approve modal now shows a multi-select checkbox list
  of all available classes with capacity indicators. Selected classes are
  passed in the POST body as `classIds`; student is enrolled immediately and
  class `enrolled` counts are incremented. Full classes are greyed out.
- **PDPA admin-only account deletion** — `DELETE /api/families/{id}/pdpa`.
  Soft-deletes the family, all linked students, and the parent user account.
  Overwrites PII (name, email, phone, address, medical info) with
  `[deleted]` so invoices and audit logs remain intact for tax/legal
  retention. Triple confirmation UI (confirm + confirm + type "delete").
  Audit log entry: `pdpa_account_deleted`.
- **Enrollment approved email** — when admin approves an enrollment, the
  parent receives `renderEnrollmentApprovedEmail` with the child's name and
  assigned classes (if any). Sent async post-commit.
- **Parent profile modal** — "My Profile" button on parent dashboard hero.
  `GET/PUT /api/auth/profile` updates name and phone on user + family +
  student rows. `POST /api/auth/change-password` requires current password
  verification before setting a new one. Both audit-logged.

### Fixed — Audit-discovered bugs + UX improvements

#### Critical fixes from codebase audit
- **Parent invoice query missing `referral_credit` column** — parent users
  would see zero invoices because Scan read 16 fields but the parent SELECT
  only had 15 columns. Added `COALESCE(i.referral_credit,0)` to both parent
  query variants in `handlers_invoices.go`.
- **`referralRewards` missing from store.js ARRAY_DEFAULTS** — users with
  stale localStorage would crash on `.filter()` calls. Added
  `referralRewards: []` and `registrations: []` to the defaults.
- **`.env.example` was stale** — referenced `DB_PATH=studyhub.db` from the
  SQLite era. Rewritten with current env vars.

#### UX improvements from user story analysis
- **Parent snapshot includes their own enrollment requests** — parents now
  see pending enrolments on their dashboard (was empty because snapshot only
  populated registrations for admin). New `listParentEnrollments` query.
- **Referral code validated at enrollment time** — `POST /api/enrollment-
  requests` checks if the code exists and warns (doesn't block) if invalid
  or self-referral. Frontend shows warning toast with the explanation.
- **Admin "Needs Attention" card enhanced** with:
  - Pending enrolments (separated from teacher apps and legacy regs)
  - Pending teacher applications (separate count)
  - Payments awaiting verification (`Pending Verification` status)
  - Orphan parents (families with no active students linked)
- **Teacher empty-state dashboard** — teachers with zero assigned classes
  see a friendly "No classes assigned yet — your admin will set up your
  schedule shortly" placeholder instead of a blank dashboard.
- **Payment confirmation email to parent** — when a parent clicks "I've
  Paid", the backend sends `renderPaymentReceivedEmail` confirming the
  amount, description, and payment method. Sent async in a goroutine so it
  doesn't delay the response. Stops the "did you get my payment?" WhatsApp
  messages.

### Changed — Registration flow split: parent account vs child enrolment

The public `/register.html` form no longer collects student info. The two
flows are now cleanly separated:

**Parent signup (public, self-serve):**
- Parent enters name, email, phone, password on `/register.html`
- Backend creates `users` row (pending_verification) + `families` row (with
  referral code) — no `registrations` row needed
- Parent verifies email → account activated → lands on dashboard

**Child enrolment (in-app, parent-initiated):**
- From the parent dashboard, parent fills in child details (name, DOB,
  gender, school, grade, subjects, referral code, notes) via an inline form
- `POST /api/enrollment-requests` creates a `registrations` row with
  `type='enrollment'`, automatically email-verified (parent already verified)
- Admin sees it in the pending queue with a purple "Child enrolment" badge
- Admin clicks "Enrol student" → student created, linked to parent's family,
  referral code validated

**Admin pending list updated:**
- Now shows all three types: enrollment requests (purple badge), teacher
  applications, and legacy student registrations
- Enrollment requests are auto-verified (no amber "awaiting" badge) since
  the parent account is already active
- Approve button label adapts: "Enrol student" / "Approve teacher" /
  "Link student to parent" / "Approve & create account"

**Backwards compat:**
- Existing `type='student'` pending registrations from the old combined flow
  are still visible and approvable via the legacy path
- `handleRegistrationApprove` now handles three types: `enrollment` (just
  create student + link), `teacher` (create staff + user + send set-password
  email), `student` (legacy combined flow)

### Added — Security hardening + background jobs + sanitization

#### Security
- **Failed login lockout per account.** New `users.failed_login_count` and
  `users.locked_until` columns. After 5 failed password attempts an account
  is locked for 15 minutes; even the correct password is rejected during
  the lockout window with a "try again in N minutes" message. Successful
  login resets the counter. Closes the per-account brute-force window that
  per-IP rate limiting alone doesn't cover.
- **HTML sanitization on email templates.** New `safeName()` helper in
  `mailer.go` runs every user-provided string through `html.EscapeString`
  before interpolation. Stops a parent who registers as
  `<script>alert(1)</script>` from injecting tags into every email we send
  to them. Stdlib only — no `bluemonday` dependency since the templates
  only ever take plain-text fields.

#### Background jobs
- **In-process job runner** (`backend/jobs.go`) using the `time.NewTicker`
  pattern. `startJobs(db)` is called once from `main()`. Each job runs in
  its own goroutine, recovers from panics so a buggy job can't crash the
  server, and logs the start + duration of every cycle.
- **`archive-announcements`** (daily) — flips `status` to `archived` for
  any announcement whose `archive_on` date has passed. Idempotent.
- **`overdue-reminders`** (hourly) — sends a friendly reminder email for
  every unpaid invoice past its due date. Deduped via the new
  `invoices.reminder_sent_on` column so each invoice is reminded at most
  once every 3 days. Uses the new `renderInvoiceReminderEmail` template.
- **`referral-recheck`** (hourly) — sweeps every `pending` referral_rewards
  row and re-evaluates against the 3-paid-month threshold. Belt-and-braces
  for invoices marked paid via direct DB update or batch import.
- All three jobs run **once on startup** (not waiting for the first tick)
  so freshly-deployed servers do useful work immediately.

#### Tests
- **`backend/feature_flow_test.go`** — 11 new tests covering:
  - Failed login lockout after 5 attempts (and correct password still
    rejected during lock window)
  - Successful login resets the failure counter
  - Family auto-creation when admin adds a student
  - `GET /api/families/{id}/referral` returns the family code
  - Invoice create + mark-paid + DB persistence round-trip
  - Negative amount rejected (renamed to avoid collision with main_test.go)
  - Attendance check-in persists to the DB
  - Referral milestone full lifecycle: register → 3 paid invoices →
    auto-earned → consume one credit → decremented
  - Referral pending below threshold (only 2 paid → still pending)
  - Referral consume returns 400 when status is not `earned`
- Plus the **15 email-flow tests** from the previous turn now run alongside
  these via the same Postgres CI service. Total new test count this batch:
  **26 integration tests**.

### Added — Phase 5 + observability + persistence cleanup
- **Admin pending registrations UI** now shows a green "Email verified" badge
  for confirmed registrations, an amber "Awaiting email verification" badge
  otherwise, and a blue "Parent already has account" badge when the parent
  has self-registered. Approve button label adapts: "Link student to parent"
  for self-served, "Approve & create account" for legacy admin-driven, and
  "Approve teacher" for staff applications.
- **Approve toast adapts to response shape** — temp passwords are only
  surfaced for the legacy admin-driven flow; self-served and teacher flows
  show the friendly message from the backend instead.
- **`Registration.EmailVerifiedAt`** added to the model and surfaced through
  `listRegistrations` so the admin badge can render.
- **Invoice persistence fixed** — the long-standing localStorage-only
  inconsistency. `_doGenerateMonthly`, `_doSiblingInvoice`, and the single
  invoice creation form all now POST to `/api/invoices` and reload the
  snapshot. Failures are surfaced via toast with a count of saved/failed.
- **Structured logging via `log/slog` (stdlib)**. New `backend/logger.go`
  with `logger` global, JSON output in production / staging, text in dev.
  `logFromCtx(ctx)` and `logFromReq(r)` automatically include the
  request_id from middleware. All `fmt.Println` calls in the email/auth
  paths replaced with structured `logger.Error` / `logger.Info`.
- **Mailer dev-mode logging** now uses slog so the verification link is a
  proper structured field — easy to grep with jq or just eyeball in stdout.
- **`backend/email_flow_test.go`** — 14 new integration tests covering the
  health endpoint, parent self-serve registration (success, duplicate email,
  short password), parent verification end-to-end, token reuse rejection,
  teacher application + verification, password reset enumeration safety,
  password reset happy path, set-password happy path, and resend
  verification token rotation. Compiles cleanly; runs against the Postgres
  test container provisioned in `.github/workflows/ci.yml`.
- **CI workflow credentials updated** — Postgres service now uses the
  user/password that the existing `testDSN()` helper expects, and the test
  step exports `TEST_DATABASE_URL` instead of `DATABASE_URL` so the legacy
  helper picks it up.

### Added — Email Phases 3 + 4: teacher verification + set-password flow
- **Teacher applicants must verify their email** before their application
  reaches the admin queue. New token purpose `verify_teacher`. Cuts spam from
  fake/typo email addresses.
- **Single `/api/verify-email` endpoint dispatches on token purpose** —
  parent verification still auto-logs in and redirects; teacher verification
  shows a "thanks, in review" page without creating an account or cookie.
  New `consumeEmailTokenAny` helper accepts multiple purposes.
- **`POST /api/resend-verification` works for both flows** — looks up a
  pending parent user first, falls back to a pending teacher registration.
- **Admin teacher approval no longer generates temp passwords.** Approving a
  teacher creates the staff + user records (with an unguessable placeholder
  hash so the account can't be logged into yet) and emails a `set_password`
  link. Audit log entry: `teacher_approved`.
- **`POST /api/set-password` endpoint** — consumes a `set_password` token,
  writes the chosen password, activates the user, issues an auth cookie,
  returns user info so the frontend redirects to the dashboard signed in.
- **`set-password.html` landing page** for the first-password flow.
- **Two new email templates** — `renderVerifyTeacherEmail` and
  `renderTeacherWelcomeEmail`.
- **Teacher tab on register.html** now shows "Check your email" success
  state with the applicant's email surfaced in the message.
- **`verify.html` dispatches** on the response `type` field — parents auto-
  redirect to dashboard, teachers stay on the page with a "we'll review"
  message and a back-to-login link.

### Added — Email Phase 2: parent self-serve registration
- **Parents pick their own password at registration.** No more admin-generated
  temp passwords for parents. The form now requires a password + confirm with
  show/hide toggle.
- **Email verification flow.** New parents get a one-click verification link;
  clicking it activates their account, sets the auth cookie, and lands them
  on the dashboard. New endpoint `GET /api/verify-email?token=`. New
  `verify.html` landing page.
- **Resend verification.** New endpoint `POST /api/resend-verification`,
  enumeration-safe (always 200). Login page shows a "Resend verification
  email" banner when the user tries to log in before verifying.
- **Login gates on verification status.** `users.status = pending_verification`
  blocks login with a 403 + `needs_verification` sentinel; frontend swaps the
  generic error for the resend banner.
- **Welcome empty state on parent dashboard.** Self-served parents whose
  children haven't been linked yet see a friendly placeholder instead of a
  blank dashboard.
- **`handleRegistrationApprove` is backwards-compatible** — when admin
  approves a registration where the parent already self-served, it skips
  user creation and only creates the student/family/links, omitting the
  temp password from the response.
- **`renderVerifyParentEmail`** template added to `mailer.go`.

### Added
- **Atlas-style numbered SQL migrations** in `backend/migrations/`. New schema
  changes go into versioned files instead of being appended to
  `runMigrations`. Tracked by checksum in `schema_migrations` table; safe
  against accidental edits to applied files (warns loudly).
- **`GET /api/health` endpoint** — returns `{ok, db, version, env}` after a
  DB ping. Public route, suitable for uptime monitors and Docker healthchecks.
- **Multi-environment config** — server now loads `.env` then `.env.${APP_ENV}`
  on top, so per-environment overrides are supported. Defaults to
  `APP_ENV=development`.
- **Request ID in error responses** — `respondError` now returns
  `{error, request_id}`. Lets support correlate user reports with server logs.
- **Centralised frontend API error handling** — `App.Api` now auto-toasts
  failures, parses backend `{error, request_id}` envelopes, and surfaces
  network errors. Pass `{silent: true}` to opt out per call site.
- **Password show/hide toggle** — eye-button on the login page and reset page.
  Reusable `togglePw()` helper for future password fields.
- **Real password reset emails** via Resend — `/api/forgot-password` now
  generates a token, sends a link, and `/api/reset-password` consumes it.
  New `reset.html` landing page handles the form. Falls back to stdout in
  dev mode when `RESEND_API_KEY` is unset.
- **Mailer infrastructure** — `backend/mailer.go` (Resend HTTP client + dev
  fallback), `backend/email_tokens.go` (verification/reset/set-password token
  helpers), `email_tokens` table for verification + reset + set-password flows.
- **Referral discount system** — parents share a `SH-XXXX` code at
  registration; system tracks referrer→referred linkage, auto-detects when
  the referred student hits 3 paid monthly invoices, applies RM10/month off
  for 3 cycles to the referrer's next invoices. Admin sees the full ledger
  in the family modal. New endpoints: `GET /api/referrals`,
  `GET /api/families/{id}/referral`, `POST /api/referrals/{id}/earn`,
  `POST /api/referrals/{id}/consume`.
- **Project documentation** — `README.md`, `CONTRIBUTING.md`, this changelog.
- **CI workflow** — `.github/workflows/ci.yml` runs `go vet`, `go build`,
  `go test` on every push and PR.

### Changed
- **Backend handler files split by domain.** `handlers.go` (1327 lines) and
  `handlers_extra.go` (1742 lines) replaced with one
  `handlers_<domain>.go` per concern (students, classes, invoices,
  attendance, feedback, etc.). `handlers.go` now contains only shared helpers
  and the snapshot endpoint (~163 lines).
- **MEMORY.md / project memory** updated to reflect the actual stack (Postgres
  not SQLite). Email setup notes added as a separate memory file.

### Schema
- New tables: `referral_rewards`, `email_tokens`, `schema_migrations`
- New columns: `families.referral_code`, `students.referred_by_family_id`,
  `registrations.referral_code`, `invoices.referral_credit`,
  `users.status`, `users.email_verified_at`, `registrations.email_verified_at`

---

## How to update this file

- Add to `## [Unreleased]` while you work.
- When deploying, rename `Unreleased` to `## [YYYY-MM-DD]` and add a fresh
  empty `Unreleased` block at the top.
- Categories: **Added**, **Changed**, **Fixed**, **Removed**, **Security**,
  **Schema** (for migrations).
- Don't log purely internal refactors unless they affect deployment or rollback.
