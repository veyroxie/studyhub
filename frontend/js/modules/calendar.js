(function() {
  window.App = window.App || {};

  const DAYS = ['Monday','Tuesday','Wednesday','Thursday','Friday','Saturday','Sunday'];

  function getWeekStart(date) {
    const d = new Date(date);
    const day = d.getDay();
    const diff = (day === 0 ? -6 : 1 - day);
    d.setDate(d.getDate() + diff);
    return d;
  }

  function addDays(date, n) {
    const d = new Date(date);
    d.setDate(d.getDate() + n);
    return d;
  }

  function fmtShortDate(date) {
    return date.toLocaleDateString('en-MY', { day:'numeric', month:'short' });
  }

  let _weekStart = getWeekStart(new Date());
  let _view = 'week'; // 'week' or 'month'
  let _filterTeacher = ''; // '' = all
  let _filterSearch  = ''; // text search on class name

  function render(container) {
    const { classes, staff, students, cancelledClasses } = App.Store.get();
    const _cancelledClasses = cancelledClasses || [];
    const isAdmin = App.currentRole === 'admin';
    const isClient = App.currentRole === 'client';
    const isTeacher = App.currentRole === 'teacher';
    const weekDates = DAYS.map(function(_, i) { return addDays(_weekStart, i); });

    // Parent filter: only show classes the parent's children are enrolled in
    let enrolledClassIds = null;
    if (isClient && App.clientParent) {
      const myKids = students.filter(function(s) { return s.contact === App.clientParent; });
      enrolledClassIds = {};
      myKids.forEach(function(s) {
        s.enrolledClasses.forEach(function(cid) { enrolledClassIds[cid] = true; });
      });
    }

    // Teacher filter: only show their own classes
    let teacherClassIds = null;
    if (isTeacher && App.currentTeacher) {
      const myClasses = classes.filter(function(c) { return c.teacherIds.indexOf(App.currentTeacher) > -1; });
      teacherClassIds = {};
      myClasses.forEach(function(c) { teacherClassIds[c.id] = true; });
    }

    const viewClasses = teacherClassIds !== null ? classes.filter(function(c) { return teacherClassIds[c.id]; }) : classes;

    const classesByDay = {};
    DAYS.forEach(function(day) {
      classesByDay[day] = classes
        .filter(function(c) {
          if (c.day !== day) return false;
          if (enrolledClassIds !== null && !enrolledClassIds[c.id]) return false;
          if (isClient && c.enrolled >= c.capacity) return false; // hide full classes from parents
          if (teacherClassIds !== null && !teacherClassIds[c.id]) return false;
          if (_filterTeacher && !c.teacherIds.includes(_filterTeacher)) return false;
          if (_filterSearch && !c.name.toLowerCase().includes(_filterSearch.toLowerCase())) return false;
          return true;
        })
        .sort(function(a, b) { return a.time.localeCompare(b.time); });
    });

    const totalClasses = viewClasses.length;
    const avgFill = viewClasses.reduce(function(s, c) { return s + (c.enrolled / c.capacity); }, 0) / (viewClasses.length || 1);
    const fullClasses = viewClasses.filter(function(c) { return c.enrolled >= c.capacity; }).length;

    const viewToggle = '<div style="display:flex;gap:0.25rem;background:#f1f5f9;border-radius:8px;padding:3px">'
      + '<button onclick="App.Calendar._setView(\'week\')" style="padding:0.3rem 0.85rem;font-size:0.72rem;font-weight:600;border:none;border-radius:6px;cursor:pointer;background:' + (_view==='week'?'var(--gold, #f59e0b)':'transparent') + ';color:' + (_view==='week'?'#0a0a0a':'#94a3b8') + '">Week</button>'
      + '<button onclick="App.Calendar._setView(\'month\')" style="padding:0.3rem 0.85rem;font-size:0.72rem;font-weight:600;border:none;border-radius:6px;cursor:pointer;background:' + (_view==='month'?'var(--gold, #f59e0b)':'transparent') + ';color:' + (_view==='month'?'#0a0a0a':'#94a3b8') + '">Month</button>'
      + '<button onclick="App.Calendar._setView(\'timetable\')" style="padding:0.3rem 0.85rem;font-size:0.72rem;font-weight:600;border:none;border-radius:6px;cursor:pointer;background:' + (_view==='timetable'?'var(--gold, #f59e0b)':'transparent') + ';color:' + (_view==='timetable'?'#0a0a0a':'#94a3b8') + '">Timetable</button>'
      + (isAdmin ? '<button onclick="App.Calendar._setView(\'programs\')" style="padding:0.3rem 0.85rem;font-size:0.72rem;font-weight:600;border:none;border-radius:6px;cursor:pointer;background:' + (_view==='programs'?'var(--gold, #f59e0b)':'transparent') + ';color:' + (_view==='programs'?'#0a0a0a':'#94a3b8') + '">Programs</button>' : '')
      + '</div>';

    const headerHtml = ''
      + '<div class="flex items-center justify-between mb-6">'
      +   '<div>'
      +     '<h1 class="text-2xl font-bold text-slate-800">Class Schedule</h1>'
      +     '<p class="text-sm text-slate-500 mt-0.5">Week of ' + fmtShortDate(_weekStart) + ' – ' + fmtShortDate(weekDates[6]) + '</p>'
      +   '</div>'
      +   '<div class="flex items-center gap-3">'
      +     (_view === 'week'
            ? '<button onclick="App.Calendar._prevWeek()" class="px-3 py-1.5 text-sm border border-slate-200 rounded-lg hover:bg-slate-50 text-slate-600">&#8592; Prev</button>'
            + '<button onclick="App.Calendar._nextWeek()" class="px-3 py-1.5 text-sm border border-slate-200 rounded-lg hover:bg-slate-50 text-slate-600">Next &#8594;</button>'
            : '')
      +     viewToggle
      +     (isAdmin ? '<button onclick="App.Calendar._addClassModal()" class="px-4 py-1.5 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">+ Add Class</button>' : '')
      +   '</div>'
      + '</div>'

      + (isClient && enrolledClassIds !== null
        ? '<div class="mb-4 px-4 py-2 bg-blue-50 border border-blue-100 rounded-xl text-sm text-blue-700">Showing classes for your child only</div>'
        : '')

      + (isTeacher && teacherClassIds !== null
        ? '<div class="mb-4 px-4 py-2 bg-purple-50 border border-purple-100 rounded-xl text-sm text-purple-700">Showing your assigned classes only</div>'
        : '')

      + '<div class="grid grid-cols-4 gap-4 mb-6">'
      + _statCard('Total Classes', totalClasses, 'text-blue-600')
      + _statCard('Full Classes', fullClasses, 'text-red-500')
      + _statCard('Avg Fill Rate', Math.round(avgFill * 100) + '%', 'text-emerald-600')
      + _statCard('Active Staff', staff.length, 'text-purple-600')
      + '</div>';

    const filterBar = '<div style="display:flex;align-items:center;gap:0.75rem;margin-bottom:1.25rem;flex-wrap:wrap">'
      + '<input type="search" placeholder="Search class..." value="' + App.Utils.esc(_filterSearch) + '" oninput="App.Calendar._setSearch(this.value)" style="padding:0.45rem 0.75rem;font-size:0.82rem;border:1px solid #e2e8f0;border-radius:8px;outline:none;width:180px;background:#fff">'
      + (!isTeacher
        ? '<select onchange="App.Calendar._setTeacher(this.value)" style="padding:0.45rem 0.75rem;font-size:0.82rem;border:1px solid #e2e8f0;border-radius:8px;background:#fff;color:#374151;cursor:pointer">'
          + '<option value="">All Tutors</option>'
          + staff.map(function(s) { return '<option value="' + s.id + '" ' + (_filterTeacher === s.id ? 'selected' : '') + '>' + App.Utils.esc(s.name) + '</option>'; }).join('')
          + '</select>'
        : '')
      + (_filterTeacher || _filterSearch ? '<button onclick="App.Calendar._clearFilters()" style="padding:0.45rem 0.85rem;font-size:0.8rem;border:none;border-radius:8px;background:#f1f5f9;color:#64748b;cursor:pointer">Clear</button>' : '')
      + '</div>';

    const hasActiveFilter = !!(_filterTeacher || _filterSearch);

    if (_view === 'month') {
      const displayClasses = teacherClassIds !== null
        ? classes.filter(function(c) { return teacherClassIds[c.id]; })
        : classes;
      // Count classes that would actually be visible after all filters
      const monthFilteredCount = displayClasses.filter(function(c) {
        if (enrolledClassIds !== null && !enrolledClassIds[c.id]) return false;
        if (_filterTeacher && !c.teacherIds.includes(_filterTeacher)) return false;
        if (_filterSearch && !c.name.toLowerCase().includes(_filterSearch.toLowerCase())) return false;
        return true;
      }).length;
      const monthFilterEmptyBanner = (hasActiveFilter && monthFilteredCount === 0)
        ? '<div class="bg-white rounded-xl border border-slate-100 shadow-sm mb-4">'
          + App.Utils.emptyState(
              'No classes match your filters',
              'Try clearing the tutor or search filter to see all classes.',
              '<button onclick="App.Calendar._clearFilters()" style="padding:0.5rem 1.25rem;font-size:0.83rem;font-weight:600;background:#f1f5f9;color:#475569;border:none;border-radius:8px;cursor:pointer">Clear Filters</button>'
            )
          + '</div>'
        : '';
      container.innerHTML = headerHtml + filterBar + monthFilterEmptyBanner + _renderMonthView(displayClasses, staff, enrolledClassIds, isAdmin);
      return;
    }

    if (_view === 'timetable') {
      const displayClasses = teacherClassIds !== null
        ? classes.filter(function(c) { return teacherClassIds[c.id]; })
        : enrolledClassIds !== null
        ? classes.filter(function(c) { return enrolledClassIds[c.id]; })
        : classes;
      container.innerHTML = headerHtml + _renderTimetableView(displayClasses, staff);
      return;
    }

    if (_view === 'programs') {
      container.innerHTML = headerHtml + _renderProgramsView(classes, students, staff);
      return;
    }

    // Check if all days are empty due to an active filter (not just a naturally quiet week)
    const totalFilteredClasses = DAYS.reduce(function(sum, day) { return sum + classesByDay[day].length; }, 0);
    const weekFilterEmptyBanner = (hasActiveFilter && totalFilteredClasses === 0)
      ? '<div class="bg-white rounded-xl border border-slate-100 shadow-sm mb-4">'
        + App.Utils.emptyState(
            'No classes match your filters',
            'Try clearing the tutor or search filter to see all classes.',
            '<button onclick="App.Calendar._clearFilters()" style="padding:0.5rem 1.25rem;font-size:0.83rem;font-weight:600;background:#f1f5f9;color:#475569;border:none;border-radius:8px;cursor:pointer">Clear Filters</button>'
          )
        + '</div>'
      : '';

    container.innerHTML = headerHtml + filterBar + weekFilterEmptyBanner

      // Calendar grid with vertical dividers between columns
      + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm overflow-hidden">'
      +   '<div class="grid grid-cols-7 divide-x divide-slate-100">'
      +   DAYS.map(function(day, i) {
            const dayClasses = classesByDay[day];
            const dateLabel = fmtShortDate(weekDates[i]);
            const isToday = weekDates[i].toDateString() === new Date().toDateString();
            return '<div class="min-w-0">'
              + '<div class="p-3 border-b border-slate-100 bg-slate-50 text-center">'
              +   '<div class="text-xs font-semibold ' + (isToday ? 'text-blue-600' : 'text-slate-500') + ' uppercase tracking-wide">' + day.slice(0,3) + '</div>'
              +   '<div class="mt-1 ' + (isToday ? 'w-7 h-7 bg-blue-600 text-white rounded-full flex items-center justify-center mx-auto text-sm font-bold' : 'text-slate-700 text-sm font-bold text-center') + '">' + weekDates[i].getDate() + '</div>'
              +   '<div class="text-xs text-slate-400 mt-0.5">' + weekDates[i].toLocaleDateString('en-MY', {month:'short'}) + '</div>'
              + '</div>'
              + '<div class="p-2 space-y-2 min-h-24">'
              + (dayClasses.length === 0
                ? '<div class="border border-dashed border-slate-200 rounded-lg p-2 text-center mt-1"><p class="text-xs text-slate-300">—</p></div>'
                : dayClasses.map(function(c) { return _classCard(c, staff, _cancelledClasses, weekDates, i); }).join(''))
              + '</div>'
              + '</div>';
          }).join('')
      +   '</div>'
      + '</div>';
  }

  function _renderMonthView(classes, staff, enrolledClassIds, isAdmin) {
    var today = new Date();
    var year  = _weekStart.getFullYear();
    var month = _weekStart.getMonth();
    // First day of month
    var firstDay = new Date(year, month, 1);
    // Start grid from the Monday on or before firstDay
    var startDow = firstDay.getDay(); // 0=Sun
    var gridStart = new Date(firstDay);
    gridStart.setDate(firstDay.getDate() - (startDow === 0 ? 6 : startDow - 1));

    var DAYS_SHORT = ['Mon','Tue','Wed','Thu','Fri','Sat','Sun'];
    var DAY_NAMES  = ['Monday','Tuesday','Wednesday','Thursday','Friday','Saturday','Sunday'];

    var cells = '';
    var d = new Date(gridStart);
    var weeksShown = 0;
    while (weeksShown < 6) {
      var weekCells = '';
      for (var di = 0; di < 7; di++) {
        var dayName  = DAY_NAMES[di];
        var isToday  = d.toDateString() === today.toDateString();
        var inMonth  = d.getMonth() === month;
        var dayCls   = classes.filter(function(c) {
          if (c.day !== dayName) return false;
          if (enrolledClassIds !== null && !enrolledClassIds[c.id]) return false;
          if (_filterTeacher && !c.teacherIds.includes(_filterTeacher)) return false;
          if (_filterSearch && !c.name.toLowerCase().includes(_filterSearch.toLowerCase())) return false;
          return true;
        });
        weekCells += '<td style="border:1px solid #f0ede8;vertical-align:top;width:' + (100/7) + '%;min-height:90px;padding:0.35rem;background:' + (!inMonth ? '#fafaf8' : '#fff') + '">'
          + '<div style="font-size:0.72rem;font-weight:' + (isToday ? '800' : '500') + ';color:' + (isToday ? 'var(--gold, #f59e0b)' : inMonth ? '#374151' : '#cbd5e1') + ';margin-bottom:3px;width:1.4rem;height:1.4rem;display:flex;align-items:center;justify-content:center;border-radius:50%;background:' + (isToday ? 'var(--gold-dim, #fef3c7)' : 'transparent') + '">' + d.getDate() + '</div>'
          + dayCls.slice(0, 3).map(function(c) {
              var colors = App.Utils.colorClasses(c.color);
              return '<div style="font-size:0.65rem;font-weight:600;border-radius:4px;padding:1px 5px;margin-bottom:2px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis" class="' + colors.pill + '">' + App.Utils.formatTime(c.time) + ' ' + c.name + '</div>';
            }).join('')
          + (dayCls.length > 3 ? '<div style="font-size:0.63rem;color:#94a3b8">+' + (dayCls.length - 3) + ' more</div>' : '')
          + '</td>';
        d.setDate(d.getDate() + 1);
      }
      // Stop if we've passed the month and done at least 4 weeks
      if (weeksShown >= 4 && d.getMonth() !== month) break;
      cells += '<tr>' + weekCells + '</tr>';
      weeksShown++;
    }

    var monthName = new Date(year, month, 1).toLocaleDateString('en-MY', { month:'long', year:'numeric' });

    return '<div>'
      + '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.75rem">'
      +   '<div style="display:flex;align-items:center;gap:0.5rem">'
      +     '<button onclick="App.Calendar._prevMonth()" style="width:2rem;height:2rem;border:1px solid #e2e8f0;border-radius:8px;background:#fff;cursor:pointer;font-size:1rem;display:flex;align-items:center;justify-content:center">‹</button>'
      +     '<span style="font-size:0.95rem;font-weight:700;color:#111;min-width:160px;text-align:center">' + monthName + '</span>'
      +     '<button onclick="App.Calendar._nextMonth()" style="width:2rem;height:2rem;border:1px solid #e2e8f0;border-radius:8px;background:#fff;cursor:pointer;font-size:1rem;display:flex;align-items:center;justify-content:center">›</button>'
      +   '</div>'
      + '</div>'
      + '<table style="width:100%;border-collapse:collapse;table-layout:fixed">'
      +   '<thead><tr>' + DAYS_SHORT.map(function(d) { return '<th style="font-size:0.7rem;font-weight:600;color:#94a3b8;padding:0.4rem;text-align:center;border-bottom:1px solid #f0ede8">' + d + '</th>'; }).join('') + '</tr></thead>'
      +   '<tbody>' + cells + '</tbody>'
      + '</table>'
      + '</div>';
  }

  function _statCard(label, value, colorClass) {
    return '<div class="bg-white rounded-xl border border-slate-100 shadow-sm p-4">'
      + '<div class="text-2xl font-bold ' + colorClass + '">' + value + '</div>'
      + '<div class="text-xs text-slate-500 mt-1">' + label + '</div>'
      + '</div>';
  }

  function _classCard(c, staff, cancelledClasses, weekDates, dayIndex) {
    const U = App.Utils;
    const colors = U.colorClasses(c.color);
    const fillPct = Math.round((c.enrolled / c.capacity) * 100);
    const fillBarColor = fillPct >= 100 ? 'bg-red-500' : fillPct >= 70 ? 'bg-amber-400' : 'bg-emerald-500';
    const teachers = c.teacherIds.map(function(tid) {
      const s = staff.find(function(s) { return s.id === tid; });
      return s ? s.name : tid;
    }).join(', ');

    // Check if this class is cancelled on this specific date
    let dateStr = null;
    if (weekDates && dayIndex !== undefined && weekDates[dayIndex]) {
      const d = weekDates[dayIndex];
      dateStr = d.getFullYear() + '-' + String(d.getMonth()+1).padStart(2,'0') + '-' + String(d.getDate()).padStart(2,'0');
    }
    const isCancelled = dateStr && cancelledClasses && cancelledClasses.some(function(cc) { return cc.classId === c.id && cc.date === dateStr; });

    if (isCancelled) {
      return '<div class="bg-red-50 border-l-4 border-red-300 rounded-lg p-2 opacity-60 relative">'
        + '<div style="position:absolute;top:3px;right:4px;font-size:0.6rem;font-weight:700;color:#ef4444;background:#fee2e2;padding:1px 5px;border-radius:4px">Cancelled</div>'
        + '<div class="font-semibold text-xs text-red-300 leading-tight truncate line-through">' + c.name + '</div>'
        + '<div class="text-xs text-red-200 mt-0.5">' + U.formatTime(c.time) + ' – ' + U.formatTime(c.endTime) + '</div>'
        + '<div class="text-xs text-red-200 mt-0.5 truncate">' + teachers + '</div>'
        + '</div>';
    }

    return '<div class="' + colors.bg + ' border-l-4 ' + colors.border + ' rounded-lg p-2 cursor-pointer hover:shadow-sm transition-shadow" onclick="App.Calendar._classModal(\'' + c.id + '\')">'
      + '<div class="font-semibold text-xs ' + colors.text + ' leading-tight truncate">' + c.name + '</div>'
      + '<div class="text-xs ' + colors.text + ' opacity-70 mt-0.5">' + U.formatTime(c.time) + ' – ' + U.formatTime(c.endTime) + '</div>'
      + '<div class="text-xs text-slate-500 mt-0.5 truncate">' + teachers + '</div>'
      + '<div class="mt-1.5 flex items-center gap-1">'
      +   '<div class="flex-1 h-1 bg-white/60 rounded-full overflow-hidden">'
      +     '<div class="h-full ' + fillBarColor + ' rounded-full" style="width:' + Math.min(fillPct,100) + '%"></div>'
      +   '</div>'
      +   '<span class="text-xs ' + colors.text + ' font-medium whitespace-nowrap">' + c.enrolled + '/' + c.capacity + '</span>'
      + '</div>'
      + '</div>';
  }

  function _classModal(classId) {
    var state = App.Store.get();
    var c = state.classes.find(function(x) { return x.id === classId; });
    if (!c) return;
    var isAdmin   = App.currentRole === 'admin';
    var isClient  = App.currentRole === 'client';
    var isTeacher = App.currentRole === 'teacher';
    var teachers  = c.teacherIds.map(function(tid) {
      var s = state.staff.find(function(x) { return x.id === tid; });
      return s ? s.fullName : tid;
    }).join(', ');
    var enrolled  = state.students.filter(function(s) { return s.enrolledClasses.indexOf(classId) > -1; });
    var feedbackList = (state.feedback || []).filter(function(f) { return f.classId === classId; });
    var avgRating = feedbackList.length > 0
      ? (feedbackList.reduce(function(a,f){ return a+(f.rating||0); },0)/feedbackList.length).toFixed(1)
      : null;
    var colors = App.Utils.colorClasses(c.color);

    var canLeaveFeedback = isClient; // parents can rate
    var parentStudentIds = isClient && App.clientParent
      ? state.students.filter(function(s){ return s.contact===App.clientParent; }).map(function(s){ return s.id; })
      : [];
    var alreadyReviewed = isClient && feedbackList.some(function(f) {
      return parentStudentIds.indexOf(f.studentId) > -1;
    });

    App.Utils.showModal(
      '<div class="p-6">'
      + '<div class="flex items-center gap-3 mb-5">'
      +   '<div class="w-3 h-12 rounded-full ' + colors.bg + ' ' + colors.border + ' border-2"></div>'
      +   '<div>'
      +     '<h2 class="text-xl font-bold">' + App.Utils.esc(c.name) + '</h2>'
      +     '<div class="text-sm text-slate-500">' + c.day + ' · ' + App.Utils.formatTime(c.time) + '–' + App.Utils.formatTime(c.endTime) + ' · ' + c.classroom + '</div>'
      +   '</div>'
      + '</div>'
      + '<div class="grid grid-cols-2 gap-3 mb-5 text-sm">'
      +   '<div class="bg-slate-50 rounded-lg p-3"><div class="text-xs text-slate-400 mb-1">Teacher(s)</div><div class="font-medium">' + App.Utils.esc(teachers) + '</div></div>'
      +   '<div class="bg-slate-50 rounded-lg p-3"><div class="text-xs text-slate-400 mb-1">Enrolled</div><div class="font-medium">' + c.enrolled + '/' + c.capacity + '</div></div>'
      +   '<div class="bg-slate-50 rounded-lg p-3"><div class="text-xs text-slate-400 mb-1">Category</div><div class="font-medium">' + (c.category || 'Academic') + '</div></div>'
      +   '<div class="bg-slate-50 rounded-lg p-3"><div class="text-xs text-slate-400 mb-1">Type</div><div class="font-medium">' + (c.classType || 'Group') + '</div></div>'
      + '</div>'
      // Feedback section
      + '<div class="border-t border-slate-100 pt-4">'
      +   '<div class="flex items-center justify-between mb-3">'
      +     '<h3 class="text-sm font-bold text-slate-700">Class Feedback</h3>'
      +     (avgRating ? '<span class="text-sm font-bold text-amber-600">' + avgRating + '/5 (' + feedbackList.length + ' reviews)</span>' : '<span class="text-xs text-slate-400">No reviews yet</span>')
      +   '</div>'
      +   (feedbackList.length > 0
            ? '<div class="space-y-2 max-h-36 overflow-y-auto mb-4">'
            + feedbackList.map(function(f) {
                var stars = '★'.repeat(f.rating||0) + '☆'.repeat(5-(f.rating||0));
                return '<div class="bg-slate-50 rounded-lg p-2.5">'
                  + '<div class="flex items-center gap-2">'
                  +   '<span class="text-amber-400 text-sm">' + stars + '</span>'
                  +   '<span class="text-xs text-slate-400">' + App.Utils.formatDate(f.createdOn) + '</span>'
                  + '</div>'
                  + (f.comment ? '<div class="text-xs text-slate-600 mt-1">' + App.Utils.esc(f.comment) + '</div>' : '')
                  + '</div>';
              }).join('')
            + '</div>'
            : '<div class="text-xs text-slate-400 text-center py-4 mb-3">Be the first to leave a review!</div>')
      +   (canLeaveFeedback && !alreadyReviewed
            ? '<div id="feedback-form-' + classId + '">'
            +   '<div class="text-xs font-semibold text-slate-600 mb-2">Your Rating</div>'
            +   '<div style="display:flex;gap:0.5rem;margin-bottom:0.75rem" id="star-row-' + classId + '">'
            +   [1,2,3,4,5].map(function(n) {
                  return '<button onclick="App.Calendar._setStar(\'' + classId + '\',' + n + ')" data-star="' + n + '" style="font-size:1.5rem;background:none;border:none;cursor:pointer;color:#d1d5db;transition:color 0.1s">★</button>';
                }).join('')
            +   '</div>'
            +   '<textarea id="feedback-comment-' + classId + '" rows="2" placeholder="Optional comment..." style="width:100%;padding:0.5rem;font-size:0.82rem;border:1px solid #e2e8f0;border-radius:8px;resize:none;outline:none;margin-bottom:0.5rem"></textarea>'
            +   '<button onclick="App.Calendar._submitFeedback(\'' + classId + '\')" style="padding:0.4rem 1rem;font-size:0.82rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Submit Review</button>'
            + '</div>'
            : (alreadyReviewed ? '<div class="text-xs text-emerald-600 font-semibold">You have already reviewed this class.</div>' : ''))
      + '</div>'
      + '</div>'
    );
  }

  var _starRating = {}; // classId -> chosen rating

  function _setStar(classId, n) {
    _starRating[classId] = n;
    var row = document.getElementById('star-row-' + classId);
    if (!row) return;
    row.querySelectorAll('[data-star]').forEach(function(btn) {
      btn.style.color = parseInt(btn.dataset.star) <= n ? '#f59e0b' : '#d1d5db';
    });
  }

  function _submitFeedback(classId) {
    var rating = _starRating[classId];
    if (!rating) { App.Utils.showToast('Please select a star rating', 'warning'); return; }
    var comment = (document.getElementById('feedback-comment-' + classId)||{}).value || '';
    var state = App.Store.get();
    var parentStudentIds = state.students.filter(function(s){ return s.contact===App.clientParent; }).map(function(s){ return s.id; });
    var studentId = parentStudentIds[0] || '';
    var newFeedback = {
      id: App.Utils.generateId('fb'),
      classId: classId,
      studentId: studentId,
      rating: rating,
      comment: comment.trim(),
      createdOn: App.Utils.today()
    };
    App.Store.set({ feedback: [...(state.feedback||[]), newFeedback] });
    App.Utils.hideModal();
    App.Utils.showToast('Thank you for your feedback!', 'success');
  }

  function _prevWeek() {
    _weekStart = addDays(_weekStart, -7);
    App.Router.refresh();
  }

  function _nextWeek() {
    _weekStart = addDays(_weekStart, 7);
    App.Router.refresh();
  }

  function _prevMonth() {
    var d = new Date(_weekStart);
    d.setMonth(d.getMonth() - 1);
    _weekStart = d;
    App.Router.refresh();
  }

  function _nextMonth() {
    var d = new Date(_weekStart);
    d.setMonth(d.getMonth() + 1);
    _weekStart = d;
    App.Router.refresh();
  }

  function _setView(v) { _view = v; App.Router.refresh(); }

  function _addClassModal() {
    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-4">Add New Class</h2>'
      + '<form id="add-class-form" class="space-y-4">'
      + _field('Class Name', '<input name="name" class="form-input" placeholder="e.g. Level 1 & 2" required>')
      // Class type toggle
      + '<div>'
      +   '<label class="block text-sm font-medium text-slate-700 mb-2">Class Type</label>'
      +   '<div style="display:grid;grid-template-columns:1fr 1fr;gap:0.5rem">'
      +   ['Private','Group'].map(function(t) {
            const cap = t === 'Private' ? 1 : 5;
            const desc = t === 'Private' ? 'Max 1 student' : 'Max 5 students';
            return '<label style="display:flex;align-items:center;gap:0.5rem;padding:0.65rem 0.85rem;border:2px solid #e2e8f0;border-radius:10px;cursor:pointer;transition:all 0.15s" class="class-type-opt">'
              + '<input type="radio" name="classType" value="' + t + '" data-cap="' + cap + '" onchange="App.Calendar._onTypeChange(this)" ' + (t === 'Group' ? 'checked' : '') + ' style="accent-color:var(--gold)">'
              + '<div><div style="font-size:0.83rem;font-weight:600">' + t + '</div><div style="font-size:0.7rem;color:#94a3b8">' + desc + '</div></div>'
              + '</label>';
          }).join('')
      +   '</div>'
      + '</div>'
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Day', '<select name="day" class="form-input">' + ['Monday','Tuesday','Wednesday','Thursday','Friday','Saturday','Sunday'].map(function(d){ return '<option>' + d + '</option>'; }).join('') + '</select>')
      + _field('Classroom', '<input name="classroom" class="form-input" placeholder="Classroom 1">')
      + '</div>'
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Start Time', '<input name="time" type="time" class="form-input" required>')
      + _field('End Time', '<input name="endTime" type="time" class="form-input" required>')
      + '</div>'
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Capacity', '<input id="cap-input" name="capacity" type="number" min="1" max="5" class="form-input" value="5" readonly style="background:#f8fafc;color:#64748b">')
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Category</label><select name="category" class="form-input"><option>Academic</option><option>Non-academic</option><option>Workshop</option></select></div>'
      + '</div>'
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" style="padding:0.5rem 1.1rem;font-size:0.85rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Add Class</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );
    document.getElementById('add-class-form').addEventListener('submit', function(e) {
      e.preventDefault();
      const fd = new FormData(e.target);
      const state = App.Store.get();
      const classType = fd.get('classType') || 'Group';
      const capacity  = classType === 'Private' ? 1 : 5;
      // Clash check: same day + overlapping time in same classroom
      const day    = fd.get('day');
      const time   = fd.get('time');
      const endTime = fd.get('endTime');
      const classroom = fd.get('classroom') || 'Classroom 1';
      const overlaps = function(c) { return time < c.endTime && endTime > c.time; };
      const roomClash = state.classes.find(function(c) {
        return c.day === day && c.classroom === classroom && overlaps(c);
      });
      if (roomClash) {
        App.Utils.showToast('Room clash: ' + classroom + ' already booked ' + App.Utils.formatTime(roomClash.time) + '–' + App.Utils.formatTime(roomClash.endTime) + ' on ' + day, 'error');
        return;
      }
      const newClass = {
        id: App.Utils.generateId('c'),
        name: fd.get('name'),
        classType: classType,
        teacherIds: [],
        classroom: classroom,
        day: day,
        time: time,
        endTime: endTime,
        capacity: capacity,
        enrolled: 0,
        color: classType === 'Private' ? 'purple' : 'blue',
        category: fd.get('category') || 'Academic'
      };
      App.Store.set({ classes: [...state.classes, newClass] });

      // Send in-app announcement to notify parents of the new class
      const newAnnouncement = {
        id: App.Utils.generateId('ann'),
        title: 'New Class Added: ' + newClass.name,
        message: 'A new ' + newClass.classType.toLowerCase() + ' class "' + newClass.name + '" has been scheduled on ' + newClass.day + 's from ' + App.Utils.formatTime(newClass.time) + ' to ' + App.Utils.formatTime(newClass.endTime) + ' in ' + newClass.classroom + '. Enrolment is now open.',
        audience: 'All Parents',
        type: 'Notice',
        createdOn: new Date().toISOString().slice(0, 10),
        createdBy: 'Admin'
      };
      const updatedAnns = [...(App.Store.get().announcements || []), newAnnouncement];
      App.Store.set({ announcements: updatedAnns });

      App.Utils.hideModal();
      App.Utils.showToast('Class added! Parents have been notified via announcement.', 'success');
      App.Router.refresh();
    });
  }

  // Called when Private/Group radio changes — update the capacity field
  function _onTypeChange(radio) {
    const capInput = document.getElementById('cap-input');
    if (capInput) capInput.value = radio.dataset.cap;
  }

  function _field(label, inputHtml) {
    return '<div><label class="block text-sm font-medium text-slate-700 mb-1">' + label + '</label>' + inputHtml + '</div>';
  }

  function _setSearch(val) { _filterSearch = val; App.Router.refresh(); }
  function _setTeacher(val) { _filterTeacher = val; App.Router.refresh(); }
  function _clearFilters() { _filterTeacher = ''; _filterSearch = ''; App.Router.refresh(); }

  function _renderTimetableView(displayClasses, staff) {
    const DAYS_TT = ['Monday','Tuesday','Wednesday','Thursday','Friday','Saturday','Sunday'];
    const DAYS_SHORT_TT = ['Mon','Tue','Wed','Thu','Fri','Sat','Sun'];
    // Time slots: 8:00 to 20:00, every hour
    const slots = [];
    for (var h = 8; h <= 20; h++) { slots.push(h < 10 ? '0' + h + ':00' : h + ':00'); }

    // Color map for classes
    const COLOR_MAP = {
      green:  { bg:'#dcfce7', border:'#86efac', text:'#15803d' },
      blue:   { bg:'#dbeafe', border:'#93c5fd', text:'#1d4ed8' },
      teal:   { bg:'#ccfbf1', border:'#5eead4', text:'#0f766e' },
      orange: { bg:'#ffedd5', border:'#fdba74', text:'#c2410c' },
      purple: { bg:'#f3e8ff', border:'#d8b4fe', text:'#7c3aed' },
      red:    { bg:'#fee2e2', border:'#fca5a5', text:'#b91c1c' }
    };

    // Build lookup: { day: { slotHour: [class, ...] } }
    var grid = {};
    DAYS_TT.forEach(function(d) { grid[d] = {}; });
    displayClasses.forEach(function(c) {
      var hour = parseInt((c.time || '08:00').split(':')[0], 10);
      if (!grid[c.day]) return;
      if (!grid[c.day][hour]) grid[c.day][hour] = [];
      grid[c.day][hour].push(c);
    });

    // Determine which rows to show (only slots that have at least one class or ±1 buffer)
    var activeHours = {};
    displayClasses.forEach(function(c) {
      var h = parseInt((c.time||'08:00').split(':')[0], 10);
      var eh = parseInt((c.endTime||c.time||'08:00').split(':')[0], 10);
      for (var x = Math.max(8, h - 1); x <= Math.min(20, eh + 1); x++) activeHours[x] = true;
    });
    var showSlots = slots.filter(function(s) {
      var h = parseInt(s.split(':')[0], 10);
      return activeHours[h] || displayClasses.length === 0;
    });
    if (showSlots.length === 0) showSlots = slots;

    var header = '<div style="display:grid;grid-template-columns:60px repeat(7,1fr);border-radius:14px 14px 0 0;overflow:hidden;background:#f8fafc;border:1px solid #e2e8f0;border-bottom:none">'
      + '<div style="padding:0.65rem 0.5rem;font-size:0.68rem;font-weight:700;color:#94a3b8;text-align:center;border-right:1px solid #e2e8f0">TIME</div>'
      + DAYS_TT.map(function(d, i) {
          return '<div style="padding:0.65rem 0.5rem;font-size:0.72rem;font-weight:700;color:#374151;text-align:center;' + (i < 6 ? 'border-right:1px solid #e2e8f0;' : '') + '">' + DAYS_SHORT_TT[i] + '</div>';
        }).join('')
      + '</div>';

    var body = '<div style="border:1px solid #e2e8f0;border-radius:0 0 14px 14px;overflow:hidden;background:#fff">';
    showSlots.forEach(function(slot, si) {
      var h = parseInt(slot.split(':')[0], 10);
      var timeLabel = (h > 12 ? h - 12 : h) + ':00' + (h >= 12 ? ' PM' : ' AM');
      body += '<div style="display:grid;grid-template-columns:60px repeat(7,1fr);border-bottom:' + (si < showSlots.length - 1 ? '1px solid #f0ede8' : 'none') + ';min-height:64px">'
        + '<div style="padding:0.5rem 0.35rem;font-size:0.68rem;font-weight:700;color:#94a3b8;text-align:right;border-right:1px solid #e2e8f0;display:flex;align-items:flex-start;justify-content:flex-end">' + timeLabel + '</div>'
        + DAYS_TT.map(function(d, di) {
            var cellClasses = (grid[d] && grid[d][h]) || [];
            var borderStyle = di < 6 ? 'border-right:1px solid #f0ede8;' : '';
            if (cellClasses.length === 0) return '<div style="padding:0.35rem;' + borderStyle + '"></div>';
            return '<div style="padding:0.3rem;' + borderStyle + 'display:flex;flex-direction:column;gap:0.2rem">'
              + cellClasses.map(function(c) {
                  var teacher = staff.find(function(x) { return c.teacherIds.indexOf(x.id) > -1; });
                  var col = COLOR_MAP[c.color] || COLOR_MAP.blue;
                  // Duration label
                  var startH = parseInt((c.time||'08:00').split(':')[0],10), startM = parseInt((c.time||'08:00').split(':')[1]||'0',10);
                  var endH   = parseInt((c.endTime||c.time||'08:00').split(':')[0],10), endM = parseInt((c.endTime||c.time||'08:00').split(':')[1]||'0',10);
                  var dur = (endH*60+endM) - (startH*60+startM);
                  var fmtT = function(hh,mm){ return (hh>12?hh-12:hh)+':'+(mm<10?'0'+mm:mm)+(hh>=12?'pm':'am'); };
                  return '<div onclick="App.Calendar._classModal(\'' + c.id + '\')" style="background:' + col.bg + ';border:1px solid ' + col.border + ';border-left:3px solid ' + col.border + ';border-radius:6px;padding:0.3rem 0.45rem;cursor:pointer" title="' + c.name + '">'
                    + '<div style="font-size:0.72rem;font-weight:700;color:' + col.text + ';white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + c.name + '</div>'
                    + '<div style="font-size:0.62rem;color:' + col.text + ';opacity:0.75">' + fmtT(startH,startM) + (dur>0?' · '+dur+'m':'') + '</div>'
                    + (teacher ? '<div style="font-size:0.62rem;color:' + col.text + ';opacity:0.65">' + teacher.name + '</div>' : '')
                    + '</div>';
                }).join('')
              + '</div>';
          }).join('')
        + '</div>';
    });
    body += '</div>';

    if (displayClasses.length === 0) {
      return '<div class="bg-white rounded-xl border border-dashed border-slate-200 p-12 text-center">'
        + '<p class="text-slate-400 text-sm">No classes to display</p>'
        + '</div>';
    }

    return '<div style="overflow-x:auto">' + header + body + '</div>';
  }

  // ── Programs View (Subjects + Workshops) ──────────────────────────────────────
  function _renderProgramsView(classes, students, staff) {
    const state = App.Store.get();
    const subjects = state.subjects || [];
    const workshops = state.workshops || [];
    const isAdmin = App.currentRole === 'admin';

    const colorDot = { green:'#10b981', teal:'#0d9488', blue:'#3b82f6', purple:'#8b5cf6', orange:'#f59e0b' };

    // Subjects section
    const subjectCards = subjects.map(function(sub) {
      const subClasses = classes.filter(function(c) { return c.subjectId === sub.id || c.name === sub.name; });
      const enrolled = subClasses.reduce(function(a, c) { return a + c.enrolled; }, 0);
      const dot = colorDot[sub.color] || '#94a3b8';
      return '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);box-shadow:0 1px 3px rgba(0,0,0,0.04);padding:1.1rem 1.2rem;display:flex;flex-direction:column;gap:0.6rem">'
        + '<div style="display:flex;align-items:flex-start;justify-content:space-between;gap:0.5rem">'
        +   '<div style="display:flex;align-items:center;gap:0.5rem">'
        +     '<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background:' + dot + ';flex-shrink:0"></span>'
        +     '<span style="font-size:0.88rem;font-weight:700;color:#111">' + App.Utils.esc(sub.name) + '</span>'
        +   '</div>'
        +   (isAdmin ? '<button onclick="App.Calendar._deleteSubject(\'' + sub.id + '\')" style="font-size:0.7rem;color:#94a3b8;background:none;border:none;cursor:pointer;padding:0;line-height:1" title="Delete">&#10005;</button>' : '')
        + '</div>'
        + '<div style="display:flex;gap:0.4rem;flex-wrap:wrap">'
        +   App.Utils.badge(sub.category, sub.category === 'Academic' ? 'blue' : 'purple')
        +   '<span style="font-size:0.68rem;font-weight:600;color:#64748b;background:#f8fafc;padding:2px 8px;border-radius:5px;border:1px solid #e2e8f0">' + App.Utils.esc(sub.level) + '</span>'
        + '</div>'
        + '<p style="font-size:0.75rem;color:#64748b;margin:0;line-height:1.4">' + App.Utils.esc(sub.description) + '</p>'
        + '<div style="display:flex;justify-content:space-between;align-items:center;margin-top:0.2rem;padding-top:0.6rem;border-top:1px solid #f4f4f2">'
        +   '<span style="font-size:0.72rem;color:#94a3b8">' + subClasses.length + ' class' + (subClasses.length !== 1 ? 'es' : '') + ' · ' + enrolled + ' enrolled</span>'
        +   '<span style="font-size:0.82rem;font-weight:700;color:var(--gold)">RM ' + sub.monthlyFee + '/mo</span>'
        + '</div>'
        + '</div>';
    }).join('');

    // Upcoming workshops
    const today = App.Utils.today();
    const sortedWS = workshops.slice().sort(function(a,b){ return a.date.localeCompare(b.date); });
    const workshopRows = sortedWS.map(function(ws) {
      const teacherNames = (ws.teacherIds || []).map(function(tid) {
        const t = staff.find(function(x){ return x.id === tid; }); return t ? t.name : tid;
      }).join(', ');
      const isPast = ws.date < today;
      const statusColor = ws.status === 'completed' ? '#94a3b8' : ws.status === 'cancelled' ? '#ef4444' : '#22c55e';
      const pct = ws.capacity > 0 ? Math.round(ws.enrolled / ws.capacity * 100) : 0;
      return '<div style="display:flex;align-items:center;gap:1rem;padding:0.85rem 1.25rem;border-bottom:1px solid #f4f4f2' + (isPast ? ';opacity:0.6' : '') + '">'
        + '<div style="min-width:56px;text-align:center">'
        +   '<div style="font-size:1rem;font-weight:800;color:#111">' + ws.date.slice(8) + '</div>'
        +   '<div style="font-size:0.65rem;font-weight:600;text-transform:uppercase;letter-spacing:0.05em;color:#94a3b8">' + new Date(ws.date + 'T00:00:00').toLocaleDateString('en-MY',{month:'short'}) + '</div>'
        + '</div>'
        + '<div style="width:1px;background:#f0ede8;align-self:stretch;flex-shrink:0"></div>'
        + '<div style="flex:1;min-width:0">'
        +   '<div style="font-size:0.85rem;font-weight:700;color:#111">' + App.Utils.esc(ws.name) + '</div>'
        +   '<div style="font-size:0.72rem;color:#94a3b8;margin-top:2px">' + App.Utils.formatTime(ws.time) + '–' + App.Utils.formatTime(ws.endTime) + ' · ' + App.Utils.esc(ws.classroom) + (teacherNames ? ' · ' + teacherNames : '') + '</div>'
        + '</div>'
        + '<div style="text-align:right;flex-shrink:0">'
        +   '<div style="font-size:0.72rem;font-weight:700;color:' + (pct >= 100 ? '#dc2626' : '#374151') + '">' + ws.enrolled + '/' + ws.capacity + '</div>'
        +   '<div style="width:48px;height:3px;background:#f1f5f9;border-radius:99px;overflow:hidden;margin-top:3px"><div style="width:' + Math.min(pct,100) + '%;height:100%;background:var(--gold);border-radius:99px"></div></div>'
        + '</div>'
        + '<span style="font-size:0.62rem;font-weight:700;text-transform:uppercase;padding:2px 7px;border-radius:5px;background:' + (ws.status==='completed'?'#f1f5f9':ws.status==='cancelled'?'#fef2f2':'#f0fdf4') + ';color:' + statusColor + ';flex-shrink:0">' + ws.status + '</span>'
        + '<span style="font-size:0.78rem;font-weight:700;color:var(--gold);flex-shrink:0">RM ' + ws.fee + '</span>'
        + (isAdmin ? '<button onclick="App.Calendar._deleteWorkshop(\'' + ws.id + '\')" style="font-size:0.7rem;color:#94a3b8;background:none;border:none;cursor:pointer;padding:0 0.2rem" title="Delete">&#10005;</button>' : '')
        + '</div>';
    }).join('');

    return '<div>'
      // Subjects
      + '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.75rem">'
      +   '<h2 style="font-size:1rem;font-weight:700;color:#111;margin:0">Subjects / Courses</h2>'
      +   (isAdmin ? '<button onclick="App.Calendar._addSubjectModal()" style="padding:0.35rem 0.85rem;font-size:0.78rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">+ Add Subject</button>' : '')
      + '</div>'
      + (subjects.length === 0
          ? '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);padding:2rem;text-align:center;color:#94a3b8;font-size:0.84rem;margin-bottom:1.5rem">No subjects yet. Add your first subject to get started.</div>'
          : '<div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(260px,1fr));gap:0.85rem;margin-bottom:2rem">' + subjectCards + '</div>')

      // Workshops
      + '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.75rem">'
      +   '<h2 style="font-size:1rem;font-weight:700;color:#111;margin:0">Workshops</h2>'
      +   (isAdmin ? '<button onclick="App.Calendar._addWorkshopModal()" style="padding:0.35rem 0.85rem;font-size:0.78rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">+ Add Workshop</button>' : '')
      + '</div>'
      + '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);box-shadow:0 1px 3px rgba(0,0,0,0.04);overflow:hidden">'
      + (workshops.length === 0
          ? '<div style="padding:2rem;text-align:center;color:#94a3b8;font-size:0.84rem">No workshops yet.</div>'
          : workshopRows)
      + '</div>'
      + '</div>';
  }

  function _addSubjectModal() {
    const colorOpts = ['green','teal','blue','purple','orange'].map(function(c) {
      return '<option value="' + c + '">' + c.charAt(0).toUpperCase() + c.slice(1) + '</option>';
    }).join('');
    App.Utils.showModal(
      '<div class="p-6" style="min-width:380px">'
      + '<h2 style="font-size:1.1rem;font-weight:700;color:#111;margin:0 0 1.25rem">Add Subject / Course</h2>'
      + '<form id="add-subject-form" class="space-y-3">'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Subject Name</label><input name="name" class="form-input" placeholder="e.g. Japanese Level 1 & 2" required></div>'
      + '<div class="grid grid-cols-2 gap-3">'
      +   '<div><label class="block text-sm font-medium text-slate-700 mb-1">Category</label><select name="category" class="form-input"><option>Academic</option><option>Non-academic</option></select></div>'
      +   '<div><label class="block text-sm font-medium text-slate-700 mb-1">Level</label><input name="level" class="form-input" placeholder="e.g. Beginner" value="All Levels"></div>'
      + '</div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Description</label><textarea name="description" class="form-input" rows="2" placeholder="Short description of this course"></textarea></div>'
      + '<div class="grid grid-cols-2 gap-3">'
      +   '<div><label class="block text-sm font-medium text-slate-700 mb-1">Monthly Fee (RM)</label><input name="monthlyFee" type="number" min="0" class="form-input" value="150" required></div>'
      +   '<div><label class="block text-sm font-medium text-slate-700 mb-1">Color</label><select name="color" class="form-input">' + colorOpts + '</select></div>'
      + '</div>'
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" style="padding:0.5rem 1.1rem;font-size:0.85rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Add Subject</button>'
      + '</div>'
      + '</form></div>'
    );
    document.getElementById('add-subject-form').addEventListener('submit', function(e) {
      e.preventDefault();
      const fd = new FormData(e.target);
      const state = App.Store.get();
      const subjects = (state.subjects || []).slice();
      subjects.push({
        id: App.Utils.generateId('sub'),
        name: fd.get('name').trim(),
        category: fd.get('category'),
        level: fd.get('level').trim() || 'All Levels',
        description: fd.get('description').trim(),
        monthlyFee: parseFloat(fd.get('monthlyFee')) || 0,
        color: fd.get('color')
      });
      App.Store.set({ subjects: subjects });
      App.Utils.hideModal();
      App.Utils.showToast('Subject added', 'success');
      App.Router.refresh();
    });
  }

  function _deleteSubject(subId) {
    if (!confirm('Delete this subject? Classes linked to it will not be removed.')) return;
    const state = App.Store.get();
    App.Store.set({ subjects: (state.subjects || []).filter(function(s) { return s.id !== subId; }) });
    App.Utils.showToast('Subject deleted', 'info');
    App.Router.refresh();
  }

  function _addWorkshopModal() {
    const { staff } = App.Store.get();
    App.Utils.showModal(
      '<div class="p-6" style="min-width:420px">'
      + '<h2 style="font-size:1.1rem;font-weight:700;color:#111;margin:0 0 1.25rem">Add Workshop</h2>'
      + '<form id="add-workshop-form" class="space-y-3">'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Workshop Name</label><input name="name" class="form-input" placeholder="e.g. Hiragana Bootcamp" required></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Description</label><textarea name="description" class="form-input" rows="2" placeholder="What will students learn?"></textarea></div>'
      + '<div class="grid grid-cols-2 gap-3">'
      +   '<div><label class="block text-sm font-medium text-slate-700 mb-1">Date</label><input name="date" type="date" class="form-input" required></div>'
      +   '<div><label class="block text-sm font-medium text-slate-700 mb-1">Classroom</label><input name="classroom" class="form-input" placeholder="Classroom 1"></div>'
      + '</div>'
      + '<div class="grid grid-cols-2 gap-3">'
      +   '<div><label class="block text-sm font-medium text-slate-700 mb-1">Start Time</label><input name="time" type="time" class="form-input" required></div>'
      +   '<div><label class="block text-sm font-medium text-slate-700 mb-1">End Time</label><input name="endTime" type="time" class="form-input" required></div>'
      + '</div>'
      + '<div class="grid grid-cols-2 gap-3">'
      +   '<div><label class="block text-sm font-medium text-slate-700 mb-1">Capacity</label><input name="capacity" type="number" min="1" class="form-input" value="15" required></div>'
      +   '<div><label class="block text-sm font-medium text-slate-700 mb-1">Fee (RM)</label><input name="fee" type="number" min="0" class="form-input" value="0" required></div>'
      + '</div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Teacher</label><select name="teacherId" class="form-input">'
      + staff.map(function(s) { return '<option value="' + s.id + '">' + App.Utils.esc(s.fullName) + '</option>'; }).join('')
      + '</select></div>'
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" style="padding:0.5rem 1.1rem;font-size:0.85rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Add Workshop</button>'
      + '</div>'
      + '</form></div>'
    );
    document.getElementById('add-workshop-form').addEventListener('submit', function(e) {
      e.preventDefault();
      const fd = new FormData(e.target);
      const state = App.Store.get();
      const workshops = (state.workshops || []).slice();
      workshops.push({
        id: App.Utils.generateId('ws'),
        name: fd.get('name').trim(),
        description: fd.get('description').trim(),
        date: fd.get('date'),
        time: fd.get('time'),
        endTime: fd.get('endTime'),
        classroom: fd.get('classroom').trim() || 'Classroom 1',
        capacity: parseInt(fd.get('capacity')) || 15,
        enrolled: 0,
        fee: parseFloat(fd.get('fee')) || 0,
        teacherIds: [fd.get('teacherId')],
        status: 'upcoming'
      });
      App.Store.set({ workshops: workshops });
      App.Utils.hideModal();
      App.Utils.showToast('Workshop added', 'success');
      App.Router.refresh();
    });
  }

  function _deleteWorkshop(wsId) {
    if (!confirm('Delete this workshop?')) return;
    const state = App.Store.get();
    App.Store.set({ workshops: (state.workshops || []).filter(function(w) { return w.id !== wsId; }) });
    App.Utils.showToast('Workshop deleted', 'info');
    App.Router.refresh();
  }

  App.Calendar = { render: render, _prevWeek: _prevWeek, _nextWeek: _nextWeek, _addClassModal: _addClassModal, _setView: _setView, _prevMonth: _prevMonth, _nextMonth: _nextMonth, _onTypeChange: _onTypeChange, _setSearch: _setSearch, _setTeacher: _setTeacher, _clearFilters: _clearFilters, _classModal: _classModal, _setStar: _setStar, _submitFeedback: _submitFeedback, _addSubjectModal: _addSubjectModal, _deleteSubject: _deleteSubject, _addWorkshopModal: _addWorkshopModal, _deleteWorkshop: _deleteWorkshop };
})();
