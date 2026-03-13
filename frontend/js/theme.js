(function() {
  window.App = window.App || {};

  var THEMES = {
    a: { label: 'Slate',   desc: 'Clean & professional' },
    b: { label: 'Gold',    desc: 'Black & gold branding' },
    c: { label: 'Minimal', desc: 'Top nav, full width' }
  };

  var _current = localStorage.getItem('sh_theme') || 'b';
  var _sidebarOpen = false;

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
      return '<div class="flex h-full">'
        + '<div class="w-5 bg-black h-full flex flex-col items-center pt-1 gap-0.5"><div class="w-2 h-0.5 rounded-full bg-yellow-400"></div><div class="w-2 h-0.5 rounded-full bg-yellow-400 opacity-40"></div><div class="w-2 h-0.5 rounded-full bg-yellow-400 opacity-40"></div></div>'
        + '<div class="flex-1" style="background:#f9f8f5"><div class="m-1 h-2 bg-white rounded border border-slate-100"></div></div>'
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
    syncTopNav: _syncTopNavActive,
    syncTopRole: _syncTopRole,
    syncTopBadge: _syncTopBadge,
    init: function() { _apply(_current); }
  };
})();
