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

  // _showToSGate renders a blocking modal with the current ToS text. Returns
  // a Promise that resolves true when accepted, false when declined. The
  // caller is responsible for handling the false path (logout + reload).
  // Modal cannot be dismissed by clicking outside or pressing Esc — those
  // affordances would let a user slip past the gate.
  // _showMFAGate collects the 6-digit TOTP (or a recovery code) after a login
  // that returned mfaRequired, and exchanges it via App.Api.mfaVerify.
  // Resolves with the login user object, or null if the user cancels.
  function _showMFAGate(interimToken) {
    return new Promise(function(resolve) {
      var overlay = document.createElement('div');
      overlay.style.cssText = 'position:fixed;inset:0;background:rgba(15,15,15,0.6);z-index:99999;display:flex;align-items:center;justify-content:center;padding:1.5rem;backdrop-filter:blur(4px)';
      overlay.innerHTML =
        '<div style="background:#fff;border-radius:14px;max-width:380px;width:100%;padding:1.75rem;box-shadow:0 20px 60px rgba(0,0,0,0.3)">'
        + '<h2 style="margin:0 0 0.35rem;font-family:\'Fraunces\',\'Cormorant Garamond\',serif;font-size:1.35rem;font-weight:500;color:#0a0a0a">Two-factor code</h2>'
        + '<p id="mfa-gate-hint" style="margin:0 0 1rem;font-size:0.8rem;color:#94a3b8">Enter the 6-digit code from your authenticator app.</p>'
        + '<input id="mfa-gate-code" inputmode="numeric" autocomplete="one-time-code" maxlength="16" style="width:100%;padding:0.6rem 0.75rem;font-size:1.1rem;letter-spacing:0.2em;text-align:center;border:1px solid #e2e8f0;border-radius:10px;outline:none" autofocus>'
        + '<p id="mfa-gate-err" style="display:none;margin:0.6rem 0 0;font-size:0.78rem;color:#dc2626"></p>'
        + '<button id="mfa-gate-verify" style="width:100%;margin-top:1rem;padding:0.6rem;font-size:0.9rem;font-weight:700;background:var(--gold,#C9A227);color:#0a0a0a;border:none;border-radius:10px;cursor:pointer">Verify</button>'
        + '<div style="display:flex;justify-content:space-between;margin-top:0.85rem">'
        +   '<button id="mfa-gate-recovery" style="background:none;border:none;font-size:0.75rem;color:#64748b;cursor:pointer;text-decoration:underline">Use a recovery code</button>'
        +   '<button id="mfa-gate-cancel" style="background:none;border:none;font-size:0.75rem;color:#64748b;cursor:pointer">Cancel</button>'
        + '</div>'
        + '</div>';
      document.body.appendChild(overlay);
      var isRecovery = false;
      var input = overlay.querySelector('#mfa-gate-code');
      var errEl = overlay.querySelector('#mfa-gate-err');
      var done = function(result) { overlay.remove(); resolve(result); };
      var verify = function() {
        var code = (input.value || '').trim();
        if (!code) return;
        errEl.style.display = 'none';
        App.Api.mfaVerify(interimToken, code, isRecovery).then(done).catch(function(err) {
          errEl.textContent = err.message || 'Code did not match';
          errEl.style.display = 'block';
          input.select();
        });
      };
      overlay.querySelector('#mfa-gate-verify').addEventListener('click', verify);
      input.addEventListener('keydown', function(e) { if (e.key === 'Enter') { e.preventDefault(); verify(); } });
      overlay.querySelector('#mfa-gate-recovery').addEventListener('click', function() {
        isRecovery = !isRecovery;
        this.textContent = isRecovery ? 'Use an authenticator code' : 'Use a recovery code';
        overlay.querySelector('#mfa-gate-hint').textContent = isRecovery
          ? 'Enter one of your saved recovery codes.'
          : 'Enter the 6-digit code from your authenticator app.';
        input.value = ''; input.focus();
      });
      overlay.querySelector('#mfa-gate-cancel').addEventListener('click', function() { done(null); });
      setTimeout(function() { input.focus(); }, 50);
    });
  }

  function _showToSGate() {
    return new Promise(function(resolve) {
      var overlay = document.createElement('div');
      overlay.style.cssText = 'position:fixed;inset:0;background:rgba(15,15,15,0.6);z-index:99999;display:flex;align-items:center;justify-content:center;padding:1.5rem;backdrop-filter:blur(4px)';
      overlay.innerHTML =
        '<div style="background:#fff;border-radius:14px;max-width:560px;width:100%;max-height:90vh;display:flex;flex-direction:column;overflow:hidden;box-shadow:0 20px 60px rgba(0,0,0,0.3)">'
        + '<div style="padding:1.5rem 1.75rem 0.75rem;border-bottom:1px solid #f1f5f9">'
        +   '<h2 style="margin:0;font-family:\'Fraunces\',\'Cormorant Garamond\',serif;font-size:1.5rem;font-weight:500;color:#0a0a0a">Terms of Service</h2>'
        +   '<p style="margin:0.35rem 0 0;font-size:0.78rem;color:#94a3b8">Please review and accept before continuing.</p>'
        + '</div>'
        + '<div style="padding:1.25rem 1.75rem;overflow-y:auto;flex:1;font-size:0.86rem;line-height:1.7;color:#374151">'
        +   '<h3 style="font-size:0.85rem;font-weight:700;color:#0a0a0a;margin:0 0 0.4rem">1. Acceptance</h3>'
        +   '<p style="margin:0 0 1rem">By using The Study Hub you agree to these terms. They cover how we handle your personal data, billing, and the platform itself.</p>'
        +   '<h3 style="font-size:0.85rem;font-weight:700;color:#0a0a0a;margin:0 0 0.4rem">2. Data &amp; privacy</h3>'
        +   '<p style="margin:0 0 1rem">We store the information you provide (name, contact email, phone, your children\'s names and class enrolments, attendance, payment history) to operate the platform. You can export your data at any time from your profile, and you can request deletion under PDPA.</p>'
        +   '<h3 style="font-size:0.85rem;font-weight:700;color:#0a0a0a;margin:0 0 0.4rem">3. Billing</h3>'
        +   '<p style="margin:0 0 1rem">Tuition is billed monthly based on your subscription package. Refunds are at the centre\'s discretion. Payment proof uploads and gateway transactions are kept for at least 7 years for tax purposes.</p>'
        +   '<h3 style="font-size:0.85rem;font-weight:700;color:#0a0a0a;margin:0 0 0.4rem">4. Acceptable use</h3>'
        +   '<p style="margin:0 0 1rem">Use the platform for its intended purpose. Don\'t share your account, attempt to access other families\' data, or scrape the platform.</p>'
        +   '<h3 style="font-size:0.85rem;font-weight:700;color:#0a0a0a;margin:0 0 0.4rem">5. Changes</h3>'
        +   '<p style="margin:0">We may update these terms; if material, we\'ll ask you to accept again. Contact us at hello@studyhub.fit for any questions.</p>'
        + '</div>'
        + '<div style="padding:1rem 1.75rem;border-top:1px solid #f1f5f9;display:flex;gap:0.6rem;justify-content:flex-end;background:#fafaf8">'
        +   '<button id="tos-decline" style="padding:0.55rem 1.1rem;font-size:0.82rem;font-weight:600;background:#fff;border:1px solid #e2e8f0;border-radius:8px;color:#64748b;cursor:pointer">Decline &amp; sign out</button>'
        +   '<button id="tos-accept" style="padding:0.55rem 1.4rem;font-size:0.82rem;font-weight:700;background:#0a0a0a;color:#fff;border:none;border-radius:8px;cursor:pointer">Accept &amp; continue</button>'
        + '</div>'
        + '</div>';
      document.body.appendChild(overlay);
      document.getElementById('tos-accept').addEventListener('click', async function() {
        try {
          await App.Api.post('/api/account/accept-tos', {});
        } catch(e) {
          // App.Api auto-toasts the failure; keep the modal open so the user
          // can retry instead of slipping past on a network blip.
          return;
        }
        overlay.remove();
        resolve(true);
      });
      document.getElementById('tos-decline').addEventListener('click', function() {
        overlay.remove();
        resolve(false);
      });
    });
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
      // Dev shortcuts never auto-remember — they're for testing on shared
      // machines where you don't want a 30-day cookie sticking around.
      await App.Login._doLogin(email, password, false);
    },
    async _doLogin(email, password, rememberOverride) {
      const btn = document.getElementById('login-btn');
      const errEl = document.getElementById('login-error');
      const resendBanner = document.getElementById('resend-verify-banner');
      // Read the "Keep me signed in" checkbox if no explicit override was
      // passed (the dev quick-login buttons always force false).
      let remember = false;
      if (typeof rememberOverride === 'boolean') {
        remember = rememberOverride;
      } else {
        const cb = document.getElementById('login-remember');
        remember = !!(cb && cb.checked);
      }
      // Spinner inside the button so the user feels instant feedback even
      // before the full-screen loader appears for the snapshot fetch.
      if (btn && !btn._origHTML) btn._origHTML = btn.innerHTML;
      if (btn) {
        btn.disabled = true;
        btn.innerHTML = '<span class="sh-spinner"></span><span style="margin-left:0.5em">Signing in...</span>';
      }
      errEl.classList.add('hidden');
      if (resendBanner) resendBanner.classList.add('hidden');
      try {
        _showLoading('Signing in...');
        let data = await App.Api.login(email, password, remember);
        // MFA challenge: no session yet — collect the TOTP code and exchange
        // it for the real cookie. Without this branch an MFA-enabled admin
        // could never complete login (the missing prompt was a hard lockout).
        if (data && data.mfaRequired) {
          _hideLoading();
          data = await _showMFAGate(data.token);
          if (!data) {
            if (btn) { btn.disabled = false; btn.innerHTML = btn._origHTML || 'Sign In'; }
            return;
          }
          _showLoading('Signing in...');
        }
        try { localStorage.setItem('sh_remember', remember ? '1' : '0'); } catch (e) {}
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
        // Block entry until the user accepts the current ToS version. The
        // server sets data.mustAcceptTos when the user's stored version is
        // below currentToSVersion. We resolve only when the modal is
        // dismissed — declining redirects to logout so abandoned sessions
        // don't slip in without acceptance.
        if (data.mustAcceptTos) {
          _hideLoading();
          const accepted = await _showToSGate();
          if (!accepted) {
            await App.Api.post('/api/auth/logout', {}, { silent: true }).catch(function(){});
            window.location.reload();
            return;
          }
        }
        // Shell-first render: hide the login screen and show the dashboard
        // structure IMMEDIATELY. The snapshot loads in the background and
        // the active module re-renders when data lands. Users see the nav
        // + empty cards in ~50ms instead of waiting ~500ms for the full
        // snapshot round-trip before any pixel changes.
        _hideLoading();
        App.Login.hide();
        App.Theme.init();
        applyRole();
        App.Dev.init();
        App.Router.navigate('dashboard');
        var snapshotPromise = App.Api.loadSnapshot().then(function() {
          // Data arrived — re-render the current page so empty placeholders
          // are replaced with real rows.
          App.Router.refresh();
        });
        // Existing post-login side-effects can wait for the data without
        // blocking the visible shell.
        await snapshotPromise;
        App.Notifs.updateBadge();
        // Open the live WebSocket now, same as the session-restore path — a
        // fresh interactive login otherwise never subscribes to live events
        // (e.g. check-in toasts) until the next reload.
        App.Api.connectWS();
        if (App.Billing && App.Billing.checkLoginNotifications) App.Billing.checkLoginNotifications();
        App.IdleTimeout.start();
        if (App.Push) { App.Push.init(); App.Push.maybeNudge(); }
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
        if (btn) {
          btn.disabled = false;
          btn.innerHTML = btn._origHTML || 'Sign In';
          btn._origHTML = null;
        }
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
      progress:   false
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
        // Server-side import: the old App.Store.importJSON path only wrote
        // localStorage, so the next snapshot reload silently reverted it.
        var students;
        try { students = JSON.parse(ev.target.result); } catch (e) { students = null; }
        if (!Array.isArray(students) || students.length === 0) {
          App.Utils.showToast('Import failed — expected a JSON array of students', 'error');
          return;
        }
        App.Api.post('/api/admin/import', students).then(function(res) {
          return App.Api.loadSnapshot().then(function() {
            var made = res && res.studentsCreated != null ? res.studentsCreated : students.length;
            var skipped = (res && res.studentsSkipped) || 0;
            App.Utils.showToast('Imported ' + made + ' students' + (skipped ? ' · ' + skipped + ' skipped (already exist)' : ''), 'success');
            App.Router.refresh();
          });
        });
      };
      reader.readAsText(file);
    };
    input.click();
  }

  async function resetData() {
    // Honest scope: the server endpoint removes the demo/seed rows, not all
    // data. The old label promised a full reset and only touched localStorage.
    var ok = await App.Utils.showConfirm({ title: 'Remove demo data', message: 'This removes the seeded demo students, classes and invoices from the server. Real data is untouched. This cannot be undone.', confirmLabel: 'Remove', danger: true });
    if (!ok) return;
    App.Api.post('/api/admin/clear-seed').then(function() {
      return App.Api.loadSnapshot().then(function() {
        App.Utils.showToast('Demo data removed', 'success');
        App.Router.refresh();
      });
    });
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
    App.Router.register('progress',      App.Progress);
    // analytics is lazy-loaded (see the loader in index.html) and registers
    // itself with the router on load — registering here would store undefined.
    App.Router.register('profile',       App.Profile);

    // Init router (sets up nav button click handlers)
    App.Router.init();

    // Populate parent selector
    const parentSelect = document.getElementById('parent-select');
    if (parentSelect) {
      const { students } = App.Store.get();
      const uniqueParents = {};
      students.forEach(function(s) { uniqueParents[s.contact] = s.parentName; });
      parentSelect.innerHTML = Object.keys(uniqueParents).map(function(email) {
        return '<option value="' + App.Utils.esc(email) + '">' + App.Utils.esc(uniqueParents[email] || email) + '</option>';
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
          App.Utils.showToast(msg, data.type === 'CHECK_IN' ? 'info' : 'success');
        }
      };
    } catch(e) { console.error('BroadcastChannel init failed', e); }

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
    App.Api.isLoggedIn().then(async function(loggedIn) {
      if (loggedIn) {
        const user = App.Api.currentUser();
        App.currentRole = (user && user.role === 'admin') ? 'admin' : (user && user.role === 'teacher') ? 'teacher' : 'client';
        if (user && user.role === 'parent') App.clientParent = user.email;
        if (user && user.role === 'teacher') { App.currentTeacher = user.staffId || ''; sessionStorage.setItem('sh_teacher', App.currentTeacher); }
        if (user && user.mustAcceptTos) {
          const accepted = await _showToSGate();
          if (!accepted) {
            await App.Api.post('/api/auth/logout', {}, { silent: true }).catch(function(){});
            window.location.reload();
            return;
          }
        }
        // Shell-first on session-restore too: paint the dashboard
        // structure immediately, fill in data when the snapshot arrives.
        App.Login.hide();
        App.Theme.init();
        App.Dev.init();
        applyRole();
        App.Router.navigate('dashboard');
        return App.Api.loadSnapshot().then(function() {
          App.Router.refresh();
          App.Notifs.updateBadge();
          App.Api.connectWS();
          App.IdleTimeout.start();
          if (App.Push) { App.Push.init(); App.Push.maybeNudge(); }
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
          return '<option value="' + App.Utils.esc(email) + '">' + App.Utils.esc((uniqueParents[email] || '') + ' (' + email + ')') + '</option>';
        }).join('');
        if (App.clientParent) devSel.value = App.clientParent;
        else App.clientParent = Object.keys(uniqueParents)[0] || '';
      }

      const devTeacherSel = document.getElementById('dev-teacher-select');
      if (devTeacherSel) {
        devTeacherSel.innerHTML = staff.map(function(s) {
          return '<option value="' + App.Utils.esc(s.id) + '">' + App.Utils.esc(s.fullName) + '</option>';
        }).join('');
        if (App.currentTeacher) devTeacherSel.value = App.currentTeacher;
        else App.currentTeacher = (staff[0] && staff[0].id) || '';
      }

      // Also populate header teacher selector
      const headerTeacherSel = document.getElementById('teacher-select');
      if (headerTeacherSel) {
        headerTeacherSel.innerHTML = staff.map(function(s) {
          return '<option value="' + App.Utils.esc(s.id) + '">' + App.Utils.esc(s.fullName) + '</option>';
        }).join('');
        if (App.currentTeacher) headerTeacherSel.value = App.currentTeacher;
      }

      App.Dev._update();
    }
  };

  // ── Session Idle Timeout ──────────────────────────────────────────────────
  (function() {
    var IDLE_LIMIT   = 2 * 60 * 60 * 1000; // 2 hours
    var WARN_BEFORE  = 60 * 1000;          // warn 60s before logout
    var CHECK_INTERVAL = 15 * 1000;        // check every 15s
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
      // "Remember me" sessions opt out of the idle timeout entirely — the
      // whole point of the checkbox is to stay signed in indefinitely.
      try {
        if (localStorage.getItem('sh_remember') === '1') return;
      } catch (e) {}
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
