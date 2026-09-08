(function() {
  window.App = window.App || {};

  const TITLES = {
    dashboard:     'Dashboard',
    calendar:      'Class Schedule',
    communication: 'Announcements',
    students:      'Students',
    billing:       'Bills & Payments',
    staff:         'Staff',
    attendance:    'Attendance',
    progress:      'Progress Reports',
    analytics:     'Analytics',
    pricing:       'Pricing',
    profile:       'My Profile'
  };

  const _modules = {};
  let _current = null;

  App.Router = {
    register(pageId, module) {
      _modules[pageId] = module;
    },
    // The page you are on survives a reload, because it lives in the URL hash.
    // A refresh used to drop you back on the dashboard, which is a poor trade
    // when the whole workflow is "look at this screen, fix something, reload".
    // The hash also makes back and forward work and makes a page linkable.
    navigate(pageId) {
      // analytics.js is lazy-loaded — kick the loader before we try to
      // render. The render will run when the module registers via
      // _modules[pageId] once the script has parsed.
      if (pageId === 'analytics' && window.__loadAnalytics) window.__loadAnalytics();

      document.querySelectorAll('.page').forEach(function(p) { p.classList.remove('active'); });
      document.querySelectorAll('.nav-btn').forEach(function(b) { b.classList.remove('active'); b.removeAttribute('aria-current'); });

      const page = document.getElementById(pageId + '-page');
      if (!page) return;
      page.classList.add('active');
      if (window.location.hash.slice(1) !== pageId) {
        // replaceState, not a hash assignment: writing location.hash fires
        // hashchange, which would call navigate again and render twice.
        try { history.replaceState(null, '', '#' + pageId); }
        catch (e) { /* file:// and some embeds refuse replaceState */ }
      }

      const btn = document.querySelector('.nav-btn[data-page="' + pageId + '"]');
      if (btn) { btn.classList.add('active'); btn.setAttribute('aria-current', 'page'); }

      const titleEl = document.getElementById('page-title');
      if (titleEl) titleEl.textContent = TITLES[pageId] || pageId;

      if (_modules[pageId]) _modules[pageId].render(page);
      _current = pageId;
      if (App.Theme) {
        App.Theme.syncTopNav();
        App.Theme.syncDock();
      }
    },
    current() { return _current; },
    refresh() {
      if (_current) {
        const page = document.getElementById(_current + '-page');
        if (page && _modules[_current]) _modules[_current].render(page);
      }
    },
    // fromHash returns the page named in the URL, but only if that page really
    // exists in this document -- a stale or hand-typed hash must not leave the
    // app on no page at all.
    fromHash() {
      const id = (window.location.hash || '').replace(/^#/, '').trim();
      if (!id || !document.getElementById(id + '-page')) return null;
      return id;
    },
    init() {
      document.querySelectorAll('.nav-btn').forEach(function(btn) {
        btn.addEventListener('click', function() {
          App.Router.navigate(btn.dataset.page);
        });
      });
      // Back and forward move between pages rather than doing nothing.
      window.addEventListener('hashchange', function() {
        const id = App.Router.fromHash();
        if (id && id !== _current) App.Router.navigate(id);
      });
    }
  };
})();
