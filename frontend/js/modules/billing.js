(function() {
  window.App = window.App || {};

  let _filter = 'Unpaid';
  let _studentFilter = ''; // empty = all students
  let _menuListenerAdded = false;
  let _selectedInv = {};

  function render(container) {
    const { invoices, students } = App.Store.get();
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
      : displayInvoices.filter(function(i) { return i.status === _filter; });

    const totalRevenue = displayInvoices.reduce(function(s, i) { return s + i.amount; }, 0);
    const collected    = displayInvoices.filter(function(i) { return i.status === 'Paid'; }).reduce(function(s, i) { return s + i.amount; }, 0);
    const pending      = displayInvoices.filter(function(i) { return i.status === 'Unpaid'; }).reduce(function(s, i) { return s + i.amount; }, 0);
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

    const colCount = isAdmin ? 8 : 6;

    container.innerHTML = ''
      + '<div class="flex items-center justify-between mb-6">'
      +   '<h1 class="text-2xl font-bold text-slate-800">Billing</h1>'
      +   (isAdmin
          ? '<div class="flex gap-2">'
          + '<button onclick="App.Billing._generateMonthlyModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50 text-slate-600">Generate Monthly</button>'
          + '<button onclick="App.Billing._siblingInvoiceModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50 text-slate-600">Sibling Invoice</button>'
          + '<button onclick="App.Billing._exportCSV()" class="px-4 py-2 text-sm bg-emerald-600 text-white rounded-lg hover:bg-emerald-700">Export CSV</button>'
          + '<button onclick="App.Billing._createModal()" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">+ Create Invoice</button>'
          + '</div>'
          : '')
      + '</div>'

      + notifBanner

      + '<div class="grid grid-cols-4 gap-4 mb-6">'
      + _statCard('Total Revenue', App.Utils.formatCurrency(totalRevenue), 'text-slate-700', 'bg-slate-50')
      + _statCard('Collected', App.Utils.formatCurrency(collected), 'text-emerald-600', 'bg-emerald-50')
      + _statCard('Pending', App.Utils.formatCurrency(pending), 'text-amber-600', 'bg-amber-50')
      + _statCard('Overdue', App.Utils.formatCurrency(overdue), 'text-red-600', 'bg-red-50')
      + '</div>'

      + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm">'
      +   '<div class="p-4 border-b border-slate-100 flex items-center gap-2 flex-wrap">'
      // Filter tabs
      +   ['Unpaid','Overdue','All','Archive'].map(function(f) {
            const active = f === _filter;
            const isArchive = f === 'Archive';
            return '<button onclick="App.Billing._setFilter(\'' + f + '\')" class="px-3 py-1.5 text-sm rounded-lg font-medium transition-colors '
              + (active ? (isArchive ? 'bg-slate-600 text-white' : 'bg-blue-600 text-white') : 'text-slate-600 hover:bg-slate-100')
              + '">' + (isArchive ? 'Archive (Paid)' : f) + '</button>';
          }).join('')
      // Student filter dropdown
      +   '<div class="ml-auto">'
      +     '<select onchange="App.Billing._setStudentFilter(this.value)" class="text-sm border border-slate-200 rounded-lg px-3 py-1.5 focus:outline-none focus:ring-2 focus:ring-blue-400 text-slate-600">'
      +     '<option value="">All Students</option>'
      +     students.map(function(s) {
              return '<option value="' + s.id + '"' + (s.id === _studentFilter ? ' selected' : '') + '>' + s.firstName + ' ' + s.lastName + '</option>';
            }).join('')
      +     '</select>'
      +   '</div>'
      +   '</div>'
      +   (isAdmin ? '<div id="inv-bulk-bar" style="padding:0 1rem">' + _bulkBar() + '</div>' : '')
      +   '<div class="overflow-x-auto">'
      +     '<table class="w-full">'
      +       '<thead class="bg-slate-50 border-b border-slate-100"><tr>'
      +         (isAdmin ? '<th class="th" style="width:36px"><input type="checkbox" id="select-all-inv-cb" onchange="App.Billing._toggleSelectAllInv(this.checked)" style="cursor:pointer"></th>' : '')
      +         '<th class="th">Student</th><th class="th">Description</th><th class="th">Type</th>'
      +         '<th class="th">Due Date</th><th class="th text-right">Amount</th><th class="th">Status</th>'
      +         (isAdmin ? '<th class="th w-10"></th>' : '')
      +       '</tr></thead>'
      +       '<tbody class="divide-y divide-slate-50">'
      +       (filtered.length === 0
          ? '<tr><td colspan="' + colCount + '" style="padding:0">' + App.Utils.emptyState(
              (_filter !== 'All' || _studentFilter) ? 'No invoices match this filter' : 'No invoices yet',
              (_filter !== 'All' || _studentFilter) ? 'Try selecting a different filter or student.' : 'Create your first invoice to start tracking payments.',
              (isAdmin && _filter === 'All' && !_studentFilter) ? '<button onclick="App.Billing._createModal()" style="padding:0.5rem 1.25rem;font-size:0.83rem;font-weight:600;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">+ Create Invoice</button>' : ''
            ) + '</td></tr>'
          : filtered.map(function(inv) {
              const stu = students.find(function(s) { return s.id === inv.studentId; });
              const stuName = stu ? stu.firstName + ' ' + stu.lastName : inv.studentId;
              const isNearDue = inv.status === 'Unpaid' && new Date(inv.dueDate) <= in7 && new Date(inv.dueDate) >= today;
              return '<tr class="hover:bg-slate-50 transition-colors">'
                + (isAdmin ? '<td class="td" style="width:36px"><input type="checkbox" class="inv-cb" data-id="' + inv.id + '" onchange="App.Billing._toggleSelectInv(\'' + inv.id + '\',this.checked)" style="cursor:pointer"' + (_selectedInv[inv.id] ? ' checked' : '') + '></td>' : '')
                + '<td class="td"><div class="font-medium text-slate-800">' + stuName + '</div><div class="text-xs text-slate-400">' + inv.id + '</div></td>'
                + '<td class="td text-sm text-slate-600">' + inv.description + '</td>'
                + '<td class="td">' + App.Utils.badge(inv.type, inv.type === 'Monthly' ? 'blue' : 'purple') + '</td>'
                + '<td class="td text-sm ' + (isNearDue ? 'text-amber-600 font-medium' : 'text-slate-600') + '">'
                +   App.Utils.formatDate(inv.dueDate) + (isNearDue ? ' <span class="text-xs">(soon)</span>' : '')
                + '</td>'
                + '<td class="td text-sm font-semibold text-slate-800 text-right">' + App.Utils.formatCurrency(inv.amount) + '</td>'
                + '<td class="td">' + App.Utils.statusBadge(inv.status) + '</td>'
                + (isAdmin ? '<td class="td">'
                  + '<div class="relative flex justify-center">'
                  +   '<button onclick="App.Billing._toggleMenu(event,\'' + inv.id + '\')" class="w-7 h-7 flex items-center justify-center rounded-lg hover:bg-slate-100 text-slate-400 hover:text-slate-700 text-lg leading-none font-bold">&#8942;</button>'
                  +   '<div id="inv-menu-' + inv.id + '" class="inv-menu hidden absolute right-0 top-8 z-20 bg-white border border-slate-200 shadow-xl rounded-xl py-1 min-w-36">'
                  +     (inv.status !== 'Paid'
                          ? '<button onclick="App.Billing._markPaid(\'' + inv.id + '\')" class="w-full text-left px-4 py-2 text-sm hover:bg-slate-50 text-slate-700">Mark as Paid</button>'
                          : '<button onclick="App.Billing._markUnpaid(\'' + inv.id + '\')" class="w-full text-left px-4 py-2 text-sm hover:bg-slate-50 text-slate-700">Mark Unpaid</button>')
                  +     '<button onclick="App.Billing._editModal(\'' + inv.id + '\')" class="w-full text-left px-4 py-2 text-sm hover:bg-slate-50 text-slate-700">Edit</button>'
                  +     '<div class="my-1 border-t border-slate-100"></div>'
                  +     '<button onclick="App.Billing._deleteInvoice(\'' + inv.id + '\')" class="w-full text-left px-4 py-2 text-sm hover:bg-red-50 text-red-600">Delete</button>'
                  +   '</div>'
                  + '</div>'
                  + '</td>' : '')
                + '</tr>';
            }).join(''))
      +       '</tbody>'
      +     '</table>'
      +   '</div>'
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
    App.Utils.hideModal();
    App.Utils.showToast('Marked ' + ids.length + ' invoice' + (ids.length !== 1 ? 's' : '') + ' as paid · ' + method, 'success');
    App.Notifs.refresh();
    _selectedInv = {};
    App.Router.refresh();
  }

  function _statCard(label, value, textClass, bgClass) {
    return '<div class="' + bgClass + ' rounded-xl border border-slate-100 shadow-sm p-4">'
      + '<div class="text-xl font-bold ' + textClass + '">' + value + '</div>'
      + '<div class="text-xs text-slate-500 mt-1">' + label + '</div>'
      + '</div>';
  }

  function _setFilter(f) { _filter = f; App.Router.refresh(); }
  function _setStudentFilter(v) { _studentFilter = v; App.Router.refresh(); }

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
      + '<div class="grid grid-cols-3 gap-3 mb-5">'
      + ['Cash', 'Bank Transfer', 'QR Pay'].map(function(m) {
          return '<button onclick="App.Billing._confirmPaid(\'' + invId + '\',\'' + m + '\')" '
            + 'class="p-3 border-2 border-slate-200 rounded-xl text-sm font-semibold text-slate-700 hover:border-blue-400 hover:bg-blue-50 transition-all text-center">' + m + '</button>';
        }).join('')
      + '</div>'
      + '<button onclick="App.Utils.hideModal()" class="w-full py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '</div>';
    App.Utils.showModal(html);
  }

  function _confirmPaid(invId, method) {
    var { invoices } = App.Store.get();
    var inv = invoices.find(function(i) { return i.id === invId; });
    if (!inv) return;
    App.Api && App.Api.updateInvoice
      ? App.Api.updateInvoice(invId, { status: 'Paid', paidOn: App.Utils.today(), paymentMethod: method })
          .then(function() {
            App.Utils.hideModal();
            App.Utils.showToast('Marked paid · ' + method, 'success');
            App.Notifs.refresh();
            App.Router.refresh();
          }).catch(function() {
            var updated = invoices.map(function(i) {
              return i.id === invId ? Object.assign({}, i, { status: 'Paid', paidOn: App.Utils.today(), paymentMethod: method }) : i;
            });
            App.Store.set({ invoices: updated });
            App.Utils.hideModal();
            App.Utils.showToast('Marked paid · ' + method, 'success');
            App.Notifs.refresh();
            App.Router.refresh();
          })
      : (function() {
          var updated = invoices.map(function(i) {
            return i.id === invId ? Object.assign({}, i, { status: 'Paid', paidOn: App.Utils.today(), paymentMethod: method }) : i;
          });
          App.Store.set({ invoices: updated });
          App.Utils.hideModal();
          App.Utils.showToast('Marked paid · ' + method, 'success');
          App.Notifs.refresh();
          App.Router.refresh();
        })();
  }

  function _markPaid(invoiceId) {
    _markPaidModal(invoiceId);
  }

  function _markUnpaid(invoiceId) {
    const state = App.Store.get();
    App.Store.set({ invoices: state.invoices.map(function(inv) {
      return inv.id === invoiceId ? Object.assign({}, inv, { status: 'Unpaid', paidOn: null }) : inv;
    })});
    App.Utils.showToast('Invoice marked as unpaid', 'info');
    App.Router.refresh();
  }

  function _deleteInvoice(invoiceId) {
    if (!confirm('Delete this invoice? This cannot be undone.')) return;
    const state = App.Store.get();
    App.Store.set({ invoices: state.invoices.filter(function(inv) { return inv.id !== invoiceId; }) });
    App.Utils.showToast('Invoice deleted', 'info');
    App.Router.refresh();
  }

  function _editModal(invoiceId) {
    const state = App.Store.get();
    const inv = state.invoices.find(function(i) { return i.id === invoiceId; });
    if (!inv) return;
    const stu = state.students.find(function(s) { return s.id === inv.studentId; });
    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-1">Edit Invoice</h2>'
      + '<p class="text-sm text-slate-500 mb-5">' + (stu ? stu.firstName + ' ' + stu.lastName : inv.studentId) + ' · ' + inv.id + '</p>'
      + '<form id="edit-invoice-form" class="space-y-4">'
      + _field('Description', '<input name="description" class="form-input" value="' + inv.description + '" required>')
      + '<div class="grid grid-cols-2 gap-4">'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Type</label><select name="type" class="form-input"><option' + (inv.type==='Monthly'?' selected':'') + '>Monthly</option><option' + (inv.type==='Adhoc'?' selected':'') + '>Adhoc</option></select></div>'
      + _field('Amount (RM)', '<input name="amount" type="number" min="0" step="0.01" class="form-input" value="' + inv.amount + '" required>')
      + '</div>'
      + _field('Due Date', '<input name="dueDate" type="date" class="form-input" value="' + inv.dueDate + '" required>')
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
      const st = App.Store.get();
      App.Store.set({ invoices: st.invoices.map(function(i) {
        if (i.id !== invoiceId) return i;
        return Object.assign({}, i, {
          description: fd.get('description'),
          type: fd.get('type'),
          amount: parseFloat(fd.get('amount')),
          dueDate: fd.get('dueDate')
        });
      })});
      App.Utils.hideModal();
      App.Utils.showToast('Invoice updated', 'success');
      App.Router.refresh();
    });
  }

  function _createModal() {
    const { students } = App.Store.get();
    App.Utils.showModal(
      '<div class="p-6">'
      + '<h2 class="text-xl font-bold mb-4">Create Invoice</h2>'
      + '<form id="create-invoice-form" class="space-y-4">'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Student</label>'
      + '<select name="studentId" class="form-input" required>'
      + '<option value="">Select student...</option>'
      + students.map(function(s) {
          return '<option value="' + s.id + '"' + (s.id === _studentFilter ? ' selected' : '') + '>' + s.firstName + ' ' + s.lastName + '</option>';
        }).join('')
      + '</select></div>'
      + _field('Description', '<input name="description" class="form-input" placeholder="e.g. Mar 2026 Tuition" required>')
      + '<div class="grid grid-cols-2 gap-4">'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Type</label><select name="type" class="form-input"><option>Monthly</option><option>Adhoc</option></select></div>'
      + _field('Amount (RM)', '<input id="inv-base-amount" name="amount" type="number" min="0" step="0.01" class="form-input" required oninput="App.Billing._updateNetAmount()">')
      + '</div>'
      // Early bird discount
      + '<div style="background:#fafaf8;border:1px solid #f0ede8;border-radius:10px;padding:0.85rem">'
      +   '<div style="display:flex;align-items:center;gap:0.6rem;margin-bottom:0.5rem">'
      +     '<input type="checkbox" id="early-bird-cb" onchange="App.Billing._toggleEarlyBird()" style="width:16px;height:16px;accent-color:var(--gold);cursor:pointer">'
      +     '<label for="early-bird-cb" style="font-size:0.83rem;font-weight:600;color:#374151;cursor:pointer">Early Bird Discount</label>'
      +   '</div>'
      +   '<div id="early-bird-fields" style="display:none;margin-top:0.5rem">'
      +     '<div class="grid grid-cols-2 gap-3">'
      +       _field('Discount %', '<input id="discount-pct" name="discountPct" type="number" min="0" max="100" step="1" class="form-input" value="10" oninput="App.Billing._updateNetAmount()">')
      +       _field('Pay by (cutoff)', '<input name="earlyBirdCutoff" type="date" class="form-input">')
      +     '</div>'
      +     '<div id="net-amount-preview" style="margin-top:0.5rem;font-size:0.82rem;color:#6b7280"></div>'
      +   '</div>'
      + '</div>'
      + _field('Due Date', '<input name="dueDate" type="date" class="form-input" required>')
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">Create Invoice</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );
    document.getElementById('create-invoice-form').addEventListener('submit', function(e) {
      e.preventDefault();
      const fd = new FormData(e.target);
      const state = App.Store.get();
      const baseAmount = parseFloat(fd.get('amount')) || 0;
      const discountPct = document.getElementById('early-bird-cb') && document.getElementById('early-bird-cb').checked
        ? (parseFloat(fd.get('discountPct')) || 0) : 0;
      const finalAmount = parseFloat((baseAmount * (1 - discountPct / 100)).toFixed(2));
      const newInvoice = {
        id: 'INV' + String(state.invoices.length + 1).padStart(3,'0'),
        studentId: fd.get('studentId'),
        description: fd.get('description') + (discountPct > 0 ? ' (' + discountPct + '% early bird)' : ''),
        type: fd.get('type'),
        amount: finalAmount,
        discountPct: discountPct || undefined,
        earlyBirdCutoff: fd.get('earlyBirdCutoff') || undefined,
        dueDate: fd.get('dueDate'),
        status: 'Unpaid',
        createdOn: App.Utils.today(),
        paidOn: null
      };
      App.Store.set({ invoices: [...state.invoices, newInvoice] });
      App.Utils.hideModal();
      App.Utils.showToast('Invoice created' + (discountPct > 0 ? ' with ' + discountPct + '% early bird discount' : ''), 'success');
      App.Router.refresh();
    });
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

  function _generateMonthlyModal() {
    const state = App.Store.get();
    const activeStudents = state.students.filter(function(s) { return s.status === 'Active' || s.status === 'New'; });
    // Default month = current month
    const now = new Date();
    const defaultMonth = now.getFullYear() + '-' + String(now.getMonth() + 1).padStart(2,'0');
    // Last day of current month for due date
    const lastDay = new Date(now.getFullYear(), now.getMonth() + 1, 0);
    const defaultDue = lastDay.getFullYear() + '-' + String(lastDay.getMonth() + 1).padStart(2,'0') + '-' + String(lastDay.getDate()).padStart(2,'0');

    App.Utils.showModal(
      '<div class="p-6" style="min-width:420px;max-width:520px">'
      + '<h2 class="text-xl font-bold mb-1">Generate Monthly Invoices</h2>'
      + '<p class="text-sm text-slate-500 mb-5">Creates one invoice per active student for the selected month. Students who already have a Monthly invoice for that month are skipped.</p>'
      + '<form id="gen-monthly-form" class="space-y-4">'
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Month', '<input name="month" type="month" class="form-input" value="' + defaultMonth + '" required onchange="App.Billing._previewMonthly(this.value)">')
      + _field('Default Amount (RM)', '<input id="gen-amount" name="amount" type="number" min="0" step="0.01" class="form-input" value="150" required oninput="App.Billing._updateGenPreview()">')
      + '</div>'
      // Early bird section
      + '<div style="background:#fafaf8;border:1px solid #f0ede8;border-radius:10px;padding:0.85rem">'
      +   '<div style="display:flex;align-items:center;gap:0.6rem;margin-bottom:0.5rem">'
      +     '<input type="checkbox" id="gen-early-bird-cb" onchange="App.Billing._updateGenPreview()" style="width:16px;height:16px;accent-color:var(--gold);cursor:pointer">'
      +     '<label for="gen-early-bird-cb" style="font-size:0.83rem;font-weight:600;color:#374151;cursor:pointer">Early Bird Discount</label>'
      +   '</div>'
      +   '<div id="gen-eb-fields" style="display:none">'
      +     '<div class="grid grid-cols-2 gap-3">'
      +       _field('Discount %', '<input id="gen-discount-pct" name="discountPct" type="number" min="0" max="100" step="1" class="form-input" value="10" oninput="App.Billing._updateGenPreview()">')
      +       _field('Pay by (cutoff)', '<input name="earlyBirdCutoff" type="date" class="form-input">')
      +     '</div>'
      +   '</div>'
      + '</div>'
      + _field('Due Date', '<input name="dueDate" type="date" class="form-input" value="' + defaultDue + '" required>')
      + '<div id="gen-preview" style="background:#f0fdf4;border:1px solid #bbf7d0;border-radius:10px;padding:0.75rem;font-size:0.82rem;color:#166534"></div>'
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" style="padding:0.5rem 1.1rem;font-size:0.85rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Generate Invoices</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );

    // Toggle early bird fields
    document.getElementById('gen-early-bird-cb').addEventListener('change', function() {
      var fields = document.getElementById('gen-eb-fields');
      if (fields) fields.style.display = this.checked ? 'block' : 'none';
    });

    // Initial preview
    _updateGenPreview();

    document.getElementById('gen-monthly-form').addEventListener('submit', function(e) {
      e.preventDefault();
      const fd = new FormData(e.target);
      _doGenerateMonthly(fd);
    });
  }

  function _updateGenPreview() {
    var monthInput = document.querySelector('#gen-monthly-form [name="month"]');
    var month = monthInput ? monthInput.value : '';
    _previewMonthly(month);
  }

  function _previewMonthly(month) {
    var preview = document.getElementById('gen-preview');
    if (!preview || !month) return;
    var state = App.Store.get();
    var [yr, mo] = month.split('-');
    var monthLabel = new Date(parseInt(yr), parseInt(mo) - 1, 1).toLocaleDateString('en-MY', { month:'long', year:'numeric' });
    var activeStudents = state.students.filter(function(s) { return s.status === 'Active' || s.status === 'New'; });
    var alreadyHas = {};
    state.invoices.forEach(function(i) {
      if (i.type === 'Monthly' && i.createdOn && i.createdOn.startsWith(month)) alreadyHas[i.studentId] = true;
    });
    var toCreate = activeStudents.filter(function(s) { return !alreadyHas[s.id]; });
    var skipped  = activeStudents.filter(function(s) { return  alreadyHas[s.id]; });

    var baseAmount = parseFloat((document.getElementById('gen-amount') || {}).value) || 0;
    var ebCb = document.getElementById('gen-early-bird-cb');
    var pctEl = document.getElementById('gen-discount-pct');
    var discountPct = (ebCb && ebCb.checked && pctEl) ? (parseFloat(pctEl.value) || 0) : 0;
    var net = parseFloat((baseAmount * (1 - discountPct / 100)).toFixed(2));

    preview.innerHTML = '<strong>' + toCreate.length + ' invoice' + (toCreate.length !== 1 ? 's' : '') + ' will be created</strong> for ' + monthLabel
      + ' · RM ' + net.toFixed(2) + ' each'
      + (discountPct > 0 ? ' <span style="color:#92400e">(' + discountPct + '% early bird)</span>' : '')
      + (skipped.length > 0 ? '<br><span style="color:#94a3b8">' + skipped.length + ' student' + (skipped.length !== 1 ? 's' : '') + ' skipped (already invoiced)</span>' : '');
  }

  function _doGenerateMonthly(fd) {
    var month = fd.get('month');
    var baseAmount = parseFloat(fd.get('amount')) || 0;
    var dueDate = fd.get('dueDate');
    var ebCb = document.getElementById('gen-early-bird-cb');
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
      App.Utils.hideModal();
      return;
    }

    var existing = state.invoices;
    var newInvoices = toCreate.map(function(s, idx) {
      return {
        id: 'INV' + String(existing.length + idx + 1).padStart(3,'0'),
        studentId: s.id,
        description: monthLabel + ' Tuition' + (discountPct > 0 ? ' (' + discountPct + '% early bird)' : ''),
        type: 'Monthly',
        amount: finalAmount,
        discountPct: discountPct || undefined,
        earlyBirdCutoff: earlyBirdCutoff,
        dueDate: dueDate,
        status: 'Unpaid',
        createdOn: month + '-01',
        paidOn: null
      };
    });

    App.Store.set({ invoices: existing.concat(newInvoices) });
    App.Utils.hideModal();
    App.Utils.showToast('Generated ' + newInvoices.length + ' invoices for ' + monthLabel, 'success');
    App.Router.refresh();
  }

  function _siblingInvoiceModal() {
    const { students } = App.Store.get();
    // Group students by parent contact (only families with 2+ children)
    const byParent = {};
    students.forEach(function(s) {
      if (!s.contact) return;
      byParent[s.contact] = byParent[s.contact] || [];
      byParent[s.contact].push(s);
    });
    const families = Object.keys(byParent).filter(function(email) { return byParent[email].length >= 2; });

    if (families.length === 0) {
      App.Utils.showToast('No families with multiple children found.', 'info');
      return;
    }

    const now = new Date();
    const lastDay = new Date(now.getFullYear(), now.getMonth() + 1, 0);
    const defaultDue = lastDay.getFullYear() + '-' + String(lastDay.getMonth() + 1).padStart(2,'0') + '-' + String(lastDay.getDate()).padStart(2,'0');
    const defaultMonth = now.toLocaleDateString('en-MY', { month: 'long', year: 'numeric' });

    App.Utils.showModal(
      '<div class="p-6" style="min-width:420px;max-width:520px">'
      + '<h2 class="text-xl font-bold mb-1">Sibling Invoice</h2>'
      + '<p class="text-sm text-slate-500 mb-5">Create one combined invoice covering multiple children from the same family.</p>'
      + '<form id="sibling-invoice-form" class="space-y-4">'
      + '<div><label class="block text-sm font-medium text-slate-700 mb-1">Family</label>'
      + '<select id="sibling-family-select" name="parentEmail" class="form-input" required onchange="App.Billing._updateSiblingChildren(this.value)">'
      + '<option value="">Select family...</option>'
      + families.map(function(email) {
          const children = byParent[email];
          const label = children[0].parentName + ' (' + children.map(function(c) { return c.firstName; }).join(', ') + ')';
          return '<option value="' + App.Utils.esc(email) + '">' + App.Utils.esc(label) + '</option>';
        }).join('')
      + '</select></div>'
      + '<div id="sibling-children-list" style="display:none;background:#fafaf8;border:1px solid #f0ede8;border-radius:10px;padding:0.75rem">'
      +   '<p class="text-xs font-semibold text-slate-500 mb-2">Include children:</p>'
      +   '<div id="sibling-children-checks"></div>'
      + '</div>'
      + _field('Description', '<input name="description" class="form-input" value="' + defaultMonth + ' Tuition" required>')
      + '<div class="grid grid-cols-2 gap-4">'
      + _field('Amount per child (RM)', '<input id="sibling-per-child" name="amountPerChild" type="number" min="0" step="0.01" class="form-input" value="150" required oninput="App.Billing._updateSiblingTotal()">')
      + _field('Sibling Discount %', '<input id="sibling-discount" name="siblingDiscount" type="number" min="0" max="100" step="1" class="form-input" value="10" oninput="App.Billing._updateSiblingTotal()">')
      + '</div>'
      + '<div id="sibling-total-preview" style="background:#f0fdf4;border:1px solid #bbf7d0;border-radius:10px;padding:0.65rem;font-size:0.82rem;color:#166534;display:none"></div>'
      + _field('Due Date', '<input name="dueDate" type="date" class="form-input" value="' + defaultDue + '" required>')
      + '<div class="flex justify-end gap-3 pt-2">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="submit" style="padding:0.5rem 1.1rem;font-size:0.85rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Create Sibling Invoice</button>'
      + '</div>'
      + '</form>'
      + '</div>'
    );

    document.getElementById('sibling-invoice-form').addEventListener('submit', function(e) {
      e.preventDefault();
      const fd = new FormData(e.target);
      _doSiblingInvoice(fd);
    });
  }

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
        + '<span style="color:#94a3b8;font-size:0.75rem">(' + s.status + ')</span>'
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
    const description = fd.get('description');
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

    // Create a single combined invoice linked to the first child, with siblings listed in description
    const newInvoice = {
      id: 'INV' + String(state.invoices.length + 1).padStart(3,'0'),
      studentId: children[0].id,  // primary child
      siblingIds: children.slice(1).map(function(c) { return c.id; }),
      parentEmail: email,
      description: desc,
      type: 'Monthly',
      amount: totalAmount,
      siblingDiscount: discount || undefined,
      dueDate: dueDate,
      status: 'Unpaid',
      createdOn: App.Utils.today(),
      paidOn: null
    };

    App.Store.set({ invoices: [...state.invoices, newInvoice] });
    App.Utils.hideModal();
    App.Utils.showToast('Sibling invoice created — RM ' + totalAmount.toFixed(2) + ' for ' + childNames, 'success');
    App.Router.refresh();
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
    _markUnpaid: _markUnpaid,
    _deleteInvoice: _deleteInvoice,
    _editModal: _editModal,
    _createModal: _createModal,
    checkLoginNotifications: checkLoginNotifications,
    _toggleSelectAllInv: _toggleSelectAllInv,
    _toggleSelectInv: _toggleSelectInv,
    _bulkDeselectInv: _bulkDeselectInv,
    _bulkMarkPaid: _bulkMarkPaid,
    _bulkConfirmPaid: _bulkConfirmPaid,
    _generateMonthlyModal: _generateMonthlyModal,
    _previewMonthly: _previewMonthly,
    _updateGenPreview: _updateGenPreview,
    _toggleEarlyBird: _toggleEarlyBird,
    _updateNetAmount: _updateNetAmount,
    _siblingInvoiceModal: _siblingInvoiceModal,
    _updateSiblingChildren: _updateSiblingChildren,
    _updateSiblingTotal: _updateSiblingTotal,
    _exportCSV: _exportCSV
  };
})();
