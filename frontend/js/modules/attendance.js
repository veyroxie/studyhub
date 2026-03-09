(function() {
  window.App = window.App || {};

  let _attTab = 'staff';
  let _attDate = '';
  let _attClassId = '';

  // ─── shared mobile-friendly style constants ───────────────────────────────
  var ROW_STYLE    = 'display:flex;align-items:flex-start;justify-content:space-between;padding:1rem;border-bottom:1px solid #f9f8f6;gap:0.75rem;flex-wrap:wrap;min-height:72px';
  var AVATAR_STYLE = 'width:3rem;height:3rem;border-radius:50%;font-weight:800;font-size:1rem;display:flex;align-items:center;justify-content:center;flex-shrink:0';
  var NAME_STYLE   = 'font-weight:700;font-size:1rem;color:#111;line-height:1.3';
  var TIME_STYLE   = 'font-size:0.8rem;color:#94a3b8;margin-top:3px;line-height:1.4';

  // ─── shared student row builder ───────────────────────────────────────────
  function _studentRow(s, rec) {
    var checkedIn  = rec && rec.checkIn;
    var checkedOut = rec && rec.checkOut;
    var rowBg      = checkedOut ? '#f0fdf4' : checkedIn ? '#fefce8' : '#fff';
    var avatarBg   = checkedOut ? '#dcfce7' : checkedIn ? '#fef9c3' : '#f1f5f9';
    var avatarCol  = checkedOut ? '#15803d' : checkedIn ? '#a16207' : '#64748b';
    var timeStr    = checkedOut
      ? 'In: ' + App.Utils.formatTime(rec.checkIn) + '  ·  Out: ' + App.Utils.formatTime(rec.checkOut)
      : checkedIn ? 'In: ' + App.Utils.formatTime(rec.checkIn) + '  · still in'
      : 'Not checked in';

    var actionBtn;
    if (!checkedIn) {
      actionBtn = '<button onclick="App.Attendance._checkInStudent(\'' + s.id + '\')" style="'
        + 'min-height:52px;width:100%;padding:0.6rem 1.1rem;background:#22c55e;color:#fff;border:none;'
        + 'border-radius:12px;font-size:0.95rem;font-weight:700;cursor:pointer;transition:opacity 0.15s" '
        + 'onmouseover="this.style.opacity=\'0.85\'" onmouseout="this.style.opacity=\'1\'">Check In</button>';
    } else if (!checkedOut) {
      actionBtn = '<button onclick="App.Attendance._checkOutStudent(\'' + s.id + '\')" style="'
        + 'min-height:52px;width:100%;padding:0.6rem 1.1rem;background:#64748b;color:#fff;border:none;'
        + 'border-radius:12px;font-size:0.95rem;font-weight:700;cursor:pointer;transition:opacity 0.15s" '
        + 'onmouseover="this.style.opacity=\'0.85\'" onmouseout="this.style.opacity=\'1\'">Check Out</button>';
    } else {
      actionBtn = '<div style="display:flex;align-items:center;justify-content:center;padding:0.6rem 1rem;'
        + 'background:#dcfce7;border-radius:12px;min-height:52px">'
        + '<span style="font-size:0.95rem;font-weight:700;color:#15803d">Done</span></div>';
    }

    return '<div style="' + ROW_STYLE + ';background:' + rowBg + '">'
      + '<div style="display:flex;align-items:center;gap:0.85rem;flex:1 1 auto;min-width:0">'
      +   '<div style="' + AVATAR_STYLE + ';background:' + avatarBg + ';color:' + avatarCol + '">'
      +     (s.firstName||'?').charAt(0) + (s.lastName||'').charAt(0)
      +   '</div>'
      +   '<div style="min-width:0">'
      +     '<div style="' + NAME_STYLE + '">' + App.Utils.esc(s.firstName + ' ' + s.lastName) + '</div>'
      +     '<div style="' + TIME_STYLE + '">' + timeStr + '</div>'
      +   '</div>'
      + '</div>'
      + '<div style="flex:0 0 auto;min-width:110px;max-width:160px;align-self:center">'
      +   actionBtn
      + '</div>'
      + '</div>';
  }

  function render(container) {
    try {
      if (!_attDate) {
        try { _attDate = App.Utils.today(); } catch(e) { _attDate = new Date().toISOString().slice(0,10); }
      }
      const { classes } = App.Store.get();
      if (!_attClassId && classes.length) _attClassId = classes[0].id;
      const isClient = App.currentRole === 'client';
      const isTeacher = App.currentRole === 'teacher';

      container.innerHTML = '<div style="display:flex;flex-direction:column;gap:1rem">'
        + '<div style="display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:0.5rem">'
        +   '<h1 style="font-size:1.4rem;font-weight:800;color:#0d0d0d;letter-spacing:-0.03em;margin:0">Attendance</h1>'
        +   (isClient ? '<div style="font-size:0.78rem;color:#94a3b8;background:#f1f5f9;padding:0.4rem 0.85rem;border-radius:8px">Viewing: ' + (App.clientParent || 'Your child') + '</div>' : '')
        + '</div>'
        + (isClient ? _renderClientView() : isTeacher ? _renderTeacherView() : _renderAdminView())
        + '</div>';
    } catch(err) {
      container.innerHTML = '<div style="padding:2rem;background:#fef2f2;border:1px solid #fca5a5;border-radius:14px;color:#dc2626;font-size:0.9rem">'
        + '<strong>Attendance failed to load.</strong> ' + (err && err.message ? err.message : '') + '</div>';
    }
  }

  function _renderAdminView() {
    return '<div style="display:flex;gap:0.35rem;background:#f1f5f9;border-radius:10px;padding:3px;width:fit-content;margin-bottom:0.25rem">'
      + ['staff','students'].map(function(t) {
          const active = t === _attTab;
          const label  = t === 'staff' ? 'Staff' : 'Students';
          return '<button onclick="App.Attendance._setTab(\'' + t + '\')" style="'
            + 'padding:0.5rem 1.2rem;font-size:0.85rem;font-weight:700;border:none;border-radius:8px;cursor:pointer;min-height:44px;transition:all 0.15s;'
            + (active ? 'background:var(--gold);color:#0a0a0a;' : 'background:transparent;color:#94a3b8;')
            + '">' + label + '</button>';
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

    if (myRecords.length === 0) {
      return '<div style="background:#fff;border-radius:14px;border:2px dashed #e2e8f0;padding:3rem;text-align:center;color:#94a3b8;font-size:0.9rem">No attendance records found</div>';
    }

    // Card-list layout — works on all screen widths without a horizontal-scroll table
    return '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);overflow:hidden">'
      + myRecords.map(function(rec) {
          const stu = App.Store.get().students.find(function(s) { return s.id === rec.personId; });
          const stuName = stu ? stu.firstName + ' ' + stu.lastName : rec.personId;
          return '<div style="' + ROW_STYLE + '">'
            + '<div style="flex:1 1 auto;min-width:0">'
            +   '<div style="' + NAME_STYLE + '">' + App.Utils.esc(stuName) + '</div>'
            +   '<div style="' + TIME_STYLE + '">' + App.Utils.formatDate(rec.date) + '</div>'
            +   '<div style="' + TIME_STYLE + '">'
            +     (rec.checkIn  ? 'In: '  + App.Utils.formatTime(rec.checkIn)  : '')
            +     (rec.checkIn && rec.checkOut ? '  ·  ' : '')
            +     (rec.checkOut ? 'Out: ' + App.Utils.formatTime(rec.checkOut) : '')
            +   '</div>'
            + '</div>'
            + '<div style="flex-shrink:0;align-self:center">' + App.Utils.statusBadge(rec.status) + '</div>'
            + '</div>';
        }).join('')
      + '</div>';
  }

  var SCOL = {
    Present: { bg:'#f0fdf4', border:'#86efac', text:'#15803d', active:'#22c55e', activeText:'#fff' },
    Late:    { bg:'#fffbeb', border:'#fcd34d', text:'#d97706', active:'#f59e0b', activeText:'#fff' },
    Absent:  { bg:'#fef2f2', border:'#fca5a5', text:'#dc2626', active:'#ef4444', activeText:'#fff' }
  };

  function _staffTab() {
    const { staff, attendance } = App.Store.get();
    return '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);overflow:hidden">'
      + '<div style="padding:0.9rem 1rem;border-bottom:1px solid #f0ede8;background:#faf9f7;display:flex;align-items:center;gap:0.75rem;flex-wrap:wrap">'
      +   '<input type="date" value="' + _attDate + '" onchange="App.Attendance._setDate(this.value)" style="padding:0.6rem 0.9rem;font-size:0.9rem;border:1px solid #e2e8f0;border-radius:9px;outline:none;min-height:48px">'
      +   '<span style="font-size:0.82rem;color:#94a3b8">Staff attendance for selected date</span>'
      + '</div>'
      + '<div>'
      + staff.map(function(s) {
          const rec    = attendance.find(function(a) { return a.personId === s.id && a.personType === 'staff' && a.date === _attDate; });
          const status = rec ? rec.status : null;
          const timeStr = rec && rec.checkIn
            ? 'In: ' + App.Utils.formatTime(rec.checkIn) + (rec.checkOut ? '  ·  Out: ' + App.Utils.formatTime(rec.checkOut) : '  · still in')
            : 'Not recorded';
          return '<div style="' + ROW_STYLE + '">'
            + '<div style="display:flex;align-items:center;gap:0.85rem;flex:1 1 auto;min-width:0">'
            +   '<div style="' + AVATAR_STYLE + ';background:var(--gold-dim);color:var(--gold);border:1px solid rgba(201,162,39,0.2)">' + (s.name || s.fullName || '?').charAt(0) + '</div>'
            +   '<div style="min-width:0">'
            +     '<div style="' + NAME_STYLE + '">' + App.Utils.esc(s.fullName) + '</div>'
            +     '<div style="' + TIME_STYLE + '">' + timeStr + '</div>'
            +   '</div>'
            + '</div>'
            + '<div style="display:flex;gap:0.45rem;flex-shrink:0;align-self:center;flex-wrap:wrap">'
            + ['Present','Late','Absent'].map(function(st) {
                const active = status === st;
                const c = SCOL[st];
                return '<button onclick="App.Attendance._markStaff(\'' + s.id + '\',\'' + st + '\')" style="'
                  + 'min-height:48px;padding:0.4rem 1rem;border-radius:999px;font-size:0.85rem;font-weight:700;'
                  + 'cursor:pointer;transition:all 0.15s;white-space:nowrap;'
                  + 'border:2px solid ' + (active ? c.active : c.border) + ';'
                  + 'background:' + (active ? c.active : c.bg) + ';'
                  + 'color:' + (active ? c.activeText : c.text) + '">'
                  + st + '</button>';
              }).join('')
            + '</div>'
            + '</div>';
        }).join('')
      + '</div>'
      + '</div>';
  }

  function _studentTab() {
    const { classes, students, attendance } = App.Store.get();
    const selectedClass    = classes.find(function(c) { return c.id === _attClassId; });
    const enrolledStudents = selectedClass
      ? students.filter(function(s) { return s.enrolledClasses.indexOf(_attClassId) > -1; })
      : [];

    const todayRecs    = attendance.filter(function(a) { return a.classId === _attClassId && a.date === _attDate; });
    const presentCount = todayRecs.filter(function(a) { return a.checkIn; }).length;

    return '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);overflow:hidden">'
      + '<div style="padding:0.9rem 1rem;border-bottom:1px solid #f0ede8;background:#faf9f7">'
      +   '<div style="display:flex;gap:0.6rem;flex-wrap:wrap;align-items:center">'
      +     '<input type="date" value="' + _attDate + '" onchange="App.Attendance._setDate(this.value)" style="padding:0.6rem 0.9rem;font-size:0.9rem;border:1px solid #e2e8f0;border-radius:9px;outline:none;min-height:48px;flex:1;min-width:140px">'
      +     '<select onchange="App.Attendance._setClass(this.value)" style="padding:0.6rem 0.9rem;font-size:0.9rem;border:1px solid #e2e8f0;border-radius:9px;outline:none;min-height:48px;flex:2;min-width:180px;background:#fff">'
      +     classes.map(function(c) { return '<option value="' + c.id + '" ' + (c.id === _attClassId ? 'selected' : '') + '>' + c.name + ' — ' + c.day + '</option>'; }).join('')
      +     '</select>'
      +   '</div>'
      +   (selectedClass
          ? '<div style="margin-top:0.5rem;font-size:0.78rem;color:#94a3b8">'
          +   enrolledStudents.length + ' enrolled  ·  <span style="color:#15803d;font-weight:600">' + presentCount + ' checked in today</span>'
          + '</div>'
          : '')
      + '</div>'
      + (enrolledStudents.length === 0
        ? '<div style="padding:3rem;text-align:center;color:#94a3b8;font-size:0.9rem">No students enrolled in this class</div>'
        : '<div>'
        + enrolledStudents.map(function(s) {
            const rec = attendance.find(function(a) { return a.personId === s.id && a.classId === _attClassId && a.date === _attDate; });
            return _studentRow(s, rec);
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

    // If marked Absent and user is admin, check for classes on this day and offer to cancel
    if (status === 'Absent' && App.currentRole === 'admin') {
      const state2 = App.Store.get();
      const dateDay = new Date(_attDate + 'T00:00:00').toLocaleDateString('en-US', { weekday: 'long' });
      const staffMember = state2.staff.find(function(s) { return s.id === staffId; });
      const affectedClasses = state2.classes.filter(function(c) { return c.teacherIds.indexOf(staffId) > -1 && c.day === dateDay; });
      if (affectedClasses.length > 0) {
        App.Router.refresh();
        _cancelClassModal(staffMember, affectedClasses);
        return;
      }
    }
    App.Router.refresh();
  }

  function _cancelClassModal(staffMember, affectedClasses) {
    const staffName = staffMember ? staffMember.fullName : 'Teacher';
    const ids = affectedClasses.map(function(c) { return c.id; });
    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-lg font-bold mb-1">Cancel Classes?</h2>'
      + '<p class="text-sm text-slate-500 mb-4">' + App.Utils.esc(staffName) + ' is absent on ' + App.Utils.formatDate(_attDate) + '. These classes will be affected:</p>'
      + '<div class="space-y-2 mb-5">'
      + affectedClasses.map(function(c) {
          return '<div style="padding:0.65rem 0.85rem;background:#fef2f2;border:1px solid #fca5a5;border-radius:10px;display:flex;justify-content:space-between;align-items:center">'
            + '<div><span style="font-weight:600;font-size:0.85rem">' + App.Utils.esc(c.name) + '</span>'
            + '<span style="font-size:0.78rem;color:#94a3b8;margin-left:0.5rem">' + App.Utils.formatTime(c.time) + '–' + App.Utils.formatTime(c.endTime) + '</span></div>'
            + App.Utils.badge(c.enrolled + ' student' + (c.enrolled !== 1 ? 's' : ''), 'red')
            + '</div>';
        }).join('')
      + '</div>'
      + '<p class="text-sm text-slate-500 mb-4">Enrolled parents will receive an in-app message notification.</p>'
      + '<div class="flex gap-3">'
      + '<button onclick="App.Utils.hideModal()" class="flex-1 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Skip</button>'
      + '<button onclick="App.Attendance._doCancelClasses(' + JSON.stringify(ids) + ')" style="flex:1;padding:0.5rem;font-size:0.85rem;font-weight:700;background:#ef4444;color:#fff;border:none;border-radius:8px;cursor:pointer">Cancel &amp; Notify Parents</button>'
      + '</div>'
      + '</div>'
    );
  }

  function _doCancelClasses(classIds) {
    const state = App.Store.get();
    const cancelled     = (state.cancelledClasses || []).slice();
    const messages      = (state.messages || []).slice();
    const announcements = (state.announcements || []).slice();
    const now = new Date().toISOString();

    classIds.forEach(function(classId) {
      cancelled.push({ id: App.Utils.generateId('cancel'), classId: classId, date: _attDate, reason: 'Teacher absent' });

      const cls = state.classes.find(function(c) { return c.id === classId; });
      if (!cls) return;

      // Create an in-app announcement for all parents
      const timeRange = App.Utils.formatTime(cls.time) + '\u2013' + App.Utils.formatTime(cls.endTime);
      announcements.push({
        id: App.Utils.generateId('ann'),
        title: 'Class Cancelled: ' + cls.name,
        message: "Today's " + cls.name + ' class (' + timeRange + ') has been cancelled due to teacher absence. We apologise for the inconvenience.',
        audience: 'All Parents',
        type: 'Urgent',
        createdOn: _attDate,
        createdBy: 'Admin'
      });

      // Also send direct messages to each enrolled parent
      const enrolledStudents = state.students.filter(function(s) { return s.enrolledClasses.indexOf(classId) > -1; });
      const parentEmails = {};
      enrolledStudents.forEach(function(s) { if (s.contact) parentEmails[s.contact] = true; });

      Object.keys(parentEmails).forEach(function(email) {
        messages.push({
          id: App.Utils.generateId('msg'),
          fromRole: 'admin',
          fromLabel: 'Admin',
          toParent: email,
          text: 'Class "' + cls.name + '" on ' + App.Utils.formatDate(_attDate) + ' (' + App.Utils.formatTime(cls.time) + ') has been cancelled due to teacher absence. We apologise for the inconvenience.',
          ts: now,
          read: false
        });
      });
    });

    App.Store.set({ cancelledClasses: cancelled, messages: messages, announcements: announcements });
    App.Utils.hideModal();
    if (App.Notifs && App.Notifs.refresh) App.Notifs.refresh();
    App.Utils.showToast('Class cancelled. Parents have been notified.', 'success');
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

  function _renderTeacherView() {
    const { classes } = App.Store.get();
    // Get teacher's classes
    const myClasses = classes.filter(function(c) { return c.teacherIds.indexOf(App.currentTeacher) > -1; });

    // Auto-select first class if current selection isn't theirs
    if (!myClasses.find(function(c) { return c.id === _attClassId; }) && myClasses.length > 0) {
      _attClassId = myClasses[0].id;
    }

    return '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);margin-bottom:1rem;overflow:hidden">'
      + '<div style="padding:0.75rem 1.1rem;border-bottom:1px solid #f0ede8;background:rgba(139,92,246,0.05)">'
      +   '<span style="font-size:0.78rem;font-weight:700;color:#7c3aed">MY CLASSES — Student Attendance</span>'
      + '</div>'
      + _studentTabFiltered(myClasses)
      + '</div>';
  }

  function _studentTabFiltered(myClasses) {
    const { students, attendance } = App.Store.get();
    if (myClasses.length === 0) {
      return '<div style="padding:3rem;text-align:center;color:#94a3b8;font-size:0.85rem">No classes assigned to you yet</div>';
    }
    const selectedClass = myClasses.find(function(c) { return c.id === _attClassId; }) || myClasses[0];
    const enrolledStudents = selectedClass
      ? students.filter(function(s) { return s.enrolledClasses.indexOf(selectedClass.id) > -1; })
      : [];
    const todayRecs = attendance.filter(function(a) { return a.classId === selectedClass.id && a.date === _attDate; });
    const presentCount = todayRecs.filter(function(a) { return a.checkIn; }).length;

    return '<div style="padding:0.9rem 1rem;border-bottom:1px solid #f0ede8;background:#faf9f7">'
      +   '<div style="display:flex;gap:0.6rem;flex-wrap:wrap;align-items:center">'
      +     '<input type="date" value="' + _attDate + '" onchange="App.Attendance._setDate(this.value)" style="padding:0.6rem 0.9rem;font-size:0.9rem;border:1px solid #e2e8f0;border-radius:9px;outline:none;min-height:48px">'
      +     '<select onchange="App.Attendance._setClass(this.value)" style="padding:0.6rem 0.9rem;font-size:0.9rem;border:1px solid #e2e8f0;border-radius:9px;outline:none;min-height:48px;background:#fff">'
      +     myClasses.map(function(c) { return '<option value="' + c.id + '" ' + (c.id === selectedClass.id ? 'selected' : '') + '>' + c.name + ' — ' + c.day + '</option>'; }).join('')
      +     '</select>'
      +   '</div>'
      +   '<div style="margin-top:0.5rem;font-size:0.78rem;color:#94a3b8">' + enrolledStudents.length + ' enrolled  ·  <span style="color:#15803d;font-weight:600">' + presentCount + ' checked in</span></div>'
      + '</div>'
      + (enrolledStudents.length === 0
        ? '<div style="padding:3rem;text-align:center;color:#94a3b8;font-size:0.9rem">No students enrolled in this class</div>'
        : '<div>'
        + enrolledStudents.map(function(s) {
            const rec = attendance.find(function(a) { return a.personId === s.id && a.classId === selectedClass.id && a.date === _attDate; });
            return _studentRow(s, rec);
          }).join('')
        + '</div>');
  }

  App.Attendance = { render: render, _setTab: _setTab, _setDate: _setDate, _setClass: _setClass, _markStaff: _markStaff, _checkInStudent: _checkInStudent, _checkOutStudent: _checkOutStudent, _doCancelClasses: _doCancelClasses };
})();
