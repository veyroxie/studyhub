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

  // _csrfToken reads the server-issued sh_csrf cookie. The server sets this
  // on any response where the request didn't already carry it (see the Go
  // csrfMiddleware). State-changing requests echo it back in X-CSRF-Token
  // as the second half of the double-submit pattern — attackers on another
  // origin can't read the cookie, so they can't forge the header.
  function _csrfToken() {
    var m = document.cookie.match(/(?:^|;\s*)sh_csrf=([^;]+)/);
    return m ? decodeURIComponent(m[1]) : '';
  }

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

    // _tryRefresh attempts a silent access-token rotation. Returns true on
    // success. Single-flighted so concurrent 401s from a tab don't kick
    // off N parallel rotations (which the server would treat as token
    // reuse and burn the whole session). The first 401 starts the
    // rotation; subsequent ones wait on the same promise.
    _refreshInFlight: null,
    async _tryRefresh() {
      if (!this._refreshInFlight) {
        const headers = {};
        const csrf = _csrfToken();
        if (csrf) headers['X-CSRF-Token'] = csrf;
        this._refreshInFlight = fetch(BASE + '/api/auth/refresh', {
          method: 'POST',
          headers: headers,
          credentials: 'include'
        }).then(function(r) {
          return r.ok;
        }).catch(function() {
          return false;
        }).finally(() => { this._refreshInFlight = null; });
      }
      return this._refreshInFlight;
    },

    async login(email, password, rememberMe) {
      // Login is CSRF-exempt server-side (no session yet), but we still
      // send the header if available — keeps the flow consistent and
      // primes the cookie for the post-login session.
      const headers = { 'Content-Type': 'application/json' };
      const csrf = _csrfToken();
      if (csrf) headers['X-CSRF-Token'] = csrf;
      const res = await fetch(BASE + '/api/auth/login', {
        method: 'POST',
        headers: headers,
        body: JSON.stringify({ email, password, rememberMe: !!rememberMe }),
        credentials: 'include'
      });
      if (!res.ok) {
        const msg = await res.text();
        throw new Error(msg || 'Invalid email or password');
      }
      const user = await res.json();
      // MFA challenge: no cookie was issued; the caller must collect a TOTP
      // code and call mfaVerify with the intermediate token.
      if (user.mfaRequired) return user;
      this._user = user;
      return user;
    },

    // mfaVerify exchanges the intermediate token + a 6-digit TOTP code (or a
    // recovery code) for the real auth cookie. Returns the login user object.
    async mfaVerify(interimToken, code, isRecovery) {
      const body = { token: interimToken };
      if (isRecovery) { body.recoveryCode = code; } else { body.code = code; }
      const headers = { 'Content-Type': 'application/json' };
      const csrf = _csrfToken();
      if (csrf) headers['X-CSRF-Token'] = csrf;
      const res = await fetch(BASE + '/api/auth/mfa/verify', {
        method: 'POST', headers: headers, body: JSON.stringify(body), credentials: 'include'
      });
      if (!res.ok) {
        const msg = await res.text();
        throw new Error(msg || 'Code did not match');
      }
      const user = await res.json();
      this._user = user;
      return user;
    },

    // _clearLocalData wipes every trace of the signed-in user from browser
    // storage: the cached snapshot (which holds student PII, medical info,
    // allergies, staff NRIC/salary), the remember flag, and the role/parent
    // selectors. Without this, a shared/family device leaks the whole snapshot
    // to the next person straight out of DevTools after "logout".
    _clearLocalData() {
      try { App.Store.reset(); } catch (e) {}
      try { localStorage.removeItem('studyhub_v2'); } catch (e) {}
      try { localStorage.removeItem('sh_remember'); } catch (e) {}
      ['sh_role','sh_parent','sh_teacher','sh_dash_view','sh_notif_read'].forEach(function(k) {
        try { sessionStorage.removeItem(k); } catch (e) {}
      });
    },

    async logout() {
      this._user = null;
      this._clearLocalData();
      var headers = {};
      var csrf = _csrfToken();
      if (csrf) headers['X-CSRF-Token'] = csrf;
      await fetch(BASE + '/api/auth/logout', { method: 'POST', headers: headers, credentials: 'include' });
    },

    // optimisticRemove pulls a row out of the local Store immediately so
    // the UI updates without waiting for a full snapshot round-trip.
    // Returns the previous list so callers can restore on API failure.
    //
    //   collection — top-level key in App.Store ('students','invoices',...)
    //   id         — primary key string
    //
    // Pattern at the call site:
    //   var prev = App.Api.optimisticRemove('invoices', invoiceId);
    //   try { await App.Api.del('/api/invoices/' + invoiceId); }
    //   catch (err) { App.Store.set({invoices: prev}); throw err; }
    //   App.Router.refresh();
    //   App.Api.loadSnapshot();   // background reconcile, no await
    optimisticRemove(collection, id) {
      var state = App.Store.get();
      var prev = state[collection] || [];
      var next = prev.filter(function(x) { return x && x.id !== id; });
      var patch = {}; patch[collection] = next;
      App.Store.set(patch);
      return prev;
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
      const headers = body ? { 'Content-Type': 'application/json' } : {};
      // Attach CSRF header on state-changing requests. GET / HEAD / OPTIONS
      // are exempt server-side so it's not required there.
      if (method && method !== 'GET' && method !== 'HEAD' && method !== 'OPTIONS') {
        const csrf = _csrfToken();
        if (csrf) headers['X-CSRF-Token'] = csrf;
      }
      const init = {
        method: method,
        headers: headers,
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

      // Silent refresh: a 401 on the first attempt could be a freshly
      // expired access token. Try /api/auth/refresh once; on success
      // re-run the original request with the new cookie. If refresh
      // itself fails (no/invalid refresh cookie), fall back to the
      // existing logout path.
      if (res.status === 401 && !opts._retried && path !== '/api/auth/refresh') {
        const refreshed = await this._tryRefresh();
        if (refreshed) {
          opts._retried = true;
          return this._request(method, path, body, opts);
        }
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
      this._clearLocalData();
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
