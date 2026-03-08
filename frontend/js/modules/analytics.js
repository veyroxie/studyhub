(function() {
  window.App = window.App || {};

  let _filterStudent = '';  // '' = all students
  let _filterTeacher = '';  // '' = all staff
  let _filterCategory = ''; // '' = all, or 'Academic', 'Non-academic', 'Workshop'
  let _filterMonths = 6;    // number of months to show
  let _filterView = 'overview'; // 'overview' or 'financial'

  let _charts = {};

  function render(container) {
    const { students, staff, classes, invoices, attendance } = App.Store.get();

    const viewToggle = '<div style="display:flex;gap:0.25rem;background:#f1f5f9;border-radius:8px;padding:3px;margin-bottom:1rem;width:fit-content">'
      + '<button onclick="App.Analytics._setView(\'overview\')" style="padding:0.3rem 1rem;font-size:0.75rem;font-weight:600;border:none;border-radius:6px;cursor:pointer;background:' + (_filterView==='overview'?'var(--gold, #f59e0b)':'transparent') + ';color:' + (_filterView==='overview'?'#0a0a0a':'#94a3b8') + '">Overview</button>'
      + '<button onclick="App.Analytics._setView(\'financial\')" style="padding:0.3rem 1rem;font-size:0.75rem;font-weight:600;border:none;border-radius:6px;cursor:pointer;background:' + (_filterView==='financial'?'var(--gold, #f59e0b)':'transparent') + ';color:' + (_filterView==='financial'?'#0a0a0a':'#94a3b8') + '">Financial</button>'
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
    if (_filterCategory) filteredClasses = filteredClasses.filter(function(c) { return c.category === _filterCategory; });
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

    container.innerHTML = filterBar
      + '<div class="flex items-center justify-between mb-6">'
      +   '<h1 class="text-2xl font-bold text-slate-800">Analytics</h1>'
      +   '<span class="text-sm text-slate-400">Last ' + _filterMonths + ' months · as of March 2026</span>'
      + '</div>'
      + '<div class="grid grid-cols-2 gap-5">'
      + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm p-5"><h3 class="font-semibold text-slate-700 mb-4">Enrollment Trend</h3><canvas id="chart-enrollment" height="200"></canvas></div>'
      + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm p-5"><h3 class="font-semibold text-slate-700 mb-4">Class Fill Rate</h3><canvas id="chart-fillrate" height="200"></canvas></div>'
      + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm p-5"><h3 class="font-semibold text-slate-700 mb-4">Revenue Collection</h3><canvas id="chart-revenue" height="200"></canvas></div>'
      + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm p-5"><h3 class="font-semibold text-slate-700 mb-4">Student Attendance Rate</h3><canvas id="chart-attendance" height="200"></canvas></div>'
      + '</div>';

    // Destroy old charts to avoid canvas reuse error
    Object.keys(_charts).forEach(function(k) { if (_charts[k]) { _charts[k].destroy(); delete _charts[k]; } });

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

  function _setStudent(v)  { _filterStudent = v; App.Router.refresh(); }
  function _setTeacher(v)  { _filterTeacher = v; App.Router.refresh(); }
  function _setCategory(v) { _filterCategory = v; App.Router.refresh(); }
  function _setMonths(v)   { _filterMonths = parseInt(v) || 6; App.Router.refresh(); }
  function _clearFilters() { _filterStudent = ''; _filterTeacher = ''; _filterCategory = ''; _filterMonths = 6; App.Router.refresh(); }

  App.Analytics = {
    render: render,
    _setStudent: _setStudent,
    _setTeacher: _setTeacher,
    _setCategory: _setCategory,
    _setMonths: _setMonths,
    _clearFilters: _clearFilters
  };
})();
