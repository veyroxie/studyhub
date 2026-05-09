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

    // _request is the single low-level wrapper used by get/post/put/del.
    // Centralising it means error handling, 401 redirects, response parsing,
    // and toasting are consistent across every call site. Callers can pass
    // { silent: true } in opts to suppress automatic error toasts (useful
    // when the caller wants to display the error inline in a form).
    async _request(method, path, body, opts) {
      opts = opts || {};
      const init = {
        method: method,
        headers: body ? { 'Content-Type': 'application/json' } : {},
        credentials: 'include'
      };
      if (body !== undefined && body !== null) init.body = JSON.stringify(body);

      let res;
      try {
        res = await fetch(BASE + path, init);
      } catch (networkErr) {
        // Network failure (offline, DNS, CORS) — synthesise an error.
        const err = new Error('Network error — check your connection and try again');
        err.cause = networkErr;
        if (!opts.silent) this._toastError(err);
        throw err;
      }

      if (res.status === 401) { this._handle401(); return null; }
      if (res.status === 204) return null;

      // Try to parse JSON; if it isn't JSON, fall back to text.
      let payload;
      const ctype = res.headers.get('Content-Type') || '';
      if (ctype.indexOf('application/json') > -1) {
        payload = await res.json().catch(() => null);
      } else {
        payload = await res.text().catch(() => '');
      }

      if (!res.ok) {
        // Backend returns {error, request_id} for failures. Surface a useful
        // error message and attach the request ID so devs can grep logs.
        const message = (payload && payload.error) || (typeof payload === 'string' && payload) || ('Request failed (' + res.status + ')');
        const err = new Error(message);
        err.status = res.status;
        err.requestId = (payload && payload.request_id) || res.headers.get('X-Request-Id') || '';
        if (!opts.silent) this._toastError(err);
        throw err;
      }
      return payload;
    },

    // _toastError handles the show-a-toast side effect, gracefully degrading
    // if the toast helper isn't loaded yet (e.g. during early page load).
    _toastError(err) {
      try {
        if (window.App && App.Utils && App.Utils.showToast) {
          App.Utils.showToast(err.message || 'Something went wrong', 'error');
        }
      } catch(e) {}
      // Always console.error so failures aren't silent in dev tools.
      console.error('[App.Api]', err.message, err.requestId ? '(request_id=' + err.requestId + ')' : '', err);
    },

    async get(path, opts)        { return this._request('GET',    path, null, opts); },
    async post(path, body, opts) { return this._request('POST',   path, body, opts); },
    async put(path, body, opts)  { return this._request('PUT',    path, body, opts); },
    async del(path, opts)        { return this._request('DELETE', path, null, opts); },

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

              // For parents: only show toast for their own children, and
              // only when the family is up to date on Monthly invoices.
              // The same gate applies to progress reports + receipts so
              // the unpaid-invoice consequence is consistent everywhere.
              const isParent = App.currentRole === 'client';
              const isMyChild = isParent && stu && stu.contact === App.clientParent;

              const hasUnpaidMonthly = isParent && (state.invoices || []).some(function(i) {
                return i.type === 'Monthly' && (i.status === 'Unpaid' || i.status === 'Overdue');
              });

              if (!isParent || (isMyChild && !hasUnpaidMonthly)) {
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
              }).catch(function(err) { console.error('WS snapshot reload failed:', err); });
            }
          } catch(ex) { console.error('WS message error:', ex); }
        };
        ws.onerror = function() {
          console.error('WS error — will reconnect on close');
        };
        ws.onclose = function() {
          setTimeout(function() { App.Api.connectWS(); }, 5000);
        };
      } catch(e) { console.error('WebSocket connect failed', e); }
    }
  };
})();
