# Changelog

All notable user-visible or operationally-significant changes to StudyHub.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
but kept loose. Add an entry to **Unreleased** on every PR; promote to a
dated section when you cut a deploy.

## [Unreleased]

### Added
- The invoice builder has a "New student" section: registration fee (RM250)
  and a one-month deposit priced by class type and level band.

### Fixed
- Invoices no longer print "Contact: Contact: ..." when the label was typed
  into the settings field along with the phone number.
- Confirming a parent-submitted payment no longer dead-ends when the invoice
  has no reference number: the verify dialog now takes the reference from
  the receipt and sends it with the confirmation.
- The parent calendar feed now marks cancelled sessions as cancelled
  (same event, STATUS:CANCELLED and a "Cancelled:" title) instead of
  showing them as still happening.
- Cancelling a class now grants replacement credits matching the class's
  duration (1 credit = 15 minutes, so a 1-hour class earns 4) instead of a
  flat 1; existing shortchanged credits are topped up by migration 0041.
- Marking a student absent credits the class's actual duration, so the
  30-minute class earns 2 credits instead of 4.
- Parents no longer receive two announcements for one class cancellation.
- The self-study tab now shows each student's own package hours instead of
  assuming 4 free hours for everyone.

### Changed
- Replacement credits are for make-up classes only: the self-study category
  is no longer offered when granting or redeeming credits (existing
  self-study credit rows still display).

### Fixed — class enrolment, and prices that silently came out as RM 0

- **Enrolling a student into a class works again.** The Add Class form never
  captured a teacher, and the enrolment picker matched a class by slot + type +
  teacher — so a class created in Calendar could never be found once a teacher
  was selected, reporting "create it in Calendar first" for a class that already
  existed. The form now takes teachers, and the picker lists the real classes
  instead of reconstructing one from three dropdowns.
- **Editing a class no longer wipes its subject**, which is what names the
  monthly invoice line ("Math group lessons").
- **Classes the pricing matrix cannot price no longer bill RM 0.** Phonics has
  no level band and a 30-minute group runs below the Level 1 rate; both missed
  the `(class_type, level_band)` lookup. A class can now carry its own monthly
  fee, which wins over the matrix when set.
- **Level is read from the class's level band, not parsed out of its name.** The
  old `/level (\d+)/` regex hid Phonics from the make-up class picker entirely
  and mis-grouped it in analytics. Level reporting now groups by pricing band
  (1-3 / 4-6) instead of levels 1..6.

### Added

- **A products catalogue** for the things the centre sells — tuition seeded from
  the live pricing matrix, plus Registration (RM250) and level-based Deposit.
  Deposit ships inactive until its amounts are set, so it cannot reach an
  invoice unpriced.
- **Long dropdowns are now typeable.** A shared filter box narrows class and
  student lists in place, keeping the native select underneath.
- The replacement-credit buttons state their direction — "Mark absent
  (+ credit)" and "Book make-up (- credit)" — after the spend button was clicked
  in place of the earn one.

### Security — teacher write paths and remaining leaks

- **Teachers can no longer write records for students they don't teach.** The
  read paths were scoped first, which left creating/deleting self-study sessions,
  issuing or redeeming replacement credits, and reading a credit balance as the
  easier route to the same data.
- **Progress reports can no longer be forged.** A teacher could create a report
  attributed to a colleague and publish it straight to that family; authorship is
  now taken from the session and limited to their own students.
- **Family contact details are hidden from teachers.** Student records already
  blanked parent name/email/phone, but the family list returned them in full —
  and students carry `familyId`, so the redaction was cosmetic.
- **The referral/credit ledger is admin and parent only** — it exposed family
  names, referred-student names and credit balances to teachers.
- Progress-report edit checks now respect soft deletes, and the staff lookup
  behind teacher authorization is tenant-scoped.

### Security — teacher data scoping

- **Teachers are now scoped to their own classes** for feedback, attendance,
  self-study sessions and replacement credits, on both the REST endpoints and the
  snapshot. Students were already class-scoped, but these parallel record types
  were tenant-wide, so a teacher could read data for every family — and staff
  attendance rows exposed colleagues' work patterns.
- **Teacher announcements no longer publish unreviewed.** Creation defaulted to
  `published`, bypassing the admin approval queue that already existed; non-admin
  authors are now forced to `pending`.
- **Background push/email tasks can no longer crash the API.** Fire-and-forget
  goroutines ran outside chi's panic recovery, so one bad push subscription could
  take down the whole process for every user. They now run under `goSafe`.
- **Deleting a user reports not-found instead of success** when nothing matched,
  and the user list no longer panics on a query error.

### Reliability & correctness

- **Dates are recorded in local time.** `today()` used UTC while `nowTime()` used
  local, so in UTC+8 anything logged before 08:00 (attendance, credits,
  self-study) got yesterday's date paired with today's time.
- **Duplicate form submissions are blocked app-wide**, preventing double-clicks
  from creating duplicate announcements, progress reports and classes.
- **Failed attendance writes roll back** instead of leaving the table showing a
  student as present when the server rejected the write.
- **Removed the bulk "Send Message" feature**, which had no backend endpoint at
  all: it wrote to local state, reported "Message sent to N parents", and lost
  everything on the next refresh.
- Referral-code copy buttons pass the code as a data attribute instead of
  inlining it into an `onclick` JavaScript string.
- Added a `(tenant_id, created_at)` index on `audit_logs`, and converted
  `registrations.school_fees` to `NUMERIC(12,2)` — the one money column the
  earlier numeric migration missed.

### Security — staff-data RBAC

- **Teachers can no longer read or fabricate staff performance reviews.** Reviews
  were readable tenant-wide by any teacher (including via the snapshot) and any
  teacher could POST a review against any colleague's staff id. Writing is now
  admin-only and a teacher sees only their own reviews.
- **Teachers can no longer edit other teachers' progress reports.** The update
  path checked only "is staff", so any teacher could rewrite a colleague's report
  or flip `published` to push someone else's draft to parents. Edits are now
  restricted to the report's author (admins keep full rights), matching the
  existing rule for feedback.
- **Announcement expiry date is escaped** where it was interpolated raw into a
  `title` attribute.

### Billing — payment confirmation UX

- **Marking cash paid now requires typing the exact amount received**, so the
  action is deliberate and the recorded amount is confirmed against the invoice.
- **Early-bird discount now applies to sibling invoices**, which previously showed
  the checkbox but silently ignored it.
- **"Payment received" email only sends when confirming a payment the parent
  submitted**, so bulk and admin cash mark-paid no longer mail every parent.

### Security

- **Teachers can no longer access any family's billing.** The invoice list,
  snapshot, payment-proof upload/download, checkout, and invoice/receipt PDF
  endpoints only gated parents by ownership and let every other authenticated
  role (including teachers) through to tenant-wide financial data and receipts.
  All now restrict to admins and the owning parent, matching the pay endpoint.
- **Stored XSS in Messages fixed.** The conversation-list preview rendered
  another user's last-message text unescaped, so a crafted message body could
  execute script in a recipient's (including an admin's) session. Message
  previews and the shared badge helper are now escaped; the module's local
  escaper delegates to the canonical one so it can't drift again.

### Billing — behavioral

- **"Payment received" email now sends on confirmation, not submission.** It
  fired the moment a parent submitted a payment for verification, implying the
  money had arrived. It now sends when the invoice is actually marked Paid, to
  the owning parent (regardless of whether an admin verified or logged it).
- **Editing an invoice no longer destroys its line items** unless the amount
  changed. A description- or due-date-only edit previously wiped the itemised
  breakdown, collapsing the receipt/PDF to a single synthesized line.
- **The Create Invoice modal stays open if creation fails**, so the built line
  items aren't lost and the admin can correct and resubmit.

### Billing — reliability

- **`paid_on` is only set when an invoice becomes Paid.** It was stamped on
  every pay-endpoint call, so a parent's "Pending Verification" submission (or an
  admin setting Overdue) gave an unpaid invoice a paid date.
- **Parent receipt is now mandatory on upload failure.** The parent flow used to
  submit the payment without the receipt if the upload failed; it now blocks and
  asks the parent to retry, matching the admin flow.
- **Admin confirm no longer gets stuck.** If the pay request failed after a
  successful receipt upload, the button stayed disabled on "Uploading..."; it now
  re-enables so the admin can retry.
- **Undo on invoice delete is honoured.** The delete fired 500ms before the undo
  toast closed, so a late Undo click still deleted; the delete now runs after the
  undo window closes.
- **Pay-online is guarded against double-clicks** that could mint duplicate
  payment-gateway checkout sessions.

### Billing — money correctness

- **Early-bird discount now applies to the net subtotal.** It was computed on
  the gross positive line total, so a free/FOC add-on (a +RM40 line cancelled by
  a -RM40 credit) inflated the discount base and over-credited the parent — and
  an all-free line could push the total negative and get rejected. The discount
  now uses the net of all lines.
- **Bulk "Mark All Paid" is Cash-only.** It offered Bank Transfer / QR Pay, but
  those require a per-invoice reference the bulk modal can't collect, so they
  always failed. Bulk is now cash-only; non-cash payments are confirmed per
  invoice where the receipt and reference are captured.

### Billing — payment confirmation fixes

- **Bulk "Mark All Paid" now persists.** The bulk confirm only updated local
  state and never called the API, so every invoice reverted to Unpaid on the
  next data reload. It now records each payment server-side and reports partial
  failures (e.g. a non-cash invoice missing a reference number) instead of
  silently claiming success.
- **Verify Payment now persists.** Approving a parent-submitted payment had the
  same local-only defect and reverted on reload; it now commits through the pay
  endpoint.
- **Receipt uploads no longer fail as a "network error".** The 5MB cap tripped
  mid-upload on ordinary phone photos, which the browser reported as a lost
  connection. Raised the limit to 15MB and added a client-side size check that
  rejects oversized files with a clear message before uploading.

### Security & correctness — prod-readiness hardening pass

- **Billing data corruption fixed:** a legacy startup migration re-divided every
  replacement-credit balance by 15 on every boot (45 → 3 → 1). Removed; the
  ledger is consistently in minutes.
- **Payment webhooks fail closed:** Billplz/Stripe webhooks now reject requests
  when the signing secret is unset (previously they skipped verification,
  letting anyone mark invoices paid on a misconfigured deploy). Added Stripe
  timestamp tolerance to block replays.
- **Session revocation:** logout now revokes the refresh-token family and clears
  its cookie; password change/reset revoke refresh families (and reset bumps the
  iCal feed token) so a compromised session can't survive.
- **IDs are collision-safe:** `GenerateID` appends crypto-random bytes, so
  same-millisecond cron IDs no longer collide and silently drop invoice/payroll
  rows.
- **Registration approve is idempotent:** double-clicks no longer create
  duplicate students / double-bump class counts; the "enrolled" email only sends
  after the transaction commits.
- **PDPA delete** checks every anonymisation statement and rolls back on failure
  instead of reporting success while PII survives.
- **Money precision:** `invoices.early_bird_discount`, `pricing_tiers.monthly_fee`
  and `registrations.school_fees` converted from float to `NUMERIC(12,2)`
  (migration `0025`).
- **Concurrency:** attendance upserts and student-edit capacity checks are now
  serialized (advisory locks) so kiosk double-scans / concurrent edits can't
  create duplicate rows or overfill a class.
- **Audit logs are tenant-scoped:** `audit_logs` rows are stamped with the
  correct tenant instead of defaulting to tenant 1.
- **MFA:** the enrolment QR is generated server-side (the TOTP secret is no
  longer sent to a third-party QR service); disabling 2FA requires a fresh code;
  intermediate tokens are hashed at rest.
- **Frontend:** cached snapshot (student PII, medical info, staff salary/NRIC) is
  cleared on logout/401; XSS-safe brand rendering; SRI on CDN scripts; dev
  quick-login credentials and real-looking mock emails removed.
- **Infra:** docker-compose requires `JWT_SECRET`/`POSTGRES_PASSWORD` (no
  insecure fallbacks) and rotates container logs; healthcheck fixed; analytics
  runs non-root; deploy SSH action pinned to a commit SHA.
- **Multi-tenant RLS foundation:** removed the middleware that poisoned pooled
  connections; staged a non-superuser DB role (migration `0026`) and an
  activation runbook (`notes/rls-activation.md`). Tenant isolation is enforced by
  `ScopeTenant` (tested).

### Fixed — Teacher privacy, progress-report scoping, payroll correctness

- **Teacher privacy (PDPA):** the students API/snapshot now strips parent
  name/email/phone, emergency contact and admin notes for teacher sessions
  (server-side, not just hidden in the UI); the student list table no longer
  shows a Parent/Contact column to teachers; the progress-report list is
  scoped server-side to a teacher's own students.
- **Progress-report PDF** is now tenant-scoped and teachers can only download
  reports for their own students.
- **Payroll recalculation is real:** the monthly cron and the admin
  "Recalculate from check-ins" action now refresh stale Pending rows in place
  (late check-ins are captured). Rows marked Paid or hand-edited are frozen.
  Admin can hand-edit any payroll row (base/bonus/deductions/status) via
  `PUT /api/payroll/{id}` — edits flag the row `manually_edited` (migration
  `0022`). The old "Generate Payroll" button, which only wrote to local
  browser state, is replaced by the real recalc + edit flow.
- **Part-time payroll bug:** the cron compared employment type against
  `"Part-time"` while the staff form saves `parttime` — part-time teachers
  were silently paid a flat salary (or skipped). Comparison is now normalized.

### Added — Analytics by level & on-screen invoice breakdown

- **Analytics "By Level" view** — per-level (1–6) table of students, classes,
  attendance rate, class fill and collected revenue, plus an
  attendance-by-level chart and a Level filter across all analytics views.
- **Invoice breakdown on screen** — click an invoice's description to see the
  itemized packages/discounts (same data as the PDF); breakdown also shows in
  the parent Submit Payment and admin Verify Payment dialogs. Sibling and
  self-study invoices are now itemized too.

### Added — Package line items on invoices (Skooly-format)

- **Invoice PDF redesigned** to the centre's Skooly-style layout: centered
  letterhead with logo, a two-column **Items | Amount (RM)** table where each
  line shows its billing period, a descriptor, and a `qty x unit` sub-line,
  then `Subtotal` / `Total Tax` / named discount lines / `Total Due`, a payment
  **Note** block, and numbered terms.
- **Package picker in Create Invoice** — admins build an invoice by adding
  packages from a dropdown (Group/Private Level 1–6 priced from the matrix,
  Self-study 4h/8h, and an hourly add-on) instead of typing a free-text amount.
  Included self-study is auto-added as an FOC discount line that nets to zero;
  extra hours use the add-on. The total is derived server-side from the items.
- **Monthly & self-study cron invoices are now itemized** — one line per
  enrolled class, the included self-study membership with its FOC line, and
  named referral/sibling/early-bird discount lines.
- Invoices gained a `line_items` JSON column (migration `0021`). Invoices
  created before this render unchanged via a single synthesized line.

### Changed — Production hardening (greenre standard audit)

- **`.dockerignore` added** — excludes `.git/`, docs, tests, IDE files from
  Docker build context. Faster builds, smaller layer cache, no history leak.
- **Dockerfile optimized** — Go binary now built with `-trimpath -s -w` flags
  (strips debug symbols + paths). Binary ~30% smaller, no local path info in
  stack traces.
- **Build-time version embedding** — `docker compose build` now accepts a
  `VERSION` build arg (via `ldflags -X main.buildVersion`). Surfaces in
  `/api/health` response and startup log. Defaults to `dev` for local builds.
- **All startup logging unified to slog** — replaced `log.Printf` / `log.Fatal`
  calls in `main()` with structured `logger.Info` / `logger.Warn` /
  `logger.Error`. Every line from boot to shutdown is now structured JSON in
  production (greppable, ships to log aggregators).
- **`docker-compose.yml` env gaps filled** — added `APP_ENV` (defaults to
  `production`), `RESEND_API_KEY`, `EMAIL_FROM`, `APP_URL` to the `api`
  service. Container no longer silently runs in dev mode.
- **PR template** — `.github/pull_request_template.md` with Summary, Key
  Changes, and Test Plan sections.
- **CI pipeline improved** — `go mod tidy` drift detection (fails if go.mod/
  go.sum are stale), CHANGELOG update reminder on PRs.
- **Conventional commit format** — CONTRIBUTING.md now specifies
  `<type>(<scope>): <summary>` format with examples.

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
