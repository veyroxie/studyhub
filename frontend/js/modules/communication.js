(function() {
  window.App = window.App || {};

  let _typeFilter = 'All';

  function render(container) {
    const { announcements } = App.Store.get();
    const isAdmin = App.currentRole === 'admin';

    const sorted = announcements.slice().sort(function(a, b) { return b.createdOn.localeCompare(a.createdOn); });
    const filtered = _typeFilter === 'All' ? sorted : sorted.filter(function(a) { return a.type === _typeFilter; });

    container.innerHTML = ''
      + '<div class="flex items-center justify-between mb-6">'
      +   '<div>'
      +     '<h1 class="text-2xl font-bold text-slate-800">Communication</h1>'
      +     '<p class="text-sm text-slate-500 mt-0.5">Announcements & broadcasts to parents</p>'
      +   '</div>'
      +   (isAdmin ? '<button onclick="App.Communication._newModal()" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">+ New Announcement</button>' : '')
      + '</div>'

      + '<div class="flex gap-2 mb-5">'
      + ['All','Notice','Reminder','Urgent'].map(function(f) {
          const active = f === _typeFilter;
          const colors = { Notice:'bg-blue-600 text-white', Reminder:'bg-amber-500 text-white', Urgent:'bg-red-500 text-white' };
          return '<button onclick="App.Communication._setFilter(\'' + f + '\')" class="px-3 py-1.5 text-sm rounded-lg font-medium transition-colors ' + (active ? (colors[f] || 'bg-slate-700 text-white') : 'bg-white border border-slate-200 text-slate-600 hover:bg-slate-50') + '">' + f + '</button>';
        }).join('')
      + '</div>'

      + (filtered.length === 0
        ? '<div class="bg-white rounded-xl border border-dashed border-slate-200 p-12 text-center"><p class="text-slate-400">No announcements yet</p></div>'
        : '<div class="space-y-3">' + filtered.map(function(ann) { return _annCard(ann, isAdmin); }).join('') + '</div>'
      );
  }

  function _annCard(ann, isAdmin) {
    const typeColors = {
      Notice:   { border:'border-blue-300', bg:'bg-blue-50', badge:'blue', icon:'📋' },
      Reminder: { border:'border-amber-300', bg:'bg-amber-50', badge:'yellow', icon:'🔔' },
      Urgent:   { border:'border-red-300', bg:'bg-red-50', badge:'red', icon:'🚨' }
    };
    const tc = typeColors[ann.type] || typeColors.Notice;

    return '<div class="bg-white rounded-xl border border-slate-100 shadow-sm overflow-hidden">'
      + '<div class="flex items-start gap-4 p-5">'
      +   '<div class="text-2xl mt-0.5">' + tc.icon + '</div>'
      +   '<div class="flex-1 min-w-0">'
      +     '<div class="flex items-start justify-between gap-3 flex-wrap">'
      +       '<h3 class="font-semibold text-slate-800">' + ann.title + '</h3>'
      +       '<div class="flex items-center gap-2 shrink-0">'
      +         App.Utils.statusBadge(ann.type)
      +         App.Utils.badge(ann.audience, 'gray')
      +       '</div>'
      +     '</div>'
      +     '<p class="text-sm text-slate-600 mt-2 leading-relaxed">' + ann.message + '</p>'
      +     '<div class="flex items-center justify-between mt-3">'
      +       '<span class="text-xs text-slate-400">' + App.Utils.formatDate(ann.createdOn) + ' · ' + ann.createdBy + '</span>'
      +       (isAdmin ? '<button onclick="App.Communication._delete(\'' + ann.id + '\')" class="text-xs text-red-400 hover:text-red-600">Delete</button>' : '')
      +     '</div>'
      +   '</div>'
      + '</div>'
      + '</div>';
  }

  function _setFilter(f) {
    _typeFilter = f;
    App.Router.refresh();
  }

  function _delete(annId) {
    const state = App.Store.get();
    App.Store.set({ announcements: state.announcements.filter(function(a) { return a.id !== annId; }) });
    App.Utils.showToast('Announcement deleted', 'info');
    App.Router.refresh();
  }

  function _newModal() {
    const { students } = App.Store.get();
    const uniqueClasses = [...new Set(students.flatMap(function(s) { return s.enrolledClasses; }))];
    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-4">New Announcement</h2>'
      + '<form id="ann-form" class="space-y-4">'
      + _field('Title', '<input name="title" class="form-input" placeholder="e.g. Holiday Schedule Notice" required maxlength="150">')
      + _field('Message', '<textarea name="message" class="form-input" rows="4" placeholder="Write your message here..." required maxlength="1000"></textarea>')
      + '<div class="grid grid-cols-2 gap-4">'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Audience</label>'
      + '<select name="audience" class="form-input">'
      + '<option>All Parents</option><option>All Staff</option>'
      + '</select></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Type</label>'
      + '<select name="type" class="form-input">'
      + '<option>Notice</option><option>Reminder</option><option>Urgent</option>'
      + '</select></div>'
      + '</div>'
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Publish</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );
    document.getElementById('ann-form').addEventListener('submit', function(e) {
      e.preventDefault();
      const fd = new FormData(e.target);
      const state = App.Store.get();
      const newAnn = {
        id: App.Utils.generateId('ANN'),
        title: fd.get('title').trim(),
        message: fd.get('message').trim(),
        audience: fd.get('audience'),
        type: fd.get('type'),
        createdOn: App.Utils.today(),
        createdBy: 'Admin'
      };
      App.Store.set({ announcements: [newAnn, ...state.announcements] });
      App.Utils.hideModal();
      App.Utils.showToast('Announcement published!', 'success');
      App.Router.refresh();
    });
  }

  function _field(label, inputHtml) {
    return '<div><label class="block text-sm font-medium text-slate-700 mb-1">' + label + '</label>' + inputHtml + '</div>';
  }

  App.Communication = { render: render, _setFilter: _setFilter, _delete: _delete, _newModal: _newModal };
})();
