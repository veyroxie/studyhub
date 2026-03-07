(function() {
  window.App = window.App || {};

  let _attTab = 'staff';
  let _attDate = '';
  let _attClassId = '';

  function render(container) {
    if (!_attDate) _attDate = App.Utils.today();
    const { classes } = App.Store.get();
    if (!_attClassId && classes.length) _attClassId = classes[0].id;
    const isClient = App.currentRole === 'client';

    container.innerHTML = ''
      + '<div class="flex items-center justify-between mb-6">'
      +   '<h1 class="text-2xl font-bold text-slate-800">Attendance</h1>'
      +   (isClient ? '<div class="text-sm text-slate-400 bg-slate-100 px-3 py-1.5 rounded-lg">Viewing: ' + (App.clientParent || 'Your child') + '</div>' : '')
      + '</div>'
      + (isClient ? _renderClientView() : _renderAdminView());
  }

  function _renderAdminView() {
    return '<div class="flex border-b border-slate-100 mb-5 gap-1">'
      + ['staff','students'].map(function(t) {
          const active = t === _attTab;
          const label = t === 'staff' ? 'Staff Attendance' : 'Student Check-In/Out';
          return '<button onclick="App.Attendance._setTab(\'' + t + '\')" class="px-5 py-2.5 text-sm font-medium transition-colors ' + (active ? 'border-b-2 border-blue-600 text-blue-600' : 'text-slate-500 hover:text-slate-700') + '">' + label + '</button>';
        }).join('')
      + '</div>'
      + (_attTab === 'staff' ? _staffTab() : _studentTab());
  }

  function _renderClientView() {
    const { attendance, students } = App.Store.get();
    const myStudentIds = App.clientParent
      ? students.filter(function(s) { return s.contact === App.clientParent; }).map(function(s) { return s.id; })
      : [];
    const myRecords = attendance.filter(function(a) {
      return a.personType === 'student' && myStudentIds.indexOf(a.personId) > -1;
    }).sort(function(a, b) { return b.date.localeCompare(a.date); });

    if (myRecords.length === 0) return '<div class="bg-white rounded-xl border border-dashed border-slate-200 p-12 text-center"><p class="text-slate-400">No attendance records found</p></div>';

    return '<div class="bg-white rounded-xl border border-slate-100 shadow-sm overflow-hidden">'
      + '<table class="w-full"><thead class="bg-slate-50 border-b"><tr>'
      + '<th class="th">Date</th><th class="th">Student</th><th class="th">Check In</th><th class="th">Check Out</th><th class="th">Status</th>'
      + '</tr></thead><tbody class="divide-y divide-slate-50">'
      + myRecords.map(function(rec) {
          const stu = App.Store.get().students.find(function(s) { return s.id === rec.personId; });
          const stuName = stu ? stu.firstName + ' ' + stu.lastName : rec.personId;
          return '<tr class="hover:bg-slate-50"><td class="td text-sm">' + App.Utils.formatDate(rec.date) + '</td>'
            + '<td class="td font-medium">' + stuName + '</td>'
            + '<td class="td text-sm">' + (rec.checkIn ? App.Utils.formatTime(rec.checkIn) : '—') + '</td>'
            + '<td class="td text-sm">' + (rec.checkOut ? App.Utils.formatTime(rec.checkOut) : '—') + '</td>'
            + '<td class="td">' + App.Utils.statusBadge(rec.status) + '</td>'
            + '</tr>';
        }).join('')
      + '</tbody></table></div>';
  }

  function _staffTab() {
    const { staff, attendance } = App.Store.get();
    return '<div class="bg-white rounded-xl border border-slate-100 shadow-sm">'
      + '<div class="p-4 border-b border-slate-100 flex items-center gap-4">'
      +   '<input type="date" value="' + _attDate + '" onchange="App.Attendance._setDate(this.value)" class="px-3 py-2 text-sm border border-slate-200 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-400">'
      +   '<span class="text-sm text-slate-500">Staff attendance for selected date</span>'
      + '</div>'
      + '<div class="divide-y divide-slate-50">'
      + staff.map(function(s) {
          const rec = attendance.find(function(a) { return a.personId === s.id && a.personType === 'staff' && a.date === _attDate; });
          const status = rec ? rec.status : null;
          const statusColors = { Present:'bg-emerald-50 border-emerald-200 text-emerald-700', Late:'bg-amber-50 border-amber-200 text-amber-700', Absent:'bg-red-50 border-red-200 text-red-700' };
          return '<div class="flex items-center justify-between px-5 py-4">'
            + '<div class="flex items-center gap-3">'
            +   '<div class="w-10 h-10 rounded-full bg-blue-50 text-blue-600 font-bold flex items-center justify-center">' + s.name.charAt(0) + '</div>'
            +   '<div><div class="font-medium text-slate-800">' + s.fullName + '</div>'
            +   '<div class="text-xs text-slate-400">' + (rec && rec.checkIn ? 'In: ' + App.Utils.formatTime(rec.checkIn) + (rec.checkOut ? '  ·  Out: ' + App.Utils.formatTime(rec.checkOut) : '') : 'Not recorded') + '</div>'
            +   '</div>'
            + '</div>'
            + '<div class="flex items-center gap-2">'
            + ['Present','Late','Absent'].map(function(st) {
                const active = status === st;
                return '<button onclick="App.Attendance._markStaff(\'' + s.id + '\',\'' + st + '\')" class="text-xs px-3 py-1.5 rounded-lg border font-medium transition-all ' + (active ? (statusColors[st] || '') : 'border-slate-200 text-slate-500 hover:bg-slate-50') + '">' + st + '</button>';
              }).join('')
            + '</div>'
            + '</div>';
        }).join('')
      + '</div>'
      + '</div>';
  }

  function _studentTab() {
    const { classes, students, attendance } = App.Store.get();
    const selectedClass = classes.find(function(c) { return c.id === _attClassId; });
    const enrolledStudents = selectedClass
      ? students.filter(function(s) { return s.enrolledClasses.indexOf(_attClassId) > -1; })
      : [];

    return '<div class="bg-white rounded-xl border border-slate-100 shadow-sm">'
      + '<div class="p-4 border-b border-slate-100 flex items-center gap-3 flex-wrap">'
      +   '<input type="date" value="' + _attDate + '" onchange="App.Attendance._setDate(this.value)" class="px-3 py-2 text-sm border border-slate-200 rounded-lg">'
      +   '<select onchange="App.Attendance._setClass(this.value)" class="px-3 py-2 text-sm border border-slate-200 rounded-lg">'
      +   classes.map(function(c) { return '<option value="' + c.id + '" ' + (c.id === _attClassId ? 'selected' : '') + '>' + c.name + ' (' + c.day + ')</option>'; }).join('')
      +   '</select>'
      + '</div>'
      + (enrolledStudents.length === 0
        ? '<div class="p-8 text-center text-slate-400 text-sm">No students enrolled in this class</div>'
        : '<div class="divide-y divide-slate-50">'
        + enrolledStudents.map(function(s) {
            const rec = attendance.find(function(a) { return a.personId === s.id && a.classId === _attClassId && a.date === _attDate; });
            const checkedIn = rec && rec.checkIn;
            const checkedOut = rec && rec.checkOut;
            return '<div class="flex items-center justify-between px-5 py-4">'
              + '<div class="flex items-center gap-3">'
              +   '<div class="w-10 h-10 rounded-full bg-emerald-50 text-emerald-700 font-bold text-sm flex items-center justify-center">' + s.firstName.charAt(0) + s.lastName.charAt(0) + '</div>'
              +   '<div><div class="font-medium text-slate-800">' + s.firstName + ' ' + s.lastName + '</div>'
              +   '<div class="text-xs text-slate-400">' + (checkedIn ? 'In ' + App.Utils.formatTime(rec.checkIn) + (checkedOut ? ' · Out ' + App.Utils.formatTime(rec.checkOut) : '') : 'Not checked in') + '</div>'
              +   '</div>'
              + '</div>'
              + '<div class="flex items-center gap-2">'
              +   (!checkedIn ? '<button onclick="App.Attendance._checkInStudent(\'' + s.id + '\')" class="text-xs px-4 py-1.5 bg-emerald-500 text-white rounded-lg hover:bg-emerald-600 font-medium">Check In</button>' : '')
              +   (checkedIn && !checkedOut ? '<button onclick="App.Attendance._checkOutStudent(\'' + s.id + '\')" class="text-xs px-4 py-1.5 bg-slate-500 text-white rounded-lg hover:bg-slate-600 font-medium">Check Out</button>' : '')
              +   (checkedOut ? '<span class="text-xs text-slate-400 font-medium">Done ✓</span>' : '')
              + '</div>'
              + '</div>';
          }).join('')
        + '</div>')
      + '</div>';
  }

  function _setTab(tab) { _attTab = tab; App.Router.refresh(); }
  function _setDate(date) { _attDate = date; App.Router.refresh(); }
  function _setClass(classId) { _attClassId = classId; App.Router.refresh(); }

  function _markStaff(staffId, status) {
    const state = App.Store.get();
    const existingIdx = state.attendance.findIndex(function(a) { return a.personId === staffId && a.personType === 'staff' && a.date === _attDate; });
    const now = App.Utils.nowTime();
    let newAtt = state.attendance.slice();
    if (existingIdx > -1) {
      newAtt[existingIdx] = { ...newAtt[existingIdx], status: status, checkIn: status !== 'Absent' ? (newAtt[existingIdx].checkIn || now) : null };
    } else {
      newAtt.push({ id: App.Utils.generateId('ATT'), personId: staffId, personType: 'staff', date: _attDate, checkIn: status !== 'Absent' ? now : null, checkOut: null, status: status });
    }
    App.Store.set({ attendance: newAtt });
    App.Router.refresh();
  }

  function _checkInStudent(studentId) {
    const state = App.Store.get();
    const now = App.Utils.nowTime();
    const existing = state.attendance.find(function(a) { return a.personId === studentId && a.classId === _attClassId && a.date === _attDate; });
    let newAtt = state.attendance.slice();
    if (!existing) {
      newAtt.push({ id: App.Utils.generateId('ATT'), personId: studentId, personType: 'student', date: _attDate, classId: _attClassId, checkIn: now, checkOut: null, status: 'Present' });
    }
    App.Store.set({ attendance: newAtt });
    const stu = state.students.find(function(s) { return s.id === studentId; });
    const stuName = stu ? stu.firstName + ' ' + stu.lastName : studentId;
    App.Utils.showToast(stuName + ' checked in at ' + App.Utils.formatTime(now), 'info');
    // Simulate parent notification via BroadcastChannel
    try {
      const ch = new BroadcastChannel('studyhub_notifs');
      ch.postMessage({ type: 'CHECK_IN', student: stuName, time: now, parent: stu ? stu.contact : '' });
      ch.close();
    } catch(e) {}
    App.Router.refresh();
  }

  function _checkOutStudent(studentId) {
    const state = App.Store.get();
    const now = App.Utils.nowTime();
    const newAtt = state.attendance.map(function(a) {
      return (a.personId === studentId && a.classId === _attClassId && a.date === _attDate) ? { ...a, checkOut: now } : a;
    });
    App.Store.set({ attendance: newAtt });
    const stu = state.students.find(function(s) { return s.id === studentId; });
    const stuName = stu ? stu.firstName + ' ' + stu.lastName : studentId;
    App.Utils.showToast('📱 Parent notified: ' + stuName + ' checked out at ' + App.Utils.formatTime(now), 'success');
    try {
      const ch = new BroadcastChannel('studyhub_notifs');
      ch.postMessage({ type: 'CHECK_OUT', student: stuName, time: now, parent: stu ? stu.contact : '' });
      ch.close();
    } catch(e) {}
    App.Router.refresh();
  }

  App.Attendance = { render: render, _setTab: _setTab, _setDate: _setDate, _setClass: _setClass, _markStaff: _markStaff, _checkInStudent: _checkInStudent, _checkOutStudent: _checkOutStudent };
})();
