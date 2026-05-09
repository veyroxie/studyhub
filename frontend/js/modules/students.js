(function() {
  window.App = window.App || {};

  let _search = '';
  let _statusFilter = 'All';
  let _selected = {};
  let _studentPage = 0;
  var _PAGE_SIZE = 15;
  let _studentsTab = 'families'; // default to families for admin
  let _familySearch = '';

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

    // Parents always see students list, not families
    if (isClient) _studentsTab = 'students';

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

    const colCount = isAdmin ? 6 : 5;

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

      + ((isAdmin || isTeacher) ? '<div style="display:flex;gap:0.25rem;background:#f1f5f9;border-radius:8px;padding:3px;margin-bottom:1rem;width:fit-content">'
      + '<button onclick="App.Students._setTab(\'students\')" style="padding:0.35rem 1rem;font-size:0.78rem;font-weight:600;border:none;border-radius:6px;cursor:pointer;background:' + (_studentsTab==='students'?'var(--gold)':'transparent') + ';color:' + (_studentsTab==='students'?'#0a0a0a':'#94a3b8') + '">Students</button>'
      + '<button onclick="App.Students._setTab(\'families\')" style="padding:0.35rem 1rem;font-size:0.78rem;font-weight:600;border:none;border-radius:6px;cursor:pointer;background:' + (_studentsTab==='families'?'var(--gold)':'transparent') + ';color:' + (_studentsTab==='families'?'#0a0a0a':'#94a3b8') + '">Families</button>'
      + '</div>' : '');

    if (_studentsTab === 'families' && (isAdmin || isTeacher)) {
      container.innerHTML += _familiesView();
      return;
    }

    container.innerHTML += '<div class="grid grid-cols-5 gap-4 mb-6">'
      + ['Total','Active','Inactive','New','Waitlisted'].map(function(k) {
          const colors = { Total:'text-blue-600', Active:'text-emerald-600', Inactive:'text-red-500', New:'text-blue-500', Waitlisted:'text-amber-500' };
          var filterVal = (k === 'Total') ? 'All' : k;
          var isActive = (filterVal === _statusFilter);
          var activeStyle = isActive ? 'border-color:var(--gold);box-shadow:0 0 0 2px var(--gold-dim, rgba(201,162,39,0.18));' : '';
          return '<div onclick="App.Students._onFilter(\'' + filterVal + '\')" class="bg-white rounded-xl border border-slate-100 shadow-sm p-4 text-center cursor-pointer hover:border-slate-200 transition-colors" style="' + activeStyle + '">'
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
      +         '<th scope="col" class="th">Parent / Contact</th><th scope="col" class="th">Status</th>'
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
              var rowSubStatus = s.subscriptionStatus || 'active';
              var rowSubChip = '';
              if (rowSubStatus === 'paused') rowSubChip = ' <span style="display:inline-block;padding:0.1rem 0.45rem;font-size:0.6rem;font-weight:700;background:#fef3c7;color:#92400e;border:1px solid #fde68a;border-radius:999px;vertical-align:middle;margin-left:4px">Paused</span>';
              else if (rowSubStatus === 'frozen') rowSubChip = ' <span style="display:inline-block;padding:0.1rem 0.45rem;font-size:0.6rem;font-weight:700;background:#dbeafe;color:#1e40af;border:1px solid #bfdbfe;border-radius:999px;vertical-align:middle;margin-left:4px">Frozen</span>';
              return '<tr onclick="App.Students._viewModal(\'' + s.id + '\')" class="hover:bg-slate-50 transition-colors cursor-pointer">'
                + (isAdmin ? '<td class="td" style="width:36px" onclick="event.stopPropagation()"><input type="checkbox" class="stu-cb" data-id="' + s.id + '" onchange="App.Students._toggleSelect(\'' + s.id + '\',this.checked)" style="cursor:pointer"' + (_selected[s.id] ? ' checked' : '') + '></td>' : '')
                + '<td class="td"><div class="flex items-center gap-3">'
                +   '<div class="w-9 h-9 rounded-full bg-blue-100 text-blue-700 font-bold text-sm flex items-center justify-center shrink-0">' + App.Utils.esc(s.firstName.charAt(0)) + App.Utils.esc(s.lastName.charAt(0)) + '</div>'
                +   '<div><div class="font-medium text-slate-800">' + App.Utils.esc(s.firstName) + ' ' + App.Utils.esc(s.lastName)
                +     (_bal > 0 ? ' <span style="display:inline-block;padding:0.1rem 0.45rem;font-size:0.65rem;font-weight:700;background:#fffbeb;color:#92400e;border:1px solid #fef3c7;border-radius:999px;vertical-align:middle;margin-left:4px" title="Replacement balance">' + _bal + 'cr</span>' : '')
                +     rowSubChip
                +   '</div><div class="text-xs text-slate-400">' + s.id + '</div></div>'
                + '</div></td>'
                + '<td class="td"><div class="flex flex-wrap gap-1">'
                + (enrolledNames.length === 0 ? '<span class="text-xs text-slate-400">—</span>'
                  : enrolledNames.map(function(n) { return '<span class="text-xs px-2 py-0.5 bg-blue-50 text-blue-700 rounded-full border border-blue-100">' + App.Utils.esc(n) + '</span>'; }).join(''))
                + '</div></td>'
                + '<td class="td text-sm text-slate-600">' + App.Utils.formatDate(s.dob) + '</td>'
                + '<td class="td text-sm"><div class="text-slate-700">' + App.Utils.esc(s.parentName) + '</div><div class="text-slate-400 text-xs">' + App.Utils.esc(s.contact) + '</div></td>'
                + '<td class="td">' + App.Utils.statusBadge(s.status) + '</td>'
                + '</tr>';
            }).join(''))
      +       '</tbody>'
      +     '</table>'
      +   '</div>'
      +   _paginationControls(_studentPage, filtered.length, 'App.Students._setPage')
      + '</div>';

    if (_focusSearchAfterRender) {
      _focusSearchAfterRender = false;
      setTimeout(function() {
        var input = document.getElementById('student-search');
        if (input) {
          input.focus();
          var len = input.value.length;
          try { input.setSelectionRange(len, len); } catch(e) {}
        }
      }, 0);
    }
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

  let _searchTimer = null;
  let _focusSearchAfterRender = false;
  async function _subscriptionAction(studentId, action) {
    var label = { pause:'pause', resume:'resume', freeze:'freeze' }[action] || action;
    var ok = await App.Utils.showConfirm({
      title: label.charAt(0).toUpperCase() + label.slice(1) + ' subscription',
      message: action === 'resume'
        ? 'Resume monthly invoicing and re-include this student in class rosters?'
        : 'Stop monthly invoice generation. The student will be hidden from class rosters until resumed.',
      confirmLabel: label.charAt(0).toUpperCase() + label.slice(1),
      danger: action !== 'resume'
    });
    if (!ok) return;
    try {
      var res = await App.Api.post('/api/students/' + studentId + '/subscription', { action: action });
      var state = App.Store.get();
      App.Store.set({ students: state.students.map(function(s) {
        return s.id === studentId ? Object.assign({}, s, { subscriptionStatus: res.subscriptionStatus }) : s;
      }) });
      App.Utils.showToast('Subscription ' + label + 'd', 'success');
      App.Router.refresh();
      _viewModal(studentId);
    } catch (err) {
      App.Utils.showToast(err.message || 'Could not update subscription', 'error');
    }
  }

  function _onSearch(val) {
    _search = val;
    _studentPage = 0;
    _focusSearchAfterRender = true;
    if (_searchTimer) clearTimeout(_searchTimer);
    _searchTimer = setTimeout(function() { App.Router.refresh(); }, 250);
  }
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
    var classCreds = stuCredits.filter(function(rc) { return (rc.category || 'class') === 'class'; });
    var ssCreds = stuCredits.filter(function(rc) { return rc.category === 'self-study'; });
    var classBalance = classCreds.filter(function(rc) { return rc.type === 'earned'; }).reduce(function(a, rc) { return a + (rc.minutes || 0); }, 0)
                     - classCreds.filter(function(rc) { return rc.type === 'used'; }).reduce(function(a, rc) { return a + (rc.minutes || 0); }, 0);
    var ssBalance = ssCreds.filter(function(rc) { return rc.type === 'earned'; }).reduce(function(a, rc) { return a + (rc.minutes || 0); }, 0)
                  - ssCreds.filter(function(rc) { return rc.type === 'used'; }).reduce(function(a, rc) { return a + (rc.minutes || 0); }, 0);
    var balanceMin = classBalance + ssBalance;

    const enrolledClasses = s.enrolledClasses.map(function(cid) {
      return classes.find(function(c) { return c.id === cid; });
    }).filter(Boolean);

    const studentInvoices = invoices.filter(function(inv) { return inv.studentId === studentId; });
    const totalPaid = studentInvoices.filter(function(i) { return i.status === 'Paid'; }).reduce(function(s, i) { return s + i.amount; }, 0);

    var subStatus = s.subscriptionStatus || 'active';
    var subChip = '';
    if (subStatus === 'paused') {
      subChip = '<span style="display:inline-block;padding:0.15rem 0.55rem;font-size:0.65rem;font-weight:700;background:#fef3c7;color:#92400e;border:1px solid #fde68a;border-radius:999px;margin-left:6px">Paused</span>';
    } else if (subStatus === 'frozen') {
      subChip = '<span style="display:inline-block;padding:0.15rem 0.55rem;font-size:0.65rem;font-weight:700;background:#dbeafe;color:#1e40af;border:1px solid #bfdbfe;border-radius:999px;margin-left:6px">Frozen</span>';
    }

    App.Utils.showModal(
      '<div class="p-6">'
      + '<div class="flex items-center gap-4 mb-6">'
      +   '<div class="w-16 h-16 rounded-2xl bg-blue-100 text-blue-700 font-bold text-2xl flex items-center justify-center">' + App.Utils.esc(s.firstName.charAt(0)) + App.Utils.esc(s.lastName.charAt(0)) + '</div>'
      +   '<div>'
      +     '<h2 class="text-xl font-bold text-slate-800">' + App.Utils.esc(s.firstName) + ' ' + App.Utils.esc(s.lastName) + subChip + '</h2>'
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
      +   _infoRow('Gender', App.Utils.esc(s.gender))
      +   (isTeacher ? '' : _infoRow('Parent / Guardian', App.Utils.esc(s.parentName)))
      +   (isTeacher ? '' : _infoRow('Email', App.Utils.esc(s.contact)))
      +   (isTeacher ? '' : _infoRow('Phone', App.Utils.esc(s.phone)))
      +   _infoRow('Branch', App.Utils.esc(s.branch))
      +   (isTeacher ? '' : (function() {
            var fam = (App.Store.get().families || []).find(function(x) { return x.id === s.familyId; });
            return fam ? _infoRow('Family', '<a href="#" onclick="event.preventDefault();App.Utils.hideModal(true);App.Students._familyModal(\'' + fam.id + '\')" style="color:var(--gold);font-weight:600;text-decoration:none">' + App.Utils.esc(fam.name) + '</a>') : '';
          })())
      +   (isTeacher ? '' : (function() {
            if (!s.referredByFamilyId) return '';
            var refFam = (App.Store.get().families || []).find(function(x) { return x.id === s.referredByFamilyId; });
            if (!refFam) return _infoRow('Referred By', '<span style="color:#94a3b8">—</span>');
            return _infoRow('Referred By', '<a href="#" onclick="event.preventDefault();App.Utils.hideModal(true);App.Students._familyModal(\'' + refFam.id + '\')" style="color:var(--gold);font-weight:600;text-decoration:none">' + App.Utils.esc(refFam.name) + '</a>');
          })())
      +   _infoRow('Registered On', App.Utils.formatDate(s.registeredOn))
      +   (!isTeacher && s.siblings && s.siblings.length ? _infoRow('Siblings', (function() {
            var allStudents = App.Store.get().students;
            return s.siblings.map(function(sibId) {
              var sib = allStudents.find(function(x) { return x.id === sibId; });
              return sib ? App.Utils.esc(sib.firstName + ' ' + sib.lastName) : sibId;
            }).join(', ');
          })()) : '')
      +   (!isTeacher && s.emergency2Name ? _infoRow('Emergency Contact', App.Utils.esc(s.emergency2Name) + (s.emergency2Phone ? ' · ' + App.Utils.esc(s.emergency2Phone) : '')) : '')
      +   (!isTeacher && s.notes ? _infoRow('Notes', '<div style="white-space:pre-wrap">' + App.Utils.esc(s.notes) + '</div>') : '')
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
      +   (isAdmin ? (function() {
            var pkgAmt = s.packageAmount || 0;
            var pkgHrs = (s.packageSelfStudyHours == null) ? 4 : s.packageSelfStudyHours;
            var paused = subStatus !== 'active';
            return '<div style="margin-top:1rem;padding:1rem;background:#fff;border:1px solid #e2e8f0;border-radius:12px">'
              + '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.65rem">'
              +   '<div style="font-size:0.72rem;font-weight:700;color:#475569;text-transform:uppercase;letter-spacing:0.05em">Subscription</div>'
              +   '<span style="font-size:0.7rem;font-weight:700;color:' + (paused ? '#92400e' : '#15803d') + '">' + (paused ? subStatus.toUpperCase() : 'ACTIVE') + '</span>'
              + '</div>'
              + '<div style="display:grid;grid-template-columns:1fr 1fr;gap:0.5rem;font-size:0.83rem;color:#374151;margin-bottom:0.85rem">'
              +   '<div>Package: <strong>RM ' + pkgAmt.toFixed(2) + '/mo</strong></div>'
              +   '<div>Self-study included: <strong>' + pkgHrs + ' hrs</strong></div>'
              + '</div>'
              + '<div style="display:flex;gap:0.5rem">'
              + (paused
                  ? '<button onclick="App.Students._subscriptionAction(\'' + studentId + '\',\'resume\')" style="padding:0.45rem 0.95rem;font-size:0.78rem;font-weight:700;background:#22c55e;color:#fff;border:none;border-radius:8px;cursor:pointer">Resume</button>'
                  : '<button onclick="App.Students._subscriptionAction(\'' + studentId + '\',\'pause\')" style="padding:0.45rem 0.95rem;font-size:0.78rem;font-weight:700;background:#fef3c7;color:#92400e;border:1px solid #fde68a;border-radius:8px;cursor:pointer">Pause</button>'
                  + '<button onclick="App.Students._subscriptionAction(\'' + studentId + '\',\'freeze\')" style="padding:0.45rem 0.95rem;font-size:0.78rem;font-weight:700;background:#dbeafe;color:#1e40af;border:1px solid #bfdbfe;border-radius:8px;cursor:pointer">Freeze</button>')
              + '</div>'
              + '</div>';
          })() : '')
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
            + '<div class="font-semibold ' + colors.text + '">' + App.Utils.esc(c.name) + '</div>'
            + '<div class="text-sm text-slate-600 mt-1">' + c.day + ' · ' + App.Utils.formatTime(c.time) + ' – ' + App.Utils.formatTime(c.endTime) + '</div>'
            + '<div class="text-sm text-slate-500">' + App.Utils.esc(teachers) + ' · ' + App.Utils.esc(c.classroom) + '</div>'
            + '</div>';
        }).join('')
      + '</div>'

      + '<div id="tab-panel-invoices" class="hidden"' + (App.currentRole === 'teacher' ? ' style="display:none"' : '') + '>'
      + '<div class="flex justify-between items-center mb-3"><span class="text-sm text-slate-500">Total paid:</span><span class="font-bold text-emerald-600">' + App.Utils.formatCurrency(totalPaid) + '</span></div>'
      + (studentInvoices.length === 0 ? '<p class="text-sm text-slate-400 text-center py-6">No invoices</p>'
        : '<table class="w-full text-sm"><thead><tr class="border-b"><th class="text-left py-2 text-slate-500 font-medium">Description</th><th class="text-right py-2 text-slate-500 font-medium">Amount</th><th class="text-right py-2 text-slate-500 font-medium">Status</th></tr></thead><tbody>'
          + studentInvoices.map(function(inv) {
              return '<tr class="border-b border-slate-50"><td class="py-2"><div>' + App.Utils.esc(inv.description) + '</div><div class="text-xs text-slate-400">Due ' + App.Utils.formatDate(inv.dueDate) + '</div></td><td class="py-2 text-right font-medium">' + App.Utils.formatCurrency(inv.amount) + '</td><td class="py-2 text-right">' + App.Utils.statusBadge(inv.status) + '</td></tr>';
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
      +       '<div style="font-size:0.7rem;color:#94a3b8;text-transform:uppercase;letter-spacing:0.04em;font-weight:600">Credits</div>'
      +       '<div style="font-size:0.92rem;font-weight:700;color:' + (balanceMin > 0 ? '#92400e' : '#64748b') + ';font-family:\'Cormorant Garamond\',serif">Class: ' + classBalance + ' | Self-study: ' + ssBalance + '</div>'
      +     '</div>'
      +   '</div>'
      +   ((isAdmin || isTeacher) ? '<div style="display:flex;gap:0.5rem">'
      +     '<button onclick="App.Students._addCreditModal(\'' + studentId + '\')" style="padding:0.45rem 0.85rem;font-size:0.78rem;font-weight:600;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer;white-space:nowrap">Mark absent</button>'
      +     '<button onclick="App.Students._useCreditModal(\'' + studentId + '\')" style="padding:0.45rem 0.85rem;font-size:0.78rem;font-weight:600;background:#f1f5f9;color:#475569;border:1px solid #e2e8f0;border-radius:8px;cursor:pointer;white-space:nowrap">Use credit</button>'
      +   '</div>' : '')
      + '</div>'
      + (stuCredits.length === 0
        ? '<div style="text-align:center;padding:2rem 1rem;color:#a1a1aa;font-size:0.85rem">No replacements recorded</div>'
        : '<div class="overflow-x-auto"><table class="w-full text-sm"><thead><tr class="border-b border-slate-100">'
          + '<th class="text-left py-2 px-2 text-slate-500 font-medium">Date</th>'
          + '<th class="text-left py-2 px-2 text-slate-500 font-medium">Type</th>'
          + '<th class="text-left py-2 px-2 text-slate-500 font-medium">Category</th>'
          + '<th class="text-right py-2 px-2 text-slate-500 font-medium">Credits</th>'
          + '<th class="text-left py-2 px-2 text-slate-500 font-medium">Note</th>'
          + '<th class="text-left py-2 px-2 text-slate-500 font-medium">Class</th>'
          + ((isAdmin || isTeacher) ? '<th class="text-right py-2 px-2 text-slate-500 font-medium"></th>' : '')
          + '</tr></thead><tbody>'
          + stuCredits.slice().sort(function(a, b) { return b.date < a.date ? -1 : b.date > a.date ? 1 : 0; }).map(function(rc) {
              var cls = rc.classId ? classes.find(function(c) { return c.id === rc.classId; }) : null;
              var typeBg = rc.type === 'earned' ? 'background:#ecfdf5;color:#059669;border:1px solid #a7f3d0' : 'background:#fef2f2;color:#dc2626;border:1px solid #fecaca';
              var catLabel = (rc.category || 'class') === 'self-study' ? 'Self-study' : 'Class';
              var catBg = catLabel === 'Self-study' ? 'background:#eff6ff;color:#2563eb;border:1px solid #bfdbfe' : 'background:#faf5ff;color:#7c3aed;border:1px solid #e9d5ff';
              return '<tr class="border-b border-slate-50">'
                + '<td class="py-2 px-2 text-slate-600">' + App.Utils.formatDate(rc.date) + '</td>'
                + '<td class="py-2 px-2"><span style="display:inline-block;padding:0.15rem 0.55rem;font-size:0.7rem;font-weight:600;border-radius:999px;' + typeBg + '">' + (rc.type === 'earned' ? 'Absent' : 'Extended') + '</span></td>'
                + '<td class="py-2 px-2"><span style="display:inline-block;padding:0.15rem 0.55rem;font-size:0.7rem;font-weight:600;border-radius:999px;' + catBg + '">' + catLabel + '</span></td>'
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
      + _field('Monthly Package (RM)', '<input name="packageAmount" type="number" step="0.01" min="0" class="form-input" value="' + (s.packageAmount || 0) + '">')
      + _field('Self-study hours included', '<input name="packageSelfStudyHours" type="number" min="0" class="form-input" value="' + (s.packageSelfStudyHours == null ? 4 : s.packageSelfStudyHours) + '">')
      + '</div>'
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
        allergies: fd.get('allergies') || '',
        packageAmount: parseFloat(fd.get('packageAmount')) || 0,
        packageSelfStudyHours: parseInt(fd.get('packageSelfStudyHours'), 10) || 4
      });

      App.Store.set({ students: st.students.map(function(x) { return x.id === studentId ? updated : x; }), classes: newClasses2 });
      App.Utils.hideModal(true);
      App.Utils.showToast(App.Utils.esc(updated.firstName) + ' ' + App.Utils.esc(updated.lastName) + ' updated', 'success');
      App.Router.refresh();
    });
  }

  function _addModal() {
    const { classes } = App.Store.get();
    var suggestedId = App.Utils.generateId('STU');
    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-4">Add New Student</h2>'
      + '<form id="add-student-form" class="space-y-4">'
      + _field('Student # <span class="text-slate-400 font-normal">(auto-generated, editable)</span>', '<input name="id" class="form-input" value="' + suggestedId + '" required>')
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
      + _field('Monthly Package (RM)', '<input name="packageAmount" type="number" step="0.01" min="0" class="form-input" value="0" placeholder="e.g. 380">')
      + _field('Self-study hours included', '<input name="packageSelfStudyHours" type="number" min="0" class="form-input" value="4">')
      + '</div>'
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
      const submittedId = (fd.get('id') || '').trim();
      const newId = submittedId || App.Utils.generateId('STU');
      // Reject duplicate within the local store
      if (state.students.some(function(s) { return s.id === newId; })) {
        App.Utils.showToast('Student # "' + newId + '" already in use', 'error');
        return;
      }
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
        allergies: fd.get('allergies') || '',
        packageAmount: parseFloat(fd.get('packageAmount')) || 0,
        packageSelfStudyHours: parseInt(fd.get('packageSelfStudyHours'), 10) || 4,
        subscriptionStatus: 'active'
      };
      const newClasses = state.classes.map(function(c) {
        return selectedClasses.indexOf(c.id) > -1 ? Object.assign({}, c, { enrolled: c.enrolled + 1 }) : c;
      });
      App.Store.set({ students: [...state.students, newStudent], classes: newClasses });
      App.Utils.hideModal(true);
      App.Utils.showToast(App.Utils.esc(newStudent.firstName) + ' ' + App.Utils.esc(newStudent.lastName) + ' added!', 'success');
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
            + '<span class="text-sm text-slate-700 group-hover:text-blue-600 transition-colors">' + App.Utils.esc(c.name) + '</span>'
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
    const state = App.Store.get();
    const registrations = state.registrations || [];
    const families = state.families || [];
    const pending = registrations.filter(function(r) { return r.status === 'pending'; });

    // Build a set of emails that already have a parent account so the UI
    // can flag self-served parents (admin only needs to link the child).
    const parentEmails = {};
    families.forEach(function(f) { if (f.contact) parentEmails[f.contact.toLowerCase()] = true; });

    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-1">Pending Registrations</h2>'
      + '<p class="text-sm text-slate-500 mb-5">' + pending.length + ' application' + (pending.length !== 1 ? 's' : '') + ' awaiting review</p>'
      + (pending.length === 0
          ? '<div class="py-8 text-center text-slate-400">No pending registrations</div>'
          : '<div class="space-y-4 max-h-[60vh] overflow-y-auto pr-1">'
          + pending.map(function(reg) {
              const isTeacher = reg.type === 'teacher';
              const isEnrollment = reg.type === 'enrollment';
              const emailVerified = !!reg.emailVerifiedAt;
              const parentSelfServed = (isEnrollment || (!isTeacher && parentEmails[(reg.email || '').toLowerCase()]));
              const badges = [];

              // Type badge for enrollment requests
              if (isEnrollment) {
                badges.push('<span style="display:inline-flex;align-items:center;gap:0.3rem;padding:0.2rem 0.55rem;background:#f5f3ff;border:1px solid #ddd6fe;border-radius:999px;font-size:0.68rem;font-weight:700;color:#7c3aed">Child enrolment</span>');
              }

              if (emailVerified) {
                badges.push('<span style="display:inline-flex;align-items:center;gap:0.3rem;padding:0.2rem 0.55rem;background:#f0fdf4;border:1px solid #bbf7d0;border-radius:999px;font-size:0.68rem;font-weight:700;color:#15803d"><svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3"><path d="M5 13l4 4L19 7"/></svg>Email verified</span>');
              } else if (!isEnrollment) {
                badges.push('<span style="display:inline-flex;align-items:center;gap:0.3rem;padding:0.2rem 0.55rem;background:#fffbeb;border:1px solid #fde68a;border-radius:999px;font-size:0.68rem;font-weight:700;color:#92400e">Awaiting email verification</span>');
              }
              if (parentSelfServed && !isEnrollment) {
                badges.push('<span style="display:inline-flex;align-items:center;gap:0.3rem;padding:0.2rem 0.55rem;background:#eff6ff;border:1px solid #bfdbfe;border-radius:999px;font-size:0.68rem;font-weight:700;color:#1d4ed8">Parent already has account</span>');
              }

              const approveLabel = isEnrollment
                ? 'Enrol student'
                : (parentSelfServed ? 'Link student to parent' : (isTeacher ? 'Approve teacher' : 'Approve & create account'));

              const headerName = isTeacher
                ? App.Utils.esc(reg.parentName || 'Teacher applicant')
                : (App.Utils.esc(reg.studentFirstName || '') + ' ' + App.Utils.esc(reg.studentLastName || '')).trim() || 'Student';
              const subline = isTeacher
                ? 'Teacher application · ' + App.Utils.esc(reg.email)
                : (isEnrollment ? 'Parent: ' + App.Utils.esc(reg.parentName) + ' (existing account)' : 'Parent: ' + App.Utils.esc(reg.parentName) + ' · ' + App.Utils.esc(reg.email));

              return '<div class="border border-slate-200 rounded-xl p-4">'
                + '<div class="flex items-start justify-between gap-3 mb-2">'
                +   '<div>'
                +     '<div class="font-semibold text-slate-800">' + headerName + '</div>'
                +     '<div class="text-xs text-slate-500 mt-0.5">' + subline + '</div>'
                +   '</div>'
                +   '<span class="text-xs text-slate-400 shrink-0">' + App.Utils.formatDate(reg.submittedOn) + '</span>'
                + '</div>'
                + '<div style="display:flex;flex-wrap:wrap;gap:0.35rem;margin-bottom:0.75rem">' + badges.join('') + '</div>'
                + '<div class="grid grid-cols-2 gap-2 text-xs text-slate-600 mb-3">'
                + (reg.phone ? '<div><span class="text-slate-400">Phone:</span> ' + App.Utils.esc(reg.phone) + '</div>' : '')
                + (reg.studentDob ? '<div><span class="text-slate-400">DOB:</span> ' + App.Utils.formatDate(reg.studentDob) + '</div>' : '')
                + (reg.studentGender ? '<div><span class="text-slate-400">Gender:</span> ' + App.Utils.esc(reg.studentGender) + '</div>' : '')
                + (reg.classInterest ? '<div class="col-span-2"><span class="text-slate-400">Interested in:</span> ' + App.Utils.esc(reg.classInterest) + '</div>' : '')
                + (reg.specialization ? '<div class="col-span-2"><span class="text-slate-400">Specialty:</span> ' + App.Utils.esc(reg.specialization) + '</div>' : '')
                + (reg.emergencyName ? '<div class="col-span-2"><span class="text-slate-400">Emergency:</span> ' + App.Utils.esc(reg.emergencyName) + ' · ' + App.Utils.esc(reg.emergencyPhone) + '</div>' : '')
                + (reg.notes ? '<div class="col-span-2"><span class="text-slate-400">Notes:</span> ' + App.Utils.esc(reg.notes) + '</div>' : '')
                + '</div>'
                // Class assignment picker for enrollment requests
                + (isEnrollment ? (function() {
                    var allClasses = state.classes || [];
                    if (allClasses.length === 0) return '';
                    return '<div style="margin-bottom:0.75rem">'
                      + '<div style="font-size:0.68rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.4rem">Assign classes (optional)</div>'
                      + '<div style="max-height:120px;overflow-y:auto;border:1px solid #e2e8f0;border-radius:8px;padding:0.4rem">'
                      + allClasses.map(function(cls) {
                          var full = cls.enrolled >= cls.capacity;
                          return '<label style="display:flex;align-items:center;gap:0.4rem;padding:0.25rem 0.35rem;font-size:0.78rem;cursor:' + (full ? 'not-allowed' : 'pointer') + ';opacity:' + (full ? '0.5' : '1') + '">'
                            + '<input type="checkbox" class="enroll-class-cb" data-reg="' + reg.id + '" value="' + cls.id + '"' + (full ? ' disabled' : '') + ' style="width:14px;height:14px;accent-color:var(--gold);cursor:inherit">'
                            + '<span>' + App.Utils.esc(cls.name) + ' <span style="color:#94a3b8;font-size:0.7rem">' + App.Utils.esc(cls.day) + ' ' + App.Utils.formatTime(cls.time) + (full ? ' (FULL)' : ' (' + cls.enrolled + '/' + cls.capacity + ')') + '</span></span>'
                            + '</label>';
                        }).join('')
                      + '</div></div>';
                  })() : '')
                + '<div class="flex gap-2">'
                +   '<button onclick="App.Students._approveReg(\'' + reg.id + '\')" class="flex-1 py-1.5 text-sm bg-emerald-500 text-white rounded-lg hover:bg-emerald-600 font-medium">' + approveLabel + '</button>'
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
      // Collect any checked class checkboxes for this registration.
      var classIds = Array.from(document.querySelectorAll('.enroll-class-cb[data-reg="' + regId + '"]:checked'))
        .map(function(cb) { return cb.value; });
      const result = await App.Api.post('/api/registrations/' + regId + '/approve', { classIds: classIds });
      if (result) {
        App.Utils.hideModal(true);
        // Three response shapes from the backend:
        //   1. parent self-served: no tempPassword, message says "linked"
        //   2. teacher: no tempPassword, set-password email is sent automatically
        //   3. legacy admin-driven parent: tempPassword present, must share manually
        if (result.tempPassword) {
          App.Utils.showToast('Approved. Temp password: ' + result.tempPassword + ' (share with parent)', 'success', 15000);
        } else if (result.message) {
          App.Utils.showToast(result.message, 'success', 8000);
        } else {
          App.Utils.showToast('Approved.', 'success');
        }
        await App.Api.loadSnapshot();
        App.Notifs.refresh();
        App.Router.refresh();
      }
    } catch(err) {
      App.Utils.showToast(err.message || 'Approval failed', 'error');
    }
  }

  async function _rejectReg(regId) {
    var ok = await App.Utils.showConfirm({ title: 'Reject registration', message: 'This registration will be removed from the pending list.', confirmLabel: 'Reject', danger: true });
    if (!ok) return;
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
    var state = App.Store.get();
    var stuClasses = (state.students.find(function(x) { return x.id === studentId; }) || {}).enrolledClasses || [];
    var classOpts = '<option value="">-- select class --</option>'
      + state.classes.filter(function(c) { return stuClasses.indexOf(c.id) > -1; }).map(function(c) {
          return '<option value="' + c.id + '">' + App.Utils.esc(c.name) + '</option>';
        }).join('');
    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-lg font-bold mb-1">Mark child absent</h2>'
      + '<p class="text-sm text-slate-500 mb-4">If informed at least 3 hours before class start, the child earns a replacement credit. If informed late, tick "Late absence" to skip the credit.</p>'
      + '<form id="add-credit-form" class="space-y-4">'
      + _field('Class', '<select name="classId" class="form-input">' + classOpts + '</select>')
      + '<label style="display:flex;align-items:center;gap:0.5rem;font-size:0.85rem;color:#374151;background:#fef2f2;border:1px solid #fecaca;border-radius:8px;padding:0.55rem 0.75rem;cursor:pointer">'
      +   '<input type="checkbox" name="lateAbsence" style="cursor:pointer">'
      +   'Late absence (informed less than 3 hours before — no credit)'
      + '</label>'
      + _field('Credits <span class="text-slate-400 font-normal">(ignored if late absence)</span>', '<select name="minutes" class="form-input"><option value="1">1 credit</option><option value="2">2 credits</option><option value="3">3 credits</option><option value="4" selected>4 credits</option></select>')
      + _field('Category', '<select name="category" class="form-input"><option value="class" selected>Class</option><option value="self-study">Self-study</option></select>')
      + _field('Note', '<input name="note" class="form-input" placeholder="e.g. Sick, family event">')
      + _field('Date', '<input name="date" type="date" class="form-input" value="' + today + '" required>')
      + '<div class="flex justify-end gap-2 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" style="padding:0.45rem 1rem;font-size:0.84rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Mark absent</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );
    document.getElementById('add-credit-form').addEventListener('submit', async function(e) {
      e.preventDefault();
      var fd = new FormData(e.target);
      var classId = fd.get('classId') || '';
      var date = fd.get('date');
      var isLate = fd.get('lateAbsence') === 'on';
      try {
        // Mark absent in attendance if class selected
        if (classId) {
          await App.Api.post('/api/attendance', { personId: studentId, personType: 'student', date: date, classId: classId, status: 'Absent' });
        }
        if (!isLate) {
          var cls = classId ? state.classes.find(function(c) { return c.id === classId; }) : null;
          await App.Api.post('/api/replacement-credits', {
            studentId: studentId,
            type: 'earned',
            minutes: parseInt(fd.get('minutes'), 10),
            category: fd.get('category') || 'class',
            note: fd.get('note') || (cls ? 'Absent from ' + cls.name + ' on ' + date : ''),
            classId: classId,
            date: date
          });
          App.Utils.showToast('Marked absent — replacement credit added', 'success');
        } else {
          App.Utils.showToast('Marked absent — late notice, no credit issued', 'info');
        }
        App.Utils.hideModal(true);
        App.Router.refresh();
        _viewModal(studentId);
        _switchTab('replacements');
      } catch(err) {
        App.Utils.showToast(err.message || 'Failed to record absence', 'error');
      }
    });
  }

  function _useCreditModal(studentId) {
    var today = App.Utils.today ? App.Utils.today() : new Date().toISOString().slice(0, 10);
    var state = App.Store.get();
    var stuClasses = (state.students.find(function(x) { return x.id === studentId; }) || {}).enrolledClasses || [];

    // Filter the extension class picker to levels the student is already
    // enrolled in. Level is parsed from the "Level N" pattern in class
    // names; if the student has no recognised level, we fall back to all
    // classes so admin can still pick something.
    var enrolledLevels = {};
    stuClasses.forEach(function(cid) {
      var c = state.classes.find(function(x) { return x.id === cid; });
      if (!c) return;
      var m = (c.name || '').match(/level\s*(\d+)/i);
      if (m) enrolledLevels[m[1]] = true;
    });
    var hasLevels = Object.keys(enrolledLevels).length > 0;
    var allowed = state.classes.filter(function(c) {
      if (!hasLevels) return true;
      var m = (c.name || '').match(/level\s*(\d+)/i);
      return m && enrolledLevels[m[1]];
    });

    var classOpts = '<option value="">-- none --</option>'
      + allowed.map(function(c) {
          return '<option value="' + c.id + '">' + App.Utils.esc(c.name) + '</option>';
        }).join('');
    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-lg font-bold mb-1">Use replacement credit</h2>'
      + '<p class="text-sm text-slate-500 mb-4">Apply earned credits to an extension class (1-4 credits per session).</p>'
      + '<form id="use-credit-form" class="space-y-4">'
      + _field('Credits', '<select name="minutes" class="form-input"><option value="1">1 credit</option><option value="2">2 credits</option><option value="3">3 credits</option><option value="4">4 credits</option></select>')
      + _field('Category', '<select name="category" class="form-input"><option value="class" selected>Class</option><option value="self-study">Self-study</option></select>')
      + _field('Class (optional)', '<select name="classId" class="form-input">' + classOpts + '</select>')
      + _field('Note', '<input name="note" class="form-input" placeholder="e.g. Extended English session">')
      + _field('Date', '<input name="date" type="date" class="form-input" value="' + today + '" required>')
      + '<div class="flex justify-end gap-2 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" style="padding:0.45rem 1rem;font-size:0.84rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Use credit</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );
    document.getElementById('use-credit-form').addEventListener('submit', async function(e) {
      e.preventDefault();
      var fd = new FormData(e.target);
      var mins = parseInt(fd.get('minutes'), 10);
      var cat = fd.get('category') || 'class';
      // Check balance by category
      var creds = (App.Store.get().replacementCredits || []).filter(function(rc) { return rc.studentId === studentId && (rc.category || 'class') === cat; });
      var bal = creds.filter(function(rc) { return rc.type === 'earned'; }).reduce(function(a, rc) { return a + (rc.minutes || 0); }, 0)
              - creds.filter(function(rc) { return rc.type === 'used'; }).reduce(function(a, rc) { return a + (rc.minutes || 0); }, 0);
      if (mins > bal) {
        App.Utils.showToast('Insufficient ' + (cat === 'self-study' ? 'self-study' : 'class') + ' credits (' + bal + ' available)', 'error');
        return;
      }
      try {
        await App.Api.post('/api/replacement-credits', {
          studentId: studentId,
          type: 'used',
          minutes: mins,
          category: cat,
          classId: fd.get('classId') || '',
          note: fd.get('note') || '',
          date: fd.get('date')
        });
        App.Utils.hideModal(true);
        App.Utils.showToast(mins + ' ' + (cat === 'self-study' ? 'self-study' : 'class') + ' credit(s) used', 'success');
        await App.Api.refresh();
        _viewModal(studentId);
        _switchTab('replacements');
      } catch(err) {
        App.Utils.showToast(err.message || 'Failed to use replacement', 'error');
      }
    });
  }

  async function _deleteCredit(creditId, studentId) {
    var ok = await App.Utils.showConfirm({ title: 'Delete replacement entry', confirmLabel: 'Delete', danger: true });
    if (!ok) return;
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

  function _setStudentsTab(tab) {
    _studentsTab = tab;
    App.Router.refresh();
  }

  function _searchFamilies(val) {
    _familySearch = val;
    App.Router.refresh();
  }

  function _familiesView() {
    var { families, students, invoices, classes } = App.Store.get();
    var isAdmin = App.currentRole === 'admin';

    var allFamilies = families || [];
    // Filter by search
    if (_familySearch) {
      var q = _familySearch.toLowerCase();
      allFamilies = allFamilies.filter(function(f) {
        return (f.name || '').toLowerCase().indexOf(q) > -1
          || (f.parentName || '').toLowerCase().indexOf(q) > -1
          || (f.contact || '').toLowerCase().indexOf(q) > -1;
      });
    }

    var familyCards = allFamilies.map(function(f) {
        var children = students.filter(function(s) { return s.familyId === f.id && s.status !== 'Inactive'; });
        var childIds = children.map(function(s) { return s.id; });
        var outstanding = invoices.filter(function(i) {
            return childIds.indexOf(i.studentId) > -1 && (i.status === 'Unpaid' || i.status === 'Overdue');
        }).reduce(function(a, i) { return a + i.amount; }, 0);

        // Count active classes across all children
        var activeClassSet = {};
        children.forEach(function(s) { (s.enrolledClasses || []).forEach(function(cid) { activeClassSet[cid] = true; }); });
        var activeClassCount = Object.keys(activeClassSet).length;

        // Children with status pills
        var childPills = children.map(function(s) {
          var statusColor = s.status === 'Active' ? 'background:#ecfdf5;color:#059669;border:1px solid #a7f3d0'
            : s.status === 'New' ? 'background:#eff6ff;color:#2563eb;border:1px solid #bfdbfe'
            : s.status === 'Waitlisted' ? 'background:#fffbeb;color:#d97706;border:1px solid #fde68a'
            : 'background:#f8fafc;color:#94a3b8;border:1px solid #e2e8f0';
          return '<span style="display:inline-flex;align-items:center;gap:0.3rem;font-size:0.72rem;margin-right:0.25rem">'
            + App.Utils.esc(s.firstName)
            + ' <span style="display:inline-block;padding:0.05rem 0.4rem;font-size:0.62rem;font-weight:600;border-radius:999px;' + statusColor + '">' + App.Utils.esc(s.status) + '</span>'
            + '</span>';
        }).join('');

        return '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);padding:1.25rem 1.25rem 1rem;cursor:pointer;transition:box-shadow 0.15s" onclick="App.Students._familyModal(\'' + f.id + '\')" onmouseover="this.style.boxShadow=\'0 4px 12px rgba(0,0,0,0.08)\'" onmouseout="this.style.boxShadow=\'none\'">'
            // Family name + avatar
            + '<div style="display:flex;align-items:center;gap:0.75rem;margin-bottom:0.65rem">'
            +   '<div style="width:2.5rem;height:2.5rem;border-radius:10px;background:var(--gold-dim);color:var(--gold);font-weight:800;font-size:1rem;display:flex;align-items:center;justify-content:center;flex-shrink:0">' + App.Utils.esc(f.name).charAt(0) + '</div>'
            +   '<div style="flex:1;min-width:0">'
            +     '<div style="font-weight:700;font-size:0.95rem;color:#111;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + App.Utils.esc(f.name) + '</div>'
            +     '<div style="font-size:0.73rem;color:#64748b">' + App.Utils.esc(f.parentName || '') + '</div>'
            +   '</div>'
            + '</div>'
            // Parent contact info
            + '<div style="font-size:0.72rem;color:#94a3b8;margin-bottom:0.65rem;display:flex;flex-wrap:wrap;gap:0.15rem 0.75rem">'
            +   '<span>' + App.Utils.esc(f.contact) + '</span>'
            +   (f.phone ? '<span>' + App.Utils.esc(f.phone) + '</span>' : '')
            + '</div>'
            // Children with status pills
            + (childPills ? '<div style="margin-bottom:0.65rem;line-height:1.7">' + childPills + '</div>' : '')
            // Stats row
            + '<div style="display:flex;gap:0.75rem;font-size:0.75rem;color:#64748b;padding-top:0.5rem;border-top:1px solid #f4f4f2">'
            +   '<div><span style="font-weight:700;color:#111">' + children.length + '</span> child' + (children.length !== 1 ? 'ren' : '') + '</div>'
            +   '<div><span style="font-weight:700;color:#111">' + activeClassCount + '</span> class' + (activeClassCount !== 1 ? 'es' : '') + '</div>'
            +   (outstanding > 0
              ? '<div style="margin-left:auto"><span style="font-weight:700;color:#dc2626">' + App.Utils.formatCurrency(outstanding) + '</span> due</div>'
              : '<div style="margin-left:auto;color:#15803d;font-weight:600">All paid</div>')
            + '</div>'
            + '</div>';
    }).join('');

    // Search bar + Add Family button
    var toolbar = '<div style="display:flex;align-items:center;gap:0.75rem;margin-bottom:1rem;flex-wrap:wrap">'
        + '<input type="text" placeholder="Search families..." oninput="App.Students._searchFamilies(this.value)" value="' + App.Utils.esc(_familySearch) + '" style="flex:1;min-width:200px;padding:0.5rem 0.85rem;font-size:0.84rem;border:1px solid #e2e8f0;border-radius:10px;outline:none;background:#fff;font-family:inherit">'
        + (isAdmin ? '<button onclick="App.Students._addFamilyModal()" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700" style="white-space:nowrap">+ Add Family</button>' : '')
        + '</div>';

    return toolbar
        + (familyCards.length === 0
            ? '<div style="background:#fff;border-radius:14px;border:1px solid rgba(0,0,0,0.07)">' + App.Utils.emptyState(
                _familySearch ? 'No families match your search' : 'No families yet',
                _familySearch ? 'Try adjusting your search term.' : 'Families will be created automatically when students are added.',
                _familySearch ? '<button onclick="App.Students._searchFamilies(\'\')" style="padding:0.5rem 1.25rem;font-size:0.83rem;font-weight:600;background:#f1f5f9;color:#475569;border:none;border-radius:8px;cursor:pointer">Clear Search</button>' : ''
              ) + '</div>'
            : '<div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(300px,1fr));gap:1rem">' + familyCards + '</div>');
  }

  function _familyModal(familyId) {
    var state = App.Store.get();
    var families = state.families || [];
    var students = state.students || [];
    var invoices = state.invoices || [];
    var classes = state.classes || [];
    var staff = state.staff || [];
    var feedbackEntries = state.feedback || [];
    var replacementCredits = state.replacementCredits || [];
    var isAdmin = App.currentRole === 'admin';
    var f = families.find(function(x) { return x.id === familyId; });
    if (!f) return;

    var children = students.filter(function(s) { return s.familyId === familyId; });
    var childIds = children.map(function(s) { return s.id; });
    var familyInvoices = invoices.filter(function(i) { return childIds.indexOf(i.studentId) > -1; });

    // --- Referrals: rewards this family has earned by referring others ---
    var familyReferralRewards = (state.referralRewards || []).filter(function(r) { return r.referrerFamilyId === familyId; });
    var referralHtml = '';
    if (f.referralCode || familyReferralRewards.length > 0) {
      var earnedTotal = familyReferralRewards
        .filter(function(r) { return r.status === 'earned'; })
        .reduce(function(a, r) { return a + (r.creditsRemaining || 0); }, 0);
      referralHtml = '<div style="margin-top:1rem;padding:1rem;background:#fffbeb;border:1px solid #fef3c7;border-radius:12px">'
        + '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.5rem">'
        +   '<div style="font-size:0.72rem;font-weight:700;color:#92400e;text-transform:uppercase;letter-spacing:0.05em">Referrals</div>'
        +   (f.referralCode ? '<button onclick="navigator.clipboard.writeText(\'' + f.referralCode + '\').then(function(){App.Utils.showToast(\'Code copied\',\'success\')})" style="font-size:0.7rem;padding:0.25rem 0.65rem;background:var(--gold);color:#0a0a0a;border:none;border-radius:6px;cursor:pointer;font-weight:700">Copy code</button>' : '')
        + '</div>'
        + (f.referralCode ? '<div style="font-family:var(--serif);font-size:1.2rem;font-weight:700;color:var(--gold);letter-spacing:0.04em">' + App.Utils.esc(f.referralCode) + '</div>' : '')
        + (earnedTotal > 0 ? '<div style="margin-top:0.4rem;font-size:0.72rem;color:#92400e;font-weight:600">Active credit · ' + earnedTotal + ' invoice' + (earnedTotal !== 1 ? 's' : '') + ' remaining</div>' : '')
        + (familyReferralRewards.length > 0
            ? '<div style="margin-top:0.6rem;border-top:1px solid #fde68a;padding-top:0.5rem">'
              + familyReferralRewards.map(function(r) {
                  var statusColor = r.status === 'earned' ? '#15803d' : r.status === 'exhausted' ? '#94a3b8' : '#d97706';
                  var statusBg    = r.status === 'earned' ? '#f0fdf4' : r.status === 'exhausted' ? '#f1f5f9' : '#fffbeb';
                  var progress = r.status === 'pending' ? (r.paidInvoiceCount || 0) + ' / 3 paid' : r.status === 'earned' ? r.creditsRemaining + ' credits left' : 'Used';
                  return '<div style="display:flex;align-items:center;justify-content:space-between;padding:0.35rem 0;font-size:0.76rem;color:#78350f">'
                    + '<span>' + App.Utils.esc(r.referredName || r.referredStudentId) + '</span>'
                    + '<span style="display:inline-flex;align-items:center;gap:0.4rem">'
                    +   '<span style="font-size:0.7rem;color:#94a3b8">' + progress + '</span>'
                    +   '<span style="padding:0.15rem 0.55rem;background:' + statusBg + ';color:' + statusColor + ';border-radius:999px;font-size:0.66rem;font-weight:700;text-transform:uppercase">' + r.status + '</span>'
                    + '</span>'
                    + '</div>';
                }).join('')
              + '</div>'
            : '<p style="margin:0.5rem 0 0;font-size:0.72rem;color:#92400e">Share this code — when a referred student stays for 3 months, RM10/month off auto-applies for 3 months.</p>')
        + '</div>';
    }
    var outstanding = familyInvoices.filter(function(i) { return i.status === 'Unpaid' || i.status === 'Overdue'; }).reduce(function(a, i) { return a + i.amount; }, 0);
    var totalPaid = familyInvoices.filter(function(i) { return i.status === 'Paid'; }).reduce(function(a, i) { return a + i.amount; }, 0);

    // Collect all class IDs the family's children are enrolled in
    var familyClassIds = [];
    children.forEach(function(s) { (s.enrolledClasses || []).forEach(function(cid) { if (familyClassIds.indexOf(cid) === -1) familyClassIds.push(cid); }); });

    // --- Recent Feedback (last 3) ---
    var familyFeedback = feedbackEntries.filter(function(fb) {
      return familyClassIds.indexOf(fb.classId) > -1;
    }).sort(function(a, b) { return (b.date || '') > (a.date || '') ? 1 : -1; }).slice(0, 3);

    var feedbackHtml = '';
    if (familyFeedback.length > 0) {
      feedbackHtml = '<div style="margin-top:1rem">'
        + '<div style="font-size:0.72rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.4rem">Recent Feedback</div>'
        + '<div style="border-top:1px solid #f1f5f9">'
        + familyFeedback.map(function(fb) {
            var cls = classes.find(function(c) { return c.id === fb.classId; });
            var teacher = fb.teacherId ? staff.find(function(st) { return st.id === fb.teacherId; }) : null;
            var teacherName = teacher ? teacher.fullName : (fb.teacher || '');
            return '<div style="padding:0.45rem 0;border-bottom:1px solid #f8f8f6;font-size:0.78rem">'
              + '<div style="color:#374151;font-style:italic">"' + App.Utils.esc(fb.notes || fb.content || '') + '"</div>'
              + '<div style="font-size:0.68rem;color:#94a3b8;margin-top:0.15rem">'
              + (teacherName ? App.Utils.esc(teacherName) + ', ' : '')
              + (cls ? App.Utils.esc(cls.name) : '')
              + (fb.date ? ', ' + App.Utils.formatDate(fb.date) : '')
              + '</div></div>';
          }).join('')
        + '</div></div>';
    }

    // --- Upcoming Classes (today + tomorrow) ---
    var today = new Date();
    var tomorrow = new Date(today);
    tomorrow.setDate(tomorrow.getDate() + 1);
    var dayNames = ['Sunday','Monday','Tuesday','Wednesday','Thursday','Friday','Saturday'];
    var todayDay = dayNames[today.getDay()];
    var tomorrowDay = dayNames[tomorrow.getDay()];
    var months = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
    var todayLabel = todayDay.slice(0,3) + ' ' + today.getDate() + ' ' + months[today.getMonth()];
    var tomorrowLabel = tomorrowDay.slice(0,3) + ' ' + tomorrow.getDate() + ' ' + months[tomorrow.getMonth()];

    var upcomingClasses = classes.filter(function(c) {
      return familyClassIds.indexOf(c.id) > -1 && (c.day === todayDay || c.day === tomorrowDay);
    }).sort(function(a, b) {
      var dayOrder = a.day === todayDay ? 0 : 1;
      var dayOrder2 = b.day === todayDay ? 0 : 1;
      if (dayOrder !== dayOrder2) return dayOrder - dayOrder2;
      return (a.time || '') < (b.time || '') ? -1 : 1;
    });

    var upcomingHtml = '';
    if (upcomingClasses.length > 0) {
      upcomingHtml = '<div style="margin-top:1rem">'
        + '<div style="font-size:0.72rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.4rem">Upcoming Classes</div>'
        + '<div style="border-top:1px solid #f1f5f9">'
        + upcomingClasses.map(function(c) {
            var dayLabel = c.day === todayDay ? todayLabel : tomorrowLabel;
            var enrolledChildren = children.filter(function(s) { return (s.enrolledClasses || []).indexOf(c.id) > -1; });
            var childNameStr = enrolledChildren.map(function(s) { return App.Utils.esc(s.firstName); }).join(', ');
            var teacherNames = (c.teacherIds || []).map(function(tid) {
              var st = staff.find(function(x) { return x.id === tid; });
              return st ? st.fullName : '';
            }).filter(Boolean).join(', ');
            return '<div style="padding:0.4rem 0;border-bottom:1px solid #f8f8f6;font-size:0.78rem;display:flex;gap:0.5rem;align-items:baseline">'
              + '<span style="font-weight:600;color:#374151;white-space:nowrap">' + dayLabel + ' ' + App.Utils.formatTime(c.time) + '</span>'
              + '<span style="color:#64748b">' + App.Utils.esc(c.name) + ' (' + childNameStr + ')'
              + (c.classroom ? ' · ' + App.Utils.esc(c.classroom) : '')
              + (teacherNames ? ' · ' + App.Utils.esc(teacherNames) : '')
              + '</span></div>';
          }).join('')
        + '</div></div>';
    }

    // --- Replacement Balances per child ---
    var hasAnyBalance = false;
    var replacementRows = children.map(function(s) {
      var creds = replacementCredits.filter(function(rc) { return rc.studentId === s.id; });
      var earned = creds.filter(function(rc) { return rc.type === 'earned'; }).reduce(function(a, rc) { return a + (rc.minutes || 0); }, 0);
      var used = creds.filter(function(rc) { return rc.type === 'used'; }).reduce(function(a, rc) { return a + (rc.minutes || 0); }, 0);
      var bal = earned - used;
      if (bal > 0) hasAnyBalance = true;
      return '<div style="display:flex;justify-content:space-between;padding:0.3rem 0;font-size:0.78rem;border-bottom:1px solid #f8f8f6">'
        + '<span style="color:#374151">' + App.Utils.esc(s.firstName) + '</span>'
        + '<span style="font-weight:600;color:' + (bal > 0 ? '#92400e' : '#94a3b8') + '">' + (bal > 0 ? bal + ' credits' : '0 credits') + '</span>'
        + '</div>';
    }).join('');

    var replacementHtml = '';
    if (children.length > 0) {
      replacementHtml = '<div style="margin-top:1rem">'
        + '<div style="font-size:0.72rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.4rem">Replacements</div>'
        + '<div style="border-top:1px solid #f1f5f9">' + replacementRows + '</div>'
        + '</div>';
    }

    App.Utils.showModal(
        '<div class="p-6">'
        + '<div style="display:flex;align-items:center;gap:0.75rem;margin-bottom:1.5rem">'
        +   '<div style="width:3rem;height:3rem;border-radius:12px;background:var(--gold-dim);color:var(--gold);font-weight:800;font-size:1.25rem;display:flex;align-items:center;justify-content:center">' + App.Utils.esc(f.name).charAt(0) + '</div>'
        +   '<div>'
        +     '<h2 style="font-size:1.15rem;font-weight:700;color:#111;margin:0">' + App.Utils.esc(f.name) + '</h2>'
        +     '<div style="font-size:0.78rem;color:#94a3b8">' + App.Utils.esc(f.contact) + (f.phone ? ' · ' + App.Utils.esc(f.phone) : '') + '</div>'
        +   '</div>'
        +   (isAdmin ? '<button onclick="App.Students._editFamilyModal(\'' + familyId + '\')" style="margin-left:auto;font-size:0.75rem;padding:0.35rem 0.75rem;border:1px solid #e2e8f0;border-radius:8px;background:#fff;color:#64748b;cursor:pointer">Edit</button>' : '')
        + '</div>'

        // Billing summary
        + '<div style="display:grid;grid-template-columns:1fr 1fr;gap:0.75rem;margin-bottom:1.25rem">'
        +   '<div style="padding:0.75rem;background:#f0fdf4;border:1px solid #bbf7d0;border-radius:10px;text-align:center">'
        +     '<div style="font-size:0.65rem;color:#15803d;text-transform:uppercase;font-weight:600;letter-spacing:0.04em">Total Paid</div>'
        +     '<div style="font-family:var(--serif);font-size:1.1rem;font-weight:700;color:#15803d">' + App.Utils.formatCurrency(totalPaid) + '</div>'
        +   '</div>'
        +   '<div style="padding:0.75rem;background:' + (outstanding > 0 ? '#fef2f2' : '#f8fafc') + ';border:1px solid ' + (outstanding > 0 ? '#fecaca' : '#e2e8f0') + ';border-radius:10px;text-align:center">'
        +     '<div style="font-size:0.65rem;color:' + (outstanding > 0 ? '#dc2626' : '#64748b') + ';text-transform:uppercase;font-weight:600;letter-spacing:0.04em">Outstanding</div>'
        +     '<div style="font-family:var(--serif);font-size:1.1rem;font-weight:700;color:' + (outstanding > 0 ? '#dc2626' : '#64748b') + '">' + App.Utils.formatCurrency(outstanding) + '</div>'
        +   '</div>'
        + '</div>'

        // Children
        + '<div style="font-size:0.72rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.5rem">Children (' + children.length + ')</div>'
        + (children.length === 0
            ? '<div style="padding:1.5rem;text-align:center;color:#94a3b8;font-size:0.83rem">No students in this family</div>'
            : children.map(function(s) {
                var enrolledNames = s.enrolledClasses.map(function(cid) { var c = classes.find(function(x) { return x.id === cid; }); return c ? App.Utils.esc(c.name) : cid; }).join(', ');
                return '<div style="display:flex;align-items:center;gap:0.65rem;padding:0.6rem 0;border-bottom:1px solid #f4f4f2;cursor:pointer" onclick="App.Utils.hideModal(true);App.Students._viewModal(\'' + s.id + '\')">'
                    + '<div style="width:2rem;height:2rem;border-radius:50%;background:#eff6ff;color:#1d4ed8;font-weight:700;font-size:0.75rem;display:flex;align-items:center;justify-content:center;flex-shrink:0">' + App.Utils.esc(s.firstName).charAt(0) + App.Utils.esc(s.lastName).charAt(0) + '</div>'
                    + '<div style="flex:1;min-width:0">'
                    +   '<div style="font-weight:600;font-size:0.85rem;color:#111">' + App.Utils.esc(s.firstName) + ' ' + App.Utils.esc(s.lastName) + '</div>'
                    +   '<div style="font-size:0.72rem;color:#94a3b8">' + (enrolledNames || 'No classes') + '</div>'
                    + '</div>'
                    + App.Utils.statusBadge(s.status)
                    + '</div>';
            }).join(''))

        // New sections: feedback, upcoming, replacements
        + upcomingHtml
        + feedbackHtml
        + replacementHtml
        + referralHtml

        + (f.address ? '<div style="margin-top:1rem;font-size:0.78rem;color:#64748b"><span style="font-weight:600">Address:</span> ' + App.Utils.esc(f.address) + '</div>' : '')
        + (f.notes ? '<div style="margin-top:0.5rem;font-size:0.78rem;color:#64748b"><span style="font-weight:600">Notes:</span> ' + App.Utils.esc(f.notes) + '</div>' : '')

        + '<div style="margin-top:1.25rem;display:flex;justify-content:space-between;align-items:center">'
        + (isAdmin ? '<button onclick="App.Students._pdpaDelete(\'' + familyId + '\')" style="padding:0.4rem 0.85rem;font-size:0.72rem;border:1px solid #fecaca;border-radius:8px;background:#fff;color:#dc2626;cursor:pointer" title="PDPA: permanently anonymise this family\'s data">Delete account</button>' : '<div></div>')
        + '<div style="display:flex;gap:0.5rem">'
        + (isAdmin ? '<button onclick="App.Utils.hideModal(true);App.Students._addModal(\'' + familyId + '\')" style="padding:0.4rem 0.85rem;font-size:0.78rem;font-weight:600;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">+ Add Child</button>' : '')
        + '<button onclick="App.Utils.hideModal()" style="padding:0.4rem 0.85rem;font-size:0.78rem;border:1px solid #e2e8f0;border-radius:8px;background:#fff;color:#64748b;cursor:pointer">Close</button>'
        + '</div></div>'
        + '</div>'
    );
  }

  function _addFamilyModal() {
    App.Utils.showModal(
        '<div class="p-6">'
        + '<h2 class="text-lg font-bold mb-4">Add Family</h2>'
        + '<form id="add-family-form" class="space-y-3">'
        + _field('Family Name', '<input name="name" class="form-input" placeholder="e.g. The Ahmad Family" required>')
        + _field('Parent Email', '<input name="contact" type="email" class="form-input" placeholder="parent@email.com" required>')
        + _field('Parent Name', '<input name="parentName" class="form-input" placeholder="Full name">')
        + _field('Phone', '<input name="phone" class="form-input" placeholder="Phone number">')
        + _field('Address', '<input name="address" class="form-input" placeholder="Home address">')
        + _field('Notes', '<textarea name="notes" class="form-input" rows="2" placeholder="Any notes"></textarea>')
        + '<div class="flex justify-end gap-2 pt-2">'
        + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
        + '<button type="submit" style="padding:0.45rem 1rem;font-size:0.84rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Add Family</button>'
        + '</div>'
        + '</form>'
        + '</div>'
    );
    document.getElementById('add-family-form').addEventListener('submit', async function(e) {
        e.preventDefault();
        var fd = new FormData(e.target);
        try {
            await App.Api.post('/api/families', {
                name: fd.get('name'),
                contact: fd.get('contact'),
                parentName: fd.get('parentName') || '',
                phone: fd.get('phone') || '',
                address: fd.get('address') || '',
                notes: fd.get('notes') || ''
            });
            App.Utils.hideModal(true);
            App.Utils.showToast('Family added', 'success');
            await App.Api.refresh();
        } catch(err) {
            App.Utils.showToast(err.message || 'Failed to add family', 'error');
        }
    });
  }

  function _editFamilyModal(familyId) {
    var { families } = App.Store.get();
    var f = (families || []).find(function(x) { return x.id === familyId; });
    if (!f) return;

    App.Utils.showModal(
        '<div class="p-6">'
        + '<h2 class="text-lg font-bold mb-4">Edit Family</h2>'
        + '<form id="edit-family-form" class="space-y-3">'
        + _field('Family Name', '<input name="name" class="form-input" value="' + App.Utils.esc(f.name) + '" required>')
        + _field('Parent Email', '<input name="contact" type="email" class="form-input" value="' + App.Utils.esc(f.contact) + '" required>')
        + _field('Parent Name', '<input name="parentName" class="form-input" value="' + App.Utils.esc(f.parentName || '') + '">')
        + _field('Phone', '<input name="phone" class="form-input" value="' + App.Utils.esc(f.phone || '') + '">')
        + _field('Address', '<input name="address" class="form-input" value="' + App.Utils.esc(f.address || '') + '">')
        + _field('Notes', '<textarea name="notes" class="form-input" rows="2">' + App.Utils.esc(f.notes || '') + '</textarea>')
        + '<div class="flex justify-end gap-2 pt-2">'
        + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
        + '<button type="submit" style="padding:0.45rem 1rem;font-size:0.84rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Save</button>'
        + '</div>'
        + '</form>'
        + '</div>'
    );
    document.getElementById('edit-family-form').addEventListener('submit', async function(e) {
        e.preventDefault();
        var fd = new FormData(e.target);
        try {
            await App.Api.put('/api/families/' + familyId, {
                name: fd.get('name'),
                contact: fd.get('contact'),
                parentName: fd.get('parentName') || '',
                phone: fd.get('phone') || '',
                address: fd.get('address') || '',
                notes: fd.get('notes') || ''
            });
            App.Utils.hideModal(true);
            App.Utils.showToast('Family updated', 'success');
            await App.Api.refresh();
        } catch(err) {
            App.Utils.showToast(err.message || 'Failed to update family', 'error');
        }
    });
  }

  App.Students = {
    render: render,
    _onSearch: _onSearch,
    _onFilter: _onFilter,
    _viewModal: _viewModal,
    _editModal: _editModal,
    _subscriptionAction: _subscriptionAction,
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
    _deleteCredit: _deleteCredit,
    _setTab: _setStudentsTab,
    _searchFamilies: _searchFamilies,
    _familyModal: _familyModal,
    _addFamilyModal: _addFamilyModal,
    _editFamilyModal: _editFamilyModal,
    _pdpaDelete: _pdpaDelete
  };

  async function _pdpaDelete(familyId) {
    var ok = await App.Utils.showConfirm({
      title: 'Delete account (PDPA)',
      message: 'This will permanently anonymise all personal data for this family, their children, and the parent account. Invoices kept for tax purposes but contact details will be redacted.<br><br><strong>This cannot be undone.</strong>',
      confirmLabel: 'Delete permanently',
      danger: true
    });
    if (!ok) return;
    try {
      await App.Api.del('/api/families/' + familyId + '/pdpa');
      App.Utils.showToast('Account deleted and data anonymised', 'success');
      await App.Api.loadSnapshot();
      App.Router.refresh();
    } catch(err) {}
  }
})();
