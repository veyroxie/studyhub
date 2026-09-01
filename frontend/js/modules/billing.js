(function() {
  window.App = window.App || {};

  let _filter = 'Unpaid';
  let _studentFilter = ''; // empty = all students
  let _menuListenerAdded = false;
  let _selectedInv = {};
  let _billingPage = 0;
  var _PAGE_SIZE = 15;
  var REGISTRATION_FEE = 250;       // RM, matches the seeded Registration product
  var SELF_STUDY_SESSION_RATE = 10; // RM per self-study session (manual drop-in billing)
  var SELF_STUDY_HOUR_RATE = 10;    // RM per self-study hour (member included value / add-on)
  // Keep in sync with maxProofBytes in backend handlers_uploads.go. Oversized
  // uploads trip the server's MaxBytesReader mid-stream, which the browser
  // reports as a network error rather than a clean rejection — so we reject
  // them here first with a readable message.
  var MAX_PROOF_BYTES = 15 * 1024 * 1024;

  // ── Package line-item builder (Create Invoice → Single tab) ──────────────────
  // Line items selected for the invoice under construction. The server derives
  // the invoice total from these, so the client only tracks display state.
  var _lineItems = [];
  var _lineSeq = 0;

  // _packageCatalog builds the selectable packages: Group/Private by level
  // (priced from the pricing matrix) plus self-study packages and the hourly
  // add-on. `foc` marks a member package whose included hours are waived.
  function _packageCatalog() {
    var tiers = App.Store.get().pricingTiers || [];
    var feeFor = function(type, band) {
      var t = tiers.find(function(x) { return x.classType === type && x.levelBand === band; });
      return t ? (t.monthlyFee || 0) : 0;
    };
    var bandOf = function(level) { return level <= 3 ? '1-3' : '4-6'; };
    var cat = [];
    ['Group', 'Private'].forEach(function(type) {
      for (var lvl = 1; lvl <= 6; lvl++) {
        cat.push({ key: type + '-' + lvl, group: type, label: type + ' — Level ' + lvl,
          name: type + ' Class — Level ' + lvl, descriptor: type + ' tuition, Level ' + lvl,
          qty: 1, unitPrice: feeFor(type, bandOf(lvl)), kind: 'item', editableQty: false });
      }
    });
    [4, 8].forEach(function(hrs) {
      cat.push({ key: 'ss-' + hrs, group: 'Self-study', label: 'Self-study — ' + hrs + ' hours (member)',
        name: 'TSH Membership', descriptor: hrs + ' self-study hours included',
        qty: 1, unitPrice: hrs * SELF_STUDY_HOUR_RATE, kind: 'item', editableQty: false, foc: true });
    });
    cat.push({ key: 'ss-addon', group: 'Self-study', label: 'Self-study add-on (extra hours)',
      name: 'Self-study add-on', descriptor: 'Extra self-study hours',
      qty: 1, unitPrice: SELF_STUDY_HOUR_RATE, kind: 'item', editableQty: true });
    // New-student lines. Registration matches the seeded product (RM250).
    // The deposit is one month's fee, held and applied to the student's last
    // month (Nadine, 27/08), so it mirrors the tuition price for the band.
    cat.push({ key: 'reg-fee', group: 'New student', label: 'Registration fee',
      name: 'Registration Fee', descriptor: 'One-time registration',
      qty: 1, unitPrice: REGISTRATION_FEE, kind: 'item', editableQty: false });
    ['Group', 'Private'].forEach(function(type) {
      ['1-3', '4-6'].forEach(function(band) {
        cat.push({ key: 'dep-' + type + '-' + band, group: 'New student',
          label: 'Deposit (1 month) — ' + type + ' Level ' + band,
          name: 'Deposit (1 month) — ' + type + ' Level ' + band,
          descriptor: 'Refunded against the final month',
          qty: 1, unitPrice: feeFor(type, band), kind: 'item', editableQty: false });
      });
    });
    return cat;
  }

  function _packageCatalogOptions() {
    var cat = _packageCatalog();
    var groups = ['Group', 'Private', 'Self-study', 'New student'];
    var html = '<option value="">Select a package…</option>';
    groups.forEach(function(g) {
      html += '<optgroup label="' + g + '">';
      cat.filter(function(c) { return c.group === g; }).forEach(function(c) {
        html += '<option value="' + c.key + '">' + App.Utils.esc(c.label) + ' — RM ' + (c.unitPrice || 0) + '</option>';
      });
      html += '</optgroup>';
    });
    return html;
  }

  // _lineItemAmount returns the signed amount: positive for items, negative for
  // discount lines (matching how the backend stores them).
  function _lineItemAmount(li) {
    var raw = (parseFloat(li.qty) || 0) * (parseFloat(li.unitPrice) || 0);
    return li.kind === 'discount' ? -Math.abs(raw) : raw;
  }

  function _addLineItem(key) {
    if (!key) return;
    var c = _packageCatalog().find(function(x) { return x.key === key; });
    if (!c) return;
    _lineSeq++;
    _lineItems.push({ id: _lineSeq, kind: c.kind, name: c.name, descriptor: c.descriptor,
      qty: c.qty, unitPrice: c.unitPrice, editableQty: !!c.editableQty });
    if (c.foc) {
      _lineSeq++;
      _lineItems.push({ id: _lineSeq, kind: 'discount', name: 'Special pass FOC (self-study included)',
        descriptor: '', qty: 1, unitPrice: c.unitPrice, editableQty: false });
    }
    _renderLineItems();
  }

  function _removeLineItem(id) {
    _lineItems = _lineItems.filter(function(li) { return li.id !== id; });
    _renderLineItems();
  }

  function _editLineItem(id, field, value) {
    var li = _lineItems.find(function(x) { return x.id === id; });
    if (!li) return;
    li[field] = value;
    _updateLineItemsTotal();
  }

  function _renderLineItems() {
    var list = document.getElementById('line-items-list');
    if (!list) return;
    if (_lineItems.length === 0) {
      list.innerHTML = '<div style="background:#fafaf8;border:1px dashed #e2ded7;border-radius:10px;padding:0.75rem;font-size:0.82rem;color:#94a3b8;text-align:center">No packages added yet.</div>';
      _updateLineItemsTotal();
      return;
    }
    list.innerHTML = _lineItems.map(_lineItemRow).join('');
    _updateLineItemsTotal();
  }

  function _lineItemRow(li) {
    var isDiscount = li.kind === 'discount';
    var priceInput = isDiscount
      ? ''
      : '<input type="number" min="0" step="0.01" value="' + (li.unitPrice || 0)
        + '" oninput="App.Billing._editLineItem(' + li.id + ',\'unitPrice\',this.value)" style="width:74px;padding:0.25rem 0.4rem;border:1px solid #e2e8f0;border-radius:6px;font-size:0.78rem" title="Unit price (RM)">';
    var qtyInput = (!isDiscount && li.editableQty)
      ? '<input type="number" min="1" step="1" value="' + (li.qty || 1)
        + '" oninput="App.Billing._editLineItem(' + li.id + ',\'qty\',this.value)" style="width:48px;padding:0.25rem 0.4rem;border:1px solid #e2e8f0;border-radius:6px;font-size:0.78rem" title="Quantity"> ×'
      : '';
    // The name is editable so one-off lines (Phonics, the 30-minute Math
    // class) can be labelled properly until session billing carries real
    // class names. esc() is attribute-safe: it escapes quotes.
    var nameCell = isDiscount
      ? '<div style="font-size:0.84rem;font-weight:600;color:#166534">' + App.Utils.esc(li.name) + '</div>'
      : '<input type="text" value="' + App.Utils.esc(li.name)
        + '" oninput="App.Billing._editLineItem(' + li.id + ',\'name\',this.value)" '
        + 'style="width:100%;padding:0.2rem 0.3rem;border:1px solid transparent;border-radius:6px;font-size:0.84rem;font-weight:600;color:#111;background:transparent" '
        + 'onfocus="this.style.borderColor=\'#e2e8f0\';this.style.background=\'#fff\'" onblur="this.style.borderColor=\'transparent\';this.style.background=\'transparent\'" '
        + 'title="Line name, click to edit">';
    return '<div style="display:flex;align-items:center;gap:0.5rem;padding:0.45rem 0;border-bottom:1px solid #f2efea">'
      + '<div style="flex:1;min-width:0">'
      +   nameCell
      +   (li.descriptor ? '<div style="font-size:0.72rem;color:#94a3b8">' + App.Utils.esc(li.descriptor) + '</div>' : '')
      + '</div>'
      + qtyInput
      + priceInput
      + '<div id="li-amt-' + li.id + '" style="width:88px;text-align:right;font-size:0.82rem;font-weight:600;color:' + (isDiscount ? '#166534' : '#111') + '"></div>'
      + '<button type="button" onclick="App.Billing._removeLineItem(' + li.id + ')" title="Remove" style="background:none;border:none;color:#cbd5e1;cursor:pointer;font-size:1rem;line-height:1">&#10005;</button>'
      + '</div>';
  }

  function _updateLineItemsTotal() {
    var total = 0;
    _lineItems.forEach(function(li) {
      var amt = _lineItemAmount(li);
      total += amt;
      var el = document.getElementById('li-amt-' + li.id);
      if (el) el.textContent = (amt < 0 ? '- RM ' : 'RM ') + Math.abs(amt).toFixed(2);
    });
    var t = document.getElementById('line-items-total');
    if (t) t.innerHTML = '<strong>Total: RM ' + total.toFixed(2) + '</strong>';
    _updateNetAmount();
  }

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
          + '<div class="text-sm" style="color:#92400e"><span class="font-semibold">Early bird discount active</span> — pay by the <strong>7th</strong> to keep your <strong>RM10 off</strong>; unpaid invoices return to full price after.</div>'
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
          + '<button onclick="App.Billing._generateMonthly()" class="px-4 py-2 text-sm text-white rounded-lg" style="background:#4f46e5" title="Run the monthly invoice + payroll job for this month">Generate Monthly</button>'
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
                + '<td class="td text-sm text-slate-600"><button type="button" onclick="App.Billing._viewInvoiceModal(\'' + inv.id + '\')" title="View breakdown" style="text-align:left;background:none;border:none;padding:0;color:inherit;cursor:pointer;font:inherit;text-decoration:underline;text-decoration-color:#e2e8f0;text-underline-offset:2px">' + App.Utils.esc(inv.description) + '</button></td>'
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
      + '<button onclick="App.Billing._bulkDeleteInv()" style="padding:0.35rem 0.85rem;font-size:0.75rem;font-weight:600;background:#dc2626;color:#fff;border:none;border-radius:7px;cursor:pointer">Delete Selected</button>'
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
      + '<p class="text-sm text-slate-500 mb-4">Bulk mark-paid is for cash received. Bank Transfer / QR Pay need a receipt and reference per invoice, so confirm those individually.</p>'
      + '<button onclick="App.Billing._bulkConfirmPaid(\'Cash\')" class="w-full p-3 mb-3 border-2 border-slate-200 rounded-xl text-sm font-semibold text-slate-700 hover:border-yellow-400 hover:bg-yellow-50 transition-all text-center">Mark ' + ids.length + ' as paid (Cash)</button>'
      + '<button onclick="App.Utils.hideModal()" class="w-full py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '</div>';
    App.Utils.showModal(html);
  }

  function _bulkConfirmPaid(method) {
    var ids = Object.keys(_selectedInv);
    if (ids.length === 0) return;
    App.Utils.hideModal(true);
    // Persist each invoice to the server. The old version only mutated the
    // local store, so every "paid" reverted on the next snapshot reload.
    // Isolate per-invoice failures (silent so we own the messaging) so a
    // transient error on one invoice doesn't abort the batch or falsely
    // report the rest as paid.
    Promise.all(ids.map(function(id) {
      return App.Api.put('/api/invoices/' + id + '/pay', { status: 'Paid', paymentMethod: method }, { silent: true })
        .then(function() { return true; })
        .catch(function() { return false; });
    })).then(function(results) {
      var ok = results.filter(Boolean).length;
      var failed = results.length - ok;
      _selectedInv = {};
      return App.Api.loadSnapshot().then(function() {
        if (failed === 0) {
          App.Utils.showToast('Marked ' + ok + ' invoice' + (ok !== 1 ? 's' : '') + ' as paid · ' + method, 'success');
        } else {
          App.Utils.showToast('Marked ' + ok + ' paid · ' + failed + ' failed — please retry', ok === 0 ? 'error' : 'warning');
        }
        App.Notifs.refresh();
        App.Router.refresh();
      });
    });
  }

  function _bulkDeleteInv() {
    var ids = Object.keys(_selectedInv);
    if (ids.length === 0) return;
    var noun = 'invoice' + (ids.length !== 1 ? 's' : '');
    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-lg font-bold mb-1">Delete ' + ids.length + ' ' + noun + '?</h2>'
      + '<p class="text-sm text-slate-500 mb-4">Only unpaid invoices are removed — paid ones are kept as records. Deletes are recoverable by an admin if needed.</p>'
      + '<button onclick="App.Billing._bulkDeleteInvConfirm()" class="w-full p-3 mb-3 rounded-xl text-sm font-bold text-white bg-red-600 hover:bg-red-700 transition-all">Delete ' + ids.length + ' ' + noun + '</button>'
      + '<button onclick="App.Utils.hideModal()" class="w-full py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '</div>'
    );
  }

  function _bulkDeleteInvConfirm() {
    var ids = Object.keys(_selectedInv);
    if (ids.length === 0) return;
    App.Utils.hideModal(true);
    App.Api.post('/api/invoices/bulk-delete', { ids: ids }).then(function(res) {
      _selectedInv = {};
      return App.Api.loadSnapshot().then(function() {
        var msg = 'Deleted ' + res.deleted + ' invoice' + (res.deleted !== 1 ? 's' : '');
        if (res.skipped > 0) msg += ' · ' + res.skipped + ' kept (paid or already removed)';
        App.Utils.showToast(msg, res.deleted === 0 ? 'warning' : 'success');
        App.Notifs.refresh();
        App.Router.refresh();
      });
    });
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
    const menu = document.getElementById('inv-menu-' + id);
    const wasOpen = menu && !menu.classList.contains('hidden');
    document.querySelectorAll('.inv-menu').forEach(function(m) { m.classList.add('hidden'); });
    if (!menu || wasOpen) return; // second click on the same button closes it
    menu.classList.remove('hidden');
    // The row's ⋮ menu is absolutely positioned inside a `.overflow-x-auto`
    // table wrapper (clips it vertically) AND the theme-B bottom dock is a
    // fixed bar with z-index:50. So for bottom rows the menu was both clipped
    // and rendered *under* the dock — the "actions get blocked" bug. Re-anchor
    // it as `fixed` at the button (escapes the clip), lift it above the dock
    // (z-index), and flip it upward when it would collide with the dock zone.
    const DOCK_RESERVE = 96; // ~5.5rem dock area at the viewport bottom
    const r = event.currentTarget.getBoundingClientRect();
    menu.style.position = 'fixed';
    menu.style.right = 'auto';
    menu.style.zIndex = '60'; // above #bottom-dock (z-index:50)
    const mw = menu.offsetWidth || 160;
    const mh = menu.offsetHeight || 0;
    let left = r.right - mw;
    if (left < 8) left = 8;
    let top = r.bottom + 4;
    // Flip above the button if opening downward would reach the dock zone.
    if (top + mh > window.innerHeight - DOCK_RESERVE) top = r.top - mh - 4;
    if (top < 8) top = 8;
    menu.style.left = left + 'px';
    menu.style.top = top + 'px';
  }

  function _markPaidModal(invId) {
    var html = '<div class="p-6">'
      + '<h2 class="text-lg font-bold mb-1">Confirm Payment</h2>'
      + '<p class="text-sm text-slate-500 mb-4">Select payment method received</p>'
      + '<div id="admin-payment-methods-grid" class="grid grid-cols-3 gap-3 mb-5">'
      // Cash — show a confirm screen (no receipt to attach, so make it deliberate)
      + '<button onclick="App.Billing._confirmCash(\'' + invId + '\')" '
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
      +   '<p style="font-size:0.82rem;font-weight:600;color:#374151;margin:0 0 0.5rem">Upload payment receipt <span style="color:#64748b;font-weight:500">(optional)</span></p>'
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

    // The receipt is optional: many parents never send one, and blocking on it
    // pushed real transfers to be logged as cash just to close the invoice —
    // losing both the receipt AND the true payment method. The reference number
    // stays required and is the durable evidence, since it is traceable in the
    // centre's own bank statement without the parent doing anything.
    var fileInput = document.getElementById('admin-proof-file');
    var hasFile = fileInput && fileInput.files && fileInput.files[0];
    if (hasFile && fileInput.files[0].size > MAX_PROOF_BYTES) {
      App.Utils.showToast('Receipt must be under ' + (MAX_PROOF_BYTES / 1024 / 1024) + 'MB — try a photo, not a scan', 'error');
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
        // A receipt is optional, but a receipt the admin CHOSE to attach and
        // that failed to upload must not be silently dropped — recording the
        // payment now would lose the evidence they meant to keep. Retry.
        if (submitBtn) { submitBtn.disabled = false; submitBtn.textContent = 'Confirm Payment'; }
        App.Utils.showToast('Receipt upload failed — payment not recorded, please retry', 'error');
      });
    } else {
      _confirmPaid(invId, method, refNo);
    }
  }

  function _confirmCash(invId) {
    var inv = App.Store.get().invoices.find(function(i) { return i.id === invId; });
    if (!inv) return;
    var html = '<div class="p-6">'
      + '<h2 class="text-lg font-bold mb-1">Confirm cash payment</h2>'
      + '<p class="text-sm text-slate-500 mb-4">Type the exact amount received to mark "' + App.Utils.esc(inv.description) + '" as paid.</p>'
      + '<label class="block text-xs font-semibold text-slate-500 mb-1">Amount received (invoice is ' + App.Utils.formatCurrency(inv.amount) + ')</label>'
      + '<input id="cash-confirm-amount" type="number" step="0.01" min="0" inputmode="decimal" placeholder="0.00" autofocus onkeydown="if(event.key===\'Enter\'){event.preventDefault();App.Billing._confirmCashSubmit(\'' + invId + '\')}" class="w-full px-3 py-2 border border-slate-200 rounded-lg text-sm mb-4 focus:border-blue-500 focus:outline-none">'
      + '<div class="flex gap-2">'
      + '<button onclick="App.Billing._confirmCashSubmit(\'' + invId + '\')" class="flex-1 py-2 text-sm font-bold text-white rounded-lg bg-blue-600 hover:bg-blue-700">Confirm payment</button>'
      + '<button onclick="App.Billing._markPaidModal(\'' + invId + '\')" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Back</button>'
      + '</div>'
      + '</div>';
    App.Utils.showModal(html);
    var el = document.getElementById('cash-confirm-amount');
    if (el) el.focus();
  }

  function _confirmCashSubmit(invId) {
    var inv = App.Store.get().invoices.find(function(i) { return i.id === invId; });
    if (!inv) return;
    var el = document.getElementById('cash-confirm-amount');
    var typed = el ? parseFloat(el.value) : NaN;
    // Require an exact match (to the cent) so marking paid is deliberate and
    // the recorded amount is confirmed to equal what was received.
    if (isNaN(typed) || Math.abs(typed - inv.amount) > 0.005) {
      App.Utils.showToast('Amount must match the invoice exactly (' + App.Utils.formatCurrency(inv.amount) + ')', 'error');
      if (el) el.focus();
      return;
    }
    _confirmPaid(invId, 'Cash');
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
      }).catch(function() {
        // If this came from the proof-upload flow, its submit button was
        // disabled and relabelled "Uploading..." — re-enable it so the admin
        // can retry instead of being stuck. App.Api already toasted the error.
        var btn = document.getElementById('admin-proof-submit-btn');
        if (btn) { btn.disabled = false; btn.textContent = 'Confirm Payment'; }
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
      +   (_invoiceBreakdownHtml(inv) || '<div style="font-size:1rem;font-weight:800;color:var(--gold);margin-top:2px">' + App.Utils.formatCurrency(inv.amount) + '</div>')
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
      +   '<p style="font-size:0.82rem;font-weight:600;color:#374151;margin:0 0 0.5rem">Upload payment receipt <span style="color:#64748b;font-weight:500">(optional)</span></p>'
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

    // Optional here too — a submission without a receipt still lands in
    // Pending Verification, so an admin confirms it against the bank before
    // anything is marked paid.
    var fileInput = document.getElementById('proof-file');
    var hasFile = fileInput && fileInput.files && fileInput.files[0];
    if (hasFile && fileInput.files[0].size > MAX_PROOF_BYTES) {
      App.Utils.showToast('Receipt must be under ' + (MAX_PROOF_BYTES / 1024 / 1024) + 'MB — try a photo, not a scan', 'error');
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
      .catch(function() {
        // Receipt is mandatory for non-cash — do NOT submit without it (this
        // matches the admin flow). Re-enable the button so the parent retries.
        if (submitBtn) { submitBtn.disabled = false; submitBtn.textContent = 'Submit Payment'; }
        App.Utils.showToast('Receipt upload failed — payment not submitted, please retry', 'error');
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
      // Honest failure: never fake a submitted state locally — the admin
      // would never see it and the parent would believe it went through.
      App.Utils.showToast('Payment submission failed — please check your connection and try again', 'error');
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
      +   _invoiceBreakdownHtml(inv)
      + '</div>'
      + proofSection
      // Older invoices can sit in Pending Verification with a non-cash method
      // and no reference (submitted before the reference became mandatory).
      // The server rejects confirming those, so let the admin supply the
      // reference from the receipt right here.
      + '<div style="margin-bottom:0.75rem"><p style="font-size:0.82rem;font-weight:600;color:#374151;margin:0 0 0.4rem">Reference number' + ((inv.paymentMethod && inv.paymentMethod !== 'Cash') ? ' <span style="color:#dc2626">*required for ' + App.Utils.esc(inv.paymentMethod) + '</span>' : '') + '</p>'
      +   '<input id="verify-ref" class="form-input" value="' + App.Utils.esc(inv.referenceNo || '') + '" placeholder="From the receipt or bank statement"></div>'
      + '<div style="display:flex;gap:0.5rem">'
      +   '<button onclick="App.Billing._confirmVerify(\'' + invId + '\')" style="flex:1;padding:0.55rem;font-size:0.85rem;font-weight:700;background:#16a34a;color:#fff;border:none;border-radius:8px;cursor:pointer">Confirm Payment</button>'
      +   '<button onclick="App.Billing._markUnpaid(\'' + invId + '\')" style="padding:0.55rem 1rem;font-size:0.83rem;border:1px solid #fca5a5;border-radius:8px;background:#fff;cursor:pointer;color:#dc2626;font-weight:600">Reject</button>'
      + '</div>'
      + '<button onclick="App.Utils.hideModal()" style="width:100%;padding:0.5rem;font-size:0.83rem;border:1px solid #e2e8f0;border-radius:8px;background:#fff;cursor:pointer;color:#64748b;margin-top:0.5rem">Cancel</button>'
      + '</div>'
    );
  }

  function _confirmVerify(invId) {
    // Persist via the pay endpoint — the previous version only mutated the
    // local store, so the next snapshot reload silently reverted the invoice
    // to Pending Verification. Server auto-stamps paid_on and assigns the
    // receipt number. The reference travels with the confirm so invoices
    // stuck without one (pre-mandatory-reference submissions) are fixable.
    var inv = (App.Store.get().invoices || []).find(function(i) { return i.id === invId; }) || {};
    var refEl = document.getElementById('verify-ref');
    var refNo = refEl ? refEl.value.trim() : '';
    if (inv.paymentMethod && inv.paymentMethod !== 'Cash' && !refNo) {
      App.Utils.showToast('Enter the reference number from the receipt — required for ' + inv.paymentMethod, 'error');
      return;
    }
    var payload = { status: 'Paid' };
    if (refNo) payload.referenceNo = refNo;
    App.Api.put('/api/invoices/' + invId + '/pay', payload)
      .then(function() { return App.Api.loadSnapshot(); })
      .then(function() {
        App.Utils.hideModal(true);
        App.Utils.showToast('Payment verified — invoice marked as Paid', 'success');
        App.Notifs && App.Notifs.refresh && App.Notifs.refresh();
        App.Router.refresh();
      })
      .catch(function() { /* App.Api already toasted; keep the modal open for a retry */ });
  }

  async function _deleteInvoice(invoiceId) {
    var ok = await App.Utils.showConfirm({ title: 'Delete invoice', message: 'This will be voided and removed from active reports.', confirmLabel: 'Delete', danger: true });
    if (!ok) return;
    var prev = App.Api.optimisticRemove('invoices', invoiceId);
    App.Router.refresh();

    // Optimistic + undoable: defer the actual DELETE until after the 6s undo
    // toast has closed. Firing before it closed let an Undo click in the final
    // ~500ms land after the DELETE had already committed. Pressing Undo
    // restores the local store and cancels the pending DELETE.
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
    }, 6500);
  }

  // _payOnline kicks off a hosted-checkout flow with the configured gateway
  // (Billplz / Stripe). The server returns a redirect URL; we navigate to it
  // and the gateway POSTs back to /api/payments/webhook/* on payment.
  var _payOnlineInFlight = false;
  async function _payOnline(invoiceId) {
    // Guard against rapid double-clicks minting duplicate gateway sessions.
    if (_payOnlineInFlight) return;
    _payOnlineInFlight = true;
    try {
      var res = await App.Api.post('/api/invoices/' + invoiceId + '/checkout', {});
      if (res && res.url) {
        window.location.href = res.url;
        return; // navigating away — keep the guard set so no second POST fires
      }
      App.Utils.showToast('Could not start checkout', 'error');
    } catch (err) {
      // App.Api auto-toasts; if the gateway is unconfigured it returns 502.
    }
    _payOnlineInFlight = false;
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
    _lineItems = [];
    _lineSeq = 0;
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
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Type</label><select name="type" class="form-input"><option>Monthly</option><option>Adhoc</option></select></div>'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Add package</label>'
      +   '<select id="pkg-catalog" class="form-input" onchange="App.Billing._addLineItem(this.value); this.selectedIndex=0;">'
      +   _packageCatalogOptions()
      +   '</select>'
      +   '<p class="text-xs text-slate-400 mt-1">Pick Group/Private by level, or self-study. Self-study within the free hours is added as an FOC line; use the add-on for extra hours.</p>'
      + '</div>'
      + '<div id="line-items-list" style="margin-top:0.25rem"></div>'
      + '<div id="line-items-total" style="text-align:right;font-size:0.9rem;color:#111;margin-top:0.35rem"></div>'
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
      +       _field('Discount (RM)', '<input id="discount-rm" name="discountRM" type="number" min="0" step="0.01" class="form-input" value="10" oninput="App.Billing._updateNetAmount()">')
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

    _renderLineItems();

    // Auto-enable early bird if within the early-bird window (1st–7th)
    (function() {
      if (new Date().getDate() <= 7) {
        var cb = document.getElementById('early-bird-cb');
        if (cb) { cb.checked = true; _toggleEarlyBird(); }
      }
    })();

    document.getElementById('create-invoice-form').addEventListener('submit', function(e) {
      e.preventDefault();
      var mode = _currentInvMode || 'single';
      if (mode === 'sibling') { _doSiblingInvoice(new FormData(e.target)); return; }
      if (mode === 'selfstudy') { _doSelfStudyInvoice(new FormData(e.target)); return; }
      // Single invoice — built from selected package line items. The server
      // derives the total from the items, so no amount is sent.
      var fd = new FormData(e.target);
      if (!fd.get('studentId')) { App.Utils.showToast('Select a student', 'warning'); return; }
      if (_lineItems.length === 0) { App.Utils.showToast('Add at least one package', 'warning'); return; }
      var lineItems = _lineItems.map(function(li) {
        return { kind: li.kind, name: li.name, descriptor: li.descriptor || '',
          qty: parseFloat(li.qty) || 0, unitPrice: parseFloat(li.unitPrice) || 0, amount: _lineItemAmount(li) };
      });
      var earlyBirdOn = document.getElementById('early-bird-cb') && document.getElementById('early-bird-cb').checked;
      var discountRM = earlyBirdOn ? (parseFloat(fd.get('discountRM')) || 0) : 0;
      if (discountRM > 0) {
        // Discount the NET subtotal so free/FOC lines (a +40 add-on cancelled
        // by a -40 credit) and existing discounts don't inflate the base. The
        // early-bird line isn't pushed yet, so this sums everything but it.
        var netSubtotal = lineItems.reduce(function(a, li) { return a + li.amount; }, 0);
        // Flat ringgit off, matching the monthly cron's EarlyBirdRM. Clamped
        // to the subtotal so a discount larger than the bill can't invert it.
        var eb = Math.min(discountRM, netSubtotal);
        if (eb > 0) {
          lineItems.push({ kind: 'discount', name: 'Early bird discount', descriptor: '', qty: 1, unitPrice: eb, amount: -eb });
        }
      }
      var newInvoice = {
        studentId: fd.get('studentId'),
        type: fd.get('type'),
        lineItems: lineItems,
        earlyBirdCutoff: fd.get('earlyBirdCutoff') || undefined,
        dueDate: fd.get('dueDate'),
        status: 'Unpaid',
        createdOn: fd.get('invoiceDate') || App.Utils.today(),
        paidOn: null
      };
      App.Api.post('/api/invoices', newInvoice).then(function() {
        return App.Api.loadSnapshot();
      }).then(function() {
        App.Utils.hideModal(true);
        App.Utils.showToast('Invoice created', 'success');
        App.Router.refresh();
      }).catch(function() {
        // Keep the modal open on failure so the built line items aren't lost
        // and the admin can fix and resubmit. App.Api already toasted the error.
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
    var sibling   = document.getElementById('inv-sibling-fields');
    var selfstudy = document.getElementById('inv-selfstudy-fields');
    if (single)    single.style.display    = mode === 'single'    ? 'block' : 'none';
    if (sibling)   sibling.style.display   = mode === 'sibling'   ? 'block' : 'none';
    if (selfstudy) selfstudy.style.display = mode === 'selfstudy' ? 'block' : 'none';

    // Early bird discount is irrelevant for flat-rate self-study.
    var earlyBird = document.getElementById('inv-early-bird-section');
    if (earlyBird) earlyBird.style.display = mode === 'selfstudy' ? 'none' : 'block';

    var btn = document.getElementById('inv-submit-btn');
    if (btn) {
      btn.textContent = mode === 'sibling' ? 'Create Sibling Invoice' : 'Create Invoice';
    }
    if (mode === 'selfstudy') _updateSelfStudyAmount();
  }

  function _toggleEarlyBird() {
    var cb = document.getElementById('early-bird-cb');
    var fields = document.getElementById('early-bird-fields');
    if (fields) fields.style.display = cb && cb.checked ? 'block' : 'none';
    _updateNetAmount();
  }

  function _updateNetAmount() {
    var preview = document.getElementById('net-amount-preview');
    if (!preview) return;
    // Base is the positive subtotal — from the package line items in single
    // mode, or the legacy amount field if one is present.
    var base = parseFloat((document.getElementById('inv-base-amount') || {}).value) || 0;
    if (base === 0) {
      base = _lineItems.reduce(function(a, li) { var amt = _lineItemAmount(li); return a + (amt > 0 ? amt : 0); }, 0);
    }
    var cb = document.getElementById('early-bird-cb');
    var rmEl = document.getElementById('discount-rm');
    if (cb && cb.checked && rmEl && base > 0) {
      // Flat ringgit, clamped so a large discount can't drive a negative total.
      var off = Math.min(parseFloat(rmEl.value) || 0, base);
      preview.textContent = 'After early bird: RM ' + (base - off).toFixed(2) + ' (saving RM ' + off.toFixed(2) + ')';
    } else {
      preview.textContent = '';
    }
    // The early-bird control is shared, and sibling invoices now apply it too,
    // so keep that preview in sync when the checkbox or percentage changes.
    if (_currentInvMode === 'sibling') _updateSiblingTotal();
  }

  function _updateSelfStudyAmount() {
    var visits = parseInt((document.querySelector('#create-invoice-form [name="ssVisits"]') || {}).value) || 0;
    var rate = parseFloat((document.querySelector('#create-invoice-form [name="ssRate"]') || {}).value) || 0;
    var preview = document.getElementById('selfstudy-amount-preview');
    if (!preview) return;
    preview.innerHTML = '<strong>Total: RM ' + (visits * rate).toFixed(2) + '</strong> (' + visits + ' session' + (visits !== 1 ? 's' : '') + ' × RM ' + rate.toFixed(2) + ')';
  }

  // _invoiceBreakdownHtml renders an invoice's line items on screen (the same
  // data the PDF uses). Returns '' for legacy invoices with no stored items so
  // callers can fall back to the plain description + amount.
  function _invoiceBreakdownHtml(inv) {
    var items = inv.lineItems || [];
    if (!items.length) return '';
    var rows = items.map(function(li) {
      var isDisc = li.kind === 'discount';
      var amt = (typeof li.amount === 'number') ? li.amount : (li.qty || 0) * (li.unitPrice || 0);
      var right = (amt < 0 ? '- RM ' : 'RM ') + Math.abs(amt).toFixed(2);
      var sub = isDisc ? '' : ((li.descriptor ? App.Utils.esc(li.descriptor) : '')
        + (li.qty ? '<span style="color:#cbd5e1"> · ' + App.Utils.esc(String(li.qty)) + ' × RM ' + (li.unitPrice || 0).toFixed(2) + '</span>' : ''));
      return '<div style="display:flex;justify-content:space-between;gap:1rem;padding:0.35rem 0;border-bottom:1px solid #f2efea">'
        + '<div style="min-width:0"><div style="font-size:0.8rem;font-weight:600;color:' + (isDisc ? '#166534' : '#111') + '">' + App.Utils.esc(li.name) + '</div>'
        + (sub ? '<div style="font-size:0.7rem;color:#94a3b8">' + sub + '</div>' : '') + '</div>'
        + '<div style="font-size:0.8rem;font-weight:600;white-space:nowrap;color:' + (isDisc ? '#166534' : '#111') + '">' + right + '</div>'
        + '</div>';
    }).join('');
    return '<div style="margin:0.5rem 0">' + rows
      + '<div style="display:flex;justify-content:space-between;padding-top:0.5rem;font-weight:800"><span>Total</span><span>' + App.Utils.formatCurrency(inv.amount) + '</span></div>'
      + '</div>';
  }

  // _viewInvoiceModal shows the full itemized breakdown on screen (admin + parent)
  // with PDF/receipt download, opened by clicking an invoice's description.
  function _viewInvoiceModal(invId) {
    var state = App.Store.get();
    var inv = state.invoices.find(function(i) { return i.id === invId; });
    if (!inv) return;
    var stu = (state.students || []).find(function(s) { return s.id === inv.studentId; });
    var stuName = stu ? stu.firstName + ' ' + stu.lastName : inv.studentId;
    var breakdown = _invoiceBreakdownHtml(inv);
    var fallback = '<div style="background:#f8fafc;border-radius:10px;padding:0.85rem 1rem;margin:0.5rem 0"><div style="display:flex;justify-content:space-between;font-weight:700"><span>' + App.Utils.esc(inv.description) + '</span><span>' + App.Utils.formatCurrency(inv.amount) + '</span></div></div>';
    App.Utils.showModal(
      '<div class="p-6" style="min-width:360px;max-width:480px">'
      + '<h2 style="font-size:1.1rem;font-weight:700;color:#111;margin:0 0 0.15rem">' + App.Utils.esc(inv.description) + '</h2>'
      + '<p style="font-size:0.8rem;color:#94a3b8;margin:0 0 0.75rem">' + App.Utils.esc(stuName) + ' · ' + App.Utils.esc(inv.id) + ' · ' + App.Utils.statusBadge(inv.status) + '</p>'
      + (breakdown || fallback)
      + '<div style="display:flex;gap:1rem;font-size:0.78rem;color:#64748b;margin-top:0.75rem"><span>Issued ' + App.Utils.formatDate(inv.createdOn) + '</span><span>Due ' + App.Utils.formatDate(inv.dueDate) + '</span></div>'
      + '<div style="display:flex;justify-content:flex-end;gap:0.5rem;margin-top:1.25rem">'
      +   '<a href="/api/invoices/' + inv.id + '/pdf" target="_blank" style="padding:0.5rem 1rem;font-size:0.82rem;font-weight:600;border:1px solid #e2e8f0;border-radius:8px;color:#374151;text-decoration:none">Download PDF</a>'
      +   (inv.status === 'Paid' ? '<a href="/api/invoices/' + inv.id + '/receipt.pdf" target="_blank" style="padding:0.5rem 1rem;font-size:0.82rem;font-weight:600;border:1px solid #e2e8f0;border-radius:8px;color:#374151;text-decoration:none">Receipt</a>' : '')
      +   '<button onclick="App.Utils.hideModal()" style="padding:0.5rem 1rem;font-size:0.82rem;font-weight:600;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Close</button>'
      + '</div>'
      + '</div>'
    );
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
      lineItems: [{ kind: 'item', name: 'Self-study drop-in', descriptor: visits + ' session' + (visits !== 1 ? 's' : ''), qty: visits, unitPrice: rate, amount: parseFloat((visits * rate).toFixed(2)) }],
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
    let total = parseFloat((discounted * count).toFixed(2));
    // Early bird is a shared control that stays visible in sibling mode and IS
    // applied on submit — the preview has to show it or the admin reads a
    // different figure than the invoice they create.
    const ebOn = document.getElementById('early-bird-cb') && document.getElementById('early-bird-cb').checked;
    const ebRM = ebOn ? (parseFloat((document.getElementById('discount-rm') || {}).value) || 0) : 0;
    const eb = Math.min(ebRM, total);
    total = parseFloat((total - eb).toFixed(2));
    preview.style.display = 'block';
    preview.innerHTML = count + ' child' + (count !== 1 ? 'ren' : '') + ' × RM ' + discounted.toFixed(2)
      + (discount > 0 ? ' (' + discount + '% sibling discount applied)' : '')
      + (eb > 0 ? ' − RM ' + eb.toFixed(2) + ' early bird' : '')
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
    const childNames = children.map(function(c) { return c.firstName; }).join(' + ');
    const desc = description + ' — ' + childNames + (discount > 0 ? ' (' + discount + '% sibling discount)' : '');

    // Itemize: one tuition line per child at the full per-child rate, then a
    // single sibling-discount line. The server derives the total from these.
    const lineItems = children.map(function(c) {
      return { kind: 'item', name: 'Monthly tuition', descriptor: c.firstName + ' ' + c.lastName, qty: 1, unitPrice: perChild, amount: perChild };
    });
    if (discount > 0) {
      const totalDisc = parseFloat(((perChild - discounted) * children.length).toFixed(2));
      if (totalDisc > 0) lineItems.push({ kind: 'discount', name: 'Sibling discount (' + discount + '%)', qty: 1, unitPrice: totalDisc, amount: -totalDisc });
    }
    // Early bird (shared checkbox, visible in sibling mode) applies to the net
    // subtotal after the sibling discount — same rule as the single-invoice flow.
    var earlyBirdOn = document.getElementById('early-bird-cb') && document.getElementById('early-bird-cb').checked;
    var ebRM = earlyBirdOn ? (parseFloat(fd.get('discountRM')) || 0) : 0;
    if (ebRM > 0) {
      var ebBase = lineItems.reduce(function(a, li) { return a + li.amount; }, 0);
      var eb = Math.min(ebRM, ebBase);
      if (eb > 0) lineItems.push({ kind: 'discount', name: 'Early bird discount', qty: 1, unitPrice: eb, amount: -eb });
    }
    const finalTotal = lineItems.reduce(function(a, li) { return a + li.amount; }, 0);

    // Create a single combined invoice linked to the first child, with
    // siblings listed in the description. Persisted to backend so it shows
    // up consistently for both the parent and admin views.
    const newInvoice = {
      studentId: children[0].id,  // primary child
      siblingIds: JSON.stringify(children.slice(1).map(function(c) { return c.id; })),
      description: desc,
      type: 'Monthly',
      lineItems: lineItems,
      siblingDiscount: discount || undefined,
      earlyBirdCutoff: fd.get('earlyBirdCutoff') || undefined,
      dueDate: dueDate,
      status: 'Unpaid',
      createdOn: App.Utils.today(),
      paidOn: null
    };

    App.Api.post('/api/invoices', newInvoice).then(function() {
      return App.Api.loadSnapshot();
    }).then(function() {
      App.Utils.hideModal(true);
      App.Utils.showToast('Sibling invoice created — RM ' + finalTotal.toFixed(2) + ' for ' + childNames, 'success');
      App.Router.refresh();
    }).catch(function() {
      // Keep the modal open on failure so the selected children and amounts
      // aren't lost. App.Api already toasted the error.
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
    _confirmCash: _confirmCash,
    _confirmCashSubmit: _confirmCashSubmit,
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
    _viewInvoiceModal: _viewInvoiceModal,
    _createModal: _createModal,
    checkLoginNotifications: checkLoginNotifications,
    _toggleSelectAllInv: _toggleSelectAllInv,
    _toggleSelectInv: _toggleSelectInv,
    _bulkDeselectInv: _bulkDeselectInv,
    _bulkMarkPaid: _bulkMarkPaid,
    _bulkDeleteInv: _bulkDeleteInv,
    _bulkDeleteInvConfirm: _bulkDeleteInvConfirm,
    _bulkConfirmPaid: _bulkConfirmPaid,
    _setInvMode: _setInvMode,
    _toggleEarlyBird: _toggleEarlyBird,
    _updateNetAmount: _updateNetAmount,
    _addLineItem: _addLineItem,
    _removeLineItem: _removeLineItem,
    _editLineItem: _editLineItem,
    _updateSelfStudyAmount: _updateSelfStudyAmount,
    _updateSiblingChildren: _updateSiblingChildren,
    _updateSiblingTotal: _updateSiblingTotal,
    _exportCSV: _exportCSV,
    _setPage: _setBillingPage
  };
})();
