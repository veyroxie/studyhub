(function() {
  window.App = window.App || {};

  var _open = false;
  var _readIds = {};  // id -> true, persisted in sessionStorage
  var _SS_KEY = 'sh_notif_read';

  function _loadRead() {
    try { _readIds = JSON.parse(sessionStorage.getItem(_SS_KEY) || '{}'); } catch(e) { _readIds = {}; }
  }
  function _saveRead() {
    try { sessionStorage.setItem(_SS_KEY, JSON.stringify(_readIds)); } catch(e) { console.error('notifs save failed', e); }
  }
  _loadRead();

  // ── Build notification list from store ──────────────────────────────────────
  function build() {
    if (!App.Store) return [];
    var state = App.Store.get();
    var invoices   = state.invoices   || [];
    var students   = state.students   || [];
    var staff      = state.staff      || [];
    var attendance = state.attendance || [];

    var notifs = [];
    var today = App.Utils.today();
    var now   = new Date();
    var in7   = new Date(now); in7.setDate(now.getDate() + 7);
    var isAdmin = App.currentRole === 'admin';

    if (isAdmin) {
      // Payments received today
      var paidToday = invoices.filter(function(i) { return i.status === 'Paid' && i.paidOn === today; });
      paidToday.forEach(function(inv) {
        var stu = students.find(function(s) { return s.id === inv.studentId; });
        notifs.push({
          id: 'paid-today-' + inv.id,
          type: 'billing',
          severity: 'info',
          title: 'Payment received',
          body: App.Utils.esc(stu ? stu.firstName + ' ' + stu.lastName : inv.studentId) + ' · ' + App.Utils.formatCurrency(inv.amount),
          action: 'billing'
        });
      });

      // Payments submitted by parents awaiting verification
      var pendingVerif = invoices.filter(function(i) { return i.status === 'Pending Verification'; });
      if (pendingVerif.length > 0) {
        notifs.push({
          id: 'pending-verif-' + pendingVerif.length,
          type: 'billing',
          severity: 'warning',
          title: pendingVerif.length + ' payment' + (pendingVerif.length > 1 ? 's' : '') + ' awaiting verification',
          body: 'Parent' + (pendingVerif.length > 1 ? 's have' : ' has') + ' submitted payment — please confirm',
          action: 'billing'
        });
      }

      // Pending registrations
      var regs = state.registrations || [];
      var pending = regs.filter(function(r) { return r.status === 'pending'; });
      if (pending.length > 0) {
        notifs.push({
          id: 'pending-regs',
          type: 'registration',
          severity: 'info',
          title: pending.length + ' pending registration' + (pending.length > 1 ? 's' : ''),
          body: 'New enrolment applications awaiting review',
          action: 'students'
        });
      }

      // Overdue invoices
      invoices.filter(function(i) { return i.status === 'Overdue'; })
        .forEach(function(inv) {
          var stu = students.find(function(s) { return s.id === inv.studentId; });
          notifs.push({
            id: 'inv-overdue-' + inv.id,
            type: 'billing',
            severity: 'error',
            title: 'Overdue invoice',
            body: App.Utils.esc(stu ? stu.firstName + ' ' + stu.lastName : inv.studentId) + ' · ' + App.Utils.formatCurrency(inv.amount),
            action: 'billing'
          });
        });

      // Due within 7 days
      invoices.filter(function(i) {
        if (i.status !== 'Unpaid') return false;
        var d = new Date(i.dueDate);
        return d >= now && d <= in7;
      }).forEach(function(inv) {
        var stu  = students.find(function(s) { return s.id === inv.studentId; });
        var days = Math.ceil((new Date(inv.dueDate) - now) / 86400000);
        notifs.push({
          id: 'inv-soon-' + inv.id,
          type: 'billing',
          severity: 'warning',
          title: 'Payment due soon',
          body: App.Utils.esc(stu ? stu.firstName + ' ' + stu.lastName : inv.studentId) + ' · due in ' + days + ' day' + (days !== 1 ? 's' : ''),
          action: 'billing'
        });
      });

      // Staff issues today — grouped
      var lateStaff   = attendance.filter(function(a) { return a.personType === 'staff' && a.date === today && a.status === 'Late'; });
      var absentStaff = attendance.filter(function(a) { return a.personType === 'staff' && a.date === today && a.status === 'Absent'; });
      if (absentStaff.length > 0) {
        var names = absentStaff.map(function(rec) { var s = staff.find(function(x) { return x.id === rec.personId; }); return s ? App.Utils.esc(s.fullName.split(' ')[0]) : rec.personId; }).join(', ');
        notifs.push({ id: 'staff-absent-today', type: 'attendance', severity: 'error', title: absentStaff.length + ' staff absent today', body: names, action: 'attendance' });
      }
      if (lateStaff.length > 0) {
        var lateNames = lateStaff.map(function(rec) { var s = staff.find(function(x) { return x.id === rec.personId; }); return s ? App.Utils.esc(s.fullName.split(' ')[0]) : rec.personId; }).join(', ');
        notifs.push({ id: 'staff-late-today', type: 'attendance', severity: 'warning', title: lateStaff.length + ' staff late today', body: lateNames, action: 'attendance' });
      }

      // Students late/absent today — grouped
      var stuIssues = attendance.filter(function(a) { return a.personType === 'student' && a.date === today && (a.status === 'Late' || a.status === 'Absent'); });
      if (stuIssues.length > 0) {
        var absent = stuIssues.filter(function(a) { return a.status === 'Absent'; });
        var late   = stuIssues.filter(function(a) { return a.status === 'Late'; });
        if (absent.length > 0) notifs.push({ id: 'stu-absent-today', type: 'attendance', severity: 'error',   title: absent.length + ' student' + (absent.length!==1?'s':'') + ' absent today', body: absent.map(function(a){ var s=students.find(function(x){return x.id===a.personId;}); return s?App.Utils.esc(s.firstName):a.personId; }).join(', '), action: 'attendance' });
        if (late.length > 0)   notifs.push({ id: 'stu-late-today',   type: 'attendance', severity: 'warning', title: late.length   + ' student' + (late.length!==1?'s':'')   + ' late today',   body: late.map(function(a){   var s=students.find(function(x){return x.id===a.personId;}); return s?App.Utils.esc(s.firstName):a.personId; }).join(', '), action: 'attendance' });
      }

    } else if (App.currentRole === 'teacher') {
      // Teacher view
      var classes = state.classes || [];
      var todayDayName = new Date().toLocaleDateString('en-US', { weekday: 'long' });

      // Classes happening today
      var myClasses = classes.filter(function(c) {
        return c.teacherIds && c.teacherIds.indexOf(App.currentTeacher) > -1 && c.day === todayDayName;
      });
      if (myClasses.length > 0) {
        notifs.push({
          id: 'tchr-classes-today',
          type: 'attendance',
          severity: 'info',
          title: myClasses.length + ' class' + (myClasses.length > 1 ? 'es' : '') + ' today',
          body: myClasses.map(function(c) { return App.Utils.esc(c.name) + ' (' + App.Utils.formatTime(c.time) + ')'; }).join(', '),
          action: 'attendance'
        });
      }

      // Students absent today (enrolled in teacher's today-classes but not checked in)
      var absentNames = [];
      myClasses.forEach(function(cls) {
        var enrolled = students.filter(function(s) {
          return (s.enrolledClasses || []).indexOf(cls.id) > -1;
        });
        enrolled.forEach(function(s) {
          var hasRecord = attendance.some(function(a) {
            return a.personId === s.id && a.classId === cls.id && a.date === today && a.checkIn;
          });
          if (!hasRecord && absentNames.indexOf(App.Utils.esc(s.firstName)) === -1) {
            absentNames.push(App.Utils.esc(s.firstName));
          }
        });
      });
      if (absentNames.length > 0) {
        notifs.push({
          id: 'tchr-absent-students',
          type: 'attendance',
          severity: 'warning',
          title: absentNames.length + ' student' + (absentNames.length > 1 ? 's' : '') + ' not checked in',
          body: absentNames.join(', '),
          action: 'attendance'
        });
      }

      // Pending announcement approvals submitted by this teacher
      var announcements = state.announcements || [];
      var pendingAnns = announcements.filter(function(a) {
        return a.status === 'pending_approval' && a.createdBy === App.currentTeacher;
      });
      if (pendingAnns.length > 0) {
        notifs.push({
          id: 'tchr-pending-announcements',
          type: 'registration',
          severity: 'info',
          title: pendingAnns.length + ' announcement' + (pendingAnns.length > 1 ? 's' : '') + ' pending approval',
          body: 'Your submitted announcements are awaiting admin review',
          action: 'communication'
        });
      }

    } else {
      // Parent / client view
      var myIds = students
        .filter(function(s) { return s.contact === App.clientParent; })
        .map(function(s) { return s.id; });

      // Overdue invoices for their children
      invoices.filter(function(i) { return i.status === 'Overdue' && myIds.indexOf(i.studentId) > -1; })
        .forEach(function(inv) {
          var stu = students.find(function(s) { return s.id === inv.studentId; });
          notifs.push({
            id: 'inv-overdue-' + inv.id,
            type: 'billing',
            severity: 'error',
            title: 'Payment overdue',
            body: App.Utils.esc(stu ? stu.firstName : inv.studentId) + ' · ' + App.Utils.formatCurrency(inv.amount),
            action: 'billing'
          });
        });

      // Due soon for their children
      invoices.filter(function(i) {
        if (i.status !== 'Unpaid' || myIds.indexOf(i.studentId) === -1) return false;
        var d = new Date(i.dueDate);
        return d >= now && d <= in7;
      }).forEach(function(inv) {
        var stu  = students.find(function(s) { return s.id === inv.studentId; });
        var days = Math.ceil((new Date(inv.dueDate) - now) / 86400000);
        notifs.push({
          id: 'inv-soon-' + inv.id,
          type: 'billing',
          severity: 'warning',
          title: 'Payment due soon',
          body: App.Utils.esc(stu ? stu.firstName : inv.studentId) + ' · due in ' + days + ' day' + (days !== 1 ? 's' : ''),
          action: 'billing'
        });
      });

      // Their kids checked in today
      attendance.filter(function(a) {
        return a.personType === 'student'
          && myIds.indexOf(a.personId) > -1
          && a.date === today
          && a.checkIn;
      }).forEach(function(rec) {
        var stu = students.find(function(s) { return s.id === rec.personId; });
        var cls = (state.classes || []).find(function(c) { return c.id === rec.classId; });
        var clsName = cls ? cls.name : '';
        if (rec.status === 'Late' || rec.status === 'Absent') {
          notifs.push({
            id: 'att-stu-' + rec.id,
            type: 'attendance',
            severity: rec.status === 'Absent' ? 'error' : 'warning',
            title: App.Utils.esc(stu ? stu.firstName : 'Your child') + ' was ' + rec.status.toLowerCase() + ' today',
            body: (clsName ? App.Utils.esc(clsName) + ' · ' : '') + 'Checked in at ' + App.Utils.formatTime(rec.checkIn),
            action: 'attendance'
          });
        } else {
          notifs.push({
            id: 'att-checkin-' + rec.id,
            type: 'attendance',
            severity: 'info',
            title: App.Utils.esc(stu ? stu.firstName : 'Your child') + ' checked in',
            body: (clsName ? App.Utils.esc(clsName) + ' · ' : '') + App.Utils.formatTime(rec.checkIn) + (rec.checkOut ? ' — out ' + App.Utils.formatTime(rec.checkOut) : ''),
            action: 'attendance'
          });
        }
      });
    }

    return notifs;
  }

  // ── Badge ────────────────────────────────────────────────────────────────────
  function updateBadge() {
    if (App.Messages && App.Messages._updateMsgBadge) App.Messages._updateMsgBadge();
    var count = build().filter(function(n) { return !_readIds[n.id]; }).length;
    ['notif-badge','top-notif-badge'].forEach(function(id) {
      var badge = document.getElementById(id);
      if (!badge) return;
      if (count > 0) {
        badge.textContent = count > 99 ? '99+' : String(count);
        badge.classList.remove('hidden');
      } else {
        badge.classList.add('hidden');
      }
    });
  }

  // ── Panel rendering ──────────────────────────────────────────────────────────
  var _ICONS = {
    billing:      '<svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><rect x="1" y="4" width="22" height="16" rx="2"/><path stroke-linecap="round" d="M1 10h22"/></svg>',
    attendance:   '<svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2"/><rect x="9" y="3" width="6" height="4" rx="1"/><path stroke-linecap="round" d="M9 12l2 2 4-4"/></svg>',
    registration: '<svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" d="M18 9v3m0 0v3m0-3h3m-3 0h-3m-2-5a4 4 0 11-8 0 4 4 0 018 0zM3 20a6 6 0 0112 0v1H3v-1z"/></svg>'
  };
  var _COLORS = {
    error:   { icon: 'bg-red-100 text-red-600',    dot: 'bg-red-500'    },
    warning: { icon: 'bg-amber-100 text-amber-600', dot: 'bg-amber-400'  },
    info:    { icon: 'bg-blue-100 text-blue-600',   dot: 'bg-blue-500'   }
  };

  function _renderPanel(notifs) {
    var panel = document.getElementById('notif-panel');
    if (!panel) return;
    var unread = notifs.filter(function(n) { return !_readIds[n.id]; }).length;

    var header = '<div class="px-4 py-3 border-b border-slate-100 flex items-center justify-between">'
      + '<div class="flex items-center gap-2">'
      +   '<span class="font-semibold text-slate-800 text-sm">Notifications</span>'
      +   (unread > 0 ? '<span class="min-w-[18px] h-[18px] bg-red-500 text-white text-[10px] font-bold rounded-full flex items-center justify-center px-1">' + unread + '</span>' : '')
      + '</div>'
      + (unread > 0 ? '<button onclick="App.Notifs.markAllRead()" class="text-xs text-blue-600 hover:text-blue-800 font-medium">Mark all read</button>' : '')
      + '</div>';

    if (notifs.length === 0) {
      panel.innerHTML = header
        + '<div class="py-10 text-center">'
        +   '<div class="text-2xl mb-2">&#10003;</div>'
        +   '<div class="text-sm text-slate-400">All caught up!</div>'
        + '</div>';
      return;
    }

    var items = notifs.map(function(n) {
      var isRead  = !!_readIds[n.id];
      var colors  = _COLORS[n.severity] || _COLORS.info;
      var icon    = _ICONS[n.type] || _ICONS.billing;
      return '<div onclick="App.Notifs._clickNotif(\'' + n.action + '\',\'' + n.id + '\')"'
        + ' class="flex items-start gap-3 px-4 py-3 hover:bg-slate-50 cursor-pointer transition-colors border-b border-slate-50 last:border-0' + (isRead ? ' opacity-50' : '') + '">'
        + '<div class="w-8 h-8 rounded-lg ' + colors.icon + ' flex items-center justify-center shrink-0 mt-0.5">' + icon + '</div>'
        + '<div class="flex-1 min-w-0">'
        +   '<div class="flex items-center gap-1.5">'
        +     '<span class="text-sm font-medium text-slate-800 leading-snug">' + App.Utils.esc(n.title) + '</span>'
        +     (!isRead ? '<span class="w-1.5 h-1.5 rounded-full ' + colors.dot + ' shrink-0"></span>' : '')
        +   '</div>'
        +   '<div class="text-xs text-slate-500 mt-0.5 truncate">' + n.body + '</div>'
        + '</div>'
        + '</div>';
    }).join('');

    panel.innerHTML = header
      + '<div class="overflow-y-auto" style="max-height:400px">' + items + '</div>';
  }

  // ── Public API ───────────────────────────────────────────────────────────────
  function toggle() {
    _open = !_open;
    var panel    = document.getElementById('notif-panel');
    var backdrop = document.getElementById('notif-backdrop');
    if (!panel || !backdrop) return;
    if (_open) {
      _renderPanel(build());
      panel.classList.remove('hidden');
      backdrop.classList.remove('hidden');
    } else {
      panel.classList.add('hidden');
      backdrop.classList.add('hidden');
    }
  }

  function close() {
    _open = false;
    var panel    = document.getElementById('notif-panel');
    var backdrop = document.getElementById('notif-backdrop');
    if (panel)    panel.classList.add('hidden');
    if (backdrop) backdrop.classList.add('hidden');
  }

  function markAllRead() {
    build().forEach(function(n) { _readIds[n.id] = true; });
    _saveRead();
    updateBadge();
    _renderPanel(build());
  }

  function _clickNotif(page, id) {
    _readIds[id] = true;
    _saveRead();
    close();
    updateBadge();
    App.Router.navigate(page);
  }

  function refresh() {
    updateBadge();
    if (_open) _renderPanel(build());
    if (App.Messages && App.Messages._updateMsgBadge) App.Messages._updateMsgBadge();
  }

  App.Notifs = {
    build: build,
    toggle: toggle,
    close: close,
    markAllRead: markAllRead,
    _clickNotif: _clickNotif,
    refresh: refresh,
    updateBadge: updateBadge
  };
})();
