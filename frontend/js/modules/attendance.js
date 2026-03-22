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
        + 'onmouseover="this.style.opacity=\'0.85\'" onmouseout="this.style.opacity=\'1\'">Check In</button>'
        + ((App.currentRole === 'admin' || App.currentRole === 'teacher')
          ? '<button onclick="App.Attendance._markAbsentCredit(\'' + s.id + '\')" style="'
            + 'min-height:36px;width:100%;margin-top:0.35rem;padding:0.35rem 0.75rem;background:#fef2f2;color:#dc2626;border:1px solid #fecaca;'
            + 'border-radius:10px;font-size:0.75rem;font-weight:600;cursor:pointer;transition:opacity 0.15s" '
            + 'title="Mark absent and add 60 min replacement"'
            + '>Absent + Replacement</button>'
          : '');
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
      return '<div style="background:#fff;border-radius:14px;border:2px dashed #e2e8f0;padding:3rem;text-align:center;color:#94a3b8;font-size:0.9rem">No attendance records found</div>';
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
    const staffWithClass = staff.filter(function(s) {
      return classes.some(function(c) { return c.teacherIds.indexOf(s.id) > -1 && c.day === dayOfWeek; });
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
            + '</div>'
            + '</div>';
        }).join('')
      + '</div>'
      + '</div>';
  }

  function _studentTab() {
    const { classes, students, attendance } = App.Store.get();
    const dayOfWeek = new Date(_attDate + 'T00:00:00').toLocaleDateString('en-US', { weekday: 'long' });
    const scheduledClasses = classes.filter(function(c) { return c.day === dayOfWeek; });
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
      +     displayClasses.map(function(c) { return '<option value="' + c.id + '" ' + (c.id === _attClassId ? 'selected' : '') + '>' + c.name + ' — ' + App.Utils.formatTime(c.time) + '</option>'; }).join('')
      +     '</select>'
      +     (classes.length !== scheduledClasses.length || _showAllClasses
            ? '<button onclick="App.Attendance._toggleAllClasses()" style="font-size:0.75rem;font-weight:600;color:var(--gold);background:none;border:none;cursor:pointer;white-space:nowrap;min-height:48px">'
              + (_showAllClasses ? 'Scheduled only' : 'All classes') + '</button>'
            : '')
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
      + '<div style="font-size:0.9rem;color:#64748b;margin-top:0.3rem">' + scan.msg + '</div>';
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
    if (existingIdx === -1) {
      newAtt.push({ id: App.Utils.generateId('ATT'), personId: id, personType: 'student', date: today, classId: _kioskClassId, checkIn: now, checkOut: null, status: 'Present' });
      action = 'in';
    } else if (!newAtt[existingIdx].checkOut) {
      newAtt[existingIdx] = Object.assign({}, newAtt[existingIdx], { checkOut: now });
      action = 'out';
    } else {
      _kioskLastScan = { ok: false, name: stu.firstName + ' ' + stu.lastName, msg: 'Already checked in and out today', action: null };
      _kioskShowFeedback(_kioskLastScan);
      setTimeout(function() { _kioskClearFeedback(stu.firstName + ' ' + stu.lastName); }, 4000);
      return;
    }

    App.Store.set({ attendance: newAtt });
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
  var SELF_STUDY_FREE_H = 4;     // free hours per month for enrolled students

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
      const usedMin  = myMonth.reduce(function(acc, ss) { return acc + (ss.duration || 0); }, 0);
      const usedHr   = usedMin / 60;
      const freeRem  = Math.max(0, SELF_STUDY_FREE_H - usedHr);
      const billable = Math.max(0, usedHr - SELF_STUDY_FREE_H);
      const pct      = Math.min(100, (usedHr / SELF_STUDY_FREE_H) * 100);
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
        .reduce(function(a, ss) { return a + (ss.duration || 0); }, 0);
      return acc + myMin;
    }, 0);
    const billableTotal = eligible.reduce(function(acc, s) {
      const myHr = sessions.filter(function(ss) { return ss.studentId === s.id && ss.date.startsWith(thisMonth); })
        .reduce(function(a, ss) { return a + (ss.duration || 0); }, 0) / 60;
      return acc + Math.max(0, myHr - SELF_STUDY_FREE_H);
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
            +   '<div style="font-size:1rem;font-weight:800;color:#15803d">RM' + (Math.min(eligible.length * SELF_STUDY_FREE_H, monthTotal / 60) * SELF_STUDY_RATE) + '</div></div>')
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
      + 'Free allowance: ' + SELF_STUDY_FREE_H + 'hr/month per enrolled student = <strong>RM40 equivalent</strong>. Extra hours billed at RM' + SELF_STUDY_RATE + '/hr.'
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
        duration: duration,
        notes: (fd.get('notes') || '').trim()
      };
      App.Utils.hideModal(true);
      App.Api.post('/api/self-study', newSession).then(function(result) {
        var existing = App.Store.get().selfStudySessions || [];
        App.Store.set({ selfStudySessions: [result || newSession].concat(existing) });
        App.Utils.showToast('Session logged: ' + (duration >= 60 ? (duration / 60).toFixed(1) + 'hr' : duration + 'min'), 'success');
        App.Router.refresh();
      }).catch(function(err) {
        var existing = App.Store.get().selfStudySessions || [];
        App.Store.set({ selfStudySessions: [newSession].concat(existing) });
        App.Utils.showToast('Saved locally (offline)', 'warning');
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
      const { classes } = App.Store.get();
      const dayOfWeek = new Date(date + 'T00:00:00').toLocaleDateString('en-US', { weekday: 'long' });
      const first = classes.find(function(c) { return c.day === dayOfWeek; });
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

    var newCancellations = [];
    classIds.forEach(function(classId) {
      var cancellation = { id: App.Utils.generateId('cancel'), classId: classId, date: _attDate, reason: 'Teacher absent' };
      cancelled.push(cancellation);
      newCancellations.push(cancellation);

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

    App.Utils.hideModal(true);

    // Post each cancellation to the API
    var apiCalls = newCancellations.map(function(c) {
      return App.Api.post('/api/cancelled-classes', c).catch(function() { return null; });
    });
    Promise.all(apiCalls).then(function() {
      App.Store.set({ cancelledClasses: cancelled, messages: messages, announcements: announcements });
      if (App.Notifs && App.Notifs.refresh) App.Notifs.refresh();
      App.Utils.showToast('Class cancelled. Parents have been notified.', 'success');
    }).catch(function() {
      App.Store.set({ cancelledClasses: cancelled, messages: messages, announcements: announcements });
      if (App.Notifs && App.Notifs.refresh) App.Notifs.refresh();
      App.Utils.showToast('Saved locally (offline)', 'warning');
    });
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
      +   '<div style="' + AVATAR_STYLE + ';background:rgba(139,92,246,0.12);color:#7c3aed;border:1px solid rgba(139,92,246,0.2)">' + teacherName.charAt(0) + '</div>'
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
    App.Store.set({ attendance: newAtt });
    App.Utils.showToast('Checked in at ' + App.Utils.formatTime(now), 'success');
    App.Router.refresh();
  }

  function _teacherCheckOut() {
    var state = App.Store.get();
    var today = _attDate || App.Utils.today();
    var now = App.Utils.nowTime();
    var newAtt = state.attendance.map(function(a) {
      if (a.personId === App.currentTeacher && a.personType === 'staff' && a.date === today && !a.checkOut) {
        return Object.assign({}, a, { checkOut: now });
      }
      return a;
    });
    App.Store.set({ attendance: newAtt });
    App.Utils.showToast('Checked out at ' + App.Utils.formatTime(now), 'info');
    App.Router.refresh();
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
      +     myClasses.map(function(c) { return '<option value="' + c.id + '" ' + (c.id === selectedClass.id ? 'selected' : '') + '>' + c.name + ' — ' + c.day + ' ' + App.Utils.formatTime(c.time) + '</option>'; }).join('')
      +     '</select>'
      +     '<button onclick="App.Attendance._checkAllIn()" style="padding:0.5rem 0.9rem;font-size:0.82rem;font-weight:700;background:#22c55e;color:#fff;border:none;border-radius:9px;cursor:pointer;min-height:48px;white-space:nowrap;transition:opacity 0.15s" onmouseover="this.style.opacity=\'0.85\'" onmouseout="this.style.opacity=\'1\'">Check All In</button>'
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

  function _checkAllIn() {
    var state = App.Store.get();
    var selectedClass = state.classes.find(function(c) { return c.id === _attClassId; });
    if (!selectedClass) return;
    var enrolledStudents = state.students.filter(function(s) { return s.enrolledClasses.indexOf(_attClassId) > -1; });
    var now = App.Utils.nowTime();
    var today = _attDate || App.Utils.today();
    var newAtt = state.attendance.slice();
    var count = 0;
    enrolledStudents.forEach(function(s) {
      var existing = newAtt.find(function(a) { return a.personId === s.id && a.classId === _attClassId && a.date === today; });
      if (!existing) {
        newAtt.push({ id: App.Utils.generateId('ATT'), personId: s.id, personType: 'student', date: today, classId: _attClassId, checkIn: now, checkOut: null, status: 'Present' });
        count++;
      }
    });
    if (count === 0) {
      App.Utils.showToast('All students already checked in', 'info');
      return;
    }
    App.Store.set({ attendance: newAtt });
    App.Utils.showToast(count + ' student' + (count !== 1 ? 's' : '') + ' checked in', 'success');
    App.Router.refresh();
  }

  async function _markAbsentCredit(studentId) {
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

    // Post attendance to backend
    try {
      await App.Api.post('/api/attendance', { personId: studentId, personType: 'student', date: _attDate, classId: _attClassId, status: 'Absent' });
    } catch(e) {}

    // Add 60 min replacement credit
    try {
      await App.Api.post('/api/replacement-credits', {
        studentId: studentId,
        type: 'earned',
        minutes: 60,
        note: 'Absent from ' + clsName + ' on ' + _attDate,
        classId: _attClassId,
        date: _attDate
      });
      App.Utils.showToast(stuName + ' marked absent — 60 min replacement added', 'info');
    } catch(e) {
      App.Utils.showToast(stuName + ' marked absent (replacement failed: ' + e.message + ')', 'warning');
    }

    await App.Api.refresh();
  }

  App.Attendance = { render: render, _setTab: _setTab, _setDate: _setDate, _setClass: _setClass, _markStaff: _markStaff, _checkInStudent: _checkInStudent, _checkOutStudent: _checkOutStudent, _doCancelClasses: _doCancelClasses, _toggleAllStaff: _toggleAllStaff, _toggleAllClasses: _toggleAllClasses, _kioskScan: _kioskScan, _setKioskClass: _setKioskClass, _logSelfStudy: _logSelfStudy, _teacherCheckIn: _teacherCheckIn, _teacherCheckOut: _teacherCheckOut, _checkAllIn: _checkAllIn, _setClientPage: _setClientPage, _exportCSV: _exportCSV, _markAbsentCredit: _markAbsentCredit };
})();
