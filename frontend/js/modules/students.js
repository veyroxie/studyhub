(function() {
  window.App = window.App || {};

  let _search = '';
  let _statusFilter = 'All';
  let _selected = {};
  let _studentPage = 0;
  var _PAGE_SIZE = 15;

  function _paginationControls(page, total, moduleFn) {
    var totalPages = Math.ceil(total / _PAGE_SIZE);
    if (total <= _PAGE_SIZE) return '';
    var start = page * _PAGE_SIZE + 1;
    var end = Math.min((page + 1) * _PAGE_SIZE, total);
    var prevDis = page === 0;
    var nextDis = page >= totalPages - 1;
    return '<div style="display:flex;align-items:center;justify-content:space-between;margin-top:1rem;padding:0.75rem 1rem;">'
      + '<span style="font-size:0.8rem;color:#64748b;">Showing ' + start + '–' + end + ' of ' + total + '</span>'
      + '<div style="display:flex;gap:0.5rem;">'
      + '<button onclick="' + moduleFn + '(' + (page - 1) + ')"' + (prevDis ? ' disabled' : '') + ' style="padding:0.35rem 0.75rem;font-size:0.8rem;border:1px solid #e2e8f0;border-radius:8px;cursor:' + (prevDis ? 'default' : 'pointer') + ';background:#fff;color:#374151;' + (prevDis ? 'opacity:0.4;' : '') + '">Prev</button>'
      + '<button onclick="' + moduleFn + '(' + (page + 1) + ')"' + (nextDis ? ' disabled' : '') + ' style="padding:0.35rem 0.75rem;font-size:0.8rem;border:1px solid #e2e8f0;border-radius:8px;cursor:' + (nextDis ? 'default' : 'pointer') + ';background:#fff;color:#374151;' + (nextDis ? 'opacity:0.4;' : '') + '">Next</button>'
      + '</div></div>';
  }

  function render(container) {
    const { students, classes } = App.Store.get();
    const isAdmin = App.currentRole === 'admin';
    const isClient = App.currentRole === 'client';
    const isTeacher = App.currentRole === 'teacher';

    let displayStudents = students;
    if (isClient && App.clientParent) {
      displayStudents = students.filter(function(s) { return s.contact === App.clientParent; })
        .filter(function(s) { return s.status !== 'Inactive'; });
    }
    if (isTeacher && App.currentTeacher) {
      const teacherClassIds = classes
        .filter(function(c) { return c.teacherIds.indexOf(App.currentTeacher) > -1; })
        .map(function(c) { return c.id; });
      displayStudents = students.filter(function(s) {
        return s.enrolledClasses.some(function(cid) { return teacherClassIds.indexOf(cid) > -1; });
      });
    }

    const filtered = displayStudents.filter(function(s) {
      const matchSearch = !_search || (s.firstName + ' ' + s.lastName + ' ' + s.id).toLowerCase().includes(_search.toLowerCase());
      const matchStatus = _statusFilter === 'All' || s.status === _statusFilter;
      return matchSearch && matchStatus;
    });

    const counts = { Total: displayStudents.length, Active: 0, Inactive: 0, New: 0, Waitlisted: 0 };
    displayStudents.forEach(function(s) { if (counts[s.status] !== undefined) counts[s.status]++; });

    const { registrations } = App.Store.get();
    const pendingRegs = (registrations || []).filter(function(r) { return r.status === 'pending'; });

    var paged = filtered.slice(_studentPage * _PAGE_SIZE, (_studentPage + 1) * _PAGE_SIZE);

    const colCount = isAdmin ? 7 : 6;

    container.innerHTML = ''
      + '<div class="flex items-center justify-between mb-6">'
      +   '<h1 class="text-2xl font-bold text-slate-800">Students</h1>'
      +   '<div class="flex gap-2">'
      +   (isAdmin && pendingRegs.length > 0
            ? '<button onclick="App.Students._pendingModal()" class="px-4 py-2 text-sm bg-amber-500 text-white rounded-lg hover:bg-amber-600 flex items-center gap-2"><span class="w-5 h-5 bg-white text-amber-600 text-xs font-bold rounded-full flex items-center justify-center">' + pendingRegs.length + '</span>Pending</button>'
            : '')
      +   (isAdmin ? '<button onclick="App.Students._exportCSV()" class="px-4 py-2 text-sm bg-emerald-600 text-white rounded-lg hover:bg-emerald-700">Export CSV</button>' : '')
      +   (isAdmin ? '<button onclick="App.Students._addModal()" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">+ Add Student</button>' : '')
      +   '</div>'
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
      +     (isAdmin || isTeacher ? '<select onchange="App.Students._onFilter(this.value)" class="px-3 py-2 text-sm border border-slate-200 rounded-lg focus:outline-none">'
      +     ['All','Active','Inactive','New','Waitlisted'].map(function(s) {
              return '<option value="' + s + '" ' + (s === _statusFilter ? 'selected' : '') + '>' + s + '</option>';
            }).join('')
      +     '</select>' : '')
      +   '</div>'
      +   '<div id="stu-bulk-bar" style="padding:0 1rem">' + _bulkBar() + '</div>'
      +   '<div class="overflow-x-auto">'
      +     '<table class="w-full" role="table">'
      +       '<caption class="sr-only">Student list</caption>'
      +       '<thead class="bg-slate-50 border-b border-slate-100"><tr>'
      +         (isAdmin ? '<th scope="col" class="th" style="width:36px"><input type="checkbox" id="select-all-cb" onchange="App.Students._toggleSelectAll(this.checked)" style="cursor:pointer"></th>' : '')
      +         '<th scope="col" class="th">Student</th><th scope="col" class="th">Classes</th><th scope="col" class="th">DOB</th>'
      +         '<th scope="col" class="th">Parent / Contact</th><th scope="col" class="th">Status</th><th scope="col" class="th">Action</th>'
      +       '</tr></thead>'
      +       '<tbody class="divide-y divide-slate-50">'
      +       (filtered.length === 0
          ? '<tr><td colspan="' + colCount + '" style="padding:0">' + App.Utils.emptyState(
              (_search || _statusFilter !== 'All') ? 'No students match your filters' : 'No students yet',
              (_search || _statusFilter !== 'All') ? 'Try adjusting your search or status filter.' : 'Add your first student to get started.',
              (isAdmin && !(_search || _statusFilter !== 'All')) ? '<button onclick="App.Students._addModal()" style="padding:0.5rem 1.25rem;font-size:0.83rem;font-weight:600;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">+ Add Student</button>' : (_search || _statusFilter !== 'All') ? '<button onclick="App.Students._clearFilters()" style="padding:0.5rem 1.25rem;font-size:0.83rem;font-weight:600;background:#f1f5f9;color:#475569;border:none;border-radius:8px;cursor:pointer">Clear Filters</button>' : ''
            ) + '</td></tr>'
          : paged.map(function(s) {
              const { classes, replacementCredits } = App.Store.get();
              const enrolledNames = s.enrolledClasses.map(function(cid) {
                const c = classes.find(function(x) { return x.id === cid; });
                return c ? c.name : cid;
              });
              // Replacement credit balance
              var _rc = (replacementCredits || []).filter(function(rc) { return rc.studentId === s.id; });
              var _bal = _rc.filter(function(rc) { return rc.type === 'earned'; }).reduce(function(a, rc) { return a + (rc.minutes || 0); }, 0)
                      - _rc.filter(function(rc) { return rc.type === 'used'; }).reduce(function(a, rc) { return a + (rc.minutes || 0); }, 0);
              return '<tr class="hover:bg-slate-50 transition-colors">'
                + (isAdmin ? '<td class="td" style="width:36px"><input type="checkbox" class="stu-cb" data-id="' + s.id + '" onchange="App.Students._toggleSelect(\'' + s.id + '\',this.checked)" style="cursor:pointer"' + (_selected[s.id] ? ' checked' : '') + '></td>' : '')
                + '<td class="td"><div class="flex items-center gap-3">'
                +   '<div class="w-9 h-9 rounded-full bg-blue-100 text-blue-700 font-bold text-sm flex items-center justify-center shrink-0">' + s.firstName.charAt(0) + s.lastName.charAt(0) + '</div>'
                +   '<div><div class="font-medium text-slate-800">' + s.firstName + ' ' + s.lastName
                +     (_bal > 0 ? ' <span style="display:inline-block;padding:0.1rem 0.45rem;font-size:0.65rem;font-weight:700;background:#fffbeb;color:#92400e;border:1px solid #fef3c7;border-radius:999px;vertical-align:middle;margin-left:4px" title="Replacement balance">' + _bal + 'm</span>' : '')
                +   '</div><div class="text-xs text-slate-400">' + s.id + '</div></div>'
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
            }).join(''))
      +       '</tbody>'
      +     '</table>'
      +   '</div>'
      +   _paginationControls(_studentPage, filtered.length, 'App.Students._setPage')
      + '</div>';
  }

  function _bulkBar() {
    var count = Object.keys(_selected).length;
    if (count === 0) return '';
    return '<div style="display:flex;align-items:center;gap:0.75rem;padding:0.65rem 1rem;background:var(--gold-dim);border:1px solid rgba(201,162,39,0.25);border-radius:10px;margin-bottom:0.75rem">'
      + '<span style="font-size:0.82rem;font-weight:700;color:#92400e">' + count + ' selected</span>'
      + '<button onclick="App.Students._bulkMessage()" style="padding:0.35rem 0.85rem;font-size:0.75rem;font-weight:600;background:var(--gold);color:#0a0a0a;border:none;border-radius:7px;cursor:pointer">Send Message</button>'
      + '<button onclick="App.Students._bulkDeselect()" style="padding:0.35rem 0.85rem;font-size:0.75rem;font-weight:600;background:transparent;color:#92400e;border:1px solid rgba(201,162,39,0.3);border-radius:7px;cursor:pointer">Clear</button>'
      + '</div>';
  }

  function _toggleSelectAll(checked) {
    document.querySelectorAll('.stu-cb').forEach(function(cb) {
      cb.checked = checked;
      if (checked) {
        _selected[cb.dataset.id] = true;
      } else {
        delete _selected[cb.dataset.id];
      }
    });
    _refreshBulkBar();
  }

  function _toggleSelect(id, checked) {
    if (checked) _selected[id] = true; else delete _selected[id];
    _refreshBulkBar();
  }

  function _refreshBulkBar() {
    var bar = document.getElementById('stu-bulk-bar');
    if (bar) bar.innerHTML = _bulkBar();
  }

  function _bulkDeselect() {
    _selected = {};
    document.querySelectorAll('.stu-cb, #select-all-cb').forEach(function(cb) { cb.checked = false; });
    _refreshBulkBar();
  }

  function _bulkMessage() {
    var ids = Object.keys(_selected);
    if (ids.length === 0) return;
    var { students } = App.Store.get();
    var parents = {};
    students.filter(function(s) { return ids.indexOf(s.id) > -1; }).forEach(function(s) { parents[s.contact] = s.parentName; });
    var parentList = Object.keys(parents);
    var html = '<div class="p-6">'
      + '<h2 class="text-lg font-bold mb-1">Send Message</h2>'
      + '<p class="text-sm text-slate-500 mb-4">Will send to ' + parentList.length + ' parent' + (parentList.length !== 1 ? 's' : '') + '</p>'
      + '<textarea id="bulk-msg-text" rows="4" placeholder="Type your message…" class="w-full border border-slate-200 rounded-xl p-3 text-sm focus:outline-none focus:ring-2 focus:ring-yellow-300 resize-none mb-4"></textarea>'
      + '<div class="flex gap-2 justify-end">'
      + '<button onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg">Cancel</button>'
      + '<button onclick="App.Students._bulkSendMessage()" style="padding:0.45rem 1rem;font-size:0.84rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Send</button>'
      + '</div></div>';
    App.Utils.showModal(html);
  }

  function _bulkSendMessage() {
    var text = document.getElementById('bulk-msg-text').value.trim();
    if (!text) return;
    var ids = Object.keys(_selected);
    var { students, messages } = App.Store.get();
    var parents = {};
    students.filter(function(s) { return ids.indexOf(s.id) > -1; }).forEach(function(s) { parents[s.contact] = true; });
    var newMsgs = Object.keys(parents).map(function(email) {
      return { id: App.Utils.generateId(), fromRole: 'admin', fromLabel: 'Study Hub', toParent: email, text: text, ts: new Date().toISOString(), read: false };
    });
    App.Store.set({ messages: (messages || []).concat(newMsgs) });
    App.Utils.hideModal(true);
    App.Utils.showToast('Message sent to ' + newMsgs.length + ' parent' + (newMsgs.length !== 1 ? 's' : ''), 'success');
    _bulkDeselect();
  }

  function _onSearch(val) { _search = val; _studentPage = 0; App.Router.refresh(); }
  function _onFilter(val) { _statusFilter = val; _studentPage = 0; App.Router.refresh(); }
  function _clearFilters() { _search = ''; _statusFilter = 'All'; _studentPage = 0; App.Router.refresh(); }
  function _setStudentPage(n) { _studentPage = Math.max(0, n); App.Router.refresh(); }

  function _viewModal(studentId) {
    const { students, classes, invoices, selfStudySessions, replacementCredits } = App.Store.get();
    const isAdmin = App.currentRole === 'admin';
    const isTeacher = App.currentRole === 'teacher';
    const s = students.find(function(x) { return x.id === studentId; });
    if (!s) return;

    // Replacement credits for this student
    var stuCredits = (replacementCredits || []).filter(function(rc) { return rc.studentId === studentId; });
    var earnedMin = stuCredits.filter(function(rc) { return rc.type === 'earned'; }).reduce(function(a, rc) { return a + (rc.minutes || 0); }, 0);
    var usedMin = stuCredits.filter(function(rc) { return rc.type === 'used'; }).reduce(function(a, rc) { return a + (rc.minutes || 0); }, 0);
    var balanceMin = earnedMin - usedMin;

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
      + (App.currentRole === 'teacher' ? ['Details','Classes','Replacements'] : ['Details','Classes','Invoices','Replacements']).map(function(tab, i) {
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
      +   (s.siblings && s.siblings.length ? _infoRow('Siblings', (function() {
            var allStudents = App.Store.get().students;
            return s.siblings.map(function(sibId) {
              var sib = allStudents.find(function(x) { return x.id === sibId; });
              return sib ? sib.firstName + ' ' + sib.lastName : sibId;
            }).join(', ');
          })()) : '')
      +   (s.emergency2Name ? _infoRow('Emergency Contact', s.emergency2Name + (s.emergency2Phone ? ' · ' + s.emergency2Phone : '')) : '')
      +   (s.notes ? _infoRow('Notes', '<div style="white-space:pre-wrap">' + App.Utils.esc(s.notes) + '</div>') : '')
      +   '</div>'
      +   '<div style="margin-top:1rem;padding:1rem;background:#fffbeb;border:1px solid #fef3c7;border-radius:12px">'
      +     '<div style="display:flex;align-items:center;gap:0.5rem;margin-bottom:0.65rem">'
      +       '<svg style="width:18px;height:18px;color:#b45309" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M4.26 10.147a60.436 60.436 0 00-.491 6.347A48.627 48.627 0 0112 20.904a48.627 48.627 0 018.232-4.41 60.46 60.46 0 00-.491-6.347m-15.482 0a50.57 50.57 0 00-2.658-.813A59.905 59.905 0 0112 3.493a59.902 59.902 0 0110.399 5.84c-.896.248-1.783.52-2.658.814m-15.482 0A50.697 50.697 0 0112 13.489a50.702 50.702 0 017.74-3.342"/></svg>'
      +       '<span style="font-size:0.78rem;font-weight:700;color:#92400e;text-transform:uppercase;letter-spacing:0.05em">Health Information</span>'
      +     '</div>'
      +     (s.medicalInfo || s.allergies
        ? '<div class="grid grid-cols-1 gap-2 text-sm">'
          + (s.medicalInfo ? '<div><span style="font-weight:600;color:#92400e">Medical Conditions:</span> <span style="color:#78350f">' + App.Utils.esc(s.medicalInfo) + '</span></div>' : '')
          + (s.allergies ? '<div><span style="font-weight:600;color:#92400e">Allergies:</span> <span style="color:#78350f">' + App.Utils.esc(s.allergies) + '</span></div>' : '')
          + '</div>'
        : '<div style="font-size:0.83rem;color:#a1a1aa">No health information recorded</div>')
      +   '</div>'
      +   (App.currentRole === 'teacher'
        ? '<div style="margin-top:1rem;padding:1rem;background:#f8fafc;border:1px solid #e2e8f0;border-radius:12px">'
          + '<div style="font-size:0.72rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.05em;margin-bottom:0.5rem">Quick Note</div>'
          + '<textarea id="teacher-quick-note" rows="2" placeholder="e.g. Had trouble focusing today..." style="width:100%;padding:0.5rem 0.75rem;font-size:0.83rem;border:1px solid #e2e8f0;border-radius:8px;resize:none;outline:none;font-family:inherit"></textarea>'
          + '<button onclick="App.Students._saveQuickNote(\'' + studentId + '\')" style="margin-top:0.5rem;padding:0.4rem 1rem;font-size:0.78rem;font-weight:600;background:var(--gold);color:#0a0a0a;border:none;border-radius:7px;cursor:pointer">Save Note</button>'
          + '</div>'
        : '')
      +   (function() {
            if (!(s.enrolledClasses || []).length) return '';
            const sessions = (selfStudySessions || []).filter(function(ss) { return ss.studentId === studentId; });
            const now2 = new Date();
            const thisMonth2 = now2.getFullYear() + '-' + String(now2.getMonth() + 1).padStart(2,'0');
            const monthMin = sessions.filter(function(ss) { return ss.date.startsWith(thisMonth2); })
              .reduce(function(acc, ss) { return acc + (ss.duration || 0); }, 0);
            const monthHr = monthMin / 60;
            const freeRem = Math.max(0, 4 - monthHr);
            const billable = Math.max(0, monthHr - 4);
            return '<div style="margin-top:1rem;padding:0.85rem 1rem;background:#fffbeb;border:1px solid #fef3c7;border-radius:12px">'
              + '<div style="font-size:0.72rem;font-weight:700;color:#92400e;text-transform:uppercase;letter-spacing:0.05em;margin-bottom:0.5rem">Self Study Membership</div>'
              + '<div style="display:flex;gap:1.5rem;flex-wrap:wrap">'
              +   '<div><div style="font-size:0.7rem;color:#94a3b8">Monthly free</div><div style="font-weight:700;color:#0d0d0d">4 hrs (RM40 off)</div></div>'
              +   '<div><div style="font-size:0.7rem;color:#94a3b8">Used this month</div><div style="font-weight:700;color:#0d0d0d">' + (monthHr < 1 ? monthMin + 'min' : monthHr.toFixed(1) + 'hr') + '</div></div>'
              +   '<div><div style="font-size:0.7rem;color:#94a3b8">Remaining</div><div style="font-weight:700;color:' + (freeRem > 0 ? '#15803d' : '#dc2626') + '">' + freeRem.toFixed(1) + 'hr</div></div>'
              +   (billable > 0 ? '<div><div style="font-size:0.7rem;color:#dc2626">Extra (billable)</div><div style="font-weight:700;color:#dc2626">RM' + (billable * 10).toFixed(0) + '</div></div>' : '')
              + '</div>'
              + '</div>';
          })()
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

      + '<div id="tab-panel-invoices" class="hidden"' + (App.currentRole === 'teacher' ? ' style="display:none"' : '') + '>'
      + '<div class="flex justify-between items-center mb-3"><span class="text-sm text-slate-500">Total paid:</span><span class="font-bold text-emerald-600">' + App.Utils.formatCurrency(totalPaid) + '</span></div>'
      + (studentInvoices.length === 0 ? '<p class="text-sm text-slate-400 text-center py-6">No invoices</p>'
        : '<table class="w-full text-sm"><thead><tr class="border-b"><th class="text-left py-2 text-slate-500 font-medium">Description</th><th class="text-right py-2 text-slate-500 font-medium">Amount</th><th class="text-right py-2 text-slate-500 font-medium">Status</th></tr></thead><tbody>'
          + studentInvoices.map(function(inv) {
              return '<tr class="border-b border-slate-50"><td class="py-2"><div>' + inv.description + '</div><div class="text-xs text-slate-400">Due ' + App.Utils.formatDate(inv.dueDate) + '</div></td><td class="py-2 text-right font-medium">' + App.Utils.formatCurrency(inv.amount) + '</td><td class="py-2 text-right">' + App.Utils.statusBadge(inv.status) + '</td></tr>';
            }).join('')
          + '</tbody></table>')
      + '</div>'

      + '<div id="tab-panel-replacements" class="hidden">'
      + '<div style="display:flex;align-items:center;gap:0.75rem;flex-wrap:wrap;margin-bottom:1rem">'
      +   '<div style="flex:1;min-width:140px;padding:0.85rem 1rem;background:' + (balanceMin > 0 ? '#fffbeb' : '#f8fafc') + ';border:1px solid ' + (balanceMin > 0 ? '#fef3c7' : '#e2e8f0') + ';border-radius:12px;display:flex;align-items:center;gap:0.65rem">'
      +     '<div style="width:36px;height:36px;border-radius:10px;background:' + (balanceMin > 0 ? 'var(--gold-dim)' : '#f1f5f9') + ';display:flex;align-items:center;justify-content:center">'
      +       '<svg width="18" height="18" fill="none" stroke="' + (balanceMin > 0 ? '#b08d20' : '#94a3b8') + '" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6l4 2"/><circle cx="12" cy="12" r="10"/></svg>'
      +     '</div>'
      +     '<div>'
      +       '<div style="font-size:0.7rem;color:#94a3b8;text-transform:uppercase;letter-spacing:0.04em;font-weight:600">Replacement Balance</div>'
      +       '<div style="font-size:1.15rem;font-weight:700;color:' + (balanceMin > 0 ? '#92400e' : '#64748b') + ';font-family:\'Cormorant Garamond\',serif">' + balanceMin + ' min</div>'
      +     '</div>'
      +   '</div>'
      +   ((isAdmin || isTeacher) ? '<div style="display:flex;gap:0.5rem">'
      +     '<button onclick="App.Students._addCreditModal(\'' + studentId + '\')" style="padding:0.45rem 0.85rem;font-size:0.78rem;font-weight:600;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer;white-space:nowrap">Log Absence</button>'
      +     '<button onclick="App.Students._useCreditModal(\'' + studentId + '\')" style="padding:0.45rem 0.85rem;font-size:0.78rem;font-weight:600;background:#f1f5f9;color:#475569;border:1px solid #e2e8f0;border-radius:8px;cursor:pointer;white-space:nowrap">Log Extension</button>'
      +   '</div>' : '')
      + '</div>'
      + (stuCredits.length === 0
        ? '<div style="text-align:center;padding:2rem 1rem;color:#a1a1aa;font-size:0.85rem">No replacements recorded</div>'
        : '<div class="overflow-x-auto"><table class="w-full text-sm"><thead><tr class="border-b border-slate-100">'
          + '<th class="text-left py-2 px-2 text-slate-500 font-medium">Date</th>'
          + '<th class="text-left py-2 px-2 text-slate-500 font-medium">Type</th>'
          + '<th class="text-right py-2 px-2 text-slate-500 font-medium">Minutes</th>'
          + '<th class="text-left py-2 px-2 text-slate-500 font-medium">Note</th>'
          + '<th class="text-left py-2 px-2 text-slate-500 font-medium">Class</th>'
          + ((isAdmin || isTeacher) ? '<th class="text-right py-2 px-2 text-slate-500 font-medium"></th>' : '')
          + '</tr></thead><tbody>'
          + stuCredits.slice().sort(function(a, b) { return b.date < a.date ? -1 : b.date > a.date ? 1 : 0; }).map(function(rc) {
              var cls = rc.classId ? classes.find(function(c) { return c.id === rc.classId; }) : null;
              var typeBg = rc.type === 'earned' ? 'background:#ecfdf5;color:#059669;border:1px solid #a7f3d0' : 'background:#fef2f2;color:#dc2626;border:1px solid #fecaca';
              return '<tr class="border-b border-slate-50">'
                + '<td class="py-2 px-2 text-slate-600">' + App.Utils.formatDate(rc.date) + '</td>'
                + '<td class="py-2 px-2"><span style="display:inline-block;padding:0.15rem 0.55rem;font-size:0.7rem;font-weight:600;border-radius:999px;' + typeBg + '">' + (rc.type === 'earned' ? 'Absent' : 'Extended') + '</span></td>'
                + '<td class="py-2 px-2 text-right font-medium">' + (rc.type === 'earned' ? '+' : String.fromCharCode(8722)) + rc.minutes + '</td>'
                + '<td class="py-2 px-2 text-slate-600">' + App.Utils.esc(rc.note || String.fromCharCode(8212)) + '</td>'
                + '<td class="py-2 px-2 text-slate-500">' + (cls ? App.Utils.esc(cls.name) : String.fromCharCode(8212)) + '</td>'
                + ((isAdmin || isTeacher) ? '<td class="py-2 px-2 text-right"><button onclick="App.Students._deleteCredit(\'' + rc.id + '\',\'' + studentId + '\')" style="font-size:0.7rem;color:#dc2626;background:none;border:none;cursor:pointer;text-decoration:underline">Delete</button></td>' : '')
                + '</tr>';
            }).join('')
          + '</tbody></table></div>')
      + '</div>'

      + '<div class="mt-6 flex justify-end gap-2 border-t border-slate-100 pt-4">'
      + '<button onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Close</button>'
      + (isAdmin ? '<button onclick="App.Students._editModal(\'' + studentId + '\')" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Edit Student</button>' : '')
      + '</div>'
      + '</div>'
    );
  }

  function _editModal(studentId) {
    App.Utils.hideModal(true);
    const state = App.Store.get();
    const s = state.students.find(function(x) { return x.id === studentId; });
    if (!s) return;

    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-4">Edit Student — ' + App.Utils.esc(s.firstName) + ' ' + App.Utils.esc(s.lastName) + '</h2>'
      + '<form id="edit-student-form" class="space-y-4">'
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('First Name', '<input name="firstName" class="form-input" value="' + App.Utils.esc(s.firstName) + '" required>')
      + _field('Last Name', '<input name="lastName" class="form-input" value="' + App.Utils.esc(s.lastName) + '" required>')
      + '</div>'
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Date of Birth', '<input name="dob" type="date" class="form-input" value="' + App.Utils.esc(s.dob) + '">')
      + _field('Gender', '<select name="gender" class="form-input"><option' + (s.gender==='Male'?' selected':'') + '>Male</option><option' + (s.gender==='Female'?' selected':'') + '>Female</option></select>')
      + '</div>'
      + _field('Parent / Guardian Name', '<input name="parentName" class="form-input" value="' + App.Utils.esc(s.parentName) + '">')
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Parent Email', '<input name="contact" type="email" class="form-input" value="' + App.Utils.esc(s.contact) + '">')
      + _field('Phone', '<input name="phone" class="form-input" value="' + App.Utils.esc(s.phone||'') + '">')
      + '</div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Status</label>'
      + '<select name="status" class="form-input">'
      + ['Active','Inactive','New','Waitlisted'].map(function(st) { return '<option' + (s.status===st?' selected':'') + '>' + st + '</option>'; }).join('')
      + '</select></div>'
      + _multiClassField(s.enrolledClasses, state.classes)
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Emergency Contact Name', '<input name="emergency2Name" class="form-input" value="' + App.Utils.esc(s.emergency2Name||'') + '" placeholder="e.g. Uncle David">')
      + _field('Emergency Contact Phone', '<input name="emergency2Phone" class="form-input" value="' + App.Utils.esc(s.emergency2Phone||'') + '" placeholder="60123456789">')
      + '</div>'
      + _field('Medical Conditions', '<textarea name="medicalInfo" class="form-input" rows="2" placeholder="e.g., Asthma, Diabetes">' + App.Utils.esc(s.medicalInfo||'') + '</textarea>')
      + _field('Allergies', '<textarea name="allergies" class="form-input" rows="2" placeholder="e.g., Peanuts, Penicillin">' + App.Utils.esc(s.allergies||'') + '</textarea>')
      + _field('Notes', '<textarea name="notes" class="form-input" rows="2">' + App.Utils.esc(s.notes||'') + '</textarea>')
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
        notes: fd.get('notes'),
        emergency2Name: fd.get('emergency2Name') || '',
        emergency2Phone: fd.get('emergency2Phone') || '',
        medicalInfo: fd.get('medicalInfo') || '',
        allergies: fd.get('allergies') || ''
      });

      App.Store.set({ students: st.students.map(function(x) { return x.id === studentId ? updated : x; }), classes: newClasses2 });
      App.Utils.hideModal(true);
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
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Emergency Contact Name', '<input name="emergency2Name" class="form-input" placeholder="e.g. Uncle David">')
      + _field('Emergency Contact Phone', '<input name="emergency2Phone" class="form-input" placeholder="60123456789">')
      + '</div>'
      + _field('Medical Conditions', '<textarea name="medicalInfo" class="form-input" rows="2" placeholder="e.g., Asthma, Diabetes"></textarea>')
      + _field('Allergies', '<textarea name="allergies" class="form-input" rows="2" placeholder="e.g., Peanuts, Penicillin"></textarea>')
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
      const newId = App.Utils.generateId('STU');
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
        notes: '',
        emergency2Name: fd.get('emergency2Name') || '',
        emergency2Phone: fd.get('emergency2Phone') || '',
        medicalInfo: fd.get('medicalInfo') || '',
        allergies: fd.get('allergies') || ''
      };
      const newClasses = state.classes.map(function(c) {
        return selectedClasses.indexOf(c.id) > -1 ? Object.assign({}, c, { enrolled: c.enrolled + 1 }) : c;
      });
      App.Store.set({ students: [...state.students, newStudent], classes: newClasses });
      App.Utils.hideModal(true);
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
    ['details','classes','invoices','replacements'].forEach(function(t) {
      var panel = document.getElementById('tab-panel-' + t);
      if (panel) panel.classList.toggle('hidden', t !== tab);
      const btn = document.getElementById('tab-' + t);
      if (!btn) return;
      if (t === tab) { btn.classList.add('border-b-2','border-blue-600','text-blue-600'); btn.classList.remove('text-slate-500'); }
      else { btn.classList.remove('border-b-2','border-blue-600','text-blue-600'); btn.classList.add('text-slate-500'); }
    });
  }

  function _pendingModal() {
    const { registrations } = App.Store.get();
    const pending = (registrations || []).filter(function(r) { return r.status === 'pending'; });

    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-1">Pending Registrations</h2>'
      + '<p class="text-sm text-slate-500 mb-5">' + pending.length + ' application' + (pending.length !== 1 ? 's' : '') + ' awaiting review</p>'
      + (pending.length === 0
          ? '<div class="py-8 text-center text-slate-400">No pending registrations</div>'
          : '<div class="space-y-4 max-h-[60vh] overflow-y-auto pr-1">'
          + pending.map(function(reg) {
              return '<div class="border border-slate-200 rounded-xl p-4">'
                + '<div class="flex items-start justify-between gap-3 mb-3">'
                +   '<div>'
                +     '<div class="font-semibold text-slate-800">' + App.Utils.esc(reg.studentFirstName) + ' ' + App.Utils.esc(reg.studentLastName) + '</div>'
                +     '<div class="text-xs text-slate-500 mt-0.5">Parent: ' + App.Utils.esc(reg.parentName) + ' · ' + App.Utils.esc(reg.email) + '</div>'
                +   '</div>'
                +   '<span class="text-xs text-slate-400 shrink-0">' + App.Utils.formatDate(reg.submittedOn) + '</span>'
                + '</div>'
                + '<div class="grid grid-cols-2 gap-2 text-xs text-slate-600 mb-3">'
                + (reg.phone ? '<div><span class="text-slate-400">Phone:</span> ' + App.Utils.esc(reg.phone) + '</div>' : '')
                + (reg.studentDob ? '<div><span class="text-slate-400">DOB:</span> ' + App.Utils.formatDate(reg.studentDob) + '</div>' : '')
                + (reg.studentGender ? '<div><span class="text-slate-400">Gender:</span> ' + App.Utils.esc(reg.studentGender) + '</div>' : '')
                + (reg.classInterest ? '<div class="col-span-2"><span class="text-slate-400">Interested in:</span> ' + App.Utils.esc(reg.classInterest) + '</div>' : '')
                + (reg.emergencyName ? '<div class="col-span-2"><span class="text-slate-400">Emergency:</span> ' + App.Utils.esc(reg.emergencyName) + ' · ' + App.Utils.esc(reg.emergencyPhone) + '</div>' : '')
                + (reg.notes ? '<div class="col-span-2"><span class="text-slate-400">Notes:</span> ' + App.Utils.esc(reg.notes) + '</div>' : '')
                + '</div>'
                + '<div class="flex gap-2">'
                +   '<button onclick="App.Students._approveReg(\'' + reg.id + '\')" class="flex-1 py-1.5 text-sm bg-emerald-500 text-white rounded-lg hover:bg-emerald-600 font-medium">Approve</button>'
                +   '<button onclick="App.Students._rejectReg(\'' + reg.id + '\')" class="flex-1 py-1.5 text-sm bg-red-50 text-red-600 rounded-lg hover:bg-red-100 font-medium border border-red-200">Reject</button>'
                + '</div>'
                + '</div>';
            }).join('')
          + '</div>')
      + '<div class="mt-4 flex justify-end"><button onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Close</button></div>'
      + '</div>'
    );
  }

  async function _approveReg(regId) {
    try {
      const result = await App.Api.post('/api/registrations/' + regId + '/approve', {});
      if (result) {
        App.Utils.hideModal(true);
        App.Utils.showToast('Approved! Temp password: ' + result.tempPassword, 'success', 15000);
        await App.Api.loadSnapshot();
        App.Notifs.refresh();
        App.Router.refresh();
      }
    } catch(err) {
      App.Utils.showToast(err.message || 'Approval failed', 'error');
    }
  }

  async function _rejectReg(regId) {
    if (!confirm('Reject this registration?')) return;
    await App.Api.del('/api/registrations/' + regId);
    await App.Api.loadSnapshot();
    App.Notifs.refresh();
    App.Utils.hideModal(true);
    App.Utils.showToast('Registration rejected', 'info');
    App.Router.refresh();
  }

  function _saveQuickNote(studentId) {
    var textarea = document.getElementById('teacher-quick-note');
    if (!textarea) return;
    var text = textarea.value.trim();
    if (!text) { App.Utils.showToast('Please enter a note', 'error'); return; }
    var state = App.Store.get();
    var now = new Date();
    var months = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
    var prefix = '[' + months[now.getMonth()] + ' ' + now.getDate() + '] ';
    var newNote = prefix + text;
    App.Store.set({ students: state.students.map(function(s) {
      if (s.id !== studentId) return s;
      var existing = s.notes ? s.notes.trim() : '';
      return Object.assign({}, s, { notes: existing ? existing + '\n' + newNote : newNote });
    })});
    textarea.value = '';
    App.Utils.showToast('Note saved', 'success');
  }

  function _infoRow(label, value) {
    return '<div class="bg-slate-50 rounded-lg p-3"><div class="text-xs text-slate-400 mb-0.5">' + label + '</div><div class="font-medium text-slate-700">' + value + '</div></div>';
  }
  function _field(label, inputHtml) {
    return '<div><label class="block text-sm font-medium text-slate-700 mb-1">' + label + '</label>' + inputHtml + '</div>';
  }

  function _downloadCSV(csv, filename) {
    var blob = new Blob([csv], { type: 'text/csv' });
    var url = URL.createObjectURL(blob);
    var a = document.createElement('a');
    a.href = url; a.download = filename; a.click();
    URL.revokeObjectURL(url);
  }

  function _exportCSV() {
    const { students, classes } = App.Store.get();
    const headers = ['ID','First Name','Last Name','DOB','Gender','Parent','Contact','Phone','Status','Registered On','Classes','Medical Info','Allergies'];
    const rows = students.map(function(s) {
      const classNames = s.enrolledClasses.map(function(cid) {
        var c = classes.find(function(x) { return x.id === cid; });
        return c ? c.name : cid;
      }).join('; ');
      return [s.id, s.firstName, s.lastName, s.dob, s.gender, s.parentName, s.contact, s.phone, s.status, s.registeredOn, classNames, s.medicalInfo||'', s.allergies||'']
        .map(function(v) { return '"' + String(v||'').replace(/"/g,'""') + '"'; }).join(',');
    });
    _downloadCSV([headers.join(',')].concat(rows).join('\n'), 'students.csv');
    App.Utils.showToast('Exported ' + students.length + ' students', 'success');
  }

  function _addCreditModal(studentId) {
    var today = App.Utils.today ? App.Utils.today() : new Date().toISOString().slice(0, 10);
    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-lg font-bold mb-1">Log Absence</h2>'
      + '<p class="text-sm text-slate-500 mb-4">Record an absence for a missed class (default 60 min)</p>'
      + '<form id="add-credit-form" class="space-y-4">'
      + _field('Minutes', '<select name="minutes" class="form-input"><option value="15">15 min</option><option value="30">30 min</option><option value="45">45 min</option><option value="60" selected>60 min</option></select>')
      + _field('Note', '<input name="note" class="form-input" placeholder="e.g. Absent from Math 12 Mar">')
      + _field('Date', '<input name="date" type="date" class="form-input" value="' + today + '" required>')
      + '<div class="flex justify-end gap-2 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" style="padding:0.45rem 1rem;font-size:0.84rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Log Absence</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );
    document.getElementById('add-credit-form').addEventListener('submit', async function(e) {
      e.preventDefault();
      var fd = new FormData(e.target);
      try {
        await App.Api.post('/api/replacement-credits', {
          studentId: studentId,
          type: 'earned',
          minutes: parseInt(fd.get('minutes'), 10),
          note: fd.get('note') || '',
          date: fd.get('date')
        });
        App.Utils.hideModal(true);
        App.Utils.showToast('Replacement added', 'success');
        await App.Api.refresh();
        _viewModal(studentId);
        _switchTab('replacements');
      } catch(err) {
        App.Utils.showToast(err.message || 'Failed to add replacement', 'error');
      }
    });
  }

  function _useCreditModal(studentId) {
    var today = App.Utils.today ? App.Utils.today() : new Date().toISOString().slice(0, 10);
    var state = App.Store.get();
    var stuClasses = (state.students.find(function(x) { return x.id === studentId; }) || {}).enrolledClasses || [];
    var classOpts = '<option value="">-- none --</option>'
      + state.classes.map(function(c) {
          return '<option value="' + c.id + '">' + App.Utils.esc(c.name) + '</option>';
        }).join('');
    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-lg font-bold mb-1">Log Extension</h2>'
      + '<p class="text-sm text-slate-500 mb-4">Use replacement balance as a class extension (15/30/45/60 min)</p>'
      + '<form id="use-credit-form" class="space-y-4">'
      + _field('Minutes', '<select name="minutes" class="form-input"><option value="15">15 min</option><option value="30">30 min</option><option value="45">45 min</option><option value="60">60 min</option></select>')
      + _field('Class (optional)', '<select name="classId" class="form-input">' + classOpts + '</select>')
      + _field('Note', '<input name="note" class="form-input" placeholder="e.g. Extended English session">')
      + _field('Date', '<input name="date" type="date" class="form-input" value="' + today + '" required>')
      + '<div class="flex justify-end gap-2 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" style="padding:0.45rem 1rem;font-size:0.84rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Log Extension</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );
    document.getElementById('use-credit-form').addEventListener('submit', async function(e) {
      e.preventDefault();
      var fd = new FormData(e.target);
      var mins = parseInt(fd.get('minutes'), 10);
      // Check balance
      var creds = (App.Store.get().replacementCredits || []).filter(function(rc) { return rc.studentId === studentId; });
      var bal = creds.filter(function(rc) { return rc.type === 'earned'; }).reduce(function(a, rc) { return a + (rc.minutes || 0); }, 0)
              - creds.filter(function(rc) { return rc.type === 'used'; }).reduce(function(a, rc) { return a + (rc.minutes || 0); }, 0);
      if (mins > bal) {
        App.Utils.showToast('Insufficient replacement balance (' + bal + ' min available)', 'error');
        return;
      }
      try {
        await App.Api.post('/api/replacement-credits', {
          studentId: studentId,
          type: 'used',
          minutes: mins,
          classId: fd.get('classId') || '',
          note: fd.get('note') || '',
          date: fd.get('date')
        });
        App.Utils.hideModal(true);
        App.Utils.showToast('Replacement used (' + mins + ' min)', 'success');
        await App.Api.refresh();
        _viewModal(studentId);
        _switchTab('replacements');
      } catch(err) {
        App.Utils.showToast(err.message || 'Failed to use replacement', 'error');
      }
    });
  }

  async function _deleteCredit(creditId, studentId) {
    if (!confirm('Delete this replacement entry?')) return;
    try {
      await App.Api.del('/api/replacement-credits/' + creditId);
      App.Utils.showToast('Replacement entry deleted', 'info');
      await App.Api.refresh();
      _viewModal(studentId);
      _switchTab('replacements');
    } catch(err) {
      App.Utils.showToast(err.message || 'Failed to delete', 'error');
    }
  }

  App.Students = {
    render: render,
    _onSearch: _onSearch,
    _onFilter: _onFilter,
    _viewModal: _viewModal,
    _editModal: _editModal,
    _switchTab: _switchTab,
    _addModal: _addModal,
    _pendingModal: _pendingModal,
    _approveReg: _approveReg,
    _rejectReg: _rejectReg,
    _toggleSelectAll: _toggleSelectAll,
    _toggleSelect: _toggleSelect,
    _bulkDeselect: _bulkDeselect,
    _bulkMessage: _bulkMessage,
    _bulkSendMessage: _bulkSendMessage,
    _clearFilters: _clearFilters,
    _exportCSV: _exportCSV,
    _setPage: _setStudentPage,
    _saveQuickNote: _saveQuickNote,
    _addCreditModal: _addCreditModal,
    _useCreditModal: _useCreditModal,
    _deleteCredit: _deleteCredit
  };
})();
