(function() {
  window.App = window.App || {};

  function render(container) {
    const { staff, classes, payroll } = App.Store.get();
    const isAdmin = App.currentRole === 'admin';

    container.innerHTML = ''
      + '<div class="flex items-center justify-between mb-6">'
      +   '<h1 class="text-2xl font-bold text-slate-800">Staff</h1>'
      +   (isAdmin ? '<button onclick="App.Staff._addModal()" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">+ Add Staff</button>' : '')
      + '</div>'
      + (staff.length === 0
        ? '<div class="bg-white rounded-xl border border-slate-100 shadow-sm">' + App.Utils.emptyState(
            'No staff yet',
            'Add your first staff member to get started.',
            isAdmin ? '<button onclick="App.Staff._addModal()" style="padding:0.5rem 1.25rem;font-size:0.83rem;font-weight:600;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">+ Add Staff</button>' : ''
          ) + '</div>'
        : '<div class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">'
          + staff.map(function(s) { return _staffCard(s, classes, isAdmin); }).join('')
          + '</div>');
  }

  function _staffCard(s, classes, isAdmin) {
    const teachingClasses = classes.filter(function(c) { return c.teacherIds.indexOf(s.id) > -1; });
    const avatarColors = ['bg-blue-100 text-blue-700', 'bg-purple-100 text-purple-700', 'bg-emerald-100 text-emerald-700', 'bg-amber-100 text-amber-700'];
    const colorIdx = ['s1','s2','s3','s4'].indexOf(s.id);
    const avatarColor = avatarColors[colorIdx >= 0 ? colorIdx : 0];

    return '<div class="bg-white rounded-xl border border-slate-100 shadow-sm p-5 cursor-pointer hover:shadow-md hover:border-blue-200 transition-all" onclick="App.Staff._viewModal(\'' + s.id + '\')">'
      + '<div class="flex items-start gap-3 mb-4">'
      +   '<div class="w-12 h-12 rounded-xl ' + avatarColor + ' font-bold text-lg flex items-center justify-center shrink-0">' + s.name.charAt(0) + '</div>'
      +   '<div>'
      +     '<div class="font-semibold text-slate-800">' + s.fullName + '</div>'
      +     '<div class="text-xs text-slate-500 mt-0.5">' + s.role + '</div>'
      +     '<div class="mt-1">' + App.Utils.statusBadge(s.status) + '</div>'
      +   '</div>'
      + '</div>'
      + '<div class="border-t border-slate-50 pt-3 space-y-1">'
      + '<div class="text-xs text-slate-500 flex justify-between"><span>Classes</span><span class="font-medium text-slate-700">' + teachingClasses.length + '</span></div>'
      + (isAdmin ? '<div class="text-xs text-slate-500 flex justify-between"><span>Salary</span><span class="font-medium text-slate-700">' + App.Utils.formatCurrency(s.salary) + '/mo</span></div>' : '')
      + '<div class="text-xs text-slate-500 flex justify-between"><span>Since</span><span class="font-medium text-slate-700">' + App.Utils.formatDate(s.joinDate) + '</span></div>'
      + '</div>'
      + '</div>';
  }

  function _viewModal(staffId) {
    const { staff, classes, payroll } = App.Store.get();
    const isAdmin = App.currentRole === 'admin';
    const s = staff.find(function(x) { return x.id === staffId; });
    if (!s) return;

    const teachingClasses = classes.filter(function(c) { return c.teacherIds.indexOf(s.id) > -1; });
    const staffPayroll = payroll.filter(function(p) { return p.staffId === staffId; })
      .sort(function(a, b) { return b.month.localeCompare(a.month); });

    App.Utils.showModal(
      '<div class="p-6">'
      + '<div class="flex items-center gap-4 mb-6">'
      +   '<div class="w-16 h-16 rounded-2xl bg-blue-100 text-blue-700 font-bold text-2xl flex items-center justify-center">' + s.name.charAt(0) + '</div>'
      +   '<div>'
      +     '<h2 class="text-xl font-bold text-slate-800">' + s.fullName + '</h2>'
      +     '<div class="flex items-center gap-2 mt-1">' + App.Utils.badge(s.role, 'blue') + App.Utils.statusBadge(s.status) + '</div>'
      +   '</div>'
      + '</div>'

      + '<div class="flex border-b border-slate-100 mb-4 gap-1">'
      + (isAdmin ? ['Info','Schedule','Payroll'] : ['Info','Schedule']).map(function(tab, i) {
          return '<button onclick="App.Staff._switchTab(\'' + tab.toLowerCase() + '\')" id="stab-' + tab.toLowerCase() + '" class="tab-btn px-4 py-2 text-sm font-medium ' + (i===0?'border-b-2 border-blue-600 text-blue-600':'text-slate-500 hover:text-slate-700') + '">' + tab + '</button>';
        }).join('')
      + '</div>'

      + '<div id="stab-panel-info">'
      +   '<div class="grid grid-cols-2 gap-3 text-sm">'
      +   _infoRow('Email', s.email)
      +   _infoRow('Phone', s.phone)
      +   _infoRow('Role', s.role)
      +   _infoRow('Joined', App.Utils.formatDate(s.joinDate))
      +   (isAdmin ? _infoRow('Monthly Salary', App.Utils.formatCurrency(s.salary)) : '')
      +   '</div>'
      + '</div>'

      + '<div id="stab-panel-schedule" class="hidden">'
      + (teachingClasses.length === 0 ? '<p class="text-sm text-slate-400 text-center py-6">No classes assigned</p>'
        : '<table class="w-full text-sm"><thead><tr class="border-b"><th class="text-left py-2 text-slate-500 font-medium">Class</th><th class="text-left py-2 text-slate-500 font-medium">Schedule</th><th class="text-left py-2 text-slate-500 font-medium">Room</th><th class="text-right py-2 text-slate-500 font-medium">Enrolled</th></tr></thead><tbody>'
        + teachingClasses.map(function(c) {
            return '<tr class="border-b border-slate-50"><td class="py-2 font-medium">' + c.name + '</td><td class="py-2 text-slate-600">' + c.day + ' ' + App.Utils.formatTime(c.time) + '</td><td class="py-2 text-slate-500">' + c.classroom + '</td><td class="py-2 text-right">' + c.enrolled + '/' + c.capacity + '</td></tr>';
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

      + '<div class="mt-4 flex justify-between">'
      + (isAdmin ? '<button onclick="App.Utils.hideModal(); setTimeout(function(){App.Staff._editModal(\'' + staffId + '\')},50)" class="px-4 py-2 text-sm bg-slate-700 text-white rounded-lg hover:bg-slate-800">Edit Details</button>' : '<div></div>')
      + '<button onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Close</button>'
      + '</div>'
      + '</div>'
    );
  }

  function _switchTab(tab) {
    ['info','schedule','payroll'].forEach(function(t) {
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
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Add Staff</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );
    document.getElementById('add-staff-form').addEventListener('submit', function(e) {
      e.preventDefault();
      const fd = new FormData(e.target);
      const state = App.Store.get();
      const empType = fd.get('employmentType') || 'fulltime';
      const newStaff = {
        id: 's' + (state.staff.length + 1),
        name: fd.get('name'),
        fullName: fd.get('fullName'),
        role: fd.get('role'),
        email: fd.get('email'),
        phone: fd.get('phone'),
        employmentType: empType,
        salary: empType === 'fulltime' ? (parseFloat(fd.get('salary')) || 0) : 0,
        hourlyRate: empType === 'parttime' ? (parseFloat(fd.get('hourlyRate')) || 0) : undefined,
        joinDate: fd.get('joinDate'),
        status: 'Active'
      };
      App.Store.set({ staff: [...state.staff, newStaff] });
      App.Utils.hideModal();
      App.Utils.showToast(newStaff.fullName + ' added!', 'success');
      App.Router.refresh();
    });
  }

  function _editModal(staffId) {
    const state = App.Store.get();
    const s = state.staff.find(function(x) { return x.id === staffId; });
    if (!s) return;

    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-4">Edit Staff — ' + s.fullName + '</h2>'
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
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Save Changes</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );

    document.getElementById('edit-staff-form').addEventListener('submit', function(e) {
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
        joinDate: fd.get('joinDate')
      });
      const st = App.Store.get();
      App.Store.set({ staff: st.staff.map(function(x) { return x.id === staffId ? updated : x; }) });
      App.Utils.hideModal();
      App.Utils.showToast(updated.fullName + ' updated!', 'success');
      App.Router.refresh();
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
      App.Utils.hideModal();
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

  App.Staff = { render: render, _viewModal: _viewModal, _switchTab: _switchTab, _addModal: _addModal, _editModal: _editModal, _togglePayFields: _togglePayFields, _genPayrollModal: _genPayrollModal, _previewPayroll: _previewPayroll };
})();
