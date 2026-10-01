// Developer page: technical history for whoever maintains StudyHub (DEVELOPER_EMAILS).
// Admins run the centre and never see it; the server refuses everyone else too.
(function() {
  'use strict';
  window.App = window.App || {};

  var TABS = { audit: 'Audit log', failures: 'Failed emails', health: 'Health' };
  var _tab = 'audit';
  var _filter = { q: '', action: '', from: '', to: '' };

  function _when(iso) {
    var d = new Date(iso);
    return isNaN(d) ? '' : d.toLocaleString('en-MY', { dateStyle: 'medium', timeStyle: 'short' });
  }

  function _auditQuery(f) {
    var parts = ['q', 'action', 'from', 'to'].filter(function(k) { return f[k]; }).map(function(k) {
      return k + '=' + encodeURIComponent(f[k]);
    });
    return parts.length ? '?' + parts.join('&') : '';
  }

  var CELL = 'padding:0.5rem 0.6rem;border-bottom:1px solid #f1f5f9;vertical-align:top;font-size:0.78rem';

  function _auditRowsHtml(entries) {
    if (!entries.length) return '<p style="color:#94a3b8;font-size:0.82rem;padding:1rem 0">Nothing matches.</p>';
    return '<div style="overflow-x:auto"><table style="width:100%;border-collapse:collapse">'
      + '<thead><tr style="text-align:left;color:#94a3b8;font-size:0.68rem;text-transform:uppercase">'
      + '<th style="padding:0.4rem 0.6rem">When</th><th style="padding:0.4rem 0.6rem">Who</th><th style="padding:0.4rem 0.6rem">Action</th><th style="padding:0.4rem 0.6rem">On</th><th style="padding:0.4rem 0.6rem">Detail</th>'
      + '</tr></thead><tbody>'
      + entries.map(function(e) {
        return '<tr>'
          + '<td style="' + CELL + ';white-space:nowrap">' + App.Utils.esc(_when(e.createdAt)) + '</td>'
          + '<td style="' + CELL + '">' + App.Utils.esc(e.actorEmail) + '</td>'
          + '<td style="' + CELL + ';font-family:var(--mono,monospace)">' + App.Utils.esc(e.action) + '</td>'
          + '<td style="' + CELL + '">' + App.Utils.esc(e.entityType + ' ' + e.entityId) + '</td>'
          + '<td style="' + CELL + ';color:#64748b;word-break:break-word">' + App.Utils.esc(e.detail) + '</td>'
          + '</tr>';
      }).join('')
      + '</tbody></table></div>';
  }

  function _auditFormHtml(actions) {
    var opts = ['<option value="">All actions</option>'].concat(actions.map(function(a) {
      return '<option value="' + App.Utils.esc(a) + '"' + (a === _filter.action ? ' selected' : '') + '>' + App.Utils.esc(a) + '</option>';
    })).join('');
    return '<form id="dev-audit-form" style="display:flex;flex-wrap:wrap;gap:0.5rem;margin-bottom:1rem">'
      + '<input name="q" class="form-input" style="flex:1 1 12rem" placeholder="Email, id or detail" aria-label="Search" value="' + App.Utils.esc(_filter.q) + '">'
      + '<select name="action" class="form-input" style="flex:0 1 12rem" aria-label="Action">' + opts + '</select>'
      + '<input name="from" type="date" class="form-input" style="flex:0 1 9rem" aria-label="From" value="' + App.Utils.esc(_filter.from) + '">'
      + '<input name="to" type="date" class="form-input" style="flex:0 1 9rem" aria-label="To" value="' + App.Utils.esc(_filter.to) + '">'
      + '<button type="submit" style="padding:0.45rem 1rem;font-size:0.8rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:4px;cursor:pointer">Search</button>'
      + '</form>';
  }

  function _failuresHtml(data) {
    var emails = data.emails || [];
    var jobs = data.jobs || [];
    var emailRows = emails.length === 0 ? '<p style="color:#94a3b8;font-size:0.82rem">No failed emails.</p>'
      : emails.map(function(e) {
        return '<div style="padding:0.6rem 0;border-bottom:1px solid #f1f5f9;font-size:0.8rem">'
          + '<div style="font-weight:600;color:#111">' + App.Utils.esc(e.subject) + '</div>'
          + '<div style="color:#64748b">To ' + App.Utils.esc(e.to) + ' · ' + App.Utils.esc(_when(e.createdAt)) + ' · ' + e.attempts + ' tries</div>'
          + '<div style="color:#b91c1c;word-break:break-word">' + App.Utils.esc(e.lastError) + '</div>'
          + '</div>';
      }).join('');
    var jobRows = jobs.length === 0 ? '<p style="color:#94a3b8;font-size:0.82rem">No stuck jobs.</p>'
      : jobs.map(function(j) {
        return '<div style="padding:0.6rem 0;border-bottom:1px solid #f1f5f9;font-size:0.8rem">'
          + '<div style="font-weight:600;color:#111;font-family:var(--mono,monospace)">' + App.Utils.esc(j.topic) + '</div>'
          + '<div style="color:#64748b">' + App.Utils.esc(_when(j.createdAt)) + ' · ' + j.attempts + ' tries</div>'
          + '<div style="color:#b91c1c;word-break:break-word">' + App.Utils.esc(j.lastError) + '</div>'
          + '</div>';
      }).join('');
    return '<h3 style="font-size:0.85rem;font-weight:700;margin:0 0 0.4rem">Emails nothing will retry</h3>' + emailRows
      + '<h3 style="font-size:0.85rem;font-weight:700;margin:1.25rem 0 0.4rem">Jobs that keep failing <span style="font-weight:400;color:#94a3b8">(an email that was never queued)</span></h3>' + jobRows;
  }

  function _yesNo(on, yes, no) {
    return '<span style="font-weight:700;color:' + (on ? '#15803d' : '#b45309') + '">' + (on ? yes : no) + '</span>';
  }

  function _healthHtml(h) {
    var m = h.migrations || {};
    var q = h.emailQueue || {};
    var rows = [
      ['Version', App.Utils.esc(h.version) + ' · ' + App.Utils.esc(h.env)],
      ['Up for', Math.round((h.uptimeSec || 0) / 60) + ' min'],
      ['Database', _yesNo(h.dbOK, 'reachable', 'DOWN')],
      ['Migrations', m.count + ' applied, latest ' + App.Utils.esc(m.latest || '-') + (m.appliedAt ? ' on ' + App.Utils.esc(_when(m.appliedAt)) : '')],
      ['Background jobs', _yesNo(h.jobsEnabled, 'running', 'off')],
      ['Email', _yesNo(h.emailLive, 'sent for real', 'logged only, nothing leaves')],
      ['Email reaches', h.emailOnlyTo > 0 ? _yesNo(false, '', h.emailOnlyTo + ' allowed address(es) only') : _yesNo(true, 'everyone', '')],
      ['Email queue', q.pending + ' waiting · ' + q.sent24h + ' sent today · ' + q.failed + ' failed'],
      ['Online payments', _yesNo(h.onlinePayments, 'configured', 'not configured')],
      ['Runtime', App.Utils.esc(h.goVersion || '') + ' · ' + (h.goroutines || 0) + ' goroutines'],
      ['DB pool', h.dbPool ? h.dbPool.inUse + ' in use of ' + h.dbPool.open + ' open · ' + h.dbPool.waitCount + ' waits' : '-']
    ];
    return '<dl style="display:grid;grid-template-columns:minmax(8rem,auto) 1fr;gap:0.5rem 1rem;font-size:0.82rem;margin:0">'
      + rows.map(function(r) { return '<dt style="color:#64748b">' + r[0] + '</dt><dd style="margin:0;color:#111">' + r[1] + '</dd>'; }).join('')
      + '</dl>';
  }

  function _body() {
    return document.getElementById('dev-body');
  }

  function _loadAudit() {
    return App.Api.get('/api/dev/audit-logs' + _auditQuery(_filter)).then(function(res) {
      var body = _body();
      if (!body) return;
      body.innerHTML = _auditFormHtml(res.actions || []) + _auditRowsHtml(res.entries || []);
      document.getElementById('dev-audit-form').addEventListener('submit', function(e) {
        e.preventDefault();
        var fd = new FormData(e.target);
        _filter = { q: (fd.get('q') || '').trim(), action: fd.get('action') || '', from: fd.get('from') || '', to: fd.get('to') || '' };
        _loadAudit();
      });
    });
  }

  var LOADERS = {
    audit: _loadAudit,
    failures: function() {
      return App.Api.get('/api/dev/failures').then(function(res) { if (_body()) _body().innerHTML = _failuresHtml(res); });
    },
    health: function() {
      return App.Api.get('/api/dev/health').then(function(res) { if (_body()) _body().innerHTML = _healthHtml(res); });
    }
  };

  function _tabsHtml() {
    return Object.keys(TABS).map(function(key) {
      var on = key === _tab;
      return '<button type="button" onclick="App.Developer._setTab(\'' + key + '\')" aria-pressed="' + on + '" '
        + 'style="padding:0.45rem 0.9rem;font-size:0.8rem;font-weight:700;border:1px solid ' + (on ? '#111' : '#e2e8f0') + ';background:' + (on ? '#111' : '#fff') + ';color:' + (on ? '#fff' : '#374151') + ';border-radius:4px;cursor:pointer">'
        + TABS[key] + '</button>';
    }).join('');
  }

  function render(container) {
    container.innerHTML = '<div style="max-width:72rem">'
      + '<p style="font-size:0.8rem;color:#94a3b8;margin:0 0 1rem">Only you can open this page. Nothing here changes anything.</p>'
      + '<div style="display:flex;gap:0.5rem;flex-wrap:wrap;margin-bottom:1rem">' + _tabsHtml() + '</div>'
      + '<div id="dev-body" style="background:#fff;border:1px solid rgba(0,0,0,0.07);padding:1.25rem"><p style="color:#94a3b8;font-size:0.82rem">Loading...</p></div>'
      + '</div>';
    LOADERS[_tab]().catch(function() { /* App.Api already toasted, e.g. "developer only" */ });
  }

  function _setTab(key) {
    if (!TABS[key]) return;
    _tab = key;
    App.Router.refresh();
  }

  App.Developer = {
    render: render,
    _setTab: _setTab,
    _auditQuery: _auditQuery,
    _auditRowsHtml: _auditRowsHtml,
    _failuresHtml: _failuresHtml,
    _healthHtml: _healthHtml
  };
})();
