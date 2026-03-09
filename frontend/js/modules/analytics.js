(function() {
  window.App = window.App || {};

  let _filterStudent = '';  // '' = all students
  let _filterTeacher = '';  // '' = all staff
  let _filterCategory = ''; // '' = all, or 'Academic', 'Non-academic', 'Workshop'
  let _filterMonths = 6;    // number of months to show
  let _filterView = 'overview'; // 'overview' | 'financial' | 'bystudents' | 'byteachers' | 'bysubject'

  let _charts = {};

  function render(container) {
    const { students, staff, classes, invoices, attendance } = App.Store.get();

    function _vbtn(v, label) {
      var active = _filterView === v;
      return '<button onclick="App.Analytics._setView(\'' + v + '\')" style="padding:0.3rem 0.85rem;font-size:0.75rem;font-weight:600;border:none;border-radius:6px;cursor:pointer;white-space:nowrap;background:' + (active ? 'var(--gold, #f59e0b)' : 'transparent') + ';color:' + (active ? '#0a0a0a' : '#94a3b8') + '">' + label + '</button>';
    }
    const viewToggle = '<div style="display:flex;gap:0.25rem;background:#f1f5f9;border-radius:8px;padding:3px;margin-bottom:1rem;width:fit-content;flex-wrap:wrap">'
      + _vbtn('overview', 'Overview')
      + _vbtn('financial', 'Financial')
      + _vbtn('bystudents', 'By Students')
      + _vbtn('byteachers', 'By Teachers')
      + _vbtn('bysubject', 'By Subject')
      + '</div>';

    const filterBar = '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);padding:1rem 1.25rem;margin-bottom:1.25rem;display:flex;gap:0.75rem;flex-wrap:wrap;align-items:center">'
      + '<span style="font-size:0.78rem;font-weight:700;color:#94a3b8;text-transform:uppercase;letter-spacing:0.05em;white-space:nowrap">Filter by</span>'
      // Student filter
      + '<select onchange="App.Analytics._setStudent(this.value)" style="padding:0.4rem 0.7rem;font-size:0.82rem;border:1px solid #e2e8f0;border-radius:8px;background:#fff;cursor:pointer;color:#374151">'
      +   '<option value="">All Students</option>'
      +   students.map(function(s) { return '<option value="' + s.id + '"' + (_filterStudent === s.id ? ' selected' : '') + '>' + App.Utils.esc(s.firstName + ' ' + s.lastName) + '</option>'; }).join('')
      + '</select>'
      // Teacher filter
      + '<select onchange="App.Analytics._setTeacher(this.value)" style="padding:0.4rem 0.7rem;font-size:0.82rem;border:1px solid #e2e8f0;border-radius:8px;background:#fff;cursor:pointer;color:#374151">'
      +   '<option value="">All Tutors</option>'
      +   staff.map(function(s) { return '<option value="' + s.id + '"' + (_filterTeacher === s.id ? ' selected' : '') + '>' + App.Utils.esc(s.name) + '</option>'; }).join('')
      + '</select>'
      // Category filter
      + '<select onchange="App.Analytics._setCategory(this.value)" style="padding:0.4rem 0.7rem;font-size:0.82rem;border:1px solid #e2e8f0;border-radius:8px;background:#fff;cursor:pointer;color:#374151">'
      +   '<option value="">All Categories</option>'
      +   ['Academic','Non-academic','Workshop'].map(function(c) { return '<option value="' + c + '"' + (_filterCategory === c ? ' selected' : '') + '>' + c + '</option>'; }).join('')
      + '</select>'
      // Month range filter
      + '<select onchange="App.Analytics._setMonths(this.value)" style="padding:0.4rem 0.7rem;font-size:0.82rem;border:1px solid #e2e8f0;border-radius:8px;background:#fff;cursor:pointer;color:#374151">'
      +   [3,6,12].map(function(m) { return '<option value="' + m + '"' + (_filterMonths === m ? ' selected' : '') + '>Last ' + m + ' months</option>'; }).join('')
      + '</select>'
      + ((_filterStudent || _filterTeacher || _filterCategory || _filterMonths !== 6)
          ? '<button onclick="App.Analytics._clearFilters()" style="padding:0.4rem 0.85rem;font-size:0.8rem;border:none;border-radius:8px;background:#f1f5f9;color:#64748b;cursor:pointer">Clear</button>'
          : '')
      + '</div>';

    // --- Filter classes ---
    let filteredClasses = classes;
    if (_filterTeacher) filteredClasses = filteredClasses.filter(function(c) { return c.teacherIds.indexOf(_filterTeacher) > -1; });
    if (_filterCategory) filteredClasses = filteredClasses.filter(function(c) { return (c.category || 'Academic') === _filterCategory; });
    const filteredClassIds = filteredClasses.map(function(c) { return c.id; });

    // --- Filter students ---
    let filteredStudents = students;
    if (_filterStudent) filteredStudents = filteredStudents.filter(function(s) { return s.id === _filterStudent; });
    if (_filterTeacher || _filterCategory) {
      filteredStudents = filteredStudents.filter(function(s) {
        return s.enrolledClasses.some(function(cid) { return filteredClassIds.indexOf(cid) > -1; });
      });
    }

    // --- Filter attendance ---
    let filteredAttendance = attendance;
    if (_filterStudent || _filterTeacher || _filterCategory) {
      filteredAttendance = attendance.filter(function(a) {
        if (a.personType !== 'student') return false;
        if (_filterStudent && a.personId !== _filterStudent) return false;
        if ((_filterTeacher || _filterCategory) && filteredClassIds.indexOf(a.classId) === -1) return false;
        return true;
      });
    }

    // --- Filter invoices ---
    let filteredInvoices = invoices;
    if (_filterStudent) filteredInvoices = filteredInvoices.filter(function(i) { return i.studentId === _filterStudent; });
    if (_filterTeacher || _filterCategory) {
      const stuIds = filteredStudents.map(function(s) { return s.id; });
      filteredInvoices = filteredInvoices.filter(function(i) { return stuIds.indexOf(i.studentId) > -1; });
    }

    const header = '<div class="flex items-center justify-between mb-6">'
      +   '<h1 class="text-2xl font-bold text-slate-800">Analytics</h1>'
      +   '<span class="text-sm text-slate-400">Last ' + _filterMonths + ' months</span>'
      + '</div>';

    // Destroy old charts
    Object.keys(_charts).forEach(function(k) { if (_charts[k]) { _charts[k].destroy(); delete _charts[k]; } });

    if (_filterView === 'financial') {
      container.innerHTML = viewToggle + filterBar + header + _financialHTML(filteredInvoices, students);
      setTimeout(function() { _buildFinancialCharts(filteredInvoices); }, 50);
      return;
    }

    if (_filterView === 'bystudents') {
      container.innerHTML = viewToggle + filterBar + header + _byStudentsHTML(filteredStudents, filteredAttendance);
      setTimeout(function() { _buildStudentsChart(filteredStudents, filteredAttendance); }, 50);
      return;
    }

    if (_filterView === 'byteachers') {
      container.innerHTML = viewToggle + filterBar + header + _byTeachersHTML(staff, classes, attendance, students);
      setTimeout(function() { _buildTeachersChart(staff, classes, attendance); }, 50);
      return;
    }

    if (_filterView === 'bysubject') {
      container.innerHTML = viewToggle + filterBar + header + _bySubjectHTML(classes, invoices);
      setTimeout(function() { _buildSubjectChart(classes); }, 50);
      return;
    }

    container.innerHTML = viewToggle + filterBar + header
      + '<div class="grid grid-cols-2 gap-5">'
      + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm p-5"><h3 class="font-semibold text-slate-700 mb-4">Enrollment Trend</h3><canvas id="chart-enrollment" height="200"></canvas></div>'
      + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm p-5"><h3 class="font-semibold text-slate-700 mb-4">Class Fill Rate</h3><canvas id="chart-fillrate" height="200"></canvas></div>'
      + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm p-5"><h3 class="font-semibold text-slate-700 mb-4">Revenue Collection</h3><canvas id="chart-revenue" height="200"></canvas></div>'
      + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm p-5"><h3 class="font-semibold text-slate-700 mb-4">Student Attendance Rate</h3><canvas id="chart-attendance" height="200"></canvas></div>'
      + '</div>';

    setTimeout(function() {
      _buildEnrollmentChart(filteredStudents);
      _buildFillRateChart(filteredClasses);
      _buildRevenueChart(filteredInvoices);
      _buildAttendanceChart(filteredStudents, filteredAttendance);
    }, 50);
  }

  function _months(count) {
    const labels = [];
    const keys = [];
    const now = new Date(2026, 2, 1); // March 2026
    for (let i = count - 1; i >= 0; i--) {
      const d = new Date(now.getFullYear(), now.getMonth() - i, 1);
      const monthNames = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
      labels.push(monthNames[d.getMonth()] + ' ' + d.getFullYear().toString().slice(2));
      keys.push(d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0'));
    }
    return { labels: labels, keys: keys };
  }

  function _buildEnrollmentChart(students) {
    const m = _months(_filterMonths);
    let cumulative = 0;
    const counts = m.keys.map(function(key) {
      const inMonth = students.filter(function(s) {
        return s.registeredOn && s.registeredOn.slice(0, 7) === key;
      }).length;
      cumulative += inMonth;
      return cumulative;
    });
    const ctx = document.getElementById('chart-enrollment');
    if (!ctx) return;
    _charts.enrollment = new Chart(ctx.getContext('2d'), {
      type: 'line',
      data: {
        labels: m.labels,
        datasets: [{ label: 'Total Students', data: counts, borderColor: '#3b82f6', backgroundColor: 'rgba(59,130,246,0.1)', tension: 0.4, fill: true, pointBackgroundColor: '#3b82f6' }]
      },
      options: { responsive: true, plugins: { legend: { display: false } }, scales: { y: { beginAtZero: true, grid: { color: '#f1f5f9' } }, x: { grid: { display: false } } } }
    });
  }

  function _buildFillRateChart(classes) {
    const ctx = document.getElementById('chart-fillrate');
    if (!ctx) return;
    const sorted = classes.slice().sort(function(a, b) { return (b.enrolled / b.capacity) - (a.enrolled / a.capacity); });
    const bgColors = sorted.map(function(c) {
      const pct = c.enrolled / c.capacity;
      return pct >= 1 ? '#ef4444' : pct >= 0.7 ? '#f59e0b' : '#10b981';
    });
    _charts.fillrate = new Chart(ctx.getContext('2d'), {
      type: 'bar',
      data: {
        labels: sorted.map(function(c) { return c.name + ' (' + c.day.slice(0,3) + ')'; }),
        datasets: [{ label: 'Fill Rate %', data: sorted.map(function(c) { return Math.round((c.enrolled / c.capacity) * 100); }), backgroundColor: bgColors, borderRadius: 6 }]
      },
      options: { responsive: true, plugins: { legend: { display: false } }, scales: { y: { beginAtZero: true, max: 100, ticks: { callback: function(v) { return v + '%'; } }, grid: { color: '#f1f5f9' } }, x: { grid: { display: false }, ticks: { font: { size: 11 } } } } }
    });
  }

  function _buildRevenueChart(invoices) {
    const m = _months(_filterMonths);
    const paid = m.keys.map(function(key) {
      return invoices.filter(function(i) { return i.status === 'Paid' && i.paidOn && i.paidOn.slice(0, 7) === key; }).reduce(function(s, i) { return s + i.amount; }, 0);
    });
    const pending = m.keys.map(function(key) {
      return invoices.filter(function(i) { return i.status !== 'Paid' && i.createdOn && i.createdOn.slice(0, 7) === key; }).reduce(function(s, i) { return s + i.amount; }, 0);
    });
    const ctx = document.getElementById('chart-revenue');
    if (!ctx) return;
    _charts.revenue = new Chart(ctx.getContext('2d'), {
      type: 'bar',
      data: {
        labels: m.labels,
        datasets: [
          { label: 'Collected', data: paid, backgroundColor: '#10b981', borderRadius: 4 },
          { label: 'Pending', data: pending, backgroundColor: '#fbbf24', borderRadius: 4 }
        ]
      },
      options: { responsive: true, plugins: { legend: { position: 'top' } }, scales: { x: { stacked: false, grid: { display: false } }, y: { grid: { color: '#f1f5f9' }, ticks: { callback: function(v) { return 'RM' + v; } } } } }
    });
  }

  function _buildAttendanceChart(students, attendance) {
    const ctx = document.getElementById('chart-attendance');
    if (!ctx) return;

    // Show ALL active students — even those with zero records (shows 0%)
    const activeStudents = students.filter(function(s) { return s.status === 'Active' || s.status === 'New'; });
    const stuRecords = activeStudents.map(function(s) {
      const records = attendance.filter(function(a) { return a.personId === s.id && a.personType === 'student'; });
      const present = records.filter(function(a) { return a.status === 'Present' || a.status === 'Late'; }).length;
      const pct = records.length > 0 ? Math.round((present / records.length) * 100) : 0;
      return { name: s.firstName, pct: pct, count: records.length };
    });

    const sorted = stuRecords.sort(function(a, b) { return b.pct - a.pct; });
    const bgColors = sorted.map(function(r) {
      if (r.count === 0) return '#e2e8f0'; // slate-200 — no data yet
      return r.pct >= 80 ? '#10b981' : r.pct >= 60 ? '#f59e0b' : '#ef4444';
    });

    _charts.attendance = new Chart(ctx.getContext('2d'), {
      type: 'bar',
      data: {
        labels: sorted.map(function(r) { return r.name; }),
        datasets: [{ label: 'Attendance %', data: sorted.map(function(r) { return r.pct; }), backgroundColor: bgColors, borderRadius: 6 }]
      },
      options: {
        responsive: true,
        plugins: {
          legend: { display: false },
          tooltip: { callbacks: { afterLabel: function(ctx) {
            const r = sorted[ctx.dataIndex];
            return r.count === 0 ? 'No records yet' : r.count + ' session(s) recorded';
          }}}
        },
        scales: {
          y: { beginAtZero: true, max: 100, ticks: { callback: function(v) { return v + '%'; } }, grid: { color: '#f1f5f9' } },
          x: { grid: { display: false }, ticks: { font: { size: 11 } } }
        }
      }
    });
  }

  function _byStudentsHTML(students, attendance) {
    var rows = students.map(function(s) {
      var recs = attendance.filter(function(a) { return a.personId === s.id && a.personType === 'student'; });
      var present = recs.filter(function(a) { return a.status === 'Present' || a.status === 'Late'; }).length;
      var pct = recs.length > 0 ? Math.round(present / recs.length * 100) : null;
      var badge = pct === null
        ? '<span style="color:#94a3b8;font-size:0.78rem">No data</span>'
        : '<span style="padding:0.2rem 0.55rem;border-radius:20px;font-size:0.75rem;font-weight:700;background:' + (pct>=80?'#dcfce7':'pct>=60?#fef3c7':'#fee2e2') + ';color:' + (pct>=80?'#166534':pct>=60?'#92400e':'#991b1b') + '">' + pct + '%</span>';
      var lastAbsent = recs.filter(function(a) { return a.status === 'Absent'; }).sort(function(a,b){ return b.date.localeCompare(a.date); })[0];
      return { name: s.firstName + ' ' + s.lastName, status: s.status, sessions: recs.length, pct: pct === null ? 101 : pct, badge: badge, lastAbsent: lastAbsent ? App.Utils.formatDate(lastAbsent.date) : '—' };
    }).sort(function(a,b) { return a.pct - b.pct; });

    var tableRows = rows.map(function(r) {
      return '<tr style="border-bottom:1px solid #f4f4f2">'
        + '<td style="padding:0.65rem 1rem;font-size:0.83rem;font-weight:600">' + App.Utils.esc(r.name) + '</td>'
        + '<td style="padding:0.65rem 1rem">' + r.badge + '</td>'
        + '<td style="padding:0.65rem 1rem;font-size:0.83rem;color:#64748b">' + r.sessions + ' sessions</td>'
        + '<td style="padding:0.65rem 1rem;font-size:0.83rem;color:#94a3b8">' + r.lastAbsent + '</td>'
        + '</tr>';
    }).join('');

    return '<div style="display:grid;grid-template-columns:1fr 1fr;gap:1rem;margin-bottom:1.25rem">'
      + '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);padding:1.25rem"><h3 style="font-weight:600;font-size:0.9rem;color:#374151;margin:0 0 1rem">Attendance Rate by Student</h3><canvas id="chart-stu-attend" height="220"></canvas></div>'
      + '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);padding:1.25rem"><h3 style="font-weight:600;font-size:0.9rem;color:#374151;margin:0 0 0.5rem">At-Risk Students</h3><p style="font-size:0.78rem;color:#94a3b8;margin:0 0 1rem">Sorted by lowest attendance first</p>'
        + '<div style="overflow-y:auto;max-height:240px"><table style="width:100%"><thead><tr style="background:#f8fafc"><th style="text-align:left;padding:0.5rem 0.75rem;font-size:0.72rem;font-weight:600;color:#94a3b8">Student</th><th style="text-align:left;padding:0.5rem 0.75rem;font-size:0.72rem;font-weight:600;color:#94a3b8">Rate</th><th style="text-align:left;padding:0.5rem 0.75rem;font-size:0.72rem;font-weight:600;color:#94a3b8">Sessions</th><th style="text-align:left;padding:0.5rem 0.75rem;font-size:0.72rem;font-weight:600;color:#94a3b8">Last Absent</th></tr></thead><tbody>' + tableRows + '</tbody></table></div>'
      + '</div>'
      + '</div>';
  }

  function _buildStudentsChart(students, attendance) {
    var ctx = document.getElementById('chart-stu-attend');
    if (!ctx) return;
    var data = students.map(function(s) {
      var recs = attendance.filter(function(a) { return a.personId === s.id && a.personType === 'student'; });
      var present = recs.filter(function(a) { return a.status === 'Present' || a.status === 'Late'; }).length;
      return { name: s.firstName, pct: recs.length > 0 ? Math.round(present / recs.length * 100) : 0 };
    }).sort(function(a,b) { return b.pct - a.pct; });
    _charts.stuAttend = new Chart(ctx.getContext('2d'), {
      type: 'bar',
      data: {
        labels: data.map(function(d) { return d.name; }),
        datasets: [{ data: data.map(function(d) { return d.pct; }), backgroundColor: data.map(function(d) { return d.pct>=80?'#10b981':d.pct>=60?'#f59e0b':'#ef4444'; }), borderRadius: 6 }]
      },
      options: { responsive: true, plugins: { legend: { display: false } }, scales: { y: { beginAtZero: true, max: 100, ticks: { callback: function(v) { return v + '%'; } }, grid: { color: '#f1f5f9' } }, x: { grid: { display: false } } } }
    });
  }

  function _byTeachersHTML(staff, classes, attendance, students) {
    var teachers = staff.filter(function(s) { return s.role && s.role.toLowerCase().indexOf('teacher') > -1; });
    if (!teachers.length) teachers = staff; // fallback

    var rows = teachers.map(function(t) {
      var myClasses = classes.filter(function(c) { return c.teacherIds && c.teacherIds.indexOf(t.id) > -1; });
      var totalStudents = myClasses.reduce(function(acc, c) { return acc + (c.enrolled || 0); }, 0);
      var myClassIds = myClasses.map(function(c) { return c.id; });
      var attRecs = attendance.filter(function(a) { return a.personType === 'student' && myClassIds.indexOf(a.classId) > -1; });
      var present = attRecs.filter(function(a) { return a.status === 'Present' || a.status === 'Late'; }).length;
      var avgAtt = attRecs.length > 0 ? Math.round(present / attRecs.length * 100) : null;
      return { name: t.fullName || t.name, role: t.role, classes: myClasses.length, students: totalStudents, avgAtt: avgAtt };
    });

    var tableRows = rows.map(function(r) {
      var attBadge = r.avgAtt === null
        ? '<span style="color:#94a3b8;font-size:0.78rem">No data</span>'
        : '<span style="padding:0.2rem 0.55rem;border-radius:20px;font-size:0.75rem;font-weight:700;background:' + (r.avgAtt>=80?'#dcfce7':r.avgAtt>=60?'#fef3c7':'#fee2e2') + ';color:' + (r.avgAtt>=80?'#166534':r.avgAtt>=60?'#92400e':'#991b1b') + '">' + r.avgAtt + '%</span>';
      return '<tr style="border-bottom:1px solid #f4f4f2">'
        + '<td style="padding:0.7rem 1rem;font-size:0.83rem;font-weight:600">' + App.Utils.esc(r.name) + '</td>'
        + '<td style="padding:0.7rem 1rem;font-size:0.82rem;color:#64748b">' + App.Utils.esc(r.role) + '</td>'
        + '<td style="padding:0.7rem 1rem;font-size:0.83rem;text-align:center">' + r.classes + '</td>'
        + '<td style="padding:0.7rem 1rem;font-size:0.83rem;text-align:center">' + r.students + '</td>'
        + '<td style="padding:0.7rem 1rem;text-align:center">' + attBadge + '</td>'
        + '</tr>';
    }).join('');

    return '<div style="display:grid;grid-template-columns:1fr 1fr;gap:1rem;margin-bottom:1.25rem">'
      + '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);padding:1.25rem"><h3 style="font-weight:600;font-size:0.9rem;color:#374151;margin:0 0 1rem">Classes per Teacher</h3><canvas id="chart-tchr-classes" height="220"></canvas></div>'
      + '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);padding:1.25rem"><h3 style="font-weight:600;font-size:0.9rem;color:#374151;margin:0 0 1rem">Students per Teacher</h3><canvas id="chart-tchr-students" height="220"></canvas></div>'
      + '</div>'
      + '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);overflow:hidden">'
      + '<div style="padding:0.85rem 1rem;border-bottom:1px solid #f4f4f2"><span style="font-weight:700;font-size:0.85rem">Teacher Overview</span></div>'
      + '<table style="width:100%"><thead><tr style="background:#f8fafc;border-bottom:1px solid #f4f4f2">'
      + '<th style="text-align:left;padding:0.6rem 1rem;font-size:0.75rem;font-weight:600;color:#94a3b8">Name</th>'
      + '<th style="text-align:left;padding:0.6rem 1rem;font-size:0.75rem;font-weight:600;color:#94a3b8">Role</th>'
      + '<th style="text-align:center;padding:0.6rem 1rem;font-size:0.75rem;font-weight:600;color:#94a3b8">Classes</th>'
      + '<th style="text-align:center;padding:0.6rem 1rem;font-size:0.75rem;font-weight:600;color:#94a3b8">Students</th>'
      + '<th style="text-align:center;padding:0.6rem 1rem;font-size:0.75rem;font-weight:600;color:#94a3b8">Avg Attendance</th>'
      + '</tr></thead><tbody>' + tableRows + '</tbody></table>'
      + '</div>';
  }

  function _buildTeachersChart(staff, classes, attendance) {
    var teachers = staff.filter(function(s) { return s.role && s.role.toLowerCase().indexOf('teacher') > -1; });
    if (!teachers.length) teachers = staff;
    var names = teachers.map(function(t) { return (t.fullName || t.name).split(' ')[0]; });
    var classCounts = teachers.map(function(t) { return classes.filter(function(c) { return c.teacherIds && c.teacherIds.indexOf(t.id) > -1; }).length; });
    var stuCounts = teachers.map(function(t) {
      return classes.filter(function(c) { return c.teacherIds && c.teacherIds.indexOf(t.id) > -1; }).reduce(function(acc, c) { return acc + (c.enrolled || 0); }, 0);
    });
    var ctx1 = document.getElementById('chart-tchr-classes');
    if (ctx1) _charts.tchrClasses = new Chart(ctx1.getContext('2d'), { type: 'bar', data: { labels: names, datasets: [{ data: classCounts, backgroundColor: '#6366f1', borderRadius: 6 }] }, options: { responsive: true, plugins: { legend: { display: false } }, scales: { y: { beginAtZero: true, ticks: { stepSize: 1 }, grid: { color: '#f1f5f9' } }, x: { grid: { display: false } } } } });
    var ctx2 = document.getElementById('chart-tchr-students');
    if (ctx2) _charts.tchrStudents = new Chart(ctx2.getContext('2d'), { type: 'bar', data: { labels: names, datasets: [{ data: stuCounts, backgroundColor: '#0ea5e9', borderRadius: 6 }] }, options: { responsive: true, plugins: { legend: { display: false } }, scales: { y: { beginAtZero: true, ticks: { stepSize: 1 }, grid: { color: '#f1f5f9' } }, x: { grid: { display: false } } } } });
  }

  function _bySubjectHTML(classes, invoices) {
    var cats = ['Academic', 'Non-academic', 'Workshop'];
    var catData = cats.map(function(cat) {
      var catClasses = classes.filter(function(c) { return (c.category || 'Academic') === cat; });
      var enrolled = catClasses.reduce(function(acc, c) { return acc + (c.enrolled || 0); }, 0);
      var capacity = catClasses.reduce(function(acc, c) { return acc + (c.capacity || 0); }, 0);
      return { cat: cat, count: catClasses.length, enrolled: enrolled, capacity: capacity, fill: capacity > 0 ? Math.round(enrolled / capacity * 100) : 0 };
    });

    var classRows = classes.map(function(c) {
      var fill = c.capacity > 0 ? Math.round((c.enrolled || 0) / c.capacity * 100) : 0;
      var fillColor = fill >= 90 ? '#ef4444' : fill >= 70 ? '#f59e0b' : '#10b981';
      return '<tr style="border-bottom:1px solid #f4f4f2">'
        + '<td style="padding:0.65rem 1rem;font-size:0.83rem;font-weight:600">' + App.Utils.esc(c.name) + '</td>'
        + '<td style="padding:0.65rem 1rem;font-size:0.82rem;color:#64748b">' + App.Utils.esc(c.category || 'Academic') + '</td>'
        + '<td style="padding:0.65rem 1rem;font-size:0.83rem">' + (c.enrolled || 0) + ' / ' + c.capacity + '</td>'
        + '<td style="padding:0.65rem 1rem"><div style="display:flex;align-items:center;gap:0.5rem"><div style="flex:1;height:6px;background:#f1f5f9;border-radius:3px"><div style="height:6px;width:' + fill + '%;background:' + fillColor + ';border-radius:3px"></div></div><span style="font-size:0.75rem;font-weight:600;color:' + fillColor + '">' + fill + '%</span></div></td>'
        + '</tr>';
    }).join('');

    var summaryCards = catData.map(function(d) {
      var colors = { Academic: '#3b82f6', 'Non-academic': '#8b5cf6', Workshop: '#f59e0b' };
      var col = colors[d.cat] || '#64748b';
      return '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);padding:1rem 1.2rem">'
        + '<div style="display:flex;align-items:center;gap:0.5rem;margin-bottom:0.5rem"><div style="width:10px;height:10px;border-radius:2px;background:' + col + '"></div><span style="font-size:0.8rem;font-weight:700;color:#374151">' + d.cat + '</span></div>'
        + '<div style="font-size:1.6rem;font-weight:800;color:#0f172a">' + d.enrolled + '</div>'
        + '<div style="font-size:0.75rem;color:#94a3b8;margin-top:0.2rem">' + d.count + ' class' + (d.count!==1?'es':'') + ' · ' + d.fill + '% full</div>'
        + '</div>';
    }).join('');

    return '<div style="display:grid;grid-template-columns:repeat(3,1fr);gap:0.75rem;margin-bottom:1.25rem">' + summaryCards + '</div>'
      + '<div style="display:grid;grid-template-columns:1fr 2fr;gap:1rem;margin-bottom:1.25rem">'
      + '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);padding:1.25rem"><h3 style="font-weight:600;font-size:0.9rem;color:#374151;margin:0 0 1rem">Enrollment by Category</h3><canvas id="chart-subj-pie" height="220"></canvas></div>'
      + '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);overflow:hidden"><div style="padding:0.85rem 1rem;border-bottom:1px solid #f4f4f2"><span style="font-weight:700;font-size:0.85rem">All Classes</span></div><table style="width:100%"><thead><tr style="background:#f8fafc"><th style="text-align:left;padding:0.6rem 1rem;font-size:0.72rem;font-weight:600;color:#94a3b8">Class</th><th style="text-align:left;padding:0.6rem 1rem;font-size:0.72rem;font-weight:600;color:#94a3b8">Category</th><th style="text-align:left;padding:0.6rem 1rem;font-size:0.72rem;font-weight:600;color:#94a3b8">Enrolled</th><th style="text-align:left;padding:0.6rem 1rem;font-size:0.72rem;font-weight:600;color:#94a3b8">Fill Rate</th></tr></thead><tbody>' + classRows + '</tbody></table></div>'
      + '</div>';
  }

  function _buildSubjectChart(classes) {
    var ctx = document.getElementById('chart-subj-pie');
    if (!ctx) return;
    var cats = ['Academic', 'Non-academic', 'Workshop'];
    var data = cats.map(function(cat) {
      return classes.filter(function(c) { return (c.category || 'Academic') === cat; }).reduce(function(acc, c) { return acc + (c.enrolled || 0); }, 0);
    });
    _charts.subjPie = new Chart(ctx.getContext('2d'), {
      type: 'doughnut',
      data: { labels: cats, datasets: [{ data: data, backgroundColor: ['#3b82f6', '#8b5cf6', '#f59e0b'], borderWidth: 0 }] },
      options: { responsive: true, plugins: { legend: { position: 'bottom' } }, cutout: '60%' }
    });
  }

  function _financialHTML(invoices, students) {
    var total     = invoices.reduce(function(s,i){ return s + i.amount; }, 0);
    var collected = invoices.filter(function(i){ return i.status === 'Paid'; }).reduce(function(s,i){ return s + i.amount; }, 0);
    var outstanding = invoices.filter(function(i){ return i.status !== 'Paid'; }).reduce(function(s,i){ return s + i.amount; }, 0);
    var rate = total > 0 ? Math.round(collected / total * 100) : 0;
    var today = new Date();

    function statCard(label, value, color) {
      return '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);padding:1.1rem 1.2rem;box-shadow:0 1px 2px rgba(0,0,0,0.04)">'
        + '<p style="font-size:0.68rem;font-weight:700;text-transform:uppercase;letter-spacing:0.08em;color:#94a3b8;margin:0 0 0.4rem">' + label + '</p>'
        + '<p style="font-size:1.7rem;font-weight:800;color:' + color + ';margin:0">' + value + '</p>'
        + '</div>';
    }

    var overdueInvs = invoices.filter(function(i){ return i.status === 'Overdue'; });
    var overdueRows = overdueInvs.length === 0
      ? '<tr><td colspan="4" style="text-align:center;padding:1.5rem;color:#94a3b8;font-size:0.83rem">No overdue invoices</td></tr>'
      : overdueInvs.map(function(inv) {
          var stu = students.find(function(s){ return s.id === inv.studentId; });
          var daysOverdue = Math.floor((today - new Date(inv.dueDate)) / 86400000);
          return '<tr style="border-bottom:1px solid #f4f4f2">'
            + '<td style="padding:0.65rem 1rem;font-size:0.83rem;font-weight:600">' + (stu ? stu.firstName + ' ' + stu.lastName : inv.studentId) + '</td>'
            + '<td style="padding:0.65rem 1rem;font-size:0.83rem;color:#dc2626;font-weight:700">' + App.Utils.formatCurrency(inv.amount) + '</td>'
            + '<td style="padding:0.65rem 1rem;font-size:0.83rem;color:#94a3b8">' + App.Utils.formatDate(inv.dueDate) + '</td>'
            + '<td style="padding:0.65rem 1rem"><span style="padding:0.2rem 0.6rem;background:#fef2f2;color:#dc2626;border-radius:20px;font-size:0.72rem;font-weight:700">' + daysOverdue + 'd overdue</span></td>'
            + '</tr>';
        }).join('');

    return '<div style="display:grid;grid-template-columns:repeat(4,1fr);gap:0.75rem;margin-bottom:1.25rem">'
      + statCard('Total Revenue', App.Utils.formatCurrency(total), '#374151')
      + statCard('Collected', App.Utils.formatCurrency(collected), '#15803d')
      + statCard('Outstanding', App.Utils.formatCurrency(outstanding), outstanding > 0 ? '#dc2626' : '#15803d')
      + statCard('Collection Rate', rate + '%', rate >= 80 ? '#15803d' : rate >= 60 ? '#d97706' : '#dc2626')
      + '</div>'
      + '<div style="display:grid;grid-template-columns:3fr 2fr;gap:1rem;margin-bottom:1.25rem">'
      + '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);padding:1.25rem"><h3 style="font-weight:600;font-size:0.9rem;color:#374151;margin:0 0 1rem">Monthly Collection</h3><canvas id="chart-fin-monthly" height="200"></canvas></div>'
      + '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);padding:1.25rem"><h3 style="font-weight:600;font-size:0.9rem;color:#374151;margin:0 0 1rem">Payment Status</h3><canvas id="chart-fin-pie" height="200"></canvas></div>'
      + '</div>'
      + '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);overflow:hidden">'
      + '<div style="padding:0.85rem 1rem;border-bottom:1px solid #f4f4f2"><span style="font-weight:700;font-size:0.85rem">Overdue Invoices (' + overdueInvs.length + ')</span></div>'
      + '<table style="width:100%"><thead><tr style="background:#f8fafc;border-bottom:1px solid #f4f4f2">'
      + '<th style="text-align:left;padding:0.6rem 1rem;font-size:0.75rem;font-weight:600;color:#94a3b8">Student</th>'
      + '<th style="text-align:left;padding:0.6rem 1rem;font-size:0.75rem;font-weight:600;color:#94a3b8">Amount</th>'
      + '<th style="text-align:left;padding:0.6rem 1rem;font-size:0.75rem;font-weight:600;color:#94a3b8">Due Date</th>'
      + '<th style="text-align:left;padding:0.6rem 1rem;font-size:0.75rem;font-weight:600;color:#94a3b8">Status</th>'
      + '</tr></thead><tbody>' + overdueRows + '</tbody></table>'
      + '</div>';
  }

  function _buildFinancialCharts(invoices) {
    var m = _months(_filterMonths);
    var paid = m.keys.map(function(k){ return invoices.filter(function(i){ return i.status==='Paid' && i.paidOn && i.paidOn.slice(0,7)===k; }).reduce(function(s,i){ return s+i.amount; },0); });
    var unpaid = m.keys.map(function(k){ return invoices.filter(function(i){ return i.status!=='Paid' && i.createdOn && i.createdOn.slice(0,7)===k; }).reduce(function(s,i){ return s+i.amount; },0); });

    var monthly = document.getElementById('chart-fin-monthly');
    if (monthly) {
      _charts.finMonthly = new Chart(monthly.getContext('2d'), {
        type: 'bar',
        data: { labels: m.labels, datasets: [
          { label: 'Collected', data: paid, backgroundColor: '#10b981', borderRadius: 4 },
          { label: 'Unpaid', data: unpaid, backgroundColor: '#fbbf24', borderRadius: 4 }
        ]},
        options: { responsive: true, plugins: { legend: { position: 'top' } }, scales: { x: { grid: { display: false } }, y: { ticks: { callback: function(v){ return 'RM'+v; } }, grid: { color: '#f1f5f9' } } } }
      });
    }

    var paidCount   = invoices.filter(function(i){ return i.status==='Paid'; }).length;
    var unpaidCount = invoices.filter(function(i){ return i.status==='Unpaid'; }).length;
    var overdueCount= invoices.filter(function(i){ return i.status==='Overdue'; }).length;
    var pie = document.getElementById('chart-fin-pie');
    if (pie) {
      _charts.finPie = new Chart(pie.getContext('2d'), {
        type: 'doughnut',
        data: { labels: ['Paid','Unpaid','Overdue'], datasets: [{ data: [paidCount, unpaidCount, overdueCount], backgroundColor: ['#10b981','#fbbf24','#ef4444'], borderWidth: 0 }] },
        options: { responsive: true, plugins: { legend: { position: 'bottom' } }, cutout: '65%' }
      });
    }
  }

  function _setView(v)     { _filterView = v; Object.keys(_charts).forEach(function(k){if(_charts[k]){_charts[k].destroy();delete _charts[k];}}); App.Router.refresh(); }
  function _setStudent(v)  { _filterStudent = v; App.Router.refresh(); }
  function _setTeacher(v)  { _filterTeacher = v; App.Router.refresh(); }
  function _setCategory(v) { _filterCategory = v; App.Router.refresh(); }
  function _setMonths(v)   { _filterMonths = parseInt(v) || 6; App.Router.refresh(); }
  function _clearFilters() { _filterStudent = ''; _filterTeacher = ''; _filterCategory = ''; _filterMonths = 6; App.Router.refresh(); }

  App.Analytics = {
    render: render,
    _setView: _setView,
    _setStudent: _setStudent,
    _setTeacher: _setTeacher,
    _setCategory: _setCategory,
    _setMonths: _setMonths,
    _clearFilters: _clearFilters
  };
})();
