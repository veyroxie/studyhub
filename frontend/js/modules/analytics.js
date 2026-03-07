(function() {
  window.App = window.App || {};

  let _charts = {};

  function render(container) {
    const { students, classes, invoices, attendance } = App.Store.get();

    container.innerHTML = ''
      + '<div class="flex items-center justify-between mb-6">'
      +   '<h1 class="text-2xl font-bold text-slate-800">Analytics</h1>'
      +   '<span class="text-sm text-slate-400">Last 6 months · as of March 2026</span>'
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
      _buildEnrollmentChart(students);
      _buildFillRateChart(classes);
      _buildRevenueChart(invoices);
      _buildAttendanceChart(students, attendance);
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
    const m = _months(6);
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
    const m = _months(6);
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

  App.Analytics = { render: render };
})();
