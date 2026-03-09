(function() {
  window.App = window.App || {};

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
      if (btn) { btn.textContent = 'Signing in...'; btn.disabled = true; }
      errEl.classList.add('hidden');
      try {
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
        await App.Api.loadSnapshot();
        App.Login.hide();
        App.Theme.init();
        applyRole();
        App.Dev.init();
        App.Router.navigate('dashboard');
        App.Notifs.updateBadge();
        if (App.Billing && App.Billing.checkLoginNotifications) App.Billing.checkLoginNotifications();
      } catch(err) {
        errEl.textContent = err.message || 'Login failed';
        errEl.classList.remove('hidden');
      } finally {
        if (btn) { btn.textContent = 'Sign In'; btn.disabled = false; }
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
      attendance: isClient,
      feedback:   isClient
    };
    Object.keys(pageHidden).forEach(function(page) {
      const btn = document.querySelector('.nav-btn[data-page="' + page + '"]');
      if (!btn) return;
      const hide = pageHidden[page];
      const li = btn.closest('li');
      if (li) li.classList.toggle('hidden', hide); else btn.classList.toggle('hidden', hide);
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

    // Check if already logged in (reads HttpOnly cookie server-side)
    App.Api.isLoggedIn().then(function(loggedIn) {
      if (loggedIn) {
        const user = App.Api.currentUser();
        App.currentRole = (user && user.role === 'admin') ? 'admin' : (user && user.role === 'teacher') ? 'teacher' : 'client';
        if (user && user.role === 'parent') App.clientParent = user.email;
        if (user && user.role === 'teacher') { App.currentTeacher = user.staffId || ''; sessionStorage.setItem('sh_teacher', App.currentTeacher); }
        return App.Api.loadSnapshot().then(function() {
          App.Login.hide();
          App.Dev.init();
          applyRole();
          App.Router.navigate('dashboard');
          App.Notifs.updateBadge();
          App.Api.connectWS();
        });
      } else {
        App.Login.show();
      }
    });
  });

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
          statusEl.innerHTML = '<span class="text-purple-400 font-semibold">Teacher</span>: ' + (t ? t.fullName : App.currentTeacher);
        } else {
          const { students } = App.Store.get();
          const myStudents = students.filter(function(s) { return s.contact === App.clientParent; });
          const names = myStudents.map(function(s) { return s.firstName; }).join(', ');
          statusEl.innerHTML = '<span class="text-emerald-400 font-semibold">Parent</span>: ' + (App.clientParent || '—')
            + '<br>Children: <span class="text-white">' + (names || 'none') + '</span>';
        }
      }
    },
    init: function() {
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
          return '<option value="' + s.id + '">' + s.fullName + '</option>';
        }).join('');
        if (App.currentTeacher) devTeacherSel.value = App.currentTeacher;
        else App.currentTeacher = (staff[0] && staff[0].id) || '';
      }

      // Also populate header teacher selector
      const headerTeacherSel = document.getElementById('teacher-select');
      if (headerTeacherSel) {
        headerTeacherSel.innerHTML = staff.map(function(s) {
          return '<option value="' + s.id + '">' + s.fullName + '</option>';
        }).join('');
        if (App.currentTeacher) headerTeacherSel.value = App.currentTeacher;
      }

      App.Dev._update();
    }
  };

  // Expose globally for HTML onclick handlers
  App.toggleRole = toggleRole;
  App.exportData = exportData;
  App.importData = importData;
  App.resetData = resetData;
})();
