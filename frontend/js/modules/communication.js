(function() {
  window.App = window.App || {};

  let _typeFilter = 'All';

  function render(container) {
    const { announcements } = App.Store.get();
    const isAdmin   = App.currentRole === 'admin';
    const isTeacher = App.currentRole === 'teacher';

    // Pending approval queue (admin only)
    const pendingAnns = announcements.filter(function(a) { return a.status === 'pending_approval'; });

    // Published announcements only (for display to all)
    const today = App.Utils.today();
    const isClient = App.currentRole === 'client';

    // Build parent's child class IDs for targetClassIds filtering
    var parentClassIds = null;
    if (isClient && App.clientParent) {
      var { students } = App.Store.get();
      parentClassIds = {};
      students.filter(function(s) { return s.contact === App.clientParent; }).forEach(function(s) {
        (s.enrolledClasses || []).forEach(function(cid) { parentClassIds[cid] = true; });
      });
    }

    const published = announcements
      .filter(function(a) {
        if (a.status !== 'published' && a.status) return false;
        // Hide auto-archived from non-admins
        if (!isAdmin && a.archiveOn && a.archiveOn < today) return false;
        // Filter targeted announcements for parents
        if (isClient && a.targetClassIds && a.targetClassIds.length > 0 && parentClassIds) {
          var hasMatch = a.targetClassIds.some(function(cid) { return parentClassIds[cid]; });
          if (!hasMatch) return false;
        }
        return true;
      })
      .sort(function(a, b) { return b.createdOn.localeCompare(a.createdOn); });
    const filtered = _typeFilter === 'All' ? published : published.filter(function(a) { return a.type === _typeFilter; });

    const canWrite = isAdmin || isTeacher;
    const btnLabel  = isTeacher ? 'Submit for Approval' : '+ New Announcement';

    container.innerHTML = ''
      + '<div class="flex items-center justify-between mb-6">'
      +   '<div>'
      +     '<h1 class="text-2xl font-bold text-slate-800">Communication</h1>'
      +     '<p class="text-sm text-slate-500 mt-0.5">Announcements & broadcasts to parents</p>'
      +   '</div>'
      +   (canWrite ? '<button onclick="App.Communication._newModal()" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">' + btnLabel + '</button>' : '')
      + '</div>'

      // Admin: full approval queue
      + (isAdmin && pendingAnns.length > 0
          ? '<div class="mb-5 bg-amber-50 border border-amber-200 rounded-xl overflow-hidden">'
          +   '<div class="px-4 py-2.5 border-b border-amber-200 flex items-center justify-between">'
          +     '<span class="text-sm font-semibold text-amber-800">Pending Approval (' + pendingAnns.length + ')</span>'
          +   '</div>'
          +   '<div class="divide-y divide-amber-100">'
          +   pendingAnns.map(function(ann) {
                return '<div class="px-4 py-3 flex items-start justify-between gap-3">'
                  + '<div>'
                  +   '<div class="font-semibold text-sm text-slate-800">' + App.Utils.esc(ann.title) + '</div>'
                  +   '<div class="text-xs text-slate-500 mt-0.5">' + App.Utils.esc(ann.createdBy) + ' · ' + App.Utils.formatDate(ann.createdOn) + '</div>'
                  +   '<div class="text-xs text-slate-600 mt-1">' + App.Utils.esc(ann.message.slice(0,80)) + (ann.message.length > 80 ? '…' : '') + '</div>'
                  + '</div>'
                  + '<div class="flex gap-2 shrink-0">'
                  +   '<button onclick="App.Communication._approve(\'' + ann.id + '\')" style="padding:0.3rem 0.8rem;font-size:0.75rem;font-weight:700;background:#22c55e;color:#fff;border:none;border-radius:7px;cursor:pointer">Approve</button>'
                  +   '<button onclick="App.Communication._reject(\'' + ann.id + '\')" style="padding:0.3rem 0.8rem;font-size:0.75rem;font-weight:700;background:#f1f5f9;color:#ef4444;border:none;border-radius:7px;cursor:pointer">Reject</button>'
                  + '</div>'
                  + '</div>';
              }).join('')
          +   '</div>'
          + '</div>'
          : '')

      // Teacher: their own pending submissions
      + (isTeacher
          ? (function() {
              const teacherName = _getTeacherName();
              const myPending = announcements.filter(function(a) { return a.status === 'pending_approval' && a.createdBy === teacherName; });
              if (myPending.length === 0) return '';
              return '<div class="mb-5 bg-amber-50 border border-amber-200 rounded-xl overflow-hidden">'
                + '<div class="px-4 py-2.5 border-b border-amber-200">'
                +   '<span class="text-sm font-semibold text-amber-800">My Pending Submissions (' + myPending.length + ')</span>'
                +   '<span class="text-xs text-amber-600 ml-2">Awaiting admin approval</span>'
                + '</div>'
                + '<div class="divide-y divide-amber-100">'
                + myPending.map(function(ann) {
                    return '<div class="px-4 py-3 flex items-start justify-between gap-3">'
                      + '<div class="flex-1 min-w-0">'
                      +   '<div class="font-semibold text-sm text-slate-800">' + App.Utils.esc(ann.title) + '</div>'
                      +   '<div class="text-xs text-slate-500 mt-0.5">' + App.Utils.badge(ann.type, 'yellow') + ' · ' + App.Utils.formatDate(ann.createdOn) + '</div>'
                      +   '<div class="text-xs text-slate-600 mt-1">' + App.Utils.esc(ann.message.slice(0,80)) + (ann.message.length > 80 ? '…' : '') + '</div>'
                      + '</div>'
                      + '<span class="text-xs px-2 py-1 bg-amber-100 text-amber-700 rounded-lg font-medium shrink-0">Pending</span>'
                      + '</div>';
                  }).join('')
                + '</div>'
                + '</div>';
            })()
          : '')

      + '<div class="flex gap-2 mb-5">'
      + ['All','Notice','Reminder','Urgent'].map(function(f) {
          const active = f === _typeFilter;
          const colors = { Notice:'bg-blue-600 text-white', Reminder:'bg-amber-500 text-white', Urgent:'bg-red-500 text-white' };
          return '<button onclick="App.Communication._setFilter(\'' + f + '\')" class="px-3 py-1.5 text-sm rounded-lg font-medium transition-colors ' + (active ? (colors[f] || 'bg-slate-700 text-white') : 'bg-white border border-slate-200 text-slate-600 hover:bg-slate-50') + '">' + f + '</button>';
        }).join('')
      + '</div>'

      + (filtered.length === 0
        ? '<div class="bg-white rounded-xl border border-slate-100 shadow-sm">' + App.Utils.emptyState(
            _typeFilter !== 'All' ? 'No announcements match this filter' : 'No announcements yet',
            _typeFilter !== 'All' ? 'Try selecting a different type filter.' : 'Post your first announcement to reach parents.',
            (canWrite && _typeFilter === 'All') ? '<button onclick="App.Communication._newModal()" style="padding:0.5rem 1.25rem;font-size:0.83rem;font-weight:600;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">' + btnLabel + '</button>' : ''
          ) + '</div>'
        : '<div class="space-y-3">' + filtered.map(function(ann) { return _annCard(ann, isAdmin, today); }).join('') + '</div>'
      );
  }

  function _annCard(ann, isAdmin, today) {
    today = today || App.Utils.today();
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
      +       '<h3 class="font-semibold text-slate-800">' + App.Utils.esc(ann.title) + '</h3>'
      +       '<div class="flex items-center gap-2 shrink-0">'
      +         App.Utils.statusBadge(ann.type)
      +         App.Utils.badge(ann.audience, 'gray')
      +       '</div>'
      +     '</div>'
      +     (ann.message.length > 200
          ? '<p class="text-sm text-slate-600 mt-2 leading-relaxed"><span id="ann-short-' + ann.id + '">' + App.Utils.esc(ann.message.slice(0, 200)) + '... <a href="#" onclick="event.preventDefault();document.getElementById(\'ann-short-' + ann.id + '\').style.display=\'none\';document.getElementById(\'ann-full-' + ann.id + '\').style.display=\'inline\';" style="color:var(--gold);font-weight:600;font-size:0.78rem">Show more</a></span><span id="ann-full-' + ann.id + '" style="display:none">' + App.Utils.esc(ann.message) + ' <a href="#" onclick="event.preventDefault();document.getElementById(\'ann-full-' + ann.id + '\').style.display=\'none\';document.getElementById(\'ann-short-' + ann.id + '\').style.display=\'inline\';" style="color:var(--gold);font-weight:600;font-size:0.78rem">Show less</a></span></p>'
          : '<p class="text-sm text-slate-600 mt-2 leading-relaxed">' + App.Utils.esc(ann.message) + '</p>')
      +     '<div class="flex items-center justify-between mt-3">'
      +       '<span class="text-xs text-slate-400">'
      +         App.Utils.formatDate(ann.createdOn) + ' · ' + ann.createdBy
      +         (ann.archiveOn ? ' · <span title="Auto-archives on ' + ann.archiveOn + '" style="color:' + (ann.archiveOn < today ? '#ef4444' : '#94a3b8') + '">expires ' + App.Utils.formatDate(ann.archiveOn) + '</span>' : '')
      +       '</span>'
      +       (isAdmin ? '<button onclick="App.Communication._editModal(\'' + ann.id + '\')" class="text-xs text-blue-400 hover:text-blue-600 mr-2">Edit</button>' : '')
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
    if (!confirm('Delete this announcement?')) return;
    App.Api.del('/api/announcements/' + annId).then(function() {
      return App.Api.loadSnapshot();
    }).then(function() {
      App.Utils.showToast('Announcement deleted', 'info');
      App.Router.refresh();
    }).catch(function(e) {
      App.Utils.showToast('Delete failed: ' + e.message, 'error');
    });
  }

  function _approve(annId) {
    App.Api.put('/api/announcements/' + annId + '/approve', { status: 'published' }).then(function() {
      return App.Api.loadSnapshot();
    }).then(function() {
      App.Utils.showToast('Announcement approved and published', 'success');
      App.Router.refresh();
    }).catch(function(e) {
      App.Utils.showToast('Approve failed: ' + e.message, 'error');
    });
  }

  function _reject(annId) {
    if (!confirm('Reject and delete this announcement draft?')) return;
    App.Api.del('/api/announcements/' + annId).then(function() {
      return App.Api.loadSnapshot();
    }).then(function() {
      App.Utils.showToast('Announcement rejected', 'info');
      App.Router.refresh();
    }).catch(function(e) {
      App.Utils.showToast('Reject failed: ' + e.message, 'error');
    });
  }

  function _newModal() {
    const isTeacher = App.currentRole === 'teacher';
    const isAdmin   = App.currentRole === 'admin';
    const byline    = isTeacher ? _getTeacherName() : 'Admin';
    const submitLabel = isTeacher ? 'Submit for Approval' : 'Publish';

    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-1">New Announcement</h2>'
      + (isTeacher ? '<p class="text-sm text-amber-600 bg-amber-50 px-3 py-2 rounded-lg mb-4">Your announcement will be reviewed by admin before publishing.</p>' : '<div class="mb-4"></div>')
      + '<form id="ann-form" class="space-y-4">'
      + _field('Title', '<input name="title" class="form-input" placeholder="e.g. Holiday Schedule Notice" required maxlength="150">')
      + _field('Message', '<textarea name="message" class="form-input" rows="4" placeholder="Write your message here..." required maxlength="1000"></textarea>')
      + '<div class="grid grid-cols-2 gap-4">'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Audience</label>'
      + '<select name="audience" class="form-input" id="ann-audience-sel"' + (isTeacher ? ' onchange="App.Communication._onAudienceChange(this.value)"' : '') + '>'
      + '<option>All Parents</option><option>All Staff</option>'
      + (isTeacher ? '<option>My Class Parents</option>' : '')
      + '</select></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Type</label>'
      + '<select name="type" class="form-input">'
      + '<option>Notice</option><option>Reminder</option><option>Urgent</option>'
      + '</select></div>'
      + '</div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Auto-archive on <span class="text-xs text-slate-400 font-normal">(optional — hides from parents after this date)</span></label>'
      + '<input name="archiveOn" type="date" class="form-input" min="' + App.Utils.today() + '"></div>'
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" class="px-4 py-2 text-sm ' + (isTeacher ? 'bg-amber-500 hover:bg-amber-600' : 'bg-blue-600 hover:bg-blue-700') + ' text-white rounded-lg">' + submitLabel + '</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );
    document.getElementById('ann-form').addEventListener('submit', function(e) {
      e.preventDefault();
      const fd = new FormData(e.target);
      const state = App.Store.get();
      var audience = fd.get('audience');
      var targetClassIds = null;
      if (audience === 'My Class Parents' && App.currentTeacher) {
        var { classes } = App.Store.get();
        targetClassIds = classes
          .filter(function(c) { return c.teacherIds && c.teacherIds.indexOf(App.currentTeacher) > -1; })
          .map(function(c) { return c.id; });
      }
      const newAnn = {
        title: fd.get('title').trim(),
        message: fd.get('message').trim(),
        audience: audience,
        type: fd.get('type'),
        status: isTeacher ? 'pending_approval' : 'published',
        createdBy: byline,
        archiveOn: fd.get('archiveOn') || ''
      };
      App.Api.post('/api/announcements', newAnn).then(function() {
        return App.Api.loadSnapshot();
      }).then(function() {
        App.Utils.hideModal(true);
        App.Utils.showToast(isTeacher ? 'Submitted for admin approval' : 'Announcement published!', isTeacher ? 'info' : 'success');
        App.Router.refresh();
      }).catch(function(e) {
        App.Utils.showToast('Failed: ' + e.message, 'error');
      });
    });
  }

  function _getTeacherName() {
    if (!App.currentTeacher) return 'Teacher';
    const { staff } = App.Store.get();
    const s = staff.find(function(x) { return x.id === App.currentTeacher; });
    return s ? s.fullName : 'Teacher';
  }

  function _field(label, inputHtml) {
    return '<div><label class="block text-sm font-medium text-slate-700 mb-1">' + label + '</label>' + inputHtml + '</div>';
  }

  function _editModal(annId) {
    const state = App.Store.get();
    const ann = state.announcements.find(function(a) { return a.id === annId; });
    if (!ann) return;

    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-4">Edit Announcement</h2>'
      + '<form id="edit-ann-form" class="space-y-4">'
      + _field('Title', '<input name="title" class="form-input" value="' + App.Utils.esc(ann.title) + '" required maxlength="150">')
      + _field('Message', '<textarea name="message" class="form-input" rows="4" required maxlength="1000">' + App.Utils.esc(ann.message) + '</textarea>')
      + '<div class="grid grid-cols-2 gap-4">'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Type</label>'
      + '<select name="type" class="form-input">'
      + ['Notice','Reminder','Urgent'].map(function(t) { return '<option' + (ann.type === t ? ' selected' : '') + '>' + t + '</option>'; }).join('')
      + '</select></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Auto-archive on</label>'
      + '<input name="archiveOn" type="date" class="form-input" value="' + (ann.archiveOn || '') + '"></div>'
      + '</div>'
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Save Changes</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );
    document.getElementById('edit-ann-form').addEventListener('submit', function(e) {
      e.preventDefault();
      var fd = new FormData(e.target);
      App.Api.put('/api/announcements/' + annId, {
        title: fd.get('title').trim(),
        message: fd.get('message').trim(),
        type: fd.get('type'),
        archiveOn: fd.get('archiveOn') || ''
      }).then(function() {
        return App.Api.loadSnapshot();
      }).then(function() {
        App.Utils.hideModal(true);
        App.Utils.showToast('Announcement updated', 'success');
        App.Router.refresh();
      }).catch(function(e) {
        App.Utils.showToast('Update failed: ' + e.message, 'error');
      });
    });
  }

  function _onAudienceChange(val) {
    // No-op for now; targetClassIds are set at submit time
  }

  App.Communication = { render: render, _setFilter: _setFilter, _delete: _delete, _newModal: _newModal, _approve: _approve, _reject: _reject, _editModal: _editModal, _onAudienceChange: _onAudienceChange };
})();
