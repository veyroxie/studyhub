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
      + '<div class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">'
      + staff.map(function(s) { return _staffCard(s, classes, isAdmin); }).join('')
      + '</div>';
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
        + (staffPayroll.length === 0 ? '<p class="text-sm text-slate-400 text-center py-6">No payroll records</p>'
          : '<table class="w-full text-sm"><thead><tr class="border-b"><th class="text-left py-2 text-slate-500 font-medium">Month</th><th class="text-right py-2 text-slate-500 font-medium">Base</th><th class="text-right py-2 text-slate-500 font-medium">Bonus</th><th class="text-right py-2 text-slate-500 font-medium">Total</th><th class="text-right py-2 text-slate-500 font-medium">Status</th></tr></thead><tbody>'
          + staffPayroll.map(function(p) {
              return '<tr class="border-b border-slate-50"><td class="py-2 font-medium">' + App.Utils.formatMonth(p.month) + '</td><td class="py-2 text-right text-slate-600">' + App.Utils.formatCurrency(p.baseSalary) + '</td><td class="py-2 text-right text-slate-600">' + (p.bonus ? App.Utils.formatCurrency(p.bonus) : '—') + '</td><td class="py-2 text-right font-semibold">' + App.Utils.formatCurrency(p.total) + '</td><td class="py-2 text-right">' + App.Utils.statusBadge(p.status) + '</td></tr>';
            }).join('')
          + '</tbody></table>')
        + '</div>' : '')

      + '<div class="mt-4 flex justify-end"><button onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Close</button></div>'
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
      + _field('Monthly Salary (RM)', '<input name="salary" type="number" min="0" class="form-input">')
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
      const newStaff = {
        id: 's' + (state.staff.length + 1),
        name: fd.get('name'),
        fullName: fd.get('fullName'),
        role: fd.get('role'),
        email: fd.get('email'),
        phone: fd.get('phone'),
        salary: parseFloat(fd.get('salary')) || 0,
        joinDate: fd.get('joinDate'),
        status: 'Active'
      };
      App.Store.set({ staff: [...state.staff, newStaff] });
      App.Utils.hideModal();
      App.Utils.showToast(newStaff.fullName + ' added!', 'success');
      App.Router.refresh();
    });
  }

  function _infoRow(label, value) {
    return '<div class="bg-slate-50 rounded-lg p-3"><div class="text-xs text-slate-400 mb-0.5">' + label + '</div><div class="font-medium text-slate-700">' + (value || '—') + '</div></div>';
  }
  function _field(label, inputHtml) {
    return '<div><label class="block text-sm font-medium text-slate-700 mb-1">' + label + '</label>' + inputHtml + '</div>';
  }

  App.Staff = { render: render, _viewModal: _viewModal, _switchTab: _switchTab, _addModal: _addModal };
})();
