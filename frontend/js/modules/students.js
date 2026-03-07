(function() {
  window.App = window.App || {};

  let _search = '';
  let _statusFilter = 'All';

  function render(container) {
    const { students } = App.Store.get();
    const isAdmin = App.currentRole === 'admin';
    const isClient = App.currentRole === 'client';

    let displayStudents = students;
    if (isClient && App.clientParent) {
      displayStudents = students.filter(function(s) { return s.contact === App.clientParent; });
    }

    const filtered = displayStudents.filter(function(s) {
      const matchSearch = !_search || (s.firstName + ' ' + s.lastName + ' ' + s.id).toLowerCase().includes(_search.toLowerCase());
      const matchStatus = _statusFilter === 'All' || s.status === _statusFilter;
      return matchSearch && matchStatus;
    });

    const counts = { Total: displayStudents.length, Active: 0, Inactive: 0, New: 0, Waitlisted: 0 };
    displayStudents.forEach(function(s) { if (counts[s.status] !== undefined) counts[s.status]++; });

    container.innerHTML = ''
      + '<div class="flex items-center justify-between mb-6">'
      +   '<h1 class="text-2xl font-bold text-slate-800">Students</h1>'
      +   (isAdmin ? '<button onclick="App.Students._addModal()" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">+ Add Student</button>' : '')
      + '</div>'

      + '<div class="grid grid-cols-5 gap-4 mb-6">'
      + ['Total','Active','Inactive','New','Waitlisted'].map(function(k) {
          const colors = { Total:'text-blue-600', Active:'text-emerald-600', Inactive:'text-red-500', New:'text-blue-500', Waitlisted:'text-amber-500' };
          return '<div class="bg-white rounded-xl border border-slate-100 shadow-sm p-4 text-center">'
            + '<div class="text-3xl font-bold ' + (colors[k]||'text-slate-700') + '">' + counts[k] + '</div>'
            + '<div class="text-xs text-slate-500 mt-1">' + k + '</div>'
            + '</div>';
        }).join('')
      + '</div>'

      + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm">'
      +   '<div class="p-4 border-b border-slate-100 flex items-center gap-3 flex-wrap">'
      +     '<input id="student-search" type="text" placeholder="Search by name or ID..." value="' + _search + '" oninput="App.Students._onSearch(this.value)" class="flex-1 min-w-48 px-3 py-2 text-sm border border-slate-200 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-400">'
      +     '<select onchange="App.Students._onFilter(this.value)" class="px-3 py-2 text-sm border border-slate-200 rounded-lg focus:outline-none">'
      +     ['All','Active','Inactive','New','Waitlisted'].map(function(s) {
              return '<option value="' + s + '" ' + (s === _statusFilter ? 'selected' : '') + '>' + s + '</option>';
            }).join('')
      +     '</select>'
      +   '</div>'
      +   '<div class="overflow-x-auto">'
      +     '<table class="w-full">'
      +       '<thead class="bg-slate-50 border-b border-slate-100"><tr>'
      +         '<th class="th">Student</th><th class="th">Classes</th><th class="th">DOB</th>'
      +         '<th class="th">Parent / Contact</th><th class="th">Status</th><th class="th">Action</th>'
      +       '</tr></thead>'
      +       '<tbody class="divide-y divide-slate-50">'
      +       (filtered.length === 0 ? '<tr><td colspan="6" class="px-6 py-12 text-center text-slate-400 text-sm">No students found</td></tr>' : '')
      +       filtered.map(function(s) {
              const { classes } = App.Store.get();
              const enrolledNames = s.enrolledClasses.map(function(cid) {
                const c = classes.find(function(x) { return x.id === cid; });
                return c ? c.name : cid;
              });
              return '<tr class="hover:bg-slate-50 transition-colors">'
                + '<td class="td"><div class="flex items-center gap-3">'
                +   '<div class="w-9 h-9 rounded-full bg-blue-100 text-blue-700 font-bold text-sm flex items-center justify-center shrink-0">' + s.firstName.charAt(0) + s.lastName.charAt(0) + '</div>'
                +   '<div><div class="font-medium text-slate-800">' + s.firstName + ' ' + s.lastName + '</div><div class="text-xs text-slate-400">' + s.id + '</div></div>'
                + '</div></td>'
                + '<td class="td"><div class="flex flex-wrap gap-1">'
                + (enrolledNames.length === 0 ? '<span class="text-xs text-slate-400">—</span>'
                  : enrolledNames.map(function(n) { return '<span class="text-xs px-2 py-0.5 bg-blue-50 text-blue-700 rounded-full border border-blue-100">' + n + '</span>'; }).join(''))
                + '</div></td>'
                + '<td class="td text-sm text-slate-600">' + App.Utils.formatDate(s.dob) + '</td>'
                + '<td class="td text-sm"><div class="text-slate-700">' + s.parentName + '</div><div class="text-slate-400 text-xs">' + s.contact + '</div></td>'
                + '<td class="td">' + App.Utils.statusBadge(s.status) + '</td>'
                + '<td class="td"><button onclick="App.Students._viewModal(\'' + s.id + '\')" class="text-xs px-3 py-1.5 border border-slate-200 rounded-lg hover:bg-slate-50 text-slate-600">View</button></td>'
                + '</tr>';
            }).join('')
      +       '</tbody>'
      +     '</table>'
      +   '</div>'
      + '</div>';
  }

  function _onSearch(val) { _search = val; App.Router.refresh(); }
  function _onFilter(val) { _statusFilter = val; App.Router.refresh(); }

  function _viewModal(studentId) {
    const { students, classes, invoices } = App.Store.get();
    const isAdmin = App.currentRole === 'admin';
    const s = students.find(function(x) { return x.id === studentId; });
    if (!s) return;

    const enrolledClasses = s.enrolledClasses.map(function(cid) {
      return classes.find(function(c) { return c.id === cid; });
    }).filter(Boolean);

    const studentInvoices = invoices.filter(function(inv) { return inv.studentId === studentId; });
    const totalPaid = studentInvoices.filter(function(i) { return i.status === 'Paid'; }).reduce(function(s, i) { return s + i.amount; }, 0);

    App.Utils.showModal(
      '<div class="p-6">'
      + '<div class="flex items-center gap-4 mb-6">'
      +   '<div class="w-16 h-16 rounded-2xl bg-blue-100 text-blue-700 font-bold text-2xl flex items-center justify-center">' + s.firstName.charAt(0) + s.lastName.charAt(0) + '</div>'
      +   '<div>'
      +     '<h2 class="text-xl font-bold text-slate-800">' + s.firstName + ' ' + s.lastName + '</h2>'
      +     '<div class="flex items-center gap-2 mt-1">' + App.Utils.statusBadge(s.status) + '<span class="text-xs text-slate-400">' + s.id + '</span></div>'
      +   '</div>'
      + '</div>'

      + '<div class="flex border-b border-slate-100 mb-4 gap-1" id="student-tabs">'
      + ['Details','Classes','Invoices'].map(function(tab, i) {
          return '<button onclick="App.Students._switchTab(\'' + tab.toLowerCase() + '\')" id="tab-' + tab.toLowerCase() + '" class="tab-btn px-4 py-2 text-sm font-medium ' + (i===0?'border-b-2 border-blue-600 text-blue-600':'text-slate-500 hover:text-slate-700') + '">' + tab + '</button>';
        }).join('')
      + '</div>'

      + '<div id="tab-panel-details">'
      +   '<div class="grid grid-cols-2 gap-3 text-sm">'
      +   _infoRow('Date of Birth', App.Utils.formatDate(s.dob))
      +   _infoRow('Gender', s.gender)
      +   _infoRow('Parent / Guardian', s.parentName)
      +   _infoRow('Email', s.contact)
      +   _infoRow('Phone', s.phone)
      +   _infoRow('Branch', s.branch)
      +   _infoRow('Registered On', App.Utils.formatDate(s.registeredOn))
      +   (s.siblings && s.siblings.length ? _infoRow('Siblings', s.siblings.join(', ')) : '')
      +   (s.notes ? _infoRow('Notes', s.notes) : '')
      +   '</div>'
      + '</div>'

      + '<div id="tab-panel-classes" class="hidden">'
      + (enrolledClasses.length === 0 ? '<p class="text-sm text-slate-400 text-center py-6">Not enrolled in any class</p>' : '')
      + enrolledClasses.map(function(c) {
          const { staff } = App.Store.get();
          const colors = App.Utils.colorClasses(c.color);
          const teachers = c.teacherIds.map(function(tid) {
            const st = staff.find(function(x) { return x.id === tid; });
            return st ? st.fullName : tid;
          }).join(', ');
          return '<div class="' + colors.bg + ' border-l-4 ' + colors.border + ' rounded-xl p-4 mb-2">'
            + '<div class="font-semibold ' + colors.text + '">' + c.name + '</div>'
            + '<div class="text-sm text-slate-600 mt-1">' + c.day + ' · ' + App.Utils.formatTime(c.time) + ' – ' + App.Utils.formatTime(c.endTime) + '</div>'
            + '<div class="text-sm text-slate-500">' + teachers + ' · ' + c.classroom + '</div>'
            + '</div>';
        }).join('')
      + '</div>'

      + '<div id="tab-panel-invoices" class="hidden">'
      + '<div class="flex justify-between items-center mb-3"><span class="text-sm text-slate-500">Total paid:</span><span class="font-bold text-emerald-600">' + App.Utils.formatCurrency(totalPaid) + '</span></div>'
      + (studentInvoices.length === 0 ? '<p class="text-sm text-slate-400 text-center py-6">No invoices</p>'
        : '<table class="w-full text-sm"><thead><tr class="border-b"><th class="text-left py-2 text-slate-500 font-medium">Description</th><th class="text-right py-2 text-slate-500 font-medium">Amount</th><th class="text-right py-2 text-slate-500 font-medium">Status</th></tr></thead><tbody>'
          + studentInvoices.map(function(inv) {
              return '<tr class="border-b border-slate-50"><td class="py-2"><div>' + inv.description + '</div><div class="text-xs text-slate-400">Due ' + App.Utils.formatDate(inv.dueDate) + '</div></td><td class="py-2 text-right font-medium">' + App.Utils.formatCurrency(inv.amount) + '</td><td class="py-2 text-right">' + App.Utils.statusBadge(inv.status) + '</td></tr>';
            }).join('')
          + '</tbody></table>')
      + '</div>'

      + '<div class="mt-6 flex justify-end gap-2 border-t border-slate-100 pt-4">'
      + '<button onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Close</button>'
      + (isAdmin ? '<button onclick="App.Students._editModal(\'' + studentId + '\')" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Edit Student</button>' : '')
      + '</div>'
      + '</div>'
    );
  }

  function _editModal(studentId) {
    App.Utils.hideModal();
    const state = App.Store.get();
    const s = state.students.find(function(x) { return x.id === studentId; });
    if (!s) return;

    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-4">Edit Student — ' + s.firstName + ' ' + s.lastName + '</h2>'
      + '<form id="edit-student-form" class="space-y-4">'
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('First Name', '<input name="firstName" class="form-input" value="' + s.firstName + '" required>')
      + _field('Last Name', '<input name="lastName" class="form-input" value="' + s.lastName + '" required>')
      + '</div>'
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Date of Birth', '<input name="dob" type="date" class="form-input" value="' + s.dob + '">')
      + _field('Gender', '<select name="gender" class="form-input"><option' + (s.gender==='Male'?' selected':'') + '>Male</option><option' + (s.gender==='Female'?' selected':'') + '>Female</option></select>')
      + '</div>'
      + _field('Parent / Guardian Name', '<input name="parentName" class="form-input" value="' + s.parentName + '">')
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Parent Email', '<input name="contact" type="email" class="form-input" value="' + s.contact + '">')
      + _field('Phone', '<input name="phone" class="form-input" value="' + (s.phone||'') + '">')
      + '</div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Status</label>'
      + '<select name="status" class="form-input">'
      + ['Active','Inactive','New','Waitlisted'].map(function(st) { return '<option' + (s.status===st?' selected':'') + '>' + st + '</option>'; }).join('')
      + '</select></div>'
      + _multiClassField(s.enrolledClasses, state.classes)
      + _field('Notes', '<textarea name="notes" class="form-input" rows="2">' + (s.notes||'') + '</textarea>')
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Save Changes</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );

    document.getElementById('edit-student-form').addEventListener('submit', function(e) {
      e.preventDefault();
      const fd = new FormData(e.target);
      const newClasses = fd.getAll('classIds');
      const st = App.Store.get();

      // Recalculate class enrollment counts
      let newClasses2 = st.classes.map(function(c) {
        const wasEnrolled = s.enrolledClasses.indexOf(c.id) > -1;
        const willEnroll  = newClasses.indexOf(c.id) > -1;
        if (wasEnrolled && !willEnroll) return Object.assign({}, c, { enrolled: Math.max(0, c.enrolled - 1) });
        if (!wasEnrolled && willEnroll) return Object.assign({}, c, { enrolled: c.enrolled + 1 });
        return c;
      });

      const updated = Object.assign({}, s, {
        firstName: fd.get('firstName'),
        lastName: fd.get('lastName'),
        dob: fd.get('dob'),
        gender: fd.get('gender'),
        parentName: fd.get('parentName'),
        contact: fd.get('contact'),
        phone: fd.get('phone'),
        status: fd.get('status'),
        enrolledClasses: newClasses,
        notes: fd.get('notes')
      });

      App.Store.set({ students: st.students.map(function(x) { return x.id === studentId ? updated : x; }), classes: newClasses2 });
      App.Utils.hideModal();
      App.Utils.showToast(updated.firstName + ' ' + updated.lastName + ' updated', 'success');
      App.Router.refresh();
    });
  }

  function _addModal() {
    const { classes } = App.Store.get();
    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-4">Add New Student</h2>'
      + '<form id="add-student-form" class="space-y-4">'
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('First Name', '<input name="firstName" class="form-input" required>')
      + _field('Last Name', '<input name="lastName" class="form-input" required>')
      + '</div>'
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Date of Birth', '<input name="dob" type="date" class="form-input" required>')
      + _field('Gender', '<select name="gender" class="form-input"><option>Male</option><option>Female</option></select>')
      + '</div>'
      + _field('Parent / Guardian Name', '<input name="parentName" class="form-input" required>')
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Parent Email', '<input name="contact" type="email" class="form-input" required>')
      + _field('Phone (with country code)', '<input name="phone" class="form-input" placeholder="60123456789">')
      + '</div>'
      + _multiClassField([], classes)
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Add Student</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );
    document.getElementById('add-student-form').addEventListener('submit', function(e) {
      e.preventDefault();
      const fd = new FormData(e.target);
      const state = App.Store.get();
      const selectedClasses = fd.getAll('classIds');
      const newId = 'STU' + String(state.students.length + 1).padStart(3,'0');
      const newStudent = {
        id: newId,
        firstName: fd.get('firstName'),
        lastName: fd.get('lastName'),
        dob: fd.get('dob'),
        gender: fd.get('gender'),
        parentName: fd.get('parentName'),
        contact: fd.get('contact'),
        phone: fd.get('phone'),
        branch: 'The Study Hub',
        status: 'New',
        registeredOn: App.Utils.today(),
        enrolledClasses: selectedClasses,
        siblings: [],
        notes: ''
      };
      const newClasses = state.classes.map(function(c) {
        return selectedClasses.indexOf(c.id) > -1 ? Object.assign({}, c, { enrolled: c.enrolled + 1 }) : c;
      });
      App.Store.set({ students: [...state.students, newStudent], classes: newClasses });
      App.Utils.hideModal();
      App.Utils.showToast(newStudent.firstName + ' ' + newStudent.lastName + ' added!', 'success');
      App.Router.refresh();
    });
  }

  // Multi-select class field — renders checkboxes for each class
  function _multiClassField(selected, classes) {
    return '<div><label class="block text-sm font-medium text-slate-700 mb-1">Enrolled Classes <span class="text-slate-400 font-normal">(select one or more)</span></label>'
      + '<div class="border border-slate-200 rounded-xl p-3 space-y-1.5 max-h-48 overflow-y-auto bg-white">'
      + (classes.length === 0 ? '<p class="text-xs text-slate-400">No classes yet</p>' : '')
      + classes.map(function(c) {
          const checked = selected.indexOf(c.id) > -1 ? ' checked' : '';
          return '<label class="flex items-center gap-2.5 cursor-pointer group">'
            + '<input type="checkbox" name="classIds" value="' + c.id + '"' + checked + ' class="w-3.5 h-3.5 rounded accent-blue-600">'
            + '<span class="text-sm text-slate-700 group-hover:text-blue-600 transition-colors">' + c.name + '</span>'
            + '<span class="text-xs text-slate-400 ml-auto">' + c.day + ' ' + App.Utils.formatTime(c.time) + '</span>'
            + '</label>';
        }).join('')
      + '</div></div>';
  }

  function _switchTab(tab) {
    ['details','classes','invoices'].forEach(function(t) {
      document.getElementById('tab-panel-' + t).classList.toggle('hidden', t !== tab);
      const btn = document.getElementById('tab-' + t);
      if (t === tab) { btn.classList.add('border-b-2','border-blue-600','text-blue-600'); btn.classList.remove('text-slate-500'); }
      else { btn.classList.remove('border-b-2','border-blue-600','text-blue-600'); btn.classList.add('text-slate-500'); }
    });
  }

  function _infoRow(label, value) {
    return '<div class="bg-slate-50 rounded-lg p-3"><div class="text-xs text-slate-400 mb-0.5">' + label + '</div><div class="font-medium text-slate-700">' + value + '</div></div>';
  }
  function _field(label, inputHtml) {
    return '<div><label class="block text-sm font-medium text-slate-700 mb-1">' + label + '</label>' + inputHtml + '</div>';
  }

  App.Students = { render: render, _onSearch: _onSearch, _onFilter: _onFilter, _viewModal: _viewModal, _editModal: _editModal, _switchTab: _switchTab, _addModal: _addModal };
})();
