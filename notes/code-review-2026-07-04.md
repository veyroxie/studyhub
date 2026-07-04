# StudyHub full-code review — fix tickets (2026-07-04)

Senior review of the whole codebase (Go backend, vanilla JS frontend, Python analytics).
Every finding below was verified against the actual code (file:line evidence read, not guessed).

## Rules for the implementer (read first)

- Work ticket by ticket, in order. One Conventional Commit per ticket, e.g. `fix(staff): ...` — do NOT batch unrelated tickets into one commit.
- Follow project conventions: raw SQL with `?` placeholders, tenant scoping via `store.ScopeTenant`, `deleted_at IS NULL` on soft-deleted tables, `core.Respond/RespondError`, frontend strings escaped with `App.Utils.esc()`, no build step, IIFE modules on `window.App`.
- NEVER edit an already-shipped migration file (0001–0022 are checksum-tracked). New DDL = new numbered file. **Migration numbers are pre-assigned below: 0023 and 0024. Do not renumber.**
- After each backend ticket: `cd backend && go build ./... && go vet ./...` must pass. After each frontend ticket: `node --check frontend/js/<changed file>` must pass.
- Do NOT refactor beyond what a ticket says. Do NOT "fix" anything in the *Verified clean* list at the bottom.

---

## P0 — actively destructive or money-wrong today

### T1. "Save Notes" on staff performance tab wipes the entire staff record
**Files:** `frontend/js/modules/staff.js:214-232` (`_saveNotes`), `backend/internal/handlers/handlers_staff.go:171-182`, `backend/internal/models/models.go` (Staff struct), `backend/internal/store/database.go` (staff DDL), new migration.
**Problem:** `_saveNotes` PUTs `{ performanceNotes: notes }` only. The backend PUT decodes into a fresh `models.Staff` and runs a full-row `UPDATE staff SET name=?,full_name=?,role=?,email=?,phone=?,salary=?...` — every field the payload omitted becomes zero/empty. One click destroys the staff member's name, email, salary, NRIC, etc. Worse, `performanceNotes` doesn't exist on the backend model at all, so the note itself is dropped. The `.catch` also fakes success ("Saved locally (offline)").
**Fix:**
1. New migration `backend/internal/store/migrations/0023_staff_performance_notes.sql`:
   ```sql
   ALTER TABLE staff ADD COLUMN IF NOT EXISTS performance_notes TEXT DEFAULT '';
   ```
   Mirror the column into the `CREATE TABLE IF NOT EXISTS staff` block in `database.go`.
2. Add `PerformanceNotes string \`json:"performanceNotes,omitempty"\`` to `models.Staff`. Include `performance_notes` in the staff SELECT in `listStaff` (`handlers_staff.go:64`) and in the PUT UPDATE column list (`handlers_staff.go:181-182`). For parents, zero it in the same place other sensitive staff fields are zeroed (~`handlers_staff.go:88-105`).
3. In `_saveNotes` (staff.js), send the FULL object: `var s = App.Store.get().staff.find(function(x){return x.id===staffId;}); App.Api.put('/api/staff/' + staffId, Object.assign({}, s, { performanceNotes: notes }))`. Delete the `.catch` local-write branch entirely; on failure keep the textarea content (App.Api auto-toasts).
**Accept:** editing notes persists across reload; editing notes does NOT change any other staff field (verify by reloading snapshot and comparing name/salary).

### T2. Self-study sessions use `duration` but the backend field is `durationMin` — all usage math is 0
**Files:** `frontend/js/modules/attendance.js:543, 576, 581, 647`, `frontend/js/modules/students.js:550`. Backend truth: `models.go:379` `DurationMin int \`json:"durationMin"\``.
**Problem:** POSTing `{duration: X}` stores `duration_min = 0` (unknown JSON key ignored); reading `ss.duration` is always undefined. Result: used-hours, free-remaining and the RM10/hr overflow billing are permanently zero — self-study billing silently never triggers for UI-logged sessions.
**Fix:** rename at exactly these five sites: attendance.js:647 `duration: duration,` → `durationMin: duration,`; the three reducers attendance.js:543/576/581 and students.js:550 `ss.duration` → `ss.durationMin`.
**Accept:** log a 90-minute session in the UI, reload; the student profile self-study card shows 1.5h used, and `SELECT duration_min FROM self_study_sessions ORDER BY created DESC LIMIT 1` (or the snapshot payload) shows 90.

### T3. Duplicate `withLoading` — second definition overrides the real one and fakes success on failure
**Files:** `frontend/js/utils.js:323-335`.
**Problem:** The correct `async withLoading(btn, fn)` at utils.js:145 returns the promise. A second `App.Utils.withLoading = function(btn, asyncFn){... asyncFn().finally(...)}` at 323-335 executes later and silently replaces it — it returns undefined, so every `await App.Utils.withLoading(...)` caller (students.js:378/728/817, staff.js:399/589, billing edit forms) resolves immediately: success toast before the request finishes, and `catch` never fires on failure.
**Fix:** delete lines 323-335 (the whole `/* ── Loading-state helper ── */ App.Utils.withLoading = ...` block). Nothing else. The object-literal version at 145 already handles disable/spinner/restore.
**Accept:** `node --check` passes; in the app, submitting Edit Student with the network offline shows the error toast and does NOT show "updated".

### T4. Cron dedup guards fail-open — one transient DB error mass-duplicates invoices/payroll
**Files:** `backend/internal/jobs/cron.go` — `loadExistingMonthlyInvoiceStudentIDs` (~:280 caller / ~:558 function), overflow preloads `existingOverflow`/`existingRollover` (~:156/:171), payroll preload (~:746). New migration `0024`.
**Problem:** every dedup preload does `if rows, err := db.Query(...); err == nil { ...fill map... }` or returns an empty map on error. On a transient query failure the cron proceeds believing NOTHING exists and re-issues invoices/credits/payroll to everyone. The cron runs 7×/month plus every boot.
**Fix:**
1. Change `loadExistingMonthlyInvoiceStudentIDs` to return `(map[string]bool, error)`; in `generateMonthlyInvoices`, on error: `core.Logger.Error(...)` and `return 0`.
2. In `generateSelfStudyOverflowInvoices`: if either the `existingOverflow` or `existingRollover` preload query errors, log and `return 0` (do not proceed with empty maps).
3. In `generateMonthlyPayroll`: same — if the `existingPayroll` preload errors, log and `return 0`.
4. Backstop migration `backend/internal/store/migrations/0024_payroll_unique_month.sql`:
   ```sql
   CREATE UNIQUE INDEX IF NOT EXISTS ux_payroll_staff_month ON payroll(tenant_id, staff_id, month);
   ```
   **Deliberately do NOT add a unique index on invoices** — a student can legitimately have two Monthly-type invoices in one month (manual sibling invoice + admin adhoc), so the fail-closed dedup is the correct remedy there.
**Accept:** `go build/vet` pass; grep confirms no dedup preload path proceeds after `err != nil`.

### T5. Scheduled/boot cron takes no advisory lock — races the manual "Run monthly" endpoint into duplicates
**Files:** `backend/internal/jobs/cron.go` `runMonthlyInvoiceCycle` (~:109-126) vs the lock in `HandleRunMonthlyCron` (~:662-682).
**Problem:** the 00:05 tick and the boot-time run take no lock; the manual admin endpoint takes `core.AdvisoryLockKey("monthly_cron")`. Overlap (deploy at midnight during days 1-7, or admin clicking during the tick) → both preload dedup sets before either inserts → duplicate invoices + double referral decrements.
**Fix:** at the top of `runMonthlyInvoiceCycle`, acquire a dedicated conn and the SAME key the manual handler uses:
```go
conn, err := db.DB.Conn(context.Background())
if err != nil { core.Logger.Error("cron lock conn", "err", err); return }
defer conn.Close()
var got bool
if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, core.AdvisoryLockKey("monthly_cron")).Scan(&got); err != nil || !got {
    core.Logger.Info("monthly cron skipped — another run holds the lock")
    return
}
defer conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, core.AdvisoryLockKey("monthly_cron"))
```
Copy the exact conn/lock/unlock pattern already used in `HandleRegeneratePayroll` (~:909) — key name must match `HandleRunMonthlyCron`'s.
**Accept:** build/vet pass; both the scheduler path and manual handler reference the identical lock key.

### T6. Early-bird expiry bumps `amount` but leaves the "Early bird discount" line item — expired invoices' PDFs are off by RM10
**Files:** `backend/internal/jobs/jobs.go:165-183` (`applyEarlyBirdExpiry`); renders wrong in `backend/internal/pdf/pdf_invoices.go` `renderTotalsBlock`.
**Problem:** the single UPDATE adds `early_bird_discount` back to `amount` but `line_items` still contains `{"kind":"discount","name":"Early bird discount","amount":-10}` → the customer-facing PDF shows Subtotal − discounts ≠ Total Due.
**Fix:** replace the single UPDATE with a select-then-update inside one `db.BeginTx`:
1. `SELECT id, COALESCE(line_items,'[]') FROM invoices WHERE <same predicate as today>`.
2. Per row: `items := models.ParseLineItems(raw)`; remove entries where `Kind == models.LineItemKindDiscount && strings.HasPrefix(it.Name, "Early bird")` (prefix match — the manual-create path names it "Early bird discount (10%)").
3. `UPDATE invoices SET amount = amount + early_bird_discount, status='Overdue', early_bird_discount=0, early_bird_cutoff='', line_items=? WHERE id=?` with `models.MarshalLineItems(items)`.
4. Commit; keep the existing count-log + `SnapshotCacheInvalidateAll()`.
Also fix the stale comment "(the 10th)" → "(the 7th)" in the function docstring.
**Accept:** create an unpaid invoice with an early-bird line and a past cutoff, run the expiry (hourly job or call the function in a test), then confirm sum of line_items == amount and the PDF totals add up.

---

## P1 — auth gaps and shipped-but-broken features

### T7. Pending-users list not tenant-scoped — cross-tenant PII in every admin snapshot
**Files:** `backend/internal/handlers/handlers.go:128-129` (`listPendingUsers`), call site `handlers.go:265`.
**Fix:** change signature to `listPendingUsers(db *store.DB, c *core.Claims)`; add `tw, twArgs := store.ScopeTenant(c, "")` and append `+tw` to the WHERE, passing `twArgs...`. Update the call site.
**Accept:** query contains tenant scope; build passes.

### T8. User-create endpoint accepts arbitrary `role` — admin can self-provision superadmin
**Files:** `backend/internal/handlers/handlers_users.go:63-73`.
**Fix:** after the empty→"parent" default, reject anything outside `{"parent","teacher","admin"}` with `core.RespondError(w, "invalid role", 400)`. Superadmin must not be creatable here.
**Accept:** POST with `role:"superadmin"` returns 400.

### T9. Online payments (Billplz/Stripe webhooks) never assign a receipt number
**Files:** `backend/internal/handlers/payments.go:192-195` and `:348-351`; reference implementation `handlers_invoices.go:367-372`.
**Fix:** in both webhook handlers, after the status-transition UPDATE reports `RowsAffected() > 0`, run the same idempotent assignment: `UPDATE invoices SET receipt_no='RCPT-'||lpad(nextval('receipt_no_seq')::text,6,'0') WHERE id=? AND status='Paid' AND (receipt_no IS NULL OR receipt_no='')`. Log on error but do not fail the webhook response.
**Accept:** simulated webhook pay produces a row with a receipt_no; receipt PDF shows it.

### T10. "Cancel & Notify Parents" button is dead — `JSON.stringify` breaks the onclick attribute
**Files:** `frontend/js/modules/attendance.js:728`.
**Problem:** `onclick="App.Attendance._doCancelClasses(' + JSON.stringify(ids) + ')"` — the `"` in the JSON terminates the attribute; clicking throws SyntaxError.
**Fix:** pass a comma-joined string: `onclick="App.Attendance._doCancelClasses(\'' + ids.join(',') + '\')"` and at the top of `_doCancelClasses` do `if (typeof classIds === 'string') classIds = classIds.split(',').filter(Boolean);`.
**Accept:** the teacher-absent cancel flow completes without console errors.

### T11. Class-cancellation "notifications" are localStorage-only — parents are never actually notified
**Files:** `frontend/js/modules/attendance.js:752-794`; backend `handlers_cancelled.go:39-70`.
**Problem:** announcements + messages are pushed into `App.Store` only (wiped on next snapshot); errors are swallowed by `.catch(() => null)`; toast claims "Parents have been notified."
**Fix (frontend-scoped, smaller change):** in `_doCancelClasses`: (a) for each cancelled class, `App.Api.post('/api/announcements', {...})` with the same shape communication.js uses for creating announcements (title `'Class Cancelled: ' + cls.name`, message with date, audience parents); (b) delete the local `messages` mock entirely; (c) remove the per-call `.catch(function(){return null;})` so a failure rejects `Promise.all` and hits the outer catch (toast the failure, don't claim success).
**Accept:** cancelling a class creates a real announcement visible to a parent account after reload.

### T12. Teacher "Quick Note" never persists
**Files:** `frontend/js/modules/students.js:1076-1093` (`_saveQuickNote`); backend `handlers_students.go` PUT.
**Problem:** writes only to local store; note vanishes on next snapshot. NOTE: the students PUT does a full-row update and may be admin-gated; do NOT have teachers PUT the whole student.
**Fix (backend + frontend):**
1. New endpoint in `handlers_students.go`: `HandleStudentNote` — `POST /api/students/{id}/note`, allowed for `core.IsStaffRole(c)` (admin + teacher). Body `{note: string}` (validate non-empty, cap ~2000 chars). For teachers, verify the student is in `teacherClassIDSet` (same check as listStudents) — 403 otherwise. Append server-side: `UPDATE students SET notes = CASE WHEN COALESCE(notes,'')='' THEN ? ELSE notes || E'\n' || ? END WHERE id=?` + tenant scope + `deleted_at IS NULL`. Audit-log it. Register the route next to the other `/api/students/{id}/...` routes in server.go.
2. In `_saveQuickNote`: replace the store-write with `App.Api.post('/api/students/' + studentId + '/note', {note: newNote})` then `App.Api.loadSnapshot()` and toast on success only.
Note: teachers can't see `notes` in their redacted snapshot (by design — notes are admin-facing); the toast should say "Note sent to admin".
**Accept:** teacher saves a note; admin sees it appended on the student profile after reload; teacher for a different class's student gets 403.

### T13. Performance-review form fields don't exist on the backend — review text is dropped
**Files:** `frontend/js/modules/staff.js:117-135` (render), `:189-198` (submit); backend model `models.go:383-391` (`PerformanceReview`: ID, StaffID, ReviewerEmail, Date, Rating, ParentRating, Notes).
**Fix (map to existing model, no schema change):** in `_addReviewModal` submit, build `{staffId, rating, date: App.Utils.today(), notes: 'Period: ' + period + '\nStrengths: ' + strengths + '\nAreas to improve: ' + areas, reviewerEmail: <leave for backend to derive from claims if it does; otherwise omit>}`. Update the render (staff.js:117-135) to show `rv.notes` (pre-wrap) and `rv.reviewerEmail`, removing reads of `rv.period/rv.strengths/rv.areasToImprove/rv.reviewedBy`. Also remove that form's "Saved locally (offline)" catch (see T14).
**Accept:** add a review, reload snapshot, the text still renders.

### T14. Systemic "Saved locally (offline)" catches cache server rejections as success
**Files (each catch block to fix):** `staff.js:205-210, 224-230` · `attendance.js:656-661, 790-794` · `calendar.js:538-542, 700-710, 1135-1143, 1221-1227, 1298-1306, 1349-1355` · `billing.js:659-667`.
**Problem:** a 400/403/500 is treated like offline: fake row written to local store with client ID, success-ish toast, data silently vanishes on next snapshot. (CODE RED #6: error cached as success.) The mark-paid flow in billing.js was already fixed and has a comment explaining the policy (~billing.js:618-622) — replicate it.
**Fix:** in each listed catch: delete the `App.Store.set(...)` local write and the "Saved locally (offline)" toast; keep UI state (re-open modal / keep form) where practical; rely on App.Api's auto-toast. Do not add an outbox — out of scope.
**Accept:** with the API stopped, each affected action shows an error and does NOT show a fake row; `node --check` passes on all touched files.

### T15. Admin mark-paid proceeds without receipt when the upload fails
**Files:** `frontend/js/modules/billing.js:562-565` (mandatory gate) vs `:592-596` (catch that bypasses it).
**Fix:** in the upload `.catch`, do NOT call `_confirmPaid`. Re-enable the submit button, keep the modal open, toast `'Receipt upload failed — payment not recorded, please retry'`.
**Accept:** with upload endpoint failing, invoice stays Unpaid.

### T16. WebSocket never connects after a fresh interactive login
**Files:** `frontend/js/main.js` — login path (~:159-171) lacks the `App.Api.connectWS()` the session-restore path has (~:501-508).
**Fix:** add `App.Api.connectWS();` in `_doLogin` immediately after the snapshot await (next to `App.Notifs.updateBadge()`).
**Accept:** after form login (no reload), a check-in from another session shows the live toast.

---

## P2 — latent risks and multi-tenant readiness

### T17. Referral credit comment/code mismatch — per-child is INTENDED, just document it
**Decision (owner, 2026-07-04):** per-child referral credit is the desired behavior. A family with N children gets RM10 off each child's invoice, consuming credits N-per-month. Do NOT change the apply/decrement logic.
**Files:** `backend/internal/jobs/cron.go` — the `ReferralMonthlyRM` constant comment (~:23-25) and the referral block in `generateMonthlyInvoices`.
**Fix (comment only):** update the constant's docstring and any nearby comment so they state the credit is applied per enrolled child (one credit per child-invoice), not "per family for 3 months." Leave the code as-is.
**Known edge (acceptable, do not fix):** in the final month where remaining credits < child count, only some siblings get the discount that month (whichever the loop reaches while credits remain). Owner is fine with this.
**Accept:** comment matches code; no logic change.

### T18. Payroll refresh is check-then-act — can overwrite a concurrent admin edit
**Files:** `backend/internal/jobs/cron.go` refresh UPDATE (~:791-797).
**Fix:** make the guard atomic and use live bonus/deductions: `UPDATE payroll SET base_salary=?, total=? + bonus - deductions WHERE id=? AND status='Pending' AND COALESCE(manually_edited,false)=FALSE` (args: `total, total, ex.id`); only `refreshed++` when `RowsAffected()==1`. Remove the now-redundant in-Go `newTotal` computation.
**Accept:** build/vet pass; a row flipped to manually_edited between preload and update is untouched.

### T19. `LoadTenantSettings` caches the default fallback on DB error for 10 minutes
**Files:** `backend/internal/store/tenant_settings.go:100-105`.
**Fix:** split handling: `err == nil` or `sql.ErrNoRows` → cache as today. Any other error → `core.Logger.Error("tenant settings load failed", "err", err, "tenant_id", tenantID)` and `return DefaultTenantSettings` WITHOUT writing the cache (next call retries).
**Accept:** code path on generic error skips the cache write.

### T20. RLS GUC is set on a pooled connection — the "safety net" isn't bound to the request
**Files:** `backend/internal/store/rls.go:35`.
**Problem:** `db.Exec("SET app.tenant_id = ...")` borrows an arbitrary pooled conn; subsequent queries run on other conns with the GUC unset. RLS silently doesn't filter; only explicit ScopeTenant clauses protect.
**Fix (documentation-honesty now, real fix later):** this is an architecture change (per-request `*sql.Conn` or per-request tx). For THIS ticket: correct the comments in rls.go and any doc claiming RLS actively guards requests, stating explicitly that RLS is enforced only for connections that ran the SET (e.g. superadmin tools), and add a `// TODO(rls-binding)` referencing this review. Do NOT attempt the conn-routing refactor without a dedicated plan.
**Accept:** comments no longer overstate the guarantee.

### T21. Receipt numbers come from one global sequence — not per-tenant (gapless-per-business expectation)
**Files:** `handlers_invoices.go:369`, `payments.go` (after T9), sequence from migration 0020.
**Fix:** new table in a future migration `tenant_receipt_counters(tenant_id INT PRIMARY KEY, next_no INT NOT NULL DEFAULT 1)`; assignment becomes `UPDATE tenant_receipt_counters SET next_no = next_no + 1 WHERE tenant_id=? RETURNING next_no` (insert-on-missing first), formatted `RCPT-000123`, inside the pay transaction. Keep the only-when-Paid-and-empty idempotency. Single-tenant today, so this can wait until just before onboarding tenant #2 — but do it before then.
**Accept:** two tenants each get contiguous numbering.

### T22. Manual cron run mid-month creates born-overdue invoices with an already-clawed-back discount
**Files:** `backend/internal/jobs/cron.go` `generateMonthlyInvoices` due-date block (~:384-390) + `HandleRunMonthlyCron` bypassing the day guard.
**Fix:** in `generateMonthlyInvoices`, when `now.Day() > 7`: set `dueDate = now.AddDate(0,0,7).Format("2006-01-02")`, `earlyBirdCutoff = ""`, force `earlyBirdApplied = 0` (skip the discount subtraction and the early-bird line item + email note). Mirror the due-date clamp in `generateSelfStudyOverflowInvoices`.
**Accept:** manual run on the 20th produces full-price invoices due in 7 days, no early-bird anywhere on them.

### T23. Feedback replies can be posted by any authenticated user into any thread
**Files:** `backend/internal/handlers/handlers_feedback.go:397-425`.
**Fix:** after decode, resolve the feedback row tenant-scoped (`SELECT class_id FROM feedback WHERE id=? AND deleted_at IS NULL` + tw); 404 if missing. Then: parent → require the class in the parent's children's enrolled classes (there is an existing helper pattern; if none, build the set from `students WHERE contact=?` enrolled_classes); teacher → require `teacherClassIDSet(db, c)[classID]`; admin → allow.
**Accept:** parent with no child in the class gets 403.

### T24. Parent dashboard links to a page that doesn't exist ("View all feedback →")
**Files:** `frontend/js/modules/dashboard.js:708`, `frontend/js/tutorial.js:73-74`; `js/modules/feedback.js` is unloaded dead code.
**Fix:** dashboard.js:708 → `App.Router.navigate('progress')`, label "View progress reports →". tutorial.js: retarget the step to `[data-page="progress"]` or delete the step. Move `feedback.js` to the same archived state as messages.js (comment header noting it's replaced by progress.js) — do not delete history.
**Accept:** parent clicking the link lands on Progress; tutorial doesn't reference a dead selector.

---

## P3 — hygiene / consistency

### T25. Self-study overflow join misses tenant equality
`cron.go:186` — add `AND ss.tenant_id = s.tenant_id` to the LEFT JOIN in `generateSelfStudyOverflowInvoices` (defends the cross-tenant STU_id-collision case the monthly cron already defends).

### T26. Parent invoice snapshot shows soft-deleted students' invoices
`store/snapshot_bounded.go:115` — add `AND s.deleted_at IS NULL` to the parent branch of `ListInvoicesRecent` (matches `ListAttendanceRecent`).

### T27. Snapshot invalidation asymmetry in the cron
`cron.go` — move `store.SnapshotCacheInvalidateAll()` out of `generateMonthlyInvoices` into `runMonthlyInvoiceCycle` (and the goroutine in `HandleRunMonthlyCron`), called once when `created+overflow+payrolls > 0`.

### T28. Progress-report PDF: mojibake + tenant-1 branding hardcoded
`pdf/pdf_progress.go` — (a) add `tr := pdf.UnicodeTranslatorFromDescriptor("")` and wrap every printed string (student/teacher names, all `pr.*` fields, brand, the literal "—" placeholders); (b) change `RenderProgressReportPDF` to accept `*store.TenantSettings` (caller `HandleProgressReportPDF` loads via `store.LoadTenantSettings(db, store.TenantID(c))`) instead of `mailer.Brand()` which always loads tenant 1.

### T29. `HandleInvoicePay` re-pay isn't idempotent
`handlers_invoices.go:358` — when the new status is "Paid", add `AND status<>'Paid'` to the UPDATE and gate `store.ReferralCheckMilestoneOnPay` on `RowsAffected() > 0` (preserves original paid_on, prevents double milestone).

### T30. Announcements reject superadmin
`handlers_announcements.go:90` — `if !core.IsAdminRole(c) && c.Role != "teacher" { 403 }`.

### T31. Family delete: no idempotency guard, unconditional 204
`handlers_families.go:170` — append `AND deleted_at IS NULL`, check `RowsAffected()`, 404 on zero. Matches `HandleInvoiceDelete`.

### T32. Chart.js loader rejections unhandled
`analytics.js` — the six `_loadChartLib().then(...)` chains need a shared `.catch(function(){ App.Utils.showToast('Charts failed to load', 'warning'); })` (small helper `_withCharts(buildFn)` is fine).

### T33. `_editClassModal` writes back a stale captured `state`
`calendar.js:1148/1216/1222` — inside the submit success path, re-read `App.Store.get().classes` at write time instead of the `state` captured at modal open.

### T34. `esc()` erases legitimate 0/false
`utils.js:294` — `String(str == null ? '' : str)` instead of `String(str || '')`.

### T35. Python: `datetime.utcnow()` deprecated
`services/analytics/main.py:122` — `from datetime import datetime, timezone`; use `datetime.now(timezone.utc).isoformat().replace('+00:00','Z')`.

---

## Verified clean — do NOT "fix" these
- SQL injection: all queries use `?` placeholders; the only string-built identifiers are hardcoded literals.
- XSS: user-string interpolations traced through `App.Utils.esc()`; onclick IDs are server/client-generated (T10 is the one real quoting break).
- Parent-role data exposure: snapshot filtering is server-side (students/invoices/attendance/self-study scoped by contact email; staff sensitive fields zeroed for parents; payroll admin-only). Teacher redaction shipped 2026-07-04.
- Webhook HMAC checks are constant-time; login lockout increments atomic; CSRF double-submit + single-flight refresh in api.js sound.
- Monthly/overflow/rollover dedup KEYS are correct (the fail-open on error, T4, is the only hole).
- `NormalizeLineItems` rounding, monthly-cron tx boundaries (emails after commit), Content-Disposition filenames (server IDs), migration checksums/advisory locks, `(tenant_id, deleted_at)` indexes.
- Payroll refresh freeze semantics (aside from T18 atomicity) and `manually_edited` flow shipped 2026-07-04.
