(function() {
  window.App = window.App || {};

  var THEMES = {
    a: { label: 'Slate',   desc: 'Clean & professional' },
    b: { label: 'Gold',    desc: 'Bottom dock, white & gold' },
    c: { label: 'Minimal', desc: 'Top nav, full width' }
  };

  var _current = localStorage.getItem('sh_theme') || 'b';
  var _sidebarOpen = false;
  var _collapsed = localStorage.getItem('sh_sidebar_collapsed') === '1';

  function _apply(theme) {
    document.body.classList.remove('theme-a', 'theme-b', 'theme-c');
    if (theme !== 'a') document.body.classList.add('theme-' + theme);
    _current = theme;
    localStorage.setItem('sh_theme', theme);

    // Theme C: wire top nav buttons
    if (theme === 'c') {
      _wireTopNav();
      _syncTopNavActive();
    }

    // Theme B: wire dock buttons
    if (theme === 'b') {
      _wireDock();
      _syncDockActive();
      _syncDockRole();
      _syncDockBadge();
    }

    // Sync top role button
    _syncTopRole();
    _syncTopBadge();
  }

  function _wireTopNav() {
    document.querySelectorAll('.top-nav-btn').forEach(function(btn) {
      // avoid double-wiring
      if (btn.dataset.wired) return;
      btn.dataset.wired = '1';
      btn.addEventListener('click', function() {
        App.Router.navigate(btn.dataset.page);
      });
    });
  }

  function _syncTopNavActive() {
    var current = App.Router ? App.Router.current() : null;
    document.querySelectorAll('.top-nav-btn').forEach(function(btn) {
      var active = btn.dataset.page === current;
      btn.classList.toggle('text-white', active);
      btn.classList.toggle('bg-slate-700', active);
      btn.classList.toggle('text-slate-400', !active);
    });
  }

  function _syncTopRole() {
    var btn = document.getElementById('top-role-btn');
    if (!btn) return;
    var isAdmin = App.currentRole === 'admin';
    btn.textContent = isAdmin ? 'Admin' : 'Parent';
    btn.className = 'px-2 py-1 text-xs font-semibold rounded-lg border transition-colors '
      + (isAdmin ? 'border-blue-500 text-blue-400 hover:bg-slate-800' : 'border-emerald-500 text-emerald-400 hover:bg-slate-800');

    // hide admin-only top nav buttons in client mode
    document.querySelectorAll('.top-nav-admin').forEach(function(el) {
      el.classList.toggle('hidden', !isAdmin);
    });
  }

  function _syncTopBadge() {
    // Mirror the main notif badge into the top navbar badge
    var main = document.getElementById('notif-badge');
    var top  = document.getElementById('top-notif-badge');
    if (!main || !top) return;
    top.textContent  = main.textContent;
    top.className    = main.className.replace('notif-badge', 'top-notif-badge');
  }

  // Toggle mobile sidebar
  function toggleSidebar() {
    _sidebarOpen = !_sidebarOpen;
    var sidebar = document.getElementById('sidebar');
    if (sidebar) sidebar.classList.toggle('open', _sidebarOpen);
    document.body.classList.toggle('sidebar-open', _sidebarOpen);
  }

  // Toggle desktop sidebar collapse
  function toggleCollapse() {
    _collapsed = !_collapsed;
    document.body.classList.toggle('sidebar-collapsed', _collapsed);
    localStorage.setItem('sh_sidebar_collapsed', _collapsed ? '1' : '0');
  }

  function _restoreCollapse() {
    if (_collapsed) document.body.classList.add('sidebar-collapsed');
  }

  // Show theme picker modal
  function picker() {
    var html = '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-1">Choose Theme</h2>'
      + '<p class="text-sm text-slate-500 mb-5">Pick a look that suits you best</p>'
      + '<div class="space-y-3">';

    Object.keys(THEMES).forEach(function(key) {
      var t    = THEMES[key];
      var active = key === _current;
      var preview = _themePreview(key);
      html += '<button onclick="App.Theme.set(\'' + key + '\'); App.Utils.hideModal(true)" '
        + 'class="w-full flex items-center gap-4 p-4 rounded-xl border-2 transition-all text-left '
        + (active ? 'border-blue-500 bg-blue-50' : 'border-slate-200 hover:border-slate-300 hover:bg-slate-50') + '">'
        + '<div class="w-16 h-10 rounded-lg overflow-hidden shrink-0 border border-slate-200">' + preview + '</div>'
        + '<div class="flex-1">'
        +   '<div class="font-semibold text-slate-800 flex items-center gap-2">' + t.label
        +     (active ? ' <span class="text-xs text-blue-600 font-normal">current</span>' : '')
        +   '</div>'
        +   '<div class="text-xs text-slate-500 mt-0.5">' + t.desc + '</div>'
        + '</div>'
        + (active ? '<svg class="w-5 h-5 text-blue-500 shrink-0" fill="none" stroke="currentColor" stroke-width="2.5" viewBox="0 0 24 24"><path stroke-linecap="round" d="M5 13l4 4L19 7"/></svg>' : '')
        + '</button>';
    });

    html += '</div>'
      + '<div class="mt-4 flex justify-end">'
      + '<button onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Close</button>'
      + '</div>'
      + '</div>';

    App.Utils.showModal(html);
  }

  function _themePreview(key) {
    if (key === 'a') {
      return '<div class="flex h-full">'
        + '<div class="w-5 bg-slate-800 h-full"></div>'
        + '<div class="flex-1 bg-slate-50 p-1"><div class="w-full h-2 bg-white rounded mb-1 border border-slate-100"></div><div class="w-3/4 h-1.5 bg-blue-100 rounded"></div></div>'
        + '</div>';
    }
    if (key === 'b') {
      return '<div class="flex flex-col h-full">'
        + '<div class="h-2.5 bg-white flex items-center px-1 border-b border-slate-100"><div class="w-2 h-1 rounded-sm" style="background:#C9A227"></div></div>'
        + '<div class="flex-1" style="background:#FAF9F6;padding:2px"><div class="h-2 bg-white rounded border border-slate-100 mb-0.5"></div><div class="grid grid-cols-2 gap-0.5"><div class="h-1.5 bg-white rounded border border-slate-100"></div><div class="h-1.5 bg-white rounded border border-slate-100"></div></div></div>'
        + '<div class="h-2 bg-gray-900 flex items-center justify-center gap-0.5 rounded-t"><div class="w-1 h-0.5 rounded-full bg-yellow-400"></div><div class="w-1 h-0.5 rounded-full bg-yellow-400 opacity-40"></div><div class="w-1 h-0.5 rounded-full bg-yellow-400 opacity-40"></div></div>'
        + '</div>';
    }
    if (key === 'c') {
      return '<div class="flex flex-col h-full">'
        + '<div class="h-3 bg-slate-800 flex items-center px-1 gap-0.5"><div class="w-1.5 h-1 rounded-sm bg-slate-600"></div><div class="w-2 h-1 rounded-sm bg-blue-500"></div><div class="w-1.5 h-1 rounded-sm bg-slate-600"></div></div>'
        + '<div class="flex-1 bg-slate-50 p-1"><div class="grid grid-cols-3 gap-0.5"><div class="h-2 bg-white rounded border border-slate-100"></div><div class="h-2 bg-white rounded border border-slate-100"></div><div class="h-2 bg-white rounded border border-slate-100"></div></div></div>'
        + '</div>';
    }
    return '';
  }

  // ── Bottom Dock (Theme B) ──────────────────────────────────────────────────
  function _wireDock() {
    document.querySelectorAll('.dock-btn').forEach(function(btn) {
      if (btn.dataset.wired) return;
      btn.dataset.wired = '1';
      btn.addEventListener('click', function() {
        App.Router.navigate(btn.dataset.page);
      });
    });
  }

  function _syncDockActive() {
    var current = App.Router ? App.Router.current() : null;
    document.querySelectorAll('.dock-btn').forEach(function(btn) {
      btn.classList.toggle('active', btn.dataset.page === current);
    });
  }

  function _syncDockRole() {
    var btn = document.getElementById('dock-role-btn');
    if (!btn) return;
    var isAdmin   = App.currentRole === 'admin';
    var isTeacher = App.currentRole === 'teacher';
    var labels = { admin: 'Admin', teacher: 'Teacher', client: 'Parent' };
    btn.textContent = labels[App.currentRole] || 'Admin';

    // hide admin-only dock buttons
    document.querySelectorAll('.dock-admin').forEach(function(el) {
      el.style.display = isAdmin ? '' : 'none';
    });

    // Show/hide role-specific dock buttons
    var pageHidden = {
      billing:    isTeacher,
      staff:      !isAdmin,
      analytics:  !isAdmin,
      students:   App.currentRole === 'client',
      attendance: false,
      feedback:   false
    };
    document.querySelectorAll('.dock-btn').forEach(function(el) {
      var page = el.dataset.page;
      if (pageHidden[page] !== undefined) {
        el.style.display = pageHidden[page] ? 'none' : '';
      }
    });

    // Mirror selectors to dock topbar
    var dockParentWrap = document.getElementById('dock-parent-selector-wrap');
    var dockTeacherWrap = document.getElementById('dock-teacher-selector-wrap');
    if (dockParentWrap) dockParentWrap.style.display = App.currentRole === 'client' ? 'flex' : 'none';
    if (dockTeacherWrap) dockTeacherWrap.style.display = isTeacher ? 'flex' : 'none';

    // Mirror the parent/teacher selects
    var mainParentSel = document.getElementById('parent-select');
    var dockParentSel = document.getElementById('dock-parent-select');
    if (mainParentSel && dockParentSel && dockParentSel.innerHTML === '') {
      dockParentSel.innerHTML = mainParentSel.innerHTML;
      dockParentSel.value = mainParentSel.value;
      dockParentSel.onchange = function() {
        mainParentSel.value = this.value;
        mainParentSel.dispatchEvent(new Event('change'));
      };
    }
    var mainTeacherSel = document.getElementById('teacher-select');
    var dockTeacherSel = document.getElementById('dock-teacher-select');
    if (mainTeacherSel && dockTeacherSel && dockTeacherSel.innerHTML === '') {
      dockTeacherSel.innerHTML = mainTeacherSel.innerHTML;
      dockTeacherSel.value = mainTeacherSel.value;
      dockTeacherSel.onchange = function() {
        mainTeacherSel.value = this.value;
        mainTeacherSel.dispatchEvent(new Event('change'));
      };
    }
  }

  function _syncDockBadge() {
    var main = document.getElementById('notif-badge');
    var dock = document.getElementById('dock-notif-badge');
    if (!main || !dock) return;
    dock.textContent = main.textContent;
    var count = parseInt(main.textContent, 10) || 0;
    dock.style.display = count > 0 ? 'flex' : 'none';
  }

  // Public API
  App.Theme = {
    set: function(t) {
      _apply(t);
      // Re-apply role visibility
      if (App.Router && App.Router.current()) App.Router.refresh();
    },
    current: function() { return _current; },
    picker:  picker,
    toggleSidebar: toggleSidebar,
    toggleCollapse: toggleCollapse,
    syncTopNav: _syncTopNavActive,
    syncTopRole: _syncTopRole,
    syncTopBadge: _syncTopBadge,
    syncDock: _syncDockActive,
    syncDockRole: _syncDockRole,
    syncDockBadge: _syncDockBadge,
    init: function() { _restoreCollapse(); _apply(_current); }
  };
})();
