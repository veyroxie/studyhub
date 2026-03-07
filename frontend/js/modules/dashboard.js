(function() {
  window.App = window.App || {};

  function render(container) {
    var isAdmin = App.currentRole === 'admin';
    container.innerHTML = isAdmin ? _adminDash() : _parentDash();
  }

  // ── Admin Dashboard ──────────────────────────────────────────────────────────
  function _adminDash() {
    var s = App.Store.get();
    var students   = s.students   || [];
    var classes    = s.classes    || [];
    var invoices   = s.invoices   || [];
    var staff      = s.staff      || [];
    var attendance = s.attendance || [];
    var announcements = s.announcements || [];

    var today     = App.Utils.today();
    var todayDay  = new Date(today + 'T00:00:00').toLocaleDateString('en-US', { weekday: 'long' });
    var now       = new Date();
    var in7       = new Date(now); in7.setDate(now.getDate() + 7);

    var registrations   = s.registrations || [];
    var pendingRegs     = registrations.filter(function(r) { return r.status === 'pending'; }).length;
    var activeStudents  = students.filter(function(s) { return s.status === 'Active'; }).length;
    var newStudents     = students.filter(function(s) { return s.status === 'New'; }).length;
    var todayClasses    = classes.filter(function(c) { return c.day === todayDay; });
    var overdueInvs     = invoices.filter(function(i) { return i.status === 'Overdue'; });
    var unpaidInvs      = invoices.filter(function(i) { return i.status === 'Unpaid'; });
    var dueSoonInvs     = unpaidInvs.filter(function(i) { var d = new Date(i.dueDate); return d >= now && d <= in7; });

    // Revenue this month
    var thisMonth = today.slice(0, 7); // YYYY-MM
    var monthRevenue = invoices.filter(function(i) { return i.status === 'Paid' && i.paidOn && i.paidOn.slice(0,7) === thisMonth; })
      .reduce(function(sum, i) { return sum + i.amount; }, 0);

    // Staff absent today
    var absentStaff = attendance.filter(function(a) { return a.personType === 'staff' && a.date === today && a.status === 'Absent'; });

    // Recent students (last 5 by registeredOn)
    var recentStudents = students.slice().sort(function(a, b) { return b.registeredOn.localeCompare(a.registeredOn); }).slice(0, 5);

    // Latest announcements
    var latestAnnouncements = announcements.slice().sort(function(a, b) { return b.createdOn.localeCompare(a.createdOn); }).slice(0, 3);

    return '<div class="space-y-6">'

      // Header
      + '<div class="flex items-center justify-between">'
      +   '<div>'
      +     '<h1 class="text-2xl font-bold text-slate-800">Good ' + _timeOfDay() + '</h1>'
      +     '<p class="text-sm text-slate-500 mt-0.5">' + _formatTodayFull() + ' · ' + todayClasses.length + ' class' + (todayClasses.length !== 1 ? 'es' : '') + ' today</p>'
      +   '</div>'
      +   '<div class="flex gap-2">'
      +     '<button onclick="App.Router.navigate(\'students\')" class="px-3 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700 font-medium">+ Add Student</button>'
      +     '<button onclick="App.Router.navigate(\'billing\')" class="px-3 py-2 text-sm bg-slate-100 text-slate-700 rounded-lg hover:bg-slate-200 font-medium">+ Invoice</button>'
      +   '</div>'
      + '</div>'

      // Stat cards
      + '<div class="grid grid-cols-2 lg:grid-cols-4 gap-4">'
      + _statCard('Active Students',   activeStudents,                           'text-blue-600',   'bg-blue-50',   'M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2M9 7a4 4 0 100 8 4 4 0 000-8zM23 21v-2a4 4 0 00-3-3.87m-4-12a4 4 0 010 7.75')
      + _statCard('Revenue This Month', App.Utils.formatCurrency(monthRevenue),  'text-emerald-600','bg-emerald-50','M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z')
      + _statCard('Overdue Invoices',  overdueInvs.length,                       'text-red-600',    'bg-red-50',    'M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z')
      + _statCard('New Students',      newStudents,                              'text-purple-600', 'bg-purple-50', 'M18 9v3m0 0v3m0-3h3m-3 0h-3m-2-5a4 4 0 11-8 0 4 4 0 018 0zM3 20a6 6 0 0112 0v1H3v-1z')
      + '</div>'

      // Middle row: Today's classes + Action items
      + '<div class="grid grid-cols-1 lg:grid-cols-2 gap-6">'

        // Today's classes
        + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm">'
        +   '<div class="px-5 py-4 border-b border-slate-100 flex items-center justify-between">'
        +     '<h2 class="font-semibold text-slate-800">Today\'s Classes</h2>'
        +     '<span class="text-xs text-slate-400">' + todayDay + '</span>'
        +   '</div>'
        +   (todayClasses.length === 0
              ? '<div class="py-10 text-center text-slate-400 text-sm">No classes scheduled today</div>'
              : '<div class="divide-y divide-slate-50">'
              + todayClasses.map(function(c) {
                  var colors = App.Utils.colorClasses(c.color);
                  var teachers = (s.staff || []).filter(function(st) { return c.teacherIds.indexOf(st.id) > -1; }).map(function(st) { return st.name; }).join(', ');
                  var fillPct = c.capacity > 0 ? Math.round((c.enrolled / c.capacity) * 100) : 0;
                  var full = c.enrolled >= c.capacity;
                  return '<div class="px-5 py-3 flex items-center gap-3">'
                    + '<div class="w-1 h-10 rounded-full ' + colors.dot + ' shrink-0"></div>'
                    + '<div class="flex-1 min-w-0">'
                    +   '<div class="font-medium text-slate-800 text-sm">' + c.name + '</div>'
                    +   '<div class="text-xs text-slate-400">' + App.Utils.formatTime(c.time) + '–' + App.Utils.formatTime(c.endTime) + ' · ' + (teachers || 'Unassigned') + '</div>'
                    + '</div>'
                    + '<div class="text-right shrink-0">'
                    +   '<div class="text-xs font-semibold ' + (full ? 'text-red-600' : 'text-slate-600') + '">' + c.enrolled + '/' + c.capacity + '</div>'
                    +   '<div class="text-xs text-slate-400">' + c.classroom + '</div>'
                    + '</div>'
                    + '</div>';
                }).join('')
              + '</div>')
        + '</div>'

        // Action items
        + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm">'
        +   '<div class="px-5 py-4 border-b border-slate-100">'
        +     '<h2 class="font-semibold text-slate-800">Needs Attention</h2>'
        +   '</div>'
        +   '<div class="divide-y divide-slate-50">'
        +   (overdueInvs.length === 0 && dueSoonInvs.length === 0 && absentStaff.length === 0 && pendingRegs === 0
              ? '<div class="py-10 text-center text-slate-400 text-sm">All clear!</div>'
              : '')
        +   (pendingRegs > 0
              ? _actionItem('info', pendingRegs + ' pending registration' + (pendingRegs > 1 ? 's' : ''), 'New enrolment applications to review', 'students')
              : '')
        +   (overdueInvs.length > 0
              ? _actionItem('error', overdueInvs.length + ' overdue invoice' + (overdueInvs.length > 1 ? 's' : ''), 'Collect payment from parents', 'billing')
              : '')
        +   (dueSoonInvs.length > 0
              ? _actionItem('warning', dueSoonInvs.length + ' payment' + (dueSoonInvs.length > 1 ? 's' : '') + ' due this week', 'Send reminders to parents', 'billing')
              : '')
        +   (absentStaff.length > 0
              ? _actionItem('error', absentStaff.length + ' staff absent today', 'Check class coverage', 'attendance')
              : '')
        +   (newStudents > 0
              ? _actionItem('info', newStudents + ' new student' + (newStudents > 1 ? 's' : '') + ' enrolled', 'Review and activate accounts', 'students')
              : '')
        +   '</div>'
        + '</div>'

      + '</div>'

      // Bottom row: Recent students + Announcements
      + '<div class="grid grid-cols-1 lg:grid-cols-2 gap-6">'

        // Recent students
        + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm">'
        +   '<div class="px-5 py-4 border-b border-slate-100 flex items-center justify-between">'
        +     '<h2 class="font-semibold text-slate-800">Recent Students</h2>'
        +     '<button onclick="App.Router.navigate(\'students\')" class="text-xs text-blue-600 hover:text-blue-800 font-medium">View all</button>'
        +   '</div>'
        +   '<div class="divide-y divide-slate-50">'
        +   recentStudents.map(function(stu) {
              var cls = (s.classes || []).filter(function(c) { return stu.enrolledClasses.indexOf(c.id) > -1; }).map(function(c) { return c.name; }).join(', ');
              return '<div class="px-5 py-3 flex items-center gap-3">'
                + '<div class="w-8 h-8 rounded-full bg-blue-100 text-blue-700 font-bold text-sm flex items-center justify-center shrink-0">' + stu.firstName.charAt(0) + '</div>'
                + '<div class="flex-1 min-w-0">'
                +   '<div class="text-sm font-medium text-slate-800">' + stu.firstName + ' ' + stu.lastName + '</div>'
                +   '<div class="text-xs text-slate-400 truncate">' + (cls || 'No class') + '</div>'
                + '</div>'
                + App.Utils.statusBadge(stu.status)
                + '</div>';
            }).join('')
        +   '</div>'
        + '</div>'

        // Announcements
        + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm">'
        +   '<div class="px-5 py-4 border-b border-slate-100 flex items-center justify-between">'
        +     '<h2 class="font-semibold text-slate-800">Announcements</h2>'
        +     '<button onclick="App.Router.navigate(\'communication\')" class="text-xs text-blue-600 hover:text-blue-800 font-medium">View all</button>'
        +   '</div>'
        +   (latestAnnouncements.length === 0
              ? '<div class="py-10 text-center text-slate-400 text-sm">No announcements yet</div>'
              : '<div class="divide-y divide-slate-50">'
              + latestAnnouncements.map(function(a) {
                  var typeColors = { Notice:'blue', Reminder:'yellow', Urgent:'red' };
                  return '<div class="px-5 py-3">'
                    + '<div class="flex items-center gap-2 mb-1">'
                    +   App.Utils.badge(a.type, typeColors[a.type] || 'blue')
                    +   '<span class="text-xs text-slate-400">' + App.Utils.formatDate(a.createdOn) + '</span>'
                    + '</div>'
                    + '<div class="text-sm font-medium text-slate-800">' + a.title + '</div>'
                    + '<div class="text-xs text-slate-500 mt-0.5 line-clamp-1">' + a.message + '</div>'
                    + '</div>';
                }).join('')
              + '</div>')
        + '</div>'

      + '</div>'
    + '</div>';
  }

  // ── Parent Dashboard ─────────────────────────────────────────────────────────
  function _parentDash() {
    var s = App.Store.get();
    var students      = s.students      || [];
    var classes       = s.classes       || [];
    var invoices      = s.invoices      || [];
    var announcements = s.announcements || [];

    var myStudents = students.filter(function(st) { return st.contact === App.clientParent; });
    var myIds      = myStudents.map(function(st) { return st.id; });
    var myInvoices = invoices.filter(function(i) { return myIds.indexOf(i.studentId) > -1; });

    var now = new Date();
    var in7 = new Date(now); in7.setDate(now.getDate() + 7);

    var overdueInvs = myInvoices.filter(function(i) { return i.status === 'Overdue'; });
    var dueSoonInvs = myInvoices.filter(function(i) {
      if (i.status !== 'Unpaid') return false;
      var d = new Date(i.dueDate);
      return d >= now && d <= in7;
    });
    var totalOwed = overdueInvs.concat(myInvoices.filter(function(i) { return i.status === 'Unpaid'; }))
      .reduce(function(sum, i) { return sum + i.amount; }, 0);

    // Enrolled classes across all children
    var enrolledClassIds = [];
    myStudents.forEach(function(st) { st.enrolledClasses.forEach(function(cid) { if (enrolledClassIds.indexOf(cid) === -1) enrolledClassIds.push(cid); }); });
    var myClasses = classes.filter(function(c) { return enrolledClassIds.indexOf(c.id) > -1; });

    var latestAnnouncements = announcements.slice().sort(function(a, b) { return b.createdOn.localeCompare(a.createdOn); }).slice(0, 3);

    var childNames = myStudents.map(function(st) { return st.firstName; }).join(' & ');

    return '<div class="space-y-6">'

      // Header
      + '<div>'
      +   '<h1 class="text-2xl font-bold text-slate-800">Welcome back!</h1>'
      +   '<p class="text-sm text-slate-500 mt-0.5">' + (childNames ? 'Managing: ' + childNames : 'Parent portal') + ' · ' + _formatTodayFull() + '</p>'
      + '</div>'

      // Alert banners
      + (overdueInvs.length > 0
          ? '<div class="px-4 py-3 bg-red-50 border border-red-200 rounded-xl flex items-center gap-3">'
          +   '<div class="w-2 h-2 rounded-full bg-red-500 shrink-0"></div>'
          +   '<div class="text-sm text-red-700"><span class="font-semibold">Payment overdue</span> — ' + overdueInvs.length + ' invoice' + (overdueInvs.length > 1 ? 's are' : ' is') + ' past due. Please settle as soon as possible.</div>'
          +   '<button onclick="App.Router.navigate(\'billing\')" class="ml-auto text-xs font-semibold text-red-600 hover:text-red-800 whitespace-nowrap">Pay Now</button>'
          +   '</div>'
          : dueSoonInvs.length > 0
          ? '<div class="px-4 py-3 bg-amber-50 border border-amber-200 rounded-xl flex items-center gap-3">'
          +   '<div class="w-2 h-2 rounded-full bg-amber-500 shrink-0"></div>'
          +   '<div class="text-sm text-amber-700"><span class="font-semibold">Payment due soon</span> — ' + dueSoonInvs.length + ' invoice' + (dueSoonInvs.length > 1 ? 's are' : ' is') + ' due within 7 days.</div>'
          +   '<button onclick="App.Router.navigate(\'billing\')" class="ml-auto text-xs font-semibold text-amber-600 hover:text-amber-800 whitespace-nowrap">View</button>'
          +   '</div>'
          : '')

      // Stat cards
      + '<div class="grid grid-cols-3 gap-4">'
      + _statCard('My Children',     myStudents.length,                    'text-blue-600',   'bg-blue-50',   'M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2M9 7a4 4 0 100 8 4 4 0 000-8z')
      + _statCard('Classes Enrolled', myClasses.length,                   'text-emerald-600','bg-emerald-50','M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z')
      + _statCard('Balance Due',      App.Utils.formatCurrency(totalOwed), 'text-amber-600',  'bg-amber-50',  'M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z')
      + '</div>'

      // Class schedule
      + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm">'
      +   '<div class="px-5 py-4 border-b border-slate-100">'
      +     '<h2 class="font-semibold text-slate-800">Class Schedule</h2>'
      +   '</div>'
      +   (myClasses.length === 0
            ? '<div class="py-10 text-center text-slate-400 text-sm">No classes enrolled yet</div>'
            : '<div class="divide-y divide-slate-50">'
            + myClasses.map(function(c) {
                var colors   = App.Utils.colorClasses(c.color);
                var teachers = (s.staff || []).filter(function(st) { return c.teacherIds.indexOf(st.id) > -1; }).map(function(st) { return st.name; }).join(', ');
                var enrolled = myStudents.filter(function(st) { return st.enrolledClasses.indexOf(c.id) > -1; }).map(function(st) { return st.firstName; }).join(', ');
                return '<div class="px-5 py-3 flex items-center gap-3">'
                  + '<div class="w-1 h-12 rounded-full ' + colors.dot + ' shrink-0"></div>'
                  + '<div class="flex-1">'
                  +   '<div class="text-sm font-medium text-slate-800">' + c.name + '</div>'
                  +   '<div class="text-xs text-slate-400">' + c.day + ' · ' + App.Utils.formatTime(c.time) + '–' + App.Utils.formatTime(c.endTime) + ' · ' + (teachers || 'TBC') + '</div>'
                  +   '<div class="text-xs text-blue-600 mt-0.5">' + enrolled + '</div>'
                  + '</div>'
                  + '<div class="text-xs text-slate-400">' + c.classroom + '</div>'
                  + '</div>';
              }).join('')
            + '</div>')
      + '</div>'

      // Announcements
      + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm">'
      +   '<div class="px-5 py-4 border-b border-slate-100 flex items-center justify-between">'
      +     '<h2 class="font-semibold text-slate-800">Latest Announcements</h2>'
      +     '<button onclick="App.Router.navigate(\'communication\')" class="text-xs text-blue-600 hover:text-blue-800 font-medium">View all</button>'
      +   '</div>'
      +   (latestAnnouncements.length === 0
            ? '<div class="py-10 text-center text-slate-400 text-sm">No announcements yet</div>'
            : '<div class="divide-y divide-slate-50">'
            + latestAnnouncements.map(function(a) {
                var typeColors = { Notice:'blue', Reminder:'yellow', Urgent:'red' };
                return '<div class="px-5 py-4">'
                  + '<div class="flex items-center gap-2 mb-1">'
                  +   App.Utils.badge(a.type, typeColors[a.type] || 'blue')
                  +   '<span class="text-xs text-slate-400">' + App.Utils.formatDate(a.createdOn) + '</span>'
                  + '</div>'
                  + '<div class="text-sm font-medium text-slate-800">' + a.title + '</div>'
                  + '<div class="text-xs text-slate-500 mt-0.5">' + a.message + '</div>'
                  + '</div>';
              }).join('')
            + '</div>')
      + '</div>'
    + '</div>';
  }

  // ── Helpers ──────────────────────────────────────────────────────────────────
  function _statCard(label, value, textClass, bgClass, iconPath) {
    return '<div class="' + bgClass + ' rounded-xl border border-slate-100 shadow-sm p-4 flex items-start gap-3">'
      + '<div class="w-9 h-9 rounded-lg bg-white/70 flex items-center justify-center shrink-0 ' + textClass + '">'
      +   '<svg class="w-5 h-5" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" d="' + iconPath + '"/></svg>'
      + '</div>'
      + '<div>'
      +   '<div class="text-xl font-bold ' + textClass + '">' + value + '</div>'
      +   '<div class="text-xs text-slate-500 mt-0.5">' + label + '</div>'
      + '</div>'
      + '</div>';
  }

  function _actionItem(severity, title, subtitle, page) {
    var colors = { error: 'bg-red-100 text-red-600', warning: 'bg-amber-100 text-amber-600', info: 'bg-blue-100 text-blue-600' };
    var dots   = { error: 'bg-red-500', warning: 'bg-amber-400', info: 'bg-blue-500' };
    return '<div onclick="App.Router.navigate(\'' + page + '\')" class="px-5 py-3 flex items-center gap-3 hover:bg-slate-50 cursor-pointer transition-colors">'
      + '<div class="w-2 h-2 rounded-full ' + (dots[severity] || dots.info) + ' shrink-0"></div>'
      + '<div class="flex-1">'
      +   '<div class="text-sm font-medium text-slate-800">' + title + '</div>'
      +   '<div class="text-xs text-slate-400">' + subtitle + '</div>'
      + '</div>'
      + '<svg class="w-4 h-4 text-slate-300 shrink-0" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" d="M9 5l7 7-7 7"/></svg>'
      + '</div>';
  }

  function _timeOfDay() {
    var h = new Date().getHours();
    if (h < 12) return 'morning';
    if (h < 17) return 'afternoon';
    return 'evening';
  }

  function _formatTodayFull() {
    return new Date().toLocaleDateString('en-MY', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });
  }

  App.Dashboard = { render: render };
})();
