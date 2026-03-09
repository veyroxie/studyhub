(function() {
  window.App = window.App || {};

  var _filterDate = '';
  var _filterClass = '';

  function render(container) {
    try {
      var isAdmin   = App.currentRole === 'admin';
      var isTeacher = App.currentRole === 'teacher';
      var isParent  = App.currentRole === 'client';

      var state = App.Store.get();
      var classes    = state.classes    || [];
      var students   = state.students   || [];
      var staff      = state.staff      || [];
      var feedbacks  = state.feedback   || [];

      if (!_filterDate) _filterDate = App.Utils.today();

      // Determine which classes to show in selector
      var availClasses = classes;
      if (isTeacher) {
        availClasses = classes.filter(function(c) {
          return c.teacherIds && c.teacherIds.indexOf(App.currentTeacher) > -1;
        });
      }
      if (isParent) {
        var myChildIds = students.filter(function(s) { return s.contact === App.clientParent; }).map(function(s) { return s.id; });
        var myClassIds = [];
        classes.forEach(function(c) {
          if ((c.teacherIds || []).length >= 0) myClassIds.push(c.id);
        });
        // For parents: show classes their children are enrolled in
        myClassIds = [];
        students.filter(function(s) { return s.contact === App.clientParent; }).forEach(function(s) {
          (s.enrolledClasses || []).forEach(function(cid) { if (myClassIds.indexOf(cid) === -1) myClassIds.push(cid); });
        });
        availClasses = classes.filter(function(c) { return myClassIds.indexOf(c.id) > -1; });
      }

      // Filter feedbacks
      var filtered = feedbacks.filter(function(fb) {
        if (_filterDate && fb.date !== _filterDate) return false;
        if (_filterClass && fb.classId !== _filterClass) return false;
        return true;
      });

      // For teacher: only their own
      if (isTeacher) {
        filtered = filtered.filter(function(fb) { return fb.teacherId === App.currentTeacher; });
      }

      // For parent: only feedbacks from their children's classes
      if (isParent) {
        var myChildIds2 = students.filter(function(s) { return s.contact === App.clientParent; }).map(function(s) { return s.id; });
        var parentClassIds = [];
        students.filter(function(s) { return s.contact === App.clientParent; }).forEach(function(s) {
          (s.enrolledClasses || []).forEach(function(cid) { if (parentClassIds.indexOf(cid) === -1) parentClassIds.push(cid); });
        });
        filtered = filtered.filter(function(fb) { return parentClassIds.indexOf(fb.classId) > -1; });
      }

      // Sort newest first
      filtered.sort(function(a, b) { return b.date.localeCompare(a.date); });

      var btnHtml = '';
      if (isTeacher) {
        btnHtml = '<button onclick="App.Feedback._logModal()" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Log Feedback</button>';
      }

      var classOptions = '<option value="">All Classes</option>'
        + availClasses.map(function(c) {
            return '<option value="' + c.id + '"' + (_filterClass === c.id ? ' selected' : '') + '>' + App.Utils.esc(c.name) + '</option>';
          }).join('');

      var filterBar = '<div class="flex gap-3 mb-5 flex-wrap items-center">'
        + '<div class="flex items-center gap-2"><label class="text-xs font-medium text-slate-500">Date</label>'
        + '<input type="date" value="' + _filterDate + '" onchange="App.Feedback._setDate(this.value)" class="text-sm border border-slate-200 rounded-lg px-3 py-1.5 focus:outline-none focus:ring-2 focus:ring-blue-400"></div>'
        + '<div class="flex items-center gap-2"><label class="text-xs font-medium text-slate-500">Class</label>'
        + '<select onchange="App.Feedback._setClass(this.value)" class="text-sm border border-slate-200 rounded-lg px-3 py-1.5 focus:outline-none focus:ring-2 focus:ring-blue-400">' + classOptions + '</select></div>'
        + (_filterDate !== App.Utils.today() || _filterClass
            ? '<button onclick="App.Feedback._clearFilters()" class="text-xs text-slate-400 hover:text-slate-600 px-2 py-1.5">Clear</button>'
            : '')
        + '</div>';

      var cardsHtml = '';
      if (filtered.length === 0) {
        cardsHtml = '<div class="bg-white rounded-xl border border-slate-100 shadow-sm p-10 text-center">'
          + '<div class="text-3xl mb-3">📋</div>'
          + '<p class="font-semibold text-slate-600 text-sm">No feedback logged</p>'
          + '<p class="text-xs text-slate-400 mt-1">'
          + (isTeacher ? 'Log feedback after each class session.' : 'No feedback for this date yet.')
          + '</p>'
          + (isTeacher ? '<button onclick="App.Feedback._logModal()" class="mt-4 px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Log Today\'s Feedback</button>' : '')
          + '</div>';
      } else {
        cardsHtml = '<div class="space-y-3">' + filtered.map(function(fb) {
          return _fbCard(fb, classes, staff, students, isAdmin, isTeacher, isParent);
        }).join('') + '</div>';
      }

      container.innerHTML = '<div class="flex items-center justify-between mb-6">'
        + '<div><h1 class="text-2xl font-bold text-slate-800">Classroom Feedback</h1>'
        + '<p class="text-sm text-slate-500 mt-0.5">'
        + (isTeacher ? 'Log daily feedback for your classes' : 'Daily class session feedback')
        + '</p></div>'
        + btnHtml
        + '</div>'
        + filterBar
        + cardsHtml;
    } catch(e) {
      container.innerHTML = '<div class="bg-red-50 border border-red-200 rounded-xl p-6 text-red-700 text-sm">Error rendering feedback: ' + e.message + '</div>';
    }
  }

  function _fbCard(fb, classes, staff, students, isAdmin, isTeacher, isParent) {
    var cls = classes.find(function(c) { return c.id === fb.classId; }) || {};
    var teacher = staff.find(function(s) { return s.id === fb.teacherId; }) || {};
    var moodColors = { Great: { bg: '#dcfce7', color: '#166534' }, Good: { bg: '#fef9c3', color: '#854d0e' }, 'Needs Work': { bg: '#fee2e2', color: '#991b1b' } };
    var mc = moodColors[fb.mood] || moodColors.Good;

    var stuNotes = '';
    if ((fb.studentNotes || []).length > 0 && !isParent) {
      stuNotes = '<div class="mt-3 border-t border-slate-100 pt-3">'
        + '<p class="text-xs font-semibold text-slate-500 mb-2">Individual Notes</p>'
        + '<div class="space-y-1.5">'
        + fb.studentNotes.filter(function(sn) { return sn.note && sn.note.trim(); }).map(function(sn) {
            var stu = students.find(function(s) { return s.id === sn.studentId; }) || {};
            return '<div class="flex gap-2 text-xs"><span class="font-medium text-slate-600 shrink-0">' + App.Utils.esc(stu.firstName || sn.studentId) + ':</span><span class="text-slate-500">' + App.Utils.esc(sn.note) + '</span></div>';
          }).join('')
        + '</div></div>';
    }

    // For parent view: only show their child's note
    if (isParent) {
      var parentChildNotes = (fb.studentNotes || []).filter(function(sn) {
        var stu = students.find(function(s) { return s.id === sn.studentId; });
        return stu && stu.contact === App.clientParent && sn.note && sn.note.trim();
      });
      if (parentChildNotes.length > 0) {
        stuNotes = '<div class="mt-3 border-t border-slate-100 pt-3">'
          + '<p class="text-xs font-semibold text-slate-500 mb-1">Note for your child</p>'
          + parentChildNotes.map(function(sn) {
              var stu = students.find(function(s) { return s.id === sn.studentId; }) || {};
              return '<p class="text-xs text-slate-600">' + App.Utils.esc(stu.firstName || '') + ': ' + App.Utils.esc(sn.note) + '</p>';
            }).join('')
          + '</div>';
      }
    }

    var canEdit = isTeacher && fb.teacherId === App.currentTeacher;
    var editBtn = canEdit
      ? '<button onclick="App.Feedback._editModal(\'' + fb.id + '\')" class="text-xs text-blue-500 hover:text-blue-700">Edit</button>'
      : '';
    var delBtn = isAdmin
      ? '<button onclick="App.Feedback._delete(\'' + fb.id + '\')" class="text-xs text-red-400 hover:text-red-600">Delete</button>'
      : '';

    return '<div class="bg-white rounded-xl border border-slate-100 shadow-sm p-5">'
      + '<div class="flex items-start justify-between gap-3">'
      + '<div class="flex-1 min-w-0">'
      + '<div class="flex items-center gap-2 flex-wrap">'
      + '<span class="font-semibold text-slate-800 text-sm">' + App.Utils.esc(cls.name || fb.classId) + '</span>'
      + '<span style="padding:0.15rem 0.55rem;border-radius:20px;font-size:0.7rem;font-weight:700;background:' + mc.bg + ';color:' + mc.color + '">' + (fb.mood || 'Good') + '</span>'
      + '</div>'
      + '<p class="text-xs text-slate-400 mt-0.5">' + App.Utils.formatDate(fb.date) + ' · ' + App.Utils.esc(teacher.fullName || teacher.name || fb.teacherId) + '</p>'
      + (fb.topic ? '<p class="text-xs font-medium text-slate-600 mt-2">Topic: ' + App.Utils.esc(fb.topic) + '</p>' : '')
      + (fb.notes ? '<p class="text-sm text-slate-600 mt-1.5 leading-relaxed">' + App.Utils.esc(fb.notes) + '</p>' : '')
      + stuNotes
      + '</div>'
      + '<div class="flex gap-2 shrink-0">' + editBtn + delBtn + '</div>'
      + '</div>'
      + '</div>';
  }

  function _logModal() {
    var state = App.Store.get();
    var classes = state.classes || [];
    var students = state.students || [];
    var myClasses = classes.filter(function(c) {
      return c.teacherIds && c.teacherIds.indexOf(App.currentTeacher) > -1;
    });

    if (myClasses.length === 0) {
      App.Utils.showToast('No classes assigned to you yet', 'info');
      return;
    }

    var classOpts = myClasses.map(function(c) {
      return '<option value="' + c.id + '">' + App.Utils.esc(c.name) + ' (' + c.day + ')</option>';
    }).join('');

    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-4">Log Classroom Feedback</h2>'
      + '<form id="fb-form" class="space-y-4">'
      + '<div class="grid grid-cols-2 gap-4">'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Class</label>'
      + '<select name="classId" id="fb-class-sel" class="form-input" onchange="App.Feedback._updateStudentList()">' + classOpts + '</select></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Date</label>'
      + '<input name="date" type="date" value="' + App.Utils.today() + '" class="form-input"></div>'
      + '</div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Topic Covered</label>'
      + '<input name="topic" class="form-input" placeholder="e.g. Multiplication Tables" maxlength="150"></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Class Mood</label>'
      + '<div class="flex gap-2">'
      + ['Great','Good','Needs Work'].map(function(m) {
          var colors = { Great:'bg-green-500', Good:'bg-amber-500', 'Needs Work':'bg-red-500' };
          return '<label class="flex-1 cursor-pointer"><input type="radio" name="mood" value="' + m + '" class="sr-only" ' + (m==='Good'?'checked':'') + '><div class="text-center py-2 px-3 rounded-lg border-2 border-transparent text-sm font-semibold text-white ' + colors[m] + ' hover:opacity-90 transition peer-checked:ring-2">' + m + '</div></label>';
        }).join('')
      + '</div></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">General Notes</label>'
      + '<textarea name="notes" class="form-input" rows="3" placeholder="How did the class go? Any observations..." maxlength="1000"></textarea></div>'
      + '<div id="fb-student-notes"><label class="block text-sm font-medium text-slate-700 mb-2">Individual Student Notes <span class="text-xs text-slate-400 font-normal">(optional)</span></label>'
      + '<div id="fb-stu-list" class="space-y-2"></div></div>'
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Save Feedback</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );

    // Populate student list for initial class
    App.Feedback._updateStudentList();

    document.getElementById('fb-form').addEventListener('submit', function(e) {
      e.preventDefault();
      var fd = new FormData(e.target);
      var classId = fd.get('classId');
      var cls = (App.Store.get().classes || []).find(function(c) { return c.id === classId; }) || {};
      var enrolledStudents = (App.Store.get().students || []).filter(function(s) {
        return (s.enrolledClasses || []).indexOf(classId) > -1;
      });
      var stuNotes = enrolledStudents.map(function(s) {
        var noteEl = document.getElementById('fb-note-' + s.id);
        return { studentId: s.id, note: noteEl ? noteEl.value.trim() : '' };
      }).filter(function(sn) { return sn.note; });

      var newFb = {
        id: App.Utils.generateId('FB'),
        classId: classId,
        date: fd.get('date') || App.Utils.today(),
        teacherId: App.currentTeacher,
        topic: fd.get('topic') ? fd.get('topic').trim() : '',
        mood: fd.get('mood') || 'Good',
        notes: fd.get('notes') ? fd.get('notes').trim() : '',
        studentNotes: stuNotes
      };
      var existing = App.Store.get().feedback || [];
      App.Store.set({ feedback: [newFb, ...existing] });
      App.Utils.hideModal();
      App.Utils.showToast('Feedback logged!', 'success');
      App.Router.refresh();
    });
  }

  function _updateStudentList() {
    var sel = document.getElementById('fb-class-sel');
    if (!sel) return;
    var classId = sel.value;
    var enrolled = (App.Store.get().students || []).filter(function(s) {
      return (s.enrolledClasses || []).indexOf(classId) > -1;
    });
    var listEl = document.getElementById('fb-stu-list');
    if (!listEl) return;
    if (enrolled.length === 0) {
      listEl.innerHTML = '<p class="text-xs text-slate-400">No students enrolled in this class</p>';
      return;
    }
    listEl.innerHTML = enrolled.map(function(s) {
      return '<div class="flex items-center gap-3">'
        + '<span class="text-sm text-slate-700 w-28 shrink-0">' + App.Utils.esc(s.firstName) + ' ' + App.Utils.esc(s.lastName) + '</span>'
        + '<input id="fb-note-' + s.id + '" class="form-input flex-1" placeholder="Optional note for ' + App.Utils.esc(s.firstName) + '..." maxlength="300">'
        + '</div>';
    }).join('');
  }

  function _editModal(fbId) {
    var state = App.Store.get();
    var feedbacks = state.feedback || [];
    var fb = feedbacks.find(function(f) { return f.id === fbId; });
    if (!fb) return;

    var classes = state.classes || [];
    var cls = classes.find(function(c) { return c.id === fb.classId; }) || {};
    var enrolled = (state.students || []).filter(function(s) {
      return (s.enrolledClasses || []).indexOf(fb.classId) > -1;
    });

    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-4">Edit Feedback — ' + App.Utils.esc(cls.name || '') + '</h2>'
      + '<form id="fb-edit-form" class="space-y-4">'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Topic Covered</label>'
      + '<input name="topic" class="form-input" value="' + App.Utils.esc(fb.topic || '') + '" maxlength="150"></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Class Mood</label>'
      + '<div class="flex gap-2">'
      + ['Great','Good','Needs Work'].map(function(m) {
          var colors = { Great:'bg-green-500', Good:'bg-amber-500', 'Needs Work':'bg-red-500' };
          return '<label class="flex-1 cursor-pointer"><input type="radio" name="mood" value="' + m + '" class="sr-only" ' + (fb.mood===m?'checked':'') + '><div class="text-center py-2 px-3 rounded-lg border-2 border-transparent text-sm font-semibold text-white ' + colors[m] + ' hover:opacity-90 transition">' + m + '</div></label>';
        }).join('')
      + '</div></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">General Notes</label>'
      + '<textarea name="notes" class="form-input" rows="3" maxlength="1000">' + App.Utils.esc(fb.notes || '') + '</textarea></div>'
      + (enrolled.length > 0
          ? '<div><label class="block text-sm font-medium text-slate-700 mb-2">Individual Notes</label>'
          + '<div class="space-y-2">'
          + enrolled.map(function(s) {
              var existing = (fb.studentNotes || []).find(function(sn) { return sn.studentId === s.id; });
              return '<div class="flex items-center gap-3">'
                + '<span class="text-sm text-slate-700 w-28 shrink-0">' + App.Utils.esc(s.firstName) + '</span>'
                + '<input id="fbedit-note-' + s.id + '" class="form-input flex-1" value="' + App.Utils.esc((existing && existing.note) || '') + '" maxlength="300">'
                + '</div>';
            }).join('')
          + '</div></div>'
          : '')
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Save Changes</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );

    document.getElementById('fb-edit-form').addEventListener('submit', function(e) {
      e.preventDefault();
      var fd = new FormData(e.target);
      var stuNotes = enrolled.map(function(s) {
        var noteEl = document.getElementById('fbedit-note-' + s.id);
        return { studentId: s.id, note: noteEl ? noteEl.value.trim() : '' };
      });
      var updated = feedbacks.map(function(f) {
        if (f.id !== fbId) return f;
        return Object.assign({}, f, {
          topic: fd.get('topic') ? fd.get('topic').trim() : f.topic,
          mood: fd.get('mood') || f.mood,
          notes: fd.get('notes') ? fd.get('notes').trim() : f.notes,
          studentNotes: stuNotes
        });
      });
      App.Store.set({ feedback: updated });
      App.Utils.hideModal();
      App.Utils.showToast('Feedback updated', 'success');
      App.Router.refresh();
    });
  }

  function _delete(fbId) {
    if (!confirm('Delete this feedback entry?')) return;
    var existing = App.Store.get().feedback || [];
    App.Store.set({ feedback: existing.filter(function(f) { return f.id !== fbId; }) });
    App.Utils.showToast('Feedback deleted', 'info');
    App.Router.refresh();
  }

  function _setDate(v) { _filterDate = v; App.Router.refresh(); }
  function _setClass(v) { _filterClass = v; App.Router.refresh(); }
  function _clearFilters() { _filterDate = App.Utils.today(); _filterClass = ''; App.Router.refresh(); }

  App.Feedback = {
    render: render,
    _logModal: _logModal,
    _editModal: _editModal,
    _delete: _delete,
    _updateStudentList: _updateStudentList,
    _setDate: _setDate,
    _setClass: _setClass,
    _clearFilters: _clearFilters
  };
})();
