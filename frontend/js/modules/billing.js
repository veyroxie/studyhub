(function() {
  window.App = window.App || {};

  let _filter = 'Unpaid';
  let _studentFilter = ''; // empty = all students
  let _menuListenerAdded = false;
  let _selectedInv = {};
  let _billingPage = 0;
  var _PAGE_SIZE = 15;
  var SELF_STUDY_SESSION_RATE = 10; // RM per self-study session (manual drop-in billing)

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
    const { invoices, students } = App.Store.get();
    // Pre-compute lookup so the per-row students.find() inside paged.map()
    // becomes O(1). Without this, rendering 50 invoices over 200 students
    // is 10k iterations per render — costly when search re-renders on every
    // keystroke.
    const _studentMap = {};
    students.forEach(function(s) { _studentMap[s.id] = s; });
    const isAdmin = App.currentRole === 'admin';
    const isClient = App.currentRole === 'client';

    let displayInvoices = invoices;
    if (isClient && App.clientParent) {
      const myStudentIds = students.filter(function(s) { return s.contact === App.clientParent; }).map(function(s) { return s.id; });
      displayInvoices = invoices.filter(function(inv) { return myStudentIds.indexOf(inv.studentId) > -1; });
    }

    // Student filter
    if (_studentFilter) {
      displayInvoices = displayInvoices.filter(function(i) { return i.studentId === _studentFilter; });
    }

    const filtered = _filter === 'All'     ? displayInvoices
      : _filter === 'Archive' ? displayInvoices.filter(function(i) { return i.status === 'Paid'; })
      : _filter === 'Pending' ? displayInvoices.filter(function(i) { return i.status === 'Pending Verification'; })
      : displayInvoices.filter(function(i) { return i.status === _filter; });

    const pendingVerifCount = displayInvoices.filter(function(i) { return i.status === 'Pending Verification'; }).length;

    const totalRevenue = displayInvoices.reduce(function(s, i) { return s + i.amount; }, 0);
    const collected    = displayInvoices.filter(function(i) { return i.status === 'Paid'; }).reduce(function(s, i) { return s + i.amount; }, 0);
    const pending      = displayInvoices.filter(function(i) { return i.status === 'Unpaid' || i.status === 'Pending Verification'; }).reduce(function(s, i) { return s + i.amount; }, 0);
    const overdue      = displayInvoices.filter(function(i) { return i.status === 'Overdue'; }).reduce(function(s, i) { return s + i.amount; }, 0);

    // Due-soon: unpaid invoices due within 7 days
    const today = new Date();
    const in7   = new Date(today); in7.setDate(today.getDate() + 7);
    const dueSoon = invoices.filter(function(i) {
      if (i.status !== 'Unpaid') return false;
      const d = new Date(i.dueDate);
      return d >= today && d <= in7;
    });
    const overdueInvs = invoices.filter(function(i) { return i.status === 'Overdue'; });

    // ── Early bird check ──────────────────────────────────────────────────────
    const todayDay = new Date().getDate();
    const isEarlyBirdPeriod = todayDay <= 7;

    // ── Notification banners ──────────────────────────────────────────────────
    let notifBanner = '';
    if (isClient) {
      const myIds = students.filter(function(s) { return s.contact === App.clientParent; }).map(function(s) { return s.id; });
      const myOverdue  = overdueInvs.filter(function(i) { return myIds.indexOf(i.studentId) > -1; });
      const myDueSoon  = dueSoon.filter(function(i) { return myIds.indexOf(i.studentId) > -1; });
      if (myOverdue.length > 0) {
        notifBanner = '<div class="mb-4 px-4 py-3 bg-red-50 border border-red-200 rounded-xl flex items-center gap-3">'
          + '<div class="w-2 h-2 rounded-full bg-red-500 shrink-0"></div>'
          + '<div class="text-sm text-red-700"><span class="font-semibold">Payment overdue</span> — '
          + myOverdue.length + ' invoice' + (myOverdue.length > 1 ? 's are' : ' is') + ' overdue. Please settle to avoid late fees.</div>'
          + '<button onclick="App.Billing._setFilter(\'Overdue\')" class="ml-auto text-xs font-semibold text-red-600 hover:text-red-800 whitespace-nowrap">View</button>'
          + '</div>';
      } else if (isEarlyBirdPeriod) {
        notifBanner = '<div class="mb-4 px-4 py-3 rounded-xl flex items-center gap-3" style="background:#fffbeb;border:1px solid #C9A227">'
          + '<div style="width:8px;height:8px;border-radius:50%;background:var(--gold);flex-shrink:0"></div>'
          + '<div class="text-sm" style="color:#92400e"><span class="font-semibold">Early bird discount active</span> — pay your invoice before the 7th and get <strong>10% off</strong> automatically.</div>'
          + '</div>';
      } else if (myDueSoon.length > 0) {
        notifBanner = '<div class="mb-4 px-4 py-3 bg-amber-50 border border-amber-200 rounded-xl flex items-center gap-3">'
          + '<div class="w-2 h-2 rounded-full bg-amber-500 shrink-0"></div>'
          + '<div class="text-sm text-amber-700"><span class="font-semibold">Payment due soon</span> — '
          + myDueSoon.length + ' invoice' + (myDueSoon.length > 1 ? 's are' : ' is') + ' due within 7 days.</div>'
          + '<button onclick="App.Billing._setFilter(\'Unpaid\')" class="ml-auto text-xs font-semibold text-amber-600 hover:text-amber-800 whitespace-nowrap">View</button>'
          + '</div>';
      }
    } else if (isAdmin && overdueInvs.length > 0) {
      notifBanner = '<div class="mb-4 px-4 py-3 bg-red-50 border border-red-200 rounded-xl flex items-center gap-3">'
        + '<div class="w-2 h-2 rounded-full bg-red-500 shrink-0"></div>'
        + '<div class="text-sm text-red-700"><span class="font-semibold">' + overdueInvs.length + ' overdue invoice' + (overdueInvs.length > 1 ? 's' : '') + '</span> — consider following up with parents.</div>'
        + '<button onclick="App.Billing._setFilter(\'Overdue\')" class="ml-auto text-xs font-semibold text-red-600 hover:text-red-800 whitespace-nowrap">View all</button>'
        + '</div>';
    }

    var paged = filtered.slice(_billingPage * _PAGE_SIZE, (_billingPage + 1) * _PAGE_SIZE);

    const colCount = isAdmin ? 8 : isClient ? 7 : 6;

    container.innerHTML = ''
      + '<div class="flex items-center justify-between mb-6">'
      +   '<h1 class="text-2xl font-bold text-slate-800">Billing</h1>'
      +   (isAdmin
          ? '<div class="flex gap-2">'
          + '<button onclick="App.Billing._generateMonthly()" class="px-4 py-2 text-sm bg-indigo-600 text-white rounded-lg hover:bg-indigo-700" title="Run the monthly invoice + payroll job for this month">Generate Monthly</button>'
          + '<button onclick="App.Billing._exportCSV()" class="px-4 py-2 text-sm bg-emerald-600 text-white rounded-lg hover:bg-emerald-700">Export CSV</button>'
          + '<button onclick="App.Billing._createModal()" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">+ Create Invoice</button>'
          + '</div>'
          : '')
      + '</div>'

      + notifBanner

      + (isClient
        ? '<div class="grid grid-cols-2 gap-4 mb-6">'
          + _statCard('Pending', App.Utils.formatCurrency(pending), 'text-amber-600', 'bg-amber-50', 'Unpaid')
          + _statCard('Overdue', App.Utils.formatCurrency(overdue), 'text-red-600', 'bg-red-50', 'Overdue')
          + '</div>'
        : '<div class="grid grid-cols-4 gap-4 mb-6">'
          + _statCard('Total Revenue', App.Utils.formatCurrency(totalRevenue), 'text-slate-700', 'bg-slate-50', 'All')
          + _statCard('Collected', App.Utils.formatCurrency(collected), 'text-emerald-600', 'bg-emerald-50', 'Archive')
          + _statCard('Pending', App.Utils.formatCurrency(pending), 'text-amber-600', 'bg-amber-50', 'Unpaid')
          + _statCard('Overdue', App.Utils.formatCurrency(overdue), 'text-red-600', 'bg-red-50', 'Overdue')
          + '</div>')

      + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm">'
      +   '<div class="p-4 border-b border-slate-100 flex items-center gap-2 flex-wrap">'
      // Filter tabs — hide 'Pending' for parents since they see 'Pending Verification' status inline
      +   (isClient ? ['Unpaid','Overdue','All','Archive'] : ['Unpaid','Overdue','Pending','All','Archive']).map(function(f) {
            const active = f === _filter;
            const isArchive = f === 'Archive';
            const isPending = f === 'Pending';
            const label = isArchive ? 'Paid' : isPending ? 'Awaiting Confirmation' + (pendingVerifCount > 0 ? ' (' + pendingVerifCount + ')' : '') : f;
            const activeClass = isArchive ? 'bg-slate-600 text-white' : isPending ? 'bg-purple-600 text-white' : 'bg-blue-600 text-white';
            return '<button onclick="App.Billing._setFilter(\'' + f + '\')" class="px-3 py-1.5 text-sm rounded-lg font-medium transition-colors '
              + (active ? activeClass : 'text-slate-600 hover:bg-slate-100')
              + '">' + label + '</button>';
          }).join('')
      // Student filter dropdown
      +   '<div class="ml-auto">'
      +     '<select onchange="App.Billing._setStudentFilter(this.value)" class="text-sm border border-slate-200 rounded-lg px-3 py-1.5 focus:outline-none focus:ring-2 focus:ring-blue-400 text-slate-600">'
      +     '<option value="">All Students</option>'
      +     (isClient ? students.filter(function(s) { return s.contact === App.clientParent; }) : students).map(function(s) {
              return '<option value="' + s.id + '"' + (s.id === _studentFilter ? ' selected' : '') + '>' + App.Utils.esc(s.firstName + ' ' + s.lastName) + '</option>';
            }).join('')
      +     '</select>'
      +   '</div>'
      +   '</div>'
      +   (isAdmin ? '<div id="inv-bulk-bar" style="padding:0 1rem">' + _bulkBar() + '</div>' : '')
      +   '<div class="overflow-x-auto">'
      +     '<table class="w-full" role="table">'
      +       '<caption class="sr-only">Invoice list</caption>'
      +       '<thead class="bg-slate-50 border-b border-slate-100"><tr>'
      +         (isAdmin ? '<th scope="col" class="th" style="width:36px"><input type="checkbox" id="select-all-inv-cb" onchange="App.Billing._toggleSelectAllInv(this.checked)" style="cursor:pointer"></th>' : '')
      +         '<th scope="col" class="th">Student</th><th scope="col" class="th">Description</th><th scope="col" class="th">Type</th>'
      +         '<th scope="col" class="th">Due Date</th><th scope="col" class="th text-right">Amount</th><th scope="col" class="th">Status</th>'
      +         (isAdmin || isClient ? '<th scope="col" class="th w-10"></th>' : '')
      +       '</tr></thead>'
      +       '<tbody class="divide-y divide-slate-50">'
      +       (filtered.length === 0
          ? '<tr><td colspan="' + colCount + '" style="padding:0">' + App.Utils.emptyState(
              (_filter !== 'All' || _studentFilter) ? 'No invoices match this filter' : 'No invoices yet',
              (_filter !== 'All' || _studentFilter) ? 'Try selecting a different filter or student.' : 'Create your first invoice to start tracking payments.',
              (isAdmin && _filter === 'All' && !_studentFilter) ? '<button onclick="App.Billing._createModal()" style="padding:0.5rem 1.25rem;font-size:0.83rem;font-weight:600;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">+ Create Invoice</button>' : ''
            ) + '</td></tr>'
          : paged.map(function(inv) {
              const stu = _studentMap[inv.studentId];
              const stuName = stu ? stu.firstName + ' ' + stu.lastName : inv.studentId;
              const isNearDue = inv.status === 'Unpaid' && new Date(inv.dueDate) <= in7 && new Date(inv.dueDate) >= today;
              return '<tr class="hover:bg-slate-50 transition-colors">'
                + (isAdmin ? '<td class="td" style="width:36px"><input type="checkbox" class="inv-cb" data-id="' + inv.id + '" onchange="App.Billing._toggleSelectInv(\'' + inv.id + '\',this.checked)" style="cursor:pointer"' + (_selectedInv[inv.id] ? ' checked' : '') + '></td>' : '')
                + '<td class="td"><div class="font-medium text-slate-800">' + App.Utils.esc(stuName) + '</div><div class="text-xs text-slate-400">' + inv.id + '</div></td>'
                + '<td class="td text-sm text-slate-600">' + App.Utils.esc(inv.description) + '</td>'
                + '<td class="td">' + App.Utils.badge(inv.type, inv.type === 'Monthly' ? 'blue' : 'purple') + '</td>'
                + '<td class="td text-sm ' + (isNearDue ? 'text-amber-600 font-medium' : 'text-slate-600') + '">'
                +   App.Utils.formatDate(inv.dueDate) + (isNearDue ? ' <span class="text-xs">(soon)</span>' : '')
                + '</td>'
                + '<td class="td text-sm font-semibold text-slate-800 text-right">' + App.Utils.formatCurrency(inv.amount) + (inv.discountPct > 0 ? '<br><span style="font-size:0.7rem;color:#92400e;font-weight:500">' + inv.discountPct + '% early bird</span>' : '') + (inv.referralCredit > 0 ? '<br><span style="font-size:0.7rem;color:#92400e;font-weight:500">−RM ' + inv.referralCredit.toFixed(2) + ' referral</span>' : '') + '</td>'
                + '<td class="td">'
                +   '<div style="display:flex;align-items:center;gap:4px">'
                +   App.Utils.statusBadge(inv.status)
                +   (inv.paymentProof ? '<a href="/api/' + App.Utils.esc(inv.paymentProof) + '" target="_blank" title="View receipt" style="display:inline-flex;align-items:center;color:#94a3b8;hover:color:#374151"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg></a>' : '')
                +   '</div>'
                +   (inv.status === 'Pending Verification' && inv.paymentMethod ? '<div style="font-size:0.68rem;color:#94a3b8;margin-top:3px">' + App.Utils.esc(inv.paymentMethod) + (inv.paidOn ? ' · ' + App.Utils.formatDate(inv.paidOn) : '') + '</div>' : '')
                + '</td>'
                + (isAdmin ? '<td class="td">'
                  + '<div class="relative flex justify-center">'
                  +   '<button onclick="App.Billing._toggleMenu(event,\'' + inv.id + '\')" class="w-7 h-7 flex items-center justify-center rounded-lg hover:bg-slate-100 text-slate-400 hover:text-slate-700 text-lg leading-none font-bold">&#8942;</button>'
                  +   '<div id="inv-menu-' + inv.id + '" class="inv-menu hidden absolute right-0 top-8 z-20 bg-white border border-slate-200 shadow-xl rounded-xl py-1 min-w-40">'
                  +     (inv.status === 'Pending Verification'
                          ? '<button onclick="App.Billing._verifyPaid(\'' + inv.id + '\')" class="w-full text-left px-4 py-2 text-sm hover:bg-green-50 text-green-700 font-semibold">Verify Payment</button>'
                            + '<button onclick="App.Billing._markPaid(\'' + inv.id + '\')" class="w-full text-left px-4 py-2 text-sm hover:bg-slate-50 text-slate-700">Override &amp; Mark Paid</button>'
                            + '<button onclick="App.Billing._markUnpaid(\'' + inv.id + '\')" class="w-full text-left px-4 py-2 text-sm hover:bg-slate-50 text-slate-700">Reject (Mark Unpaid)</button>'
                          : inv.status === 'Paid'
                          ? '<button onclick="App.Billing._markUnpaid(\'' + inv.id + '\')" class="w-full text-left px-4 py-2 text-sm hover:bg-slate-50 text-slate-700">Mark Unpaid</button>'
                          : '<button onclick="App.Billing._markPaid(\'' + inv.id + '\')" class="w-full text-left px-4 py-2 text-sm hover:bg-slate-50 text-slate-700">Mark as Paid</button>')
                  +     '<button onclick="App.Billing._editModal(\'' + inv.id + '\')" class="w-full text-left px-4 py-2 text-sm hover:bg-slate-50 text-slate-700">Edit</button>'
                  +     '<a href="/api/invoices/' + inv.id + '/pdf" target="_blank" class="block px-4 py-2 text-sm hover:bg-slate-50 text-slate-700">Download invoice</a>'
                  +     (inv.status === 'Paid' ? '<a href="/api/invoices/' + inv.id + '/receipt.pdf" target="_blank" class="block px-4 py-2 text-sm hover:bg-slate-50 text-slate-700">Download receipt</a>' : '')
                  +     '<div class="my-1 border-t border-slate-100"></div>'
                  +     '<button onclick="App.Billing._deleteInvoice(\'' + inv.id + '\')" class="w-full text-left px-4 py-2 text-sm hover:bg-red-50 text-red-600">Delete</button>'
                  +   '</div>'
                  + '</div>'
                  + '</td>'
                  : isClient && (inv.status === 'Unpaid' || inv.status === 'Overdue')
                  ? '<td class="td"><div style="display:flex;gap:0.5rem;align-items:center;justify-content:flex-end;flex-wrap:wrap"><a href="/api/invoices/' + inv.id + '/pdf" target="_blank" style="font-size:0.7rem;color:#475569;text-decoration:underline">Invoice PDF</a><button onclick="App.Billing._payOnline(\'' + inv.id + '\')" style="padding:0.3rem 0.75rem;font-size:0.75rem;font-weight:700;background:#0a0a0a;color:#ffffff;border:none;border-radius:7px;cursor:pointer;white-space:nowrap">Pay Online</button><button onclick="App.Billing._parentSubmitPaid(\'' + inv.id + '\')" style="padding:0.3rem 0.75rem;font-size:0.75rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:7px;cursor:pointer;white-space:nowrap">I\'ve Paid</button></div></td>'
                  : isClient && inv.status === 'Pending Verification'
                  ? '<td class="td"><span style="font-size:0.72rem;color:#7c3aed;font-weight:600">Awaiting confirmation</span></td>'
                  : isClient && inv.status === 'Paid'
                  ? '<td class="td" style="text-align:right"><a href="/api/invoices/' + inv.id + '/receipt.pdf" target="_blank" style="font-size:0.72rem;color:#15803d;font-weight:600;text-decoration:underline">Receipt PDF</a></td>'
                  : '<td></td>')
                + '</tr>';
            }).join(''))
      +       '</tbody>'
      +     '</table>'
      +   '</div>'
      +   _paginationControls(_billingPage, filtered.length, 'App.Billing._setPage')
      + '</div>';

    if (!_menuListenerAdded) {
      document.addEventListener('click', function() {
        document.querySelectorAll('.inv-menu').forEach(function(m) { m.classList.add('hidden'); });
      });
      _menuListenerAdded = true;
    }
  }

  function _bulkBar() {
    var count = Object.keys(_selectedInv).length;
    if (count === 0) return '';
    return '<div style="display:flex;align-items:center;gap:0.75rem;padding:0.65rem 1rem;background:var(--gold-dim);border:1px solid rgba(201,162,39,0.25);border-radius:10px;margin-bottom:0.75rem">'
      + '<span style="font-size:0.82rem;font-weight:700;color:#92400e">' + count + ' invoice' + (count !== 1 ? 's' : '') + ' selected</span>'
      + '<button onclick="App.Billing._bulkMarkPaid()" style="padding:0.35rem 0.85rem;font-size:0.75rem;font-weight:600;background:var(--gold);color:#0a0a0a;border:none;border-radius:7px;cursor:pointer">Mark All Paid</button>'
      + '<button onclick="App.Billing._bulkDeselectInv()" style="padding:0.35rem 0.85rem;font-size:0.75rem;font-weight:600;background:transparent;color:#92400e;border:1px solid rgba(201,162,39,0.3);border-radius:7px;cursor:pointer">Clear</button>'
      + '</div>';
  }

  function _toggleSelectAllInv(checked) {
    document.querySelectorAll('.inv-cb').forEach(function(cb) {
      cb.checked = checked;
      if (checked) {
        _selectedInv[cb.dataset.id] = true;
      } else {
        delete _selectedInv[cb.dataset.id];
      }
    });
    _refreshBulkBar();
  }

  function _toggleSelectInv(id, checked) {
    if (checked) _selectedInv[id] = true; else delete _selectedInv[id];
    _refreshBulkBar();
  }

  function _refreshBulkBar() {
    var bar = document.getElementById('inv-bulk-bar');
    if (bar) bar.innerHTML = _bulkBar();
  }

  function _bulkDeselectInv() {
    _selectedInv = {};
    document.querySelectorAll('.inv-cb, #select-all-inv-cb').forEach(function(cb) { cb.checked = false; });
    _refreshBulkBar();
  }

  function _bulkMarkPaid() {
    var ids = Object.keys(_selectedInv);
    if (ids.length === 0) return;
    var html = '<div class="p-6">'
      + '<h2 class="text-lg font-bold mb-1">Mark ' + ids.length + ' Invoice' + (ids.length !== 1 ? 's' : '') + ' as Paid</h2>'
      + '<p class="text-sm text-slate-500 mb-4">Select payment method received</p>'
      + '<div class="grid grid-cols-3 gap-3 mb-5">'
      + ['Cash', 'Bank Transfer', 'QR Pay'].map(function(m) {
          return '<button onclick="App.Billing._bulkConfirmPaid(\'' + m + '\')" class="p-3 border-2 border-slate-200 rounded-xl text-sm font-semibold text-slate-700 hover:border-yellow-400 hover:bg-yellow-50 transition-all text-center">' + m + '</button>';
        }).join('')
      + '</div>'
      + '<button onclick="App.Utils.hideModal()" class="w-full py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '</div>';
    App.Utils.showModal(html);
  }

  function _bulkConfirmPaid(method) {
    var ids = Object.keys(_selectedInv);
    var { invoices } = App.Store.get();
    var today = App.Utils.today();
    var updated = invoices.map(function(i) {
      return ids.indexOf(i.id) > -1 ? Object.assign({}, i, { status: 'Paid', paidOn: today, paymentMethod: method }) : i;
    });
    App.Store.set({ invoices: updated });
    App.Utils.hideModal(true);
    App.Utils.showToast('Marked ' + ids.length + ' invoice' + (ids.length !== 1 ? 's' : '') + ' as paid · ' + method, 'success');
    App.Notifs.refresh();
    _selectedInv = {};
    App.Router.refresh();
  }

  function _statCard(label, value, textClass, bgClass, filter) {
    var isActive = filter && filter === _filter;
    var activeStyle = isActive ? 'border-color:var(--gold);box-shadow:0 0 0 2px var(--gold-dim, rgba(201,162,39,0.18));' : '';
    var clickAttr = filter ? ' onclick="App.Billing._setFilter(\'' + filter + '\')" style="cursor:pointer;' + activeStyle + '"' : '';
    return '<div class="' + bgClass + ' rounded-xl border border-slate-100 shadow-sm p-4 transition-shadow hover:shadow-md"' + clickAttr + '>'
      + '<div class="text-xl font-bold ' + textClass + '">' + value + '</div>'
      + '<div class="text-xs text-slate-500 mt-1">' + label + '</div>'
      + '</div>';
  }

  function _setFilter(f) { _filter = f; _billingPage = 0; App.Router.refresh(); }
  function _setStudentFilter(v) { _studentFilter = v; _billingPage = 0; App.Router.refresh(); }
  function _setBillingPage(n) { _billingPage = Math.max(0, n); App.Router.refresh(); }

  function _toggleMenu(event, id) {
    event.stopPropagation();
    document.querySelectorAll('.inv-menu').forEach(function(m) { m.classList.add('hidden'); });
    const menu = document.getElementById('inv-menu-' + id);
    if (menu) menu.classList.remove('hidden');
  }

  function _markPaidModal(invId) {
    var html = '<div class="p-6">'
      + '<h2 class="text-lg font-bold mb-1">Confirm Payment</h2>'
      + '<p class="text-sm text-slate-500 mb-4">Select payment method received</p>'
      + '<div id="admin-payment-methods-grid" class="grid grid-cols-3 gap-3 mb-5">'
      // Cash — direct confirm
      + '<button onclick="App.Billing._confirmPaid(\'' + invId + '\',\'Cash\')" '
      +   'class="p-3 border-2 border-slate-200 rounded-xl text-sm font-semibold text-slate-700 hover:border-blue-400 hover:bg-blue-50 transition-all text-center">Cash</button>'
      // Bank Transfer — show admin proof upload
      + '<button onclick="App.Billing._showAdminProofUpload(\'' + invId + '\',\'Bank Transfer\')" '
      +   'class="p-3 border-2 border-slate-200 rounded-xl text-sm font-semibold text-slate-700 hover:border-blue-400 hover:bg-blue-50 transition-all text-center">Bank Transfer</button>'
      // QR Pay — show admin proof upload
      + '<button onclick="App.Billing._showAdminProofUpload(\'' + invId + '\',\'QR Pay\')" '
      +   'class="p-3 border-2 border-slate-200 rounded-xl text-sm font-semibold text-slate-700 hover:border-blue-400 hover:bg-blue-50 transition-all text-center">QR Pay</button>'
      + '</div>'
      // Admin proof upload area
      + '<div id="admin-proof-upload-area" style="display:none">'
      +   '<p style="font-size:0.82rem;font-weight:600;color:#374151;margin:0 0 0.4rem">Reference number <span style="color:#dc2626">*required</span></p>'
      +   '<input type="text" id="admin-payment-ref" placeholder="e.g. bank slip / QR txn ID" style="width:100%;padding:0.55rem 0.75rem;font-size:0.85rem;border:1px solid #e2e8f0;border-radius:8px;outline:none;margin-bottom:1rem">'
      +   '<p style="font-size:0.82rem;font-weight:600;color:#374151;margin:0 0 0.5rem">Upload payment receipt <span style="color:#dc2626">*required</span></p>'
      +   '<div style="border:2px dashed #e2e8f0;border-radius:10px;padding:1.5rem;text-align:center;cursor:pointer" onclick="document.getElementById(\'admin-proof-file\').click()">'
      +     '<input type="file" id="admin-proof-file" accept="image/*,.pdf" style="display:none" onchange="App.Billing._previewAdminProof(this)">'
      +     '<div id="admin-proof-preview" style="margin-bottom:0.5rem"></div>'
      +     '<p id="admin-proof-placeholder" style="font-size:0.8rem;color:#94a3b8;margin:0">Click to upload receipt image or PDF</p>'
      +   '</div>'
      +   '<div style="display:flex;gap:0.5rem;margin-top:1rem">'
      +     '<button id="admin-proof-submit-btn" style="flex:1;padding:0.55rem;font-size:0.85rem;font-weight:700;background:#3b82f6;color:#fff;border:none;border-radius:8px;cursor:pointer">Confirm Payment</button>'
      +     '<button onclick="App.Billing._showAdminPaymentMethods()" style="padding:0.55rem 1rem;font-size:0.83rem;border:1px solid #e2e8f0;border-radius:8px;background:#fff;cursor:pointer;color:#64748b">Back</button>'
      +   '</div>'
      + '</div>'
      + '<button id="admin-cancel-btn" onclick="App.Utils.hideModal()" class="w-full py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '</div>';
    App.Utils.showModal(html);
  }

  var _adminProofInvId = '';
  var _adminProofMethod = '';

  function _showAdminProofUpload(invId, method) {
    _adminProofInvId = invId;
    _adminProofMethod = method;
    var uploadArea = document.getElementById('admin-proof-upload-area');
    var methodGrid = document.getElementById('admin-payment-methods-grid');
    var cancelBtn = document.getElementById('admin-cancel-btn');
    if (uploadArea) uploadArea.style.display = 'block';
    if (methodGrid) methodGrid.style.display = 'none';
    if (cancelBtn) cancelBtn.style.display = 'none';
    var submitBtn = document.getElementById('admin-proof-submit-btn');
    if (submitBtn) {
      submitBtn.onclick = function() { App.Billing._adminSubmitWithProof(_adminProofInvId, _adminProofMethod); };
    }
  }

  function _showAdminPaymentMethods() {
    var uploadArea = document.getElementById('admin-proof-upload-area');
    var methodGrid = document.getElementById('admin-payment-methods-grid');
    var cancelBtn = document.getElementById('admin-cancel-btn');
    if (uploadArea) uploadArea.style.display = 'none';
    if (methodGrid) methodGrid.style.display = 'grid';
    if (cancelBtn) cancelBtn.style.display = 'block';
    var fileInput = document.getElementById('admin-proof-file');
    if (fileInput) fileInput.value = '';
    var preview = document.getElementById('admin-proof-preview');
    if (preview) preview.innerHTML = '';
    var placeholder = document.getElementById('admin-proof-placeholder');
    if (placeholder) placeholder.style.display = 'block';
  }

  function _previewAdminProof(input) {
    var preview = document.getElementById('admin-proof-preview');
    var placeholder = document.getElementById('admin-proof-placeholder');
    if (!preview || !input.files || !input.files[0]) return;
    var file = input.files[0];
    if (file.type.startsWith('image/')) {
      var reader = new FileReader();
      reader.onload = function(e) {
        preview.innerHTML = '<img src="' + e.target.result + '" style="max-width:200px;max-height:150px;border-radius:8px;margin:0 auto;display:block;border:1px solid #e2e8f0">';
        if (placeholder) placeholder.style.display = 'none';
      };
      reader.readAsDataURL(file);
    } else {
      preview.innerHTML = '<div style="display:flex;align-items:center;justify-content:center;gap:0.5rem;padding:0.5rem;background:#f8fafc;border-radius:8px;border:1px solid #e2e8f0">'
        + '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#ef4444" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>'
        + '<span style="font-size:0.82rem;font-weight:600;color:#374151">' + App.Utils.esc(file.name) + '</span>'
        + '</div>';
      if (placeholder) placeholder.style.display = 'none';
    }
  }

  function _adminSubmitWithProof(invId, method) {
    var refInput = document.getElementById('admin-payment-ref');
    var refNo = refInput ? refInput.value.trim() : '';
    if (!refNo) {
      App.Utils.showToast('Reference number is required for ' + method, 'error');
      if (refInput) refInput.focus();
      return;
    }

    var fileInput = document.getElementById('admin-proof-file');
    var hasFile = fileInput && fileInput.files && fileInput.files[0];
    if (!hasFile) {
      App.Utils.showToast('Receipt upload is required for ' + method, 'error');
      return;
    }

    if (hasFile) {
      var submitBtn = document.getElementById('admin-proof-submit-btn');
      if (submitBtn) { submitBtn.disabled = true; submitBtn.textContent = 'Uploading...'; }

      var formData = new FormData();
      formData.append('proof', fileInput.files[0]);
      formData.append('invoiceId', invId);

      fetch('/api/upload-proof', {
        method: 'POST',
        body: formData,
        credentials: 'include',
        headers: (function(){ var m=document.cookie.match(/(?:^|;\s*)sh_csrf=([^;]+)/); return m?{'X-CSRF-Token':decodeURIComponent(m[1])}:{}; })()
      })
      .then(function(res) {
        if (!res.ok) throw new Error('Upload failed');
        return res.json();
      })
      .then(function(data) {
        var state = App.Store.get();
        App.Store.set({ invoices: state.invoices.map(function(i) {
          return i.id === invId ? Object.assign({}, i, { paymentProof: data.path }) : i;
        })});
        _confirmPaid(invId, method, refNo);
      })
      .catch(function() {
        if (submitBtn) { submitBtn.disabled = false; submitBtn.textContent = 'Confirm Payment'; }
        App.Utils.showToast('Receipt upload failed — marking paid without receipt', 'warning');
        _confirmPaid(invId, method, refNo);
      });
    } else {
      _confirmPaid(invId, method, refNo);
    }
  }

  function _confirmPaid(invId, method, refNo) {
    var { invoices } = App.Store.get();
    var inv = invoices.find(function(i) { return i.id === invId; });
    if (!inv) return;
    var payload = { status: 'Paid', paidOn: App.Utils.today(), paymentMethod: method };
    if (refNo) payload.referenceNo = refNo;
    App.Api.put('/api/invoices/' + invId + '/pay', payload)
      .then(function() {
        return App.Api.loadSnapshot();
      }).then(function() {
        _checkReferralMilestoneClient(inv.studentId);
        App.Utils.hideModal(true);
        App.Utils.showToast('Marked paid · ' + method, 'success');
        App.Notifs.refresh();
        App.Router.refresh();
      });
    // No local fallback: when the server rejects (e.g. missing reference
    // number for non-cash), App.Api auto-toasts the error and we leave the
    // status untouched. The previous fallback wrote Paid locally even on a
    // 5xx, then the next snapshot reload silently reverted it — making the
    // admin think the payment was logged when it wasn't.
  }

  // _checkReferralMilestoneClient counts paid Monthly invoices for a student
  // and, if the count just reached 3 and there's a pending referral_rewards row
  // for them, calls the backend earn endpoint. We do this on the client because
  // invoices currently live in localStorage — once invoices fully migrate to
  // the backend, the server-side referralCheckMilestoneOnPay hook will take
  // over and this client-side check becomes a redundant safety net.
  function _checkReferralMilestoneClient(studentId) {
    var state = App.Store.get();
    var rewards = state.referralRewards || [];
    var pending = rewards.find(function(r) { return r.referredStudentId === studentId && r.status === 'pending'; });
    if (!pending) return;
    var paidCount = (state.invoices || []).filter(function(i) {
      return i.studentId === studentId && i.type === 'Monthly' && i.status === 'Paid';
    }).length;
    if (paidCount < 3) return;
    App.Api.post('/api/referrals/' + pending.id + '/earn', {})
      .then(function() {
        App.Utils.showToast('Referral milestone reached — RM10/month credit earned for referrer', 'success');
        return App.Api.loadSnapshot();
      })
      .catch(function() {});
  }

  function _markPaid(invoiceId) {
    _markPaidModal(invoiceId);
  }

  function _markUnpaid(invoiceId) {
    App.Utils.hideModal(true);
    App.Api.put('/api/invoices/' + invoiceId + '/pay', { status: 'Unpaid' })
      .then(function() { return App.Api.loadSnapshot(); })
      .then(function() {
        App.Utils.showToast('Invoice marked as unpaid', 'info');
        App.Router.refresh();
      }).catch(function() {
        // Fallback local
        var state = App.Store.get();
        App.Store.set({ invoices: state.invoices.map(function(inv) {
          return inv.id === invoiceId ? Object.assign({}, inv, { status: 'Unpaid', paidOn: null, paymentMethod: null, submittedByParent: false }) : inv;
        })});
        App.Utils.showToast('Invoice marked as unpaid (offline)', 'warning');
        App.Router.refresh();
      });
  }

  function _parentSubmitPaid(invId) {
    const inv = App.Store.get().invoices.find(function(i) { return i.id === invId; });
    if (!inv) return;
    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 style="font-size:1.1rem;font-weight:700;color:#111;margin:0 0 0.25rem">Submit Payment</h2>'
      + '<p style="font-size:0.82rem;color:#94a3b8;margin:0 0 1.25rem">Let admin know you\'ve paid — they\'ll confirm receipt.</p>'
      + '<div style="background:#f8fafc;border-radius:10px;padding:0.85rem 1rem;margin-bottom:1.25rem">'
      +   '<div style="font-size:0.78rem;color:#94a3b8">Invoice</div>'
      +   '<div style="font-size:0.9rem;font-weight:700;color:#111">' + App.Utils.esc(inv.description) + '</div>'
      +   '<div style="font-size:1rem;font-weight:800;color:var(--gold);margin-top:2px">' + App.Utils.formatCurrency(inv.amount) + '</div>'
      + '</div>'
      + '<p style="font-size:0.82rem;font-weight:600;color:#374151;margin:0 0 0.6rem">How did you pay?</p>'
      + '<div id="payment-methods-grid" style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:0.5rem;margin-bottom:1.25rem">'
      // Cash — direct submit (no proof needed)
      + '<button onclick="App.Billing._parentConfirmSubmit(\'' + invId + '\',\'Cash\')" '
      +   'style="padding:0.65rem 0.5rem;border:2px solid #e2e8f0;border-radius:10px;font-size:0.8rem;font-weight:600;color:#374151;background:#fff;cursor:pointer;text-align:center;transition:all 0.15s" '
      +   'onmouseover="this.style.borderColor=\'var(--gold)\';this.style.color=\'var(--gold)\'" '
      +   'onmouseout="this.style.borderColor=\'#e2e8f0\';this.style.color=\'#374151\'">Cash</button>'
      // Bank Transfer — show proof upload
      + '<button onclick="App.Billing._showProofUpload(\'' + invId + '\',\'Bank Transfer\')" '
      +   'style="padding:0.65rem 0.5rem;border:2px solid #e2e8f0;border-radius:10px;font-size:0.8rem;font-weight:600;color:#374151;background:#fff;cursor:pointer;text-align:center;transition:all 0.15s" '
      +   'onmouseover="this.style.borderColor=\'var(--gold)\';this.style.color=\'var(--gold)\'" '
      +   'onmouseout="this.style.borderColor=\'#e2e8f0\';this.style.color=\'#374151\'">Bank Transfer</button>'
      // QR Pay — show proof upload
      + '<button onclick="App.Billing._showProofUpload(\'' + invId + '\',\'QR Pay\')" '
      +   'style="padding:0.65rem 0.5rem;border:2px solid #e2e8f0;border-radius:10px;font-size:0.8rem;font-weight:600;color:#374151;background:#fff;cursor:pointer;text-align:center;transition:all 0.15s" '
      +   'onmouseover="this.style.borderColor=\'var(--gold)\';this.style.color=\'var(--gold)\'" '
      +   'onmouseout="this.style.borderColor=\'#e2e8f0\';this.style.color=\'#374151\'">QR Pay</button>'
      + '</div>'
      // Proof upload area (hidden initially)
      + '<div id="proof-upload-area" style="display:none">'
      +   '<p style="font-size:0.82rem;font-weight:600;color:#374151;margin:0 0 0.4rem">Reference number <span style="color:#dc2626">*required</span></p>'
      +   '<input type="text" id="parent-payment-ref" placeholder="bank slip or transaction ID" style="width:100%;padding:0.55rem 0.75rem;font-size:0.85rem;border:1px solid #e2e8f0;border-radius:8px;outline:none;margin-bottom:1rem">'
      +   '<p style="font-size:0.82rem;font-weight:600;color:#374151;margin:0 0 0.5rem">Upload payment receipt <span style="color:#dc2626">*required</span></p>'
      +   '<div id="proof-drop-zone" style="border:2px dashed #e2e8f0;border-radius:10px;padding:1.5rem;text-align:center;cursor:pointer" onclick="document.getElementById(\'proof-file\').click()">'
      +     '<input type="file" id="proof-file" accept="image/*,.pdf" style="display:none" onchange="App.Billing._previewProof(this)">'
      +     '<div id="proof-preview" style="margin-bottom:0.5rem"></div>'
      +     '<p id="proof-placeholder" style="font-size:0.8rem;color:#94a3b8;margin:0">Click to upload receipt image or PDF</p>'
      +   '</div>'
      +   '<div style="display:flex;gap:0.5rem;margin-top:1rem">'
      +     '<button id="proof-submit-btn" style="flex:1;padding:0.55rem;font-size:0.85rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Submit Payment</button>'
      +     '<button onclick="App.Billing._showPaymentMethods()" style="padding:0.55rem 1rem;font-size:0.83rem;border:1px solid #e2e8f0;border-radius:8px;background:#fff;cursor:pointer;color:#64748b">Back</button>'
      +   '</div>'
      + '</div>'
      + '<button id="cancel-payment-btn" onclick="App.Utils.hideModal()" style="width:100%;padding:0.5rem;font-size:0.83rem;border:1px solid #e2e8f0;border-radius:8px;background:#fff;cursor:pointer;color:#64748b">Cancel</button>'
      + '</div>'
    );
  }

  var _proofInvId = '';
  var _proofMethod = '';

  function _showProofUpload(invId, method) {
    _proofInvId = invId;
    _proofMethod = method;
    var uploadArea = document.getElementById('proof-upload-area');
    var methodGrid = document.getElementById('payment-methods-grid');
    var cancelBtn = document.getElementById('cancel-payment-btn');
    if (uploadArea) uploadArea.style.display = 'block';
    if (methodGrid) methodGrid.style.display = 'none';
    if (cancelBtn) cancelBtn.style.display = 'none';
    // Wire up submit button
    var submitBtn = document.getElementById('proof-submit-btn');
    if (submitBtn) {
      submitBtn.onclick = function() { App.Billing._parentSubmitWithProof(_proofInvId, _proofMethod); };
    }
  }

  function _showPaymentMethods() {
    var uploadArea = document.getElementById('proof-upload-area');
    var methodGrid = document.getElementById('payment-methods-grid');
    var cancelBtn = document.getElementById('cancel-payment-btn');
    if (uploadArea) uploadArea.style.display = 'none';
    if (methodGrid) methodGrid.style.display = 'grid';
    if (cancelBtn) cancelBtn.style.display = 'block';
    // Reset file input
    var fileInput = document.getElementById('proof-file');
    if (fileInput) fileInput.value = '';
    var preview = document.getElementById('proof-preview');
    if (preview) preview.innerHTML = '';
    var placeholder = document.getElementById('proof-placeholder');
    if (placeholder) placeholder.style.display = 'block';
  }

  function _previewProof(input) {
    var preview = document.getElementById('proof-preview');
    var placeholder = document.getElementById('proof-placeholder');
    if (!preview || !input.files || !input.files[0]) return;
    var file = input.files[0];
    if (file.type.startsWith('image/')) {
      var reader = new FileReader();
      reader.onload = function(e) {
        preview.innerHTML = '<img src="' + e.target.result + '" style="max-width:200px;max-height:150px;border-radius:8px;margin:0 auto;display:block;border:1px solid #e2e8f0">';
        if (placeholder) placeholder.style.display = 'none';
      };
      reader.readAsDataURL(file);
    } else {
      // PDF
      preview.innerHTML = '<div style="display:flex;align-items:center;justify-content:center;gap:0.5rem;padding:0.5rem;background:#f8fafc;border-radius:8px;border:1px solid #e2e8f0">'
        + '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#ef4444" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>'
        + '<span style="font-size:0.82rem;font-weight:600;color:#374151">' + App.Utils.esc(file.name) + '</span>'
        + '</div>';
      if (placeholder) placeholder.style.display = 'none';
    }
  }

  function _parentSubmitWithProof(invId, method) {
    var refInput = document.getElementById('parent-payment-ref');
    var refNo = refInput ? refInput.value.trim() : '';
    if (!refNo) {
      App.Utils.showToast('Reference number is required for ' + method, 'error');
      if (refInput) refInput.focus();
      return;
    }

    var fileInput = document.getElementById('proof-file');
    var hasFile = fileInput && fileInput.files && fileInput.files[0];
    if (!hasFile) {
      App.Utils.showToast('Receipt upload is required for ' + method, 'error');
      return;
    }

    if (hasFile) {
      var submitBtn = document.getElementById('proof-submit-btn');
      if (submitBtn) { submitBtn.disabled = true; submitBtn.textContent = 'Uploading...'; }

      var formData = new FormData();
      formData.append('proof', fileInput.files[0]);
      formData.append('invoiceId', invId);

      fetch('/api/upload-proof', {
        method: 'POST',
        body: formData,
        credentials: 'include',
        headers: (function(){ var m=document.cookie.match(/(?:^|;\s*)sh_csrf=([^;]+)/); return m?{'X-CSRF-Token':decodeURIComponent(m[1])}:{}; })()
      })
      .then(function(res) {
        if (!res.ok) throw new Error('Upload failed');
        return res.json();
      })
      .then(function(data) {
        // Store proof path on local invoice data
        var state = App.Store.get();
        App.Store.set({ invoices: state.invoices.map(function(i) {
          return i.id === invId ? Object.assign({}, i, { paymentProof: data.path }) : i;
        })});
        _parentConfirmSubmit(invId, method, refNo);
      })
      .catch(function(err) {
        if (submitBtn) { submitBtn.disabled = false; submitBtn.textContent = 'Submit Payment'; }
        App.Utils.showToast('Receipt upload failed — submitting without receipt', 'warning');
        _parentConfirmSubmit(invId, method, refNo);
      });
    } else {
      // No file — submit anyway
      _parentConfirmSubmit(invId, method, refNo);
    }
  }

  function _parentConfirmSubmit(invId, method, refNo) {
    App.Utils.hideModal(true);
    // Call the backend so the payment submission actually persists.
    // Send status="Pending Verification" so admin knows the parent claims
    // they've paid but it hasn't been confirmed yet.
    var payload = {
      status: 'Pending Verification',
      paymentMethod: method
    };
    if (refNo) payload.referenceNo = refNo;
    App.Api.put('/api/invoices/' + invId + '/pay', payload).then(function() {
      return App.Api.loadSnapshot();
    }).then(function() {
      App.Utils.showToast('Payment submitted — admin will verify shortly', 'success');
      App.Notifs && App.Notifs.refresh && App.Notifs.refresh();
      App.Router.refresh();
    }).catch(function() {
      // Fallback: update locally so the UI isn't stuck
      var state = App.Store.get();
      App.Store.set({ invoices: state.invoices.map(function(inv) {
        return inv.id === invId ? Object.assign({}, inv, { status: 'Pending Verification', paidOn: App.Utils.today(), paymentMethod: method, submittedByParent: true }) : inv;
      })});
      App.Utils.showToast('Payment submitted (offline — will sync later)', 'warning');
      App.Router.refresh();
    });
  }

  function _verifyPaid(invId) {
    const state = App.Store.get();
    const inv = state.invoices.find(function(i) { return i.id === invId; });
    if (!inv) return;
    const stu = state.students.find(function(s) { return s.id === inv.studentId; });
    const stuName = stu ? stu.firstName + ' ' + stu.lastName : inv.studentId;

    var proofSection = '';
    if (inv.paymentProof) {
      var isImage = /\.(jpg|jpeg|png)$/i.test(inv.paymentProof);
      if (isImage) {
        proofSection = '<div style="margin-bottom:1rem">'
          + '<p style="font-size:0.82rem;font-weight:600;color:#374151;margin:0 0 0.5rem">Payment Receipt</p>'
          + '<a href="/api/' + App.Utils.esc(inv.paymentProof) + '" target="_blank">'
          + '<img src="/api/' + App.Utils.esc(inv.paymentProof) + '" style="max-width:100%;max-height:300px;border-radius:10px;border:1px solid #e2e8f0;cursor:pointer">'
          + '</a>'
          + '</div>';
      } else {
        proofSection = '<div style="margin-bottom:1rem">'
          + '<p style="font-size:0.82rem;font-weight:600;color:#374151;margin:0 0 0.5rem">Payment Receipt</p>'
          + '<a href="/api/' + App.Utils.esc(inv.paymentProof) + '" target="_blank" style="display:inline-flex;align-items:center;gap:0.5rem;padding:0.5rem 1rem;background:#f8fafc;border:1px solid #e2e8f0;border-radius:8px;text-decoration:none;color:#374151;font-size:0.82rem;font-weight:600">'
          + '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#ef4444" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>'
          + 'View PDF Receipt'
          + '</a></div>';
      }
    } else {
      proofSection = '<div style="margin-bottom:1rem;padding:0.75rem;background:#fefce8;border:1px solid #fde68a;border-radius:8px;font-size:0.8rem;color:#92400e">'
        + 'No receipt uploaded by parent.'
        + '</div>';
    }

    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 style="font-size:1.1rem;font-weight:700;color:#111;margin:0 0 0.25rem">Verify Payment</h2>'
      + '<p style="font-size:0.82rem;color:#94a3b8;margin:0 0 1rem">' + App.Utils.esc(stuName) + ' · ' + App.Utils.esc(inv.id) + '</p>'
      + '<div style="background:#f8fafc;border-radius:10px;padding:0.85rem 1rem;margin-bottom:1rem">'
      +   '<div style="display:flex;justify-content:space-between;align-items:center">'
      +     '<div>'
      +       '<div style="font-size:0.78rem;color:#94a3b8">' + App.Utils.esc(inv.paymentMethod || 'Unknown method') + '</div>'
      +       '<div style="font-size:0.9rem;font-weight:700;color:#111">' + App.Utils.esc(inv.description) + '</div>'
      +     '</div>'
      +     '<div style="font-size:1rem;font-weight:800;color:var(--gold)">' + App.Utils.formatCurrency(inv.amount) + '</div>'
      +   '</div>'
      + '</div>'
      + proofSection
      + '<div style="display:flex;gap:0.5rem">'
      +   '<button onclick="App.Billing._confirmVerify(\'' + invId + '\')" style="flex:1;padding:0.55rem;font-size:0.85rem;font-weight:700;background:#16a34a;color:#fff;border:none;border-radius:8px;cursor:pointer">Confirm Payment</button>'
      +   '<button onclick="App.Billing._markUnpaid(\'' + invId + '\')" style="padding:0.55rem 1rem;font-size:0.83rem;border:1px solid #fca5a5;border-radius:8px;background:#fff;cursor:pointer;color:#dc2626;font-weight:600">Reject</button>'
      + '</div>'
      + '<button onclick="App.Utils.hideModal()" style="width:100%;padding:0.5rem;font-size:0.83rem;border:1px solid #e2e8f0;border-radius:8px;background:#fff;cursor:pointer;color:#64748b;margin-top:0.5rem">Cancel</button>'
      + '</div>'
    );
  }

  function _confirmVerify(invId) {
    const state = App.Store.get();
    App.Store.set({ invoices: state.invoices.map(function(i) {
      return i.id === invId
        ? Object.assign({}, i, { status: 'Paid', verifiedOn: App.Utils.today(), verifiedBy: 'Admin' })
        : i;
    })});
    App.Utils.hideModal(true);
    App.Utils.showToast('Payment verified — invoice marked as Paid', 'success');
    App.Notifs && App.Notifs.refresh && App.Notifs.refresh();
    App.Router.refresh();
  }

  async function _deleteInvoice(invoiceId) {
    var ok = await App.Utils.showConfirm({ title: 'Delete invoice', message: 'This will be voided and removed from active reports.', confirmLabel: 'Delete', danger: true });
    if (!ok) return;
    var prev = App.Api.optimisticRemove('invoices', invoiceId);
    App.Router.refresh();

    // Optimistic + undoable: defer the actual DELETE for ~6s so the user
    // can hit Undo from the toast. Pressing Undo restores the local
    // store and cancels the pending DELETE.
    var cancelled = false;
    App.Utils.showToast('Invoice deleted', 'info', 6000, {
      action: {
        label: 'Undo',
        onClick: function() {
          cancelled = true;
          App.Store.set({ invoices: prev });
          App.Router.refresh();
        }
      }
    });
    setTimeout(async function() {
      if (cancelled) return;
      try {
        await App.Api.del('/api/invoices/' + invoiceId);
        App.Api.loadSnapshot().catch(function(){});
      } catch (err) {
        App.Store.set({ invoices: prev });
        App.Router.refresh();
      }
    }, 5500);
  }

  // _payOnline kicks off a hosted-checkout flow with the configured gateway
  // (Billplz / Stripe). The server returns a redirect URL; we navigate to it
  // and the gateway POSTs back to /api/payments/webhook/* on payment.
  async function _payOnline(invoiceId) {
    try {
      var res = await App.Api.post('/api/invoices/' + invoiceId + '/checkout', {});
      if (res && res.url) {
        window.location.href = res.url;
      } else {
        App.Utils.showToast('Could not start checkout', 'error');
      }
    } catch (err) {
      // App.Api auto-toasts; if the gateway is unconfigured it returns 502.
    }
  }

  // _generateMonthly fires the manual cron — useful when admin needs to
  // catch up after a missed window or after onboarding new students mid-month.
  // The backend runs in a goroutine and returns 202 immediately.
  async function _generateMonthly() {
    var ok = await App.Utils.showConfirm({
      title: 'Generate monthly invoices?',
      message: 'This will create this month\'s subscription invoices + last month\'s payroll for any active student/staff that doesn\'t already have one. Safe to run multiple times — duplicates are skipped.',
      confirmLabel: 'Run',
    });
    if (!ok) return;
    try {
      await App.Api.post('/api/admin/cron/run-monthly-invoices', {});
      App.Utils.showToast('Generation started — refresh in ~30 seconds to see new rows', 'info');
      setTimeout(function() { App.Api.loadSnapshot().then(function() { App.Router.refresh(); }); }, 25000);
    } catch (err) {
      // Auto-toasted (e.g. 409 if another run is in flight).
    }
  }

  function _editModal(invoiceId) {
    const state = App.Store.get();
    const inv = state.invoices.find(function(i) { return i.id === invoiceId; });
    if (!inv) return;
    const stu = state.students.find(function(s) { return s.id === inv.studentId; });
    // Preserve the invoice's existing type. The picker offers Monthly/Adhoc, but
    // system types (e.g. "Self-study", "Self-study Overflow") must stay intact —
    // otherwise editing such an invoice's date/amount would silently reclassify
    // it as Monthly and corrupt billing reports + the overflow dedup.
    const editTypes = ['Monthly', 'Adhoc'];
    if (inv.type && editTypes.indexOf(inv.type) === -1) editTypes.push(inv.type);
    const typeOptions = editTypes.map(function(t) {
      return '<option' + (inv.type === t ? ' selected' : '') + '>' + App.Utils.esc(t) + '</option>';
    }).join('');
    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-1">Edit Invoice</h2>'
      + '<p class="text-sm text-slate-500 mb-5">' + App.Utils.esc(stu ? stu.firstName + ' ' + stu.lastName : inv.studentId) + ' · ' + inv.id + '</p>'
      + '<form id="edit-invoice-form" class="space-y-4">'
      + _field('Description', '<input name="description" class="form-input" value="' + App.Utils.esc(inv.description) + '" required>')
      + '<div class="grid grid-cols-2 gap-4">'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Type</label><select name="type" class="form-input">' + typeOptions + '</select></div>'
      + _field('Amount (RM)', '<input name="amount" type="number" min="0" step="0.01" class="form-input" value="' + inv.amount + '" required>')
      + '</div>'
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Invoice Date', '<input name="invoiceDate" type="date" class="form-input" value="' + (inv.createdOn || '') + '" required>')
      + _field('Due Date', '<input name="dueDate" type="date" class="form-input" value="' + inv.dueDate + '" required>')
      + '</div>'
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Save Changes</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );
    document.getElementById('edit-invoice-form').addEventListener('submit', function(e) {
      e.preventDefault();
      const fd = new FormData(e.target);
      const payload = {
        description: fd.get('description'),
        type: fd.get('type'),
        amount: parseFloat(fd.get('amount')),
        dueDate: fd.get('dueDate'),
        createdOn: fd.get('invoiceDate')
      };
      App.Utils.hideModal(true);
      App.Api.put('/api/invoices/' + invoiceId, payload).then(function() {
        return App.Api.loadSnapshot();
      }).then(function() {
        App.Utils.showToast('Invoice updated', 'success');
        App.Router.refresh();
      }).catch(function() {
        // Error already toasted by App.Api wrapper.
      });
    });
  }

  function _createModal() {
    var state = App.Store.get();
    var students = state.students || [];
    // Manual self-study billing is only for casual drop-ins. Package students are
    // auto-billed for overflow by the cron, so excluding them here makes manual
    // double-charging impossible.
    var dropInStudents = students.filter(function(s) { return s.dropinSelfStudy; });
    var now = new Date();
    var defaultMonth = now.getFullYear() + '-' + String(now.getMonth() + 1).padStart(2,'0');
    var lastDay = new Date(now.getFullYear(), now.getMonth() + 1, 0);
    var defaultDue = lastDay.getFullYear() + '-' + String(lastDay.getMonth() + 1).padStart(2,'0') + '-' + String(lastDay.getDate()).padStart(2,'0');

    // Families with 2+ children for sibling option
    var byParent = {};
    students.forEach(function(s) { if (s.contact) { byParent[s.contact] = byParent[s.contact] || []; byParent[s.contact].push(s); } });
    var families = Object.keys(byParent).filter(function(e) { return byParent[e].length >= 2; });

    var modeTabStyle = 'padding:0.4rem 0.9rem;border-radius:8px;font-size:0.8rem;font-weight:600;cursor:pointer;border:2px solid transparent;transition:all 0.15s;';
    var modeActiveStyle = 'background:var(--gold);color:#0a0a0a;border-color:var(--gold);';
    var modeInactiveStyle = 'background:transparent;color:#64748b;border-color:#e2e8f0;';

    App.Utils.showModal(
      '<div class="p-6" style="min-width:440px;max-width:560px">'
      + '<h2 class="text-xl font-bold mb-1">Create Invoice</h2>'
      + '<p class="text-sm text-slate-500 mb-4">Choose an invoice type below.</p>'

      // ── Mode tabs ──
      + '<div id="inv-mode-tabs" style="display:flex;gap:0.5rem;margin-bottom:1.25rem;">'
      + '<button type="button" data-mode="single" onclick="App.Billing._setInvMode(\'single\')" style="' + modeTabStyle + modeActiveStyle + '">Single</button>'
      + '<button type="button" data-mode="monthly" onclick="App.Billing._setInvMode(\'monthly\')" style="' + modeTabStyle + modeInactiveStyle + '">Monthly Batch</button>'
      + (families.length > 0
          ? '<button type="button" data-mode="sibling" onclick="App.Billing._setInvMode(\'sibling\')" style="' + modeTabStyle + modeInactiveStyle + '">Sibling</button>'
          : '')
      + '<button type="button" data-mode="selfstudy" onclick="App.Billing._setInvMode(\'selfstudy\')" style="' + modeTabStyle + modeInactiveStyle + '">Self-Study</button>'
      + '</div>'

      + '<form id="create-invoice-form" class="space-y-4">'

      // ── SINGLE fields ──
      + '<div id="inv-single-fields">'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Student</label>'
      + '<select name="studentId" class="form-input">'
      + '<option value="">Select student...</option>'
      + students.map(function(s) {
          return '<option value="' + s.id + '"' + (s.id === _studentFilter ? ' selected' : '') + '>' + App.Utils.esc(s.firstName + ' ' + s.lastName) + '</option>';
        }).join('')
      + '</select></div>'
      + _field('Description', '<input name="description" class="form-input" placeholder="e.g. Mar 2026 Tuition">')
      + '<div class="grid grid-cols-2 gap-4">'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Type</label><select name="type" class="form-input"><option>Monthly</option><option>Adhoc</option></select></div>'
      + _field('Amount (RM)', '<input id="inv-base-amount" name="amount" type="number" min="0" step="0.01" class="form-input" oninput="App.Billing._updateNetAmount()">')
      + '</div>'
      + '</div>'

      // ── MONTHLY BATCH fields ──
      + '<div id="inv-monthly-fields" style="display:none">'
      + '<p class="text-xs text-slate-500 mb-3">Creates one invoice per active student for the selected month. Already-invoiced students are skipped.</p>'
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Month', '<input name="month" type="month" class="form-input" value="' + defaultMonth + '" onchange="App.Billing._previewMonthly(this.value)">')
      + _field('Amount per student (RM)', '<input id="gen-amount" name="genAmount" type="number" min="0" step="0.01" class="form-input" value="150" oninput="App.Billing._updateGenPreview()">')
      + '</div>'
      + '<div id="gen-preview" style="background:#f0fdf4;border:1px solid #bbf7d0;border-radius:10px;padding:0.75rem;font-size:0.82rem;color:#166534;margin-top:0.5rem"></div>'
      + '</div>'

      // ── SIBLING fields ──
      + '<div id="inv-sibling-fields" style="display:none">'
      + '<p class="text-xs text-slate-500 mb-3">Create one combined invoice covering multiple children from the same family.</p>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Family</label>'
      + '<select id="sibling-family-select" name="parentEmail" class="form-input" onchange="App.Billing._updateSiblingChildren(this.value)">'
      + '<option value="">Select family...</option>'
      + families.map(function(email) {
          var children = byParent[email];
          var label = children[0].parentName + ' (' + children.map(function(c) { return c.firstName; }).join(', ') + ')';
          return '<option value="' + App.Utils.esc(email) + '">' + App.Utils.esc(label) + '</option>';
        }).join('')
      + '</select></div>'
      + '<div id="sibling-children-list" style="display:none;background:#fafaf8;border:1px solid #f0ede8;border-radius:10px;padding:0.75rem">'
      +   '<p class="text-xs font-semibold text-slate-500 mb-2">Include children:</p>'
      +   '<div id="sibling-children-checks"></div>'
      + '</div>'
      + _field('Description', '<input name="sibDescription" class="form-input" value="' + now.toLocaleDateString('en-MY', { month: 'long', year: 'numeric' }) + ' Tuition">')
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Amount per child (RM)', '<input id="sibling-per-child" name="amountPerChild" type="number" min="0" step="0.01" class="form-input" value="150" oninput="App.Billing._updateSiblingTotal()">')
      + _field('Sibling Discount %', '<input id="sibling-discount" name="siblingDiscount" type="number" min="0" max="100" step="1" class="form-input" value="10" oninput="App.Billing._updateSiblingTotal()">')
      + '</div>'
      + '<div id="sibling-total-preview" style="background:#f0fdf4;border:1px solid #bbf7d0;border-radius:10px;padding:0.65rem;font-size:0.82rem;color:#166534;display:none"></div>'
      + '</div>'

      // ── SELF-STUDY (flat per-session, drop-ins only) fields ──
      + '<div id="inv-selfstudy-fields" style="display:none">'
      + '<p class="text-xs text-slate-500 mb-3">Flat rate per session for drop-in self-study. Package students are auto-billed for extra hours, so only pay-per-session drop-ins appear here.</p>'
      + (dropInStudents.length === 0
          ? '<div style="background:#fffbeb;border:1px solid #fef3c7;border-radius:10px;padding:0.85rem;font-size:0.82rem;color:#92400e">No drop-in students yet — tick <strong>"Pay-per-session drop-in"</strong> on a student\'s profile to bill them here.</div>'
          : '<div><label class="block text-sm font-medium text-slate-700 mb-1">Student</label>'
            + '<select name="ssStudentId" class="form-input">'
            + '<option value="">Select student...</option>'
            + dropInStudents.map(function(s) {
                return '<option value="' + s.id + '"' + (s.id === _studentFilter ? ' selected' : '') + '>' + App.Utils.esc(s.firstName + ' ' + s.lastName) + '</option>';
              }).join('')
            + '</select></div>'
            + '<div class="grid grid-cols-2 gap-4" style="margin-top:0.5rem">'
            + _field('Number of sessions', '<input name="ssVisits" type="number" min="1" step="1" class="form-input" value="1" oninput="App.Billing._updateSelfStudyAmount()">')
            + _field('Rate per session (RM)', '<input name="ssRate" type="number" min="0" step="0.01" class="form-input" value="' + SELF_STUDY_SESSION_RATE + '" oninput="App.Billing._updateSelfStudyAmount()">')
            + '</div>'
            + '<div id="selfstudy-amount-preview" style="background:#f0fdf4;border:1px solid #bbf7d0;border-radius:10px;padding:0.65rem;font-size:0.82rem;color:#166534;margin-top:0.5rem"></div>')
      + '</div>'

      // ── Shared: early bird + due date ──
      + '<div id="inv-early-bird-section" style="background:#fafaf8;border:1px solid #f0ede8;border-radius:10px;padding:0.85rem">'
      +   '<div style="display:flex;align-items:center;gap:0.6rem;margin-bottom:0.5rem">'
      +     '<input type="checkbox" id="early-bird-cb" onchange="App.Billing._toggleEarlyBird()" style="width:16px;height:16px;accent-color:var(--gold);cursor:pointer">'
      +     '<label for="early-bird-cb" style="font-size:0.83rem;font-weight:600;color:#374151;cursor:pointer">Early Bird Discount</label>'
      +   '</div>'
      +   '<div id="early-bird-fields" style="display:none;margin-top:0.5rem">'
      +     '<div class="grid grid-cols-2 gap-3">'
      +       _field('Discount %', '<input id="discount-pct" name="discountPct" type="number" min="0" max="100" step="1" class="form-input" value="10" oninput="App.Billing._updateNetAmount();App.Billing._updateGenPreview()">')
      +       _field('Pay by (cutoff)', '<input name="earlyBirdCutoff" type="date" class="form-input">')
      +     '</div>'
      +     '<div id="net-amount-preview" style="margin-top:0.5rem;font-size:0.82rem;color:#6b7280"></div>'
      +   '</div>'
      + '</div>'

      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Invoice Date', '<input name="invoiceDate" type="date" class="form-input" value="' + App.Utils.today() + '" required>')
      + _field('Due Date', '<input name="dueDate" type="date" class="form-input" value="' + defaultDue + '" required>')
      + '</div>'

      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" id="inv-submit-btn" style="padding:0.5rem 1.1rem;font-size:0.85rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Create Invoice</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );

    // Auto-enable early bird if within first 7 days
    (function() {
      if (new Date().getDate() <= 7) {
        var cb = document.getElementById('early-bird-cb');
        if (cb) { cb.checked = true; _toggleEarlyBird(); }
      }
    })();
    _updateGenPreview();

    document.getElementById('create-invoice-form').addEventListener('submit', function(e) {
      e.preventDefault();
      var mode = _currentInvMode || 'single';
      if (mode === 'monthly') { _doGenerateMonthly(new FormData(e.target)); return; }
      if (mode === 'sibling') { _doSiblingInvoice(new FormData(e.target)); return; }
      if (mode === 'selfstudy') { _doSelfStudyInvoice(new FormData(e.target)); return; }
      // Single invoice
      var fd = new FormData(e.target);
      var st = App.Store.get();
      if (!fd.get('studentId')) { App.Utils.showToast('Select a student', 'warning'); return; }
      if (!fd.get('description')) { App.Utils.showToast('Enter a description', 'warning'); return; }
      if (!fd.get('amount') || parseFloat(fd.get('amount')) <= 0) { App.Utils.showToast('Enter an amount', 'warning'); return; }
      var baseAmount = parseFloat(fd.get('amount')) || 0;
      var discountPct = document.getElementById('early-bird-cb') && document.getElementById('early-bird-cb').checked
        ? (parseFloat(fd.get('discountPct')) || 0) : 0;
      var finalAmount = parseFloat((baseAmount * (1 - discountPct / 100)).toFixed(2));
      var newInvoice = {
        studentId: fd.get('studentId'),
        description: fd.get('description') + (discountPct > 0 ? ' (' + discountPct + '% early bird)' : ''),
        type: fd.get('type'),
        amount: finalAmount,
        discountPct: discountPct || undefined,
        earlyBirdCutoff: fd.get('earlyBirdCutoff') || undefined,
        dueDate: fd.get('dueDate'),
        status: 'Unpaid',
        createdOn: fd.get('invoiceDate') || App.Utils.today(),
        paidOn: null
      };
      App.Utils.hideModal(true);
      App.Api.post('/api/invoices', newInvoice).then(function() {
        return App.Api.loadSnapshot();
      }).then(function() {
        App.Utils.showToast('Invoice created' + (discountPct > 0 ? ' with ' + discountPct + '% early bird discount' : ''), 'success');
        App.Router.refresh();
      }).catch(function() {
        // Error already toasted by App.Api wrapper.
      });
    });
  }

  var _currentInvMode = 'single';

  function _setInvMode(mode) {
    _currentInvMode = mode;
    var tabs = document.querySelectorAll('#inv-mode-tabs button');
    tabs.forEach(function(btn) {
      var m = btn.getAttribute('data-mode');
      btn.style.background = m === mode ? 'var(--gold)' : 'transparent';
      btn.style.color = m === mode ? '#0a0a0a' : '#64748b';
      btn.style.borderColor = m === mode ? 'var(--gold)' : '#e2e8f0';
    });
    var single    = document.getElementById('inv-single-fields');
    var monthly   = document.getElementById('inv-monthly-fields');
    var sibling   = document.getElementById('inv-sibling-fields');
    var selfstudy = document.getElementById('inv-selfstudy-fields');
    if (single)    single.style.display    = mode === 'single'    ? 'block' : 'none';
    if (monthly)   monthly.style.display   = mode === 'monthly'   ? 'block' : 'none';
    if (sibling)   sibling.style.display   = mode === 'sibling'   ? 'block' : 'none';
    if (selfstudy) selfstudy.style.display = mode === 'selfstudy' ? 'block' : 'none';

    // Early bird discount is irrelevant for flat-rate self-study.
    var earlyBird = document.getElementById('inv-early-bird-section');
    if (earlyBird) earlyBird.style.display = mode === 'selfstudy' ? 'none' : 'block';

    var btn = document.getElementById('inv-submit-btn');
    if (btn) {
      btn.textContent = mode === 'monthly' ? 'Generate Invoices' : mode === 'sibling' ? 'Create Sibling Invoice' : 'Create Invoice';
    }
    if (mode === 'monthly') _updateGenPreview();
    if (mode === 'selfstudy') _updateSelfStudyAmount();
  }

  function _toggleEarlyBird() {
    var cb = document.getElementById('early-bird-cb');
    var fields = document.getElementById('early-bird-fields');
    if (fields) fields.style.display = cb && cb.checked ? 'block' : 'none';
    _updateNetAmount();
  }

  function _updateNetAmount() {
    var base = parseFloat((document.getElementById('inv-base-amount') || {}).value) || 0;
    var cb = document.getElementById('early-bird-cb');
    var pctEl = document.getElementById('discount-pct');
    var preview = document.getElementById('net-amount-preview');
    if (!preview) return;
    if (cb && cb.checked && pctEl && base > 0) {
      var pct = parseFloat(pctEl.value) || 0;
      var net = (base * (1 - pct / 100)).toFixed(2);
      preview.textContent = 'Net amount: RM ' + net + ' (saving RM ' + (base - parseFloat(net)).toFixed(2) + ')';
    } else {
      preview.textContent = '';
    }
  }

  function _updateSelfStudyAmount() {
    var visits = parseInt((document.querySelector('#create-invoice-form [name="ssVisits"]') || {}).value) || 0;
    var rate = parseFloat((document.querySelector('#create-invoice-form [name="ssRate"]') || {}).value) || 0;
    var preview = document.getElementById('selfstudy-amount-preview');
    if (!preview) return;
    preview.innerHTML = '<strong>Total: RM ' + (visits * rate).toFixed(2) + '</strong> (' + visits + ' session' + (visits !== 1 ? 's' : '') + ' × RM ' + rate.toFixed(2) + ')';
  }

  function _doSelfStudyInvoice(fd) {
    var studentId = fd.get('ssStudentId');
    var visits = parseInt(fd.get('ssVisits')) || 0;
    var rate = parseFloat(fd.get('ssRate')) || 0;
    if (!studentId) { App.Utils.showToast('Select a student', 'warning'); return; }
    if (visits < 1) { App.Utils.showToast('Enter number of sessions', 'warning'); return; }
    if (rate <= 0) { App.Utils.showToast('Enter a rate per session', 'warning'); return; }
    var newInvoice = {
      studentId: studentId,
      description: 'Self-study — ' + visits + ' session' + (visits !== 1 ? 's' : '') + ' @ RM' + rate,
      type: 'Self-study',
      amount: parseFloat((visits * rate).toFixed(2)),
      dueDate: fd.get('dueDate'),
      status: 'Unpaid',
      createdOn: fd.get('invoiceDate') || App.Utils.today(),
      paidOn: null
    };
    App.Utils.hideModal(true);
    App.Api.post('/api/invoices', newInvoice).then(function() {
      return App.Api.loadSnapshot();
    }).then(function() {
      App.Utils.showToast('Self-study invoice created', 'success');
      App.Router.refresh();
    }).catch(function() {
      // Error already toasted by App.Api wrapper.
    });
  }

  // _generateMonthlyModal merged into _createModal (Monthly Batch tab)

  function _updateGenPreview() {
    var monthInput = document.querySelector('#create-invoice-form [name="month"]');
    var month = monthInput ? monthInput.value : '';
    _previewMonthly(month);
  }

  function _previewMonthly(month) {
    var preview = document.getElementById('gen-preview');
    if (!preview || !month) return;
    var state = App.Store.get();
    var parts = month.split('-');
    var yr = parseInt(parts[0]), mo = parseInt(parts[1]);
    var monthLabel = new Date(yr, mo - 1, 1).toLocaleDateString('en-MY', { month:'long', year:'numeric' });
    var activeStudents = state.students.filter(function(s) { return s.status === 'Active' || s.status === 'New'; });
    var alreadyHas = {};
    state.invoices.forEach(function(i) {
      if (i.type === 'Monthly' && i.createdOn && i.createdOn.startsWith(month)) alreadyHas[i.studentId] = true;
    });
    var toCreate = activeStudents.filter(function(s) { return !alreadyHas[s.id]; });
    var skipped  = activeStudents.filter(function(s) { return  alreadyHas[s.id]; });

    var baseAmount = parseFloat((document.getElementById('gen-amount') || {}).value) || 0;
    var ebCb = document.getElementById('early-bird-cb');
    var pctEl = document.getElementById('discount-pct');
    var discountPct = (ebCb && ebCb.checked && pctEl) ? (parseFloat(pctEl.value) || 0) : 0;
    var net = parseFloat((baseAmount * (1 - discountPct / 100)).toFixed(2));

    preview.innerHTML = '<strong>' + toCreate.length + ' invoice' + (toCreate.length !== 1 ? 's' : '') + ' will be created</strong> for ' + monthLabel
      + ' · RM ' + net.toFixed(2) + ' each'
      + (discountPct > 0 ? ' <span style="color:#92400e">(' + discountPct + '% early bird)</span>' : '')
      + (skipped.length > 0 ? '<br><span style="color:#94a3b8">' + skipped.length + ' student' + (skipped.length !== 1 ? 's' : '') + ' skipped (already invoiced)</span>' : '');
  }

  function _doGenerateMonthly(fd) {
    var month = fd.get('month');
    var baseAmount = parseFloat(fd.get('genAmount')) || 0;
    var dueDate = fd.get('dueDate');
    var ebCb = document.getElementById('early-bird-cb');
    var discountPct = (ebCb && ebCb.checked) ? (parseFloat(fd.get('discountPct')) || 0) : 0;
    var earlyBirdCutoff = (ebCb && ebCb.checked) ? (fd.get('earlyBirdCutoff') || undefined) : undefined;
    var finalAmount = parseFloat((baseAmount * (1 - discountPct / 100)).toFixed(2));

    var state = App.Store.get();
    var [yr, mo] = month.split('-');
    var monthLabel = new Date(parseInt(yr), parseInt(mo) - 1, 1).toLocaleDateString('en-MY', { month:'long', year:'numeric' });

    var activeStudents = state.students.filter(function(s) { return s.status === 'Active' || s.status === 'New'; });
    var alreadyHas = {};
    state.invoices.forEach(function(i) {
      if (i.type === 'Monthly' && i.createdOn && i.createdOn.startsWith(month)) alreadyHas[i.studentId] = true;
    });
    var toCreate = activeStudents.filter(function(s) { return !alreadyHas[s.id]; });

    if (toCreate.length === 0) {
      App.Utils.showToast('All active students already have invoices for ' + monthLabel, 'info');
      App.Utils.hideModal(true);
      return;
    }

    var existing = state.invoices;

    // ── Referral credit auto-apply ────────────────────────────────────────
    // For each family with an 'earned' referral_reward that still has credits
    // remaining, apply -RM10 to ONE invoice per cycle (oldest reward first)
    // and remember which reward ID to consume server-side after creation.
    var rewards = (state.referralRewards || []).slice()
      .filter(function(r) { return r.status === 'earned' && r.creditsRemaining > 0; })
      .sort(function(a, b) { return (a.milestoneMetOn || '').localeCompare(b.milestoneMetOn || ''); });
    var creditByFamily = {}; // familyId -> rewardId (one credit per family per cycle)
    rewards.forEach(function(r) {
      if (!creditByFamily[r.referrerFamilyId]) creditByFamily[r.referrerFamilyId] = r.id;
    });
    var rewardConsumeQueue = []; // rewardIds to POST consume on after generation

    var newInvoices = toCreate.map(function(s, idx) {
      var fam = (state.families || []).find(function(f) { return f.id === s.familyId; });
      var rewardId = fam ? creditByFamily[fam.id] : null;
      var referralCredit = 0;
      if (rewardId) {
        referralCredit = 10;
        rewardConsumeQueue.push(rewardId);
        delete creditByFamily[fam.id]; // only one invoice per family per cycle
      }
      return {
        id: App.Utils.generateId('INV'),
        studentId: s.id,
        description: monthLabel + ' Tuition' + (discountPct > 0 ? ' (' + discountPct + '% early bird)' : '') + (referralCredit > 0 ? ' (RM10 referral credit)' : ''),
        type: 'Monthly',
        amount: parseFloat((finalAmount - referralCredit).toFixed(2)),
        discountPct: discountPct || undefined,
        referralCredit: referralCredit || undefined,
        earlyBirdCutoff: earlyBirdCutoff,
        dueDate: dueDate,
        status: 'Unpaid',
        createdOn: month + '-01',
        paidOn: null
      };
    });

    // Persist each new invoice to the backend so the DB is the source of
    // truth, not localStorage. This is the fix for the long-standing
    // localStorage-only inconsistency. We POST sequentially (low volume,
    // and serial errors are easier to surface) and update the local store
    // with the server-returned rows so IDs match.
    App.Utils.hideModal(true);
    _persistGeneratedInvoices(newInvoices, rewardConsumeQueue, monthLabel);
  }

  // _persistGeneratedInvoices is the async tail of _doGenerateMonthly. It
  // POSTs every freshly-built invoice to /api/invoices, fires referral
  // consume calls in parallel, then reloads the snapshot so all modules
  // pick up the persisted rows.
  async function _persistGeneratedInvoices(newInvoices, rewardConsumeQueue, monthLabel) {
    var savedCount = 0;
    var failed = [];
    for (var i = 0; i < newInvoices.length; i++) {
      var inv = newInvoices[i];
      try {
        // Backend assigns its own ID, so we drop our local one to avoid
        // collisions. Server returns the persisted row.
        var payload = Object.assign({}, inv);
        delete payload.id;
        await App.Api.post('/api/invoices', payload, { silent: true });
        savedCount++;
      } catch(err) {
        console.error('invoice persist failed', inv.studentId, err);
        failed.push(inv.studentId);
      }
    }

    // Referral credit consume calls — fire-and-forget. The backend tolerates
    // races and the next snapshot reload will reflect any drift.
    rewardConsumeQueue.forEach(function(rid) {
      App.Api.post('/api/referrals/' + rid + '/consume', {}, { silent: true }).catch(function() {});
    });

    // Reload the snapshot so the local store reflects the persisted truth.
    // Without this we'd be staring at our locally-generated rows that have
    // no DB representation — exactly the bug we're fixing.
    try {
      await App.Api.loadSnapshot();
    } catch(e) {
      console.error('snapshot reload after generate failed', e);
    }

    var creditMsg = rewardConsumeQueue.length > 0
      ? ' (' + rewardConsumeQueue.length + ' referral credit' + (rewardConsumeQueue.length !== 1 ? 's' : '') + ' applied)'
      : '';
    if (failed.length > 0) {
      App.Utils.showToast('Saved ' + savedCount + '/' + newInvoices.length + ' invoices for ' + monthLabel + ' — ' + failed.length + ' failed (see console)', 'warning', 8000);
    } else {
      App.Utils.showToast('Generated ' + savedCount + ' invoices for ' + monthLabel + creditMsg, 'success');
    }
    App.Router.refresh();
  }

  // _siblingInvoiceModal merged into _createModal (Sibling tab)

  function _updateSiblingChildren(email) {
    const { students } = App.Store.get();
    const children = students.filter(function(s) { return s.contact === email; });
    const list = document.getElementById('sibling-children-list');
    const checksDiv = document.getElementById('sibling-children-checks');
    if (!list || !checksDiv) return;
    if (children.length === 0) { list.style.display = 'none'; return; }
    checksDiv.innerHTML = children.map(function(s) {
      return '<label style="display:flex;align-items:center;gap:0.5rem;padding:0.3rem 0;font-size:0.84rem;cursor:pointer">'
        + '<input type="checkbox" name="childIds" value="' + s.id + '" checked style="width:15px;height:15px;accent-color:var(--gold);cursor:pointer" onchange="App.Billing._updateSiblingTotal()">'
        + '<span class="font-medium">' + App.Utils.esc(s.firstName + ' ' + s.lastName) + '</span>'
        + '<span style="color:#94a3b8;font-size:0.75rem">(' + App.Utils.esc(s.status) + ')</span>'
        + '</label>';
    }).join('');
    list.style.display = 'block';
    _updateSiblingTotal();
  }

  function _updateSiblingTotal() {
    const preview = document.getElementById('sibling-total-preview');
    if (!preview) return;
    const perChild = parseFloat((document.getElementById('sibling-per-child') || {}).value) || 0;
    const discount = parseFloat((document.getElementById('sibling-discount') || {}).value) || 0;
    const checked  = document.querySelectorAll('#sibling-children-checks input[type="checkbox"]:checked');
    const count    = checked.length;
    if (count === 0) { preview.style.display = 'none'; return; }
    const discounted = parseFloat((perChild * (1 - discount / 100)).toFixed(2));
    const total = parseFloat((discounted * count).toFixed(2));
    preview.style.display = 'block';
    preview.innerHTML = count + ' child' + (count !== 1 ? 'ren' : '') + ' × RM ' + discounted.toFixed(2)
      + (discount > 0 ? ' (' + discount + '% sibling discount applied)' : '')
      + ' = <strong>RM ' + total.toFixed(2) + ' total</strong>';
  }

  function _doSiblingInvoice(fd) {
    const state = App.Store.get();
    const email = fd.get('parentEmail');
    if (!email) { App.Utils.showToast('Select a family', 'warning'); return; }
    const description = fd.get('sibDescription') || fd.get('description');
    const perChild = parseFloat(fd.get('amountPerChild')) || 0;
    const discount = parseFloat(fd.get('siblingDiscount')) || 0;
    const dueDate  = fd.get('dueDate');
    const discounted = parseFloat((perChild * (1 - discount / 100)).toFixed(2));

    // Get checked child IDs from the form
    const childIds = Array.from(document.querySelectorAll('#sibling-children-checks input[type="checkbox"]:checked')).map(function(cb) { return cb.value; });

    if (childIds.length < 1) {
      App.Utils.showToast('Select at least one child.', 'warning');
      return;
    }

    const children = state.students.filter(function(s) { return childIds.indexOf(s.id) > -1; });
    const totalAmount = parseFloat((discounted * children.length).toFixed(2));
    const childNames = children.map(function(c) { return c.firstName; }).join(' + ');
    const desc = description + ' — ' + childNames + (discount > 0 ? ' (' + discount + '% sibling discount)' : '');

    // Create a single combined invoice linked to the first child, with
    // siblings listed in the description. Persisted to backend so it shows
    // up consistently for both the parent and admin views.
    const newInvoice = {
      studentId: children[0].id,  // primary child
      siblingIds: JSON.stringify(children.slice(1).map(function(c) { return c.id; })),
      description: desc,
      type: 'Monthly',
      amount: totalAmount,
      siblingDiscount: discount || undefined,
      dueDate: dueDate,
      status: 'Unpaid',
      createdOn: App.Utils.today(),
      paidOn: null
    };

    App.Utils.hideModal(true);
    App.Api.post('/api/invoices', newInvoice).then(function() {
      return App.Api.loadSnapshot();
    }).then(function() {
      App.Utils.showToast('Sibling invoice created — RM ' + totalAmount.toFixed(2) + ' for ' + childNames, 'success');
      App.Router.refresh();
    }).catch(function() {
      // Error already toasted by App.Api wrapper.
    });
  }

  function _field(label, inputHtml) {
    return '<div><label class="block text-sm font-medium text-slate-700 mb-1">' + label + '</label>' + inputHtml + '</div>';
  }

  // Called from main.js on login to notify parents of overdue invoices
  function checkLoginNotifications() {
    const { invoices, students } = App.Store.get();
    if (App.currentRole !== 'client' || !App.clientParent) return;
    const myIds = students.filter(function(s) { return s.contact === App.clientParent; }).map(function(s) { return s.id; });
    const myOverdue = invoices.filter(function(i) { return i.status === 'Overdue' && myIds.indexOf(i.studentId) > -1; });
    const today = new Date();
    const in7 = new Date(today); in7.setDate(today.getDate() + 7);
    const myDueSoon = invoices.filter(function(i) {
      if (i.status !== 'Unpaid' || myIds.indexOf(i.studentId) === -1) return false;
      const d = new Date(i.dueDate);
      return d >= today && d <= in7;
    });
    if (myOverdue.length > 0) {
      App.Utils.showToast('You have ' + myOverdue.length + ' overdue invoice' + (myOverdue.length > 1 ? 's' : '') + '. Please check Billing.', 'error');
    } else if (myDueSoon.length > 0) {
      App.Utils.showToast(myDueSoon.length + ' payment' + (myDueSoon.length > 1 ? 's are' : ' is') + ' due within 7 days.', 'info');
    }
  }

  function _downloadCSV(csv, filename) {
    var blob = new Blob([csv], { type: 'text/csv' });
    var url = URL.createObjectURL(blob);
    var a = document.createElement('a');
    a.href = url; a.download = filename; a.click();
    URL.revokeObjectURL(url);
  }

  function _exportCSV() {
    const { invoices, students } = App.Store.get();
    const headers = ['Invoice ID','Student','Description','Type','Amount','Due Date','Status','Created On','Paid On'];
    const rows = invoices.map(function(inv) {
      const stu = students.find(function(s) { return s.id === inv.studentId; });
      const stuName = stu ? stu.firstName + ' ' + stu.lastName : inv.studentId;
      return [inv.id, stuName, inv.description, inv.type, inv.amount, inv.dueDate, inv.status, inv.createdOn || '', inv.paidOn || '']
        .map(function(v) { return '"' + String(v||'').replace(/"/g,'""') + '"'; }).join(',');
    });
    _downloadCSV([headers.join(',')].concat(rows).join('\n'), 'invoices.csv');
    App.Utils.showToast('Exported ' + invoices.length + ' invoices', 'success');
  }

  App.Billing = {
    render: render,
    _setFilter: _setFilter,
    _setStudentFilter: _setStudentFilter,
    _toggleMenu: _toggleMenu,
    _markPaid: _markPaid,
    _markPaidModal: _markPaidModal,
    _confirmPaid: _confirmPaid,
    _showAdminProofUpload: _showAdminProofUpload,
    _showAdminPaymentMethods: _showAdminPaymentMethods,
    _previewAdminProof: _previewAdminProof,
    _adminSubmitWithProof: _adminSubmitWithProof,
    _markUnpaid: _markUnpaid,
    _parentSubmitPaid: _parentSubmitPaid,
    _parentConfirmSubmit: _parentConfirmSubmit,
    _showProofUpload: _showProofUpload,
    _showPaymentMethods: _showPaymentMethods,
    _previewProof: _previewProof,
    _parentSubmitWithProof: _parentSubmitWithProof,
    _verifyPaid: _verifyPaid,
    _confirmVerify: _confirmVerify,
    _deleteInvoice: _deleteInvoice,
    _generateMonthly: _generateMonthly,
    _payOnline: _payOnline,
    _editModal: _editModal,
    _createModal: _createModal,
    checkLoginNotifications: checkLoginNotifications,
    _toggleSelectAllInv: _toggleSelectAllInv,
    _toggleSelectInv: _toggleSelectInv,
    _bulkDeselectInv: _bulkDeselectInv,
    _bulkMarkPaid: _bulkMarkPaid,
    _bulkConfirmPaid: _bulkConfirmPaid,
    _setInvMode: _setInvMode,
    _previewMonthly: _previewMonthly,
    _updateGenPreview: _updateGenPreview,
    _toggleEarlyBird: _toggleEarlyBird,
    _updateNetAmount: _updateNetAmount,
    _updateSelfStudyAmount: _updateSelfStudyAmount,
    _updateSiblingChildren: _updateSiblingChildren,
    _updateSiblingTotal: _updateSiblingTotal,
    _exportCSV: _exportCSV,
    _setPage: _setBillingPage
  };
})();
