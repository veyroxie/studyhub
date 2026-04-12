(function() {
  window.App = window.App || {};

  // ── Loading overlay ──────────────────────────────────────────────────────
  function _showLoading(msg) {
    var el = document.getElementById('loading-overlay');
    if (!el) {
      el = document.createElement('div');
      el.id = 'loading-overlay';
      el.style.cssText = 'position:fixed;inset:0;z-index:9999;display:flex;align-items:center;justify-content:center;background:rgba(15,15,15,0.6);backdrop-filter:blur(2px)';
      el.innerHTML = '<div style="text-align:center;padding:2rem 2.5rem;background:#fff;border-radius:16px;box-shadow:0 4px 24px rgba(0,0,0,0.15)">'
        + '<div id="loading-spinner" style="width:36px;height:36px;border:3px solid #e2e8f0;border-top-color:var(--gold,#C9A227);border-radius:50%;animation:spin 0.7s linear infinite;margin:0 auto 1rem"></div>'
        + '<div id="loading-text" style="font-size:0.9rem;font-weight:600;color:#334155">' + (msg || 'Loading...') + '</div>'
        + '</div>';
      var style = document.createElement('style');
      style.textContent = '@keyframes spin{to{transform:rotate(360deg)}}';
      document.head.appendChild(style);
      document.body.appendChild(el);
    } else {
      var txt = document.getElementById('loading-text');
      if (txt) txt.textContent = msg || 'Loading...';
      el.style.display = 'flex';
    }
  }
  function _hideLoading() {
    var el = document.getElementById('loading-overlay');
    if (el) el.style.display = 'none';
  }

  // ── Login ─────────────────────────────────────────────────────────────────
  App.Login = {
    show(msg) {
      const errEl = document.getElementById('login-error');
      if (errEl && msg) { errEl.textContent = msg; errEl.classList.remove('hidden'); }
      document.getElementById('login-screen').classList.remove('hidden');
      document.getElementById('app').classList.add('hidden');
    },
    hide() {
      document.getElementById('login-screen').classList.add('hidden');
      document.getElementById('app').classList.remove('hidden');
    },
    async quickLogin(email, password) {
      await App.Login._doLogin(email, password);
    },
    async _doLogin(email, password) {
      const btn = document.getElementById('login-btn');
      const errEl = document.getElementById('login-error');
      const resendBanner = document.getElementById('resend-verify-banner');
      if (btn) { btn.textContent = 'Signing in...'; btn.disabled = true; }
      errEl.classList.add('hidden');
      if (resendBanner) resendBanner.classList.add('hidden');
      try {
        _showLoading('Signing in...');
        const data = await App.Api.login(email, password);
        App.currentRole = data.role === 'admin' ? 'admin' : (data.role === 'teacher' ? 'teacher' : 'client');
        sessionStorage.setItem('sh_role', App.currentRole);
        if (data.role === 'parent') {
          App.clientParent = data.email;
          sessionStorage.setItem('sh_parent', data.email);
        }
        if (data.role === 'teacher') {
          App.currentTeacher = data.staffId || '';
          sessionStorage.setItem('sh_teacher', App.currentTeacher);
        }
        // Load all data from backend
        _showLoading('Loading your data...');
        await App.Api.loadSnapshot();
        _hideLoading();
        App.Login.hide();
        App.Theme.init();
        applyRole();
        App.Dev.init();
        App.Router.navigate('dashboard');
        App.Notifs.updateBadge();
        if (App.Billing && App.Billing.checkLoginNotifications) App.Billing.checkLoginNotifications();
        App.IdleTimeout.start();
        if (App.Tutorial) App.Tutorial.autoStart();
      } catch(err) {
        _hideLoading();
        const msg = err.message || 'Login failed';
        // Backend signals "email not yet verified" by prefixing the error
        // with "needs_verification:". We show a friendlier UI in that case
        // and offer a one-click resend instead of just a generic error.
        if (msg.indexOf('needs_verification') > -1) {
          errEl.classList.add('hidden');
          if (resendBanner) {
            resendBanner.classList.remove('hidden');
            // Stash the email so the resend link knows who to send to.
            resendBanner.dataset.email = email;
          }
        } else {
          errEl.textContent = msg;
          errEl.classList.remove('hidden');
        }
      } finally {
        if (btn) { btn.textContent = 'Sign In'; btn.disabled = false; }
      }
    },
    // resendVerification fires the backend resend endpoint and confirms with
    // a toast. The endpoint is enumeration-safe (always returns 200) so this
    // never fails on the client.
    async resendVerification(email) {
      try {
        await App.Api.post('/api/resend-verification', { email: email }, { silent: true });
        App.Utils.showToast('Verification email sent — check your inbox', 'success');
      } catch(err) {
        App.Utils.showToast(err.message || 'Could not send verification email', 'error');
      }
    }
  };

  // Role state
  App.currentRole   = sessionStorage.getItem('sh_role')    || 'admin';
  App.clientParent  = sessionStorage.getItem('sh_parent')  || '';
  App.currentTeacher= sessionStorage.getItem('sh_teacher') || '';

  function applyRole() {
    const isAdmin   = App.currentRole === 'admin';
    const isTeacher = App.currentRole === 'teacher';
    const isClient  = App.currentRole === 'client';

    // Role button
    const roleBtn = document.getElementById('role-toggle-btn');
    if (roleBtn) {
      const labels = { admin:'Admin', teacher:'Teacher', client:'Parent View' };
      roleBtn.textContent = labels[App.currentRole] || 'Admin';
      roleBtn.className = 'flex items-center gap-2 px-3 py-1.5 text-sm font-semibold rounded-lg border-2 transition-all '
        + (isAdmin   ? 'bg-blue-50   text-blue-700   border-blue-300'
         : isTeacher ? 'bg-purple-50 text-purple-700 border-purple-300'
         :             'bg-emerald-50 text-emerald-700 border-emerald-300');
    }

    // Admin tools visibility (Export/Import/Reset)
    const adminTools = document.getElementById('admin-tools');
    if (adminTools) adminTools.classList.toggle('hidden', !isAdmin);

    // Selector visibility
    const parentSel  = document.getElementById('parent-selector-wrap');
    const teacherSel = document.getElementById('teacher-selector-wrap');
    if (parentSel)  parentSel.classList.toggle('hidden',  !isClient);
    if (teacherSel) teacherSel.classList.toggle('hidden', !isTeacher);

    // Nav visibility per role
    // admin:   all pages
    // teacher: dashboard, calendar, students, attendance, feedback, communication
    // client:  dashboard, calendar, communication, billing
    const pageHidden = {
      billing:    isTeacher,
      staff:      !isAdmin,
      analytics:  !isAdmin,
      students:   isClient,
      attendance: false,
      feedback:   false
    };
    Object.keys(pageHidden).forEach(function(page) {
      const btn = document.querySelector('.nav-btn[data-page="' + page + '"]');
      if (!btn) return;
      const hide = pageHidden[page];
      const li = btn.closest('li');
      if (li) li.classList.toggle('hidden', hide); else btn.classList.toggle('hidden', hide);
    });

    // Hide empty nav group labels for non-admin roles
    document.querySelectorAll('.nav-group-label[id]').forEach(function(label) {
      var next = label.nextElementSibling;
      var hasVisible = false;
      while (next && !next.classList.contains('nav-group-label')) {
        if (next.classList.contains('nav-btn') || next.querySelector && next.querySelector('.nav-btn')) {
          var navEl = next.classList.contains('nav-btn') ? next : next;
          if (!navEl.classList.contains('hidden')) { hasVisible = true; break; }
        }
        next = next.nextElementSibling;
      }
      label.classList.toggle('hidden', !hasVisible);
    });

    // Redirect if on a now-hidden page
    const current = App.Router.current();
    const hiddenPages = Object.keys(pageHidden).filter(function(p) { return pageHidden[p]; });
    if (hiddenPages.indexOf(current) > -1) {
      App.Router.navigate('dashboard');
    } else if (current) {
      App.Router.refresh();
    }

    App.Notifs.refresh();

    // Sync dock (theme B) visibility
    if (App.Theme && App.Theme.syncDockRole) App.Theme.syncDockRole();
    if (App.Theme && App.Theme.syncDockBadge) App.Theme.syncDockBadge();
  }

  function toggleRole() {
    const cycle = ['admin', 'teacher', 'client'];
    const next  = cycle[(cycle.indexOf(App.currentRole) + 1) % cycle.length];
    App.currentRole = next;
    sessionStorage.setItem('sh_role', next);
    // Default teacher to first staff member if not set
    if (next === 'teacher' && !App.currentTeacher) {
      const { staff } = App.Store.get();
      App.currentTeacher = (staff[0] && staff[0].id) || 's1';
      sessionStorage.setItem('sh_teacher', App.currentTeacher);
    }
    applyRole();
    const labels = { admin:'Admin', teacher:'Teacher', client:'Parent' };
    App.Utils.showToast('Switched to ' + (labels[next] || next) + ' view', 'info');
  }

  function onParentChange(val) {
    App.clientParent = val;
    sessionStorage.setItem('sh_parent', val);
    App.Router.refresh();
  }

  function exportData() {
    App.Store.exportJSON();
    App.Utils.showToast('Data exported!', 'success');
  }

  function importData() {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.json';
    input.onchange = function(e) {
      const file = e.target.files[0];
      if (!file) return;
      const reader = new FileReader();
      reader.onload = function(ev) {
        const ok = App.Store.importJSON(ev.target.result);
        if (ok) {
          App.Utils.showToast('Data imported successfully!', 'success');
          App.Router.refresh();
        } else {
          App.Utils.showToast('Import failed — invalid file format', 'error');
        }
      };
      reader.readAsText(file);
    };
    input.click();
  }

  function resetData() {
    if (!confirm('Reset all data to defaults? This cannot be undone.')) return;
    App.Store.reset();
    App.Utils.showToast('Data reset to defaults', 'info');
    App.Router.refresh();
  }

  document.addEventListener('DOMContentLoaded', function() {
    // Set header date
    var headerDateEl = document.getElementById('header-date');
    if (headerDateEl) {
      headerDateEl.textContent = new Date().toLocaleDateString('en-MY', { month: 'short', day: 'numeric', year: 'numeric' });
    }

    // Register all modules
    App.Router.register('dashboard',     App.Dashboard);
    App.Router.register('calendar',      App.Calendar);
    App.Router.register('communication', App.Communication);
    // App.Router.register('messages',   App.Messages);  // archived
    App.Router.register('students',      App.Students);
    App.Router.register('billing',       App.Billing);
    App.Router.register('staff',         App.Staff);
    App.Router.register('attendance',    App.Attendance);
    App.Router.register('feedback',      App.Feedback);
    App.Router.register('analytics',     App.Analytics);

    // Init router (sets up nav button click handlers)
    App.Router.init();

    // Populate parent selector
    const parentSelect = document.getElementById('parent-select');
    if (parentSelect) {
      const { students } = App.Store.get();
      const uniqueParents = {};
      students.forEach(function(s) { uniqueParents[s.contact] = s.parentName; });
      parentSelect.innerHTML = Object.keys(uniqueParents).map(function(email) {
        return '<option value="' + email + '">' + uniqueParents[email] + '</option>';
      }).join('');
      if (App.clientParent) parentSelect.value = App.clientParent;
      else App.clientParent = parentSelect.value || Object.keys(uniqueParents)[0] || '';
    }

    // Wire up global actions
    const roleBtn = document.getElementById('role-toggle-btn');
    if (roleBtn) roleBtn.addEventListener('click', toggleRole);

    const parentSel = document.getElementById('parent-select');
    if (parentSel) parentSel.addEventListener('change', function() { onParentChange(this.value); });

    const teacherSel = document.getElementById('teacher-select');
    if (teacherSel) teacherSel.addEventListener('change', function() {
      App.currentTeacher = this.value;
      sessionStorage.setItem('sh_teacher', this.value);
      App.Router.refresh();
      App.Dev._update();
    });

    document.getElementById('export-btn') && document.getElementById('export-btn').addEventListener('click', exportData);
    document.getElementById('import-btn') && document.getElementById('import-btn').addEventListener('click', importData);
    document.getElementById('reset-btn') && document.getElementById('reset-btn').addEventListener('click', resetData);
    document.getElementById('logout-btn') && document.getElementById('logout-btn').addEventListener('click', function() {
      App.IdleTimeout.stop();
      App.Api.logout().then(function() { App.Login.show(); });
    });

    // Listen for parent notifications in client mode
    try {
      const ch = new BroadcastChannel('studyhub_notifs');
      ch.onmessage = function(e) {
        if (App.currentRole === 'client') {
          const data = e.data;
          const msg = data.type === 'CHECK_IN'
            ? data.student + ' arrived at class at ' + App.Utils.formatTime(data.time)
            : data.student + ' has left class at ' + App.Utils.formatTime(data.time);
          App.Utils.showToast('📱 ' + msg, data.type === 'CHECK_IN' ? 'info' : 'success');
        }
      };
    } catch(e) {}

    // Wire login form
    const loginForm = document.getElementById('login-form');
    if (loginForm) {
      loginForm.addEventListener('submit', function(e) {
        e.preventDefault();
        const email = document.getElementById('login-email').value;
        const password = document.getElementById('login-password').value;
        App.Login._doLogin(email, password);
      });
    }

    // Wire resend-verification link inside the "email not verified" banner.
    var resendLink = document.getElementById('resend-verify-link');
    if (resendLink) {
      resendLink.addEventListener('click', function(e) {
        e.preventDefault();
        var banner = document.getElementById('resend-verify-banner');
        var email = (banner && banner.dataset.email) || document.getElementById('login-email').value;
        if (!email) { App.Utils.showToast('Enter your email above first', 'warning'); return; }
        App.Login.resendVerification(email);
      });
    }

    // Wire forgot password link
    var forgotLink = document.getElementById('forgot-password-link');
    if (forgotLink) {
      forgotLink.addEventListener('click', function(e) {
        e.preventDefault();
        App.Utils.showModal(
          '<div class="p-6" style="min-width:340px">'
          + '<h2 style="font-size:1.15rem;font-weight:700;color:#fff;margin-bottom:0.25rem">Forgot Password</h2>'
          + '<p style="font-size:0.82rem;color:#94a3b8;margin-bottom:1.25rem">Enter your email and we\'ll send a reset link.</p>'
          + '<form id="forgot-pw-form" class="space-y-4">'
          + '<div><label style="display:block;font-size:0.82rem;font-weight:600;color:#cbd5e1;margin-bottom:0.35rem">Email</label>'
          + '<input name="email" type="email" required placeholder="your@email.com" class="form-input" style="width:100%;padding:0.55rem 0.75rem;font-size:0.85rem;border:1px solid #e2e8f0;border-radius:10px"></div>'
          + '<div style="display:flex;justify-content:flex-end;gap:0.75rem;padding-top:0.5rem">'
          + '<button type="button" onclick="App.Utils.hideModal()" style="padding:0.45rem 1rem;font-size:0.82rem;border:1px solid #e2e8f0;border-radius:8px;background:transparent;color:#64748b;cursor:pointer">Cancel</button>'
          + '<button type="submit" style="padding:0.45rem 1rem;font-size:0.82rem;font-weight:700;background:#3b82f6;color:#fff;border:none;border-radius:8px;cursor:pointer">Reset Password</button>'
          + '</div>'
          + '</form>'
          + '</div>'
        );
        document.getElementById('forgot-pw-form').addEventListener('submit', function(ev) {
          ev.preventDefault();
          var fd = new FormData(ev.target);
          var email = fd.get('email');
          App.Api.post('/api/forgot-password', { email: email }, { silent: true }).then(function() {
            App.Utils.hideModal(true);
            App.Utils.showToast('If an account exists for that email, a password reset link has been sent. Check your inbox.', 'success', 10000);
          }).catch(function() {
            App.Utils.hideModal(true);
            App.Utils.showToast('If an account exists for that email, a password reset link has been sent. Check your inbox.', 'success', 10000);
          });
        });
      });
    }

    // Check if already logged in (reads HttpOnly cookie server-side)
    App.Api.isLoggedIn().then(function(loggedIn) {
      if (loggedIn) {
        const user = App.Api.currentUser();
        App.currentRole = (user && user.role === 'admin') ? 'admin' : (user && user.role === 'teacher') ? 'teacher' : 'client';
        if (user && user.role === 'parent') App.clientParent = user.email;
        if (user && user.role === 'teacher') { App.currentTeacher = user.staffId || ''; sessionStorage.setItem('sh_teacher', App.currentTeacher); }
        _showLoading('Loading your data...');
        return App.Api.loadSnapshot().then(function() {
          _hideLoading();
          App.Login.hide();
          App.Theme.init();
          App.Dev.init();
          applyRole();
          App.Router.navigate('dashboard');
          App.Notifs.updateBadge();
          App.Api.connectWS();
          App.IdleTimeout.start();
          if (App.Tutorial) App.Tutorial.autoStart();
        });
      } else {
        App.Login.show();
      }
    });
  });

  // Dev mode detection
  App.isDevMode = function() {
    var h = window.location.hostname;
    return h === 'localhost' || h === '127.0.0.1' || new URLSearchParams(window.location.search).has('dev');
  };

  // ========================
  // DEV TOOLBAR
  // ========================
  App.Dev = {
    setRole: function(role) {
      App.currentRole = role;
      sessionStorage.setItem('sh_role', role);
      if (role === 'teacher' && !App.currentTeacher) {
        const { staff } = App.Store.get();
        App.currentTeacher = (staff[0] && staff[0].id) || 's1';
        sessionStorage.setItem('sh_teacher', App.currentTeacher);
      }
      applyRole();
      App.Dev._update();
      App.Utils.showToast('Dev: switched to ' + role + ' view', 'info');
    },
    setParent: function(email) {
      App.clientParent = email;
      sessionStorage.setItem('sh_parent', email);
      const headerSel = document.getElementById('parent-select');
      if (headerSel) headerSel.value = email;
      App.Router.refresh();
      App.Dev._update();
    },
    setTeacher: function(staffId) {
      App.currentTeacher = staffId;
      sessionStorage.setItem('sh_teacher', staffId);
      const headerSel = document.getElementById('teacher-select');
      if (headerSel) headerSel.value = staffId;
      App.Router.refresh();
      App.Dev._update();
    },
    _update: function() {
      const isAdmin   = App.currentRole === 'admin';
      const isTeacher = App.currentRole === 'teacher';
      const isClient  = App.currentRole === 'client';

      // Role buttons
      ['admin','teacher','client'].forEach(function(r) {
        const btn = document.getElementById('dev-' + r + '-btn');
        const active = App.currentRole === r;
        const colors = { admin:'bg-blue-600 border-blue-500', teacher:'bg-purple-600 border-purple-500', client:'bg-emerald-600 border-emerald-500' };
        if (btn) btn.className = 'flex-1 py-1.5 text-xs font-semibold rounded-lg border transition-all '
          + (active ? colors[r] + ' text-white' : 'border-slate-600 text-slate-400 hover:bg-slate-700');
      });

      // Selector visibility
      const parentWrap  = document.getElementById('dev-parent-wrap');
      const teacherWrap = document.getElementById('dev-teacher-wrap');
      if (parentWrap)  parentWrap.classList.toggle('hidden',  !isClient);
      if (teacherWrap) teacherWrap.classList.toggle('hidden', !isTeacher);

      // Status line
      const statusEl = document.getElementById('dev-status');
      if (statusEl) {
        if (isAdmin) {
          statusEl.innerHTML = '<span class="text-blue-400 font-semibold">Admin</span> — full access';
        } else if (isTeacher) {
          const { staff } = App.Store.get();
          const t = staff.find(function(s) { return s.id === App.currentTeacher; });
          statusEl.innerHTML = '<span class="text-purple-400 font-semibold">Teacher</span>: ' + App.Utils.esc(t ? t.fullName : App.currentTeacher);
        } else {
          const { students } = App.Store.get();
          const myStudents = students.filter(function(s) { return s.contact === App.clientParent; });
          const names = myStudents.map(function(s) { return App.Utils.esc(s.firstName); }).join(', ');
          statusEl.innerHTML = '<span class="text-emerald-400 font-semibold">Parent</span>: ' + App.Utils.esc(App.clientParent || '—')
            + '<br>Children: <span class="text-white">' + (names || 'none') + '</span>';
        }
      }
    },
    init: function() {
      if (!App.isDevMode()) {
        var tb = document.getElementById('dev-toolbar');
        if (tb) tb.style.display = 'none';
        var rb = document.getElementById('role-toggle-btn');
        if (rb) rb.style.display = 'none';
        var trb = document.getElementById('top-role-btn');
        if (trb) trb.style.display = 'none';
        // Hide dev quick-login buttons on login screen
        var ql = document.querySelector('#login-screen .border-t.border-slate-700');
        if (ql) ql.style.display = 'none';
        return;
      }
      // Dev mode — show toolbar
      var tb = document.getElementById('dev-toolbar');
      if (tb) tb.style.display = '';
      const { students, staff } = App.Store.get();
      const uniqueParents = {};
      students.forEach(function(s) { uniqueParents[s.contact] = s.parentName; });

      const devSel = document.getElementById('dev-parent-select');
      if (devSel) {
        devSel.innerHTML = Object.keys(uniqueParents).map(function(email) {
          return '<option value="' + email + '">' + uniqueParents[email] + ' (' + email + ')</option>';
        }).join('');
        if (App.clientParent) devSel.value = App.clientParent;
        else App.clientParent = Object.keys(uniqueParents)[0] || '';
      }

      const devTeacherSel = document.getElementById('dev-teacher-select');
      if (devTeacherSel) {
        devTeacherSel.innerHTML = staff.map(function(s) {
          return '<option value="' + s.id + '">' + App.Utils.esc(s.fullName) + '</option>';
        }).join('');
        if (App.currentTeacher) devTeacherSel.value = App.currentTeacher;
        else App.currentTeacher = (staff[0] && staff[0].id) || '';
      }

      // Also populate header teacher selector
      const headerTeacherSel = document.getElementById('teacher-select');
      if (headerTeacherSel) {
        headerTeacherSel.innerHTML = staff.map(function(s) {
          return '<option value="' + s.id + '">' + App.Utils.esc(s.fullName) + '</option>';
        }).join('');
        if (App.currentTeacher) headerTeacherSel.value = App.currentTeacher;
      }

      App.Dev._update();
    }
  };

  // ── Session Idle Timeout ──────────────────────────────────────────────────
  (function() {
    var IDLE_LIMIT   = 30 * 60 * 1000; // 30 minutes
    var WARN_BEFORE  = 60 * 1000;      // warn 60s before logout
    var CHECK_INTERVAL = 15 * 1000;    // check every 15s
    var _lastActivity = Date.now();
    var _warned = false;
    var _intervalId = null;

    // Debounced activity tracker — one handler, passive, updates timestamp
    var _activityTimer = null;
    function _onActivity() {
      if (_activityTimer) return;
      _activityTimer = setTimeout(function() { _activityTimer = null; }, 2000);
      _lastActivity = Date.now();
      if (_warned) {
        _warned = false;
        App.Utils.showToast('Session renewed', 'success');
      }
    }

    function _startIdleWatch() {
      if (_intervalId) return;
      ['mousemove','keydown','click','scroll','touchstart'].forEach(function(evt) {
        document.addEventListener(evt, _onActivity, { passive: true, capture: true });
      });
      _lastActivity = Date.now();
      _warned = false;
      _intervalId = setInterval(function() {
        var idle = Date.now() - _lastActivity;
        // Already on login screen — stop watching
        var loginEl = document.getElementById('login-screen');
        if (loginEl && !loginEl.classList.contains('hidden')) return;

        if (!_warned && idle >= IDLE_LIMIT - WARN_BEFORE) {
          _warned = true;
          App.Utils.showToast('Session expiring in 60 seconds — click anywhere to stay logged in', 'warning', WARN_BEFORE);
        }
        if (idle >= IDLE_LIMIT) {
          _stopIdleWatch();
          App.Api.logout().then(function() {
            App.Login.show('Session expired due to inactivity.');
          });
        }
      }, CHECK_INTERVAL);
    }

    function _stopIdleWatch() {
      if (_intervalId) { clearInterval(_intervalId); _intervalId = null; }
      ['mousemove','keydown','click','scroll','touchstart'].forEach(function(evt) {
        document.removeEventListener(evt, _onActivity, { capture: true });
      });
      _warned = false;
    }

    App.IdleTimeout = { start: _startIdleWatch, stop: _stopIdleWatch };
  })();

  // Expose globally for HTML onclick handlers
  App.toggleRole = toggleRole;
  App.exportData = exportData;
  App.importData = importData;
  App.resetData = resetData;
})();
