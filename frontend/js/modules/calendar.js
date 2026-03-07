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
          return true;
        })
        .sort(function(a, b) { return a.time.localeCompare(b.time); });
    });

    const totalClasses = classes.length;
    const avgFill = classes.reduce(function(s, c) { return s + (c.enrolled / c.capacity); }, 0) / (classes.length || 1);
    const fullClasses = classes.filter(function(c) { return c.enrolled >= c.capacity; }).length;

    container.innerHTML = ''
      + '<div class="flex items-center justify-between mb-6">'
      +   '<div>'
      +     '<h1 class="text-2xl font-bold text-slate-800">Class Schedule</h1>'
      +     '<p class="text-sm text-slate-500 mt-0.5">Week of ' + fmtShortDate(_weekStart) + ' – ' + fmtShortDate(weekDates[6]) + '</p>'
      +   '</div>'
      +   '<div class="flex items-center gap-3">'
      +     '<button onclick="App.Calendar._prevWeek()" class="px-3 py-1.5 text-sm border border-slate-200 rounded-lg hover:bg-slate-50 text-slate-600">&#8592; Prev</button>'
      +     '<button onclick="App.Calendar._nextWeek()" class="px-3 py-1.5 text-sm border border-slate-200 rounded-lg hover:bg-slate-50 text-slate-600">Next &#8594;</button>'
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
      + '</div>'

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

  function _addClassModal() {
    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-4">Add New Class</h2>'
      + '<form id="add-class-form" class="space-y-4">'
      + _field('Class Name', '<input name="name" class="form-input" placeholder="e.g. Level 1 & 2" required>')
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Day', '<select name="day" class="form-input">' + ['Monday','Tuesday','Wednesday','Thursday','Friday','Saturday','Sunday'].map(function(d){ return '<option>' + d + '</option>'; }).join('') + '</select>')
      + _field('Classroom', '<input name="classroom" class="form-input" placeholder="Classroom 1">')
      + '</div>'
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Start Time', '<input name="time" type="time" class="form-input" required>')
      + _field('End Time', '<input name="endTime" type="time" class="form-input" required>')
      + '</div>'
      + _field('Capacity', '<input name="capacity" type="number" min="1" max="30" class="form-input" value="6">')
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Add Class</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );
    document.getElementById('add-class-form').addEventListener('submit', function(e) {
      e.preventDefault();
      const fd = new FormData(e.target);
      const state = App.Store.get();
      const newClass = {
        id: App.Utils.generateId('c'),
        name: fd.get('name'),
        teacherIds: [],
        classroom: fd.get('classroom') || 'Classroom 1',
        day: fd.get('day'),
        time: fd.get('time'),
        endTime: fd.get('endTime'),
        capacity: parseInt(fd.get('capacity'),10) || 6,
        enrolled: 0,
        color: 'blue'
      };
      App.Store.set({ classes: [...state.classes, newClass] });
      App.Utils.hideModal();
      App.Utils.showToast('Class added successfully!', 'success');
      App.Router.refresh();
    });
  }

  function _field(label, inputHtml) {
    return '<div><label class="block text-sm font-medium text-slate-700 mb-1">' + label + '</label>' + inputHtml + '</div>';
  }

  App.Calendar = { render: render, _prevWeek: _prevWeek, _nextWeek: _nextWeek, _addClassModal: _addClassModal };
})();
