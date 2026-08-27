(function() {
  window.App = window.App || {};
  // Bump this suffix to force every browser to discard its cached state on
  // next visit — the old key becomes orphaned and the loader falls back to
  // App.DATA defaults, so every device effectively starts fresh.
  const KEY = 'studyhub_v2';
  // One-shot cleanup so the old key doesn't sit in localStorage forever.
  try { localStorage.removeItem('studyhub_v1'); } catch (e) {}

  const _structuredClone = typeof structuredClone === 'function' ? structuredClone : null;
  function deepClone(obj) {
    if (_structuredClone) {
      try { return _structuredClone(obj); } catch (e) { /* fall through */ }
    }
    return JSON.parse(JSON.stringify(obj));
  }

  // Debounced persistence: every set() updates _state synchronously, but the
  // localStorage.setItem (which serializes ~100KB of state) is coalesced. A
  // burst of edits — e.g. an admin toggling 30 attendance rows — produces
  // one write 400ms after the last change, not 30 sequential serializations.
  let _persistTimer = null;
  function _persistNow() {
    if (_persistTimer) { clearTimeout(_persistTimer); _persistTimer = null; }
    try { localStorage.setItem(KEY, JSON.stringify(_state)); } catch (e) {}
  }
  function _persistSoon() {
    if (_persistTimer) return;
    _persistTimer = setTimeout(function() {
      _persistTimer = null;
      try { localStorage.setItem(KEY, JSON.stringify(_state)); } catch (e) {}
    }, 400);
  }
  // Flush pending write before the tab is unloaded so a quick close does
  // not drop the user's last few mutations.
  if (typeof window !== 'undefined') {
    window.addEventListener('pagehide', _persistNow);
  }

  function validate(patch) {
    if (patch.students) {
      patch.students = patch.students.map(s => ({
        ...s,
        firstName: String(s.firstName || '').trim().slice(0, 100),
        lastName: String(s.lastName || '').trim().slice(0, 100),
        phone: String(s.phone || '').replace(/[^\d+]/g, '').slice(0, 20),
        contact: String(s.contact || '').trim().slice(0, 200),
        amount: s.amount !== undefined ? Math.max(0, parseFloat(s.amount) || 0) : s.amount
      }));
    }
    if (patch.invoices) {
      patch.invoices = patch.invoices.map(inv => ({
        ...inv,
        amount: Math.max(0, parseFloat(inv.amount) || 0),
        description: String(inv.description || '').trim().slice(0, 200)
      }));
    }
    if (patch.staff) {
      patch.staff = patch.staff.map(s => ({
        ...s,
        salary: Math.max(0, parseFloat(s.salary) || 0),
        fullName: String(s.fullName || '').trim().slice(0, 100)
      }));
    }
    return patch;
  }

  const ARRAY_DEFAULTS = {
    feedback: [], pricingTiers: [], workshops: [], selfStudySessions: [],
    performanceReviews: [], cancelledClasses: [], messages: [], holidays: [],
    sessionMoves: [],
    replacementCredits: [],
    families: [],
    feedbackReplies: [],
    referralRewards: [],
    registrations: [],
    pendingUsers: []
  };

  function loadState() {
    try {
      const saved = localStorage.getItem(KEY);
      if (saved) {
        const parsed = JSON.parse(saved);
        // Ensure new array fields exist even if saved state predates them
        for (const k in ARRAY_DEFAULTS) {
          if (!Array.isArray(parsed[k])) parsed[k] = ARRAY_DEFAULTS[k];
        }
        return parsed;
      }
    } catch(e) {}
    return deepClone(App.DATA);
  }

  let _state = loadState();
  const _listeners = [];

  App.Store = {
    get() { return _state; },
    set(patch) {
      const validated = validate(deepClone(patch));
      Object.assign(_state, validated);
      _persistSoon();
      _listeners.forEach(fn => fn(_state));
    },
    subscribe(fn) {
      _listeners.push(fn);
      return () => { const i = _listeners.indexOf(fn); if (i > -1) _listeners.splice(i, 1); };
    },
    reset() {
      localStorage.removeItem(KEY);
      _state = deepClone(App.DATA);
      _listeners.forEach(fn => fn(_state));
    },
    exportJSON() {
      const blob = new Blob([JSON.stringify(_state, null, 2)], { type: 'application/json' });
      const a = document.createElement('a');
      a.href = URL.createObjectURL(blob);
      a.download = 'studyhub-backup-' + new Date().toISOString().slice(0,10) + '.json';
      a.click();
    },
    importJSON(jsonStr) {
      try {
        const parsed = JSON.parse(jsonStr);
        const required = ['students','classes','staff','invoices','announcements','attendance','payroll'];
        for (const key of required) {
          if (!Array.isArray(parsed[key])) throw new Error('Missing: ' + key);
        }
        _state = parsed;
        localStorage.setItem(KEY, JSON.stringify(_state));
        _listeners.forEach(fn => fn(_state));
        return true;
      } catch(e) {
        return false;
      }
    }
  };
})();
