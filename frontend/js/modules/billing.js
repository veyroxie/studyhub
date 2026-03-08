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
      +   (isAdmin ? '<button onclick="App.Billing._createModal()" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">+ Create Invoice</button>' : '')
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
      +       (filtered.length === 0 ? '<tr><td colspan="' + colCount + '" class="px-6 py-12 text-center text-slate-400 text-sm">No invoices found</td></tr>' : '')
      +       filtered.map(function(inv) {
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
            }).join('')
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
      + _field('Amount (RM)', '<input name="amount" type="number" min="0" step="0.01" class="form-input" required>')
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
      const newInvoice = {
        id: 'INV' + String(state.invoices.length + 1).padStart(3,'0'),
        studentId: fd.get('studentId'),
        description: fd.get('description'),
        type: fd.get('type'),
        amount: parseFloat(fd.get('amount')),
        dueDate: fd.get('dueDate'),
        status: 'Unpaid',
        createdOn: App.Utils.today(),
        paidOn: null
      };
      App.Store.set({ invoices: [...state.invoices, newInvoice] });
      App.Utils.hideModal();
      App.Utils.showToast('Invoice created', 'success');
      App.Router.refresh();
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
    _bulkConfirmPaid: _bulkConfirmPaid
  };
})();
