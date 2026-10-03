// A tab left open across a deploy keeps running the old code against the new
// server, which is how stale screens and odd errors reached Nadine until she
// pressed Ctrl+Shift+R. Every API reply carries X-App-Version; when it differs
// from the version this page loaded, the app refreshes itself at a safe moment:
// the next page change, or coming back to the tab with no popup open. A bar
// offers "Refresh now" meanwhile, so half-typed work is never thrown away.
(function() {
  'use strict';
  window.App = window.App || {};

  var UNSET = '__APP_VERSION__'; // the token, when the shell was not served by the API (local file)
  var _pending = false;

  function _loaded() {
    var v = window.__appVersion || '';
    return v === UNSET ? '' : v;
  }

  // check is given each API reply's version; a different, real version means a deploy.
  function check(serverVersion) {
    var mine = _loaded();
    if (_pending || !serverVersion || !mine || serverVersion === mine) return;
    _pending = true;
    _showBar();
  }

  function _popupOpen() {
    var overlay = document.getElementById('modal-overlay');
    return !!overlay && !overlay.classList.contains('hidden');
  }

  // refreshIfPending is the safe moment: no popup holding unsaved input.
  function refreshIfPending() {
    if (!_pending || _popupOpen()) return false;
    window.location.reload();
    return true;
  }

  function _showBar() {
    if (document.getElementById('update-bar')) return;
    var bar = document.createElement('div');
    bar.id = 'update-bar';
    bar.setAttribute('role', 'status');
    bar.style.cssText = 'position:fixed;left:50%;transform:translateX(-50%);top:0.75rem;z-index:9999;'
      + 'background:#111;color:#fff;padding:0.6rem 0.9rem;border-radius:6px;font-size:0.82rem;'
      + 'display:flex;gap:0.75rem;align-items:center;box-shadow:0 4px 16px rgba(0,0,0,0.2);max-width:calc(100vw - 2rem)';
    bar.innerHTML = '<span>StudyHub was updated. It will refresh when you next change page.</span>'
      + '<button type="button" style="background:var(--gold,#C9A227);color:#0a0a0a;border:none;border-radius:4px;padding:0.35rem 0.7rem;font-weight:700;cursor:pointer;min-height:36px">Refresh now</button>';
    bar.querySelector('button').addEventListener('click', function() { window.location.reload(); });
    document.body.appendChild(bar);
  }

  document.addEventListener('visibilitychange', function() {
    if (document.visibilityState === 'visible') refreshIfPending();
  });

  App.Update = { check: check, refreshIfPending: refreshIfPending, isPending: function() { return _pending; } };
})();
