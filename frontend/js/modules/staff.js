(function() {
  window.App = window.App || {};

  function render(container) {
    const { staff, classes, payroll } = App.Store.get();
    const isAdmin = App.currentRole === 'admin';

    const { registrations } = App.Store.get();
    const pendingTeachers = isAdmin ? (registrations || []).filter(function(r) { return r.status === 'pending' && r.type === 'teacher'; }) : [];

    container.innerHTML = ''
      + '<div class="flex items-center justify-between mb-6">'
      +   '<h1 class="text-2xl font-bold text-slate-800">Staff</h1>'
      +   '<div class="flex gap-2">'
      +   (isAdmin && pendingTeachers.length > 0
          ? '<button onclick="App.Staff._pendingTeacherModal()" class="px-4 py-2 text-sm bg-amber-500 text-white rounded-lg hover:bg-amber-600 flex items-center gap-2"><span class="w-5 h-5 bg-white text-amber-600 text-xs font-bold rounded-full flex items-center justify-center">' + pendingTeachers.length + '</span>Applications</button>'
          : '')
      +   (isAdmin ? '<button onclick="App.Staff._addModal()" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">+ Add Staff</button>' : '')
      +   '</div>'
      + '</div>'
      + (staff.length === 0
        ? '<div class="bg-white rounded-xl border border-slate-100 shadow-sm">' + App.Utils.emptyState(
            'No staff yet',
            'Add your first staff member to get started.',
            isAdmin ? '<button onclick="App.Staff._addModal()" style="padding:0.5rem 1.25rem;font-size:0.83rem;font-weight:600;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">+ Add Staff</button>' : ''
          ) + '</div>'
        : '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);overflow:hidden">'
          + staff.map(function(s, idx) { return _staffCard(s, idx, classes, isAdmin); }).join('')
          + '</div>');
  }

  function _staffCard(s, idx, classes, isAdmin) {
    const teachingClasses = classes.filter(function(c) { return c.teacherIds.indexOf(s.id) > -1; });
    const avatarBgs   = ['#dbeafe','#ede9fe','#d1fae5','#fef3c7'];
    const avatarCols  = ['#1d4ed8','#7c3aed','#047857','#b45309'];
    const colorIdx    = idx % 4;
    const avatarBg    = avatarBgs[colorIdx];
    const avatarCol   = avatarCols[colorIdx];
    const initial     = (s.name || s.fullName || '?').charAt(0);

    var metricStyle = 'font-size:0.78rem;color:#94a3b8';
    var metricVal   = 'font-weight:600;color:#111;font-size:0.78rem';
    function metric(label, value) {
      return '<div style="display:flex;flex-direction:column;align-items:center;gap:1px;min-width:52px">'
        + '<span style="' + metricVal + '">' + value + '</span>'
        + '<span style="' + metricStyle + '">' + label + '</span>'
        + '</div>';
    }

    return '<div style="display:flex;align-items:center;gap:1rem;padding:0.9rem 1.1rem;border-bottom:1px solid #f8f6f4;cursor:pointer" onclick="App.Staff._viewModal(\'' + s.id + '\')" onmouseover="this.style.background=\'#fafaf8\'" onmouseout="this.style.background=\'transparent\'">'
      + '<div style="width:40px;height:40px;border-radius:50%;background:' + avatarBg + ';color:' + avatarCol + ';font-weight:700;font-size:1rem;display:flex;align-items:center;justify-content:center;flex-shrink:0">' + initial + '</div>'
      + '<div style="flex:1 1 0;min-width:0">'
      +   '<div style="font-weight:700;color:#111;font-size:0.9rem;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + App.Utils.esc(s.fullName) + '</div>'
      +   '<div style="font-size:0.75rem;color:#94a3b8;margin-top:1px">' + App.Utils.esc(s.role) + '</div>'
      + '</div>'
      + '<div style="display:flex;align-items:center;gap:1.25rem;flex-shrink:0">'
      +   metric('Classes', teachingClasses.length)
      +   (isAdmin ? metric('Salary', App.Utils.formatCurrency(s.salary) + '/mo') : '')
      +   metric('Since', App.Utils.formatDate(s.joinDate))
      +   App.Utils.statusBadge(s.status)
      +   '<button onclick="event.stopPropagation();App.Staff._viewModal(\'' + s.id + '\')" style="padding:0.35rem 0.9rem;font-size:0.78rem;font-weight:600;background:#f1f5f9;color:#374151;border:1px solid #e2e8f0;border-radius:7px;cursor:pointer">View</button>'
      + '</div>'
      + '</div>';
  }

  function _perfTab(s, classes, students, attendance, feedback) {
    var myClasses  = classes.filter(function(c) { return c.teacherIds.indexOf(s.id) > -1; });
    var myClassIds = myClasses.map(function(c) { return c.id; });

    // Fill rate
    var avgFill = myClasses.length > 0
      ? Math.round(myClasses.reduce(function(acc,c){ return acc + (c.enrolled/c.capacity); },0) / myClasses.length * 100)
      : 0;

    // Unique students
    var myStudentIds = {};
    myClasses.forEach(function(c) {
      students.forEach(function(stu) {
        if (stu.enrolledClasses.indexOf(c.id) > -1) myStudentIds[stu.id] = true;
      });
    });
    var stuCount = Object.keys(myStudentIds).length;

    // Attendance rate (student records only)
    var myAttRecs = attendance.filter(function(a) {
      return a.personType === 'student' && myClassIds.indexOf(a.classId) > -1;
    });
    var attRate = myAttRecs.length > 0
      ? Math.round(myAttRecs.filter(function(a){ return a.status === 'Present'; }).length / myAttRecs.length * 100)
      : null;

    // Feedback
    var myFeedback = (feedback || []).filter(function(f) { return myClassIds.indexOf(f.classId) > -1; });
    var moodScore = { 'Great': 5, 'Good': 3, 'Needs Work': 1 };
    var avgRating = myFeedback.length > 0
      ? (myFeedback.reduce(function(acc,f){ return acc + (moodScore[f.mood] || 0); },0) / myFeedback.length).toFixed(1)
      : null;

    // Metric cards
    function metricCard(label, value, color) {
      return '<div style="background:#f8fafc;border-radius:10px;padding:0.85rem 1rem;text-align:center">'
        + '<div style="font-size:1.4rem;font-weight:800;color:' + color + '">' + value + '</div>'
        + '<div style="font-size:0.68rem;color:#94a3b8;font-weight:600;text-transform:uppercase;letter-spacing:0.05em;margin-top:3px">' + label + '</div>'
        + '</div>';
    }

    // Performance reviews
    var reviews = (App.Store.get().performanceReviews || []).filter(function(r) { return r.staffId === s.id; })
      .sort(function(a, b) { return b.date.localeCompare(a.date); });

    function stars(n) {
      var out = '';
      for (var i = 1; i <= 5; i++) out += '<span style="color:' + (i <= n ? '#d97706' : '#d1d5db') + '">&#9733;</span>';
      return out;
    }

    var reviewsHtml = '<div style="margin-top:1rem">'
      + '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.6rem">'
      +   '<span style="font-size:0.82rem;font-weight:700;color:#374151">Performance Reviews</span>'
      +   '<button onclick="App.Staff._addReviewModal(\'' + s.id + '\')" style="padding:0.28rem 0.75rem;font-size:0.75rem;font-weight:600;background:var(--gold);color:#0a0a0a;border:none;border-radius:7px;cursor:pointer">+ Add Review</button>'
      + '</div>'
      + (reviews.length === 0
          ? '<div style="text-align:center;padding:1.25rem 0;font-size:0.82rem;color:#94a3b8">No reviews yet</div>'
          : reviews.map(function(rv) {
              return '<div style="background:#f8fafc;border-radius:10px;padding:0.85rem 1rem;margin-bottom:0.6rem">'
                + '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.35rem">'
                +   '<span style="font-size:0.78rem;font-weight:700;color:#374151">' + App.Utils.esc(rv.period) + '</span>'
                +   '<div style="display:flex;align-items:center;gap:0.5rem">'
                +     '<span style="font-size:1rem;letter-spacing:-1px">' + stars(rv.rating) + '</span>'
                +     '<span style="font-size:0.7rem;color:#94a3b8">' + App.Utils.formatDate(rv.date) + '</span>'
                +   '</div>'
                + '</div>'
                + (rv.strengths ? '<div style="font-size:0.76rem;color:#374151;margin-bottom:0.25rem"><span style="color:#94a3b8;font-weight:600">Strengths: </span>' + App.Utils.esc(rv.strengths) + '</div>' : '')
                + (rv.areasToImprove ? '<div style="font-size:0.76rem;color:#374151;margin-bottom:0.25rem"><span style="color:#94a3b8;font-weight:600">Areas to Improve: </span>' + App.Utils.esc(rv.areasToImprove) + '</div>' : '')
                + '<div style="font-size:0.7rem;color:#94a3b8;margin-top:0.2rem">Reviewed by ' + App.Utils.esc(rv.reviewedBy) + '</div>'
                + '</div>';
            }).join('')
        )
      + '</div>';

    return '<div class="space-y-4">'
      + '<div style="display:grid;grid-template-columns:repeat(2,1fr);gap:0.75rem">'
      + metricCard('Classes', myClasses.length, '#6366f1')
      + metricCard('Students', stuCount, '#0891b2')
      + metricCard('Fill Rate', avgFill + '%', avgFill >= 80 ? '#22c55e' : avgFill >= 50 ? '#f59e0b' : '#ef4444')
      + metricCard('Attend. Rate', attRate !== null ? attRate + '%' : '—', attRate !== null && attRate >= 80 ? '#22c55e' : '#94a3b8')
      + '</div>'
      + (avgRating !== null
          ? '<div style="background:#fafaf8;border-radius:10px;padding:0.85rem 1rem;display:flex;align-items:center;justify-content:space-between">'
          +   '<span style="font-size:0.82rem;font-weight:600;color:#374151">Parent Feedback</span>'
          +   '<span style="font-size:0.95rem;font-weight:800;color:#d97706">' + avgRating + '/5</span>'
          +   '<span style="font-size:0.72rem;color:#94a3b8">(' + myFeedback.length + ' reviews)</span>'
          + '</div>'
          : '')
      + '<div>'
      +   '<label style="display:block;font-size:0.8rem;font-weight:600;color:#374151;margin-bottom:0.4rem">Internal Notes</label>'
      +   '<textarea id="perf-notes-' + s.id + '" rows="3" style="width:100%;padding:0.5rem 0.75rem;font-size:0.82rem;border:1px solid #e2e8f0;border-radius:8px;resize:vertical;outline:none">' + App.Utils.esc(s.performanceNotes || '') + '</textarea>'
      +   '<button onclick="App.Staff._saveNotes(\'' + s.id + '\')" style="margin-top:0.5rem;padding:0.35rem 0.9rem;font-size:0.78rem;font-weight:600;background:var(--gold);color:#0a0a0a;border:none;border-radius:7px;cursor:pointer">Save Notes</button>'
      + '</div>'
      + reviewsHtml
      + '</div>';
  }

  function _addReviewModal(staffId) {
    App.Utils.showModal(
      '<div class="p-6" style="min-width:380px">'
      + '<h2 class="text-lg font-bold mb-4">Add Performance Review</h2>'
      + '<form id="add-review-form" class="space-y-4">'
      + _field('Period (e.g. H1 2026)', '<input name="period" class="form-input" placeholder="H1 2026" required>')
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Rating</label>'
      + '<select name="rating" class="form-input">'
      + ['1 — Needs Improvement','2 — Below Expectations','3 — Meets Expectations','4 — Exceeds Expectations','5 — Outstanding'].map(function(opt, i) {
          return '<option value="' + (i+1) + '"' + (i === 2 ? ' selected' : '') + '>' + opt + '</option>';
        }).join('')
      + '</select></div>'
      + _field('Strengths', '<textarea name="strengths" rows="2" class="form-input" placeholder="What did this staff member do well?"></textarea>')
      + _field('Areas to Improve', '<textarea name="areasToImprove" rows="2" class="form-input" placeholder="What can be improved?"></textarea>')
      + _field('Reviewed By', '<input name="reviewedBy" class="form-input" value="Admin">')
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" style="padding:0.5rem 1rem;font-size:0.85rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Save Review</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );
    document.getElementById('add-review-form').addEventListener('submit', function(e) {
      e.preventDefault();
      var fd = new FormData(e.target);
      var newReview = {
        id: App.Utils.generateId(),
        staffId: staffId,
        date: App.Utils.today(),
        period: fd.get('period'),
        rating: parseInt(fd.get('rating'), 10),
        strengths: fd.get('strengths'),
        areasToImprove: fd.get('areasToImprove'),
        reviewedBy: fd.get('reviewedBy') || 'Admin'
      };
      App.Utils.hideModal(true);
      App.Api.post('/api/performance-reviews', newReview).then(function(result) {
        var existing = App.Store.get().performanceReviews || [];
        App.Store.set({ performanceReviews: existing.concat([result || newReview]) });
        App.Utils.showToast('Review saved', 'success');
        setTimeout(function() { App.Staff._viewModal(staffId); App.Staff._switchTab('performance'); }, 60);
      }).catch(function(err) {
        var existing = App.Store.get().performanceReviews || [];
        App.Store.set({ performanceReviews: existing.concat([newReview]) });
        App.Utils.showToast('Saved locally (offline)', 'warning');
        setTimeout(function() { App.Staff._viewModal(staffId); App.Staff._switchTab('performance'); }, 60);
      });
    });
  }

  function _saveNotes(staffId) {
    var textarea = document.getElementById('perf-notes-' + staffId);
    if (!textarea) return;
    var notes = textarea.value;
    App.Api.put('/api/staff/' + staffId, { performanceNotes: notes }).then(function() {
      var st = App.Store.get();
      App.Store.set({ staff: st.staff.map(function(x) {
        return x.id === staffId ? Object.assign({}, x, { performanceNotes: notes }) : x;
      })});
      App.Utils.showToast('Notes saved', 'success');
    }).catch(function() {
      var st = App.Store.get();
      App.Store.set({ staff: st.staff.map(function(x) {
        return x.id === staffId ? Object.assign({}, x, { performanceNotes: notes }) : x;
      })});
      App.Utils.showToast('Saved locally (offline)', 'warning');
    });
  }

  function _viewModal(staffId) {
    const { staff, classes, payroll, students, attendance } = App.Store.get();
    const isAdmin = App.currentRole === 'admin';
    const s = staff.find(function(x) { return x.id === staffId; });
    if (!s) return;

    const teachingClasses = classes.filter(function(c) { return c.teacherIds.indexOf(s.id) > -1; });
    const staffPayroll = payroll.filter(function(p) { return p.staffId === staffId; })
      .sort(function(a, b) { return b.month.localeCompare(a.month); });

    App.Utils.showModal(
      '<div class="p-6">'
      + '<div class="flex items-center gap-4 mb-6">'
      +   '<div class="w-16 h-16 rounded-2xl bg-blue-100 text-blue-700 font-bold text-2xl flex items-center justify-center">' + (s.name || s.fullName || '?').charAt(0) + '</div>'
      +   '<div>'
      +     '<h2 class="text-xl font-bold text-slate-800">' + App.Utils.esc(s.fullName) + '</h2>'
      +     '<div class="flex items-center gap-2 mt-1">' + App.Utils.badge(s.role, 'blue') + App.Utils.statusBadge(s.status) + '</div>'
      +   '</div>'
      + '</div>'

      + '<div class="flex border-b border-slate-100 mb-4 gap-1">'
      + (isAdmin ? ['Info','Schedule','Payroll','Performance'] : ['Info','Schedule']).map(function(tab, i) {
          return '<button onclick="App.Staff._switchTab(\'' + tab.toLowerCase() + '\')" id="stab-' + tab.toLowerCase() + '" class="tab-btn px-4 py-2 text-sm font-medium ' + (i===0?'border-b-2 border-blue-600 text-blue-600':'text-slate-500 hover:text-slate-700') + '">' + tab + '</button>';
        }).join('')
      + '</div>'

      + '<div id="stab-panel-info">'
      +   '<div class="grid grid-cols-2 gap-3 text-sm">'
      +   _infoRow('Email', App.Utils.esc(s.email))
      +   _infoRow('Phone', App.Utils.esc(s.phone))
      +   _infoRow('Role', App.Utils.esc(s.role))
      +   _infoRow('Joined', App.Utils.formatDate(s.joinDate))
      +   (s.specialization ? _infoRow('Specialization', App.Utils.esc(s.specialization)) : '')
      +   (isAdmin && s.nric ? _infoRow('IC / NRIC', App.Utils.esc(s.nric)) : '')
      +   (isAdmin && s.emergencyName ? _infoRow('Emergency Contact', App.Utils.esc(s.emergencyName) + (s.emergencyPhone ? ' · ' + App.Utils.esc(s.emergencyPhone) : '')) : '')
      +   (isAdmin ? _infoRow('Monthly Salary', App.Utils.formatCurrency(s.salary)) : '')
      +   '</div>'
      + '</div>'

      + '<div id="stab-panel-schedule" class="hidden">'
      + (teachingClasses.length === 0 ? '<p class="text-sm text-slate-400 text-center py-6">No classes assigned</p>'
        : '<table class="w-full text-sm"><thead><tr class="border-b"><th class="text-left py-2 text-slate-500 font-medium">Class</th><th class="text-left py-2 text-slate-500 font-medium">Schedule</th><th class="text-left py-2 text-slate-500 font-medium">Room</th><th class="text-right py-2 text-slate-500 font-medium">Enrolled</th></tr></thead><tbody>'
        + teachingClasses.map(function(c) {
            return '<tr class="border-b border-slate-50"><td class="py-2 font-medium">' + App.Utils.esc(c.name) + '</td><td class="py-2 text-slate-600">' + c.day + ' ' + App.Utils.formatTime(c.time) + '</td><td class="py-2 text-slate-500">' + App.Utils.esc(c.classroom) + '</td><td class="py-2 text-right">' + c.enrolled + '/' + c.capacity + '</td></tr>';
          }).join('')
        + '</tbody></table>')
      + '</div>'

      + (isAdmin ? '<div id="stab-panel-payroll" class="hidden">'
        + '<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:0.75rem">'
        +   '<span style="font-size:0.78rem;color:#94a3b8">'
        +     (s.employmentType === 'parttime' ? 'Part-time · RM ' + (s.hourlyRate || 0) + '/hr' : 'Full-time · RM ' + App.Utils.formatCurrency(s.salary) + '/mo')
        +   '</span>'
        +   '<button onclick="App.Staff._genPayrollModal(\'' + staffId + '\')" style="padding:0.3rem 0.8rem;font-size:0.75rem;font-weight:600;background:var(--gold);color:#0a0a0a;border:none;border-radius:7px;cursor:pointer">+ Generate Payroll</button>'
        + '</div>'
        + (staffPayroll.length === 0 ? '<p class="text-sm text-slate-400 text-center py-6">No payroll records</p>'
          : '<table class="w-full text-sm"><thead><tr class="border-b">'
          + '<th class="text-left py-2 text-slate-500 font-medium">Month</th>'
          + (s.employmentType === 'parttime' ? '<th class="text-right py-2 text-slate-500 font-medium">Hours</th><th class="text-right py-2 text-slate-500 font-medium">Rate/hr</th>' : '<th class="text-right py-2 text-slate-500 font-medium">Base</th>')
          + '<th class="text-right py-2 text-slate-500 font-medium">Bonus</th>'
          + '<th class="text-right py-2 text-slate-500 font-medium">Total</th>'
          + '<th class="text-right py-2 text-slate-500 font-medium">Status</th>'
          + '</tr></thead><tbody>'
          + staffPayroll.map(function(p) {
              return '<tr class="border-b border-slate-50">'
                + '<td class="py-2 font-medium">' + App.Utils.formatMonth(p.month) + '</td>'
                + (s.employmentType === 'parttime'
                    ? '<td class="py-2 text-right text-slate-600">' + (p.hoursWorked || 0) + 'h</td>'
                    + '<td class="py-2 text-right text-slate-600">' + App.Utils.formatCurrency(p.hourlyRate || s.hourlyRate || 0) + '</td>'
                    : '<td class="py-2 text-right text-slate-600">' + App.Utils.formatCurrency(p.baseSalary) + '</td>')
                + '<td class="py-2 text-right text-slate-600">' + (p.bonus ? App.Utils.formatCurrency(p.bonus) : '—') + '</td>'
                + '<td class="py-2 text-right font-semibold">' + App.Utils.formatCurrency(p.total) + '</td>'
                + '<td class="py-2 text-right">' + App.Utils.statusBadge(p.status) + '</td>'
                + '</tr>';
            }).join('')
          + '</tbody></table>')
        + '</div>' : '')

      + (isAdmin ? '<div id="stab-panel-performance" class="hidden">'
          + _perfTab(s, classes, students, attendance, App.Store.get().feedback || [])
          + '</div>' : '')

      + '<div class="mt-4 flex justify-between">'
      + (isAdmin ? '<button onclick="App.Utils.hideModal(true); setTimeout(function(){App.Staff._editModal(\'' + staffId + '\')},50)" class="px-4 py-2 text-sm bg-slate-700 text-white rounded-lg hover:bg-slate-800">Edit Details</button>' : '<div></div>')
      + '<button onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Close</button>'
      + '</div>'
      + '</div>'
    );
  }

  function _switchTab(tab) {
    ['info','schedule','payroll','performance'].forEach(function(t) {
      const panel = document.getElementById('stab-panel-' + t);
      if (panel) panel.classList.toggle('hidden', t !== tab);
      const btn = document.getElementById('stab-' + t);
      if (!btn) return;
      if (t === tab) { btn.classList.add('border-b-2','border-blue-600','text-blue-600'); btn.classList.remove('text-slate-500'); }
      else { btn.classList.remove('border-b-2','border-blue-600','text-blue-600'); btn.classList.add('text-slate-500'); }
    });
  }

  function _addModal() {
    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-4">Add Staff Member</h2>'
      + '<form id="add-staff-form" class="space-y-4">'
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Full Name (e.g. Teacher Rose)', '<input name="fullName" class="form-input" required>')
      + _field('Display Name', '<input name="name" class="form-input" placeholder="Rose" required>')
      + '</div>'
      + '<div class="grid grid-cols-2 gap-4">'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Role</label><select name="role" class="form-input"><option>Teacher</option><option>Senior Teacher</option><option>Admin</option></select></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Employment</label>'
      + '<select name="employmentType" id="add-emp-type" class="form-input" onchange="App.Staff._togglePayFields(\'add\')">'
      + '<option value="fulltime">Full-time (fixed)</option><option value="parttime">Part-time (hourly)</option>'
      + '</select></div>'
      + '</div>'
      + '<div class="grid grid-cols-2 gap-4">'
      + '<div id="add-salary-field">' + _field('Monthly Salary (RM)', '<input name="salary" type="number" min="0" class="form-input">') + '</div>'
      + '<div id="add-hourly-field" style="display:none">' + _field('Hourly Rate (RM)', '<input name="hourlyRate" type="number" min="0" step="0.01" class="form-input">') + '</div>'
      + '</div>'
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Email', '<input name="email" type="email" class="form-input">')
      + _field('Phone', '<input name="phone" class="form-input" placeholder="601234567890">')
      + '</div>'
      + _field('Join Date', '<input name="joinDate" type="date" class="form-input" value="' + App.Utils.today() + '">')
      + _field('Specialization', '<input name="specialization" class="form-input" placeholder="e.g. Japanese Level 1–4, English">')
      + _field('IC / NRIC', '<input name="nric" class="form-input" placeholder="e.g. 900101-10-1234">')
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Emergency Contact Name', '<input name="emergencyName" class="form-input" placeholder="Full name">')
      + _field('Emergency Contact Phone', '<input name="emergencyPhone" class="form-input" placeholder="601234567890">')
      + '</div>'
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Add Staff</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );
    document.getElementById('add-staff-form').addEventListener('submit', async function(e) {
      e.preventDefault();
      const fd = new FormData(e.target);
      const empType = fd.get('employmentType') || 'fulltime';
      const newStaff = {
        id: App.Utils.generateId('STF'),
        name: fd.get('name'),
        fullName: fd.get('fullName'),
        role: fd.get('role'),
        email: fd.get('email'),
        phone: fd.get('phone'),
        employmentType: empType,
        salary: empType === 'fulltime' ? (parseFloat(fd.get('salary')) || 0) : 0,
        hourlyRate: empType === 'parttime' ? (parseFloat(fd.get('hourlyRate')) || 0) : 0,
        joinDate: fd.get('joinDate'),
        specialization: fd.get('specialization') || '',
        nric: fd.get('nric') || '',
        emergencyName: fd.get('emergencyName') || '',
        emergencyPhone: fd.get('emergencyPhone') || '',
        status: 'Active'
      };
      var submitBtn = e.target.querySelector('button[type="submit"]');
      try {
        await App.Utils.withLoading(submitBtn, async function() {
          await App.Api.post('/api/staff', newStaff);
          await App.Api.loadSnapshot();
        });
        App.Utils.hideModal(true);
        App.Utils.showToast(App.Utils.esc(newStaff.fullName) + ' added!', 'success');
        App.Router.refresh();
      } catch (err) { /* auto-toasted */ }
    });
  }

  function _editModal(staffId) {
    const state = App.Store.get();
    const s = state.staff.find(function(x) { return x.id === staffId; });
    if (!s) return;

    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-4">Edit Staff — ' + App.Utils.esc(s.fullName) + '</h2>'
      + '<form id="edit-staff-form" class="space-y-4">'
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Full Name', '<input name="fullName" class="form-input" value="' + App.Utils.esc(s.fullName) + '" required>')
      + _field('Display Name', '<input name="name" class="form-input" value="' + App.Utils.esc(s.name) + '" required>')
      + '</div>'
      + '<div class="grid grid-cols-2 gap-4">'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Role</label>'
      + '<select name="role" class="form-input">'
      + ['Teacher','Senior Teacher','Admin'].map(function(r) {
          return '<option' + (s.role === r ? ' selected' : '') + '>' + r + '</option>';
        }).join('')
      + '</select></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Status</label>'
      + '<select name="status" class="form-input">'
      + ['Active','Inactive'].map(function(st) {
          return '<option' + (s.status === st ? ' selected' : '') + '>' + st + '</option>';
        }).join('')
      + '</select></div>'
      + '</div>'
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Email', '<input name="email" type="email" class="form-input" value="' + App.Utils.esc(s.email || '') + '">')
      + _field('Phone', '<input name="phone" class="form-input" value="' + App.Utils.esc(s.phone || '') + '">')
      + '</div>'
      + '<div class="grid grid-cols-2 gap-4">'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Employment</label>'
      + '<select name="employmentType" id="edit-emp-type" class="form-input" onchange="App.Staff._togglePayFields(\'edit\')">'
      + '<option value="fulltime"' + (s.employmentType !== 'parttime' ? ' selected' : '') + '>Full-time (fixed)</option>'
      + '<option value="parttime"' + (s.employmentType === 'parttime' ? ' selected' : '') + '>Part-time (hourly)</option>'
      + '</select></div>'
      + _field('Join Date', '<input name="joinDate" type="date" class="form-input" value="' + (s.joinDate || '') + '">')
      + '</div>'
      + '<div class="grid grid-cols-2 gap-4">'
      + '<div id="edit-salary-field"' + (s.employmentType === 'parttime' ? ' style="display:none"' : '') + '>' + _field('Monthly Salary (RM)', '<input name="salary" type="number" min="0" class="form-input" value="' + (s.salary || 0) + '">') + '</div>'
      + '<div id="edit-hourly-field"' + (s.employmentType !== 'parttime' ? ' style="display:none"' : '') + '>' + _field('Hourly Rate (RM)', '<input name="hourlyRate" type="number" min="0" step="0.01" class="form-input" value="' + (s.hourlyRate || 0) + '">') + '</div>'
      + '</div>'
      + _field('Specialization', '<input name="specialization" class="form-input" placeholder="e.g. Japanese Level 1–4, English" value="' + App.Utils.esc(s.specialization || '') + '">')
      + _field('IC / NRIC', '<input name="nric" class="form-input" value="' + App.Utils.esc(s.nric || '') + '">')
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Emergency Contact Name', '<input name="emergencyName" class="form-input" value="' + App.Utils.esc(s.emergencyName || '') + '">')
      + _field('Emergency Contact Phone', '<input name="emergencyPhone" class="form-input" value="' + App.Utils.esc(s.emergencyPhone || '') + '">')
      + '</div>'
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Save Changes</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );

    document.getElementById('edit-staff-form').addEventListener('submit', async function(e) {
      e.preventDefault();
      const fd = new FormData(e.target);
      const empType = fd.get('employmentType') || 'fulltime';
      const updated = Object.assign({}, s, {
        fullName: fd.get('fullName'),
        name: fd.get('name'),
        role: fd.get('role'),
        status: fd.get('status'),
        email: fd.get('email'),
        phone: fd.get('phone'),
        employmentType: empType,
        salary: empType === 'fulltime' ? (parseFloat(fd.get('salary')) || 0) : 0,
        hourlyRate: empType === 'parttime' ? (parseFloat(fd.get('hourlyRate')) || 0) : undefined,
        joinDate: fd.get('joinDate'),
        specialization: fd.get('specialization') || '',
        nric: fd.get('nric') || '',
        emergencyName: fd.get('emergencyName') || '',
        emergencyPhone: fd.get('emergencyPhone') || ''
      });
      try {
        await App.Api.put('/api/staff/' + staffId, updated);
        await App.Api.loadSnapshot();
        App.Utils.hideModal(true);
        App.Utils.showToast(App.Utils.esc(updated.fullName) + ' updated!', 'success');
        App.Router.refresh();
      } catch (err) {
        // App.Api auto-toasts the server error; no "saved locally" fallback
        // — that wrote to localStorage but the next snapshot reload would
        // wipe it, hiding the real failure from the user.
      }
    });
  }

  function _togglePayFields(prefix) {
    const sel = document.getElementById(prefix + '-emp-type');
    if (!sel) return;
    const isParttime = sel.value === 'parttime';
    const salaryField = document.getElementById(prefix + '-salary-field');
    const hourlyField = document.getElementById(prefix + '-hourly-field');
    if (salaryField) salaryField.style.display = isParttime ? 'none' : 'block';
    if (hourlyField) hourlyField.style.display = isParttime ? 'block' : 'none';
  }

  function _genPayrollModal(staffId) {
    const state = App.Store.get();
    const s = state.staff.find(function(x) { return x.id === staffId; });
    if (!s) return;
    const isParttime = s.employmentType === 'parttime';
    const now = new Date();
    const defaultMonth = now.getFullYear() + '-' + String(now.getMonth() + 1).padStart(2,'0');

    App.Utils.showModal(
      '<div class="p-6" style="min-width:380px">'
      + '<h2 class="text-lg font-bold mb-1">Generate Payroll</h2>'
      + '<p class="text-sm text-slate-500 mb-4">' + App.Utils.esc(s.fullName) + ' · ' + (isParttime ? 'Part-time' : 'Full-time') + '</p>'
      + '<form id="gen-payroll-form" class="space-y-4">'
      + _field('Month', '<input name="month" type="month" class="form-input" value="' + defaultMonth + '" required>')
      + (isParttime
          ? '<div class="grid grid-cols-2 gap-4">'
          + _field('Hours Worked', '<input name="hoursWorked" type="number" min="0" step="0.5" class="form-input" required oninput="App.Staff._previewPayroll()">')
          + _field('Hourly Rate (RM)', '<input name="hourlyRate" type="number" min="0" step="0.01" class="form-input" value="' + (s.hourlyRate || 0) + '" required oninput="App.Staff._previewPayroll()">')
          + '</div>'
          : _field('Base Salary (RM)', '<input name="salary" type="number" min="0" step="0.01" class="form-input" value="' + (s.salary || 0) + '" required>'))
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Bonus (RM)', '<input name="bonus" type="number" min="0" step="0.01" class="form-input" value="0">')
      + _field('Deductions (RM)', '<input name="deductions" type="number" min="0" step="0.01" class="form-input" value="0">')
      + '</div>'
      + (isParttime ? '<div id="pay-preview" style="background:#f0fdf4;border:1px solid #bbf7d0;border-radius:10px;padding:0.65rem;font-size:0.82rem;color:#166534;display:none"></div>' : '')
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" style="padding:0.5rem 1rem;font-size:0.85rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Generate</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );

    document.getElementById('gen-payroll-form').addEventListener('submit', function(e) {
      e.preventDefault();
      const fd = new FormData(e.target);
      const st = App.Store.get();
      const hoursWorked = isParttime ? (parseFloat(fd.get('hoursWorked')) || 0) : null;
      const hourlyRate  = isParttime ? (parseFloat(fd.get('hourlyRate')) || 0) : null;
      const baseSalary  = isParttime ? parseFloat((hoursWorked * hourlyRate).toFixed(2)) : (parseFloat(fd.get('salary')) || 0);
      const bonus = parseFloat(fd.get('bonus')) || 0;
      const deductions = parseFloat(fd.get('deductions')) || 0;
      const total = parseFloat((baseSalary + bonus - deductions).toFixed(2));
      const month = fd.get('month');
      // Check for duplicate
      const dup = st.payroll.find(function(p) { return p.staffId === staffId && p.month === month; });
      if (dup) { App.Utils.showToast('Payroll for this month already exists.', 'warning'); return; }
      const newPay = {
        id: 'PAY' + String(st.payroll.length + 1).padStart(3,'0'),
        staffId: staffId,
        month: month,
        baseSalary: baseSalary,
        hoursWorked: hoursWorked,
        hourlyRate: hourlyRate,
        bonus: bonus,
        deductions: deductions,
        total: total,
        status: 'Pending',
        paidOn: null
      };
      App.Store.set({ payroll: [...st.payroll, newPay] });
      App.Utils.hideModal(true);
      App.Utils.showToast('Payroll generated · ' + App.Utils.formatCurrency(total), 'success');
      setTimeout(function() { App.Staff._viewModal(staffId); App.Staff._switchTab('payroll'); }, 60);
    });
  }

  function _previewPayroll() {
    const hoursEl = document.querySelector('#gen-payroll-form [name="hoursWorked"]');
    const rateEl  = document.querySelector('#gen-payroll-form [name="hourlyRate"]');
    const preview = document.getElementById('pay-preview');
    if (!preview) return;
    const hours = parseFloat((hoursEl || {}).value) || 0;
    const rate  = parseFloat((rateEl || {}).value) || 0;
    if (hours > 0 && rate > 0) {
      preview.style.display = 'block';
      preview.textContent = hours + 'h × RM ' + rate.toFixed(2) + '/hr = RM ' + (hours * rate).toFixed(2) + ' base salary';
    } else {
      preview.style.display = 'none';
    }
  }

  // Show employment type badge on staff card
  function _empBadge(s) {
    if (!s.employmentType || s.employmentType === 'fulltime') return '';
    return App.Utils.badge('Part-time', 'orange');
  }

  function _infoRow(label, value) {
    return '<div class="bg-slate-50 rounded-lg p-3"><div class="text-xs text-slate-400 mb-0.5">' + label + '</div><div class="font-medium text-slate-700">' + (value || '—') + '</div></div>';
  }
  function _field(label, inputHtml) {
    return '<div><label class="block text-sm font-medium text-slate-700 mb-1">' + label + '</label>' + inputHtml + '</div>';
  }

  function _pendingTeacherModal() {
    var { registrations } = App.Store.get();
    var pending = (registrations || []).filter(function(r) { return r.status === 'pending' && r.type === 'teacher'; });
    var html = '<div style="max-height:70vh;overflow-y:auto">'
      + '<h2 class="text-xl font-bold text-slate-800 mb-4">Teacher Applications (' + pending.length + ')</h2>'
      + (pending.length === 0
        ? '<div class="py-8 text-center text-slate-400">No pending applications</div>'
        : pending.map(function(reg) {
            return '<div class="border border-slate-100 rounded-xl p-4 mb-3">'
              + '<div class="flex items-start justify-between gap-3 mb-2">'
              +   '<div>'
              +     '<div class="font-semibold text-slate-800">' + App.Utils.esc(reg.displayName || reg.parentName) + '</div>'
              +     '<div class="text-xs text-slate-500">' + App.Utils.esc(reg.email) + (reg.phone ? ' · ' + App.Utils.esc(reg.phone) : '') + '</div>'
              +   '</div>'
              +   '<span class="text-xs text-slate-400 shrink-0">' + App.Utils.formatDate(reg.submittedOn) + '</span>'
              + '</div>'
              + '<div class="grid grid-cols-2 gap-2 text-xs text-slate-600 mb-3">'
              +   (reg.specialization ? '<div><span class="text-slate-400">Specialization:</span> ' + App.Utils.esc(reg.specialization) + '</div>' : '')
              +   (reg.employmentType ? '<div><span class="text-slate-400">Employment:</span> ' + App.Utils.esc(reg.employmentType) + '</div>' : '')
              +   (reg.experience ? '<div><span class="text-slate-400">Experience:</span> ' + App.Utils.esc(reg.experience) + '</div>' : '')
              +   (reg.qualifications ? '<div><span class="text-slate-400">Qualifications:</span> ' + App.Utils.esc(reg.qualifications) + '</div>' : '')
              +   (reg.expectedSalary ? '<div><span class="text-slate-400">Expected Salary:</span> ' + App.Utils.esc(reg.expectedSalary) + '</div>' : '')
              +   (reg.bio ? '<div class="col-span-2"><span class="text-slate-400">Bio:</span> ' + App.Utils.esc(reg.bio) + '</div>' : '')
              +   (reg.emergencyName ? '<div class="col-span-2"><span class="text-slate-400">Emergency:</span> ' + App.Utils.esc(reg.emergencyName) + ' · ' + App.Utils.esc(reg.emergencyPhone) + '</div>' : '')
              + '</div>'
              + '<div class="flex gap-2">'
              +   '<button onclick="App.Staff._approveTeacher(\'' + reg.id + '\')" class="flex-1 py-1.5 text-sm bg-emerald-500 text-white rounded-lg hover:bg-emerald-600 font-medium">Approve &amp; Add to Staff</button>'
              +   '<button onclick="App.Staff._rejectTeacher(\'' + reg.id + '\')" class="flex-1 py-1.5 text-sm bg-red-50 text-red-600 rounded-lg hover:bg-red-100 font-medium border border-red-200">Reject</button>'
              + '</div>'
              + '</div>';
          }).join('')
      )
      + '</div>';
    App.Utils.showModal(html);
  }

  async function _approveTeacher(regId) {
    try {
      var result = await App.Api.post('/api/registrations/' + regId + '/approve', {});
      if (result) {
        App.Utils.hideModal(true);
        App.Utils.showToast('Teacher added to staff! Temp password: ' + result.tempPassword, 'success', 15000);
        await App.Api.loadSnapshot();
        App.Notifs.refresh();
        App.Router.refresh();
      }
    } catch(err) {
      App.Utils.showToast(err.message || 'Approval failed', 'error');
    }
  }

  async function _rejectTeacher(regId) {
    var ok = await App.Utils.showConfirm({ title: 'Reject application', message: 'This teacher application will be removed.', confirmLabel: 'Reject', danger: true });
    if (!ok) return;
    try {
      App.Api.optimisticRemove('registrations', regId);
      App.Utils.hideModal(true);
      App.Router.refresh();
      await App.Api.del('/api/registrations/' + regId);
      App.Notifs.refresh();
      App.Api.loadSnapshot().catch(function(){});
      App.Utils.showToast('Application rejected', 'info');
      App.Router.refresh();
    } catch(err) {
      App.Utils.showToast(err.message || 'Rejection failed', 'error');
    }
  }

  App.Staff = { render: render, _viewModal: _viewModal, _switchTab: _switchTab, _addModal: _addModal, _editModal: _editModal, _togglePayFields: _togglePayFields, _genPayrollModal: _genPayrollModal, _previewPayroll: _previewPayroll, _saveNotes: _saveNotes, _addReviewModal: _addReviewModal, _pendingTeacherModal: _pendingTeacherModal, _approveTeacher: _approveTeacher, _rejectTeacher: _rejectTeacher };
})();
