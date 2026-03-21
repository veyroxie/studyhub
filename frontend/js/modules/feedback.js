(function() {
  window.App = window.App || {};

  var _filterDate  = '';
  var _filterClass = '';
  var _filterChild = '';
  var _feedbackPage = 0;
  var _feedbackParentPage = 0;
  var _FB_PAGE_SIZE = 10;

  function _paginationControls(page, total, moduleFn) {
    var totalPages = Math.ceil(total / _FB_PAGE_SIZE);
    if (total <= _FB_PAGE_SIZE) return '';
    var start = page * _FB_PAGE_SIZE + 1;
    var end = Math.min((page + 1) * _FB_PAGE_SIZE, total);
    var prevDis = page === 0;
    var nextDis = page >= totalPages - 1;
    return '<div style="display:flex;align-items:center;justify-content:space-between;margin-top:1rem;padding:0.75rem 0;">'
      + '<span style="font-size:0.8rem;color:#64748b;">Showing ' + start + '–' + end + ' of ' + total + '</span>'
      + '<div style="display:flex;gap:0.5rem;">'
      + '<button onclick="' + moduleFn + '(' + (page - 1) + ')"' + (prevDis ? ' disabled' : '') + ' style="padding:0.35rem 0.75rem;font-size:0.8rem;border:1px solid #e2e8f0;border-radius:8px;cursor:' + (prevDis ? 'default' : 'pointer') + ';background:#fff;color:#374151;' + (prevDis ? 'opacity:0.4;' : '') + '">Prev</button>'
      + '<button onclick="' + moduleFn + '(' + (page + 1) + ')"' + (nextDis ? ' disabled' : '') + ' style="padding:0.35rem 0.75rem;font-size:0.8rem;border:1px solid #e2e8f0;border-radius:8px;cursor:' + (nextDis ? 'default' : 'pointer') + ';background:#fff;color:#374151;' + (nextDis ? 'opacity:0.4;' : '') + '">Next</button>'
      + '</div></div>';
  }

  // ─── helpers ────────────────────────────────────────────────────────────────

  var MOOD_STYLE = {
    Great:        { bg: '#dcfce7', color: '#166534', label: 'Great' },
    Good:         { bg: '#fef9c3', color: '#854d0e', label: 'Good' },
    'Needs Work': { bg: '#fee2e2', color: '#991b1b', label: 'Needs Work' }
  };

  function _moodStyle(mood) { return MOOD_STYLE[mood] || MOOD_STYLE.Good; }

  // ─── render (entry point) ───────────────────────────────────────────────────

  function render(container) {
    try {
      var isAdmin   = App.currentRole === 'admin';
      var isTeacher = App.currentRole === 'teacher';
      var isParent  = App.currentRole === 'client';

      var state    = App.Store.get();
      var classes  = state.classes   || [];
      var students = state.students  || [];
      var staff    = state.staff     || [];
      var feedbacks= state.feedback  || [];

      // default: show all dates (empty = no filter)

      if (isParent) {
        _renderParent(container, classes, students, staff, feedbacks);
      } else {
        _renderStaff(container, classes, students, staff, feedbacks, isAdmin, isAdmin || isTeacher);
      }
    } catch(e) {
      container.innerHTML = '<div class="bg-red-50 border border-red-200 rounded-xl p-6 text-red-700 text-sm">Error rendering feedback: ' + App.Utils.esc(e.message) + '</div>';
    }
  }

  // ─── PARENT VIEW ────────────────────────────────────────────────────────────

  function _renderParent(container, classes, students, staff, feedbacks) {
    // Find this parent's children
    var myChildren = students.filter(function(s) {
      return s.contact === App.clientParent && s.status !== 'Inactive';
    });

    // Collect all class ids across all children (or just the selected child)
    var targetChildren = myChildren;
    if (_filterChild) {
      targetChildren = myChildren.filter(function(s) { return s.id === _filterChild; });
    }

    var parentClassIds = [];
    targetChildren.forEach(function(s) {
      (s.enrolledClasses || []).forEach(function(cid) {
        if (parentClassIds.indexOf(cid) === -1) parentClassIds.push(cid);
      });
    });

    // Filter feedbacks: only from those class ids, last 30 days (or all), sorted newest first
    var cutoff = '';
    (function() {
      var d = new Date();
      d.setDate(d.getDate() - 30);
      cutoff = d.toISOString().slice(0, 10);
    })();

    var filtered = feedbacks.filter(function(fb) {
      return parentClassIds.indexOf(fb.classId) > -1 && fb.date >= cutoff;
    });
    filtered.sort(function(a, b) { return b.date.localeCompare(a.date); });

    // ── child-selector pill row (only if more than 1 child) ──
    var pillRow = '';
    if (myChildren.length > 1) {
      var pills = '<div style="display:flex;gap:0.5rem;flex-wrap:wrap;margin-bottom:1.25rem;">';
      var allActive = !_filterChild;
      pills += '<button onclick="App.Feedback._setChild(\'\')" style="'
        + 'padding:0.35rem 0.9rem;border-radius:20px;font-size:0.8rem;font-weight:600;cursor:pointer;border:2px solid #C9A227;'
        + (allActive ? 'background:#C9A227;color:#fff;' : 'background:transparent;color:#C9A227;')
        + '">All children</button>';
      myChildren.forEach(function(s) {
        var active = _filterChild === s.id;
        pills += '<button onclick="App.Feedback._setChild(\'' + s.id + '\')" style="'
          + 'padding:0.35rem 0.9rem;border-radius:20px;font-size:0.8rem;font-weight:600;cursor:pointer;border:2px solid #C9A227;'
          + (active ? 'background:#C9A227;color:#fff;' : 'background:transparent;color:#C9A227;')
          + '">' + App.Utils.esc(s.firstName) + '</button>';
      });
      pills += '</div>';
      pillRow = pills;
    }

    // ── feedback cards ──
    var cardsHtml = '';
    if (filtered.length === 0) {
      cardsHtml = '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);box-shadow:0 1px 3px rgba(0,0,0,0.06);padding:2.5rem;text-align:center;">'
        + '<p style="font-weight:600;color:#475569;font-size:0.95rem;">No feedback logged yet</p>'
        + '<p style="font-size:0.8rem;color:#94a3b8;margin-top:0.35rem;">No feedback for your child\'s classes in the last 30 days.</p>'
        + '</div>';
    } else {
      var pagedParent = filtered.slice(_feedbackParentPage * _FB_PAGE_SIZE, (_feedbackParentPage + 1) * _FB_PAGE_SIZE);
      cardsHtml = pagedParent.map(function(fb) {
        return _fbCardParent(fb, classes, staff, students);
      }).join('')
      + _paginationControls(_feedbackParentPage, filtered.length, 'App.Feedback._setParentPage');
    }

    // Count older entries beyond 30 days
    var olderCount = feedbacks.filter(function(fb) {
      return parentClassIds.indexOf(fb.classId) > -1 && fb.date < cutoff;
    }).length;

    var cutoffInfo = '<p style="font-size:0.78rem;color:#94a3b8;margin-top:0.3rem;">Showing feedback from the last 30 days'
      + (olderCount > 0 ? ' &middot; ' + olderCount + ' older entr' + (olderCount === 1 ? 'y' : 'ies') + ' not shown' : '')
      + '</p>';

    container.innerHTML = '<div style="max-width:680px;">'
      + '<div style="margin-bottom:1.5rem;">'
      + '<h1 style="font-size:1.5rem;font-weight:700;color:#1e293b;">Class Feedback</h1>'
      + '<p style="font-size:0.85rem;color:#64748b;margin-top:0.2rem;">How your child\'s classes are going</p>'
      + cutoffInfo
      + '</div>'
      + pillRow
      + '<div style="display:flex;flex-direction:column;gap:0.9rem;">'
      + cardsHtml
      + '</div>'
      + '</div>';
  }

  function _fbCardParent(fb, classes, staff, students) {
    var cls     = classes.find(function(c) { return c.id === fb.classId; }) || {};
    var teacher = staff.find(function(s) { return s.id === fb.teacherId; }) || {};
    var ms      = _moodStyle(fb.mood);
    var teacherName = App.Utils.esc(teacher.fullName || teacher.name || 'Teacher');

    // Find child notes that belong to THIS parent
    var childNotes = (fb.studentNotes || []).filter(function(sn) {
      var stu = students.find(function(s) { return s.id === sn.studentId; });
      return stu && stu.contact === App.clientParent && sn.note && sn.note.trim();
    });

    // Build per-child note blocks
    var noteBlocks = childNotes.map(function(sn) {
      var stu = students.find(function(s) { return s.id === sn.studentId; }) || {};
      return '<div style="background:#fefce8;border-left:3px solid #C9A227;padding:0.6rem 0.8rem;border-radius:0 8px 8px 0;margin-top:0.65rem;">'
        + '<p style="font-size:0.7rem;font-weight:700;color:#92400e;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.25rem;">Note for ' + App.Utils.esc(stu.firstName || 'your child') + '</p>'
        + '<p style="font-size:0.85rem;color:#44403c;line-height:1.5;">' + App.Utils.esc(sn.note) + '</p>'
        + '</div>';
    }).join('');

    return '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);box-shadow:0 1px 3px rgba(0,0,0,0.06);padding:1.1rem 1.25rem;">'
      // Header row: class name + mood pill
      + '<div style="display:flex;align-items:center;gap:0.6rem;flex-wrap:wrap;margin-bottom:0.3rem;">'
      + '<span style="font-size:1rem;font-weight:700;color:#1e293b;">' + App.Utils.esc(cls.name || fb.classId) + '</span>'
      + '<span style="padding:0.15rem 0.6rem;border-radius:20px;font-size:0.7rem;font-weight:700;background:' + ms.bg + ';color:' + ms.color + ';">' + ms.label + '</span>'
      + '</div>'
      // Sub: date · teacher
      + '<p style="font-size:0.78rem;color:#94a3b8;margin-bottom:0.5rem;">'
      + App.Utils.formatDate(fb.date) + ' &middot; ' + teacherName
      + '</p>'
      // Topic chip
      + (fb.topic
          ? '<span style="display:inline-block;font-size:0.72rem;background:#f1f5f9;color:#475569;border-radius:6px;padding:0.15rem 0.55rem;margin-bottom:0.55rem;font-weight:500;">Topic: ' + App.Utils.esc(fb.topic) + '</span>'
          : '')
      // General notes
      + (fb.notes
          ? '<p style="font-size:0.875rem;color:#64748b;font-style:italic;line-height:1.55;">' + App.Utils.esc(fb.notes) + '</p>'
          : '')
      // Child individual note(s)
      + noteBlocks
      + '</div>';
  }

  // ─── ADMIN / TEACHER VIEW ───────────────────────────────────────────────────

  function _renderStaff(container, classes, students, staff, feedbacks, isAdmin, isTeacher) {
    var canLog = isAdmin || isTeacher;
    var availClasses = classes;
    if (isTeacher && !isAdmin) {
      availClasses = classes.filter(function(c) {
        return c.teacherIds && c.teacherIds.indexOf(App.currentTeacher) > -1;
      });
    }

    // Filter feedbacks
    var filtered = feedbacks.filter(function(fb) {
      if (_filterDate  && fb.date    !== _filterDate)  return false;
      if (_filterClass && fb.classId !== _filterClass) return false;
      return true;
    });
    if (isTeacher && !isAdmin) {
      filtered = filtered.filter(function(fb) { return fb.teacherId === App.currentTeacher; });
    }
    filtered.sort(function(a, b) { return b.date.localeCompare(a.date); });

    // Filter bar
    var classOptions = '<option value="">All Classes</option>'
      + availClasses.map(function(c) {
          return '<option value="' + c.id + '"' + (_filterClass === c.id ? ' selected' : '') + '>' + App.Utils.esc(c.name) + '</option>';
        }).join('');

    var showReset = !!_filterDate || !!_filterClass;

    var filterBar = '<div class="flex flex-wrap gap-3 items-center mb-6 p-4 bg-white rounded-xl border border-slate-100 shadow-sm">'
      + '<div class="flex items-center gap-2">'
      + '<label class="text-xs font-semibold text-slate-500 whitespace-nowrap">Date</label>'
      + '<input type="date" value="' + _filterDate + '" onchange="App.Feedback._setDate(this.value)" class="text-sm border border-slate-200 rounded-lg px-3 py-1.5 focus:outline-none focus:ring-2 focus:ring-amber-400">'
      + '</div>'
      + '<div class="flex items-center gap-2">'
      + '<label class="text-xs font-semibold text-slate-500 whitespace-nowrap">Class</label>'
      + '<select onchange="App.Feedback._setClass(this.value)" class="text-sm border border-slate-200 rounded-lg px-3 py-1.5 focus:outline-none focus:ring-2 focus:ring-amber-400">' + classOptions + '</select>'
      + '</div>'
      + (showReset
          ? '<button onclick="App.Feedback._clearFilters()" class="text-xs text-slate-400 hover:text-slate-700 border border-slate-200 rounded-lg px-3 py-1.5 hover:bg-slate-50 transition">Reset</button>'
          : '')
      + '<div class="ml-auto">'
      + (canLog
          ? '<button onclick="App.Feedback._logModal()" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700 font-medium">Log Feedback</button>'
          : '')
      + '</div>'
      + '</div>';

    // Cards
    var cardsHtml = '';
    if (filtered.length === 0) {
      cardsHtml = '<div class="bg-white rounded-xl border border-slate-100 shadow-sm p-10 text-center">'
        + '<p class="font-semibold text-slate-600 text-sm">No feedback logged</p>'
        + '<p class="text-xs text-slate-400 mt-1">'
        + (canLog ? 'Log feedback after each class session.' : 'No feedback for this date / class yet.')
        + '</p>'
        + (canLog ? '<button onclick="App.Feedback._logModal()" class="mt-4 px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Log Today\'s Feedback</button>' : '')
        + '</div>';
    } else {
      var pagedStaff = filtered.slice(_feedbackPage * _FB_PAGE_SIZE, (_feedbackPage + 1) * _FB_PAGE_SIZE);
      cardsHtml = '<div class="space-y-3">'
        + pagedStaff.map(function(fb) {
            return _fbCardStaff(fb, classes, staff, students, isAdmin, isTeacher);
          }).join('')
        + '</div>'
        + _paginationControls(_feedbackPage, filtered.length, 'App.Feedback._setStaffPage');
    }

    // Header (log button is inside filter bar for teacher)
    var adminLogBtn = isAdmin
      ? '' // admin doesn't log — teachers do
      : '';

    container.innerHTML = '<div class="flex items-center justify-between mb-5">'
      + '<div>'
      + '<h1 class="text-2xl font-bold text-slate-800">Classroom Feedback</h1>'
      + '<p class="text-sm text-slate-500 mt-0.5">'
      + (canLog ? 'Log daily feedback for your classes' : 'All class session feedback')
      + '</p>'
      + '</div>'
      + '</div>'
      + filterBar
      + cardsHtml;
  }

  function _fbCardStaff(fb, classes, staff, students, isAdmin, isTeacher) {
    var cls     = classes.find(function(c) { return c.id === fb.classId; }) || {};
    var teacher = staff.find(function(s) { return s.id === fb.teacherId; }) || {};
    var ms      = _moodStyle(fb.mood);

    // Enrolled count chip
    var enrolledCount = students.filter(function(s) {
      return (s.enrolledClasses || []).indexOf(fb.classId) > -1;
    }).length;
    var countChip = '<span class="text-xs text-slate-400 bg-slate-100 rounded-full px-2 py-0.5 ml-1">'
      + enrolledCount + ' student' + (enrolledCount !== 1 ? 's' : '') + '</span>';

    // Individual student notes (admin sees all)
    var stuNotes = '';
    var notesList = (fb.studentNotes || []).filter(function(sn) { return sn.note && sn.note.trim(); });
    if (notesList.length > 0) {
      stuNotes = '<div class="mt-3 border-t border-slate-100 pt-3">'
        + '<p class="text-xs font-semibold text-slate-500 mb-2">Individual Notes</p>'
        + '<div class="space-y-1.5">'
        + notesList.map(function(sn) {
            var stu = students.find(function(s) { return s.id === sn.studentId; }) || {};
            return '<div class="flex gap-2 text-xs">'
              + '<span class="font-medium text-slate-600 shrink-0 w-24">' + App.Utils.esc(stu.firstName || sn.studentId) + ':</span>'
              + '<span class="text-slate-500">' + App.Utils.esc(sn.note) + '</span>'
              + '</div>';
          }).join('')
        + '</div></div>';
    }

    var canEdit = isAdmin || (isTeacher && fb.teacherId === App.currentTeacher);
    var editBtn = canEdit
      ? '<button onclick="App.Feedback._editModal(\'' + fb.id + '\')" class="text-xs text-blue-500 hover:text-blue-700 font-medium">Edit</button>'
      : '';
    var delBtn  = isAdmin
      ? '<button onclick="App.Feedback._delete(\'' + fb.id + '\')" class="text-xs text-red-400 hover:text-red-600 font-medium">Delete</button>'
      : '';

    var moodPill = '<span style="padding:0.15rem 0.55rem;border-radius:20px;font-size:0.7rem;font-weight:700;background:' + ms.bg + ';color:' + ms.color + ';">' + ms.label + '</span>';

    return '<div class="bg-white rounded-xl border border-slate-100 shadow-sm p-5">'
      + '<div class="flex items-start justify-between gap-3">'
      + '<div class="flex-1 min-w-0">'
      + '<div class="flex items-center gap-2 flex-wrap">'
      + '<span class="font-bold text-slate-800 text-sm">' + App.Utils.esc(cls.name || fb.classId) + '</span>'
      + moodPill
      + countChip
      + '</div>'
      + '<p class="text-xs text-slate-400 mt-0.5">'
      + App.Utils.formatDate(fb.date) + ' &middot; ' + App.Utils.esc(teacher.fullName || teacher.name || fb.teacherId)
      + '</p>'
      + (fb.topic
          ? '<p class="text-xs font-medium text-slate-600 mt-2">Topic: ' + App.Utils.esc(fb.topic) + '</p>'
          : '')
      + (fb.notes
          ? '<p class="text-sm text-slate-600 mt-1.5 leading-relaxed">' + App.Utils.esc(fb.notes) + '</p>'
          : '')
      + stuNotes
      + '</div>'
      + '<div class="flex gap-2 shrink-0">' + editBtn + delBtn + '</div>'
      + '</div>'
      + '</div>';
  }

  // ─── LOG MODAL ──────────────────────────────────────────────────────────────

  function _logModal() {
    var state = App.Store.get();
    var classes = state.classes || [];
    var staff = state.staff || [];
    var isAdmin = App.currentRole === 'admin';

    var availClasses = isAdmin ? classes : classes.filter(function(c) {
      return c.teacherIds && c.teacherIds.indexOf(App.currentTeacher) > -1;
    });

    if (availClasses.length === 0) {
      App.Utils.showToast('No classes ' + (isAdmin ? 'available' : 'assigned to you') + ' yet', 'info');
      return;
    }

    var classOpts = availClasses.map(function(c) {
      return '<option value="' + c.id + '">' + App.Utils.esc(c.name) + ' (' + App.Utils.esc(c.day || '') + ')</option>';
    }).join('');

    // Admin needs to pick a teacher; teacher auto-set
    var teacherField = '';
    if (isAdmin) {
      var teachers = staff.filter(function(s) { return s.role === 'Teacher' || s.role === 'teacher'; });
      teacherField = '<div><label class="block text-sm font-medium text-slate-700 mb-1">Teacher</label>'
        + '<select name="teacherId" id="fb-teacher-sel" class="form-input" required>'
        + '<option value="">Select teacher...</option>'
        + teachers.map(function(t) { return '<option value="' + t.id + '">' + App.Utils.esc(t.fullName || t.name) + '</option>'; }).join('')
        + '</select></div>';
    }

    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-4">Log Classroom Feedback</h2>'
      + '<form id="fb-form" class="space-y-4">'
      + (isAdmin ? '<div class="grid grid-cols-3 gap-4">' : '<div class="grid grid-cols-2 gap-4">')
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Class</label>'
      + '<select name="classId" id="fb-class-sel" class="form-input" onchange="App.Feedback._updateStudentList()">' + classOpts + '</select></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Date</label>'
      + '<input name="date" type="date" value="' + App.Utils.today() + '" class="form-input"></div>'
      + teacherField
      + '</div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Topic Covered</label>'
      + '<input name="topic" class="form-input" placeholder="e.g. Multiplication Tables" maxlength="150"></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Class Mood</label>'
      + '<div class="flex gap-2">'
      + ['Great','Good','Needs Work'].map(function(m) {
          var cls = { Great:'bg-green-500', Good:'bg-amber-500', 'Needs Work':'bg-red-500' };
          return '<label class="flex-1 cursor-pointer">'
            + '<input type="radio" name="mood" value="' + m + '" class="sr-only" ' + (m === 'Good' ? 'checked' : '') + '>'
            + '<div class="text-center py-2 px-3 rounded-lg border-2 border-transparent text-sm font-semibold text-white ' + cls[m] + ' hover:opacity-90 transition">' + m + '</div>'
            + '</label>';
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

    App.Feedback._updateStudentList();
    App.Feedback._initMoodRadios();

    document.getElementById('fb-form').addEventListener('submit', function(e) {
      e.preventDefault();
      var fd = new FormData(e.target);
      var classId = fd.get('classId');
      var enrolledStudents = (App.Store.get().students || []).filter(function(s) {
        return (s.enrolledClasses || []).indexOf(classId) > -1;
      });
      var stuNotes = enrolledStudents.map(function(s) {
        var noteEl = document.getElementById('fb-note-' + s.id);
        return { studentId: s.id, note: noteEl ? noteEl.value.trim() : '' };
      }).filter(function(sn) { return sn.note; });

      var teacherId = App.currentRole === 'admin' ? fd.get('teacherId') : App.currentTeacher;
      if (!teacherId) { App.Utils.showToast('Select a teacher', 'warning'); return; }

      var newFb = {
        id: App.Utils.generateId('FB'),
        classId: classId,
        date: fd.get('date') || App.Utils.today(),
        teacherId: teacherId,
        topic: fd.get('topic') ? fd.get('topic').trim() : '',
        mood: fd.get('mood') || 'Good',
        notes: fd.get('notes') ? fd.get('notes').trim() : '',
        studentNotes: stuNotes
      };
      App.Api.post('/api/feedback', newFb).then(function(result) {
        var existing = App.Store.get().feedback || [];
        App.Store.set({ feedback: [newFb].concat(existing) });
        App.Utils.hideModal(true);
        App.Utils.showToast('Feedback logged!', 'success');
        App.Router.refresh();
      }).catch(function(err) {
        var existing = App.Store.get().feedback || [];
        App.Store.set({ feedback: [newFb].concat(existing) });
        App.Utils.hideModal(true);
        App.Utils.showToast('Saved locally (offline)', 'warning');
        App.Router.refresh();
      });
    });
  }

  // ─── UPDATE STUDENT LIST (modal helper) ─────────────────────────────────────

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

  // ─── EDIT MODAL ─────────────────────────────────────────────────────────────

  function _editModal(fbId) {
    var state    = App.Store.get();
    var feedbacks= state.feedback || [];
    var fb       = feedbacks.find(function(f) { return f.id === fbId; });
    if (!fb) return;

    var classes  = state.classes  || [];
    var cls      = classes.find(function(c) { return c.id === fb.classId; }) || {};
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
          var cls = { Great:'bg-green-500', Good:'bg-amber-500', 'Needs Work':'bg-red-500' };
          return '<label class="flex-1 cursor-pointer">'
            + '<input type="radio" name="mood" value="' + m + '" class="sr-only" ' + (fb.mood === m ? 'checked' : '') + '>'
            + '<div class="text-center py-2 px-3 rounded-lg border-2 border-transparent text-sm font-semibold text-white ' + cls[m] + ' hover:opacity-90 transition">' + m + '</div>'
            + '</label>';
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

    App.Feedback._initMoodRadios();

    document.getElementById('fb-edit-form').addEventListener('submit', function(e) {
      e.preventDefault();
      var fd = new FormData(e.target);
      var stuNotes = enrolled.map(function(s) {
        var noteEl = document.getElementById('fbedit-note-' + s.id);
        return { studentId: s.id, note: noteEl ? noteEl.value.trim() : '' };
      });
      var updatedData = {
        topic: fd.get('topic') ? fd.get('topic').trim() : fb.topic,
        mood:  fd.get('mood')  || fb.mood,
        notes: fd.get('notes') ? fd.get('notes').trim() : fb.notes,
        studentNotes: stuNotes
      };
      var updated = feedbacks.map(function(f) {
        if (f.id !== fbId) return f;
        return Object.assign({}, f, updatedData);
      });
      App.Api.put('/api/feedback/' + fbId, updatedData).then(function(result) {
        App.Store.set({ feedback: updated });
        App.Utils.hideModal(true);
        App.Utils.showToast('Feedback updated', 'success');
        App.Router.refresh();
      }).catch(function(err) {
        App.Store.set({ feedback: updated });
        App.Utils.hideModal(true);
        App.Utils.showToast('Saved locally (offline)', 'warning');
        App.Router.refresh();
      });
    });
  }

  // ─── DELETE ─────────────────────────────────────────────────────────────────

  function _delete(fbId) {
    if (!confirm('Delete this feedback entry?')) return;
    var existing = App.Store.get().feedback || [];
    App.Api.del('/api/feedback/' + fbId).then(function() {
      App.Store.set({ feedback: existing.filter(function(f) { return f.id !== fbId; }) });
      App.Utils.showToast('Feedback deleted', 'info');
      App.Router.refresh();
    }).catch(function(err) {
      App.Store.set({ feedback: existing.filter(function(f) { return f.id !== fbId; }) });
      App.Utils.showToast('Deleted locally (offline)', 'warning');
      App.Router.refresh();
    });
  }

  // ─── FILTER SETTERS ─────────────────────────────────────────────────────────

  function _setDate(v)  { _filterDate  = v; _feedbackPage = 0; App.Router.refresh(); }
  function _setClass(v) { _filterClass = v; _feedbackPage = 0; App.Router.refresh(); }
  function _setChild(v) { _filterChild = v; _feedbackParentPage = 0; App.Router.refresh(); }
  function _clearFilters() {
    _filterDate  = '';
    _filterClass = '';
    _feedbackPage = 0;
    App.Router.refresh();
  }
  function _setStaffPage(n) { _feedbackPage = Math.max(0, n); App.Router.refresh(); }
  function _setParentPage(n) { _feedbackParentPage = Math.max(0, n); App.Router.refresh(); }

  // ─── EXPORT ─────────────────────────────────────────────────────────────────

  function _initMoodRadios() {
    var radios = document.querySelectorAll('input[name="mood"]');
    if (!radios.length) return;
    function update() {
      radios.forEach(function(r) {
        var div = r.nextElementSibling;
        if (!div) return;
        if (r.checked) {
          div.style.borderColor = '#fff';
          div.style.outline = '3px solid #fff';
          div.style.outlineOffset = '2px';
          div.style.opacity = '1';
          div.style.boxShadow = '0 0 0 3px rgba(255,255,255,0.5)';
        } else {
          div.style.borderColor = 'transparent';
          div.style.outline = 'none';
          div.style.outlineOffset = '0';
          div.style.opacity = '0.7';
          div.style.boxShadow = 'none';
        }
      });
    }
    radios.forEach(function(r) { r.addEventListener('change', update); });
    update();
  }

  App.Feedback = {
    render:             render,
    _logModal:          _logModal,
    _editModal:         _editModal,
    _delete:            _delete,
    _updateStudentList: _updateStudentList,
    _initMoodRadios:    _initMoodRadios,
    _setDate:           _setDate,
    _setClass:          _setClass,
    _setChild:          _setChild,
    _clearFilters:      _clearFilters,
    _setStaffPage:      _setStaffPage,
    _setParentPage:     _setParentPage
  };
})();
