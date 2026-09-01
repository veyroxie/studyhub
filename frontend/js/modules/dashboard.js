(function() {
  window.App = window.App || {};

  var _dashView = sessionStorage.getItem('sh_dash_view') || 'mine'; // 'mine' | 'ops'

  function _setDashView(v) {
    _dashView = v;
    sessionStorage.setItem('sh_dash_view', v);
    App.Router.refresh();
  }

  function render(container) {
    var isAdmin   = App.currentRole === 'admin';
    var isTeacher = App.currentRole === 'teacher';

    // Dashboard view toggle (admin only)
    var viewToggle = '';
    if (isAdmin) {
      viewToggle = '<div style="display:flex;align-items:center;justify-content:flex-end;margin-bottom:1rem">'
        + '<div style="display:flex;gap:0.25rem;background:#f1f5f9;border-radius:8px;padding:3px">'
        + '<button onclick="App.Dashboard._setView(\'mine\')" style="padding:0.3rem 1rem;font-size:0.75rem;font-weight:600;border:none;border-radius:6px;cursor:pointer;background:' + (_dashView==='mine'?'var(--gold)':'transparent') + ';color:' + (_dashView==='mine'?'#0a0a0a':'#94a3b8') + '">My View</button>'
        + '<button onclick="App.Dashboard._setView(\'ops\')" style="padding:0.3rem 1rem;font-size:0.75rem;font-weight:600;border:none;border-radius:6px;cursor:pointer;background:' + (_dashView==='ops'?'var(--gold)':'transparent') + ';color:' + (_dashView==='ops'?'#0a0a0a':'#94a3b8') + '">Operations</button>'
        + '</div>'
        + '</div>';
    }

    var dashContent = isTeacher ? _teacherDash() : (isAdmin && _dashView === 'ops') ? _opsDash() : isAdmin ? _adminDash() : _parentDash();
    container.innerHTML = viewToggle + dashContent;
    setTimeout(_runCountUp, 80);
  }

  // ── Count-up ─────────────────────────────────────────────────────────────────
  function _runCountUp() {
    document.querySelectorAll('[data-count]').forEach(function(el) {
      var target = parseFloat(el.dataset.count);
      if (isNaN(target) || target === 0) return;
      var isCurr = el.dataset.currency === '1';
      var t0 = null;
      (function step(ts) {
        if (!t0) t0 = ts;
        var p = Math.min((ts - t0) / 650, 1);
        var ease = 1 - Math.pow(1 - p, 3);
        var val = target * ease;
        el.textContent = isCurr
          ? 'RM\u00a0' + val.toFixed(2).replace(/\B(?=(\d{3})+(?!\d))/g, ',')
          : Math.round(val).toString();
        if (p < 1) requestAnimationFrame(step);
      })(performance.now());
    });
  }

  // ── Admin ─────────────────────────────────────────────────────────────────────
  function _adminDash() {
    var s             = App.Store.get();
    var students      = s.students      || [];
    var classes       = s.classes       || [];
    var invoices      = s.invoices      || [];
    var staff         = s.staff         || [];
    var attendance    = s.attendance    || [];
    var announcements = s.announcements || [];
    var registrations = s.registrations || [];

    var today    = App.Utils.today();
    var todayDay = new Date(today + 'T00:00:00').toLocaleDateString('en-US', { weekday: 'long' });
    var now = new Date();
    var in7 = new Date(now); in7.setDate(now.getDate() + 7);

    var pendingRegs    = registrations.filter(function(r) { return r.status === 'pending'; }).length;
    var activeStudents = students.filter(function(s) { return s.status === 'Active'; }).length;
    var newStudents    = students.filter(function(s) { return s.status === 'New'; }).length;
    var schedVersions   = s.scheduleVersions || [];
    var todayClasses   = classes.filter(function(c) { return App.Utils.scheduleOn(c, schedVersions, today).day === todayDay; });
    var overdueInvs    = invoices.filter(function(i) { return i.status === 'Overdue'; });
    var dueSoonInvs    = invoices.filter(function(i) {
      if (i.status !== 'Unpaid') return false;
      var d = new Date(i.dueDate); return d >= now && d <= in7;
    });
    var thisMonth    = today.slice(0, 7);
    var monthRevenue = invoices
      .filter(function(i) { return i.status === 'Paid' && i.paidOn && i.paidOn.slice(0, 7) === thisMonth; })
      .reduce(function(a, i) { return a + i.amount; }, 0);

    var recentStudents = students.slice()
      .sort(function(a, b) { return b.registeredOn.localeCompare(a.registeredOn); })
      .slice(0, 5);
    var topOverdue = overdueInvs.slice(0, 3);

    // ── HTML ──────────────────────────────────────────────────────────────────
    var _greetIcon = { morning:'<path d="M12 2v2m0 16v2M4.93 4.93l1.41 1.41m11.32 11.32l1.41 1.41M2 12h2m16 0h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41"/><circle cx="12" cy="12" r="4"/>', afternoon:'<circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/>', evening:'<path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>', night:'<path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>' };
    var _tod = _timeOfDay();

    return '<div class="dash-hero">'
      + '<div style="display:flex;align-items:start;justify-content:space-between;gap:1rem;flex-wrap:wrap;position:relative;z-index:1">'
      +   '<div>'
      +     '<div style="display:flex;align-items:center;gap:0.5rem;margin-bottom:6px">'
      +       '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--gold)" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + (_greetIcon[_tod] || _greetIcon.morning) + '</svg>'
      +       '<p style="font-size:0.7rem;font-weight:700;text-transform:uppercase;letter-spacing:0.12em;color:var(--gold);margin:0">' + _dateFull() + '</p>'
      +     '</div>'
      +     '<h1 style="font-family:var(--serif);font-size:1.85rem;font-weight:700;letter-spacing:-0.04em;color:#1a1a1a;line-height:1.2;margin:0">Good ' + _tod + '</h1>'
      +     '<p style="font-size:0.82rem;color:#64748b;margin:4px 0 0">' + activeStudents + ' active students · ' + todayClasses.length + ' class' + (todayClasses.length !== 1 ? 'es' : '') + ' today</p>'
      +   '</div>'
      +   '<div style="display:grid;grid-template-columns:1fr 1fr;gap:0.5rem">'
      +     _heroBtn('Add Student', "App.Router.navigate('students');setTimeout(function(){App.Students._addModal();},150)")
      +     _heroBtn('Add Invoice', "App.Router.navigate('billing');setTimeout(function(){App.Billing._createModal();},150)")
      +     _heroBtn('Announcement', "App.Router.navigate('communication');setTimeout(function(){App.Communication._newModal();},150)")
      +     _heroBtn('Attendance', "App.Router.navigate('attendance')")
      +   '</div>'
      + '</div>'
      + '</div>'

      // Stats
      + '<div style="display:grid;grid-template-columns:repeat(4,1fr);gap:1rem">'
      + stat('Active Students', activeStudents,  false, '#1d4ed8',   false, '#eff6ff', 'students')
      + stat('Revenue / Month', monthRevenue,    true,  '#92400e', true, '#fef9ec', 'billing')
      + stat('Overdue Invoices',overdueInvs.length, false, overdueInvs.length > 0 ? '#991b1b' : '#94a3b8', false, overdueInvs.length > 0 ? '#fef2f2' : '#f9f9f9', 'billing')
      + stat('Pending Regs',   pendingRegs,     false, pendingRegs > 0 ? '#92400e' : '#94a3b8', false, pendingRegs > 0 ? '#fffbeb' : '#f9f9f9', 'students')
      + '</div>'

      // Main two-col
      + '<div style="display:grid;grid-template-columns:3fr 2fr;gap:1rem;align-items:stretch">'

        // Today's classes
        + card('Today\'s Classes <span style="font-size:0.75rem;font-weight:500;color:#94a3b8;margin-left:6px">' + todayDay + '</span>', 'calendar')
        + (todayClasses.length === 0
            ? '<p style="color:#94a3b8;font-size:0.84rem;padding:1.5rem 0;text-align:center">No classes today</p>'
            : todayClasses.map(function(c) {
                var colors   = App.Utils.colorClasses(c.color);
                var teachers = staff.filter(function(t) { return c.teacherIds.indexOf(t.id) > -1; }).map(function(t) { return t.name; }).join(', ');
                var pct      = c.capacity > 0 ? Math.round(c.enrolled / c.capacity * 100) : 0;
                var pillColor = c.type === 'Private' ? { bg:'#f5f3ff', text:'#6d28d9' } : c.type === 'Workshop' ? { bg:'#f0fdfa', text:'#0f766e' } : (c.enrolled >= c.capacity) ? { bg:'#fef2f2', text:'#991b1b' } : { bg:'#f0fdf4', text:'#15803d' };
                var pillLabel = c.type === 'Private' ? 'Private' : c.type === 'Workshop' ? 'Workshop' : (c.enrolled >= c.capacity) ? 'Full' : 'Open';
                return '<div style="display:flex;align-items:center;gap:0.75rem;padding:0.65rem 0;border-bottom:1px solid #f4f4f2">'
                  + '<div style="width:3px;border-radius:99px;align-self:stretch;min-height:36px;flex-shrink:0" class="' + colors.dot + '"></div>'
                  + '<div style="font-size:0.72rem;font-weight:700;color:#64748b;min-width:48px;flex-shrink:0">' + App.Utils.formatTime(c.time) + '</div>'
                  + '<div style="width:1px;background:#f0ede8;align-self:stretch;flex-shrink:0"></div>'
                  + '<div style="flex:1;min-width:0">'
                  +   '<div style="font-weight:600;font-size:0.85rem;color:#111">' + App.Utils.esc(c.name) + '</div>'
                  +   '<div style="font-size:0.72rem;color:#94a3b8;margin-top:2px">' + App.Utils.formatTime(c.time) + '–' + App.Utils.formatTime(c.endTime) + (teachers ? '&nbsp;·&nbsp;' + App.Utils.esc(teachers) : '') + '&nbsp;·&nbsp;' + App.Utils.esc(c.classroom) + '</div>'
                  + '</div>'
                  + '<div style="display:flex;align-items:center;gap:0.5rem;flex-shrink:0">'
                  +   '<span style="font-size:0.62rem;font-weight:700;text-transform:uppercase;letter-spacing:0.04em;padding:2px 7px;border-radius:5px;background:' + pillColor.bg + ';color:' + pillColor.text + '">' + pillLabel + '</span>'
                  +   '<div style="text-align:right">'
                  +     '<div style="font-size:0.8rem;font-weight:700;color:' + (pct >= 100 ? '#dc2626' : '#374151') + '">' + c.enrolled + '/' + c.capacity + '</div>'
                  +     '<div style="width:44px;height:3px;background:#f1f5f9;border-radius:99px;margin-top:3px;overflow:hidden"><div style="width:' + Math.min(pct,100) + '%;height:100%;background:' + (pct>=100?'#ef4444':'var(--gold)') + ';border-radius:99px"></div></div>'
                  +   '</div>'
                  +   '<button onclick="App.Router.navigate(\'attendance\')" style="font-size:0.7rem;font-weight:600;padding:0.25rem 0.6rem;border:1px solid rgba(201,162,39,0.3);border-radius:6px;background:var(--gold-dim);color:var(--gold);cursor:pointer;white-space:nowrap;transition:all 0.15s" onmouseover="this.style.background=\'var(--gold)\';this.style.color=\'#0a0a0a\'" onmouseout="this.style.background=\'var(--gold-dim)\';this.style.color=\'var(--gold)\'">Mark</button>'
                  + '</div>'
                  + '</div>';
              }).join(''))
        + '</div></div>' // close inner padding div + today card

        // Needs attention
        + card('Needs Attention', null)
        + _attnItems(pendingRegs, overdueInvs, dueSoonInvs, newStudents, students, attendance, staff, classes, today, todayDay, now, invoices)
        + '</div></div>' // close inner padding div + attn card

      + '</div>' // close two-col

      + _weeklyAttendanceCard(attendance, today, students)

      // Bottom two-col: recent students + overdue invoices
      + '<div style="display:grid;grid-template-columns:3fr 2fr;gap:1rem;align-items:start">'

        // Recent students
        + card('Recent Students', 'students')
        + recentStudents.map(function(stu) {
            var cls = classes.filter(function(c) { return stu.enrolledClasses.indexOf(c.id) > -1; }).map(function(c) { return c.name; }).join(', ');
            return '<div style="display:flex;align-items:center;gap:0.7rem;padding:0.55rem 0;border-bottom:1px solid #f4f4f2">'
              + '<div style="width:2rem;height:2rem;border-radius:50%;background:var(--gold-dim);border:1px solid rgba(201,162,39,0.25);color:var(--gold);font-weight:800;font-size:0.78rem;display:flex;align-items:center;justify-content:center;flex-shrink:0">' + App.Utils.esc(stu.firstName.charAt(0)) + '</div>'
              + '<div style="flex:1;min-width:0">'
              +   '<div style="font-size:0.83rem;font-weight:600;color:#111;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + App.Utils.esc(stu.firstName) + ' ' + App.Utils.esc(stu.lastName) + '</div>'
              +   '<div style="font-size:0.71rem;color:#94a3b8;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + App.Utils.esc(cls || 'No class') + '</div>'
              + '</div>'
              + App.Utils.statusBadge(stu.status)
              + '<button onclick="App.Students && App.Students._viewModal(\'' + stu.id + '\')" style="font-size:0.7rem;color:#94a3b8;background:none;border:none;cursor:pointer;padding:0 0.2rem">View</button>'
              + '</div>';
          }).join('')
        + '<button onclick="App.Router.navigate(\'students\')" class="dash-link">View all students →</button>'
        + '</div></div>' // close inner padding div + recent students card

        // Overdue invoices quick panel
        + card('Overdue Invoices', 'billing')
        + (topOverdue.length === 0
            ? '<p style="color:#94a3b8;font-size:0.82rem;text-align:center;padding:1.5rem 0">No overdue invoices ✓</p>'
            : topOverdue.map(function(inv) {
                var stu = students.find(function(s) { return s.id === inv.studentId; });
                return '<div style="display:flex;align-items:center;gap:0.6rem;padding:0.55rem 0;border-bottom:1px solid #f4f4f2">'
                  + '<div style="flex:1;min-width:0">'
                  +   '<div style="font-size:0.82rem;font-weight:600;color:#111">' + App.Utils.esc(stu ? stu.firstName + ' ' + stu.lastName : inv.studentId) + '</div>'
                  +   '<div style="font-size:0.7rem;color:#94a3b8">' + App.Utils.formatDate(inv.dueDate) + '</div>'
                  + '</div>'
                  + '<div style="font-size:0.82rem;font-weight:700;color:#dc2626;flex-shrink:0">' + App.Utils.formatCurrency(inv.amount) + '</div>'
                  + '</div>';
              }).join(''))
        + '<button onclick="App.Router.navigate(\'billing\')" class="dash-link">Manage billing →</button>'
        + '</div></div>' // close inner padding div + overdue card

      + '</div>' // close bottom row

      // Replacement credits summary
      + _replacementCreditsCard(students, s.replacementCredits || [], classes);
  }

  function _replacementCreditsCard(students, credits, classes) {
    // Build per-student balances
    var balances = {};
    credits.forEach(function(rc) {
      if (!balances[rc.studentId]) balances[rc.studentId] = { earned: 0, used: 0 };
      if (rc.type === 'earned') balances[rc.studentId].earned += rc.minutes;
      else balances[rc.studentId].used += rc.minutes;
    });
    var rows = [];
    Object.keys(balances).forEach(function(sid) {
      var b = balances[sid];
      var bal = b.earned - b.used;
      if (bal <= 0) return;
      var stu = students.find(function(s) { return s.id === sid; });
      if (!stu) return;
      rows.push({ id: sid, name: stu.firstName + ' ' + stu.lastName, balance: bal, earned: b.earned, used: b.used });
    });
    rows.sort(function(a, b) { return b.balance - a.balance; });

    if (rows.length === 0) return '';

    return '<div style="margin-top:1rem">'
      + card('Pending Replacements', 'students')
      + '<div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(220px,1fr));gap:0.75rem">'
      + rows.map(function(r) {
          return '<div style="display:flex;align-items:center;gap:0.75rem;padding:0.7rem 0.85rem;background:#fffbeb;border:1px solid #fef3c7;border-radius:12px;cursor:pointer" onclick="App.Students._viewModal(\'' + r.id + '\')">'
            + '<div style="width:2.2rem;height:2.2rem;border-radius:10px;background:var(--gold-dim);color:var(--gold);font-weight:800;font-size:0.85rem;display:flex;align-items:center;justify-content:center;flex-shrink:0">' + App.Utils.esc(r.name.charAt(0)) + '</div>'
            + '<div style="flex:1;min-width:0">'
            +   '<div style="font-size:0.83rem;font-weight:600;color:#111;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + App.Utils.esc(r.name) + '</div>'
            +   '<div style="font-size:0.7rem;color:#94a3b8">Earned ' + r.earned + ' · Used ' + r.used + '</div>'
            + '</div>'
            + '<div style="text-align:right;flex-shrink:0">'
            +   '<div style="font-family:var(--serif);font-size:1.2rem;font-weight:700;color:#92400e">' + r.balance + '</div>'
            +   '<div style="font-size:0.6rem;color:#b08d20;font-weight:600">CR OWED</div>'
            + '</div>'
            + '</div>';
        }).join('')
      + '</div>'
      + '<button onclick="App.Router.navigate(\'students\')" class="dash-link">View all students →</button>'
      + '</div></div>'
      + '</div>';
  }

  // ── Parent (family-centric) ──────────────────────────────────────────────────
  function _parentDash() {
    var s             = App.Store.get();
    var students      = s.students      || [];
    var classes       = s.classes       || [];
    var invoices      = s.invoices      || [];
    var announcements = s.announcements || [];
    var attendance    = s.attendance    || [];
    var feedbacks     = s.feedback      || [];
    var repCredits    = s.replacementCredits || [];
    var staff         = s.staff         || [];

    var myStudents = students.filter(function(st) { return st.contact === App.clientParent && st.status !== 'Inactive'; });
    var myIds      = myStudents.map(function(st) { return st.id; });
    var myInvoices = invoices.filter(function(i)  { return myIds.indexOf(i.studentId) > -1; });

    var now   = new Date();
    var today = App.Utils.today();
    var in7   = new Date(now); in7.setDate(now.getDate() + 7);
    var todayDay = new Date(today + 'T00:00:00').toLocaleDateString('en-US', { weekday: 'long' });
    var dayOrder = ['Sunday','Monday','Tuesday','Wednesday','Thursday','Friday','Saturday'];
    var todayIdx = dayOrder.indexOf(todayDay);
    var schedVersions = s.scheduleVersions || [];
    var cutoff30 = new Date(now); cutoff30.setDate(cutoff30.getDate() - 30);
    var cutoff30Str = App.Utils.localDate(cutoff30);

    var overdueInvs = myInvoices.filter(function(i) { return i.status === 'Overdue'; });
    var dueSoonInvs = myInvoices.filter(function(i) {
      if (i.status !== 'Unpaid') return false;
      var d = new Date(i.dueDate); return d >= now && d <= in7;
    });

    // Payment gate — surface a banner whenever the family has an unpaid or
    // overdue Monthly invoice. Progress reports, attendance notifications
    // and receipts are gated behind this elsewhere in the app.
    var unpaidMonthly = myInvoices.filter(function(i) {
      return i.type === 'Monthly' && (i.status === 'Unpaid' || i.status === 'Overdue');
    });
    var paymentGateBanner = unpaidMonthly.length > 0
      ? '<div style="background:#fef3c7;border:1px solid #fde68a;border-left:4px solid #d97706;border-radius:12px;padding:1rem 1.25rem;margin-bottom:1.25rem;display:flex;align-items:flex-start;gap:0.85rem">'
        + '<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="#92400e" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="flex-shrink:0;margin-top:2px"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>'
        + '<div>'
        +   '<div style="font-size:0.92rem;font-weight:700;color:#92400e;margin-bottom:2px">You have ' + unpaidMonthly.length + ' unpaid invoice' + (unpaidMonthly.length !== 1 ? 's' : '') + '</div>'
        +   '<div style="font-size:0.82rem;color:#78350f;line-height:1.45">Progress reports, attendance notifications and receipts are paused until payment is verified. <a href="#" onclick="event.preventDefault();App.Router.navigate(\'billing\')" style="color:#92400e;font-weight:700;text-decoration:underline">Pay now</a>.</div>'
        + '</div>'
        + '</div>'
      : '';

    var childNames = myStudents.map(function(st) { return App.Utils.esc(st.firstName); }).join(' &amp; ');
    var _tod = _timeOfDay();

    // ── Hero ──────────────────────────────────────────────────────────────────
    var html = paymentGateBanner + '<div class="dash-hero">'
      + '<div style="display:flex;align-items:start;justify-content:space-between;gap:1rem;flex-wrap:wrap;position:relative;z-index:1">'
      +   '<div>'
      +     '<p style="font-size:0.7rem;font-weight:700;text-transform:uppercase;letter-spacing:0.12em;color:var(--gold);margin:0 0 6px">' + _dateFull() + '</p>'
      +     '<h1 style="font-family:var(--serif);font-size:1.7rem;font-weight:700;letter-spacing:-0.04em;color:#1a1a1a;line-height:1.2;margin:0">Good ' + _tod + (childNames ? ', ' + childNames + '\'s family' : '') + '</h1>'
      +     '<p style="font-size:0.82rem;color:#64748b;margin:4px 0 0">' + myStudents.length + ' child' + (myStudents.length !== 1 ? 'ren' : '') + ' enrolled</p>'
      +   '</div>'
      +   '<button onclick="App.Router.navigate(\'profile\')" title="Edit profile" style="padding:0.45rem 0.9rem;font-size:0.75rem;font-weight:600;border:1px solid #e2e8f0;border-radius:8px;background:#fff;color:#64748b;cursor:pointer;display:flex;align-items:center;gap:0.4rem;transition:all 0.15s" onmouseover="this.style.borderColor=\'var(--gold)\';this.style.color=\'#374151\'" onmouseout="this.style.borderColor=\'#e2e8f0\';this.style.color=\'#64748b\'">'
      +     '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>'
      +     'My Profile'
      +   '</button>'
      + '</div>'
      + '</div>';

    // ── First-login checklist (show if not dismissed) ────────────────────────
    var checklistDone = localStorage.getItem('sh_checklist_done');
    if (!checklistDone) {
      var visited = JSON.parse(localStorage.getItem('sh_checklist_visited') || '{}');
      var _cs = App.Store.get();
      var isAdminUser = App.currentRole === 'admin';
      var csTitle, csSub, checkItems;
      if (isAdminUser) {
        // Admins get a real setup sequence; "done" is data-aware where we can
        // tell (subjects/students/invoices exist), visited-based otherwise.
        csTitle = 'Set up your centre';
        csSub = 'A few steps to get StudyHub ready for billing';
        checkItems = [
          { key: 'biz',      label: 'Add business & bank details', page: 'calendar', done: !!visited['biz'] },
          { key: 'subjects', label: 'Add subjects & classes',      page: 'calendar', done: (_cs.subjects || []).length > 0 || (_cs.classes || []).length > 0 },
          { key: 'students', label: 'Add your students',           page: 'students', done: (_cs.students || []).length > 0 },
          { key: 'billing',  label: 'Generate invoices',           page: 'billing',  done: (_cs.invoices || []).length > 0 }
        ];
      } else {
        csTitle = 'Welcome to StudyHub';
        csSub = 'Get started by exploring these sections';
        checkItems = [
          { key: 'calendar',   label: 'View your child\'s schedule', page: 'calendar',   done: !!visited['calendar'] },
          { key: 'billing',    label: 'Check upcoming payments',     page: 'billing',    done: !!visited['billing'] },
          { key: 'progress',   label: 'See progress reports',        page: 'progress',   done: !!visited['progress'] },
          { key: 'attendance', label: 'View attendance records',     page: 'attendance', done: !!visited['attendance'] }
        ];
      }
      var allDone = checkItems.every(function(item) { return item.done; });

      html += '<div style="background:#fff;border-radius:14px;border:1px solid rgba(201,162,39,0.2);padding:1.25rem 1.5rem;margin-bottom:0.5rem">'
        + '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.75rem">'
        +   '<div>'
        +     '<div style="font-size:0.95rem;font-weight:700;color:#111">' + csTitle + '</div>'
        +     '<div style="font-size:0.78rem;color:#94a3b8">' + csSub + '</div>'
        +   '</div>'
        +   '<button onclick="localStorage.setItem(\'sh_checklist_done\',\'1\');App.Router.refresh()" style="font-size:0.7rem;color:#94a3b8;background:none;border:none;cursor:pointer;text-decoration:underline">Dismiss</button>'
        + '</div>'
        + '<div style="display:grid;grid-template-columns:1fr 1fr;gap:0.5rem">';

      checkItems.forEach(function(item) {
        html += '<button onclick="App.Dashboard._checklistGo(\'' + item.key + '\',\'' + item.page + '\')" '
          + 'style="display:flex;align-items:center;gap:0.5rem;padding:0.6rem 0.85rem;border-radius:10px;border:1px solid ' + (item.done ? '#bbf7d0' : '#e2e8f0') + ';background:' + (item.done ? '#f0fdf4' : '#fff') + ';cursor:pointer;text-align:left;transition:all 0.15s;font-family:inherit" '
          + 'onmouseover="this.style.borderColor=\'var(--gold)\'" onmouseout="this.style.borderColor=\'' + (item.done ? '#bbf7d0' : '#e2e8f0') + '\'">'
          + '<span style="width:18px;height:18px;border-radius:50%;border:2px solid ' + (item.done ? '#22c55e' : '#d1d5db') + ';background:' + (item.done ? '#22c55e' : '#fff') + ';display:flex;align-items:center;justify-content:center;flex-shrink:0">'
          + (item.done ? '<svg width="10" height="10" fill="none" stroke="#fff" stroke-width="3" viewBox="0 0 24 24"><path d="M5 13l4 4L19 7"/></svg>' : '')
          + '</span>'
          + '<span style="font-size:0.8rem;font-weight:600;color:' + (item.done ? '#15803d' : '#374151') + '">' + item.label + '</span>'
          + '</button>';
      });

      html += '</div>';
      if (allDone) {
        html += '<div style="margin-top:0.75rem;text-align:center;font-size:0.78rem;color:#15803d;font-weight:600">All done! You\'re all set.</div>';
      }
      html += '</div>';
    }

    // ── Alert banner ──────────────────────────────────────────────────────────
    if (overdueInvs.length > 0) {
      html += '<div style="padding:0.85rem 1.1rem;background:#fef2f2;border:1px solid #fecaca;border-left:3px solid #dc2626;border-radius:12px;display:flex;align-items:center;gap:0.75rem">'
        + '<div style="flex:1;font-size:0.83rem;color:#991b1b"><strong>Payment overdue</strong> — ' + overdueInvs.length + ' invoice' + (overdueInvs.length !== 1 ? 's are' : ' is') + ' past due.</div>'
        + '<button onclick="App.Router.navigate(\'billing\')" style="font-size:0.73rem;font-weight:700;color:#dc2626;background:#fff;border:1px solid #fecaca;border-radius:7px;padding:0.3rem 0.7rem;cursor:pointer;white-space:nowrap">Pay Now</button></div>';
    } else if (dueSoonInvs.length > 0) {
      html += '<div style="padding:0.85rem 1.1rem;background:#fffbeb;border:1px solid #fde68a;border-left:3px solid #d97706;border-radius:12px;display:flex;align-items:center;gap:0.75rem">'
        + '<div style="flex:1;font-size:0.83rem;color:#92400e"><strong>Payment due soon</strong> — ' + dueSoonInvs.length + ' invoice' + (dueSoonInvs.length !== 1 ? 's' : '') + ' due within 7 days.</div>'
        + '<button onclick="App.Router.navigate(\'billing\')" style="font-size:0.73rem;font-weight:700;color:#d97706;background:#fff;border:1px solid #fde68a;border-radius:7px;padding:0.3rem 0.7rem;cursor:pointer;white-space:nowrap">View</button></div>';
    }

    // ── No children: check for pending enrollments, show register-your-child form ──
    // This kicks in when a parent self-registered and verified their email
    // but hasn't enrolled any children yet, OR has pending enrollments that
    // admin hasn't approved.
    if (myStudents.length === 0) {
      // Check if the parent already has pending enrollment requests.
      var pendingEnrollments = (s.registrations || []).filter(function(r) {
        return r.type === 'enrollment' && r.status === 'pending' && r.email === App.clientParent;
      });

      html += '<div style="background:#fff;border-radius:16px;border:1px solid rgba(201,162,39,0.2);padding:2.5rem 2rem;max-width:560px;margin:1rem auto 0">'
        + '<div style="text-align:center;margin-bottom:1.5rem">'
        +   '<div style="width:56px;height:56px;border-radius:50%;background:rgba(201,162,39,0.08);display:flex;align-items:center;justify-content:center;margin:0 auto 1rem">'
        +     '<svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="#C9A227" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M16 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="8.5" cy="7" r="4"/><line x1="20" y1="8" x2="20" y2="14"/><line x1="23" y1="11" x2="17" y2="11"/></svg>'
        +   '</div>'
        +   '<h2 style="font-family:var(--serif);font-size:1.35rem;font-weight:700;color:#0a0a0a;margin:0 0 0.35rem">Register your child</h2>'
        +   '<p style="color:#64748b;font-size:0.85rem;line-height:1.5;margin:0 auto;max-width:380px">Fill in your child\'s details below. Once our team reviews and approves, you\'ll see their schedule, billing, and feedback here.</p>'
        + '</div>';

      // Show pending enrollment requests if any.
      if (pendingEnrollments.length > 0) {
        html += '<div style="margin-bottom:1.5rem;padding:0.85rem 1rem;background:#fffbeb;border:1px solid #fde68a;border-radius:10px">'
          + '<div style="font-size:0.72rem;font-weight:700;color:#92400e;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.35rem">Pending enrolments</div>';
        pendingEnrollments.forEach(function(r) {
          html += '<div style="font-size:0.82rem;color:#92400e;padding:0.25rem 0">' + App.Utils.esc(r.studentFirstName || '') + ' ' + App.Utils.esc(r.studentLastName || '') + ' <span style="font-size:0.7rem;color:#94a3b8">— submitted ' + App.Utils.formatDate(r.submittedOn) + '</span></div>';
        });
        html += '</div>';
      }

      // Enrollment form
      html += '<form id="enroll-child-form" style="display:flex;flex-direction:column;gap:0.85rem">'
        + '<div style="display:grid;grid-template-columns:1fr 1fr;gap:0.75rem">'
        +   '<div><label style="display:block;font-size:0.68rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.3rem">First Name *</label><input name="studentFirstName" required style="width:100%;padding:0.55rem 0.75rem;border:1px solid #e2e8f0;border-radius:8px;font-size:0.85rem;outline:none;font-family:inherit" onfocus="this.style.borderColor=\'var(--gold)\';this.style.boxShadow=\'0 0 0 3px rgba(201,162,39,0.1)\'" onblur="this.style.borderColor=\'#e2e8f0\';this.style.boxShadow=\'none\'"></div>'
        +   '<div><label style="display:block;font-size:0.68rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.3rem">Last Name</label><input name="studentLastName" style="width:100%;padding:0.55rem 0.75rem;border:1px solid #e2e8f0;border-radius:8px;font-size:0.85rem;outline:none;font-family:inherit" onfocus="this.style.borderColor=\'var(--gold)\';this.style.boxShadow=\'0 0 0 3px rgba(201,162,39,0.1)\'" onblur="this.style.borderColor=\'#e2e8f0\';this.style.boxShadow=\'none\'"></div>'
        + '</div>'
        + '<div style="display:grid;grid-template-columns:1fr 1fr;gap:0.75rem">'
        +   '<div><label style="display:block;font-size:0.68rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.3rem">Date of Birth</label><input name="studentDob" type="date" style="width:100%;padding:0.55rem 0.75rem;border:1px solid #e2e8f0;border-radius:8px;font-size:0.85rem;outline:none;font-family:inherit"></div>'
        +   '<div><label style="display:block;font-size:0.68rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.3rem">Gender</label><select name="studentGender" style="width:100%;padding:0.55rem 0.75rem;border:1px solid #e2e8f0;border-radius:8px;font-size:0.85rem;outline:none;font-family:inherit;background:#fff"><option value="">Select...</option><option>Male</option><option>Female</option></select></div>'
        + '</div>'
        + '<div style="display:grid;grid-template-columns:1fr 1fr;gap:0.75rem">'
        +   '<div><label style="display:block;font-size:0.68rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.3rem">School</label><input name="schoolName" placeholder="e.g. SK Taman Melawati" style="width:100%;padding:0.55rem 0.75rem;border:1px solid #e2e8f0;border-radius:8px;font-size:0.85rem;outline:none;font-family:inherit"></div>'
        +   '<div><label style="display:block;font-size:0.68rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.3rem">Year / Grade</label><select name="yearGrade" style="width:100%;padding:0.55rem 0.75rem;border:1px solid #e2e8f0;border-radius:8px;font-size:0.85rem;outline:none;font-family:inherit;background:#fff"><option value="">Select...</option><optgroup label="Primary"><option>Standard 1</option><option>Standard 2</option><option>Standard 3</option><option>Standard 4</option><option>Standard 5</option><option>Standard 6</option></optgroup><optgroup label="Secondary"><option>Form 1</option><option>Form 2</option><option>Form 3</option><option>Form 4</option><option>Form 5</option></optgroup><optgroup label="Pre-school"><option>Pre-school (4-5)</option><option>Pre-school (5-6)</option></optgroup></select></div>'
        + '</div>'
        + '<div><label style="display:block;font-size:0.68rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.3rem">Subjects Interested In</label><input name="subjectInterest" placeholder="e.g. Mathematics, English, Science" style="width:100%;padding:0.55rem 0.75rem;border:1px solid #e2e8f0;border-radius:8px;font-size:0.85rem;outline:none;font-family:inherit"></div>'
        + '<div><label style="display:block;font-size:0.68rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.3rem">Notes <span style="font-weight:400;text-transform:none;letter-spacing:0;color:#94a3b8">- optional</span></label><textarea name="notes" rows="2" placeholder="Learning needs, allergies, or anything we should know" style="width:100%;padding:0.55rem 0.75rem;border:1px solid #e2e8f0;border-radius:8px;font-size:0.85rem;outline:none;font-family:inherit;resize:vertical"></textarea></div>'
        + '<div><label style="display:block;font-size:0.68rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.3rem">Referral Code <span style="font-weight:400;text-transform:none;letter-spacing:0;color:#94a3b8">- optional</span></label><input name="referralCode" placeholder="SH-XXXX" maxlength="10" style="width:100%;padding:0.55rem 0.75rem;border:1px solid #e2e8f0;border-radius:8px;font-size:0.85rem;outline:none;font-family:inherit;text-transform:uppercase;letter-spacing:0.05em"><p style="margin:0.3rem 0 0;font-size:0.7rem;color:#94a3b8">Got a code from a friend? Their family gets RM10/month off when your child stays for 3 months.</p></div>'
        + '<button type="submit" id="enroll-submit-btn" style="width:100%;padding:0.7rem;background:var(--gold,#C9A227);color:#0a0a0a;font-weight:700;font-size:0.82rem;border:none;border-radius:8px;cursor:pointer;transition:opacity 0.15s;font-family:inherit">Submit enrolment request</button>'
        + '</form>'
        + '</div>';

      // Wire the form after render
      setTimeout(function() {
        var form = document.getElementById('enroll-child-form');
        if (!form) return;
        form.addEventListener('submit', async function(e) {
          e.preventDefault();
          var btn = document.getElementById('enroll-submit-btn');
          btn.disabled = true;
          btn.textContent = 'Submitting...';
          var fd = new FormData(e.target);
          var body = {};
          fd.forEach(function(v, k) { body[k] = v; });
          body.referralCode = (body.referralCode || '').trim().toUpperCase();
          try {
            var result = await App.Api.post('/api/enrollment-requests', body);
            if (result && result.codeWarning) {
              App.Utils.showToast(result.codeWarning, 'warning', 10000);
            } else {
              App.Utils.showToast(result && result.message || 'Enrolment request submitted.', 'success');
            }
            await App.Api.loadSnapshot();
            App.Router.refresh();
          } catch(err) {
            btn.disabled = false;
            btn.textContent = 'Submit enrolment request';
          }
        });
      }, 0);

      return html;
    }

    // ── Children Cards ────────────────────────────────────────────────────────
    html += '<div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(420px,1fr));gap:1.25rem">';

    myStudents.forEach(function(stu) {
      var stuClasses = classes.filter(function(c) { return (stu.enrolledClasses || []).indexOf(c.id) > -1; });
      var stuClassIds = stuClasses.map(function(c) { return c.id; });

      // Attendance: present in last 30 days vs total sessions
      var stuAttend = attendance.filter(function(a) {
        return a.personId === stu.id && a.personType === 'student' && a.date >= cutoff30Str;
      });
      var presentCount = stuAttend.filter(function(a) { return a.status === 'Present'; }).length;
      var totalSessions = stuAttend.length;

      // Next class: find next upcoming class by matching days of week
      var nextClass = null;
      var nextClassDay = '';
      for (var d = 0; d < 7; d++) {
        var checkIdx = (todayIdx + d) % 7;
        var checkDay = dayOrder[checkIdx];
        var checkDate = App.Utils.localDate(new Date(now.getFullYear(), now.getMonth(), now.getDate() + d));
        var candidates = stuClasses.filter(function(c) { return App.Utils.scheduleOn(c, schedVersions, checkDate).day === checkDay; })
          .sort(function(a, b) { return a.time.localeCompare(b.time); });
        if (d === 0) {
          // Today: only classes that haven't ended yet
          var nowTime = now.getHours().toString().padStart(2, '0') + ':' + now.getMinutes().toString().padStart(2, '0');
          candidates = candidates.filter(function(c) { return (c.endTime || c.time) > nowTime; });
        }
        if (candidates.length > 0) {
          nextClass = candidates[0];
          nextClassDay = d === 0 ? 'Today' : d === 1 ? 'Tomorrow' : checkDay.slice(0, 3);
          break;
        }
      }

      // Replacement balance
      var stuCredits = repCredits.filter(function(rc) { return rc.studentId === stu.id; });
      var earnedMin = 0, usedMin = 0;
      stuCredits.forEach(function(rc) {
        if (rc.type === 'earned') earnedMin += rc.minutes;
        else usedMin += rc.minutes;
      });
      var repBalance = earnedMin - usedMin;

      // Billing: outstanding for this student
      var stuInvoices = myInvoices.filter(function(i) { return i.studentId === stu.id; });
      var outstanding = stuInvoices.filter(function(i) { return i.status === 'Overdue' || i.status === 'Unpaid'; })
        .reduce(function(a, i) { return a + i.amount; }, 0);

      // Latest feedback for any class this child is enrolled in
      var latestFb = null;
      var latestFbTeacher = '';
      var latestFbNote = '';
      // Check studentNotes first for personalized feedback
      feedbacks.filter(function(fb) { return stuClassIds.indexOf(fb.classId) > -1; })
        .sort(function(a, b) { return b.date.localeCompare(a.date); })
        .some(function(fb) {
          // Look for a note specific to this child
          var childNote = (fb.studentNotes || []).find(function(sn) { return sn.studentId === stu.id && sn.note && sn.note.trim(); });
          if (childNote) {
            latestFb = fb;
            latestFbNote = childNote.note;
            return true;
          }
          // Fall back to general class notes
          if (!latestFb && fb.notes && fb.notes.trim()) {
            latestFb = fb;
            latestFbNote = fb.notes;
          }
          return false;
        });
      if (latestFb) {
        var fbTeacher = staff.find(function(t) { return t.id === latestFb.teacherId; });
        latestFbTeacher = fbTeacher ? App.Utils.esc(fbTeacher.name) : 'Teacher';
      }

      // Subjects list from enrolled classes
      var subjects = stuClasses.map(function(c) { return App.Utils.esc(c.name); }).slice(0, 3).join(', ');
      if (stuClasses.length > 3) subjects += ' +' + (stuClasses.length - 3);

      // Status color
      var statusColor = stu.status === 'Active' ? '#15803d' : stu.status === 'New' ? '#2563eb' : '#94a3b8';
      var statusBg    = stu.status === 'Active' ? '#f0fdf4' : stu.status === 'New' ? '#eff6ff' : '#f8fafc';

      // Avatar initial
      var initial = App.Utils.esc(stu.firstName).charAt(0).toUpperCase();

      html += '<div onclick="App.Students._viewModal(\'' + stu.id + '\')" style="background:#fff;border-radius:16px;border:1px solid rgba(0,0,0,0.07);box-shadow:0 2px 8px rgba(0,0,0,0.04);cursor:pointer;transition:box-shadow 0.2s,transform 0.2s;overflow:hidden" onmouseover="this.style.boxShadow=\'0 4px 16px rgba(0,0,0,0.1)\';this.style.transform=\'translateY(-2px)\'" onmouseout="this.style.boxShadow=\'0 2px 8px rgba(0,0,0,0.04)\';this.style.transform=\'none\'">'

        // Header row: avatar + name + status
        + '<div style="padding:1.25rem 1.5rem 0.75rem;display:flex;align-items:center;gap:0.9rem">'
        +   '<div style="width:3rem;height:3rem;border-radius:12px;background:var(--gold-dim);color:var(--gold);font-weight:800;font-size:1.2rem;display:flex;align-items:center;justify-content:center;flex-shrink:0;font-family:var(--serif)">' + initial + '</div>'
        +   '<div style="flex:1;min-width:0">'
        +     '<div style="font-size:1.05rem;font-weight:700;color:#111;font-family:var(--serif)">' + App.Utils.esc(stu.firstName) + ' ' + App.Utils.esc(stu.lastName) + '</div>'
        +     '<div style="font-size:0.78rem;color:#64748b;margin-top:2px">' + App.Utils.esc(stu.grade || '') + (subjects ? ' · ' + subjects : '') + '</div>'
        +     (function() {
                var todayAtt = attendance.find(function(a) {
                  return a.personId === stu.id && a.date === today && a.personType === 'student';
                });
                var hasClassToday = stuClasses.some(function(c) { return App.Utils.scheduleOn(c, schedVersions, today).day === todayDay; });
                if (!hasClassToday) return '';
                var todayStatus = todayAtt
                  ? (todayAtt.status === 'Absent' ? 'absent' : todayAtt.checkIn ? 'present' : 'pending')
                  : 'none';
                if (todayStatus === 'present') {
                  var timeStr = todayAtt.checkIn || '';
                  var label = timeStr ? 'Checked in at ' + timeStr : 'In class';
                  return '<div style="font-size:0.72rem;margin-top:3px;display:flex;align-items:center;gap:4px">'
                    + '<span style="width:6px;height:6px;border-radius:50%;background:#22c55e;flex-shrink:0"></span>'
                    + '<span style="color:#15803d;font-weight:600">' + App.Utils.esc(label) + '</span></div>';
                } else if (todayStatus === 'absent') {
                  return '<div style="font-size:0.72rem;margin-top:3px;display:flex;align-items:center;gap:4px">'
                    + '<span style="width:6px;height:6px;border-radius:50%;background:#ef4444;flex-shrink:0"></span>'
                    + '<span style="color:#dc2626;font-weight:600">Absent today</span></div>';
                } else {
                  return '<div style="font-size:0.72rem;margin-top:3px;display:flex;align-items:center;gap:4px">'
                    + '<span style="width:6px;height:6px;border-radius:50%;background:#f59e0b;flex-shrink:0"></span>'
                    + '<span style="color:#d97706;font-weight:600">Not checked in yet</span></div>';
                }
              })()
        +   '</div>'
        +   '<span style="font-size:0.68rem;font-weight:700;color:' + statusColor + ';background:' + statusBg + ';padding:3px 10px;border-radius:6px;flex-shrink:0;text-transform:uppercase;letter-spacing:0.04em">' + App.Utils.esc(stu.status) + '</span>'
        + '</div>'

        // Mini stats grid — each tile is a shortcut into the matching page
        + '<div style="padding:0.5rem 1.5rem 1rem;display:grid;grid-template-columns:repeat(4,1fr);gap:0.6rem">'

        // Attendance
        +   '<div onclick="App.Router.navigate(\'attendance\')" style="background:#f0fdf4;border-radius:10px;padding:0.65rem 0.7rem;text-align:center;cursor:pointer;transition:transform 0.12s,box-shadow 0.12s" onmouseover="this.style.transform=\'translateY(-1px)\';this.style.boxShadow=\'0 3px 8px rgba(21,128,61,0.12)\'" onmouseout="this.style.transform=\'none\';this.style.boxShadow=\'none\'">'
        +     '<div style="font-size:0.62rem;font-weight:700;text-transform:uppercase;letter-spacing:0.05em;color:#15803d;margin-bottom:0.3rem">Attendance</div>'
        +     '<div style="font-family:var(--serif);font-size:1.15rem;font-weight:700;color:#15803d">' + presentCount + '/' + totalSessions + '</div>'
        +     '<div style="font-size:0.6rem;color:#64748b;margin-top:0.15rem">Present</div>'
        +   '</div>'

        // Next class
        +   '<div onclick="App.Router.navigate(\'calendar\')" style="background:#eff6ff;border-radius:10px;padding:0.65rem 0.7rem;text-align:center;cursor:pointer;transition:transform 0.12s,box-shadow 0.12s" onmouseover="this.style.transform=\'translateY(-1px)\';this.style.boxShadow=\'0 3px 8px rgba(37,99,235,0.12)\'" onmouseout="this.style.transform=\'none\';this.style.boxShadow=\'none\'">'
        +     '<div style="font-size:0.62rem;font-weight:700;text-transform:uppercase;letter-spacing:0.05em;color:#2563eb;margin-bottom:0.3rem">Next Class</div>'
        +     (nextClass
              ? '<div style="font-size:0.82rem;font-weight:700;color:#2563eb">' + nextClassDay + ' ' + App.Utils.formatTime(nextClass.time) + '</div>'
                + '<div style="font-size:0.6rem;color:#64748b;margin-top:0.15rem;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + App.Utils.esc(nextClass.name) + '</div>'
              : '<div style="font-size:0.78rem;color:#94a3b8">None</div>')
        +   '</div>'

        // Replacement balance — opens the student modal already switched to
        // the Replacements tab so parents see the credit breakdown without
        // having to hunt for the right tab.
        +   '<div onclick="if(App.Students){App.Students._viewModal(\'' + stu.id + '\');App.Students._switchTab(\'replacements\');}" style="background:' + (repBalance > 0 ? '#fffbeb' : '#f8fafc') + ';border-radius:10px;padding:0.65rem 0.7rem;text-align:center;cursor:pointer;transition:transform 0.12s,box-shadow 0.12s" onmouseover="this.style.transform=\'translateY(-1px)\';this.style.boxShadow=\'0 3px 8px rgba(146,64,14,0.12)\'" onmouseout="this.style.transform=\'none\';this.style.boxShadow=\'none\'">'
        +     '<div style="font-size:0.62rem;font-weight:700;text-transform:uppercase;letter-spacing:0.05em;color:' + (repBalance > 0 ? '#92400e' : '#94a3b8') + ';margin-bottom:0.3rem">Replace</div>'
        +     '<div style="font-family:var(--serif);font-size:1.15rem;font-weight:700;color:' + (repBalance > 0 ? '#92400e' : '#64748b') + '">' + repBalance + 'cr</div>'
        +     '<div style="font-size:0.6rem;color:#64748b;margin-top:0.15rem">' + (repBalance > 0 ? 'credits' : 'none') + '</div>'
        +   '</div>'

        // Billing
        +   '<div onclick="App.Router.navigate(\'billing\')" style="background:' + (outstanding > 0 ? '#fef2f2' : '#f0fdf4') + ';border-radius:10px;padding:0.65rem 0.7rem;text-align:center;cursor:pointer;transition:transform 0.12s,box-shadow 0.12s" onmouseover="this.style.transform=\'translateY(-1px)\';this.style.boxShadow=\'0 3px 8px rgba(0,0,0,0.08)\'" onmouseout="this.style.transform=\'none\';this.style.boxShadow=\'none\'">'
        +     '<div style="font-size:0.62rem;font-weight:700;text-transform:uppercase;letter-spacing:0.05em;color:' + (outstanding > 0 ? '#991b1b' : '#15803d') + ';margin-bottom:0.3rem">Billing</div>'
        +     '<div style="font-family:var(--serif);font-size:1.05rem;font-weight:700;color:' + (outstanding > 0 ? '#991b1b' : '#15803d') + '">RM ' + outstanding.toFixed(0) + '</div>'
        +     '<div style="font-size:0.6rem;color:#64748b;margin-top:0.15rem">' + (outstanding > 0 ? 'outstanding' : 'Paid up') + '</div>'
        +   '</div>'

        + '</div>';

      // Latest feedback
      if (latestFb) {
        var notePreview = latestFbNote.length > 120 ? latestFbNote.slice(0, 120) + '...' : latestFbNote;
        html += '<div style="padding:0 1.5rem 1.15rem">'
          + '<div style="background:linear-gradient(135deg,#fef9ec 0%,#fff 70%);border:1px solid #fef3c7;border-radius:10px;padding:0.75rem 0.9rem">'
          +   '<div style="font-size:0.8rem;color:#44403c;line-height:1.45;font-style:italic">"' + App.Utils.esc(notePreview) + '"</div>'
          +   '<div style="font-size:0.68rem;color:#94a3b8;margin-top:0.4rem">— ' + latestFbTeacher + ', ' + App.Utils.formatDate(latestFb.date) + '</div>'
          + '</div>'
          + '</div>';
      }

      html += '</div>'; // close child card
    });

    html += '</div>'; // close children grid

    // ── Empty state ───────────────────────────────────────────────────────────
    if (myStudents.length === 0) {
      html += '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);padding:3rem;text-align:center">'
        + '<p style="font-size:1rem;font-weight:600;color:#475569">No children enrolled yet</p>'
        + '<p style="font-size:0.82rem;color:#94a3b8;margin-top:0.4rem">Contact the centre to register your child.</p>'
        + '</div>';
    }

    // ── Payments Due ──────────────────────────────────────────────────────────
    var unpaidInvs = myInvoices.filter(function(i) { return i.status === 'Overdue' || i.status === 'Unpaid'; })
      .sort(function(a, b) { return a.dueDate.localeCompare(b.dueDate); });

    if (unpaidInvs.length > 0) {
      html += '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);padding:1.25rem 1.5rem;margin-top:1rem">'
        + '<div style="font-size:0.72rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.75rem">Payments Due</div>';

      unpaidInvs.forEach(function(inv) {
        var stu = myStudents.find(function(st) { return st.id === inv.studentId; });
        var stuName = stu ? App.Utils.esc(stu.firstName) : '';
        var isOverdue = inv.status === 'Overdue';
        html += '<div style="display:flex;align-items:center;gap:0.75rem;padding:0.6rem 0;border-bottom:1px solid #f4f4f2">'
          + '<div style="flex:1;min-width:0">'
          +   '<div style="font-size:0.82rem;font-weight:600;color:#111">' + stuName + (inv.description ? ' — ' + App.Utils.esc(inv.description) : '') + '</div>'
          +   '<div style="font-size:0.68rem;color:#94a3b8;margin-top:2px">Due ' + App.Utils.formatDate(inv.dueDate) + (isOverdue ? ' <span style="color:#dc2626;font-weight:700">OVERDUE</span>' : '') + '</div>'
          + '</div>'
          + '<div style="font-size:0.95rem;font-weight:700;color:' + (isOverdue ? '#dc2626' : '#111') + ';white-space:nowrap;font-family:var(--serif)">RM ' + inv.amount.toFixed(2) + '</div>'
          + '<button onclick="event.stopPropagation();App.Router.navigate(\'billing\')" style="font-size:0.72rem;font-weight:700;color:#0a0a0a;background:var(--gold);border:none;border-radius:7px;padding:0.35rem 0.75rem;cursor:pointer;white-space:nowrap">Pay</button>'
          + '</div>';
      });
      html += '</div>';
    }

    // ── Teacher Notes ───────────────────────────────────────────────────────────
    var allEnrolledIds = [];
    myStudents.forEach(function(st) { (st.enrolledClasses || []).forEach(function(id) { if (allEnrolledIds.indexOf(id) === -1) allEnrolledIds.push(id); }); });

    var recentFeedbacks = feedbacks.filter(function(fb) { return allEnrolledIds.indexOf(fb.classId) > -1 && (fb.notes || (fb.studentNotes && fb.studentNotes.length)); })
      .sort(function(a, b) { return b.date.localeCompare(a.date); })
      .slice(0, 5);

    html += '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);padding:1.25rem 1.5rem;margin-top:1rem">'
      + '<div style="font-size:0.72rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.75rem">Teacher Notes</div>';

    if (recentFeedbacks.length === 0) {
      html += '<p style="color:#94a3b8;font-size:0.82rem;text-align:center;padding:1rem 0">No teacher notes yet</p>';
    } else {
      recentFeedbacks.forEach(function(fb) {
        var cls = classes.find(function(c) { return c.id === fb.classId; });
        var teacher = staff.find(function(t) { return t.id === fb.teacherId; });
        var teacherName = teacher ? App.Utils.esc(teacher.name) : 'Teacher';
        var clsName = cls ? App.Utils.esc(cls.name) : '';

        // Prefer student-specific notes for parent's children, fall back to general notes
        var noteText = '';
        myStudents.some(function(st) {
          var childNote = (fb.studentNotes || []).find(function(sn) { return sn.studentId === st.id && sn.note && sn.note.trim(); });
          if (childNote) { noteText = childNote.note; return true; }
          return false;
        });
        if (!noteText) noteText = fb.notes || '';
        if (!noteText.trim()) return;

        var preview = noteText.length > 100 ? noteText.slice(0, 100) + '...' : noteText;

        html += '<div style="border-left:3px solid var(--gold);padding:0.6rem 0 0.6rem 0.85rem;margin-bottom:0.6rem">'
          + '<div style="font-size:0.8rem;color:#44403c;line-height:1.45;font-style:italic">"' + App.Utils.esc(preview) + '"</div>'
          + '<div style="font-size:0.68rem;color:#94a3b8;margin-top:0.3rem">' + teacherName + ' · ' + clsName + ' · ' + App.Utils.formatDate(fb.date) + '</div>'
          + '</div>';
      });
    }
    html += '<button onclick="App.Router.navigate(\'progress\')" class="dash-link" style="display:block;margin-top:0.5rem">View progress reports →</button>'
      + '</div>';

    // ── Announcements (compact, last 3) ─────────────────────────────────────
    var latestAnnounce = (announcements || []).filter(function(a) { return a.status === 'published' || !a.status; })
      .slice().sort(function(a, b) { return b.createdOn.localeCompare(a.createdOn); }).slice(0, 3);

    html += '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);padding:1.25rem 1.5rem;margin-top:1rem">'
      + '<div style="font-size:0.72rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.75rem">Announcements</div>';

    if (latestAnnounce.length === 0) {
      html += '<p style="color:#94a3b8;font-size:0.82rem;text-align:center;padding:1rem 0">No announcements</p>';
    } else {
      latestAnnounce.forEach(function(a) {
        var tc = { Notice:'#2563eb', Reminder:'#d97706', Urgent:'#dc2626' };
        html += '<div style="display:flex;align-items:center;gap:0.65rem;padding:0.5rem 0;border-bottom:1px solid #f4f4f2">'
          + '<span style="width:6px;height:6px;border-radius:50%;background:' + (tc[a.type] || '#2563eb') + ';flex-shrink:0"></span>'
          + '<div style="flex:1;min-width:0">'
          +   '<div style="font-size:0.8rem;font-weight:600;color:#111;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + App.Utils.esc(a.title) + '</div>'
          + '</div>'
          + '<span style="font-size:0.68rem;color:#94a3b8;flex-shrink:0">' + App.Utils.formatDate(a.createdOn) + '</span>'
          + '</div>';
      });
    }
    html += '<button onclick="App.Router.navigate(\'communication\')" class="dash-link" style="display:block;margin-top:0.5rem">View all →</button>'
      + '</div>';

    // ── Refer a friend ──────────────────────────────────────────────────────
    html += _referAFriendCard(s);

    return html;
  }

  // Builds the "Refer a friend" tile from snapshot data. The tile self-loads
  // its own referral context (code + earned credits) from the family API on
  // first render so we don't have to thread it through the snapshot. Falls
  // back gracefully if the API isn't reachable.
  function _referAFriendCard(state) {
    var families = state.families || [];
    var rewards  = state.referralRewards || [];
    var myFamily = families.find(function(f) { return f.contact === App.clientParent; });
    if (!myFamily) return '';

    var myRewards = rewards.filter(function(r) { return r.referrerFamilyId === myFamily.id; });
    var creditsRemaining = myRewards
      .filter(function(r) { return r.status === 'earned'; })
      .reduce(function(a, r) { return a + (r.creditsRemaining || 0); }, 0);
    var pendingCount = myRewards.filter(function(r) { return r.status === 'pending'; }).length;

    var code = myFamily.referralCode || '';
    var codeDisplay = code ? App.Utils.esc(code) : '<span style="color:#94a3b8;font-size:0.78rem;font-family:var(--sans)">Generating...</span>';

    var copyAttr = code
      ? 'data-copy="' + App.Utils.esc(code) + '" onclick="event.stopPropagation();App.Utils.copyFrom(this,\'Code copied — share it with a friend\')"'
      : '';

    var creditPill = '';
    if (creditsRemaining > 0) {
      creditPill = '<span style="display:inline-flex;align-items:center;gap:0.35rem;padding:0.3rem 0.7rem;background:rgba(201,162,39,0.12);border:1px solid rgba(201,162,39,0.3);border-radius:999px;font-size:0.72rem;font-weight:700;color:#92400e">Active credit · ' + creditsRemaining + ' invoice' + (creditsRemaining !== 1 ? 's' : '') + ' remaining</span>';
    } else if (pendingCount > 0) {
      creditPill = '<span style="display:inline-flex;align-items:center;gap:0.35rem;padding:0.3rem 0.7rem;background:#f1f5f9;border:1px solid #e2e8f0;border-radius:999px;font-size:0.72rem;font-weight:700;color:#475569">' + pendingCount + ' referral' + (pendingCount !== 1 ? 's' : '') + ' in progress</span>';
    }

    return '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);padding:1.25rem 1.5rem;margin-top:1rem">'
      + '<div style="font-size:0.72rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.75rem">Refer a Friend</div>'
      + '<div style="display:flex;align-items:center;justify-content:space-between;gap:1rem;flex-wrap:wrap">'
      +   '<div style="flex:1;min-width:200px">'
      +     '<div style="font-family:var(--serif);font-size:1.6rem;font-weight:700;letter-spacing:0.04em;color:var(--gold);line-height:1">' + codeDisplay + '</div>'
      +     '<p style="margin:0.5rem 0 0;font-size:0.78rem;color:#64748b;line-height:1.45">Share your code with a friend. When their child stays for 3 months, you get <strong>RM10 off for 3 months</strong>.</p>'
      +     (creditPill ? '<div style="margin-top:0.65rem">' + creditPill + '</div>' : '')
      +   '</div>'
      + (code ? '<button ' + copyAttr + ' style="padding:0.55rem 1.1rem;font-size:0.78rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Copy code</button>' : '')
      + '</div>'
      + '</div>';
  }

  // ── Helpers ───────────────────────────────────────────────────────────────────

  // card() opens a card; with title: opens outer div + inner padding div (caller needs two </div>s); without title: one div
  function card(title, page) {
    var clickStyle = page ? 'cursor:pointer' : '';
    var clickAttr = page ? ' onclick="App.Router.navigate(\'' + page + '\')"' : '';
    var header = title
      ? '<div class="dash-card-header" style="' + clickStyle + '"' + clickAttr + '>' + title + '</div><div class="dash-card-body">'
      : '<div style="padding:1.25rem 1.5rem">';
    return '<div class="dash-card">' + header;
  }

  function stat(label, value, isCurr, color, goldBorder, bg, page) {
    var display = isCurr
      ? 'RM\u00a0' + value.toFixed(2).replace(/\B(?=(\d{3})+(?!\d))/g, ',')
      : String(value);
    var dataAttr = isCurr
      ? 'data-count="' + value + '" data-currency="1"'
      : 'data-count="' + value + '"';
    var bgGrad = bg ? 'background:linear-gradient(135deg,' + bg + ' 0%,#ffffff 70%)' : 'background:#fff';
    var borderTop = goldBorder ? ';border-top:2.5px solid var(--gold)' : '';
    var clickStyle = page ? ';cursor:pointer' : '';
    var clickAttr = page ? ' onclick="App.Router.navigate(\'' + page + '\')"' : '';
    return '<div class="dash-stat" style="' + bgGrad + borderTop + clickStyle + '"' + clickAttr + '>'
      + '<p class="dash-stat-label" style="color:' + color + '">' + label + '</p>'
      + '<p ' + dataAttr + ' class="dash-stat-number" style="color:' + color + '">' + display + '</p>'
      + '</div>';
  }

  function qbtn(label, onclick, primary) {
    return '<button onclick="' + onclick + '" style="padding:0.45rem 0.9rem;font-size:0.78rem;font-weight:700;border-radius:8px;cursor:pointer;transition:opacity 0.15s;'
      + (primary ? 'background:var(--gold);color:#0a0a0a;border:none;' : 'background:#fff;color:#374151;border:1px solid #e2e8f0;')
      + '" onmouseover="this.style.opacity=\'0.8\'" onmouseout="this.style.opacity=\'1\'">' + label + '</button>';
  }

  function _attnItems(pendingRegs, overdueInvs, dueSoonInvs, newStudents, students, attendance, staff, classes, today, todayDay, now, invoices) {
    var DOTS = { error:'#ef4444', warning:'#f59e0b', info:'#3b82f6' };
    var state = App.Store.get();
    var registrations = state.registrations || [];
    var families = state.families || [];
    var attn = [];

    // Break down pending registrations by type for clearer action items.
    var pendingEnrollments = registrations.filter(function(r) { return r.status === 'pending' && r.type === 'enrollment'; }).length;
    var pendingTeachers = registrations.filter(function(r) { return r.status === 'pending' && r.type === 'teacher'; }).length;
    var pendingOther = pendingRegs - pendingEnrollments - pendingTeachers;

    if (pendingEnrollments > 0) attn.push({ sev:'info', title: pendingEnrollments + ' child enrolment' + (pendingEnrollments!==1?'s':'') + ' to review', page:'students' });
    if (pendingTeachers > 0)    attn.push({ sev:'info', title: pendingTeachers + ' teacher application' + (pendingTeachers!==1?'s':''), page:'students' });
    if (pendingOther > 0)       attn.push({ sev:'info', title: pendingOther + ' pending registration' + (pendingOther!==1?'s':''), page:'students' });

    // Payments awaiting verification (parent submitted "I've Paid" but admin
    // hasn't confirmed yet).
    var awaitingVerification = (invoices || []).filter(function(i) { return i.status === 'Pending Verification'; });
    if (awaitingVerification.length > 0) attn.push({ sev:'warning', title: awaitingVerification.length + ' payment' + (awaitingVerification.length!==1?'s':'') + ' awaiting verification', page:'billing' });

    if (overdueInvs.length > 0) attn.push({ sev:'error',  title: overdueInvs.length + ' overdue invoice' + (overdueInvs.length!==1?'s':''), page:'billing' });
    if (dueSoonInvs.length > 0) attn.push({ sev:'warning',title: dueSoonInvs.length + ' payment' + (dueSoonInvs.length!==1?'s':'') + ' due this week', page:'billing' });
    if (newStudents > 0)        attn.push({ sev:'info',   title: newStudents + ' new student' + (newStudents!==1?'s':'') + ' to activate', page:'students' });

    // Orphan parents: families with no active/new students linked. These are
    // parents who self-registered but either haven't submitted an enrollment
    // request yet or whose enrollment was never approved.
    var studentFamilyIds = {};
    (students || []).forEach(function(s) { if (s.familyId && (s.status === 'Active' || s.status === 'New')) studentFamilyIds[s.familyId] = true; });
    var orphanFamilies = families.filter(function(f) { return !studentFamilyIds[f.id]; });
    if (orphanFamilies.length > 0) attn.push({ sev:'info', title: orphanFamilies.length + ' parent' + (orphanFamilies.length!==1?'s':'') + ' without children linked', sub: 'May need follow-up', page:'students' });

    // Churn risk: Active students with no Present record in last 14 days
    if (students && attendance) {
      var cutoff = new Date(now); cutoff.setDate(cutoff.getDate() - 14);
      var cutoffStr = App.Utils.localDate(cutoff);
      var atRisk = students.filter(function(stu) {
        if (stu.status !== 'Active') return false;
        var recentPresent = attendance.filter(function(a) {
          return a.personId === stu.id && a.personType === 'student' && a.status === 'Present' && a.date >= cutoffStr;
        });
        return recentPresent.length === 0;
      });
      if (atRisk.length > 0) attn.push({ sev:'warning', title: atRisk.length + ' student' + (atRisk.length!==1?'s':'') + ' at churn risk', sub: 'No attendance in 14+ days', page:'students' });
    }

    // Tutor not checked in for a class starting within 30 min or already started today
    if (classes && attendance && staff && now && todayDay) {
      var in30 = new Date(now); in30.setMinutes(in30.getMinutes() + 30);
      var nowTimeStr = now.getHours().toString().padStart(2,'0') + ':' + now.getMinutes().toString().padStart(2,'0');
      var in30Str = in30.getHours().toString().padStart(2,'0') + ':' + in30.getMinutes().toString().padStart(2,'0');
      var alertTeacherIds = [];
      // Include classes that have already started (time <= nowTimeStr) or start within 30 min (time <= in30Str)
      classes.filter(function(c) { return App.Utils.scheduleOn(c, App.Store.get().scheduleVersions, today).day === todayDay && c.time <= in30Str; })
        .forEach(function(c) { c.teacherIds.forEach(function(tid) { if (alertTeacherIds.indexOf(tid) === -1) alertTeacherIds.push(tid); }); });
      var notCheckedIn = alertTeacherIds.filter(function(tid) {
        return !attendance.find(function(a) { return a.personId === tid && a.personType === 'staff' && a.date === today && a.checkIn; });
      });
      if (notCheckedIn.length > 0) {
        var names = notCheckedIn.map(function(tid) { var st = staff.find(function(x){return x.id===tid;}); return st ? App.Utils.esc(st.name) : tid; }).join(', ');
        attn.push({ sev:'error', title: notCheckedIn.length + ' tutor' + (notCheckedIn.length!==1?'s':'') + ' not checked in', sub: names + ' · class started or within 30 min', page:'staff' });
      }
    }

    // Invoices overdue by more than 7 days
    var sevenDaysAgo = new Date(now); sevenDaysAgo.setDate(sevenDaysAgo.getDate() - 7);
    var longOverdue = (invoices || []).filter(function(inv) {
      if (inv.status !== 'Overdue') return false;
      var due = new Date(inv.dueDate);
      return due < sevenDaysAgo;
    });
    if (longOverdue.length > 0) attn.push({ sev:'error', title: longOverdue.length + ' invoice' + (longOverdue.length!==1?'s':'') + ' overdue 7+ days', sub: 'Requires immediate follow-up', page:'billing' });

    // Pending verification accounts (from snapshot pendingUsers)
    var pendingUsers = state.pendingUsers || [];
    if (pendingUsers.length > 0) attn.push({ sev:'warning', title: pendingUsers.length + ' account' + (pendingUsers.length!==1?'s':'') + ' awaiting email verification', sub: 'Click to verify or resend', action: '_pendingUsersModal' });

    var items = attn;

    if (items.length === 0) {
      return '<div style="padding:1.5rem 0;text-align:center"><div style="font-size:1.3rem;margin-bottom:4px">✓</div><p style="font-size:0.82rem;color:#94a3b8;margin:0">All clear</p></div>';
    }
    return items.map(function(item) {
      var clickAction = item.action
        ? 'App.Dashboard.' + item.action + '()'
        : 'App.Router.navigate(\'' + item.page + '\')';
      return '<div onclick="' + clickAction + '" style="display:flex;align-items:center;gap:0.65rem;padding:0.6rem 0.5rem;margin:0 -0.5rem;border-radius:8px;cursor:pointer;transition:background 0.15s;border-bottom:1px solid #f4f4f2" onmouseover="this.style.background=\'#fafaf8\'" onmouseout="this.style.background=\'transparent\'">'
        + '<span style="width:7px;height:7px;border-radius:50%;background:' + (DOTS[item.sev]||DOTS.info) + ';flex-shrink:0"></span>'
        + '<div style="flex:1;min-width:0">'
        +   '<div style="font-size:0.81rem;font-weight:600;color:#111">' + App.Utils.esc(item.title) + '</div>'
        +   (item.sub ? '<div style="font-size:0.71rem;color:#94a3b8;margin-top:1px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + App.Utils.esc(item.sub) + '</div>' : '')
        + '</div>'
        + '<svg style="width:0.85rem;height:0.85rem;color:#d1d5db;flex-shrink:0" fill="none" stroke="currentColor" stroke-width="2.5" viewBox="0 0 24 24"><path stroke-linecap="round" d="M9 5l7 7-7 7"/></svg>'
        + '</div>';
    }).join('');
  }

  // ── Operations View (cousin's idea) ──────────────────────────────────────────
  function _opsDash() {
    var s          = App.Store.get();
    var students   = s.students   || [];
    var classes    = s.classes    || [];
    var invoices   = s.invoices   || [];
    var staff      = s.staff      || [];
    var attendance = s.attendance || [];
    var announcements = (s.announcements || []).filter(function(a){ return a.status === 'published' || !a.status; });
    var registrations = s.registrations || [];

    var today    = App.Utils.today();
    var todayDay = new Date(today + 'T00:00:00').toLocaleDateString('en-US', { weekday: 'long' });
    var now      = new Date();

    // Sector breakdown
    var categories = ['Academic', 'Non-academic', 'Workshop'];
    var catColors  = { Academic:'#3b82f6', 'Non-academic':'#f59e0b', Workshop:'#10b981' };
    var catBg      = { Academic:'#eff6ff', 'Non-academic':'#fffbeb', Workshop:'#f0fdf4' };

    var sectorCards = categories.map(function(cat) {
      var catClasses  = classes.filter(function(c) { return c.category === cat; });
      var catClassIds = catClasses.map(function(c) { return c.id; });
      var enrolled    = 0;
      catClasses.forEach(function(c) { enrolled += c.enrolled; });
      var catStudents = students.filter(function(stu) {
        return stu.enrolledClasses.some(function(cid) { return catClassIds.indexOf(cid) > -1; });
      });
      var catRevenue  = invoices.filter(function(i) {
        return i.status === 'Paid' && catStudents.some(function(stu) { return stu.id === i.studentId; });
      }).reduce(function(acc, i) { return acc + i.amount; }, 0);
      var todayCount  = catClasses.filter(function(c) { return App.Utils.scheduleOn(c, s.scheduleVersions, today).day === todayDay; }).length;
      return '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);box-shadow:0 1px 3px rgba(0,0,0,0.05);padding:1.1rem 1.2rem;border-top:3px solid ' + catColors[cat] + '">'
        + '<div style="font-size:0.7rem;font-weight:800;text-transform:uppercase;letter-spacing:0.07em;color:' + catColors[cat] + ';margin-bottom:0.7rem">' + cat + '</div>'
        + '<div style="display:grid;grid-template-columns:1fr 1fr;gap:0.75rem">'
        + _miniStat('Classes', catClasses.length)
        + _miniStat('Students', catStudents.length)
        + _miniStat('Today', todayCount)
        + _miniStat('Revenue', 'RM ' + catRevenue.toFixed(0))
        + '</div>'
        + '</div>';
    }).join('');

    // Pending items
    var pendingRegs   = registrations.filter(function(r) { return r.status === 'pending'; });
    var pendingAnns   = (s.announcements||[]).filter(function(a) { return a.status === 'pending_approval'; });
    var overdueInvs   = invoices.filter(function(i) { return i.status === 'Overdue'; });
    var newStudents   = students.filter(function(st) { return st.status === 'New'; });

    var pendingItems = [
      pendingRegs.length > 0   ? { label: pendingRegs.length + ' registration' + (pendingRegs.length!==1?'s':'') + ' pending', page:'students', color:'#3b82f6'   } : null,
      pendingAnns.length > 0   ? { label: pendingAnns.length + ' announcement' + (pendingAnns.length!==1?'s':'') + ' to approve', page:'communication', color:'#f59e0b' } : null,
      overdueInvs.length > 0   ? { label: overdueInvs.length + ' overdue invoice' + (overdueInvs.length!==1?'s':''), page:'billing', color:'#ef4444' } : null,
      newStudents.length > 0   ? { label: newStudents.length + ' new student' + (newStudents.length!==1?'s':'') + ' to activate', page:'students', color:'#6366f1' } : null
    ].filter(Boolean);

    // Today's classes
    var todayClasses  = classes.filter(function(c) { return App.Utils.scheduleOn(c, s.scheduleVersions, today).day === todayDay; }).sort(function(a,b){ return a.time.localeCompare(b.time); });

    // Financial this month
    var thisMonth = today.slice(0,7);
    var monthRevenue  = invoices.filter(function(i) { return i.status==='Paid' && i.paidOn && i.paidOn.startsWith(thisMonth); }).reduce(function(a,i){ return a+i.amount; },0);
    var monthTarget   = invoices.filter(function(i) { return i.createdOn && i.createdOn.startsWith(thisMonth); }).reduce(function(a,i){ return a+i.amount; },0);
    var collectRate   = monthTarget > 0 ? Math.round((monthRevenue/monthTarget)*100) : 0;

    // Staff checked in today
    var staffCheckedIn = attendance.filter(function(a) { return a.personType==='staff' && a.date===today && a.checkIn; }).length;

    return '<div style="max-width:1100px">'
      // Header
      + '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:1.5rem">'
      +   '<div>'
      +     '<h1 style="font-size:1.55rem;font-weight:800;color:#0d0d0d;letter-spacing:-0.03em;margin:0">Operations View</h1>'
      +     '<p style="font-size:0.82rem;color:#94a3b8;margin:0.2rem 0 0">' + _dateFull() + '</p>'
      +   '</div>'
      + '</div>'

      // Sector breakdown
      + '<div style="margin-bottom:0.5rem"><p style="font-size:0.7rem;font-weight:700;text-transform:uppercase;letter-spacing:0.07em;color:#94a3b8;margin:0 0 0.65rem">By Sector</p></div>'
      + '<div style="display:grid;grid-template-columns:repeat(3,1fr);gap:1rem;margin-bottom:1.5rem">'
      + sectorCards
      + '</div>'

      // Pending + Today split
      + '<div style="display:grid;grid-template-columns:1fr 1fr;gap:1.25rem;margin-bottom:1.25rem">'

      // Pending actions
      + '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);box-shadow:0 1px 3px rgba(0,0,0,0.05);overflow:hidden">'
      +   '<div style="padding:0.85rem 1.25rem;border-bottom:1px solid #f0ede8;background:#faf9f7"><p style="font-size:0.8rem;font-weight:700;color:#111;margin:0">Pending Actions</p></div>'
      +   '<div style="padding:0.85rem 1.25rem">'
      +   (pendingItems.length === 0
          ? '<p style="font-size:0.82rem;color:#94a3b8;text-align:center;padding:1rem 0">All clear</p>'
          : pendingItems.map(function(item) {
              return '<div onclick="App.Router.navigate(\'' + item.page + '\')" style="display:flex;align-items:center;gap:0.65rem;padding:0.55rem 0;border-bottom:1px solid #f4f4f2;cursor:pointer" onmouseover="this.style.background=\'#fafaf8\'" onmouseout="this.style.background=\'transparent\'">'
                + '<span style="width:8px;height:8px;border-radius:50%;background:' + item.color + ';flex-shrink:0"></span>'
                + '<span style="font-size:0.82rem;font-weight:600;color:#111;flex:1">' + item.label + '</span>'
                + '<svg style="width:0.85rem;flex-shrink:0;color:#d1d5db" fill="none" stroke="currentColor" stroke-width="2.5" viewBox="0 0 24 24"><path stroke-linecap="round" d="M9 5l7 7-7 7"/></svg>'
                + '</div>';
            }).join(''))
      +   '</div>'
      + '</div>'

      // Today's classes
      + '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);box-shadow:0 1px 3px rgba(0,0,0,0.05);overflow:hidden">'
      +   '<div style="padding:0.85rem 1.25rem;border-bottom:1px solid #f0ede8;background:#faf9f7;display:flex;justify-content:space-between;align-items:center">'
      +     '<p style="font-size:0.8rem;font-weight:700;color:#111;margin:0">Today\'s Classes (' + todayClasses.length + ')</p>'
      +     '<span style="font-size:0.72rem;color:#94a3b8">' + staffCheckedIn + '/' + staff.length + ' staff in</span>'
      +   '</div>'
      +   '<div style="padding:0.85rem 1.25rem">'
      +   (todayClasses.length === 0
          ? '<p style="font-size:0.82rem;color:#94a3b8;text-align:center;padding:1rem 0">No classes today</p>'
          : todayClasses.map(function(c) {
              var teachers = c.teacherIds.map(function(tid) { var st=staff.find(function(x){return x.id===tid;}); return st?st.name:tid; }).join(', ');
              var attCount = attendance.filter(function(a){ return a.classId===c.id && a.date===today && a.checkIn; }).length;
              return '<div style="display:flex;align-items:center;gap:0.75rem;padding:0.5rem 0;border-bottom:1px solid #f4f4f2">'
                + '<div style="font-size:0.72rem;font-weight:700;color:#64748b;min-width:48px">' + App.Utils.formatTime(c.time) + '</div>'
                + '<div style="flex:1;min-width:0">'
                +   '<div style="font-size:0.83rem;font-weight:600;color:#111">' + App.Utils.esc(c.name) + '</div>'
                +   '<div style="font-size:0.7rem;color:#94a3b8">' + App.Utils.esc(teachers) + ' · ' + (c.category||'Academic') + '</div>'
                + '</div>'
                + '<div style="font-size:0.72rem;font-weight:700;color:#15803d">' + attCount + '/' + c.enrolled + '</div>'
                + '</div>';
            }).join(''))
      +   '</div>'
      + '</div>'

      + '</div>'

      // Financial summary
      + '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);box-shadow:0 1px 3px rgba(0,0,0,0.05);padding:1.1rem 1.5rem;display:flex;align-items:center;gap:2rem;flex-wrap:wrap">'
      +   '<div style="font-size:0.7rem;font-weight:700;text-transform:uppercase;letter-spacing:0.07em;color:#94a3b8;white-space:nowrap">This Month</div>'
      +   _miniStat('Collected', 'RM ' + monthRevenue.toFixed(0))
      +   _miniStat('Target', 'RM ' + monthTarget.toFixed(0))
      +   _miniStat('Collection Rate', collectRate + '%')
      +   _miniStat('Overdue', overdueInvs.length + ' inv.')
      +   '<button onclick="App.Router.navigate(\'billing\')" style="margin-left:auto;padding:0.4rem 1rem;font-size:0.78rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">View Billing</button>'
      + '</div>'

      + '</div>';
  }

  function _miniStat(label, value) {
    return '<div><div style="font-size:0.65rem;font-weight:600;text-transform:uppercase;letter-spacing:0.06em;color:#94a3b8;margin-bottom:0.2rem">' + label + '</div>'
      + '<div style="font-size:1rem;font-weight:800;color:#111">' + value + '</div></div>';
  }

  // ── Teacher Dashboard ─────────────────────────────────────────────────────────
  function _teacherDash() {
    var s          = App.Store.get();
    var students   = s.students   || [];
    var classes    = s.classes    || [];
    var attendance = s.attendance || [];
    var staff      = s.staff      || [];

    var tid        = App.currentTeacher;
    var teacher    = staff.find(function(x) { return x.id === tid; }) || {};
    var myClasses  = classes.filter(function(c) { return c.teacherIds.indexOf(tid) > -1; });
    var myClassIds = myClasses.map(function(c) { return c.id; });
    var myStudents = students.filter(function(stu) {
      return stu.enrolledClasses.some(function(cid) { return myClassIds.indexOf(cid) > -1; });
    });

    var today    = App.Utils.today();
    var todayDay = new Date(today + 'T00:00:00').toLocaleDateString('en-US', { weekday: 'long' });
    var todayClasses = myClasses.filter(function(c) { return App.Utils.scheduleOn(c, App.Store.get().scheduleVersions, today).day === todayDay; }).sort(function(a,b){ return a.time.localeCompare(b.time); });
    var myRec    = attendance.find(function(a) { return a.personId === tid && a.personType === 'staff' && a.date === today; });

    var _tod = _timeOfDay();

    // ── Empty state: teacher with no assigned classes ───────────────────
    if (myClasses.length === 0) {
      return '<div class="dash-hero" style="margin-bottom:1.25rem">'
        + '<div style="position:relative;z-index:1">'
        +   '<p style="font-size:0.7rem;font-weight:700;text-transform:uppercase;letter-spacing:0.12em;color:var(--gold);margin:0 0 6px">' + _dateFull() + '</p>'
        +   '<p style="font-family:var(--serif);font-size:1.3rem;font-weight:700;color:#1a1a1a;margin:0">Good ' + _tod + ', ' + App.Utils.esc(teacher.name || 'Teacher') + '</p>'
        + '</div></div>'
        + '<div style="background:#fff;border-radius:16px;border:1px solid rgba(201,162,39,0.2);padding:2.5rem 2rem;text-align:center;max-width:500px;margin:0 auto">'
        +   '<div style="width:56px;height:56px;border-radius:50%;background:rgba(201,162,39,0.08);display:flex;align-items:center;justify-content:center;margin:0 auto 1.25rem">'
        +     '<svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="#C9A227" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="18" rx="2" ry="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/></svg>'
        +   '</div>'
        +   '<h2 style="font-family:var(--serif);font-size:1.3rem;font-weight:700;color:#0a0a0a;margin:0 0 0.5rem">No classes assigned yet</h2>'
        +   '<p style="color:#64748b;font-size:0.88rem;line-height:1.5;margin:0 auto;max-width:340px">Your administrator will assign your teaching schedule shortly. Once that\'s done, you\'ll see your classes, student list, and attendance tools here.</p>'
        +   '<p style="margin:1.25rem 0 0;font-size:0.78rem;color:#94a3b8">Questions? Reach out to your admin directly.</p>'
        + '</div>';
    }

    return '<div>'
      // Greeting
      + '<div class="dash-hero" style="margin-bottom:1.25rem">'
      + '<div style="display:flex;align-items:center;justify-content:space-between;position:relative;z-index:1">'
      +   '<div>'
      +     '<p style="font-size:0.7rem;font-weight:700;text-transform:uppercase;letter-spacing:0.12em;color:var(--gold);margin:0 0 6px">' + _dateFull() + '</p>'
      +     '<p style="font-family:var(--serif);font-size:1.3rem;font-weight:700;color:#1a1a1a;margin:0">Good ' + _tod + ', ' + App.Utils.esc(teacher.name || 'Teacher') + '</p>'
      +     '<p style="font-size:0.82rem;color:#64748b;margin:4px 0 0">' + todayClasses.length + ' class' + (todayClasses.length !== 1 ? 'es' : '') + ' today · ' + myStudents.length + ' students</p>'
      +   '</div>'
      +   '<div style="text-align:right">'
      +     (myRec && myRec.checkIn
              ? '<div style="font-size:0.78rem;font-weight:700;color:#15803d">Checked in ' + App.Utils.formatTime(myRec.checkIn) + '</div>'
              : '<button onclick="App.Router.navigate(\'attendance\')" style="padding:0.4rem 0.9rem;font-size:0.78rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Check In</button>')
      +   '</div>'
      + '</div>'
      + '</div>'

      // Stats row
      + '<div style="display:grid;grid-template-columns:repeat(3,1fr);gap:1rem;margin-bottom:1.25rem">'
      + stat('My Classes', myClasses.length, false, '#6366f1', false, '#f5f3ff', 'calendar')
      + stat('My Students', myStudents.length, false, '#0891b2', false, '#ecfeff', 'students')
      + stat('Today', todayClasses.length, false, '#d97706', false, '#fefce8', 'attendance')
      + '</div>'

      // Today's classes
      + '<div class="dash-card" style="margin-bottom:1.25rem">'
      +   '<div class="dash-card-header">Today\'s Schedule</div>'
      +   '<div class="dash-card-body">'
      +   (todayClasses.length === 0
          ? '<p style="font-size:0.82rem;color:#94a3b8;text-align:center;padding:1rem 0">No classes today — enjoy your day!</p>'
          : todayClasses.map(function(c) {
              var enrolled = students.filter(function(stu){ return stu.enrolledClasses.indexOf(c.id)>-1; });
              var attCount = attendance.filter(function(a){ return a.classId===c.id && a.date===today && a.checkIn; }).length;
              return '<div style="display:flex;align-items:center;gap:0.75rem;padding:0.65rem 0;border-bottom:1px solid #f4f4f2">'
                + '<div style="font-size:0.72rem;font-weight:700;color:#64748b;min-width:52px">' + App.Utils.formatTime(c.time) + '</div>'
                + '<div style="flex:1">'
                +   '<div style="font-size:0.85rem;font-weight:600;color:#111">' + App.Utils.esc(c.name) + '</div>'
                +   '<div style="font-size:0.7rem;color:#94a3b8">' + App.Utils.esc(c.classroom) + ' · ' + App.Utils.formatTime(c.time) + '–' + App.Utils.formatTime(c.endTime) + '</div>'
                + '</div>'
                + '<div style="font-size:0.78rem;font-weight:700;color:#15803d;background:#f0fdf4;padding:0.25rem 0.6rem;border-radius:6px">'
                +   attCount + '/' + enrolled.length + ' in'
                + '</div>'
                + '<button onclick="App._preselectedClass=\'' + c.id + '\';App.Router.navigate(\'attendance\')" style="padding:0.3rem 0.7rem;font-size:0.72rem;font-weight:700;background:#f1f5f9;color:#374151;border:none;border-radius:7px;cursor:pointer">Attend</button>'
                + '</div>';
            }).join(''))
      +   '</div>'
      + '</div>'

      // Quick links
      + '<div style="display:grid;grid-template-columns:repeat(4,1fr);gap:0.75rem">'
      + [
          { label:'My Students', page:'students', color:'#3b82f6' },
          { label:'Log Feedback', page:'feedback', color:'#8b5cf6' },
          { label:'Attendance', page:'attendance', color:'#10b981' },
          { label:'Announcements', page:'communication', color:'#f59e0b' }
        ].map(function(item) {
          return '<button onclick="App.Router.navigate(\'' + item.page + '\')" style="padding:0.85rem;background:#fff;border:1px solid rgba(0,0,0,0.07);border-radius:12px;font-size:0.82rem;font-weight:700;color:' + item.color + ';cursor:pointer;border-left:3px solid ' + item.color + '">' + item.label + '</button>';
        }).join('')
      + '</div>'
      + '</div>';
  }

  function _heroBtn(label, onclick) {
    return '<button onclick="' + onclick + '" '
      + 'style="padding:0.4rem 0.85rem;font-size:0.75rem;font-weight:700;border-radius:8px;cursor:pointer;'
      + 'border:1.5px solid rgba(201,162,39,0.4);background:rgba(201,162,39,0.06);color:#9a7b1a;transition:all 0.15s;white-space:nowrap" '
      + 'onmouseover="this.style.background=\'var(--gold)\';this.style.color=\'#0a0a0a\';this.style.borderColor=\'var(--gold)\'" '
      + 'onmouseout="this.style.background=\'rgba(201,162,39,0.06)\';this.style.color=\'#9a7b1a\';this.style.borderColor=\'rgba(201,162,39,0.4)\'">'
      + label + '</button>';
  }

  function _qaBtn(label, page, icon, customOnclick) {
    var onclick = customOnclick || "App.Router.navigate('" + page + "')";
    return '<button onclick="' + onclick + '" '
      + 'style="display:flex;align-items:center;gap:0.5rem;padding:0.5rem 0.75rem;font-size:0.78rem;font-weight:600;border-radius:8px;cursor:pointer;background:#fff;border:1px solid #e8e4df;color:#374151;text-align:left;transition:all 0.15s;white-space:nowrap" '
      + 'onmouseover="this.style.borderColor=\'var(--gold)\';this.style.color=\'var(--gold)\'" '
      + 'onmouseout="this.style.borderColor=\'#e8e4df\';this.style.color=\'#374151\'">'
      + '<span style="font-size:0.85rem;font-weight:700;color:inherit">' + icon + '</span>'
      + label
      + '</button>';
  }

  function _weeklyAttendanceCard(attendance, today, students) {
    var days = ['Mon','Tue','Wed','Thu','Fri'];
    var todayDate = new Date(today + 'T00:00:00');
    var monday = new Date(todayDate);
    var dow = todayDate.getDay(); // 0=Sun
    var offset = dow === 0 ? -6 : 1 - dow;
    monday.setDate(todayDate.getDate() + offset);

    var rows = days.map(function(dayLabel, i) {
      var d = new Date(monday); d.setDate(monday.getDate() + i);
      var dateStr = App.Utils.localDate(d);
      var dayAtt = attendance.filter(function(a) { return a.personType === 'student' && a.date === dateStr; });
      var presentCount = dayAtt.filter(function(a) { return a.status === 'Present'; }).length;
      var total = students.filter(function(s) { return s.status === 'Active'; }).length;
      var pct = total > 0 ? Math.round(presentCount / total * 100) : 0;
      var isToday = dateStr === today;
      return '<div class="dash-list-item" style="gap:0.75rem;padding:0.45rem 0;border-bottom:none">'
        + '<div style="font-size:0.72rem;font-weight:700;color:' + (isToday ? 'var(--gold)' : '#94a3b8') + ';min-width:28px">' + dayLabel + '</div>'
        + '<div style="flex:1;height:7px;background:#f1f5f9;border-radius:99px;overflow:hidden">'
        +   '<div style="width:' + pct + '%;height:100%;background:' + (isToday ? 'var(--gold)' : '#c5c0b8') + ';border-radius:99px;transition:width 0.5s cubic-bezier(0.4,0,0.2,1)"></div>'
        + '</div>'
        + '<div style="font-size:0.72rem;font-weight:700;color:' + (isToday ? 'var(--gold)' : '#94a3b8') + ';min-width:32px;text-align:right">' + pct + '%</div>'
        + '</div>';
    }).join('');

    return '<div class="dash-card" style="margin-bottom:1rem">'
      + '<div class="dash-card-header" style="cursor:pointer" onclick="App.Router.navigate(\'attendance\')">This Week\'s Attendance</div>'
      + '<div class="dash-card-body">' + rows + '</div>'
      + '</div>';
  }

  function _timeOfDay() {
    var h = new Date().getHours();
    return h < 5 ? 'night' : h < 12 ? 'morning' : h < 17 ? 'afternoon' : h < 21 ? 'evening' : 'night';
  }

  function _dateFull() {
    return new Date().toLocaleDateString('en-MY', { weekday:'long', day:'numeric', month:'long', year:'numeric' });
  }

  // Profile is now a full page (js/modules/profile.js). Kept as a thin
  // delegator so any lingering caller still lands on the new page.
  function _profileModal() {
    App.Router.navigate('profile');
  }

  // _checklistGo marks a setup/explore step visited and navigates to it.
  // The "biz" step lives inside the Calendar → Settings view, so jump there.
  function _checklistGo(key, page) {
    try {
      var v = JSON.parse(localStorage.getItem('sh_checklist_visited') || '{}');
      v[key] = true;
      localStorage.setItem('sh_checklist_visited', JSON.stringify(v));
    } catch (e) { /* private mode — non-fatal, navigation still works */ }
    App.Router.navigate(page);
    if (key === 'biz' && App.Calendar && App.Calendar._setView) App.Calendar._setView('programs');
  }

  // ── MFA enrolment (reused by the Profile page via App.Dashboard) ─────────
  // Two states: not-enrolled (show "Enable" → setup → QR → confirm → codes)
  // and enrolled (show "Disable" with confirmation). The /api/auth/profile
  // payload carries mfaEnabled so we render the right state without a
  // second round-trip.
  function _renderMFASection(enabled) {
    var sec = document.getElementById('mfa-section');
    if (!sec) return;
    if (enabled) {
      sec.innerHTML = '<h3 style="font-size:0.9rem;font-weight:700;color:#111;margin:0 0 0.5rem">Two-factor authentication</h3>'
        + '<p style="font-size:0.78rem;color:#64748b;margin:0 0 0.75rem">Active — you\'ll be prompted for a 6-digit code at login.</p>'
        + '<button onclick="App.Dashboard._mfaDisable()" style="padding:0.4rem 0.85rem;font-size:0.78rem;border:1px solid #fecaca;border-radius:8px;background:#fff;color:#dc2626;cursor:pointer">Disable 2FA</button>';
    } else {
      sec.innerHTML = '<h3 style="font-size:0.9rem;font-weight:700;color:#111;margin:0 0 0.5rem">Two-factor authentication</h3>'
        + '<p style="font-size:0.78rem;color:#64748b;margin:0 0 0.75rem">Strongly recommended for admin accounts. Uses any TOTP app (Google Authenticator, 1Password, Authy).</p>'
        + '<button onclick="App.Dashboard._mfaStart()" style="padding:0.45rem 1rem;font-size:0.8rem;font-weight:700;background:#0a0a0a;color:#fff;border:none;border-radius:8px;cursor:pointer">Enable 2FA</button>';
    }
  }

  async function _mfaStart() {
    try {
      var res = await App.Api.post('/api/auth/mfa/setup', {});
      if (!res || !res.uri) return;
      // QR is a data: URI rendered server-side. The TOTP secret must never be
      // sent to a third-party QR image service (it would hand them the 2FA
      // seed). If absent, fall back to manual entry of the secret shown below.
      var qr = res.qr || '';
      var qrImg = qr
        ? '<img src="' + App.Utils.esc(qr) + '" alt="QR code" style="width:140px;height:140px;border:1px solid #e2e8f0;border-radius:8px;background:#fff;padding:6px">'
        : '<div style="width:140px;font-size:0.72rem;color:#64748b">QR unavailable — enter the secret manually.</div>';
      var sec = document.getElementById('mfa-section');
      sec.innerHTML = '<h3 style="font-size:0.9rem;font-weight:700;color:#111;margin:0 0 0.5rem">Scan with your authenticator</h3>'
        + '<div style="display:flex;gap:1rem;align-items:flex-start;margin-bottom:0.75rem">'
        +   qrImg
        +   '<div style="flex:1;font-size:0.74rem;color:#64748b;line-height:1.5">'
        +     '<p style="margin:0 0 0.5rem">Or enter this secret manually:</p>'
        +     '<code style="display:block;background:#f8fafc;padding:0.5rem;border-radius:6px;border:1px solid #e2e8f0;font-size:0.7rem;word-break:break-all">' + App.Utils.esc(res.secret) + '</code>'
        +   '</div>'
        + '</div>'
        + '<form id="mfa-confirm-form" style="display:flex;gap:0.5rem;align-items:center">'
        +   '<input name="code" placeholder="6-digit code" inputmode="numeric" pattern="[0-9]{6}" maxlength="6" required style="font-family:ui-monospace,monospace;font-size:1rem;letter-spacing:0.2em;text-align:center;padding:0.55rem 0.75rem;border:1px solid #e2e8f0;border-radius:8px;width:11ch">'
        +   '<button type="submit" style="padding:0.55rem 1rem;font-size:0.8rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Confirm</button>'
        + '</form>';
      document.getElementById('mfa-confirm-form').addEventListener('submit', async function(e) {
        e.preventDefault();
        var fd = new FormData(e.target);
        try {
          var out = await App.Api.post('/api/auth/mfa/confirm', { code: fd.get('code') });
          if (out && out.recoveryCodes) {
            _renderMFARecoveryCodes(out.recoveryCodes);
          }
        } catch (err) {}
      });
    } catch (err) {}
  }

  // _renderMFARecoveryCodes displays the one-time recovery codes — these
  // are the user's only fallback if they lose the authenticator, and the
  // server hashes them immediately so we cannot show them again.
  function _renderMFARecoveryCodes(codes) {
    var sec = document.getElementById('mfa-section');
    if (!sec) return;
    var list = codes.map(function(c) {
      return '<code style="display:block;font-size:0.85rem;padding:0.3rem 0.5rem;background:#f8fafc;border-radius:4px">' + App.Utils.esc(c) + '</code>';
    }).join('');
    sec.innerHTML = '<h3 style="font-size:0.9rem;font-weight:700;color:#111;margin:0 0 0.5rem">2FA enabled — save your recovery codes</h3>'
      + '<p style="font-size:0.78rem;color:#dc2626;margin:0 0 0.75rem;font-weight:600">These won\'t be shown again. Store them somewhere safe.</p>'
      + '<div style="display:grid;grid-template-columns:1fr 1fr;gap:0.4rem;margin-bottom:0.75rem">' + list + '</div>'
      + '<button onclick="App.Dashboard._mfaCopyCodes(' + JSON.stringify(JSON.stringify(codes)).replace(/"/g,'&quot;') + ')" style="padding:0.4rem 0.85rem;font-size:0.78rem;border:1px solid #e2e8f0;border-radius:8px;background:#fff;color:#374151;cursor:pointer">Copy all</button>';
  }

  function _mfaCopyCodes(codesJson) {
    try {
      var codes = JSON.parse(codesJson);
      navigator.clipboard.writeText(codes.join('\n')).then(function() {
        App.Utils.showToast('Recovery codes copied', 'success');
      });
    } catch (e) {}
  }

  async function _mfaDisable() {
    var ok = await App.Utils.showConfirm({
      title: 'Disable 2FA?',
      message: 'You\'ll be able to log in with just your password. Not recommended for admin accounts.',
      confirmLabel: 'Disable',
      danger: true,
    });
    if (!ok) return;
    // Server requires a fresh authenticator code to disable, so a stolen
    // session alone can't turn off 2FA. Prompt for it here.
    var code = window.prompt('Enter a current 6-digit code from your authenticator to confirm:');
    if (!code) return;
    try {
      await App.Api.post('/api/auth/mfa/disable', { code: code.trim() });
      _renderMFASection(false);
      App.Utils.showToast('2FA disabled', 'info');
    } catch (err) {}
  }

  // ── Pending Users Modal ────────────────────────────────────────────────────
  function _pendingUsersModal() {
    var users = (App.Store.get().pendingUsers || []);
    var rows = users.length === 0
      ? '<div class="py-8 text-center text-slate-400">No pending accounts</div>'
      : users.map(function(u) {
          return '<div style="display:flex;align-items:center;gap:0.75rem;padding:0.75rem 0;border-bottom:1px solid #f4f4f2">'
            + '<div style="flex:1;min-width:0">'
            +   '<div style="font-size:0.85rem;font-weight:600;color:#111">' + App.Utils.esc(u.name || u.email) + '</div>'
            +   '<div style="font-size:0.75rem;color:#94a3b8">' + App.Utils.esc(u.email) + ' · ' + App.Utils.esc(u.role) + '</div>'
            + '</div>'
            + '<button onclick="App.Dashboard._verifyUser(' + u.id + ')" style="padding:0.35rem 0.7rem;font-size:0.72rem;font-weight:600;border:1px solid #10b981;border-radius:6px;background:#f0fdf4;color:#059669;cursor:pointer">Verify</button>'
            + '<button onclick="App.Dashboard._resendVerification(' + u.id + ')" style="padding:0.35rem 0.7rem;font-size:0.72rem;font-weight:600;border:1px solid #e2e8f0;border-radius:6px;background:#fff;color:#64748b;cursor:pointer">Resend</button>'
            + '</div>';
        }).join('');

    App.Utils.showModal(
      '<div style="max-width:480px">'
      + '<h2 class="text-lg font-bold mb-1">Pending Verification Accounts</h2>'
      + '<p class="text-sm text-slate-500 mb-4">These users registered but haven\'t verified their email yet.</p>'
      + rows
      + '</div>'
    );
  }

  async function _verifyUser(id) {
    var res = await App.Api.post('/api/users/' + id + '/verify');
    if (res && !res.error) {
      App.Utils.showToast('Account activated', 'success');
      App.Utils.hideModal(true);
    }
  }

  async function _resendVerification(id) {
    var res = await App.Api.post('/api/users/' + id + '/resend-verification');
    if (res && !res.error) {
      App.Utils.showToast('Verification email sent', 'success');
    }
  }

  App.Dashboard = {
    render: render,
    _setView: _setDashView,
    _profileModal: _profileModal,
    _checklistGo: _checklistGo,
    _pendingUsersModal: _pendingUsersModal,
    _verifyUser: _verifyUser,
    _resendVerification: _resendVerification,
    _mfaStart: _mfaStart,
    _mfaDisable: _mfaDisable,
    _mfaCopyCodes: _mfaCopyCodes,
    _renderMFASection: _renderMFASection
  };
})();
