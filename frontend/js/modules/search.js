// Search from anywhere (admin): one box for students, parents, phone numbers,
// invoices and classes. Results come from the snapshot already in the store, so
// there is no request per keystroke and nothing new is exposed.
(function() {
  'use strict';
  window.App = window.App || {};

  var MIN_QUERY = 2;
  var MIN_PHONE_DIGITS = 4;
  var PER_KIND = 6;

  function _norm(v) {
    return String(v == null ? '' : v).toLowerCase();
  }

  function _digits(v) {
    return String(v == null ? '' : v).replace(/\D/g, '');
  }

  function _studentHit(s, q, qDigits) {
    var hay = [s.firstName + ' ' + s.lastName, s.id, s.studentNo, s.contact, s.parentName].map(_norm).join(' | ');
    if (hay.indexOf(q) > -1) return true;
    return qDigits.length >= MIN_PHONE_DIGITS && _digits(s.phone).indexOf(qDigits) > -1;
  }

  // find is pure: the same state and query always give the same ranked results.
  function find(state, query) {
    var q = _norm(query).trim();
    if (q.length < MIN_QUERY) return [];
    var qDigits = _digits(q);
    var nameOf = {};
    (state.students || []).forEach(function(s) { nameOf[s.id] = s.firstName + ' ' + s.lastName; });

    var students = (state.students || []).filter(function(s) { return _studentHit(s, q, qDigits); })
      .slice(0, PER_KIND).map(function(s) {
        return { kind: 'student', id: s.id, title: nameOf[s.id], sub: [s.parentName, s.contact, s.phone].filter(Boolean).join(' · ') };
      });
    var invoices = (state.invoices || []).filter(function(i) {
      return _norm(i.invoiceNo).indexOf(q) > -1 || _norm(i.id).indexOf(q) > -1;
    }).slice(0, PER_KIND).map(function(i) {
      return { kind: 'invoice', id: i.id, title: i.invoiceNo || i.id, sub: [nameOf[i.studentId], App.Utils.formatCurrency(i.amount), i.status].filter(Boolean).join(' · ') };
    });
    var classes = (state.classes || []).filter(function(c) { return _norm(c.name).indexOf(q) > -1; })
      .slice(0, PER_KIND).map(function(c) {
        return { kind: 'class', id: c.id, title: c.name, sub: [c.day, App.Utils.formatTime(c.time)].filter(Boolean).join(' · ') };
      });
    return students.concat(invoices, classes);
  }

  var OPENERS = {
    student: function(id) { App.Students._viewModal(id); },
    invoice: function(id) { App.Billing._viewInvoiceModal(id); },
    class:   function(id) { App.Calendar._classModal(id); }
  };
  var KIND_LABEL = { student: 'Student', invoice: 'Invoice', class: 'Class' };

  function _open(kind, id) {
    if (OPENERS[kind]) OPENERS[kind](id);
  }

  function _resultsHtml(results, query) {
    if (_norm(query).trim().length < MIN_QUERY) {
      return '<p style="font-size:0.8rem;color:#94a3b8;margin:0.75rem 0 0">Type a name, phone, email, invoice number or class.</p>';
    }
    if (results.length === 0) return '<p style="font-size:0.8rem;color:#94a3b8;margin:0.75rem 0 0">Nothing matches.</p>';
    return results.map(function(r) {
      return '<button type="button" class="sh-search-hit" onclick="App.Search._open(\'' + r.kind + '\',\'' + r.id + '\')" '
        + 'style="display:flex;width:100%;text-align:left;gap:0.75rem;align-items:baseline;padding:0.55rem 0.4rem;border:none;border-bottom:1px solid #f1f5f9;background:none;cursor:pointer">'
        + '<span style="font-size:0.65rem;font-weight:700;color:#94a3b8;text-transform:uppercase;min-width:3.8rem">' + KIND_LABEL[r.kind] + '</span>'
        + '<span style="min-width:0"><span style="display:block;font-size:0.86rem;font-weight:600;color:#111">' + App.Utils.esc(r.title) + '</span>'
        + '<span style="display:block;font-size:0.74rem;color:#64748b;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">' + App.Utils.esc(r.sub) + '</span></span>'
        + '</button>';
    }).join('');
  }

  function _update(query) {
    var box = document.getElementById('global-search-results');
    if (box) box.innerHTML = _resultsHtml(find(App.Store.get(), query), query);
  }

  // Enter opens the first result, the usual shortcut in a search box.
  function _onKey(event) {
    if (event.key !== 'Enter') return;
    event.preventDefault();
    var first = find(App.Store.get(), event.target.value)[0];
    if (first) _open(first.kind, first.id);
  }

  function open() {
    App.Utils.showModal('<div class="p-6" style="width:min(520px,92vw)">'
      + '<h2 class="text-lg font-bold mb-3">Search</h2>'
      + '<input id="global-search-input" type="search" class="form-input" autocomplete="off" aria-label="Search students, invoices and classes" '
      +   'placeholder="Name, phone, email, invoice number or class" oninput="App.Search._update(this.value)" onkeydown="App.Search._onKey(event)">'
      + '<div id="global-search-results" role="list">' + _resultsHtml([], '') + '</div>'
      + '</div>');
    setTimeout(function() {
      var input = document.getElementById('global-search-input');
      if (input) input.focus();
    }, 0);
  }

  // "/" opens search from anywhere an admin is not already typing.
  function _isTyping(target) {
    var tag = target && target.tagName;
    return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || (target && target.isContentEditable);
  }

  document.addEventListener('keydown', function(e) {
    if (e.key !== '/' || e.metaKey || e.ctrlKey || e.altKey || _isTyping(e.target)) return;
    if (App.currentRole !== 'admin') return;
    e.preventDefault();
    open();
  });

  App.Search = { find: find, open: open, _open: _open, _update: _update, _onKey: _onKey };
})();
