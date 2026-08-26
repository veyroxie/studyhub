# Frontend module contract

Read this before adding or editing anything under `frontend/js/`. Vanilla ES6, IIFE modules,
`window.App` namespace, no framework, no npm. There IS a build step, but it runs in
`backend/Dockerfile`, not locally -- see "Build" at the bottom.

## Module shape

Every module opens with the same IIFE and assigns its surface at the bottom
(`modules/students.js:1-2, 1765`):

```js
// frontend/js/modules/widgets.js
(function() {
  window.App = window.App || {};

  function render(container) {
    const { students } = App.Store.get();     // must tolerate empty pre-snapshot state
    const isAdmin = App.currentRole === 'admin';
    container.innerHTML = /* full repaint; App.Utils.esc every user string */ '';
  }

  App.Widgets = {
    render: render
    // handlers referenced from inline onclick="App.Widgets._x('id')" go here
  };
})();
```

A module written as ESM, or one that overwrites `window.App` instead of merging, breaks every
script loaded before it. Do not add `export` "for testability" -- the test harness runs these
files unmodified in a `node:vm` sandbox where the sandbox object **is** `window`, because "the
modules do `window.App = ...` and then reference bare `App`, which only resolves because in a
browser `window` is the global object" (`tests/unit/_load.mjs:2, 45, 55`).

## Registering a page

Four places, all required:

1. `index.html`: `<div id="widgets-page" class="page"></div>` among the page divs (~517-527).
2. `index.html`: nav buttons carrying `data-page="widgets"` -- both the sidebar `.nav-btn`
   (~299) and the dock `.dock-btn` (~371).
3. `index.html`: `<script src="js/modules/widgets.js" defer></script>` **before** the
   `main.js` tag (609-627).
4. `main.js`: `App.Router.register('widgets', App.Widgets)` inside DOMContentLoaded (435-446),
   plus a `TITLES` entry in `router.js:4-15`.

Script order is fixed and load-bearing: data, store, api, utils, router, theme, notifs, push,
tutorial, then feature modules, then `main.js` **last**. `main.js` references module
namespaces at DOMContentLoaded, so a module script placed after it registers `undefined`. A
module with no page div silently no-ops (`router.js:34`).

**`analytics.js` is the deliberate exception** -- it is lazy-injected by a loader in
`index.html` after `main.js` has already run its registrations, so it must register **itself**
with the router on load. `main.js:444-445` warns that registering it there "would store
undefined", and `index.html:640-646` warns the page div otherwise "stays blank (can't see
stats)". Do not copy either pattern to the other kind of module.

Role-restricted pages need an entry in **both** `pageHidden` maps -- `main.js:310-317` and
`theme.js:194-201`. They already disagree (one lists `progress`, the other `feedback`), so
updating one leaves sidebar and dock showing different nav sets.

Roles on the frontend are `App.currentRole` (`'admin' | 'teacher' | 'client'`),
`App.clientParent`, `App.currentTeacher`, initialised from sessionStorage
(`main.js:276-278`). **The JWT role `parent` maps to the frontend role `client`**
(`main.js:185`) -- filtering on `=== 'parent'` never matches.

## Lifecycle

`App.Router.register(pageId, module)` then `navigate(pageId)` toggles active classes, sets the
title, and calls `module.render(page)` (`router.js:21-49`). **Render-on-show is the only
lifecycle hook** -- there is no mount, unmount, or destroy. A module attaching global listeners
in `render` leaks them on every navigation.

`App.Router.refresh()` re-renders the current page by calling `render` again against the page
element (`router.js:51-56`). This is the app-wide "data changed, repaint" mechanism, so renders
must be idempotent full repaints of `container.innerHTML`. Incremental DOM patching is not the
house pattern.

Boot is **shell-first**, both on login and session restore: the nav and empty cards paint in
~50ms while the snapshot loads in the background, then the active module re-renders
(`main.js:209-224, 427-591`). **A module's first render runs before snapshot data exists** --
it must tolerate empty store collections rather than assume data has loaded. Do not "fix" this
by awaiting the snapshot before navigating.

## App.Api

Four verbs, all delegating to `_request(method, path, body, opts)` (`api.js:253-256`):
`get(path, opts)`, `post(path, body, opts)`, `put(path, body, opts)`, `del(path, opts)`.

Never use raw `fetch` -- it bypasses the CSRF header, the 401 refresh-and-retry, error
toasting, and request-id capture.

- **Errors auto-toast** unless you pass `{ silent: true }` (`api.js:174-175, 235`). A module
  that toasts its own error without it shows two toasts for one failure.
- **Responses are not unwrapped.** `_request` returns the parsed JSON body (or text, or `null`
  on 204/401). On failure it throws an `Error` with `.status` and `.requestId` populated from
  the backend's `{error, request_id}` shape (`api.js:217-238`).
- **On a first 401** (except for `/api/auth/refresh` itself) it single-flights a silent token
  refresh and retries once; a second 401 wipes local data, shows the login screen, and
  **returns `null` rather than throwing** (`api.js:208-216, 264-268`). Callers must handle a
  null return after session expiry.
- The refresh is single-flighted deliberately: parallel rotations are what "the server would
  treat as token reuse and burn the whole session" (`api.js:45-50`).
- Non-GET requests carry `X-CSRF-Token` read from the `sh_csrf` cookie; the JWT itself is
  HttpOnly and never touched by JS (`api.js:11-23, 179-184`).
- `API_BASE` defaults to same-origin. Hardcoding `:8080` previously sent the `:8081` test
  stack's auth calls to a different server, 401-ing everything (`api.js:4-9`).

### Data flow after a mutation

```js
await App.Api.post('/api/students', payload);
await App.Api.refresh();          // loadSnapshot() + Router.refresh()  (api.js:258-262)
```

Mutating server state without the snapshot reload leaves every other module rendering stale
data. `index.html:17-21` preloads `/api/snapshot` with `crossorigin="use-credentials"` during
HTML parse so the fetch races the JS modules -- changing that URL or its credentials mode
silently kills the optimisation.

Deletes are **optimistic**, with a documented rollback pattern (`api.js:137-157`):

```js
var prev = App.Api.optimisticRemove('invoices', invoiceId);
try { await App.Api.del('/api/invoices/' + invoiceId); }
catch (err) { App.Store.set({invoices: prev}); throw err; }
App.Router.refresh();
App.Api.loadSnapshot();           // background reconcile, no await
```

On logout or a hard 401, `_clearLocalData` wipes `studyhub_v2`, `sh_remember`, and the
sessionStorage role keys, because the cached snapshot "holds student PII, medical info,
allergies, staff NRIC/salary" and would otherwise survive logout on a shared device
(`api.js:114-126`). **A module that caches user data under its own key must add it there.**

## App.Store

`App.Store.get()` returns the state object; `App.Store.set(patch)` validates, `Object.assign`s
at the **top level only**, debounce-persists to localStorage `studyhub_v2`, and notifies
subscribers (`store.js:97-108`). Patching a nested field means replacing the whole collection
array. A `subscribe` mechanism exists, but the house pattern is explicit `App.Router.refresh()`.

`set` **sanitises** `students`, `invoices`, and `staff` patches -- trimming and capping string
lengths, clamping amounts and salary to `>= 0` (`store.js:40-66`). The store will not
necessarily hold exactly what you wrote for those three.

A new store collection must be added to `ARRAY_DEFAULTS` or saved states predating it crash on
`undefined.filter` (`store.js:68-94`). `App.DATA` (`data.js`) is only the offline/demo fallback
-- editing it does not change production data.

## App.Utils -- reuse, do not rewrite

`showModal` / `hideModal`, `showToast`, `showConfirm`, `withLoading`, `filterFor` /
`filterTarget`, `copyFrom`, `emptyState`, `badge` / `statusBadge` / `colorClasses`,
`formatDate` / `formatMonth` / `formatCurrency` / `formatTime`, `localDate` / `today` /
`nowTime`, `generateId`, `esc`. Rewriting any of these locally loses the bug fixes baked into
them.

- `hideModal(force)` prompts "You have unsaved changes. Discard?" unless forced, driven by a
  dirty flag set on input/change. Call `hideModal(true)` after a successful save
  (`utils.js:55-107`).
- `showConfirm({...})` returns `Promise<boolean>` and **escapes `message` by default** --
  "the old raw-by-default contract was one interpolated name away from XSS". Trusted markup
  goes in `messageHtml` (`utils.js:428-433`).
- `showToast` builds with text nodes, never `innerHTML`, because the message can be a server
  error string or a broadcast student name (`utils.js:164-194`). Do not "improve" it to render
  HTML.
- `formatCurrency` guards non-numeric strings because `amount || 0` once let "RM NaN" reach
  parents on an invoice; the absence of a thousands separator is **pinned by a test** as a
  deliberate choice (`utils.js:243-247`, `utils.test.mjs:48-50`).

There is an app-wide capture-phase **double-submit guard** on forms, released on the next
input/change or after 1200ms, because double-clicks created duplicate announcements, progress
reports, and classes. Holding it longer would dead-button a validation-bail resubmit
(`utils.js:512-536`). A module adding its own submit-disable can fight it.

## XSS: what esc() does and does not cover

`App.Utils.esc(str)` escapes `&` first, then `"`, `'`, `<`, `>`, and renders null/undefined as
`''` (`utils.js:297-299`). The `&`-first order is pinned by a test.

**It does not protect a value inlined into a JS string literal** -- e.g. inside
`onclick="fn('...')"`. The browser decodes entities before the JS parser runs, so such values
must travel via a data attribute instead (`utils.js:338-341`). "Escape then inline into an
inline handler" is an XSS hole that looks defended.

The ~60 existing inline handlers that interpolate ids into JS strings are safe **only because
`generateId` produces a quote-free charset**, which the test file asserts explicitly
(`utils.test.mjs:103-106`). Interpolate ids there, never names. Note `filterFor`'s `targetId`
lands in an `oninput` attribute the same way -- it must be a caller-controlled literal
(`utils.js:300-331`).

Notification `body` is a caller-escaped HTML slot -- escape at construction, because
render-time escaping would double-escape; `title` is escaped at render
(`notifs.js:333-344`).

## Dates

`toISOString().slice(0,10)` is **banned** for calendar dates. It returns the UTC day, which in
UTC+8 is still "yesterday" until 08:00 local -- the bug that mis-dated check-ins, credits, and
self-study rows. Use `App.Utils.localDate` / `today()` / `nowTime()`
(`utils.js:357-361`, `utils.test.mjs:5-8`).

## Tests

`frontend/tests/unit/*.mjs`, run with **`TZ=Asia/Kuala_Lumpur node --test frontend/tests/unit/`**.
The pinned TZ is mandatory: CI runs in UTC, so without it the date-helper tests "would pass
locally and fail there (or worse, vice versa)" (`utils.test.mjs:3-8`).

**CI does not run these tests** -- the frontend job only does `node --check` syntax parsing on
every JS file (`.github/workflows/ci.yml:76-98`). Run them yourself.

## Build

`backend/Dockerfile` has a `tailwind-builder` stage that compiles `frontend/tailwind.in.css`
with the Tailwind v4.3.0 standalone CLI and overlays the result into the runtime image
(`Dockerfile:21-37, 58-60`). No npm is involved.

`tailwind.in.css` lists `@source` globs covering the five HTML files and `js/**/*.js`. **A new
HTML page needs a new `@source` line**, or none of its classes are compiled and it silently
renders unstyled.

The same stage minifies every `frontend/js/**/*.js` in place with esbuild
`--target=es2019`, preserving the defer order and the lazy analytics load
(`Dockerfile:42-43`). Keep JS within es2019. `styles.css` is intentionally left unminified --
esbuild's CSS parser rejects some of the `@keyframes` patterns (`Dockerfile:44-46`).

Serving `frontend/` outside Docker uses the committed `tailwind.css`, which may be stale, so a
new utility class can look broken locally while being correct in production.
