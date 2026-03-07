(function() {
  window.App = window.App || {};
  const KEY = 'studyhub_v1';

  function deepClone(obj) {
    return JSON.parse(JSON.stringify(obj));
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

  function loadState() {
    try {
      const saved = localStorage.getItem(KEY);
      if (saved) return JSON.parse(saved);
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
      try { localStorage.setItem(KEY, JSON.stringify(_state)); } catch(e) {}
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
