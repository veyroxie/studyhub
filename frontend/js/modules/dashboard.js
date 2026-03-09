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
    var todayClasses   = classes.filter(function(c) { return c.day === todayDay; });
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
    return card('')
      // Greeting
      + '<div class="flex items-start justify-between gap-4 flex-wrap">'
      +   '<div>'
      +     '<p class="text-xs font-semibold uppercase tracking-widest" style="color:var(--gold);letter-spacing:0.12em">Study Hub</p>'
      +     '<h1 style="font-size:1.7rem;font-weight:800;letter-spacing:-0.04em;color:#0d0d0d;line-height:1.2;margin-top:2px">Good ' + _timeOfDay() + '</h1>'
      +     '<p style="font-size:0.8rem;color:#94a3b8;margin-top:4px">' + _dateFull() + '</p>'
      +   '</div>'
      +   '<div style="display:grid;grid-template-columns:1fr 1fr;gap:0.5rem">'
      +     _qaBtn('Add Student',   'students',      '+ S')
      +     _qaBtn('Add Invoice',   'billing',       '+ I')
      +     _qaBtn('Announcement',  'communication', '+ A')
      +     _qaBtn('Attendance',    'attendance',    '✓')
      +   '</div>'
      + '</div>'
      + '</div>' // close greeting card

      // Stats
      + '<div style="display:grid;grid-template-columns:repeat(4,1fr);gap:0.75rem">'
      + stat('Active Students', activeStudents,  false, '#1d4ed8',   false, '#eff6ff')
      + stat('Revenue / Month', monthRevenue,    true,  '#92400e', true, '#fef9ec')
      + stat('Overdue Invoices',overdueInvs.length, false, overdueInvs.length > 0 ? '#991b1b' : '#94a3b8', false, overdueInvs.length > 0 ? '#fef2f2' : '#f9f9f9')
      + stat('Pending Regs',   pendingRegs,     false, pendingRegs > 0 ? '#92400e' : '#94a3b8', false, pendingRegs > 0 ? '#fffbeb' : '#f9f9f9')
      + '</div>'

      // Main two-col
      + '<div style="display:grid;grid-template-columns:3fr 2fr;gap:0.75rem;align-items:start">'

        // Today's classes
        + card('Today\'s Classes <span style="font-size:0.75rem;font-weight:500;color:#94a3b8;margin-left:6px">' + todayDay + '</span>')
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
                  +   '<div style="font-weight:600;font-size:0.85rem;color:#111">' + c.name + '</div>'
                  +   '<div style="font-size:0.72rem;color:#94a3b8;margin-top:2px">' + App.Utils.formatTime(c.time) + '–' + App.Utils.formatTime(c.endTime) + (teachers ? '&nbsp;·&nbsp;' + teachers : '') + '&nbsp;·&nbsp;' + c.classroom + '</div>'
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
        + card('Needs Attention')
        + _attnItems(pendingRegs, overdueInvs, dueSoonInvs, newStudents, students, attendance, staff, classes, today, todayDay, now, invoices)
        + '</div></div>' // close inner padding div + attn card

      + '</div>' // close two-col

      + _weeklyAttendanceCard(attendance, today, students)

      // Bottom two-col: recent students + overdue invoices
      + '<div style="display:grid;grid-template-columns:3fr 2fr;gap:0.75rem;align-items:start">'

        // Recent students
        + card('Recent Students')
        + recentStudents.map(function(stu) {
            var cls = classes.filter(function(c) { return stu.enrolledClasses.indexOf(c.id) > -1; }).map(function(c) { return c.name; }).join(', ');
            return '<div style="display:flex;align-items:center;gap:0.7rem;padding:0.55rem 0;border-bottom:1px solid #f4f4f2">'
              + '<div style="width:2rem;height:2rem;border-radius:50%;background:var(--gold-dim);border:1px solid rgba(201,162,39,0.25);color:var(--gold);font-weight:800;font-size:0.78rem;display:flex;align-items:center;justify-content:center;flex-shrink:0">' + stu.firstName.charAt(0) + '</div>'
              + '<div style="flex:1;min-width:0">'
              +   '<div style="font-size:0.83rem;font-weight:600;color:#111;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + stu.firstName + ' ' + stu.lastName + '</div>'
              +   '<div style="font-size:0.71rem;color:#94a3b8;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + (cls || 'No class') + '</div>'
              + '</div>'
              + App.Utils.statusBadge(stu.status)
              + '<button onclick="App.Students && App.Students._viewModal(\'' + stu.id + '\')" style="font-size:0.7rem;color:#94a3b8;background:none;border:none;cursor:pointer;padding:0 0.2rem">View</button>'
              + '</div>';
          }).join('')
        + '<button onclick="App.Router.navigate(\'students\')" style="display:block;width:100%;margin-top:0.6rem;font-size:0.75rem;font-weight:600;color:var(--gold);background:none;border:none;cursor:pointer;text-align:center;padding:0.4rem">View all students →</button>'
        + '</div></div>' // close inner padding div + recent students card

        // Overdue invoices quick panel
        + card('Overdue Invoices')
        + (topOverdue.length === 0
            ? '<p style="color:#94a3b8;font-size:0.82rem;text-align:center;padding:1.5rem 0">No overdue invoices ✓</p>'
            : topOverdue.map(function(inv) {
                var stu = students.find(function(s) { return s.id === inv.studentId; });
                return '<div style="display:flex;align-items:center;gap:0.6rem;padding:0.55rem 0;border-bottom:1px solid #f4f4f2">'
                  + '<div style="flex:1;min-width:0">'
                  +   '<div style="font-size:0.82rem;font-weight:600;color:#111">' + (stu ? stu.firstName + ' ' + stu.lastName : inv.studentId) + '</div>'
                  +   '<div style="font-size:0.7rem;color:#94a3b8">' + App.Utils.formatDate(inv.dueDate) + '</div>'
                  + '</div>'
                  + '<div style="font-size:0.82rem;font-weight:700;color:#dc2626;flex-shrink:0">' + App.Utils.formatCurrency(inv.amount) + '</div>'
                  + '</div>';
              }).join(''))
        + '<button onclick="App.Router.navigate(\'billing\')" style="display:block;width:100%;margin-top:0.6rem;font-size:0.75rem;font-weight:600;color:var(--gold);background:none;border:none;cursor:pointer;text-align:center;padding:0.4rem">Manage billing →</button>'
        + '</div></div>' // close inner padding div + overdue card

      + '</div>'; // close bottom row
  }

  // ── Parent ────────────────────────────────────────────────────────────────────
  function _parentDash() {
    var s             = App.Store.get();
    var students      = s.students      || [];
    var classes       = s.classes       || [];
    var invoices      = s.invoices      || [];
    var announcements = s.announcements || [];
    var attendance    = s.attendance    || [];

    var myStudents = students.filter(function(st) { return st.contact === App.clientParent && st.status !== 'Inactive'; });
    var myIds      = myStudents.map(function(st) { return st.id; });
    var myInvoices = invoices.filter(function(i)  { return myIds.indexOf(i.studentId) > -1; });

    var now = new Date(); var in7 = new Date(now); in7.setDate(now.getDate() + 7);
    var today = App.Utils.today();

    var overdueInvs = myInvoices.filter(function(i) { return i.status === 'Overdue'; });
    var dueSoonInvs = myInvoices.filter(function(i) {
      if (i.status !== 'Unpaid') return false;
      var d = new Date(i.dueDate); return d >= now && d <= in7;
    });
    var totalOwed = myInvoices.filter(function(i) { return i.status === 'Overdue' || i.status === 'Unpaid'; })
      .reduce(function(a, i) { return a + i.amount; }, 0);

    var enrolledIds = [];
    myStudents.forEach(function(st) { st.enrolledClasses.forEach(function(id) { if (enrolledIds.indexOf(id) === -1) enrolledIds.push(id); }); });
    var myClasses = classes.filter(function(c) { return enrolledIds.indexOf(c.id) > -1; });

    var todayDay      = new Date(today + 'T00:00:00').toLocaleDateString('en-US', { weekday: 'long' });
    var todayClasses  = myClasses.filter(function(c) { return c.day === todayDay; });
    var recentAttend  = attendance.filter(function(a) { return myIds.indexOf(a.personId) > -1; })
      .sort(function(a, b) { return b.date.localeCompare(a.date); }).slice(0, 5);
    var latestAnnounce = (s.announcements || []).slice()
      .sort(function(a, b) { return b.createdOn.localeCompare(a.createdOn); }).slice(0, 3);
    var childNames = myStudents.map(function(st) { return st.firstName; }).join(' & ');

    return card('')
      + '<div class="flex items-start justify-between gap-4 flex-wrap">'
      +   '<div>'
      +     '<p style="font-size:0.72rem;font-weight:600;text-transform:uppercase;letter-spacing:0.1em;color:var(--gold)">Parent Portal</p>'
      +     '<h1 style="font-size:1.7rem;font-weight:800;letter-spacing:-0.04em;color:#0d0d0d;line-height:1.2;margin-top:2px">' + (childNames ? childNames + '\'s Dashboard' : 'Welcome back') + '</h1>'
      +     '<p style="font-size:0.8rem;color:#94a3b8;margin-top:4px">' + _dateFull() + '</p>'
      +   '</div>'
      + '</div>'
      + '</div>'

      // Alert banner
      + (overdueInvs.length > 0
          ? '<div style="padding:0.85rem 1.1rem;background:#fef2f2;border:1px solid #fecaca;border-left:3px solid #dc2626;border-radius:12px;display:flex;align-items:center;gap:0.75rem"><div style="flex:1;font-size:0.83rem;color:#991b1b"><strong>Payment overdue</strong> — ' + overdueInvs.length + ' invoice' + (overdueInvs.length !== 1 ? 's are' : ' is') + ' past due.</div><button onclick="App.Router.navigate(\'billing\')" style="font-size:0.73rem;font-weight:700;color:#dc2626;background:#fff;border:1px solid #fecaca;border-radius:7px;padding:0.3rem 0.7rem;cursor:pointer;white-space:nowrap">Pay Now</button></div>'
          : dueSoonInvs.length > 0
          ? '<div style="padding:0.85rem 1.1rem;background:#fffbeb;border:1px solid #fde68a;border-left:3px solid #d97706;border-radius:12px;display:flex;align-items:center;gap:0.75rem"><div style="flex:1;font-size:0.83rem;color:#92400e"><strong>Payment due soon</strong> — ' + dueSoonInvs.length + ' invoice' + (dueSoonInvs.length !== 1 ? 's' : '') + ' due within 7 days.</div><button onclick="App.Router.navigate(\'billing\')" style="font-size:0.73rem;font-weight:700;color:#d97706;background:#fff;border:1px solid #fde68a;border-radius:7px;padding:0.3rem 0.7rem;cursor:pointer;white-space:nowrap">View</button></div>'
          : '')

      // Stats
      + '<div style="display:grid;grid-template-columns:repeat(3,1fr);gap:0.75rem">'
      + stat('My Children',      myStudents.length, false, '#1d4ed8',    false, '#eff6ff')
      + stat('Classes Enrolled', myClasses.length,  false, '#92400e', true, '#fef9ec')
      + stat('Balance Due',      totalOwed,         true,  totalOwed > 0 ? '#991b1b' : '#15803d', false, totalOwed > 0 ? '#fef2f2' : '#f0fdf4')
      + '</div>'

      // Two col: today's classes + announcements
      + '<div style="display:grid;grid-template-columns:3fr 2fr;gap:0.75rem;align-items:start">'

        // Today's classes
        + card('Today\'s Classes')
        + (todayClasses.length === 0
            ? '<p style="color:#94a3b8;font-size:0.84rem;padding:1.5rem 0;text-align:center">No classes today</p>'
            : todayClasses.map(function(c) {
                var colors   = App.Utils.colorClasses(c.color);
                var teachers = (s.staff||[]).filter(function(t){ return c.teacherIds.indexOf(t.id)>-1; }).map(function(t){ return t.name; }).join(', ');
                var who      = myStudents.filter(function(st){ return st.enrolledClasses.indexOf(c.id)>-1; }).map(function(st){ return st.firstName; }).join(', ');
                return '<div style="display:flex;align-items:center;gap:0.75rem;padding:0.65rem 0;border-bottom:1px solid #f4f4f2">'
                  + '<div style="width:3px;border-radius:99px;align-self:stretch;min-height:36px;flex-shrink:0" class="' + colors.dot + '"></div>'
                  + '<div style="flex:1;min-width:0">'
                  +   '<div style="font-weight:600;font-size:0.85rem;color:#111">' + c.name + '</div>'
                  +   '<div style="font-size:0.72rem;color:#94a3b8;margin-top:2px">' + App.Utils.formatTime(c.time) + '–' + App.Utils.formatTime(c.endTime) + (teachers ? '&nbsp;·&nbsp;' + teachers : '') + '</div>'
                  +   '<div style="font-size:0.72rem;color:var(--gold);margin-top:1px;font-weight:600">' + who + '</div>'
                  + '</div>'
                  + '<div style="font-size:0.72rem;color:#94a3b8;flex-shrink:0">' + c.classroom + '</div>'
                  + '</div>';
              }).join(''))
        + '<button onclick="App.Router.navigate(\'calendar\')" style="display:block;width:100%;margin-top:0.6rem;font-size:0.75rem;font-weight:600;color:var(--gold);background:none;border:none;cursor:pointer;text-align:center;padding:0.4rem">Full schedule →</button>'
        + '</div></div>' // close inner padding div + today classes card

        // Announcements
        + card('Announcements')
        + (latestAnnounce.length === 0
            ? '<p style="color:#94a3b8;font-size:0.82rem;text-align:center;padding:1.5rem 0">No announcements</p>'
            : latestAnnounce.map(function(a) {
                var tc = { Notice:'#2563eb', Reminder:'#d97706', Urgent:'#dc2626' };
                var tb = { Notice:'#eff6ff', Reminder:'#fffbeb', Urgent:'#fef2f2' };
                return '<div style="padding:0.65rem 0;border-bottom:1px solid #f4f4f2">'
                  + '<div style="display:flex;align-items:center;gap:0.4rem;margin-bottom:3px">'
                  +   '<span style="font-size:0.62rem;font-weight:700;text-transform:uppercase;letter-spacing:0.05em;color:' + (tc[a.type]||'#2563eb') + ';background:' + (tb[a.type]||'#eff6ff') + ';padding:1px 6px;border-radius:4px">' + a.type + '</span>'
                  +   '<span style="font-size:0.68rem;color:#94a3b8">' + App.Utils.formatDate(a.createdOn) + '</span>'
                  + '</div>'
                  + '<div style="font-size:0.82rem;font-weight:600;color:#111;line-height:1.3">' + a.title + '</div>'
                  + '<div style="font-size:0.72rem;color:#64748b;margin-top:2px">' + a.message.slice(0,80) + (a.message.length > 80 ? '…' : '') + '</div>'
                  + '</div>';
              }).join(''))
        + '<button onclick="App.Router.navigate(\'communication\')" style="display:block;width:100%;margin-top:0.6rem;font-size:0.75rem;font-weight:600;color:var(--gold);background:none;border:none;cursor:pointer;text-align:center;padding:0.4rem">View all →</button>'
        + '</div></div>' // close inner padding div + announcements card

      + '</div>';
  }

  // ── Helpers ───────────────────────────────────────────────────────────────────

  // card() opens a card; with title: opens outer div + inner padding div (caller needs two </div>s); without title: one div
  function card(title) {
    var header = title
      ? '<div style="padding:0.85rem 1.25rem;border-bottom:1px solid #f0ede8;background:#faf9f7"><p style="font-size:0.8rem;font-weight:700;color:#111;margin:0">' + title + '</p></div><div style="padding:1rem 1.25rem">'
      : '<div style="padding:1.25rem">';
    return '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);box-shadow:0 1px 3px rgba(0,0,0,0.05);overflow:hidden">' + header;
  }

  function stat(label, value, isCurr, color, goldBorder, bg) {
    var display = isCurr
      ? 'RM\u00a0' + value.toFixed(2).replace(/\B(?=(\d{3})+(?!\d))/g, ',')
      : String(value);
    var dataAttr = isCurr
      ? 'data-count="' + value + '" data-currency="1"'
      : 'data-count="' + value + '"';
    var bgColor = bg || '#fff';
    var borderTop = goldBorder ? 'border-top:2.5px solid var(--gold);' : '';
    return '<div style="background:' + bgColor + ';border-radius:14px;border:1px solid rgba(0,0,0,0.06);padding:1.1rem 1.2rem;box-shadow:0 1px 2px rgba(0,0,0,0.04);' + borderTop + '">'
      + '<p style="font-size:0.66rem;font-weight:700;text-transform:uppercase;letter-spacing:0.08em;color:' + color + ';opacity:0.6;margin:0 0 0.45rem">' + label + '</p>'
      + '<p ' + dataAttr + ' style="font-family:\'Cormorant Garamond\',Georgia,serif;font-size:2rem;font-weight:600;line-height:1;color:' + color + ';margin:0">' + display + '</p>'
      + '</div>';
  }

  function qbtn(label, onclick, primary) {
    return '<button onclick="' + onclick + '" style="padding:0.45rem 0.9rem;font-size:0.78rem;font-weight:700;border-radius:8px;cursor:pointer;transition:opacity 0.15s;'
      + (primary ? 'background:var(--gold);color:#0a0a0a;border:none;' : 'background:#fff;color:#374151;border:1px solid #e2e8f0;')
      + '" onmouseover="this.style.opacity=\'0.8\'" onmouseout="this.style.opacity=\'1\'">' + label + '</button>';
  }

  function _attnItems(pendingRegs, overdueInvs, dueSoonInvs, newStudents, students, attendance, staff, classes, today, todayDay, now, invoices) {
    var DOTS = { error:'#ef4444', warning:'#f59e0b', info:'#3b82f6' };
    var attn = [];
    if (pendingRegs > 0)       attn.push({ sev:'info',    title: pendingRegs + ' pending reg' + (pendingRegs!==1?'s':''),    page:'students'   });
    if (overdueInvs.length > 0) attn.push({ sev:'error',  title: overdueInvs.length + ' overdue invoice' + (overdueInvs.length!==1?'s':''), page:'billing' });
    if (dueSoonInvs.length > 0) attn.push({ sev:'warning',title: dueSoonInvs.length + ' payment' + (dueSoonInvs.length!==1?'s':'') + ' due this week', page:'billing' });
    if (newStudents > 0)        attn.push({ sev:'info',   title: newStudents + ' new student' + (newStudents!==1?'s':'') + ' to activate', page:'students' });

    // Churn risk: Active students with no Present record in last 14 days
    if (students && attendance) {
      var cutoff = new Date(now); cutoff.setDate(cutoff.getDate() - 14);
      var cutoffStr = cutoff.toISOString().slice(0,10);
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
      classes.filter(function(c) { return c.day === todayDay && c.time <= in30Str; })
        .forEach(function(c) { c.teacherIds.forEach(function(tid) { if (alertTeacherIds.indexOf(tid) === -1) alertTeacherIds.push(tid); }); });
      var notCheckedIn = alertTeacherIds.filter(function(tid) {
        return !attendance.find(function(a) { return a.personId === tid && a.personType === 'staff' && a.date === today && a.checkIn; });
      });
      if (notCheckedIn.length > 0) {
        var names = notCheckedIn.map(function(tid) { var st = staff.find(function(x){return x.id===tid;}); return st ? st.name : tid; }).join(', ');
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

    var items = attn;

    if (items.length === 0) {
      return '<div style="padding:1.5rem 0;text-align:center"><div style="font-size:1.3rem;margin-bottom:4px">✓</div><p style="font-size:0.82rem;color:#94a3b8;margin:0">All clear</p></div>';
    }
    return items.map(function(item) {
      return '<div onclick="App.Router.navigate(\'' + item.page + '\')" style="display:flex;align-items:center;gap:0.65rem;padding:0.6rem 0.5rem;margin:0 -0.5rem;border-radius:8px;cursor:pointer;transition:background 0.15s;border-bottom:1px solid #f4f4f2" onmouseover="this.style.background=\'#fafaf8\'" onmouseout="this.style.background=\'transparent\'">'
        + '<span style="width:7px;height:7px;border-radius:50%;background:' + (DOTS[item.sev]||DOTS.info) + ';flex-shrink:0"></span>'
        + '<div style="flex:1;min-width:0">'
        +   '<div style="font-size:0.81rem;font-weight:600;color:#111">' + item.title + '</div>'
        +   (item.sub ? '<div style="font-size:0.71rem;color:#94a3b8;margin-top:1px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + item.sub + '</div>' : '')
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
      var todayCount  = catClasses.filter(function(c) { return c.day === todayDay; }).length;
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
    var todayClasses  = classes.filter(function(c) { return c.day === todayDay; }).sort(function(a,b){ return a.time.localeCompare(b.time); });

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
                +   '<div style="font-size:0.83rem;font-weight:600;color:#111">' + c.name + '</div>'
                +   '<div style="font-size:0.7rem;color:#94a3b8">' + teachers + ' · ' + (c.category||'Academic') + '</div>'
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
    var todayClasses = myClasses.filter(function(c) { return c.day === todayDay; }).sort(function(a,b){ return a.time.localeCompare(b.time); });
    var myRec    = attendance.find(function(a) { return a.personId === tid && a.personType === 'staff' && a.date === today; });

    return '<div>'
      // Greeting
      + '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);padding:1.25rem 1.5rem;margin-bottom:1.25rem;display:flex;align-items:center;justify-content:space-between">'
      +   '<div>'
      +     '<p style="font-size:1.1rem;font-weight:800;color:#111;margin:0">Good ' + _timeOfDay() + ', ' + (teacher.name || 'Teacher') + '</p>'
      +     '<p style="font-size:0.8rem;color:#94a3b8;margin:0.2rem 0 0">' + _dateFull() + '</p>'
      +   '</div>'
      +   '<div style="text-align:right">'
      +     (myRec && myRec.checkIn
              ? '<div style="font-size:0.78rem;font-weight:700;color:#15803d">Checked in ' + App.Utils.formatTime(myRec.checkIn) + '</div>'
              : '<button onclick="App.Router.navigate(\'attendance\')" style="padding:0.4rem 0.9rem;font-size:0.78rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Check In</button>')
      +   '</div>'
      + '</div>'

      // Stats row
      + '<div style="display:grid;grid-template-columns:repeat(3,1fr);gap:1rem;margin-bottom:1.25rem">'
      + stat('My Classes', myClasses.length, false, '#6366f1', false, '#f5f3ff')
      + stat('My Students', myStudents.length, false, '#0891b2', false, '#ecfeff')
      + stat('Today', todayClasses.length, false, '#d97706', false, '#fefce8')
      + '</div>'

      // Today's classes
      + '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);box-shadow:0 1px 3px rgba(0,0,0,0.05);overflow:hidden;margin-bottom:1.25rem">'
      +   '<div style="padding:0.85rem 1.25rem;border-bottom:1px solid #f0ede8;background:#faf9f7"><p style="font-size:0.8rem;font-weight:700;color:#111;margin:0">Today\'s Schedule</p></div>'
      +   '<div style="padding:0.85rem 1.25rem">'
      +   (todayClasses.length === 0
          ? '<p style="font-size:0.82rem;color:#94a3b8;text-align:center;padding:1rem 0">No classes today — enjoy your day!</p>'
          : todayClasses.map(function(c) {
              var enrolled = students.filter(function(stu){ return stu.enrolledClasses.indexOf(c.id)>-1; });
              var attCount = attendance.filter(function(a){ return a.classId===c.id && a.date===today && a.checkIn; }).length;
              return '<div style="display:flex;align-items:center;gap:0.75rem;padding:0.65rem 0;border-bottom:1px solid #f4f4f2">'
                + '<div style="font-size:0.72rem;font-weight:700;color:#64748b;min-width:52px">' + App.Utils.formatTime(c.time) + '</div>'
                + '<div style="flex:1">'
                +   '<div style="font-size:0.85rem;font-weight:600;color:#111">' + c.name + '</div>'
                +   '<div style="font-size:0.7rem;color:#94a3b8">' + c.classroom + ' · ' + App.Utils.formatTime(c.time) + '–' + App.Utils.formatTime(c.endTime) + '</div>'
                + '</div>'
                + '<div style="font-size:0.78rem;font-weight:700;color:#15803d;background:#f0fdf4;padding:0.25rem 0.6rem;border-radius:6px">'
                +   attCount + '/' + enrolled.length + ' in'
                + '</div>'
                + '<button onclick="App.Router.navigate(\'attendance\')" style="padding:0.3rem 0.7rem;font-size:0.72rem;font-weight:700;background:#f1f5f9;color:#374151;border:none;border-radius:7px;cursor:pointer">Attend</button>'
                + '</div>';
            }).join(''))
      +   '</div>'
      + '</div>'

      // Quick links
      + '<div style="display:grid;grid-template-columns:repeat(3,1fr);gap:0.75rem">'
      + [
          { label:'My Students', page:'students', color:'#3b82f6' },
          { label:'Attendance', page:'attendance', color:'#10b981' },
          { label:'Announcements', page:'communication', color:'#f59e0b' }
        ].map(function(item) {
          return '<button onclick="App.Router.navigate(\'' + item.page + '\')" style="padding:0.85rem;background:#fff;border:1px solid rgba(0,0,0,0.07);border-radius:12px;font-size:0.82rem;font-weight:700;color:' + item.color + ';cursor:pointer;border-left:3px solid ' + item.color + '">' + item.label + '</button>';
        }).join('')
      + '</div>'
      + '</div>';
  }

  function _qaBtn(label, page, icon) {
    return '<button onclick="App.Router.navigate(\'' + page + '\')" '
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
      var dateStr = d.toISOString().slice(0, 10);
      var dayAtt = attendance.filter(function(a) { return a.personType === 'student' && a.date === dateStr; });
      var presentCount = dayAtt.filter(function(a) { return a.status === 'Present'; }).length;
      var total = students.filter(function(s) { return s.status === 'Active'; }).length;
      var pct = total > 0 ? Math.round(presentCount / total * 100) : 0;
      var isToday = dateStr === today;
      return '<div style="display:flex;align-items:center;gap:0.75rem;margin-bottom:0.5rem">'
        + '<div style="font-size:0.7rem;font-weight:700;color:' + (isToday ? 'var(--gold)' : '#94a3b8') + ';min-width:28px">' + dayLabel + '</div>'
        + '<div style="flex:1;height:5px;background:#f1f5f9;border-radius:99px;overflow:hidden">'
        +   '<div style="width:' + pct + '%;height:100%;background:' + (isToday ? 'var(--gold)' : '#94a3b8') + ';border-radius:99px;transition:width 0.4s"></div>'
        + '</div>'
        + '<div style="font-size:0.7rem;font-weight:600;color:#94a3b8;min-width:32px;text-align:right">' + pct + '%</div>'
        + '</div>';
    }).join('');

    return '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);box-shadow:0 1px 3px rgba(0,0,0,0.05);overflow:hidden;margin-bottom:0.75rem">'
      + '<div style="padding:0.85rem 1.25rem;border-bottom:1px solid #f0ede8;background:#faf9f7">'
      +   '<p style="font-size:0.8rem;font-weight:700;color:#111;margin:0">This Week\'s Attendance</p>'
      + '</div>'
      + '<div style="padding:1rem 1.25rem">' + rows + '</div>'
      + '</div>';
  }

  function _timeOfDay() {
    var h = new Date().getHours();
    return h < 12 ? 'morning' : h < 17 ? 'afternoon' : 'evening';
  }

  function _dateFull() {
    return new Date().toLocaleDateString('en-MY', { weekday:'long', day:'numeric', month:'long', year:'numeric' });
  }

  App.Dashboard = { render: render, _setView: _setDashView };
})();
