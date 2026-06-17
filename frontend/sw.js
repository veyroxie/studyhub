// Service worker — offline shell + network-first for API.
//
// Strategy:
//   - Static assets (JS/CSS/HTML/fonts) → cache-first with stale revalidation.
//     Parents on patchy mobile networks still see the shell on cold boot.
//   - /api/* → network-first, no cache (snapshots are tenant-scoped + auth-
//     gated; caching would leak data across logins on the same device).
//   - On install: prefetch the app shell so first offline open works.
//
// Versioned cache name so a deploy invalidates the old shell. Bump SW_VERSION
// on every release (release-please can wire this).

const SW_VERSION = 'v2026-06-17-2';
const SHELL_CACHE = 'sh-shell-' + SW_VERSION;

// Files that must be available offline for the app to render its empty shell.
// Keep this tight — long lists slow down install + waste cache quota.
const SHELL = [
  '/',
  '/index.html',
  '/styles.css',
  '/js/main.js',
  '/js/api.js',
  '/js/utils.js',
  '/js/store.js',
  '/js/router.js',
  '/js/theme.js',
  '/manifest.json',
];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(SHELL_CACHE).then((cache) =>
      // addAll fails the whole install if any entry 404s. Use individual
      // adds with catch so a missing optional file doesn't block install.
      Promise.all(SHELL.map((url) =>
        cache.add(url).catch((e) => console.warn('SW: skip', url, e))
      ))
    ).then(() => self.skipWaiting())
  );
});

self.addEventListener('activate', (event) => {
  // Drop caches from previous versions.
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys
        .filter((k) => k.startsWith('sh-shell-') && k !== SHELL_CACHE)
        .map((k) => caches.delete(k))
      )
    ).then(() => self.clients.claim())
  );
});

// Web push — backend sends a JSON payload on student check-in/out. Show it as
// a system notification so parents are alerted even with the app closed.
self.addEventListener('push', (event) => {
  let data = {};
  try { data = event.data ? event.data.json() : {}; } catch (e) { data = {}; }
  const title = data.title || 'The Study Hub';
  const options = {
    body: data.body || '',
    tag: data.tag || 'studyhub',
    icon: '/icon-192.png',
    badge: '/icon-192.png',
    data: { url: data.url || '/' },
  };
  event.waitUntil(self.registration.showNotification(title, options));
});

// Clicking a notification focuses an existing tab (navigating it to the target)
// or opens a new one.
self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const target = (event.notification.data && event.notification.data.url) || '/';
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clients) => {
      for (const c of clients) {
        if ('focus' in c) {
          if ('navigate' in c) c.navigate(target);
          return c.focus();
        }
      }
      if (self.clients.openWindow) return self.clients.openWindow(target);
    })
  );
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (req.method !== 'GET') return;

  const url = new URL(req.url);

  // Never cache API responses or websocket — these are auth-scoped and
  // a returning-from-suspend cache hit could surface another user's data.
  if (url.pathname.startsWith('/api/') || url.pathname === '/ws') {
    return; // default network behavior
  }

  // Same-origin GETs: NETWORK-FIRST so a deploy is picked up immediately —
  // cache-first previously served stale JS/CSS until the SW version changed,
  // which silently hid deploys. Fall back to cache only when the network fails
  // (offline). API/ws already bypass this above.
  if (url.origin === self.location.origin) {
    event.respondWith(
      fetch(req).then((res) => {
        if (res && res.status === 200) {
          const copy = res.clone();
          caches.open(SHELL_CACHE).then((c) => c.put(req, copy));
        }
        return res;
      }).catch(() => caches.match(req).then((cached) => cached || new Response(
        'You appear to be offline.',
        { status: 503, headers: { 'Content-Type': 'text/plain' } }
      )))
    );
  }
});
