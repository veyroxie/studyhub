(function() {
  window.App = window.App || {};

  function render(container) {
    var isAdmin = App.currentRole === 'admin';
    container.innerHTML = isAdmin ? _adminDash() : _parentDash();
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
      +   '<div style="display:flex;gap:0.5rem;flex-wrap:wrap">'
      +     qbtn('+ Student',   "App.Router.navigate('students')",   true)
      +     qbtn('+ Invoice',   "App.Router.navigate('billing')",    false)
      +     qbtn('+ Announce',  "App.Router.navigate('communication')", false)
      +   '</div>'
      + '</div>'
      + '</div>' // close greeting card

      // Stats
      + '<div style="display:grid;grid-template-columns:repeat(4,1fr);gap:0.75rem">'
      + stat('Active Students', activeStudents,  false, '#0d0d0d',   false)
      + stat('Revenue / Month', monthRevenue,    true,  'var(--gold)', true)
      + stat('Overdue Invoices',overdueInvs.length, false, overdueInvs.length > 0 ? '#dc2626' : '#94a3b8', false)
      + stat('Pending Regs',   pendingRegs,     false, pendingRegs > 0 ? '#d97706' : '#94a3b8', false)
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
                return '<div style="display:flex;align-items:center;gap:0.75rem;padding:0.65rem 0;border-bottom:1px solid #f4f4f2">'
                  + '<div style="width:3px;border-radius:99px;align-self:stretch;min-height:36px;flex-shrink:0" class="' + colors.dot + '"></div>'
                  + '<div style="flex:1;min-width:0">'
                  +   '<div style="font-weight:600;font-size:0.85rem;color:#111">' + c.name + '</div>'
                  +   '<div style="font-size:0.72rem;color:#94a3b8;margin-top:2px">' + App.Utils.formatTime(c.time) + '–' + App.Utils.formatTime(c.endTime) + (teachers ? '&nbsp;·&nbsp;' + teachers : '') + '&nbsp;·&nbsp;' + c.classroom + '</div>'
                  + '</div>'
                  + '<div style="display:flex;align-items:center;gap:0.75rem;flex-shrink:0">'
                  +   '<div style="text-align:right">'
                  +     '<div style="font-size:0.8rem;font-weight:700;color:' + (pct >= 100 ? '#dc2626' : '#374151') + '">' + c.enrolled + '/' + c.capacity + '</div>'
                  +     '<div style="width:44px;height:3px;background:#f1f5f9;border-radius:99px;margin-top:3px;overflow:hidden"><div style="width:' + Math.min(pct,100) + '%;height:100%;background:' + (pct>=100?'#ef4444':'var(--gold)') + ';border-radius:99px"></div></div>'
                  +   '</div>'
                  +   '<button onclick="App.Router.navigate(\'attendance\')" style="font-size:0.7rem;font-weight:600;padding:0.25rem 0.6rem;border:1px solid rgba(201,162,39,0.3);border-radius:6px;background:var(--gold-dim);color:var(--gold);cursor:pointer;white-space:nowrap;transition:all 0.15s" onmouseover="this.style.background=\'var(--gold)\';this.style.color=\'#0a0a0a\'" onmouseout="this.style.background=\'var(--gold-dim)\';this.style.color=\'var(--gold)\'">Mark</button>'
                  + '</div>'
                  + '</div>';
              }).join(''))
        + '</div>' // close today card

        // Needs attention
        + card('Needs Attention')
        + _attnItems(pendingRegs, overdueInvs, dueSoonInvs, newStudents)
        + '</div>' // close attn card

      + '</div>' // close two-col

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
        + '</div>'

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
        + '</div>'

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

    var myStudents = students.filter(function(st) { return st.contact === App.clientParent; });
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
      + stat('My Children',      myStudents.length, false, '#0d0d0d',    false)
      + stat('Classes Enrolled', myClasses.length,  false, 'var(--gold)', true)
      + stat('Balance Due',      totalOwed,         true,  totalOwed > 0 ? '#dc2626' : '#94a3b8', false)
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
        + '</div>'

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
        + '</div>'

      + '</div>';
  }

  // ── Helpers ───────────────────────────────────────────────────────────────────

  // card() opens a card div with a heading; caller appends content + closing </div>
  function card(title) {
    return '<div style="background:#fff;border-radius:14px;border:1px solid #ebebeb;padding:1.25rem 1.35rem;box-shadow:0 1px 3px rgba(0,0,0,0.04)">'
      + (title ? '<p style="font-size:0.82rem;font-weight:700;color:#111;margin:0 0 1rem;letter-spacing:-0.01em">' + title + '</p>' : '');
  }

  function stat(label, value, isCurr, color, goldBorder) {
    var display = isCurr
      ? 'RM\u00a0' + value.toFixed(2).replace(/\B(?=(\d{3})+(?!\d))/g, ',')
      : String(value);
    var dataAttr = isCurr
      ? 'data-count="' + value + '" data-currency="1"'
      : 'data-count="' + value + '"';
    return '<div style="background:#fff;border-radius:14px;border:1px solid #ebebeb;padding:1.1rem 1.2rem;box-shadow:0 1px 3px rgba(0,0,0,0.04);' + (goldBorder ? 'border-top:2.5px solid var(--gold);' : '') + '">'
      + '<p style="font-size:0.68rem;font-weight:600;text-transform:uppercase;letter-spacing:0.07em;color:#94a3b8;margin:0 0 0.5rem">' + label + '</p>'
      + '<p ' + dataAttr + ' style="font-size:1.75rem;font-weight:800;letter-spacing:-0.05em;line-height:1;color:' + color + ';margin:0">' + display + '</p>'
      + '</div>';
  }

  function qbtn(label, onclick, primary) {
    return '<button onclick="' + onclick + '" style="padding:0.45rem 0.9rem;font-size:0.78rem;font-weight:700;border-radius:8px;cursor:pointer;transition:opacity 0.15s;'
      + (primary ? 'background:var(--gold);color:#0a0a0a;border:none;' : 'background:#fff;color:#374151;border:1px solid #e2e8f0;')
      + '" onmouseover="this.style.opacity=\'0.8\'" onmouseout="this.style.opacity=\'1\'">' + label + '</button>';
  }

  function _attnItems(pendingRegs, overdueInvs, dueSoonInvs, newStudents) {
    var DOTS = { error:'#ef4444', warning:'#f59e0b', info:'#3b82f6' };
    var items = [];
    if (pendingRegs > 0)       items.push({ sev:'info',    title: pendingRegs + ' pending reg' + (pendingRegs!==1?'s':''),    page:'students'   });
    if (overdueInvs.length > 0) items.push({ sev:'error',  title: overdueInvs.length + ' overdue invoice' + (overdueInvs.length!==1?'s':''), page:'billing' });
    if (dueSoonInvs.length > 0) items.push({ sev:'warning',title: dueSoonInvs.length + ' payment' + (dueSoonInvs.length!==1?'s':'') + ' due this week', page:'billing' });
    if (newStudents > 0)        items.push({ sev:'info',   title: newStudents + ' new student' + (newStudents!==1?'s':'') + ' to activate', page:'students' });

    if (items.length === 0) {
      return '<div style="padding:1.5rem 0;text-align:center"><div style="font-size:1.3rem;margin-bottom:4px">✓</div><p style="font-size:0.82rem;color:#94a3b8;margin:0">All clear</p></div>';
    }
    return items.map(function(item) {
      return '<div onclick="App.Router.navigate(\'' + item.page + '\')" style="display:flex;align-items:center;gap:0.65rem;padding:0.6rem 0.5rem;margin:0 -0.5rem;border-radius:8px;cursor:pointer;transition:background 0.15s;border-bottom:1px solid #f4f4f2" onmouseover="this.style.background=\'#fafaf8\'" onmouseout="this.style.background=\'transparent\'">'
        + '<span style="width:7px;height:7px;border-radius:50%;background:' + (DOTS[item.sev]||DOTS.info) + ';flex-shrink:0"></span>'
        + '<span style="flex:1;font-size:0.81rem;font-weight:600;color:#111">' + item.title + '</span>'
        + '<svg style="width:0.85rem;height:0.85rem;color:#d1d5db;flex-shrink:0" fill="none" stroke="currentColor" stroke-width="2.5" viewBox="0 0 24 24"><path stroke-linecap="round" d="M9 5l7 7-7 7"/></svg>'
        + '</div>';
    }).join('');
  }

  function _timeOfDay() {
    var h = new Date().getHours();
    return h < 12 ? 'morning' : h < 17 ? 'afternoon' : 'evening';
  }

  function _dateFull() {
    return new Date().toLocaleDateString('en-MY', { weekday:'long', day:'numeric', month:'long', year:'numeric' });
  }

  App.Dashboard = { render: render };
})();
