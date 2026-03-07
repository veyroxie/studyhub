(function() {
  window.App = window.App || {};

  const TITLES = {
    calendar:      'Class Schedule',
    communication: 'Communication',
    students:      'Students',
    billing:       'Bills & Payments',
    staff:         'Staff',
    attendance:    'Attendance',
    analytics:     'Analytics'
  };

  const _modules = {};
  let _current = null;

  App.Router = {
    register(pageId, module) {
      _modules[pageId] = module;
    },
    navigate(pageId) {
      document.querySelectorAll('.page').forEach(function(p) { p.classList.remove('active'); });
      document.querySelectorAll('.nav-btn').forEach(function(b) { b.classList.remove('active'); });

      const page = document.getElementById(pageId + '-page');
      if (!page) return;
      page.classList.add('active');

      const btn = document.querySelector('.nav-btn[data-page="' + pageId + '"]');
      if (btn) btn.classList.add('active');

      const titleEl = document.getElementById('page-title');
      if (titleEl) titleEl.textContent = TITLES[pageId] || pageId;

      if (_modules[pageId]) _modules[pageId].render(page);
      _current = pageId;
    },
    current() { return _current; },
    refresh() {
      if (_current) {
        const page = document.getElementById(_current + '-page');
        if (page && _modules[_current]) _modules[_current].render(page);
      }
    },
    init() {
      document.querySelectorAll('.nav-btn').forEach(function(btn) {
        btn.addEventListener('click', function() {
          App.Router.navigate(btn.dataset.page);
        });
      });
    }
  };
})();
