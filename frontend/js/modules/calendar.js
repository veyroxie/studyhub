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
    const { classes, staff, students } = App.Store.get();
    const isAdmin = App.currentRole === 'admin';
    const isClient = App.currentRole === 'client';
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

    const classesByDay = {};
    DAYS.forEach(function(day) {
      classesByDay[day] = classes
        .filter(function(c) {
          if (c.day !== day) return false;
          if (enrolledClassIds !== null && !enrolledClassIds[c.id]) return false;
          if (_filterTeacher && !c.teacherIds.includes(_filterTeacher)) return false;
          if (_filterSearch && !c.name.toLowerCase().includes(_filterSearch.toLowerCase())) return false;
          return true;
        })
        .sort(function(a, b) { return a.time.localeCompare(b.time); });
    });

    const totalClasses = classes.length;
    const avgFill = classes.reduce(function(s, c) { return s + (c.enrolled / c.capacity); }, 0) / (classes.length || 1);
    const fullClasses = classes.filter(function(c) { return c.enrolled >= c.capacity; }).length;

    const viewToggle = '<div style="display:flex;gap:0.25rem;background:#f1f5f9;border-radius:8px;padding:3px">'
      + '<button onclick="App.Calendar._setView(\'week\')" style="padding:0.3rem 0.85rem;font-size:0.72rem;font-weight:600;border:none;border-radius:6px;cursor:pointer;background:' + (_view==='week'?'var(--gold, #f59e0b)':'transparent') + ';color:' + (_view==='week'?'#0a0a0a':'#94a3b8') + '">Week</button>'
      + '<button onclick="App.Calendar._setView(\'month\')" style="padding:0.3rem 0.85rem;font-size:0.72rem;font-weight:600;border:none;border-radius:6px;cursor:pointer;background:' + (_view==='month'?'var(--gold, #f59e0b)':'transparent') + ';color:' + (_view==='month'?'#0a0a0a':'#94a3b8') + '">Month</button>'
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

      + '<div class="grid grid-cols-4 gap-4 mb-6">'
      + _statCard('Total Classes', totalClasses, 'text-blue-600')
      + _statCard('Full Classes', fullClasses, 'text-red-500')
      + _statCard('Avg Fill Rate', Math.round(avgFill * 100) + '%', 'text-emerald-600')
      + _statCard('Active Staff', staff.length, 'text-purple-600')
      + '</div>';

    const filterBar = '<div style="display:flex;align-items:center;gap:0.75rem;margin-bottom:1.25rem;flex-wrap:wrap">'
      + '<input type="search" placeholder="Search class..." value="' + App.Utils.esc(_filterSearch) + '" oninput="App.Calendar._setSearch(this.value)" style="padding:0.45rem 0.75rem;font-size:0.82rem;border:1px solid #e2e8f0;border-radius:8px;outline:none;width:180px;background:#fff">'
      + '<select onchange="App.Calendar._setTeacher(this.value)" style="padding:0.45rem 0.75rem;font-size:0.82rem;border:1px solid #e2e8f0;border-radius:8px;background:#fff;color:#374151;cursor:pointer">'
      + '<option value="">All Tutors</option>'
      + staff.map(function(s) { return '<option value="' + s.id + '" ' + (_filterTeacher === s.id ? 'selected' : '') + '>' + App.Utils.esc(s.name) + '</option>'; }).join('')
      + '</select>'
      + (_filterTeacher || _filterSearch ? '<button onclick="App.Calendar._clearFilters()" style="padding:0.45rem 0.85rem;font-size:0.8rem;border:none;border-radius:8px;background:#f1f5f9;color:#64748b;cursor:pointer">Clear</button>' : '')
      + '</div>';

    if (_view === 'month') {
      container.innerHTML = headerHtml + filterBar + _renderMonthView(classes, staff, enrolledClassIds, isAdmin);
      return;
    }

    container.innerHTML = headerHtml + filterBar

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
                : dayClasses.map(function(c) { return _classCard(c, staff); }).join(''))
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

  function _classCard(c, staff) {
    const U = App.Utils;
    const colors = U.colorClasses(c.color);
    const fillPct = Math.round((c.enrolled / c.capacity) * 100);
    const fillBarColor = fillPct >= 100 ? 'bg-red-500' : fillPct >= 70 ? 'bg-amber-400' : 'bg-emerald-500';
    const teachers = c.teacherIds.map(function(tid) {
      const s = staff.find(function(s) { return s.id === tid; });
      return s ? s.name : tid;
    }).join(', ');

    return '<div class="' + colors.bg + ' border-l-4 ' + colors.border + ' rounded-lg p-2 cursor-pointer hover:shadow-sm transition-shadow">'
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
      + _field('Capacity', '<input id="cap-input" name="capacity" type="number" min="1" max="5" class="form-input" value="5" readonly style="background:#f8fafc;color:#64748b">')
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
      const clash = state.classes.find(function(c) {
        if (c.day !== day || c.classroom !== classroom) return false;
        return time < c.endTime && endTime > c.time;
      });
      if (clash) {
        App.Utils.showToast('Clash: ' + classroom + ' is already booked ' + App.Utils.formatTime(clash.time) + '–' + App.Utils.formatTime(clash.endTime) + ' on ' + day, 'error');
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
        color: classType === 'Private' ? 'purple' : 'blue'
      };
      App.Store.set({ classes: [...state.classes, newClass] });
      App.Utils.hideModal();
      App.Utils.showToast('Class added!', 'success');
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

  App.Calendar = { render: render, _prevWeek: _prevWeek, _nextWeek: _nextWeek, _addClassModal: _addClassModal, _setView: _setView, _prevMonth: _prevMonth, _nextMonth: _nextMonth, _onTypeChange: _onTypeChange, _setSearch: _setSearch, _setTeacher: _setTeacher, _clearFilters: _clearFilters };
})();
