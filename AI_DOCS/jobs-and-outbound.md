# Background jobs, email queue, push, websockets

Read this before touching `internal/jobs/`, `internal/mailer/`, `internal/notify/`,
`store/email_queue.go`, `handlers_push.go`, `handlers/ws.go`, or `handlers/background.go`.

## The 2026-07-31 incident shapes this whole subsystem

A dev box with production env vars started draining the real email queue at real parents,
"stopped only by an unverified sender domain" (`mailer/mailer.go:44-46`). Several guards exist
solely because of it. Do not weaken any of them.

**Live email requires BOTH `APP_ENV=production` AND `OUTBOUND_ENABLED=1`.** An API key alone
is never enough (`mailer.go:47-48`). Web push has the identical gate -- VAPID keys are not
even loaded otherwise (`notify/webpush.go:42`).

The gate is evaluated **once at process init**: when gated, `Init` registers `devMailer`, so
every `core.SendEmail` call is suppressed to a log line. There is no per-send check, and
toggling the flag at runtime does nothing without a restart (`mailer.go:54`).

**Jobs require `APP_ENV=production` or `ENABLE_JOBS=1`** (`cmd/api/main.go:82`). A dev
instance sees nothing happen otherwise.

**`guardEnvDBCombo` refuses to boot** a non-production box pointed at a remote database unless
`ALLOW_REMOTE_DB=1` -- then `os.Exit(1)` (`main.go:133-150`). If someone reports "the server
won't start", the fix is usually an exported `DATABASE_URL` in their shell, not deleting the
guard.

`docker-compose.yml:46-49` defaults `APP_ENV` to `development` on purpose: "a forgotten
variable must fail SAFE".

## The job roster

There is no external scheduler -- no cron daemon, no queue service. Everything is an
in-process ticker (`jobs.go:20-21`). The full roster (`jobs.go:41-63`):

| Period | Job |
|---|---|
| 30s | `email-queue-worker` |
| 1h | `overdue-reminders`, `referral-recheck`, `early-bird-expiry`, `health-selfcheck` |
| 24h | `archive-announcements`, `email-tokens-purge`, `email-queue-prune` |
| daily 00:05 | monthly billing cron (separate scheduler) |

**Every ticker job also runs once immediately at startup**, so freshly-deployed servers do not
wait for the first tick (`jobs.go:275-277`). This is exactly why the production/`ENABLE_JOBS`
gate matters: jobs fire within milliseconds of a misconfigured boot. Each run is wrapped in
panic recovery so one buggy job cannot crash the API (`jobs.go:294-298`).

All timing uses `time.Now()` in the container's local zone, which compose pins to
`Asia/Kuala_Lumpur`; the Alpine image installs `tzdata` specifically so that resolves
(`docker-compose.yml:37-39`, `backend/Dockerfile:49-51`). Without it every date-boundary
decision shifts 8 hours.

## Monthly billing cron

Wakes daily at 00:05 local but **only acts on days 1-7** -- the day gate lives inside
`runMonthlyInvoiceCycle`, not the scheduler, so testing on the 15th silently no-ops
(`cron.go:107, 123, 132-134`). It also runs once at boot as catch-up.

The scheduled run and the manual admin trigger share **one advisory lock** (`"monthly_cron"`)
held on a dedicated `*sql.Conn` for the run's lifetime. A concurrent holder makes the cron
skip and the manual endpoint return 409. Acquiring via the pool would acquire and release on
different connections, so "a second concurrent click would NOT see the lock"
(`cron.go:140-153, 754-776`). Session advisory locks are connection-bound -- never move this
onto `db.QueryRow`.

`HandleRunMonthlyCron` responds **202 Accepted immediately** and runs the batch in a goroutine
holding that connection. Completion is reported only via the audit log; a 202 does not mean
invoices were created (`cron.go:778-793`).

Invoice emails are queued **after** the transaction commits, so a rollback never emails a
parent about an invoice that does not exist, and they go through the durable queue rather than
a direct send (`cron.go:462-463, 636-641`).

See `AI_DOCS/billing.md` for the pricing, dedup, and fail-closed rules inside this cron.

## Email queue

The worker claims rows by flipping them to `status='sending'` with a **10-minute lease** using
`FOR UPDATE SKIP LOCKED`, so two workers cannot double-send (`email_queue.go:66-71`).
`'sending'` is a lease, not a terminal state -- a worker crash mid-send means the row is
reclaimed after 10 minutes and may send twice. Delivery is at-least-once past the claim.

Retry is max 5 attempts with backoff 1m, 5m, 30m, 2h, 12h; hitting the cap sets
`status='failed'` permanently with `last_error` (`email_queue.go:19, 26-32, 105-107`). **Nothing
retries a failed row** -- recovery is a manual status flip.

A "stuck" queue means `status='pending'` with `next_attempt_at` more than 2 hours in the past
(`jobs.go:137`). Rows stuck in `'sending'` are invisible to this check.

`core.SendEmail` is nil-safe and **returns nil when no mailer is registered**
(`core/hooks.go:25-30`), so a process that skipped `mailer.Init()` would mark rows sent
without delivering anything.

Verification tokens are consumed atomically (`UPDATE ... RETURNING` validating and marking
used in one statement), and all invalid states collapse to one `ErrTokenInvalid` so callers
cannot leak "this token was used yesterday" (`email_tokens.go:78-91`). Splitting validation
from consumption reintroduces a double-redeem race.

## Reminders and early-bird clawback

Overdue reminders **claim before sending**: `reminder_sent_on` is set via a guarded UPDATE so
overlapping instances during a rolling deploy cannot both email the same parent, and the claim
is released on send failure (`jobs.go:433-450`). Reordering to send-then-mark reintroduces
double emails. Note these use `core.SendEmail` directly, not the queue.

At most one reminder per invoice per 3 days, and reminders are suppressed entirely on tenant
holidays (`jobs.go:357-358, 405-411, 459-461`). Holiday-skipped invoices are not marked, so
they retry next non-holiday tick; opted-out parents **are** marked, purely to keep the query
selective.

`applyEarlyBirdExpiry` mechanics are documented in `AI_DOCS/billing.md`.

Payroll generation (days 1-7, previous month) refreshes existing Pending non-hand-edited rows
in place. `Paid` or `manually_edited` rows are frozen, re-asserted in the UPDATE's WHERE
clause to survive races with admin edits (`cron.go:897, 905`). The insert has no `ON CONFLICT`
despite the `ux_payroll_staff_month` index, so a race errors and is logged rather than
silently absorbed.

Health self-check alerts (disk under 15%, backup older than 36h, stuck queue) are throttled
in-memory to once per 24h per category and emailed to `ALERT_EMAIL`; without that variable
they are only logged at ERROR. The throttle is in-memory only, so a restart re-arms all alerts
-- "the safe direction" (`jobs.go:71-86, 147-149`).

## Web push

Subscriptions are pruned only when the push service returns 404 or 410 (browsers rotate
endpoints on reinstall). There is no TTL, so this is the sole cleanup path
(`notify/webpush.go:120-124`).

Subscribe upserts on the unique `endpoint`, so re-subscribing the same browser refreshes keys
and **can reassign the subscription to a different tenant or parent** -- the endpoint belongs
to whoever subscribed it last, intentionally (`handlers_push.go:46-48`). The VAPID public key
is served unauthenticated because it is not a secret (`handlers_push.go:13-15`).

Check-in notifications on every channel are suppressed for families with an Unpaid or Overdue
Monthly invoice, but the gate **fails open on a query error**: "a check-in notification is a
safety signal -- suppressing it because a query hiccuped is worse than the rare case of one
alert slipping past the billing gate" (`notify/checkin_notify.go:61, 100-112`). Do not invert
this to fail closed. Check-in emails additionally require the `users.notify_checkin_email`
opt-in.

## WebSockets

**Was broken in production from the June restructure until 2026-09-01
(`89249be`).** Every upgrade failed: `core.statusRecorder` (metrics middleware)
embedded the `http.ResponseWriter` interface to capture status codes, which
strips the optional interfaces, so the handler's `w.(http.Hijacker)` assertion
failed. Everything below describes code that was correct throughout and simply
never ran. Guarded now by `TestMiddlewareKeepsWriterHijackable`, which checks
every core middleware, not just that one.

Connections are authenticated **before** the upgrade, parsing the JWT from the `sh_token`
cookie or a Bearer header with an HMAC-method check (`ws.go:125-145`). The route does its own
parse and bypasses the REST middleware chain -- revocation checks that middleware performs are
not applied here.

`broadcastCheckIn` scopes events so parents receive only their own child's, matched
case-insensitively on email; admin/teacher/superadmin see all (`ws.go:89-94`). Without this,
"every connected parent in the tenant received real-time attendance timing for every other
family's children" (`ws.go:86-87`). In `broadcastTenant`, `tenantID == 0` (superadmin)
receives every tenant's messages (`ws.go:107`).

Clients are removed from the hub **only** by the `HandleWS` defer when the read loop errors.
`deliver()` logs "dropping client" on write failure but does not actually remove it
(`ws.go:117-119, 161-166`) -- the log overstates what happens. Per-client write mutexes exist
because gorilla panics on concurrent writes to one connection (`ws.go:52`), and writes carry a
10s deadline.

Origins are allowlisted; plain `http://studyhub.fit` is intentionally omitted because
production is HTTPS-only (`ws.go:21-27`). `ALLOWED_ORIGIN` is appended at upgrade time.

## Fire-and-forget work

Use `goSafe(task, fn)`, never a bare `go`. chi's `Recoverer` only wraps the request goroutine,
so a panic in a detached task "would take down the whole API process for every user"
(`handlers/background.go:7-11`).
