(function() {
  window.App = window.App || {};

  var _preview = null; // null = follow role, 'admin' or 'parent' = forced preview

  function render(container) {
    var isAdmin = App.currentRole === 'admin';
    // In admin mode, show the preview toggle; in client mode, always parent view
    var showAdmin = isAdmin ? (_preview !== 'parent') : false;
    container.innerHTML = _viewToggle(isAdmin) + (showAdmin ? _adminDash() : _parentDash());
    setTimeout(_runCountUp, 60);
  }

  function _viewToggle(isAdmin) {
    if (!isAdmin) return ''; // parents don't need the toggle
    var onAdmin  = _preview !== 'parent';
    var btn = function(label, val, active) {
      return '<button onclick="App.Dashboard._setPreview(\'' + val + '\')" style="'
        + 'padding:0.3rem 0.85rem;font-size:0.73rem;font-weight:600;border:none;border-radius:6px;cursor:pointer;transition:all 0.15s;'
        + (active ? 'background:var(--gold);color:#0a0a0a;' : 'background:transparent;color:#94a3b8;')
        + '">' + label + '</button>';
    };
    return '<div style="display:flex;align-items:center;gap:0.25rem;background:#f1f5f9;border-radius:8px;padding:3px;margin-bottom:1rem;width:fit-content">'
      + btn('Admin View', 'admin', onAdmin)
      + btn('Parent View', 'parent', !onAdmin)
      + '</div>';
  }

  // ── Count-up animation ──────────────────────────────────────────────────────
  function _runCountUp() {
    document.querySelectorAll('[data-count]').forEach(function(el) {
      var target = parseFloat(el.dataset.count);
      if (isNaN(target) || target === 0) return;
      var isCurr = el.dataset.currency === '1';
      var start = 0;
      var dur   = 700;
      var t0    = null;
      function step(ts) {
        if (!t0) t0 = ts;
        var p   = Math.min((ts - t0) / dur, 1);
        var ease = 1 - Math.pow(1 - p, 3);
        var val  = start + (target - start) * ease;
        el.textContent = isCurr
          ? 'RM\u00a0' + val.toFixed(2).replace(/\B(?=(\d{3})+(?!\d))/g, ',')
          : Math.round(val).toString();
        if (p < 1) requestAnimationFrame(step);
      }
      requestAnimationFrame(step);
    });
  }

  // ── Admin Dashboard ──────────────────────────────────────────────────────────
  function _adminDash() {
    var s = App.Store.get();
    var students      = s.students      || [];
    var classes       = s.classes       || [];
    var invoices      = s.invoices      || [];
    var staff         = s.staff         || [];
    var attendance    = s.attendance    || [];
    var announcements = s.announcements || [];
    var registrations = s.registrations || [];

    var today    = App.Utils.today();
    var todayDay = new Date(today + 'T00:00:00').toLocaleDateString('en-US', { weekday: 'long' });
    var now      = new Date();
    var in7      = new Date(now); in7.setDate(now.getDate() + 7);

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
    var monthRevenue = invoices.filter(function(i) { return i.status === 'Paid' && i.paidOn && i.paidOn.slice(0,7) === thisMonth; })
      .reduce(function(acc, i) { return acc + i.amount; }, 0);
    var absentStaff    = attendance.filter(function(a) { return a.personType === 'staff' && a.date === today && a.status === 'Absent'; });
    var recentStudents = students.slice().sort(function(a, b) { return b.registeredOn.localeCompare(a.registeredOn); }).slice(0, 5);
    var latestAnnounce = announcements.slice().sort(function(a, b) { return b.createdOn.localeCompare(a.createdOn); }).slice(0, 4);

    var attn = [];
    if (pendingRegs > 0)       attn.push({ sev:'info',    title: pendingRegs + ' pending registration' + (pendingRegs !== 1 ? 's' : ''),        sub: 'Awaiting admin review',      page: 'students'  });
    if (overdueInvs.length > 0) attn.push({ sev:'error',  title: overdueInvs.length + ' overdue invoice' + (overdueInvs.length !== 1 ? 's' : ''), sub: 'Payment collection needed',   page: 'billing'   });
    if (dueSoonInvs.length > 0) attn.push({ sev:'warning',title: dueSoonInvs.length + ' payment' + (dueSoonInvs.length !== 1 ? 's' : '') + ' due this week', sub: 'Send reminders to parents', page: 'billing' });
    if (absentStaff.length > 0) attn.push({ sev:'error',  title: absentStaff.length + ' staff absent today',                                       sub: 'Check class coverage',       page: 'attendance'});
    if (newStudents > 0)         attn.push({ sev:'info',   title: newStudents + ' new student' + (newStudents !== 1 ? 's' : '') + ' awaiting activation', sub: 'Review and activate',    page: 'students'  });

    return '<div style="display:flex;flex-direction:column;gap:1rem;">'

      // ── Greeting bar ──
      + '<div style="display:flex;align-items:flex-end;justify-content:space-between;flex-wrap:wrap;gap:0.5rem">'
      +   '<div>'
      +     '<h1 style="font-size:1.6rem;font-weight:800;color:#0d0d0d;letter-spacing:-0.04em;line-height:1.15">Good ' + _timeOfDay() + ' 👋</h1>'
      +     '<p style="font-size:0.78rem;color:#94a3b8;margin-top:3px">' + _formatTodayFull() + ' &nbsp;·&nbsp; ' + todayClasses.length + ' class' + (todayClasses.length !== 1 ? 'es' : '') + ' today</p>'
      +   '</div>'
      +   '<div style="display:flex;gap:0.5rem">'
      +     '<button onclick="App.Router.navigate(\'students\')" style="padding:0.45rem 1rem;font-size:0.78rem;font-weight:700;border:none;background:var(--gold);color:#0a0a0a;border-radius:8px;cursor:pointer;letter-spacing:0.02em;transition:opacity 0.15s" onmouseover="this.style.opacity=\'0.82\'" onmouseout="this.style.opacity=\'1\'">+ Student</button>'
      +     '<button onclick="App.Router.navigate(\'billing\')" style="padding:0.45rem 1rem;font-size:0.78rem;font-weight:600;border:1px solid #e2e8f0;background:#fff;color:#374151;border-radius:8px;cursor:pointer;transition:background 0.15s" onmouseover="this.style.background=\'#f8fafc\'" onmouseout="this.style.background=\'#fff\'">+ Invoice</button>'
      +   '</div>'
      + '</div>'

      // ── Stat cards row ──
      + '<div style="display:grid;grid-template-columns:repeat(4,1fr);gap:0.85rem">'
      + _statTile('Active Students', activeStudents, false, '#1e293b',   false,  'students',   'M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2M9 7a4 4 0 100 8 4 4 0 000-8zM23 21v-2a4 4 0 00-3-3.87m-4-12a4 4 0 010 7.75')
      + _statTile('Revenue This Month', monthRevenue, true, 'var(--gold)', true, 'billing',    'M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z')
      + _statTile('Overdue Invoices', overdueInvs.length, false, overdueInvs.length > 0 ? '#dc2626' : '#94a3b8', false, 'billing', 'M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z')
      + _statTile('New Students', newStudents, false, newStudents > 0 ? '#7c3aed' : '#94a3b8', false, 'students', 'M18 9v3m0 0v3m0-3h3m-3 0h-3m-2-5a4 4 0 11-8 0 4 4 0 018 0zM3 20a6 6 0 0112 0v1H3v-1z')
      + '</div>'

      // ── Middle row ──
      + '<div style="display:grid;grid-template-columns:2fr 1fr;gap:0.85rem;align-items:start">'
      + _tileClasses(todayClasses, s, staff, todayDay)
      + _tileAttention(attn)
      + '</div>'

      // ── Bottom row ──
      + '<div style="display:grid;grid-template-columns:1fr 2fr;gap:0.85rem;align-items:start">'
      + _tileRecentStudents(recentStudents, s)
      + _tileAnnouncements(latestAnnounce)
      + '</div>'

    + '</div>';
  }

  // ── Stat tile ────────────────────────────────────────────────────────────────
  function _statTile(label, value, isCurrency, color, goldBorder, page, iconPath) {
    var display = isCurrency ? ('RM\u00a0' + value.toFixed(2).replace(/\B(?=(\d{3})+(?!\d))/g, ',')) : String(value);
    var dataAttr = isCurrency ? 'data-count="' + value + '" data-currency="1"' : 'data-count="' + value + '"';
    return '<div onclick="App.Router.navigate(\'' + page + '\')" class="bento-tile clickable" style="padding:1.1rem 1.2rem;position:relative;' + (goldBorder ? 'border-top:2.5px solid var(--gold);' : '') + '">'
      + '<div class="bento-glow" style="position:absolute;inset:0;opacity:0;background:radial-gradient(circle at 80% 20%,rgba(201,162,39,0.09),transparent 65%);transition:opacity 0.3s;pointer-events:none"></div>'
      + '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.65rem">'
      +   '<span style="font-size:0.68rem;font-weight:600;text-transform:uppercase;letter-spacing:0.06em;color:#94a3b8">' + label + '</span>'
      +   '<svg style="width:1rem;height:1rem;color:' + color + ';opacity:0.55;flex-shrink:0" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" d="' + iconPath + '"/></svg>'
      + '</div>'
      + '<div ' + dataAttr + ' style="font-size:1.65rem;font-weight:800;letter-spacing:-0.04em;color:' + color + ';line-height:1">' + display + '</div>'
      + '</div>';
  }

  // ── Today's classes tile ─────────────────────────────────────────────────────
  function _tileClasses(todayClasses, s, staff, todayDay) {
    var inner;
    if (todayClasses.length === 0) {
      inner = '<div style="padding:2.5rem;text-align:center;color:#94a3b8;font-size:0.85rem">No classes scheduled today</div>';
    } else {
      inner = '<div style="padding:0 0.25rem">'
        + todayClasses.map(function(c) {
            var colors   = App.Utils.colorClasses(c.color);
            var teachers = staff.filter(function(st) { return c.teacherIds.indexOf(st.id) > -1; }).map(function(st) { return st.name; }).join(', ');
            var fillPct  = c.capacity > 0 ? Math.round((c.enrolled / c.capacity) * 100) : 0;
            var isFull   = c.enrolled >= c.capacity;
            return '<div style="display:flex;align-items:center;gap:0.75rem;padding:0.7rem 0;border-bottom:1px solid #f1f5f9">'
              + '<div style="width:3px;border-radius:99px;align-self:stretch;flex-shrink:0" class="' + colors.dot + '"></div>'
              + '<div style="flex:1;min-width:0">'
              +   '<div style="font-weight:600;font-size:0.84rem;color:#1e293b">' + c.name + '</div>'
              +   '<div style="font-size:0.72rem;color:#94a3b8;margin-top:1px">' + App.Utils.formatTime(c.time) + '–' + App.Utils.formatTime(c.endTime) + ' · ' + (teachers || 'Unassigned') + ' · ' + c.classroom + '</div>'
              + '</div>'
              + '<div style="text-align:right;flex-shrink:0">'
              +   '<div style="font-size:0.78rem;font-weight:700;color:' + (isFull ? '#dc2626' : '#64748b') + '">' + c.enrolled + '/' + c.capacity + '</div>'
              +   '<div style="width:48px;height:3px;background:#f1f5f9;border-radius:99px;margin-top:3px;overflow:hidden"><div style="width:' + fillPct + '%;height:100%;background:' + (isFull ? '#ef4444' : 'var(--gold)') + ';border-radius:99px"></div></div>'
              + '</div>'
              + '</div>';
          }).join('')
        + '</div>';
    }
    return '<div class="bento-tile" style="padding:1.1rem 1.2rem">'
      + '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.85rem">'
      +   '<span style="font-weight:700;font-size:0.9rem;color:#1e293b">Today\'s Classes</span>'
      +   '<span style="font-size:0.72rem;color:#94a3b8;font-weight:500">' + todayDay + '</span>'
      + '</div>'
      + inner
      + '</div>';
  }

  // ── Needs attention tile ─────────────────────────────────────────────────────
  function _tileAttention(items) {
    var COLORS = { error:'#dc2626', warning:'#d97706', info:'#2563eb' };
    var DOTS   = { error:'#ef4444', warning:'#f59e0b', info:'#3b82f6' };
    var inner  = items.length === 0
      ? '<div style="padding:2.5rem;text-align:center">'
      +   '<div style="font-size:1.5rem;margin-bottom:6px">✓</div>'
      +   '<div style="font-size:0.82rem;color:#94a3b8">All clear!</div>'
      +   '</div>'
      : items.map(function(item) {
          return '<div onclick="App.Router.navigate(\'' + item.page + '\')" style="display:flex;align-items:center;gap:0.65rem;padding:0.65rem 0;border-bottom:1px solid #f1f5f9;cursor:pointer;border-radius:6px;transition:background 0.15s" onmouseover="this.style.background=\'#fafbfc\'" onmouseout="this.style.background=\'transparent\'">'
            + '<div style="width:7px;height:7px;border-radius:50%;background:' + (DOTS[item.sev] || DOTS.info) + ';flex-shrink:0"></div>'
            + '<div style="flex:1;min-width:0">'
            +   '<div style="font-size:0.81rem;font-weight:600;color:#1e293b">' + item.title + '</div>'
            +   '<div style="font-size:0.7rem;color:#94a3b8;margin-top:1px">' + item.sub + '</div>'
            + '</div>'
            + '<svg style="width:0.9rem;height:0.9rem;color:#cbd5e1;flex-shrink:0" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" d="M9 5l7 7-7 7"/></svg>'
            + '</div>';
        }).join('');

    return '<div class="bento-tile" style="padding:1.1rem 1.2rem">'
      + '<div style="margin-bottom:0.85rem">'
      +   '<span style="font-weight:700;font-size:0.9rem;color:#1e293b">Needs Attention</span>'
      + '</div>'
      + inner
      + '</div>';
  }

  // ── Recent students tile ─────────────────────────────────────────────────────
  function _tileRecentStudents(students, s) {
    var rows = students.map(function(stu) {
      var cls = (s.classes || []).filter(function(c) { return stu.enrolledClasses.indexOf(c.id) > -1; }).map(function(c) { return c.name; }).join(', ');
      var initials = stu.firstName.charAt(0).toUpperCase();
      return '<div style="display:flex;align-items:center;gap:0.65rem;padding:0.6rem 0;border-bottom:1px solid #f1f5f9">'
        + '<div style="width:2rem;height:2rem;border-radius:50%;background:var(--gold-dim);color:var(--gold);font-weight:800;font-size:0.8rem;display:flex;align-items:center;justify-content:center;flex-shrink:0;border:1px solid rgba(201,162,39,0.25)">' + initials + '</div>'
        + '<div style="flex:1;min-width:0">'
        +   '<div style="font-size:0.82rem;font-weight:600;color:#1e293b;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + stu.firstName + ' ' + stu.lastName + '</div>'
        +   '<div style="font-size:0.7rem;color:#94a3b8;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + (cls || 'No class') + '</div>'
        + '</div>'
        + App.Utils.statusBadge(stu.status)
        + '</div>';
    }).join('');

    return '<div class="bento-tile" style="padding:1.1rem 1.2rem">'
      + '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.85rem">'
      +   '<span style="font-weight:700;font-size:0.9rem;color:#1e293b">Recent Students</span>'
      +   '<button onclick="App.Router.navigate(\'students\')" style="font-size:0.7rem;font-weight:600;color:var(--gold);background:none;border:none;cursor:pointer;padding:0">View all →</button>'
      + '</div>'
      + rows
      + '</div>';
  }

  // ── Announcements tile ───────────────────────────────────────────────────────
  function _tileAnnouncements(announcements) {
    var TYPE_COLORS = { Notice: '#2563eb', Reminder: '#d97706', Urgent: '#dc2626' };
    var TYPE_BG     = { Notice: '#eff6ff', Reminder: '#fffbeb', Urgent: '#fef2f2' };

    var inner = announcements.length === 0
      ? '<div style="padding:2.5rem;text-align:center;color:#94a3b8;font-size:0.82rem">No announcements yet</div>'
      : '<div style="display:grid;grid-template-columns:1fr 1fr;gap:0.6rem">'
        + announcements.map(function(a) {
            var tc = TYPE_COLORS[a.type] || '#2563eb';
            var tb = TYPE_BG[a.type]     || '#eff6ff';
            return '<div style="padding:0.75rem;background:#fafbfc;border:1px solid #f1f5f9;border-radius:10px;border-left:3px solid ' + tc + '">'
              + '<div style="display:flex;align-items:center;gap:0.4rem;margin-bottom:0.35rem">'
              +   '<span style="font-size:0.63rem;font-weight:700;color:' + tc + ';text-transform:uppercase;letter-spacing:0.05em;background:' + tb + ';padding:1px 6px;border-radius:4px">' + a.type + '</span>'
              +   '<span style="font-size:0.65rem;color:#94a3b8">' + App.Utils.formatDate(a.createdOn) + '</span>'
              + '</div>'
              + '<div style="font-size:0.81rem;font-weight:600;color:#1e293b;line-height:1.3">' + a.title + '</div>'
              + '<div style="font-size:0.71rem;color:#64748b;margin-top:3px;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden">' + a.message + '</div>'
              + '</div>';
          }).join('')
        + '</div>';

    return '<div class="bento-tile" style="padding:1.1rem 1.2rem">'
      + '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.85rem">'
      +   '<span style="font-weight:700;font-size:0.9rem;color:#1e293b">Announcements</span>'
      +   '<button onclick="App.Router.navigate(\'communication\')" style="font-size:0.7rem;font-weight:600;color:var(--gold);background:none;border:none;cursor:pointer;padding:0">View all →</button>'
      + '</div>'
      + inner
      + '</div>';
  }

  // ── Parent Dashboard ──────────────────────────────────────────────────────────
  function _parentDash() {
    var s = App.Store.get();
    var students      = s.students      || [];
    var classes       = s.classes       || [];
    var invoices      = s.invoices      || [];
    var announcements = s.announcements || [];
    var attendance    = s.attendance    || [];

    var myStudents = students.filter(function(st) { return st.contact === App.clientParent; });
    var myIds      = myStudents.map(function(st) { return st.id; });
    var myInvoices = invoices.filter(function(i) { return myIds.indexOf(i.studentId) > -1; });

    var now = new Date();
    var in7 = new Date(now); in7.setDate(now.getDate() + 7);
    var today = App.Utils.today();

    var overdueInvs = myInvoices.filter(function(i) { return i.status === 'Overdue'; });
    var dueSoonInvs = myInvoices.filter(function(i) {
      if (i.status !== 'Unpaid') return false;
      var d = new Date(i.dueDate); return d >= now && d <= in7;
    });
    var totalOwed = myInvoices.filter(function(i) { return i.status === 'Overdue' || i.status === 'Unpaid'; })
      .reduce(function(acc, i) { return acc + i.amount; }, 0);

    var enrolledIds = [];
    myStudents.forEach(function(st) { st.enrolledClasses.forEach(function(id) { if (enrolledIds.indexOf(id) === -1) enrolledIds.push(id); }); });
    var myClasses = classes.filter(function(c) { return enrolledIds.indexOf(c.id) > -1; });
    var latestAnnounce = announcements.slice().sort(function(a, b) { return b.createdOn.localeCompare(a.createdOn); }).slice(0, 3);
    var childNames = myStudents.map(function(st) { return st.firstName; }).join(' & ');

    // Recent attendance for my kids
    var myAttendance = attendance.filter(function(a) { return myIds.indexOf(a.personId) > -1; })
      .sort(function(a, b) { return b.date.localeCompare(a.date); }).slice(0, 5);

    return '<div style="display:flex;flex-direction:column;gap:1rem;">'

      // Greeting
      + '<div>'
      +   '<h1 style="font-size:1.5rem;font-weight:800;color:#0d0d0d;letter-spacing:-0.04em">Welcome back' + (childNames ? ', ' + childNames.split(' & ')[0] + '\'s family' : '') + ' 👋</h1>'
      +   '<p style="font-size:0.78rem;color:#94a3b8;margin-top:3px">' + _formatTodayFull() + '</p>'
      + '</div>'

      // Alert banner
      + (overdueInvs.length > 0
          ? '<div style="padding:0.85rem 1rem;background:#fef2f2;border:1px solid #fecaca;border-left:3px solid #dc2626;border-radius:10px;display:flex;align-items:center;gap:0.75rem">'
          +   '<div style="flex:1;font-size:0.83rem;color:#991b1b"><strong>Payment overdue</strong> — ' + overdueInvs.length + ' invoice' + (overdueInvs.length !== 1 ? 's are' : ' is') + ' past due. Please settle as soon as possible.</div>'
          +   '<button onclick="App.Router.navigate(\'billing\')" style="font-size:0.75rem;font-weight:700;color:#dc2626;background:none;border:1px solid #fecaca;border-radius:6px;padding:0.3rem 0.6rem;cursor:pointer;flex-shrink:0;white-space:nowrap">Pay Now</button>'
          +   '</div>'
          : dueSoonInvs.length > 0
          ? '<div style="padding:0.85rem 1rem;background:#fffbeb;border:1px solid #fde68a;border-left:3px solid #d97706;border-radius:10px;display:flex;align-items:center;gap:0.75rem">'
          +   '<div style="flex:1;font-size:0.83rem;color:#92400e"><strong>Payment due soon</strong> — ' + dueSoonInvs.length + ' invoice' + (dueSoonInvs.length !== 1 ? 's are' : ' is') + ' due within 7 days.</div>'
          +   '<button onclick="App.Router.navigate(\'billing\')" style="font-size:0.75rem;font-weight:700;color:#d97706;background:none;border:1px solid #fde68a;border-radius:6px;padding:0.3rem 0.6rem;cursor:pointer;flex-shrink:0;white-space:nowrap">View</button>'
          +   '</div>'
          : '')

      // Stat cards
      + '<div style="display:grid;grid-template-columns:repeat(3,1fr);gap:0.85rem">'
      + _statTile('My Children',      myStudents.length, false, '#1e293b', false, 'students', 'M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2M9 7a4 4 0 100 8 4 4 0 000-8z')
      + _statTile('Classes Enrolled', myClasses.length,  false, 'var(--gold)', true, 'calendar', 'M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z')
      + _statTile('Balance Due',      totalOwed, true, totalOwed > 0 ? '#dc2626' : '#94a3b8', false, 'billing', 'M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z')
      + '</div>'

      // Classes + Announcements
      + '<div style="display:grid;grid-template-columns:3fr 2fr;gap:0.85rem;align-items:start">'
      + _tileParentClasses(myClasses, myStudents, s)
      + _tileAnnouncements(latestAnnounce)
      + '</div>'

    + '</div>';
  }

  function _tileParentClasses(myClasses, myStudents, s) {
    var inner = myClasses.length === 0
      ? '<div style="padding:2.5rem;text-align:center;color:#94a3b8;font-size:0.85rem">No classes enrolled yet</div>'
      : myClasses.map(function(c) {
          var colors   = App.Utils.colorClasses(c.color);
          var teachers = (s.staff || []).filter(function(st) { return c.teacherIds.indexOf(st.id) > -1; }).map(function(st) { return st.name; }).join(', ');
          var enrolled = myStudents.filter(function(st) { return st.enrolledClasses.indexOf(c.id) > -1; }).map(function(st) { return st.firstName; }).join(', ');
          return '<div style="display:flex;align-items:center;gap:0.75rem;padding:0.7rem 0;border-bottom:1px solid #f1f5f9">'
            + '<div style="width:3px;border-radius:99px;align-self:stretch;flex-shrink:0" class="' + colors.dot + '"></div>'
            + '<div style="flex:1;min-width:0">'
            +   '<div style="font-weight:600;font-size:0.84rem;color:#1e293b">' + c.name + '</div>'
            +   '<div style="font-size:0.72rem;color:#94a3b8;margin-top:1px">' + c.day + ' · ' + App.Utils.formatTime(c.time) + '–' + App.Utils.formatTime(c.endTime) + ' · ' + (teachers || 'TBC') + '</div>'
            +   '<div style="font-size:0.72rem;color:var(--gold);margin-top:1px;font-weight:600">' + enrolled + ' · ' + c.classroom + '</div>'
            + '</div>'
            + '</div>';
        }).join('');

    return '<div class="bento-tile" style="padding:1.1rem 1.2rem">'
      + '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.85rem">'
      +   '<span style="font-weight:700;font-size:0.9rem;color:#1e293b">Class Schedule</span>'
      +   '<button onclick="App.Router.navigate(\'calendar\')" style="font-size:0.7rem;font-weight:600;color:var(--gold);background:none;border:none;cursor:pointer;padding:0">Full schedule →</button>'
      + '</div>'
      + inner
      + '</div>';
  }

  // ── Shared helpers ────────────────────────────────────────────────────────────
  function _timeOfDay() {
    var h = new Date().getHours();
    if (h < 12) return 'morning';
    if (h < 17) return 'afternoon';
    return 'evening';
  }

  function _formatTodayFull() {
    return new Date().toLocaleDateString('en-MY', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });
  }

  function _setPreview(val) {
    _preview = val;
    var container = document.getElementById('dashboard-page');
    if (container) render(container);
  }

  App.Dashboard = { render: render, _setPreview: _setPreview };
})();
