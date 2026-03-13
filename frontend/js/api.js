(function() {
  window.App = window.App || {};

  // In production, frontend and backend are same origin (Go serves both).
  // In local dev, backend runs on :8080.
  const BASE = window.location.hostname === 'localhost' || window.location.hostname === '127.0.0.1'
    ? 'http://localhost:8080'
    : ''; // same origin in production

  // All fetch calls include credentials so the browser sends the HttpOnly cookie.
  // The JWT token lives in an HttpOnly cookie — JS never touches it directly.
  const OPTS = { credentials: 'include' };

  App.Api = {
    _user: null,

    // Check if user is logged in by calling /api/auth/me (reads the cookie server-side)
    async isLoggedIn() {
      try {
        const res = await fetch(BASE + '/api/auth/me', OPTS);
        if (!res.ok) return false;
        this._user = await res.json();
        return true;
      } catch(e) {
        return false;
      }
    },

    // Returns cached user info (populated after isLoggedIn or login)
    currentUser() {
      return this._user || null;
    },

    async login(email, password) {
      const res = await fetch(BASE + '/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
        credentials: 'include'
      });
      if (!res.ok) {
        const msg = await res.text();
        throw new Error(msg || 'Invalid email or password');
      }
      const user = await res.json();
      this._user = user;
      return user;
    },

    async logout() {
      this._user = null;
      await fetch(BASE + '/api/auth/logout', { method: 'POST', credentials: 'include' });
    },

    // Load ALL data in one call and hydrate App.Store
    async loadSnapshot() {
      const res = await fetch(BASE + '/api/snapshot', OPTS);
      if (res.status === 401) { this._handle401(); return; }
      const data = await res.json();
      App.Store.set(data);
      return data;
    },

    async get(path) {
      const res = await fetch(BASE + path, OPTS);
      if (res.status === 401) { this._handle401(); return null; }
      return res.json();
    },

    async post(path, body) {
      const res = await fetch(BASE + path, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
        credentials: 'include'
      });
      if (res.status === 401) { this._handle401(); return null; }
      if (!res.ok) { const t = await res.text(); throw new Error(t); }
      return res.status === 204 ? null : res.json();
    },

    async put(path, body) {
      const res = await fetch(BASE + path, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
        credentials: 'include'
      });
      if (res.status === 401) { this._handle401(); return null; }
      if (!res.ok) { const t = await res.text(); throw new Error(t); }
      return res.status === 204 ? null : res.json();
    },

    async del(path) {
      const res = await fetch(BASE + path, { method: 'DELETE', credentials: 'include' });
      if (res.status === 401) { this._handle401(); return null; }
      if (!res.ok) { const t = await res.text(); throw new Error(t); }
      return null;
    },

    // After any mutation, reload the snapshot so all modules stay in sync
    async refresh() {
      await this.loadSnapshot();
      App.Router.refresh();
    },

    _handle401() {
      this._user = null;
      App.Login.show('Session expired. Please log in again.');
    },

    // WebSocket for real-time attendance notifications
    connectWS() {
      const host = BASE.replace('http://','').replace('https://','') || window.location.host;
      const wsProto = window.location.protocol === 'https:' ? 'wss://' : 'ws://';
      try {
        const ws = new WebSocket(wsProto + host + '/ws');
        ws.onmessage = function(e) {
          try {
            const data = JSON.parse(e.data);
            if (data.type === 'CHECK_IN' || data.type === 'CHECK_OUT') {
              const state = App.Store.get();
              const students = state.students || [];
              const stu = students.find(function(s) { return s.id === data.personId; });
              const name = stu ? stu.firstName + ' ' + stu.lastName : data.personId;
              const time = data.checkIn || data.checkOut || '';

              // For parents: only show toast for their own children
              const isParent = App.currentRole === 'client';
              const isMyChild = isParent && stu && stu.contact === App.clientParent;

              if (!isParent || isMyChild) {
                if (data.type === 'CHECK_IN') {
                  App.Utils.showToast(name + ' checked in at ' + App.Utils.formatTime(time), 'info');
                } else {
                  App.Utils.showToast(name + ' checked out at ' + App.Utils.formatTime(time), 'success');
                }
              }

              // Reload data so notification panel + dashboard update
              App.Api.loadSnapshot().then(function() {
                if (App.Notifs) App.Notifs.refresh();
                if (App.Router.current() === 'attendance' || App.Router.current() === 'dashboard') {
                  App.Router.refresh();
                }
              });
            }
          } catch(ex) { console.error('WS message error:', ex); }
        };
        ws.onerror = function() {
          console.error('WS error — will reconnect on close');
        };
        ws.onclose = function() {
          setTimeout(function() { App.Api.connectWS(); }, 5000);
        };
      } catch(e) {}
    }
  };
})();
