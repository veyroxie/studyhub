(function() {
  window.App = window.App || {};

  // Term cadence: 4-month windows. Owner runs the centre on a Jan-Apr,
  // May-Aug, Sep-Dec rhythm. The label uses the year + window number so
  // the term sorts naturally (latest first) and reads cleanly on the PDF.
  function _termsForYear(year) {
    return [
      { id: year + '-T1', label: year + ' · Jan–Apr' },
      { id: year + '-T2', label: year + ' · May–Aug' },
      { id: year + '-T3', label: year + ' · Sep–Dec' }
    ];
  }

  function _currentTerm() {
    var d = new Date();
    var y = d.getFullYear();
    var m = d.getMonth();
    var t = m < 4 ? 'T1' : m < 8 ? 'T2' : 'T3';
    return y + '-' + t;
  }

  function _termOptions(selected) {
    var thisYear = new Date().getFullYear();
    var years = [thisYear - 1, thisYear, thisYear + 1];
    var opts = [];
    years.forEach(function(y) {
      _termsForYear(y).forEach(function(t) {
        opts.push('<option value="' + t.id + '"' + (t.id === selected ? ' selected' : '') + '>' + t.label + '</option>');
      });
    });
    return opts.join('');
  }

  function _termLabel(termId) {
    if (!termId) return '';
    var year = termId.slice(0, 4);
    var t = termId.slice(5);
    var window = t === 'T1' ? 'Jan–Apr' : t === 'T2' ? 'May–Aug' : 'Sep–Dec';
    return year + ' · ' + window;
  }

  var _filterTerm = '';
  var _filterStudent = '';

  function render(container) {
    try {
      var isAdmin   = App.currentRole === 'admin';
      var isTeacher = App.currentRole === 'teacher';
      var isParent  = App.currentRole === 'client';
      if (isParent) {
        _renderParent(container);
      } else {
        _renderStaff(container, isAdmin || isTeacher);
      }
    } catch (e) {
      container.innerHTML = '<div class="bg-red-50 border border-red-200 rounded-xl p-6 text-red-700 text-sm">Error rendering progress reports: ' + App.Utils.esc(e.message) + '</div>';
    }
  }

  // ── Parent view ──────────────────────────────────────────────────────────
  function _renderParent(container) {
    var s = App.Store.get();
    var students = (s.students || []).filter(function(st) {
      return st.contact === App.clientParent && st.status !== 'Inactive';
    });
    var invoices = s.invoices || [];
    var reports  = (s.progressReports || []);

    var hasUnpaid = invoices.some(function(i) {
      return i.type === 'Monthly' && (i.status === 'Unpaid' || i.status === 'Overdue');
    });

    var gateBanner = hasUnpaid
      ? '<div style="background:#fef3c7;border:1px solid #fde68a;border-left:4px solid #d97706;border-radius:12px;padding:1rem 1.25rem;margin-bottom:1.25rem">'
        + '<div style="font-size:0.92rem;font-weight:700;color:#92400e">Progress reports paused</div>'
        + '<div style="font-size:0.83rem;color:#78350f;margin-top:3px">Settle this month\'s invoice to download your child\'s termly reports.</div>'
        + '</div>'
      : '';

    var childIds = students.map(function(st) { return st.id; });
    var byChild = {};
    reports.forEach(function(pr) {
      if (childIds.indexOf(pr.studentId) === -1) return;
      (byChild[pr.studentId] = byChild[pr.studentId] || []).push(pr);
    });

    var body = '';
    if (students.length === 0) {
      body = App.Utils.emptyState('No children on file', 'Once your child is enrolled, their termly progress reports appear here.');
    } else {
      students.forEach(function(st) {
        var list = byChild[st.id] || [];
        body += '<div style="background:#fff;border:1px solid #e2e8f0;border-radius:14px;padding:1.25rem;margin-bottom:1rem">'
          + '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.75rem">'
          +   '<div style="font-size:1.05rem;font-weight:700;color:#0f0f0f">' + App.Utils.esc(st.firstName + ' ' + st.lastName) + '</div>'
          +   '<div style="font-size:0.75rem;color:#94a3b8">' + list.length + ' report' + (list.length !== 1 ? 's' : '') + '</div>'
          + '</div>';
        if (list.length === 0) {
          body += '<div style="font-size:0.85rem;color:#94a3b8;padding:0.5rem 0">No reports published yet.</div>';
        } else {
          body += '<div style="display:grid;gap:0.75rem">';
          list.forEach(function(pr) {
            body += '<div style="border:1px solid #e2e8f0;border-radius:10px;padding:0.85rem 1rem;display:flex;align-items:center;justify-content:space-between;gap:1rem">'
              + '<div>'
              +   '<div style="font-size:0.85rem;font-weight:700;color:#0f0f0f">' + App.Utils.esc(_termLabel(pr.term)) + (pr.subject ? ' · ' + App.Utils.esc(pr.subject) : '') + '</div>'
              +   (pr.grade ? '<div style="font-size:0.78rem;color:#92400e;font-weight:600;margin-top:2px">Grade: ' + App.Utils.esc(pr.grade) + '</div>' : '')
              + '</div>'
              + (hasUnpaid
                  ? '<span style="font-size:0.72rem;color:#94a3b8;font-style:italic">paused</span>'
                  : '<a href="/api/progress-reports/' + pr.id + '/pdf" target="_blank" style="padding:0.4rem 0.85rem;font-size:0.78rem;font-weight:700;background:var(--gold);color:#0a0a0a;border-radius:8px;text-decoration:none">Download PDF</a>')
              + '</div>';
          });
          body += '</div>';
        }
        body += '</div>';
      });
    }

    container.innerHTML = '<div>'
      + '<h1 class="text-2xl font-bold text-slate-800 mb-1">Progress Reports</h1>'
      + '<p class="text-sm text-slate-500 mb-5">Termly reports from your child\'s teacher.</p>'
      + gateBanner
      + body
      + '</div>';
  }

  // ── Staff view (admin + teacher) ──────────────────────────────────────────
  function _renderStaff(container, canEdit) {
    var s = App.Store.get();
    var students = s.students || [];
    var staff    = s.staff    || [];
    var reports  = s.progressReports || [];
    var isTeacher = App.currentRole === 'teacher';

    // Teachers only see students enrolled in their classes — match calendar
    // and students-module behaviour.
    var visibleStudents = students;
    if (isTeacher && App.currentTeacher) {
      var teacherClassIds = (s.classes || [])
        .filter(function(c) { return (c.teacherIds || []).indexOf(App.currentTeacher) > -1; })
        .map(function(c) { return c.id; });
      visibleStudents = students.filter(function(st) {
        return (st.enrolledClasses || []).some(function(cid) { return teacherClassIds.indexOf(cid) > -1; });
      });
    }
    var visibleIds = visibleStudents.map(function(st) { return st.id; });

    var filtered = reports.filter(function(pr) {
      if (visibleIds.indexOf(pr.studentId) === -1) return false;
      if (_filterTerm && pr.term !== _filterTerm) return false;
      if (_filterStudent && pr.studentId !== _filterStudent) return false;
      return true;
    });

    var studentOpts = '<option value="">All students</option>'
      + visibleStudents.map(function(st) {
          return '<option value="' + st.id + '"' + (_filterStudent === st.id ? ' selected' : '') + '>' + App.Utils.esc(st.firstName + ' ' + st.lastName) + '</option>';
        }).join('');

    var rows = filtered.length === 0
      ? App.Utils.emptyState('No progress reports yet',
          canEdit ? 'Click below to create the first one for the current term.' : 'Reports will appear here once published.',
          canEdit ? '<button class="sh-btn-primary" onclick="App.Progress._newModal()">+ New report</button>' : '')
      : filtered.map(function(pr) {
          var st = students.find(function(x) { return x.id === pr.studentId; });
          var teacher = staff.find(function(x) { return x.id === pr.teacherId; });
          return '<div style="background:#fff;border:1px solid #e2e8f0;border-radius:12px;padding:1rem 1.15rem;margin-bottom:0.75rem;display:flex;align-items:flex-start;gap:1rem">'
            + '<div style="flex:1;min-width:0">'
            +   '<div style="display:flex;align-items:center;gap:0.6rem;margin-bottom:4px">'
            +     '<span style="font-size:0.92rem;font-weight:700;color:#0f0f0f">' + (st ? App.Utils.esc(st.firstName + ' ' + st.lastName) : pr.studentId) + '</span>'
            +     '<span style="font-size:0.7rem;font-weight:700;padding:1px 7px;border-radius:99px;background:' + (pr.published ? '#dcfce7' : '#f1f5f9') + ';color:' + (pr.published ? '#166534' : '#64748b') + '">' + (pr.published ? 'PUBLISHED' : 'DRAFT') + '</span>'
            +   '</div>'
            +   '<div style="font-size:0.78rem;color:#64748b">' + App.Utils.esc(_termLabel(pr.term)) + (pr.subject ? ' · ' + App.Utils.esc(pr.subject) : '') + (teacher ? ' · ' + App.Utils.esc(teacher.fullName || teacher.name) : '') + '</div>'
            +   (pr.grade ? '<div style="font-size:0.78rem;color:#92400e;font-weight:600;margin-top:3px">Grade: ' + App.Utils.esc(pr.grade) + '</div>' : '')
            + '</div>'
            + '<div style="display:flex;gap:0.4rem;flex-shrink:0">'
            +   (canEdit ? '<button onclick="App.Progress._editModal(\'' + pr.id + '\')" style="padding:0.35rem 0.75rem;font-size:0.72rem;font-weight:600;background:#fff;color:#475569;border:1px solid #e2e8f0;border-radius:7px;cursor:pointer">Edit</button>' : '')
            +   '<a href="/api/progress-reports/' + pr.id + '/pdf" target="_blank" style="padding:0.35rem 0.75rem;font-size:0.72rem;font-weight:600;background:var(--gold);color:#0a0a0a;border-radius:7px;text-decoration:none">PDF</a>'
            + '</div>'
            + '</div>';
        }).join('');

    container.innerHTML = '<div>'
      + '<div class="flex items-center justify-between mb-5">'
      +   '<div>'
      +     '<h1 class="text-2xl font-bold text-slate-800">Progress Reports</h1>'
      +     '<p class="text-sm text-slate-500 mt-0.5">Termly reports — replaces per-class feedback notes.</p>'
      +   '</div>'
      +   (canEdit ? '<button onclick="App.Progress._newModal()" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">+ New report</button>' : '')
      + '</div>'
      + '<div style="display:flex;gap:0.6rem;margin-bottom:1.25rem;flex-wrap:wrap">'
      +   '<select onchange="App.Progress._setStudentFilter(this.value)" class="text-sm border border-slate-200 rounded-lg px-3 py-1.5">' + studentOpts + '</select>'
      +   '<select onchange="App.Progress._setTermFilter(this.value)" class="text-sm border border-slate-200 rounded-lg px-3 py-1.5">'
      +     '<option value="">All terms</option>' + _termOptions(_filterTerm)
      +   '</select>'
      +   ((_filterTerm || _filterStudent)
        ? '<button onclick="App.Progress._clearFilters()" class="text-xs text-slate-500 border border-slate-200 rounded-lg px-3 py-1.5">Clear</button>'
        : '')
      + '</div>'
      + rows
      + '</div>';
  }

  function _setTermFilter(v) { _filterTerm = v; App.Router.refresh(); }
  function _setStudentFilter(v) { _filterStudent = v; App.Router.refresh(); }
  function _clearFilters() { _filterTerm = ''; _filterStudent = ''; App.Router.refresh(); }

  function _newModal() {
    _showForm({ term: _currentTerm(), published: false });
  }

  function _editModal(prId) {
    var pr = (App.Store.get().progressReports || []).find(function(x) { return x.id === prId; });
    if (!pr) return;
    _showForm(pr);
  }

  function _showForm(pr) {
    var s = App.Store.get();
    var students = s.students || [];
    var staff    = s.staff || [];
    var isTeacher = App.currentRole === 'teacher';

    if (isTeacher && App.currentTeacher) {
      var teacherClassIds = (s.classes || [])
        .filter(function(c) { return (c.teacherIds || []).indexOf(App.currentTeacher) > -1; })
        .map(function(c) { return c.id; });
      students = students.filter(function(st) {
        return (st.enrolledClasses || []).some(function(cid) { return teacherClassIds.indexOf(cid) > -1; });
      });
    }

    var studentOpts = students.map(function(st) {
      return '<option value="' + st.id + '"' + (st.id === pr.studentId ? ' selected' : '') + '>' + App.Utils.esc(st.firstName + ' ' + st.lastName) + '</option>';
    }).join('');

    var teacherOpts = '<option value="">— none —</option>'
      + staff.map(function(t) {
          return '<option value="' + t.id + '"' + (t.id === pr.teacherId ? ' selected' : '') + '>' + App.Utils.esc(t.fullName || t.name) + '</option>';
        }).join('');

    var isEdit = !!pr.id;
    var publishedChecked = pr.published ? ' checked' : '';

    App.Utils.showModal(
      '<div class="p-6" style="min-width:520px;max-width:640px">'
      + '<h2 class="text-xl font-bold mb-1">' + (isEdit ? 'Edit progress report' : 'New progress report') + '</h2>'
      + '<p class="text-sm text-slate-500 mb-4">Drafts stay private to staff. Tick "Publish" to release the report to the parent.</p>'
      + '<form id="progress-form" class="space-y-3">'
      + '<div class="grid grid-cols-2 gap-3">'
      +   '<div><label class="block text-sm font-medium text-slate-700 mb-1">Student</label><select name="studentId" class="form-input" required>' + studentOpts + '</select></div>'
      +   '<div><label class="block text-sm font-medium text-slate-700 mb-1">Term</label><select name="term" class="form-input" required>' + _termOptions(pr.term || _currentTerm()) + '</select></div>'
      + '</div>'
      + '<div class="grid grid-cols-3 gap-3">'
      +   '<div class="col-span-2"><label class="block text-sm font-medium text-slate-700 mb-1">Subject</label><input name="subject" class="form-input" value="' + App.Utils.esc(pr.subject || '') + '" placeholder="e.g. English"></div>'
      +   '<div><label class="block text-sm font-medium text-slate-700 mb-1">Grade</label><input name="grade" class="form-input" value="' + App.Utils.esc(pr.grade || '') + '" placeholder="e.g. A2"></div>'
      + '</div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Teacher</label><select name="teacherId" class="form-input">' + teacherOpts + '</select></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Strengths</label><textarea name="strengths" rows="3" class="form-input" placeholder="What is the child doing well?">' + App.Utils.esc(pr.strengths || '') + '</textarea></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Areas to improve</label><textarea name="areasToImprove" rows="3" class="form-input">' + App.Utils.esc(pr.areasToImprove || '') + '</textarea></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Teacher\'s comment</label><textarea name="teacherComment" rows="3" class="form-input">' + App.Utils.esc(pr.teacherComment || '') + '</textarea></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Focus for next term</label><textarea name="nextTermFocus" rows="2" class="form-input">' + App.Utils.esc(pr.nextTermFocus || '') + '</textarea></div>'
      + '<label class="flex items-center gap-2 text-sm text-slate-700 mt-2"><input type="checkbox" name="published"' + publishedChecked + '> Publish (parent can download)</label>'
      + '<div class="flex justify-end gap-2 pt-2">'
      + (isEdit ? '<button type="button" onclick="App.Progress._delete(\'' + pr.id + '\')" class="mr-auto px-3 py-2 text-sm border border-red-200 text-red-600 rounded-lg hover:bg-red-50">Delete</button>' : '')
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">' + (isEdit ? 'Save' : 'Create') + '</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );

    document.getElementById('progress-form').addEventListener('submit', async function(e) {
      e.preventDefault();
      var fd = new FormData(e.target);
      var payload = {
        studentId: fd.get('studentId'),
        term: fd.get('term'),
        teacherId: fd.get('teacherId') || '',
        subject: fd.get('subject') || '',
        grade: fd.get('grade') || '',
        strengths: fd.get('strengths') || '',
        areasToImprove: fd.get('areasToImprove') || '',
        teacherComment: fd.get('teacherComment') || '',
        nextTermFocus: fd.get('nextTermFocus') || '',
        published: fd.get('published') === 'on'
      };
      try {
        var res;
        if (isEdit) {
          res = await App.Api.put('/api/progress-reports/' + pr.id, payload);
        } else {
          res = await App.Api.post('/api/progress-reports', payload);
        }
        var state = App.Store.get();
        var list = (state.progressReports || []).slice();
        var idx = list.findIndex(function(x) { return x.id === res.id; });
        if (idx >= 0) list[idx] = res; else list.unshift(res);
        App.Store.set({ progressReports: list });
        App.Utils.hideModal(true);
        App.Utils.showToast(isEdit ? 'Report saved' : 'Report created', 'success');
        App.Router.refresh();
      } catch (err) {
        App.Utils.showToast(err.message || 'Could not save report', 'error');
      }
    });
  }

  async function _delete(prId) {
    var ok = await App.Utils.showConfirm({ title: 'Delete progress report', message: 'This cannot be undone.', confirmLabel: 'Delete', danger: true });
    if (!ok) return;
    try {
      await App.Api.del('/api/progress-reports/' + prId);
      var state = App.Store.get();
      App.Store.set({ progressReports: (state.progressReports || []).filter(function(x) { return x.id !== prId; }) });
      App.Utils.hideModal(true);
      App.Utils.showToast('Report deleted', 'info');
      App.Router.refresh();
    } catch (err) {
      App.Utils.showToast(err.message || 'Could not delete', 'error');
    }
  }

  App.Progress = {
    render: render,
    _setTermFilter: _setTermFilter,
    _setStudentFilter: _setStudentFilter,
    _clearFilters: _clearFilters,
    _newModal: _newModal,
    _editModal: _editModal,
    _delete: _delete
  };
})();
