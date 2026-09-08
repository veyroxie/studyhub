(function() {
  'use strict';

  // The pricing catalogue editor (migrations 0051 / 0054, ADR-004).
  //
  // Before this screen the catalogue held the centre's real prices and had no
  // UI at all, so changing one meant a migration and a deploy. That is the
  // thing the whole pricing rework exists to end, which is why it lands before
  // the billing switchover rather than after it.
  //
  // It reads pricingCategories and pricingPlans from the snapshot and writes
  // through /api/pricing-categories and /api/pricing-plans. The server owns
  // both rules -- a tier must be priced, a category in use cannot be deleted --
  // so this file never decides whether a save is legal, it only reports what
  // the server said.

  var _openCat = null;   // category id whose tiers are expanded
  var _tab = 'catalogue';
  var _preview = null;   // last price-preview response
  var _pvMonth = '';

  function _money(n) { return 'RM' + (Number(n) || 0).toFixed(2); }

  function _freqLabel(n) {
    if (n === 1) return 'Once a week';
    if (n === 2) return 'Twice a week';
    return n + 'x a week';
  }

  // A tier prices EITHER a monthly subscription or an hourly overflow. Showing
  // both columns always would print RM0.00 next to every real price, which is
  // the value this whole change exists to stop treating as meaningful.
  function _priceLabel(p) {
    if (p.monthlyFee > 0 && p.hourlyRate > 0) return _money(p.monthlyFee) + '/mo · ' + _money(p.hourlyRate) + '/hr';
    if (p.hourlyRate > 0) return _money(p.hourlyRate) + ' per hour';
    return _money(p.monthlyFee) + ' per month';
  }

  function _classesUsing(categoryId) {
    return (App.Store.get().classes || []).filter(function(c) {
      return c.pricingCategoryId === categoryId;
    });
  }

  function _tiersNeeded() {
    var state = App.Store.get();
    var plans = state.pricingPlans || [];
    var known = {};
    plans.forEach(function(p) { known[p.categoryId + '|' + p.tierName] = true; });
    return (state.classes || []).filter(function(c) {
      if (!c.pricingCategoryId) return true;
      if (c.monthlyFeeOverride > 0 || c.sessionRate > 0) return false;
      var cat = (state.pricingCategories || []).find(function(x) { return x.id === c.pricingCategoryId; });
      if (cat && cat.creditCovered) return false;
      if (!c.defaultTierName) return true;
      return !known[c.pricingCategoryId + '|' + c.defaultTierName];
    });
  }

  function _thisMonth() {
    var d = new Date();
    return d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0');
  }

  function _tabBar() {
    var btn = function(id, label) {
      var on = _tab === id;
      return '<button onclick="App.Pricing._setTab(\'' + id + '\')" style="padding:0.35rem 0.9rem;font-size:0.78rem;font-weight:600;border:none;border-radius:6px;cursor:pointer;background:'
        + (on ? 'var(--gold)' : 'transparent') + ';color:' + (on ? '#0a0a0a' : '#94a3b8') + '">' + label + '</button>';
    };
    return '<div style="display:inline-flex;gap:0.15rem;background:#f1f5f9;border-radius:8px;padding:0.2rem">'
      + btn('catalogue', 'Catalogue') + btn('check', 'Invoice check') + '</div>';
  }

  function _setTab(t) {
    _tab = t;
    App.Router.refresh();
    if (t === 'check' && !_preview) _loadPreview(_pvMonth || _thisMonth());
  }

  // The differ. It compares COMPUTED prices against what was invoiced, not
  // whether an invoice exists -- 60 of 70 students have monthly invoicing off,
  // so an existence check would look clean while verifying nothing.
  async function _loadPreview(month) {
    _pvMonth = month;
    try {
      _preview = await App.Api.get('/api/billing/price-preview?month=' + encodeURIComponent(month));
    } catch (err) {
      _preview = null;
    }
    App.Router.refresh();
  }

  function _checkPanel() {
    var month = _pvMonth || _thisMonth();
    var head = '<div style="display:flex;align-items:center;gap:0.6rem;flex-wrap:wrap;margin-bottom:0.9rem">'
      + '<label style="font-size:0.8rem;color:#64748b">Month</label>'
      + '<input type="month" value="' + month + '" onchange="App.Pricing._loadPreview(this.value)" style="padding:0.4rem 0.6rem;font-size:0.85rem;border:1px solid #e2e8f0;border-radius:8px">'
      + '</div>';

    if (!_preview) return head + '<p style="font-size:0.85rem;color:#94a3b8">Loading…</p>';

    var p = _preview;
    var tile = function(label, n, colour) {
      return '<div style="flex:1;min-width:96px;background:#fff;border:1px solid #e7e0d2;border-radius:10px;padding:0.6rem 0.75rem">'
        + '<div style="font-size:0.65rem;font-weight:700;text-transform:uppercase;letter-spacing:0.05em;color:#94a3b8">' + label + '</div>'
        + '<div style="font-size:1.15rem;font-weight:700;color:' + colour + ';font-variant-numeric:tabular-nums">' + n + '</div></div>';
    };

    var rows = (p.students || []).map(function(st) {
      var flag, colour;
      if (st.unpriceable)      { flag = 'Cannot price'; colour = '#92400e'; }
      else if (!st.hasInvoice) { flag = 'No invoice';   colour = '#64748b'; }
      else if (st.difference === 0) { flag = 'Matches'; colour = '#15803d'; }
      else { flag = (st.difference > 0 ? '+' : '') + _money(st.difference); colour = '#9c3b23'; }

      var why = (st.lines || []).map(function(l) {
        var bits = [l.className || l.categoryName];
        if (l.tierName) bits.push(l.tierName);
        if (l.sessionsPerWeek > 1) bits.push(l.sessionsPerWeek + 'x a week');
        var txt = App.Utils.esc(bits.join(' · '));
        if (l.problem) return '<div style="color:#92400e">' + txt + ' — ' + App.Utils.esc(l.problem) + '</div>';
        if (l.source === 'credit-covered') return '<div style="color:#64748b">' + txt + ' — covered by credits</div>';
        return '<div style="color:#64748b">' + txt + ' — ' + _money(l.amount) + ' (' + App.Utils.esc(l.source) + ')</div>';
      }).join('');

      return '<tr style="border-bottom:1px solid #f1f5f9">'
        + '<td style="padding:0.55rem 0.5rem 0.55rem 0"><div style="font-weight:600;color:#111">' + App.Utils.esc(st.studentName) + '</div>'
        +   '<div style="font-size:0.72rem;margin-top:0.15rem">' + why + '</div></td>'
        + '<td style="padding:0.55rem 0.5rem;text-align:right;font-variant-numeric:tabular-nums">' + (st.hasInvoice ? _money(st.invoiced) : '—') + '</td>'
        + '<td style="padding:0.55rem 0.5rem;text-align:right;font-variant-numeric:tabular-nums">' + (st.unpriceable ? '—' : _money(st.computed)) + '</td>'
        + '<td style="padding:0.55rem 0 0.55rem 0.5rem;text-align:right;font-weight:700;color:' + colour + ';white-space:nowrap">' + flag + '</td>'
        + '</tr>';
    }).join('');

    return head
      + '<div style="display:flex;gap:0.6rem;flex-wrap:wrap;margin-bottom:1rem">'
      +   tile('Matches', p.matching, '#15803d')
      +   tile('Different', p.differing, '#9c3b23')
      +   tile('Cannot price', p.unpriceable, '#92400e')
      +   tile('No invoice', p.notInvoiced, '#64748b')
      + '</div>'
      + '<p style="font-size:0.76rem;color:#94a3b8;margin:0 0 0.6rem">Nothing here changes an invoice. It shows what the catalogue would charge beside what was actually billed.</p>'
      + (rows
        ? '<div style="overflow-x:auto"><table style="width:100%;border-collapse:collapse;font-size:0.85rem">'
          + '<thead><tr style="border-bottom:1px solid #d6cdb9">'
          + '<th style="text-align:left;padding:0 0.5rem 0.5rem 0;font-size:0.68rem;text-transform:uppercase;letter-spacing:0.06em;color:#94a3b8;font-weight:600">Student</th>'
          + '<th style="text-align:right;padding:0 0.5rem 0.5rem;font-size:0.68rem;text-transform:uppercase;letter-spacing:0.06em;color:#94a3b8;font-weight:600">Invoiced</th>'
          + '<th style="text-align:right;padding:0 0.5rem 0.5rem;font-size:0.68rem;text-transform:uppercase;letter-spacing:0.06em;color:#94a3b8;font-weight:600">Catalogue</th>'
          + '<th style="text-align:right;padding:0 0 0.5rem 0.5rem;font-size:0.68rem;text-transform:uppercase;letter-spacing:0.06em;color:#94a3b8;font-weight:600">Result</th>'
          + '</tr></thead><tbody>' + rows + '</tbody></table></div>'
        : '<p style="font-size:0.85rem;color:#94a3b8">No students to compare for this month.</p>');
  }

  function render(container) {
    var state = App.Store.get();
    var cats  = state.pricingCategories || [];
    var plans = state.pricingPlans || [];
    var isAdmin = App.currentRole === 'admin';
    var needing = _tiersNeeded();

    var rows = cats.map(function(cat) {
      var mine = plans.filter(function(p) { return p.categoryId === cat.id; });
      var used = _classesUsing(cat.id).length;
      var open = _openCat === cat.id;
      return ''
        + '<div style="border:1px solid #e7e0d2;border-radius:12px;background:#fff;overflow:hidden">'
        +   '<div style="display:flex;align-items:center;gap:0.75rem;padding:0.9rem 1rem;cursor:pointer" onclick="App.Pricing._toggle(\'' + cat.id + '\')">'
        +     '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#94a3b8" stroke-width="2.5" style="transform:rotate(' + (open ? '90' : '0') + 'deg);transition:transform 0.15s"><path d="M9 18l6-6-6-6"/></svg>'
        +     '<div style="flex:1;min-width:0">'
        +       '<div style="font-weight:700;color:#111">' + App.Utils.esc(cat.name)
        +         (cat.creditCovered ? ' <span style="font-size:0.65rem;font-weight:700;text-transform:uppercase;letter-spacing:0.05em;color:#1e40af;background:#dbeafe;border:1px solid #bfdbfe;border-radius:999px;padding:0.1rem 0.45rem;vertical-align:middle">Credit-covered</span>' : '')
        +       '</div>'
        +       '<div style="font-size:0.75rem;color:#94a3b8">' + mine.length + ' tier' + (mine.length === 1 ? '' : 's') + ' · ' + used + ' class' + (used === 1 ? '' : 'es') + '</div>'
        +     '</div>'
        +     (isAdmin ? '<button onclick="event.stopPropagation();App.Pricing._editCategory(\'' + cat.id + '\')" style="padding:0.3rem 0.7rem;font-size:0.72rem;font-weight:600;background:#f1f5f9;color:#475569;border:1px solid #e2e8f0;border-radius:7px;cursor:pointer">Rename</button>' : '')
        +   '</div>'
        + (open
          ? '<div style="border-top:1px solid #f0ede8;padding:0.5rem 1rem 1rem">'
            + (mine.length === 0
              ? '<p style="font-size:0.8rem;color:#94a3b8;padding:0.6rem 0">No tiers yet.' + (cat.creditCovered ? ' A credit-covered category only needs one if it charges for overflow.' : '') + '</p>'
              : '<div style="display:flex;flex-direction:column;gap:0.3rem;padding-top:0.5rem">'
                + mine.map(function(p) {
                    return '<div style="display:flex;align-items:center;gap:0.75rem;padding:0.5rem 0.65rem;background:#faf9f7;border-radius:8px">'
                      + '<div style="flex:1;min-width:0"><div style="font-size:0.86rem;font-weight:600;color:#111">' + App.Utils.esc(p.tierName) + '</div>'
                      +   '<div style="font-size:0.72rem;color:#94a3b8">' + _freqLabel(p.sessionsPerWeek) + '</div></div>'
                      + '<div style="font-family:var(--mono,monospace);font-size:0.84rem;font-weight:600;color:#111;font-variant-numeric:tabular-nums">' + _priceLabel(p) + '</div>'
                      + (isAdmin ? '<button onclick="App.Pricing._editPlan(\'' + p.id + '\')" style="padding:0.25rem 0.6rem;font-size:0.7rem;font-weight:600;background:#fff;color:#475569;border:1px solid #e2e8f0;border-radius:6px;cursor:pointer">Edit</button>' : '')
                      + '</div>';
                  }).join('')
                + '</div>')
            + (isAdmin ? '<button onclick="App.Pricing._addPlan(\'' + cat.id + '\')" style="margin-top:0.7rem;padding:0.35rem 0.8rem;font-size:0.75rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:7px;cursor:pointer">+ Add tier</button>' : '')
            + '</div>'
          : '')
        + '</div>';
    }).join('');

    container.innerHTML = ''
      + '<div style="display:flex;flex-direction:column;gap:1rem;max-width:820px">'
      + '<div style="display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:0.5rem">'
      +   '<div><h1 style="font-size:1.4rem;font-weight:800;color:#0d0d0d;letter-spacing:-0.03em;margin:0">Pricing</h1>'
      +   '<p style="font-size:0.82rem;color:#64748b;margin:2px 0 0">A category owns named tiers. A tier is one price.</p></div>'
      +   (isAdmin && _tab === 'catalogue' ? '<button onclick="App.Pricing._addCategory()" style="padding:0.45rem 0.95rem;font-size:0.8rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">+ Add category</button>' : '')
      + '</div>'
      + _tabBar()
      + (_tab === 'check' ? _checkPanel() : ''
      )
      // The backlog is the point of the catalogue tab, so it sits above the
      // list rather than being something you have to go looking for.
      + (_tab !== 'catalogue' ? '' : (needing.length > 0
        ? '<div style="background:#fffbeb;border:1px solid #fef3c7;border-radius:12px;padding:0.9rem 1rem">'
          + '<div style="font-size:0.8rem;font-weight:700;color:#92400e;margin-bottom:0.4rem">' + needing.length + ' class' + (needing.length === 1 ? '' : 'es') + ' cannot be priced yet</div>'
          + '<div style="display:flex;flex-wrap:wrap;gap:0.35rem">'
          + needing.map(function(c) {
              return '<span style="font-size:0.74rem;background:#fff;border:1px solid #fde68a;border-radius:999px;padding:0.15rem 0.6rem;color:#92400e">'
                + App.Utils.esc(c.name) + (c.day ? ' · ' + c.day : '') + '</span>';
            }).join('')
          + '</div>'
          + '<p style="font-size:0.72rem;color:#b45309;margin:0.55rem 0 0">Each needs a tier in its category, or its own fixed price on the class.</p>'
          + '</div>'
        : '<div style="background:#f0fdf4;border:1px solid #bbf7d0;border-radius:12px;padding:0.8rem 1rem;font-size:0.82rem;color:#166534;font-weight:600">Every class can be priced.</div>')
        + '<div style="display:flex;flex-direction:column;gap:0.6rem">' + (rows || '<p style="font-size:0.85rem;color:#94a3b8">No categories yet.</p>') + '</div>')
      + '</div>';
  }

  function _toggle(catId) {
    _openCat = (_openCat === catId) ? null : catId;
    App.Router.refresh();
  }

  function _field(label, inner, hint) {
    return '<div><label class="block text-sm font-medium text-slate-700 mb-1">' + label + '</label>' + inner
      + (hint ? '<p class="text-xs text-slate-400 mt-1">' + hint + '</p>' : '') + '</div>';
  }

  function _addCategory() { _categoryModal(null); }
  function _editCategory(catId) { _categoryModal(catId); }

  function _categoryModal(catId) {
    var cat = catId ? (App.Store.get().pricingCategories || []).find(function(c) { return c.id === catId; }) : null;
    var used = catId ? _classesUsing(catId).length : 0;
    App.Utils.showModal(
      '<div class="p-6" style="min-width:340px;max-width:440px">'
      + '<h2 class="text-lg font-bold mb-1">' + (cat ? 'Edit category' : 'New category') + '</h2>'
      + '<p class="text-sm text-slate-500 mb-4">A category groups tiers that share a shape, like Group, Private or Mandarin.</p>'
      + '<form id="cat-form" class="space-y-4">'
      + _field('Name', '<input name="name" class="form-input" value="' + App.Utils.esc(cat ? cat.name : '') + '" required maxlength="60">')
      + _field('Billing',
          '<label style="display:flex;gap:0.5rem;align-items:flex-start;font-size:0.85rem;color:#374151"><input type="checkbox" name="creditCovered" style="margin-top:0.2rem"' + (cat && cat.creditCovered ? ' checked' : '') + '>'
          + '<span>Covered by credits. Nothing is billed monthly; only overflow beyond the included allowance is charged.</span></label>')
      + '<div class="flex justify-between items-center gap-3 pt-2">'
      +   (cat
          ? '<button type="button" onclick="App.Pricing._deleteCategory(\'' + cat.id + '\')" style="padding:0.45rem 0.9rem;font-size:0.78rem;font-weight:600;background:#fff;color:' + (used > 0 ? '#cbd5e1' : '#dc2626') + ';border:1px solid ' + (used > 0 ? '#e2e8f0' : '#fecaca') + ';border-radius:8px;cursor:' + (used > 0 ? 'not-allowed' : 'pointer') + '"' + (used > 0 ? ' disabled title="' + used + ' class(es) still use this"' : '') + '>Delete</button>'
          : '<span></span>')
      +   '<div class="flex gap-2">'
      +     '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      +     '<button type="submit" style="padding:0.45rem 1rem;font-size:0.84rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Save</button>'
      +   '</div>'
      + '</div></form></div>'
    );
    document.getElementById('cat-form').addEventListener('submit', async function(e) {
      e.preventDefault();
      var fd = new FormData(e.target);
      var payload = { name: fd.get('name'), creditCovered: !!fd.get('creditCovered'), sortOrder: cat ? cat.sortOrder : 100 };
      try {
        if (cat) await App.Api.put('/api/pricing-categories/' + cat.id, payload);
        else await App.Api.post('/api/pricing-categories', payload);
        App.Utils.hideModal(true);
        await App.Api.loadSnapshot();
        App.Utils.showToast(cat ? 'Category updated' : 'Category added', 'success');
        App.Router.refresh();
      } catch (err) { /* auto-toasted */ }
    });
  }

  async function _deleteCategory(catId) {
    var ok = await App.Utils.showConfirm({
      title: 'Delete category',
      message: 'Its tiers go with it. Classes using it would have no price, so this is refused while any still point at it.',
      confirmLabel: 'Delete', danger: true
    });
    if (!ok) return;
    try {
      await App.Api.del('/api/pricing-categories/' + catId);
      App.Utils.hideModal(true);
      await App.Api.loadSnapshot();
      App.Utils.showToast('Category deleted', 'info');
      App.Router.refresh();
    } catch (err) { /* auto-toasted */ }
  }

  function _addPlan(catId) { _planModal(catId, null); }
  function _editPlan(planId) {
    var p = (App.Store.get().pricingPlans || []).find(function(x) { return x.id === planId; });
    if (p) _planModal(p.categoryId, p);
  }

  function _planModal(catId, plan) {
    var cat = (App.Store.get().pricingCategories || []).find(function(c) { return c.id === catId; }) || { name: '' };
    var freqOpts = [1, 2, 3, 4, 5].map(function(n) {
      return '<option value="' + n + '"' + (plan && plan.sessionsPerWeek === n ? ' selected' : '') + '>' + _freqLabel(n) + '</option>';
    }).join('');
    App.Utils.showModal(
      '<div class="p-6" style="min-width:340px;max-width:460px">'
      + '<h2 class="text-lg font-bold mb-1">' + (plan ? 'Edit tier' : 'New tier') + '</h2>'
      + '<p class="text-sm text-slate-500 mb-4">' + App.Utils.esc(cat.name) + '</p>'
      + '<form id="plan-form" class="space-y-4">'
      + _field('Tier name', '<input name="tierName" class="form-input" value="' + App.Utils.esc(plan ? plan.tierName : '') + '" required maxlength="60" placeholder="e.g. Level 1-2">',
          'What the parent sees on the invoice line.')
      + _field('How often', '<select name="sessionsPerWeek" class="form-input">' + freqOpts + '</select>',
          'Twice a week is its own price, not a multiple of the weekly one.')
      + '<div class="grid grid-cols-2 gap-4">'
      +   _field('Monthly fee', '<input name="monthlyFee" type="number" min="0" step="0.01" class="form-input" value="' + (plan ? plan.monthlyFee : 0) + '">')
      +   _field('Hourly rate', '<input name="hourlyRate" type="number" min="0" step="0.01" class="form-input" value="' + (plan ? plan.hourlyRate : 0) + '">')
      + '</div>'
      + '<p class="text-xs text-slate-400" style="margin-top:-0.4rem">Set whichever applies. A tier with neither cannot price anything and will be refused.</p>'
      + '<div class="flex justify-between items-center gap-3 pt-2">'
      +   (plan ? '<button type="button" onclick="App.Pricing._deletePlan(\'' + plan.id + '\')" style="padding:0.45rem 0.9rem;font-size:0.78rem;font-weight:600;background:#fff;color:#dc2626;border:1px solid #fecaca;border-radius:8px;cursor:pointer">Delete</button>' : '<span></span>')
      +   '<div class="flex gap-2">'
      +     '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      +     '<button type="submit" style="padding:0.45rem 1rem;font-size:0.84rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Save</button>'
      +   '</div>'
      + '</div></form></div>'
    );
    document.getElementById('plan-form').addEventListener('submit', async function(e) {
      e.preventDefault();
      var fd = new FormData(e.target);
      var payload = {
        categoryId: catId,
        tierName: fd.get('tierName'),
        sessionsPerWeek: parseInt(fd.get('sessionsPerWeek'), 10) || 1,
        monthlyFee: parseFloat(fd.get('monthlyFee')) || 0,
        hourlyRate: parseFloat(fd.get('hourlyRate')) || 0,
        sortOrder: plan ? plan.sortOrder : 100
      };
      try {
        if (plan) await App.Api.put('/api/pricing-plans/' + plan.id, payload);
        else await App.Api.post('/api/pricing-plans', payload);
        App.Utils.hideModal(true);
        await App.Api.loadSnapshot();
        App.Utils.showToast(plan ? 'Tier updated' : 'Tier added', 'success');
        App.Router.refresh();
      } catch (err) { /* auto-toasted */ }
    });
  }

  async function _deletePlan(planId) {
    var ok = await App.Utils.showConfirm({ title: 'Delete tier', confirmLabel: 'Delete', danger: true });
    if (!ok) return;
    try {
      await App.Api.del('/api/pricing-plans/' + planId);
      App.Utils.hideModal(true);
      await App.Api.loadSnapshot();
      App.Utils.showToast('Tier deleted', 'info');
      App.Router.refresh();
    } catch (err) { /* auto-toasted */ }
  }

  App.Pricing = {
    render: render,
    _setTab: _setTab,
    _loadPreview: _loadPreview,
    _toggle: _toggle,
    _addCategory: _addCategory,
    _editCategory: _editCategory,
    _deleteCategory: _deleteCategory,
    _addPlan: _addPlan,
    _editPlan: _editPlan,
    _deletePlan: _deletePlan
  };
  App.Router.register('pricing', App.Pricing);
})();
