(function() {
  window.App = window.App || {};

  let _attTab = 'staff';
  let _attDate = '';
  let _attClassId = '';
  let _showAllStaff = false;
  let _showAllClasses = false;
  let _attClientPage = 0;
  var _ATT_PAGE_SIZE = 20;

  function _paginationControls(page, total, pageSize, moduleFn) {
    var totalPages = Math.ceil(total / pageSize);
    if (total <= pageSize) return '';
    var start = page * pageSize + 1;
    var end = Math.min((page + 1) * pageSize, total);
    var prevDis = page === 0;
    var nextDis = page >= totalPages - 1;
    return '<div style="display:flex;align-items:center;justify-content:space-between;margin-top:1rem;padding:0.75rem 1rem;">'
      + '<span style="font-size:0.8rem;color:#64748b;">Showing ' + start + '–' + end + ' of ' + total + '</span>'
      + '<div style="display:flex;gap:0.5rem;">'
      + '<button onclick="' + moduleFn + '(' + (page - 1) + ')"' + (prevDis ? ' disabled' : '') + ' style="padding:0.35rem 0.75rem;font-size:0.8rem;border:1px solid #e2e8f0;border-radius:8px;cursor:' + (prevDis ? 'default' : 'pointer') + ';background:#fff;color:#374151;' + (prevDis ? 'opacity:0.4;' : '') + '">Prev</button>'
      + '<button onclick="' + moduleFn + '(' + (page + 1) + ')"' + (nextDis ? ' disabled' : '') + ' style="padding:0.35rem 0.75rem;font-size:0.8rem;border:1px solid #e2e8f0;border-radius:8px;cursor:' + (nextDis ? 'default' : 'pointer') + ';background:#fff;color:#374151;' + (nextDis ? 'opacity:0.4;' : '') + '">Next</button>'
      + '</div></div>';
  }

  // ─── shared mobile-friendly style constants ───────────────────────────────
  var ROW_STYLE    = 'display:flex;align-items:flex-start;justify-content:space-between;padding:1rem;border-bottom:1px solid #f9f8f6;gap:0.75rem;flex-wrap:wrap;min-height:72px';
  var AVATAR_STYLE = 'width:3rem;height:3rem;border-radius:50%;font-weight:800;font-size:1rem;display:flex;align-items:center;justify-content:center;flex-shrink:0';
  var NAME_STYLE   = 'font-weight:700;font-size:1rem;color:#111;line-height:1.3';
  var TIME_STYLE   = 'font-size:0.8rem;color:#94a3b8;margin-top:3px;line-height:1.4';

  // ─── shared student row builder ───────────────────────────────────────────
  function _studentRow(s, rec) {
    var checkedIn  = rec && rec.checkIn;
    var checkedOut = rec && rec.checkOut;
    var isAbsent   = rec && rec.status === 'Absent';
    var rowBg      = isAbsent ? '#fef2f2' : checkedOut ? '#f0fdf4' : checkedIn ? '#fefce8' : '#fff';
    var avatarBg   = isAbsent ? '#fee2e2' : checkedOut ? '#dcfce7' : checkedIn ? '#fef9c3' : '#f1f5f9';
    var avatarCol  = isAbsent ? '#dc2626' : checkedOut ? '#15803d' : checkedIn ? '#a16207' : '#64748b';
    var timeStr    = isAbsent ? 'Absent'
      : checkedOut
      ? 'In: ' + App.Utils.formatTime(rec.checkIn) + '  ·  Out: ' + App.Utils.formatTime(rec.checkOut)
      : checkedIn ? 'In: ' + App.Utils.formatTime(rec.checkIn) + '  · still in'
      : 'Not checked in';

    var undoBtn = (rec && App.currentRole === 'admin')
      ? '<button onclick="App.Attendance._undoAttendance(\'' + rec.id + '\',\'' + s.id + '\',' + (isAbsent ? 'true' : 'false') + ')" style="'
        + 'min-height:30px;width:100%;margin-top:0.3rem;padding:0.25rem 0.6rem;background:none;color:#94a3b8;border:1px dashed #e2e8f0;'
        + 'border-radius:8px;font-size:0.7rem;font-weight:600;cursor:pointer" title="Remove this record as if it was never marked">Undo</button>'
      : '';
    var actionBtn;
    if (isAbsent) {
      actionBtn = '<div style="display:flex;align-items:center;justify-content:center;padding:0.6rem 1rem;'
        + 'background:#fee2e2;border-radius:12px;min-height:52px">'
        + '<span style="font-size:0.95rem;font-weight:700;color:#dc2626">Absent</span></div>' + undoBtn;
    } else if (!checkedIn) {
      actionBtn = '<button onclick="App.Attendance._checkInStudent(\'' + s.id + '\')" style="'
        + 'min-height:52px;width:100%;padding:0.6rem 1.1rem;background:#22c55e;color:#fff;border:none;'
        + 'border-radius:12px;font-size:0.95rem;font-weight:700;cursor:pointer;transition:opacity 0.15s" '
        + 'onmouseover="this.style.opacity=\'0.85\'" onmouseout="this.style.opacity=\'1\'">Check In</button>'
        + ((App.currentRole === 'admin' || App.currentRole === 'teacher')
          ? '<button onclick="App.Attendance._markAbsentCredit(\'' + s.id + '\')" style="'
            + 'min-height:36px;width:100%;margin-top:0.35rem;padding:0.35rem 0.75rem;background:#fef2f2;color:#dc2626;border:1px solid #fecaca;'
            + 'border-radius:10px;font-size:0.75rem;font-weight:600;cursor:pointer;transition:opacity 0.15s" '
            + 'title="Parent informed at least 3 hours before class"'
            + '>Absent + Replacement</button>'
            + '<button onclick="App.Attendance._markAbsentNoCredit(\'' + s.id + '\')" style="'
            + 'min-height:32px;width:100%;margin-top:0.3rem;padding:0.3rem 0.75rem;background:#fff;color:#64748b;border:1px solid #e2e8f0;'
            + 'border-radius:10px;font-size:0.72rem;font-weight:600;cursor:pointer;transition:opacity 0.15s" '
            + 'title="Late notice (less than 3 hours) — no credit issued"'
            + '>Absent (no credit)</button>'
          : '');
    } else if (!checkedOut) {
      actionBtn = '<button onclick="App.Attendance._checkOutStudent(\'' + s.id + '\')" style="'
        + 'min-height:52px;width:100%;padding:0.6rem 1.1rem;background:#64748b;color:#fff;border:none;'
        + 'border-radius:12px;font-size:0.95rem;font-weight:700;cursor:pointer;transition:opacity 0.15s" '
        + 'onmouseover="this.style.opacity=\'0.85\'" onmouseout="this.style.opacity=\'1\'">Check Out</button>' + undoBtn;
    } else {
      actionBtn = '<div style="display:flex;align-items:center;justify-content:center;padding:0.6rem 1rem;'
        + 'background:#dcfce7;border-radius:12px;min-height:52px">'
        + '<span style="font-size:0.95rem;font-weight:700;color:#15803d">Done</span></div>' + undoBtn;
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
        try { _attDate = App.Utils.today(); } catch(e) { _attDate = App.Utils.localDate(new Date()); }
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

  function _downloadCSV(csv, filename) {
    var blob = new Blob([csv], { type: 'text/csv' });
    var url = URL.createObjectURL(blob);
    var a = document.createElement('a');
    a.href = url; a.download = filename; a.click();
    URL.revokeObjectURL(url);
  }

  function _exportCSV() {
    var store = App.Store.get();
    var attendance = store.attendance || [];
    var students = store.students || [];
    var staff = store.staff || [];
    var classes = store.classes || [];

    // Filter to current date and (for students tab) current class
    var filtered;
    if (_attTab === 'staff') {
      filtered = attendance.filter(function(a) { return a.date === _attDate && a.personType === 'staff'; });
    } else if (_attTab === 'students') {
      filtered = attendance.filter(function(a) { return a.date === _attDate && a.classId === _attClassId; });
    } else {
      // Export all records for current date
      filtered = attendance.filter(function(a) { return a.date === _attDate; });
    }

    if (filtered.length === 0) {
      App.Utils.showToast('No attendance records to export for this date', 'info');
      return;
    }

    var headers = ['Date','Name','Type','Class','Check In','Check Out','Status'];
    var rows = filtered.map(function(rec) {
      var name = '';
      if (rec.personType === 'staff') {
        var s = staff.find(function(x) { return x.id === rec.personId; });
        name = s ? s.fullName : rec.personId;
      } else {
        var st = students.find(function(x) { return x.id === rec.personId; });
        name = st ? (st.firstName + ' ' + st.lastName) : rec.personId;
      }
      var cls = '';
      if (rec.classId) {
        var c = classes.find(function(x) { return x.id === rec.classId; });
        cls = c ? c.name : rec.classId;
      }
      return [rec.date, name, rec.personType || '', cls, rec.checkIn || '', rec.checkOut || '', rec.status || '']
        .map(function(v) { return '"' + String(v || '').replace(/"/g, '""') + '"'; }).join(',');
    });

    _downloadCSV([headers.join(',')].concat(rows).join('\n'), 'attendance-' + _attDate + '.csv');
    App.Utils.showToast('Exported ' + filtered.length + ' attendance records', 'success');
  }

  function _renderAdminView() {
    return '<div style="display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:0.5rem;margin-bottom:0.25rem">'
      + '<div style="display:flex;gap:0.35rem;background:#f1f5f9;border-radius:10px;padding:3px;width:fit-content">'
      + ['staff','students','self-study','kiosk'].map(function(t) {
          const active = t === _attTab;
          const label  = t === 'staff' ? 'Staff' : t === 'students' ? 'Students' : t === 'kiosk' ? 'Kiosk' : 'Self Study';
          return '<button onclick="App.Attendance._setTab(\'' + t + '\')" style="'
            + 'padding:0.5rem 1.2rem;font-size:0.85rem;font-weight:700;border:none;border-radius:8px;cursor:pointer;min-height:44px;transition:all 0.15s;'
            + (active ? 'background:var(--gold);color:#0a0a0a;' : 'background:transparent;color:#94a3b8;')
            + '">' + label + '</button>';
        }).join('')
      + '</div>'
      + '<button onclick="App.Attendance._exportCSV()" style="padding:0.45rem 1rem;font-size:0.8rem;font-weight:600;border:1px solid #e2e8f0;border-radius:8px;background:#fff;color:#374151;cursor:pointer;white-space:nowrap;min-height:36px;transition:background 0.15s" onmouseover="this.style.background=\'#f8fafc\'" onmouseout="this.style.background=\'#fff\'">Export CSV</button>'
      + '</div>'
      + (_attTab === 'staff' ? _staffTab() : _attTab === 'kiosk' ? _kioskTab() : _attTab === 'self-study' ? _selfStudyTab() : _studentTab());
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
      return '<div style="background:#fff;border-radius:14px;border:1px solid var(--rule,#e2e8f0);overflow:hidden">'
        + App.Utils.emptyState('No attendance records yet',
            'Your child\'s check-ins for the selected date appear here once school marks them present.')
        + '</div>';
    }

    var pagedRecords = myRecords.slice(_attClientPage * _ATT_PAGE_SIZE, (_attClientPage + 1) * _ATT_PAGE_SIZE);

    // Card-list layout — works on all screen widths without a horizontal-scroll table
    return '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);overflow:hidden">'
      + pagedRecords.map(function(rec) {
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
      + '</div>'
      + _paginationControls(_attClientPage, myRecords.length, _ATT_PAGE_SIZE, 'App.Attendance._setClientPage');
  }

  var SCOL = {
    Present: { bg:'#f0fdf4', border:'#86efac', text:'#15803d', active:'#22c55e', activeText:'#fff' },
    Late:    { bg:'#fffbeb', border:'#fcd34d', text:'#d97706', active:'#f59e0b', activeText:'#fff' },
    Absent:  { bg:'#fef2f2', border:'#fca5a5', text:'#dc2626', active:'#ef4444', activeText:'#fff' }
  };

  function _staffTab() {
    const { staff, classes, attendance } = App.Store.get();
    const dayOfWeek = new Date(_attDate + 'T00:00:00').toLocaleDateString('en-US', { weekday: 'long' });
    // Staff with at least one class on this day
    const schedChanges = App.Store.get().scheduleChanges;
    const staffWithClass = staff.filter(function(s) {
      return classes.some(function(c) { return c.teacherIds.indexOf(s.id) > -1 && App.Utils.scheduleOn(c, schedChanges, _attDate).day === dayOfWeek; });
    });
    const displayStaff = _showAllStaff ? staff : (staffWithClass.length > 0 ? staffWithClass : staff);
    const hiddenCount = staff.length - staffWithClass.length;

    return '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);overflow:hidden">'
      + '<div style="padding:0.9rem 1rem;border-bottom:1px solid #f0ede8;background:#faf9f7;display:flex;align-items:center;gap:0.75rem;flex-wrap:wrap">'
      +   '<input type="date" value="' + _attDate + '" onchange="App.Attendance._setDate(this.value)" style="padding:0.6rem 0.9rem;font-size:0.9rem;border:1px solid #e2e8f0;border-radius:9px;outline:none;min-height:48px">'
      +   '<span style="font-size:0.82rem;color:#94a3b8">' + dayOfWeek + ' · ' + displayStaff.length + ' staff with classes</span>'
      +   (hiddenCount > 0 || _showAllStaff
          ? '<button onclick="App.Attendance._toggleAllStaff()" style="margin-left:auto;font-size:0.75rem;font-weight:600;color:var(--gold);background:none;border:none;cursor:pointer;white-space:nowrap">'
            + (_showAllStaff ? 'Show scheduled only' : 'Show all (' + staff.length + ')') + '</button>'
          : '')
      + '</div>'
      + '<div>'
      + displayStaff.map(function(s) {
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
            + (rec && App.currentRole === 'admin'
              ? '<button onclick="App.Attendance._undoStaffAttendance(\'' + rec.id + '\',\'' + s.id + '\')" style="'
                + 'min-height:48px;padding:0.4rem 0.8rem;background:none;color:#94a3b8;border:1px dashed #e2e8f0;'
                + 'border-radius:999px;font-size:0.78rem;font-weight:600;cursor:pointer" title="Remove this record as if it was never marked">Undo</button>'
              : '')
            + '</div>'
            + '</div>';
        }).join('')
      + '</div>'
      + '</div>';
  }

  function _studentTab() {
    const { classes, students, attendance } = App.Store.get();
    const dayOfWeek = new Date(_attDate + 'T00:00:00').toLocaleDateString('en-US', { weekday: 'long' });
    // A session moved away from today doesn't take attendance today; one
    // moved TO today does, even though the weekday doesn't match.
    const _mv = App.Utils.movesForDate(App.Store.get().sessionMoves, _attDate);
    const scheduledClasses = classes.filter(function(c) { return App.Utils.scheduleOn(c, App.Store.get().scheduleChanges, _attDate).day === dayOfWeek && !_mv.movedOut[c.id]; });
    _mv.movedIn.forEach(function(m) {
      var mc = classes.find(function(c) { return c.id === m.classId; });
      if (mc && scheduledClasses.indexOf(mc) === -1) scheduledClasses.push(mc);
    });
    const displayClasses = _showAllClasses ? classes : (scheduledClasses.length > 0 ? scheduledClasses : classes);

    // Auto-correct selection if current class not in display list
    if (displayClasses.length > 0 && !displayClasses.find(function(c) { return c.id === _attClassId; })) {
      _attClassId = displayClasses[0].id;
    }

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
      +     displayClasses.map(function(c) { return '<option value="' + c.id + '" ' + (c.id === _attClassId ? 'selected' : '') + '>' + App.Utils.esc(c.name) + ' — ' + App.Utils.formatTime(c.time) + '</option>'; }).join('')
      +     '</select>'
      +     (classes.length !== scheduledClasses.length || _showAllClasses
            ? '<button onclick="App.Attendance._toggleAllClasses()" style="font-size:0.75rem;font-weight:600;color:var(--gold);background:none;border:none;cursor:pointer;white-space:nowrap;min-height:48px">'
              + (_showAllClasses ? 'Scheduled only' : 'All classes') + '</button>'
            : '')
      +     '<button onclick="App.Attendance._markAllPresent()" style="padding:0.45rem 0.85rem;font-size:0.78rem;font-weight:600;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer;white-space:nowrap">Mark All Present</button>'
      +   '</div>'
      +   (selectedClass
          ? '<div style="margin-top:0.5rem;display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:0.4rem">'
          +   '<span style="font-size:0.78rem;color:#94a3b8">' + enrolledStudents.length + ' enrolled  ·  <span style="color:#15803d;font-weight:600">' + presentCount + ' checked in today</span></span>'
          +   '<button onclick="App.Attendance._quickFeedback()" style="display:inline-flex;align-items:center;gap:0.35rem;padding:0.35rem 0.75rem;font-size:0.75rem;font-weight:600;border:1px solid #e2e8f0;border-radius:8px;background:#fff;color:#64748b;cursor:pointer;white-space:nowrap;transition:background 0.15s" onmouseover="this.style.background=\'#f8fafc\'" onmouseout="this.style.background=\'#fff\'">'
          +     '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/></svg>'
          +     'Quick Note</button>'
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
        + '</div>'
        + (function() {
            var allMarked = enrolledStudents.every(function(s) {
              var rec = todayRecs.find(function(a) { return a.personId === s.id; });
              return rec && (rec.checkIn || rec.status === 'Absent');
            });
            if (allMarked && enrolledStudents.length > 0 && (App.currentRole === 'admin' || App.currentRole === 'teacher')) {
              return '<div id="post-att-prompt" style="padding:0 1rem 1rem">'
                + '<div style="margin-top:1rem;padding:1.25rem;background:linear-gradient(135deg,#fef9ec 0%,#fff 70%);border:1px solid #fef3c7;border-radius:14px">'
                +   '<div style="display:flex;align-items:center;gap:0.5rem;margin-bottom:0.65rem">'
                +     '<svg width="16" height="16" fill="none" stroke="#b08d20" stroke-width="2" viewBox="0 0 24 24"><path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z"/></svg>'
                +     '<span style="font-size:0.78rem;font-weight:700;color:#92400e">Attendance complete — add class notes?</span>'
                +   '</div>'
                +   '<textarea id="post-att-notes" rows="2" placeholder="How was the class? Any highlights?" style="width:100%;padding:0.55rem 0.75rem;font-size:0.85rem;border:1px solid #e2e8f0;border-radius:10px;resize:none;font-family:inherit;outline:none"></textarea>'
                +   '<div style="display:flex;gap:0.5rem;margin-top:0.5rem">'
                +     '<button onclick="App.Attendance._savePostAttFeedback()" style="padding:0.4rem 0.85rem;font-size:0.78rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Save Note</button>'
                +     '<button onclick="document.getElementById(\'post-att-prompt\').style.display=\'none\'" style="padding:0.4rem 0.85rem;font-size:0.78rem;border:1px solid #e2e8f0;border-radius:8px;background:#fff;color:#64748b;cursor:pointer">Skip</button>'
                +   '</div>'
                + '</div>'
                + '</div>';
            }
            return '';
          })())
      + '</div>';
  }

  // ─── Kiosk mode ───────────────────────────────────────────────────────────
  var _kioskLastScan = null;
  var _kioskClassId = '';

  function _kioskTab() {
    const { classes } = App.Store.get();
    if (!_kioskClassId && classes.length) _kioskClassId = classes[0].id;

    const classOpts = classes.map(function(c) {
      return '<option value="' + c.id + '"' + (c.id === _kioskClassId ? ' selected' : '') + '>'
        + App.Utils.esc(c.name) + ' — ' + App.Utils.formatTime(c.time)
        + '</option>';
    }).join('');

    var feedHtml = '<div id="kiosk-feedback" style="'
      + 'margin-top:1.5rem;padding:1.5rem;border-radius:16px;text-align:center;'
      + 'transition:opacity 0.3s;display:none'
      + '"></div>';

    return '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);overflow:hidden">'
      + '<div style="padding:1rem;border-bottom:1px solid #f0ede8;background:#faf9f7;display:flex;align-items:center;gap:0.75rem;flex-wrap:wrap">'
      +   '<span style="font-size:0.78rem;font-weight:700;color:#94a3b8">CLASS</span>'
      +   '<select onchange="App.Attendance._setKioskClass(this.value)" style="padding:0.5rem 0.8rem;font-size:0.9rem;border:1px solid #e2e8f0;border-radius:9px;outline:none;background:#fff;flex:1;min-width:180px">' + classOpts + '</select>'
      +   '<span style="font-size:0.78rem;color:#94a3b8">Date: ' + App.Utils.formatDate(_attDate) + '</span>'
      + '</div>'
      + '<div style="padding:2rem;text-align:center">'
      +   '<div style="font-size:1rem;font-weight:700;color:#0d0d0d;margin-bottom:0.35rem">Scan Student ID</div>'
      +   '<div style="font-size:0.82rem;color:#94a3b8;margin-bottom:1.25rem">Use a barcode scanner or type the student ID and press Enter</div>'
      +   '<div style="display:flex;gap:0.6rem;max-width:360px;margin:0 auto">'
      +     '<input id="kiosk-input" type="text" placeholder="e.g. STU001" '
      +       'onkeydown="if(event.key===\'Enter\'){ App.Attendance._kioskScan(this.value); this.value=\'\'; }" '
      +       'style="flex:1;padding:0.85rem 1rem;font-size:1.1rem;border:2px solid #e2e8f0;border-radius:12px;outline:none;text-align:center;letter-spacing:0.08em;font-weight:700" '
      +       'autofocus>'
      +     '<button onclick="var el=document.getElementById(\'kiosk-input\');App.Attendance._kioskScan(el.value);el.value=\'\';" '
      +       'style="padding:0.85rem 1.1rem;background:var(--gold);color:#0a0a0a;border:none;border-radius:12px;font-size:0.9rem;font-weight:700;cursor:pointer">Check</button>'
      +   '</div>'
      + feedHtml
      + '</div>'
      + '</div>';
  }

  function _kioskShowFeedback(scan) {
    var fb = document.getElementById('kiosk-feedback');
    if (!fb) return;
    var ok = scan.ok;
    fb.style.opacity = '1';
    fb.innerHTML = '<div style="font-size:2rem;margin-bottom:0.5rem">' + (ok ? (scan.action === 'in' ? '✅' : '👋') : '❌') + '</div>'
      + '<div style="font-size:1.3rem;font-weight:800;color:' + (ok ? '#15803d' : '#dc2626') + '">' + App.Utils.esc(scan.name) + '</div>'
      + '<div style="font-size:0.9rem;color:#64748b;margin-top:0.3rem">' + App.Utils.esc(scan.msg || '') + '</div>';
    fb.style.background = ok ? '#f0fdf4' : '#fef2f2';
    fb.style.border = '2px solid ' + (ok ? '#86efac' : '#fca5a5');
    fb.style.display = '';
  }

  function _kioskScan(rawId) {
    const id = (rawId || '').trim().toUpperCase();
    if (!id) return;
    var input = document.getElementById('kiosk-input');
    if (input) { input.value = ''; input.focus(); }

    const state = App.Store.get();
    const stu = state.students.find(function(s) { return s.id === id; });
    const now = App.Utils.nowTime();
    const today = _attDate || App.Utils.today();

    if (!stu) {
      _kioskLastScan = { ok: false, name: id, msg: 'Student not found', action: null };
      _kioskShowFeedback(_kioskLastScan);
      setTimeout(function() { _kioskClearFeedback(id); }, 4000);
      return;
    }

    // Check if enrolled in selected class
    if (_kioskClassId && (stu.enrolledClasses || []).indexOf(_kioskClassId) === -1) {
      _kioskLastScan = { ok: false, name: stu.firstName + ' ' + stu.lastName, msg: 'Not enrolled in this class', action: null };
      _kioskShowFeedback(_kioskLastScan);
      setTimeout(function() { _kioskClearFeedback(stu.firstName + ' ' + stu.lastName); }, 4000);
      return;
    }

    const existingIdx = state.attendance.findIndex(function(a) {
      return a.personId === id && a.classId === _kioskClassId && a.date === today;
    });
    let newAtt = state.attendance.slice();
    let action;
    let existingCheckIn = null;
    if (existingIdx === -1) {
      newAtt.push({ id: App.Utils.generateId('ATT'), personId: id, personType: 'student', date: today, classId: _kioskClassId, checkIn: now, checkOut: null, status: 'Present' });
      action = 'in';
    } else if (!newAtt[existingIdx].checkOut) {
      existingCheckIn = newAtt[existingIdx].checkIn || null;
      newAtt[existingIdx] = Object.assign({}, newAtt[existingIdx], { checkOut: now });
      action = 'out';
    } else {
      _kioskLastScan = { ok: false, name: stu.firstName + ' ' + stu.lastName, msg: 'Already checked in and out today', action: null };
      _kioskShowFeedback(_kioskLastScan);
      setTimeout(function() { _kioskClearFeedback(stu.firstName + ' ' + stu.lastName); }, 4000);
      return;
    }

    var prevAtt = state.attendance;
    App.Store.set({ attendance: newAtt });
    // Persist to backend so the parent's device gets the WebSocket
    // notification. Optimistic UI above keeps the kiosk feedback instant.
    const apiBody = {
      personId: id,
      personType: 'student',
      date: today,
      classId: _kioskClassId,
      status: 'Present'
    };
    if (action === 'in') {
      apiBody.checkIn = now;
    } else {
      apiBody.checkIn = existingCheckIn;
      apiBody.checkOut = now;
    }
    App.Api.post('/api/attendance', apiBody, { silent: true }).catch(function(err) {
      // Roll the optimistic row back — leaving it showed the person as
      // present even though the server rejected the write.
      App.Store.set({ attendance: prevAtt });
      App.Utils.showToast('Scan failed to save: ' + (err && err.message ? err.message : 'server error'), 'error');
      App.Router.refresh();
    });
    _kioskLastScan = {
      ok: true,
      name: stu.firstName + ' ' + stu.lastName,
      msg: action === 'in' ? ('Checked in at ' + App.Utils.formatTime(now)) : ('Checked out at ' + App.Utils.formatTime(now)),
      action: action
    };

    _kioskShowFeedback(_kioskLastScan);

    // Auto-clear feedback after 4 seconds, keep focus
    var scanName = stu.firstName + ' ' + stu.lastName;
    setTimeout(function() { _kioskClearFeedback(scanName); }, 4000);

    // Broadcast parent notification on check-out
    if (action === 'out') {
      try {
        var ch = new BroadcastChannel('studyhub_notifs');
        ch.postMessage({ type: 'CHECK_OUT', student: stu.firstName + ' ' + stu.lastName, time: now, parent: stu.contact });
        ch.close();
      } catch(e) {}
    }

    // Re-focus input (no full page refresh)
    if (input) input.focus();
  }

  function _kioskClearFeedback(name) {
    if (_kioskLastScan && _kioskLastScan.name === name) {
      _kioskLastScan = null;
      var fb = document.getElementById('kiosk-feedback');
      if (fb) { fb.style.opacity = '0'; fb.innerHTML = ''; }
    }
    var ki = document.getElementById('kiosk-input');
    if (ki) ki.focus();
  }

  function _setKioskClass(classId) { _kioskClassId = classId; App.Router.refresh(); setTimeout(function(){ var ki = document.getElementById('kiosk-input'); if (ki) ki.focus(); }, 50); }

  // ─── Self Study Membership ────────────────────────────────────────────────
  // Students enrolled in any class get 4 free hours of self study per month.
  // Extra hours are billed at RM10/hr (RM40 = 4hr equivalent).
  var SELF_STUDY_RATE   = 10;    // RM per hour
  // Free hours come from the student's package (default 4): the cron bills
  // overflow per student, so a flat constant here showed wrong numbers for
  // anyone on a non-default package.
  var _freeHours = function(s) { return s.packageSelfStudyHours == null ? 4 : s.packageSelfStudyHours; };

  function _selfStudyTab() {
    const state = App.Store.get();
    const students = state.students || [];
    const sessions = state.selfStudySessions || [];

    // Only students enrolled in at least one class
    const eligible = students.filter(function(s) {
      return (s.enrolledClasses || []).length > 0 && (s.status === 'Active' || s.status === 'New');
    });

    const now  = new Date();
    const thisMonth = now.getFullYear() + '-' + String(now.getMonth() + 1).padStart(2, '0');

    const rows = eligible.map(function(s) {
      const myMonth = sessions.filter(function(ss) {
        return ss.studentId === s.id && ss.date.startsWith(thisMonth);
      });
      const usedMin  = myMonth.reduce(function(acc, ss) { return acc + (ss.durationMin || 0); }, 0);
      const usedHr   = usedMin / 60;
      const freeH    = _freeHours(s);
      const freeRem  = Math.max(0, freeH - usedHr);
      const billable = Math.max(0, usedHr - freeH);
      const pct      = freeH > 0 ? Math.min(100, (usedHr / freeH) * 100) : 100;
      const barCol   = pct >= 100 ? '#ef4444' : pct >= 75 ? '#f59e0b' : '#22c55e';

      return '<div style="' + ROW_STYLE + '">'
        + '<div style="display:flex;align-items:center;gap:0.85rem;flex:1 1 auto;min-width:0">'
        +   '<div style="' + AVATAR_STYLE + ';background:#f0fdf4;color:#15803d">' + (s.firstName||'?').charAt(0) + (s.lastName||'').charAt(0) + '</div>'
        +   '<div style="min-width:0;flex:1">'
        +     '<div style="' + NAME_STYLE + '">' + App.Utils.esc(s.firstName + ' ' + s.lastName) + '</div>'
        +     '<div style="margin-top:5px;height:6px;background:#f1f5f9;border-radius:99px;width:140px;overflow:hidden">'
        +       '<div style="height:100%;width:' + pct + '%;background:' + barCol + ';border-radius:99px;transition:width 0.3s"></div>'
        +     '</div>'
        +     '<div style="' + TIME_STYLE + ';margin-top:3px">'
        +       (usedMin > 0 ? (usedHr < 1 ? usedMin + 'min' : usedHr.toFixed(1) + 'hr') + ' used · ' : '')
        +       freeRem.toFixed(1) + 'hr free remaining'
        +       (billable > 0 ? ' · <span style="color:#dc2626;font-weight:700">+' + billable.toFixed(1) + 'hr billable (RM' + (billable * SELF_STUDY_RATE).toFixed(0) + ')</span>' : '')
        +     '</div>'
        +   '</div>'
        + '</div>'
        + '<div style="flex:0 0 auto;align-self:center">'
        +   '<button onclick="App.Attendance._logSelfStudy(\'' + s.id + '\')" style="'
        +     'min-height:46px;padding:0.5rem 1rem;background:var(--gold-dim);color:var(--gold);border:1px solid rgba(201,162,39,0.3);'
        +     'border-radius:10px;font-size:0.82rem;font-weight:700;cursor:pointer">Log Session</button>'
        + '</div>'
        + '</div>';
    }).join('');

    // Month total summary
    const monthTotal = eligible.reduce(function(acc, s) {
      const myMin = sessions.filter(function(ss) { return ss.studentId === s.id && ss.date.startsWith(thisMonth); })
        .reduce(function(a, ss) { return a + (ss.durationMin || 0); }, 0);
      return acc + myMin;
    }, 0);
    const billableTotal = eligible.reduce(function(acc, s) {
      const myHr = sessions.filter(function(ss) { return ss.studentId === s.id && ss.date.startsWith(thisMonth); })
        .reduce(function(a, ss) { return a + (ss.durationMin || 0); }, 0) / 60;
      return acc + Math.max(0, myHr - _freeHours(s));
    }, 0);

    return '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);overflow:hidden">'
      + '<div style="padding:0.9rem 1rem;border-bottom:1px solid #f0ede8;background:#faf9f7;display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:0.5rem">'
      +   '<div>'
      +     '<span style="font-size:0.9rem;font-weight:700;color:#0d0d0d">Self Study Membership</span>'
      +     '<span style="font-size:0.78rem;color:#94a3b8;margin-left:0.6rem">' + eligible.length + ' enrolled students</span>'
      +   '</div>'
      +   '<div style="display:flex;gap:1.5rem">'
      +     '<div style="text-align:center"><div style="font-size:0.65rem;font-weight:700;color:#94a3b8;text-transform:uppercase">This month</div>'
      +       '<div style="font-size:1rem;font-weight:800;color:#0d0d0d">' + (monthTotal / 60).toFixed(1) + 'hr total</div></div>'
      +     (billableTotal > 0
            ? '<div style="text-align:center"><div style="font-size:0.65rem;font-weight:700;color:#dc2626;text-transform:uppercase">Billable</div>'
            +   '<div style="font-size:1rem;font-weight:800;color:#dc2626">RM' + (billableTotal * SELF_STUDY_RATE).toFixed(0) + '</div></div>'
            : '<div style="text-align:center"><div style="font-size:0.65rem;font-weight:700;color:#94a3b8;text-transform:uppercase">Value given</div>'
            +   '<div style="font-size:1rem;font-weight:800;color:#15803d">RM' + (Math.min(eligible.reduce(function(a, s) { return a + _freeHours(s); }, 0), monthTotal / 60) * SELF_STUDY_RATE) + '</div></div>')
      +   '</div>'
      + '</div>'
      + '<div style="padding:0.6rem 1rem;background:#fffbeb;border-bottom:1px solid #fef3c7;font-size:0.78rem;color:#92400e">'
      +   'Enrolled students get <strong>4 free hours/month</strong> of self study (equivalent to <strong>RM40 off</strong>). Extra hours are billed at RM10/hr.'
      + '</div>'
      + (eligible.length === 0
          ? '<div style="padding:3rem;text-align:center;color:#94a3b8;font-size:0.9rem">No enrolled students</div>'
          : '<div>' + rows + '</div>')
      + '</div>';
  }

  function _logSelfStudy(studentId) {
    const state = App.Store.get();
    const stu = state.students.find(function(s) { return s.id === studentId; });
    if (!stu) return;

    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-1">Log Self Study</h2>'
      + '<p class="text-sm text-slate-500 mb-4">' + App.Utils.esc(stu.firstName + ' ' + stu.lastName) + '</p>'
      + '<form id="ss-form" class="space-y-4">'
      + '<div class="grid grid-cols-2 gap-4">'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Date</label>'
      + '<input name="date" type="date" value="' + App.Utils.today() + '" class="form-input" required></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Duration (minutes)</label>'
      + '<input name="duration" type="number" min="15" max="480" step="15" value="60" class="form-input" required></div>'
      + '</div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Notes <span class="text-xs text-slate-400 font-normal">(optional)</span></label>'
      + '<input name="notes" class="form-input" placeholder="e.g. Worked on kanji revision" maxlength="200"></div>'
      + '<div style="background:#fffbeb;border:1px solid #fef3c7;border-radius:10px;padding:0.75rem;font-size:0.82rem;color:#92400e">'
      + 'Free allowance: per student package (default 4hr/month). Extra hours billed at RM' + SELF_STUDY_RATE + '/hr.'
      + '</div>'
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Save</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );

    document.getElementById('ss-form').addEventListener('submit', function(e) {
      e.preventDefault();
      const fd = new FormData(e.target);
      const duration = parseInt(fd.get('duration')) || 60;
      const newSession = {
        id: App.Utils.generateId('SS'),
        studentId: studentId,
        date: fd.get('date'),
        durationMin: duration,
        notes: (fd.get('notes') || '').trim()
      };
      App.Utils.hideModal(true);
      App.Api.post('/api/self-study', newSession).then(function(result) {
        var existing = App.Store.get().selfStudySessions || [];
        App.Store.set({ selfStudySessions: [result || newSession].concat(existing) });
        App.Utils.showToast('Session logged: ' + (duration >= 60 ? (duration / 60).toFixed(1) + 'hr' : duration + 'min'), 'success');
        App.Router.refresh();
      });
    });
  }

  function _setClientPage(n) { _attClientPage = Math.max(0, n); App.Router.refresh(); }
  function _setTab(tab) { _attTab = tab; App.Router.refresh(); }
  function _setDate(date) {
    _attDate = date;
    // Auto-select first class scheduled on the new day
    if (!_showAllClasses) {
      const { classes, scheduleChanges } = App.Store.get();
      const dayOfWeek = new Date(date + 'T00:00:00').toLocaleDateString('en-US', { weekday: 'long' });
      const first = classes.find(function(c) { return App.Utils.scheduleOn(c, scheduleChanges, date).day === dayOfWeek; });
      if (first) _attClassId = first.id;
    }
    App.Router.refresh();
  }
  function _setClass(classId) { _attClassId = classId; App.Router.refresh(); }
  function _toggleAllStaff() { _showAllStaff = !_showAllStaff; App.Router.refresh(); }
  function _toggleAllClasses() { _showAllClasses = !_showAllClasses; App.Router.refresh(); }

  function _markStaff(staffId, status) {
    const state = App.Store.get();
    const existingIdx = state.attendance.findIndex(function(a) { return a.personId === staffId && a.personType === 'staff' && a.date === _attDate; });
    const now = App.Utils.nowTime();
    var prevAtt = state.attendance;
    let newAtt = state.attendance.slice();
    let rec;
    if (existingIdx > -1) {
      rec = { ...newAtt[existingIdx], status: status, checkIn: status !== 'Absent' ? (newAtt[existingIdx].checkIn || now) : null };
      newAtt[existingIdx] = rec;
    } else {
      rec = { id: App.Utils.generateId('ATT'), personId: staffId, personType: 'staff', date: _attDate, checkIn: status !== 'Absent' ? now : null, checkOut: null, status: status };
      newAtt.push(rec);
    }
    App.Store.set({ attendance: newAtt });

    // Persist — without the POST the mark lives only in this tab: gone on
    // the next snapshot, never counted in payroll, and Undo 404s.
    App.Api.post('/api/attendance', {
      id: rec.id, personId: staffId, personType: 'staff', date: _attDate,
      // Echo both times: the backend upsert overwrites what is not sent.
      checkIn: rec.checkIn, checkOut: rec.checkOut || null, status: status
    }, { silent: true }).then(function(saved) {
      if (!saved || !saved.id || saved.id === rec.id) return;
      // The upsert matched a row this tab didn't know (e.g. a teacher
      // self check-in) — adopt the server id so Undo deletes the real row.
      const att = App.Store.get().attendance.map(function(a) { return a.id === rec.id ? { ...a, id: saved.id } : a; });
      App.Store.set({ attendance: att });
      App.Router.refresh();
    }).catch(function(err) {
      App.Store.set({ attendance: prevAtt });
      App.Utils.showToast('Attendance failed to save: ' + (err && err.message ? err.message : 'server error'), 'error');
      App.Router.refresh();
    });

    // If marked Absent and user is admin, check for classes on this day and offer to cancel
    if (status === 'Absent' && App.currentRole === 'admin') {
      const state2 = App.Store.get();
      const dateDay = new Date(_attDate + 'T00:00:00').toLocaleDateString('en-US', { weekday: 'long' });
      const staffMember = state2.staff.find(function(s) { return s.id === staffId; });
      // A session moved off this date can't be cancelled here (the backend
      // rejects cancel-with-live-move), so don't offer it.
      const mvOut = App.Utils.movesForDate(state2.sessionMoves, _attDate).movedOut;
      const affectedClasses = state2.classes.filter(function(c) { return c.teacherIds.indexOf(staffId) > -1 && App.Utils.scheduleOn(c, state2.scheduleChanges, _attDate).day === dateDay && !mvOut[c.id]; });
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
      + '<button onclick="App.Attendance._doCancelClasses(\'' + ids.join(',') + '\')" style="flex:1;padding:0.5rem;font-size:0.85rem;font-weight:700;background:#ef4444;color:#fff;border:none;border-radius:8px;cursor:pointer">Cancel &amp; Notify Parents</button>'
      + '</div>'
      + '</div>'
    );
  }

  function _doCancelClasses(classIds) {
    if (typeof classIds === 'string') classIds = classIds.split(',').filter(Boolean);
    const state = App.Store.get();

    // Only the cancellation is posted: the backend cancellation handler
    // creates the class-scoped parent announcement and the replacement
    // credits itself. Posting an announcement here too sent parents two
    // notices for one cancellation.
    var apiCalls = [];
    classIds.forEach(function(classId) {
      var cancellation = { id: App.Utils.generateId('cancel'), classId: classId, date: _attDate, reason: 'Teacher absent' };
      apiCalls.push(App.Api.post('/api/cancelled-classes', cancellation));
    });

    App.Utils.hideModal(true);

    // No per-call .catch: a failure rejects Promise.all and hits the outer
    // catch, so we never falsely claim parents were notified.
    Promise.all(apiCalls).then(function() {
      return App.Api.loadSnapshot();
    }).then(function() {
      if (App.Notifs && App.Notifs.refresh) App.Notifs.refresh();
      App.Router.refresh();
      App.Utils.showToast('Class cancelled. Parents have been notified.', 'success');
    }).catch(function() {
      App.Utils.showToast('Could not cancel class \u2014 please retry', 'error');
    });
  }

  function _checkInStudent(studentId) {
    const state = App.Store.get();
    const now = App.Utils.nowTime();
    const existing = state.attendance.find(function(a) { return a.personId === studentId && a.classId === _attClassId && a.date === _attDate; });
    // Optimistic UI update so the table flips immediately. The POST below
    // is what actually persists the row and triggers the WebSocket
    // broadcast that notifies the parent's device.
    let newAtt = state.attendance.slice();
    if (!existing) {
      newAtt.push({ id: App.Utils.generateId('ATT'), personId: studentId, personType: 'student', date: _attDate, classId: _attClassId, checkIn: now, checkOut: null, status: 'Present' });
    }
    var prevAtt = state.attendance;
    App.Store.set({ attendance: newAtt });
    const stu = state.students.find(function(s) { return s.id === studentId; });
    const stuName = stu ? stu.firstName + ' ' + stu.lastName : studentId;
    App.Utils.showToast(stuName + ' checked in at ' + App.Utils.formatTime(now), 'info');
    App.Router.refresh();
    App.Api.post('/api/attendance', {
      personId: studentId,
      personType: 'student',
      date: _attDate,
      classId: _attClassId,
      checkIn: now,
      status: 'Present'
    }, { silent: true }).catch(function(err) {
      // Roll the optimistic row back — leaving it showed the person as
      // present even though the server rejected the write.
      App.Store.set({ attendance: prevAtt });
      App.Utils.showToast('Check-in failed to save: ' + (err && err.message ? err.message : 'server error'), 'error');
      App.Router.refresh();
    });
  }

  function _checkOutStudent(studentId) {
    const state = App.Store.get();
    const now = App.Utils.nowTime();
    const existing = state.attendance.find(function(a) {
      return a.personId === studentId && a.classId === _attClassId && a.date === _attDate;
    });
    // Backend upsert overwrites check_in to whatever we send — pass the
    // existing check-in time through so check-out doesn't blank it.
    const existingCheckIn = existing && existing.checkIn ? existing.checkIn : null;
    const newAtt = state.attendance.map(function(a) {
      return (a.personId === studentId && a.classId === _attClassId && a.date === _attDate) ? Object.assign({}, a, { checkOut: now }) : a;
    });
    var prevAtt = state.attendance;
    App.Store.set({ attendance: newAtt });
    const stu = state.students.find(function(s) { return s.id === studentId; });
    const stuName = stu ? stu.firstName + ' ' + stu.lastName : studentId;
    App.Utils.showToast(stuName + ' checked out at ' + App.Utils.formatTime(now), 'success');
    App.Router.refresh();
    App.Api.post('/api/attendance', {
      personId: studentId,
      personType: 'student',
      date: _attDate,
      classId: _attClassId,
      checkIn: existingCheckIn,
      checkOut: now,
      status: 'Present'
    }, { silent: true }).catch(function(err) {
      // Roll the optimistic row back — leaving it showed the person as
      // present even though the server rejected the write.
      App.Store.set({ attendance: prevAtt });
      App.Utils.showToast('Check-out failed to save: ' + (err && err.message ? err.message : 'server error'), 'error');
      App.Router.refresh();
    });
  }

  function _renderTeacherSelfCheckIn() {
    var state = App.Store.get();
    var staff = state.staff || [];
    var attendance = state.attendance || [];
    var today = _attDate || App.Utils.today();
    var teacher = staff.find(function(s) { return s.id === App.currentTeacher; });
    var teacherName = teacher ? (teacher.fullName || teacher.name || 'Teacher') : 'Teacher';
    var rec = attendance.find(function(a) {
      return a.personId === App.currentTeacher && a.personType === 'staff' && a.date === today;
    });
    var checkedIn = rec && rec.checkIn;
    var checkedOut = rec && rec.checkOut;
    var statusText = checkedOut
      ? 'Checked out at ' + App.Utils.formatTime(rec.checkOut)
      : checkedIn
        ? 'Checked in at ' + App.Utils.formatTime(rec.checkIn)
        : 'Not checked in';
    var bgColor = checkedIn && !checkedOut ? '#f0fdf4' : '#fff';
    var borderColor = checkedIn && !checkedOut ? '#86efac' : 'rgba(0,0,0,0.07)';
    var statusColor = checkedOut ? '#15803d' : checkedIn ? '#15803d' : '#94a3b8';
    var btnHtml;
    if (!checkedIn) {
      btnHtml = '<button onclick="App.Attendance._teacherCheckIn()" style="'
        + 'min-height:48px;padding:0.6rem 1.4rem;background:#22c55e;color:#fff;border:none;'
        + 'border-radius:12px;font-size:0.9rem;font-weight:700;cursor:pointer;transition:opacity 0.15s" '
        + 'onmouseover="this.style.opacity=\'0.85\'" onmouseout="this.style.opacity=\'1\'">Check In</button>';
    } else if (!checkedOut) {
      btnHtml = '<button onclick="App.Attendance._teacherCheckOut()" style="'
        + 'min-height:48px;padding:0.6rem 1.4rem;background:#64748b;color:#fff;border:none;'
        + 'border-radius:12px;font-size:0.9rem;font-weight:700;cursor:pointer;transition:opacity 0.15s" '
        + 'onmouseover="this.style.opacity=\'0.85\'" onmouseout="this.style.opacity=\'1\'">Check Out</button>';
    } else {
      btnHtml = '<div style="display:flex;align-items:center;gap:0.4rem;padding:0.5rem 1rem;'
        + 'background:#dcfce7;border-radius:12px">'
        + '<span style="font-size:0.9rem;font-weight:700;color:#15803d">Done for today</span></div>';
    }

    return '<div style="background:' + bgColor + ';border-radius:14px;border:2px solid ' + borderColor + ';padding:1rem 1.25rem;margin-bottom:1rem;display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:0.75rem">'
      + '<div style="display:flex;align-items:center;gap:0.85rem">'
      +   '<div style="' + AVATAR_STYLE + ';background:rgba(139,92,246,0.12);color:#7c3aed;border:1px solid rgba(139,92,246,0.2)">' + App.Utils.esc(teacherName.charAt(0)) + '</div>'
      +   '<div>'
      +     '<div style="' + NAME_STYLE + '">' + App.Utils.esc(teacherName) + '</div>'
      +     '<div style="font-size:0.82rem;color:' + statusColor + ';margin-top:2px">' + statusText + '</div>'
      +   '</div>'
      + '</div>'
      + '<div>' + btnHtml + '</div>'
      + '</div>';
  }

  function _teacherCheckIn() {
    var state = App.Store.get();
    var today = _attDate || App.Utils.today();
    var now = App.Utils.nowTime();
    var newAtt = state.attendance.slice();
    newAtt.push({
      id: App.Utils.generateId('ATT'),
      personId: App.currentTeacher,
      personType: 'staff',
      date: today,
      checkIn: now,
      checkOut: null,
      status: 'Present'
    });
    var prevAtt = state.attendance;
    App.Store.set({ attendance: newAtt });
    App.Utils.showToast('Checked in at ' + App.Utils.formatTime(now), 'success');
    App.Router.refresh();
    App.Api.post('/api/attendance', {
      personId: App.currentTeacher,
      personType: 'staff',
      date: today,
      checkIn: now,
      status: 'Present'
    }, { silent: true }).catch(function(err) {
      // Roll the optimistic row back — leaving it showed the person as
      // present even though the server rejected the write.
      App.Store.set({ attendance: prevAtt });
      App.Utils.showToast('Check-in failed to save: ' + (err && err.message ? err.message : 'server error'), 'error');
      App.Router.refresh();
    });
  }

  function _teacherCheckOut() {
    var state = App.Store.get();
    var today = _attDate || App.Utils.today();
    var now = App.Utils.nowTime();
    var existing = state.attendance.find(function(a) {
      return a.personId === App.currentTeacher && a.personType === 'staff' && a.date === today && !a.checkOut;
    });
    var existingCheckIn = existing && existing.checkIn ? existing.checkIn : null;
    var newAtt = state.attendance.map(function(a) {
      if (a.personId === App.currentTeacher && a.personType === 'staff' && a.date === today && !a.checkOut) {
        return Object.assign({}, a, { checkOut: now });
      }
      return a;
    });
    var prevAtt = state.attendance;
    App.Store.set({ attendance: newAtt });
    App.Utils.showToast('Checked out at ' + App.Utils.formatTime(now), 'info');
    App.Router.refresh();
    App.Api.post('/api/attendance', {
      personId: App.currentTeacher,
      personType: 'staff',
      date: today,
      checkIn: existingCheckIn,
      checkOut: now,
      status: 'Present'
    }, { silent: true }).catch(function(err) {
      // Roll the optimistic row back — leaving it showed the person as
      // present even though the server rejected the write.
      App.Store.set({ attendance: prevAtt });
      App.Utils.showToast('Check-out failed to save: ' + (err && err.message ? err.message : 'server error'), 'error');
      App.Router.refresh();
    });
  }

  function _renderTeacherView() {
    const { classes } = App.Store.get();
    // Get teacher's classes
    const myClasses = classes.filter(function(c) { return c.teacherIds.indexOf(App.currentTeacher) > -1; });

    // Check for pre-selected class from dashboard
    if (App._preselectedClass) {
      var pre = myClasses.find(function(c) { return c.id === App._preselectedClass; });
      if (pre) _attClassId = pre.id;
      App._preselectedClass = null;
    }

    // Auto-select first class if current selection isn't theirs
    if (!myClasses.find(function(c) { return c.id === _attClassId; }) && myClasses.length > 0) {
      _attClassId = myClasses[0].id;
    }

    return _renderTeacherSelfCheckIn()
      + '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);margin-bottom:1rem;overflow:hidden">'
      + '<div style="padding:0.75rem 1.1rem;border-bottom:1px solid #f0ede8;background:rgba(139,92,246,0.05)">'
      +   '<span style="font-size:0.78rem;font-weight:700;color:#7c3aed">MY CLASSES — Student Attendance</span>'
      + '</div>'
      + _studentTabFiltered(myClasses)
      + '</div>';
  }

  function _studentTabFiltered(myClasses) {
    const { students, attendance } = App.Store.get();
    if (myClasses.length === 0) {
      return '<div style="background:#fff;border-radius:14px;border:1px solid var(--rule,#e2e8f0);overflow:hidden">'
        + App.Utils.emptyState('No classes assigned to you yet',
            'Once admin assigns you to a class, the attendance roster shows here.')
        + '</div>';
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
      +     myClasses.map(function(c) { return '<option value="' + c.id + '" ' + (c.id === selectedClass.id ? 'selected' : '') + '>' + App.Utils.esc(c.name) + ' — ' + c.day + ' ' + App.Utils.formatTime(c.time) + '</option>'; }).join('')
      +     '</select>'
      +     '<button onclick="App.Attendance._checkAllIn()" style="padding:0.5rem 0.9rem;font-size:0.82rem;font-weight:700;background:#22c55e;color:#fff;border:none;border-radius:9px;cursor:pointer;min-height:48px;white-space:nowrap;transition:opacity 0.15s" onmouseover="this.style.opacity=\'0.85\'" onmouseout="this.style.opacity=\'1\'">Check All In</button>'
      +   '</div>'
      +   '<div style="margin-top:0.5rem;display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:0.4rem">'
      +     '<span style="font-size:0.78rem;color:#94a3b8">' + enrolledStudents.length + ' enrolled  ·  <span style="color:#15803d;font-weight:600">' + presentCount + ' checked in</span></span>'
      +     '<button onclick="App.Attendance._quickFeedback()" style="display:inline-flex;align-items:center;gap:0.35rem;padding:0.35rem 0.75rem;font-size:0.75rem;font-weight:600;border:1px solid #e2e8f0;border-radius:8px;background:#fff;color:#64748b;cursor:pointer;white-space:nowrap;transition:background 0.15s" onmouseover="this.style.background=\'#f8fafc\'" onmouseout="this.style.background=\'#fff\'">'
      +       '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/></svg>'
      +       'Quick Note</button>'
      +   '</div>'
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

  function _checkAllIn() {
    var state = App.Store.get();
    var selectedClass = state.classes.find(function(c) { return c.id === _attClassId; });
    if (!selectedClass) return;
    var enrolledStudents = state.students.filter(function(s) { return s.enrolledClasses.indexOf(_attClassId) > -1; });
    var now = App.Utils.nowTime();
    var today = _attDate || App.Utils.today();
    var newAtt = state.attendance.slice();
    var toCheckIn = [];
    enrolledStudents.forEach(function(s) {
      var existing = newAtt.find(function(a) { return a.personId === s.id && a.classId === _attClassId && a.date === today; });
      if (!existing) {
        newAtt.push({ id: App.Utils.generateId('ATT'), personId: s.id, personType: 'student', date: today, classId: _attClassId, checkIn: now, checkOut: null, status: 'Present' });
        toCheckIn.push(s.id);
      }
    });
    if (toCheckIn.length === 0) {
      App.Utils.showToast('All students already checked in', 'info');
      return;
    }
    App.Store.set({ attendance: newAtt });
    App.Utils.showToast(toCheckIn.length + ' student' + (toCheckIn.length !== 1 ? 's' : '') + ' checked in', 'success');
    App.Router.refresh();
    // Persist each row so the bulk check-in survives a reload AND fires the
    // parent notifications (push/email/in-app) — the optimistic store update
    // above does neither on its own. Independent POSTs run together.
    Promise.all(toCheckIn.map(function(id) {
      return App.Api.post('/api/attendance', { personId: id, personType: 'student', date: today, classId: _attClassId, checkIn: now, status: 'Present' }, { silent: true });
    })).catch(function() {
      App.Utils.showToast('Some check-ins did not sync — please retry', 'warning');
    });
  }

  async function _undoAttendance(recId, studentId, wasAbsent) {
    var stu = (App.Store.get().students || []).find(function(x) { return x.id === studentId; });
    var name = stu ? stu.firstName + ' ' + stu.lastName : 'this student';
    var ok = await App.Utils.showConfirm({
      title: 'Undo attendance?',
      message: wasAbsent
        ? 'Removes the absence for ' + name + '. Any replacement credits already granted stay on the account; adjust them from the student profile if needed.'
        : 'Removes the check-in record for ' + name + ' as if it was never marked.',
      confirmLabel: 'Undo', danger: true
    });
    if (!ok) return;
    try {
      await App.Api.del('/api/attendance/' + recId);
      await App.Api.refresh();
      App.Utils.showToast('Attendance record removed', 'success');
    } catch (e) { /* App.Api already toasted */ }
  }

  async function _undoStaffAttendance(recId, staffId) {
    var st = (App.Store.get().staff || []).find(function(x) { return x.id === staffId; });
    var name = st ? st.fullName : 'this staff member';
    var ok = await App.Utils.showConfirm({
      title: 'Undo attendance?',
      message: 'Removes the record for ' + name + ' as if it was never marked.',
      confirmLabel: 'Undo', danger: true
    });
    if (!ok) return;
    try {
      await App.Api.del('/api/attendance/' + recId);
      await App.Api.refresh();
      App.Utils.showToast('Attendance record removed', 'success');
    } catch (e) { /* App.Api already toasted */ }
  }

  async function _markAbsentNoCredit(studentId) {
    var lockKey = 'noc|' + studentId + '|' + _attClassId + '|' + _attDate;
    if (_absenceLock[lockKey]) return;
    _absenceLock[lockKey] = true;
    setTimeout(function() { delete _absenceLock[lockKey]; }, 1500);

    var state = App.Store.get();
    var stu = state.students.find(function(s) { return s.id === studentId; });
    var stuName = stu ? stu.firstName + ' ' + stu.lastName : studentId;

    var newAtt = state.attendance.slice();
    var existing = newAtt.findIndex(function(a) { return a.personId === studentId && a.classId === _attClassId && a.date === _attDate; });
    if (existing > -1) {
      newAtt[existing] = Object.assign({}, newAtt[existing], { status: 'Absent', checkIn: null, checkOut: null });
    } else {
      newAtt.push({ id: App.Utils.generateId('ATT'), personId: studentId, personType: 'student', date: _attDate, classId: _attClassId, checkIn: null, checkOut: null, status: 'Absent' });
    }
    App.Store.set({ attendance: newAtt });

    try {
      await App.Api.post('/api/attendance', { personId: studentId, personType: 'student', date: _attDate, classId: _attClassId, status: 'Absent' });
      App.Utils.showToast(stuName + ' marked absent (no credit — late notice)', 'info');
    } catch (e) {
      App.Store.set({ attendance: state.attendance });
      App.Utils.showToast('Could not save the absence for ' + stuName + ', please try again', 'error');
    }

    App.Router.refresh();
  }

  var _absenceLock = {};
  async function _markAbsentCredit(studentId) {
    var lockKey = studentId + '|' + _attClassId + '|' + _attDate;
    if (_absenceLock[lockKey]) return;
    _absenceLock[lockKey] = true;
    setTimeout(function() { delete _absenceLock[lockKey]; }, 1500);

    var state = App.Store.get();
    var stu = state.students.find(function(s) { return s.id === studentId; });
    var stuName = stu ? stu.firstName + ' ' + stu.lastName : studentId;
    var cls = state.classes.find(function(c) { return c.id === _attClassId; });
    var clsName = cls ? cls.name : _attClassId;

    // Mark as absent in attendance
    var newAtt = state.attendance.slice();
    var existing = newAtt.findIndex(function(a) { return a.personId === studentId && a.classId === _attClassId && a.date === _attDate; });
    if (existing > -1) {
      newAtt[existing] = Object.assign({}, newAtt[existing], { status: 'Absent', checkIn: null, checkOut: null });
    } else {
      newAtt.push({ id: App.Utils.generateId('ATT'), personId: studentId, personType: 'student', date: _attDate, classId: _attClassId, checkIn: null, checkOut: null, status: 'Absent' });
    }
    App.Store.set({ attendance: newAtt });

    // A failed save here used to be swallowed: the row read Absent locally, the
    // credit was still issued, and the absence disappeared on the next refresh,
    // leaving a credit for a class nobody was ever marked absent from. Roll the
    // optimistic update back and stop before granting anything.
    try {
      await App.Api.post('/api/attendance', { personId: studentId, personType: 'student', date: _attDate, classId: _attClassId, status: 'Absent' });
    } catch (e) {
      App.Store.set({ attendance: state.attendance });
      App.Utils.showToast('Could not save the absence for ' + stuName + '. No credit was issued, please try again.', 'error');
      App.Router.refresh();
      return;
    }

    // Credit the class's duration: 1 credit = 15 minutes, so 1hr = 4.
    var credits = App.Utils.creditsForClass(cls);
    try {
      await App.Api.post('/api/replacement-credits', {
        studentId: studentId,
        type: 'earned',
        minutes: credits,
        category: 'class',
        note: 'Absent from ' + clsName + ' on ' + _attDate,
        classId: _attClassId,
        date: _attDate
      });
      App.Utils.showToast(stuName + ' marked absent — ' + credits + ' credit' + (credits === 1 ? '' : 's') + ' added', 'info');
    } catch(e) {
      App.Utils.showToast(stuName + ' marked absent (replacement failed: ' + e.message + ')', 'warning');
    }

    App.Router.refresh();
  }

  async function _markAllPresent() {
    var state = App.Store.get();
    var students = state.students.filter(function(s) { return s.enrolledClasses.indexOf(_attClassId) > -1; });
    var attendance = state.attendance;
    var now = App.Utils.nowTime();
    var count = 0;

    for (var i = 0; i < students.length; i++) {
      var s = students[i];
      var existing = attendance.find(function(a) {
        return a.personId === s.id && a.classId === _attClassId && a.date === _attDate;
      });
      // Skip if already checked in or marked absent
      if (existing && (existing.checkIn || existing.status === 'Absent')) continue;

      try {
        await App.Api.post('/api/attendance', {
          personId: s.id,
          personType: 'student',
          date: _attDate,
          classId: _attClassId,
          checkIn: now,
          status: 'Present'
        });
        count++;
      } catch(e) {}
    }

    if (count > 0) {
      App.Utils.showToast(count + ' student' + (count !== 1 ? 's' : '') + ' checked in', 'success');
      await App.Api.refresh();
    } else {
      App.Utils.showToast('All students already checked in', 'info');
    }
  }

  async function _quickFeedback() {
    var state = App.Store.get();
    var cls = state.classes.find(function(c) { return c.id === _attClassId; });
    if (!cls) { App.Utils.showToast('Select a class first', 'warning'); return; }

    var today = _attDate || App.Utils.today();

    App.Utils.showModal(
      '<div class="p-6" style="min-width:380px">'
      + '<h2 style="font-size:1.1rem;font-weight:700;margin-bottom:0.25rem">Quick Note</h2>'
      + '<p style="font-size:0.8rem;color:#94a3b8;margin-bottom:1rem">' + App.Utils.esc(cls.name) + ' &middot; ' + App.Utils.formatDate(today) + '</p>'
      + '<form id="quick-feedback-form">'
      + '<textarea name="notes" rows="3" required placeholder="How was the class? Any student highlights?" style="width:100%;padding:0.6rem 0.8rem;font-size:0.85rem;border:1px solid #e2e8f0;border-radius:10px;resize:vertical;font-family:inherit;outline:none" autofocus></textarea>'
      + '<div style="display:flex;justify-content:flex-end;gap:0.5rem;margin-top:0.75rem">'
      + '<button type="button" onclick="App.Utils.hideModal()" style="padding:0.4rem 0.85rem;font-size:0.8rem;border:1px solid #e2e8f0;border-radius:8px;background:#fff;color:#64748b;cursor:pointer">Cancel</button>'
      + '<button type="submit" style="padding:0.4rem 0.85rem;font-size:0.8rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Save</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );

    document.getElementById('quick-feedback-form').addEventListener('submit', async function(e) {
      e.preventDefault();
      var notes = new FormData(e.target).get('notes');
      var teacherId = App.currentTeacher || (cls.teacherIds && cls.teacherIds[0]) || '';
      try {
        await App.Api.post('/api/feedback', {
          classId: _attClassId,
          date: today,
          teacherId: teacherId,
          notes: notes,
          topic: '',
          mood: '',
          studentNotes: []
        });
        App.Utils.hideModal(true);
        App.Utils.showToast('Note saved', 'success');
        await App.Api.refresh();
      } catch(err) {
        App.Utils.showToast(err.message || 'Failed to save', 'error');
      }
    });
  }

  async function _savePostAttFeedback() {
    var el = document.getElementById('post-att-notes');
    if (!el || !el.value.trim()) { App.Utils.showToast('Please enter a note', 'warning'); return; }
    var state = App.Store.get();
    var cls = state.classes.find(function(c) { return c.id === _attClassId; });
    var teacherId = App.currentTeacher || (cls && cls.teacherIds && cls.teacherIds[0]) || '';
    try {
      await App.Api.post('/api/feedback', {
        classId: _attClassId,
        date: _attDate,
        teacherId: teacherId,
        notes: el.value.trim(),
        topic: '',
        mood: '',
        studentNotes: []
      });
      App.Utils.showToast('Class note saved', 'success');
      var prompt = document.getElementById('post-att-prompt');
      if (prompt) prompt.style.display = 'none';
    } catch(err) {
      App.Utils.showToast(err.message || 'Failed to save note', 'error');
    }
  }

  App.Attendance = { render: render, _setTab: _setTab, _setDate: _setDate, _setClass: _setClass, _markStaff: _markStaff, _checkInStudent: _checkInStudent, _checkOutStudent: _checkOutStudent, _doCancelClasses: _doCancelClasses, _toggleAllStaff: _toggleAllStaff, _toggleAllClasses: _toggleAllClasses, _kioskScan: _kioskScan, _setKioskClass: _setKioskClass, _logSelfStudy: _logSelfStudy, _teacherCheckIn: _teacherCheckIn, _teacherCheckOut: _teacherCheckOut, _checkAllIn: _checkAllIn, _setClientPage: _setClientPage, _exportCSV: _exportCSV, _markAbsentCredit: _markAbsentCredit, _markAbsentNoCredit: _markAbsentNoCredit, _undoAttendance: _undoAttendance, _undoStaffAttendance: _undoStaffAttendance, _markAllPresent: _markAllPresent, _quickFeedback: _quickFeedback, _savePostAttFeedback: _savePostAttFeedback };
})();
