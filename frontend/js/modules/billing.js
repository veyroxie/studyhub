(function() {
  window.App = window.App || {};

  // Default to Unpaid — paid invoices are in the Archive tab
  let _filter = 'Unpaid';
  let _menuListenerAdded = false;

  function render(container) {
    const { invoices, students } = App.Store.get();
    const isAdmin = App.currentRole === 'admin';
    const isClient = App.currentRole === 'client';

    let displayInvoices = invoices;
    if (isClient && App.clientParent) {
      const myStudentIds = students.filter(function(s) { return s.contact === App.clientParent; }).map(function(s) { return s.id; });
      displayInvoices = invoices.filter(function(inv) { return myStudentIds.indexOf(inv.studentId) > -1; });
    }

    // Archive = Paid; All = everything
    const filtered = _filter === 'All'     ? displayInvoices
      : _filter === 'Archive' ? displayInvoices.filter(function(i) { return i.status === 'Paid'; })
      : displayInvoices.filter(function(i) { return i.status === _filter; });

    const totalRevenue = displayInvoices.reduce(function(s, i) { return s + i.amount; }, 0);
    const collected    = displayInvoices.filter(function(i) { return i.status === 'Paid'; }).reduce(function(s, i) { return s + i.amount; }, 0);
    const pending      = displayInvoices.filter(function(i) { return i.status === 'Unpaid'; }).reduce(function(s, i) { return s + i.amount; }, 0);
    const overdue      = displayInvoices.filter(function(i) { return i.status === 'Overdue'; }).reduce(function(s, i) { return s + i.amount; }, 0);

    container.innerHTML = ''
      + '<div class="flex items-center justify-between mb-6">'
      +   '<h1 class="text-2xl font-bold text-slate-800">Billing</h1>'
      +   (isAdmin ? '<button onclick="App.Billing._createModal()" class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700">+ Create Invoice</button>' : '')
      + '</div>'

      + '<div class="grid grid-cols-4 gap-4 mb-6">'
      + _statCard('Total Revenue', App.Utils.formatCurrency(totalRevenue), 'text-slate-700', 'bg-slate-50')
      + _statCard('Collected', App.Utils.formatCurrency(collected), 'text-emerald-600', 'bg-emerald-50')
      + _statCard('Pending', App.Utils.formatCurrency(pending), 'text-amber-600', 'bg-amber-50')
      + _statCard('Overdue', App.Utils.formatCurrency(overdue), 'text-red-600', 'bg-red-50')
      + '</div>'

      + '<div class="bg-white rounded-xl border border-slate-100 shadow-sm">'
      +   '<div class="p-4 border-b border-slate-100 flex items-center gap-2 flex-wrap">'
      +   ['Unpaid','Overdue','All','Archive'].map(function(f) {
            const active = f === _filter;
            const isArchive = f === 'Archive';
            return '<button onclick="App.Billing._setFilter(\'' + f + '\')" class="px-3 py-1.5 text-sm rounded-lg font-medium transition-colors '
              + (active
                ? (isArchive ? 'bg-slate-600 text-white' : 'bg-blue-600 text-white')
                : 'text-slate-600 hover:bg-slate-100')
              + '">' + (isArchive ? 'Archive (Paid)' : f) + '</button>';
          }).join('')
      +   '</div>'
      +   '<div class="overflow-x-auto">'
      +     '<table class="w-full">'
      +       '<thead class="bg-slate-50 border-b border-slate-100"><tr>'
      +         '<th class="th">Student</th><th class="th">Description</th><th class="th">Type</th>'
      +         '<th class="th">Due Date</th><th class="th text-right">Amount</th><th class="th">Status</th>'
      +         (isAdmin ? '<th class="th w-10"></th>' : '')
      +       '</tr></thead>'
      +       '<tbody class="divide-y divide-slate-50">'
      +       (filtered.length === 0 ? '<tr><td colspan="7" class="px-6 py-12 text-center text-slate-400 text-sm">No invoices found</td></tr>' : '')
      +       filtered.map(function(inv) {
              const stu = students.find(function(s) { return s.id === inv.studentId; });
              const stuName = stu ? stu.firstName + ' ' + stu.lastName : inv.studentId;
              return '<tr class="hover:bg-slate-50 transition-colors">'
                + '<td class="td"><div class="font-medium text-slate-800">' + stuName + '</div><div class="text-xs text-slate-400">' + inv.id + '</div></td>'
                + '<td class="td text-sm text-slate-600">' + inv.description + '</td>'
                + '<td class="td">' + App.Utils.badge(inv.type, inv.type === 'Monthly' ? 'blue' : 'purple') + '</td>'
                + '<td class="td text-sm text-slate-600">' + App.Utils.formatDate(inv.dueDate) + '</td>'
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

    // Attach global click-away handler once
    if (!_menuListenerAdded) {
      document.addEventListener('click', function() {
        document.querySelectorAll('.inv-menu').forEach(function(m) { m.classList.add('hidden'); });
      });
      _menuListenerAdded = true;
    }
  }

  function _statCard(label, value, textClass, bgClass) {
    return '<div class="' + bgClass + ' rounded-xl border border-slate-100 shadow-sm p-4">'
      + '<div class="text-xl font-bold ' + textClass + '">' + value + '</div>'
      + '<div class="text-xs text-slate-500 mt-1">' + label + '</div>'
      + '</div>';
  }

  function _setFilter(f) { _filter = f; App.Router.refresh(); }

  function _toggleMenu(event, id) {
    event.stopPropagation();
    document.querySelectorAll('.inv-menu').forEach(function(m) { m.classList.add('hidden'); });
    const menu = document.getElementById('inv-menu-' + id);
    if (menu) menu.classList.remove('hidden');
  }

  function _markPaid(invoiceId) {
    const state = App.Store.get();
    App.Store.set({ invoices: state.invoices.map(function(inv) {
      return inv.id === invoiceId ? Object.assign({}, inv, { status: 'Paid', paidOn: App.Utils.today() }) : inv;
    })});
    App.Utils.showToast('Invoice marked as paid', 'success');
    App.Router.refresh();
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
      + students.map(function(s) { return '<option value="' + s.id + '">' + s.firstName + ' ' + s.lastName + '</option>'; }).join('')
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

  App.Billing = { render: render, _setFilter: _setFilter, _toggleMenu: _toggleMenu, _markPaid: _markPaid, _markUnpaid: _markUnpaid, _deleteInvoice: _deleteInvoice, _editModal: _editModal, _createModal: _createModal };
})();
