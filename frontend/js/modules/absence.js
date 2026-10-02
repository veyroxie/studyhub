// Absence reports: a parent tells the centre a child will miss a session. Told 3+
// hours before, an admin or the class's teacher approves the make-up credit; told
// later, it is recorded without one. The server decides both, from its own clock.
(function() {
  'use strict';
  window.App = window.App || {};

  var STATUS = {
    pending:  { label: 'Waiting for approval', color: '#b45309' },
    approved: { label: 'Approved, make-up credit given', color: '#15803d' },
    declined: { label: 'No make-up credit', color: '#b91c1c' },
    late:     { label: 'Noted, less than 3 hours before class, no make-up credit', color: '#64748b' }
  };
  var REASON_MAX = 300;

  function _nameOf(list, id, key) {
    var hit = (list || []).find(function(x) { return x.id === id; });
    return hit ? hit[key] : '';
  }

  function _sessionLabel(date, time) {
    return App.Utils.formatDate(date) + ' ' + App.Utils.formatTime(time);
  }

  // Nothing is ticked: a hurried tap must not report the wrong class.
  function _sessionOptionsHtml(sessions) {
    if (sessions.length === 0) return '<p style="font-size:0.85rem;color:#94a3b8">No classes in the next three weeks.</p>';
    return sessions.map(function(s, i) {
      var key = s.classId + '|' + s.date;
      var taken = s.reported ? ' disabled' : '';
      var note = s.reported ? (STATUS[s.reported] || {}).label || 'Reported'
        : (s.inTime ? '' : 'Less than 3 hours away: noted, but no make-up credit');
      return '<label style="display:flex;gap:0.6rem;align-items:flex-start;padding:0.55rem 0;border-bottom:1px solid #f1f5f9;cursor:pointer">'
        + '<input type="radio" name="session" value="' + App.Utils.esc(key) + '"' + taken + ' style="margin-top:0.2rem">'
        + '<span><span style="font-size:0.85rem;font-weight:600;color:#111">' + App.Utils.esc(_sessionLabel(s.date, s.time)) + ' · ' + App.Utils.esc(s.className) + '</span>'
        + (note ? '<br><span style="font-size:0.72rem;color:#94a3b8">' + App.Utils.esc(note) + '</span>' : '')
        + '</span></label>';
    }).join('');
  }

  function reportModal(studentId) {
    var child = (App.Store.get().students || []).find(function(s) { return s.id === studentId; });
    App.Api.get('/api/absence-reports/sessions?studentId=' + encodeURIComponent(studentId)).then(function(sessions) {
      App.Utils.showModal('<div class="p-6" style="width:min(480px,92vw)">'
        + '<h2 class="text-lg font-bold mb-1">Report an absence</h2>'
        + '<p style="font-size:0.8rem;color:#64748b;margin:0 0 1rem">' + App.Utils.esc(child ? child.firstName : '') + ' will miss this class. Tell us at least 3 hours before it starts for a make-up credit.</p>'
        + '<form id="absence-form">'
        + '<div style="max-height:18rem;overflow-y:auto">' + _sessionOptionsHtml(sessions || []) + '</div>'
        + '<label class="block text-sm font-medium text-slate-700 mt-3 mb-1" for="absence-reason">Reason <span class="text-xs text-slate-400 font-normal">(optional)</span></label>'
        + '<input id="absence-reason" name="reason" class="form-input" maxlength="' + REASON_MAX + '" placeholder="e.g. Doctor\'s appointment">'
        + '<div class="flex justify-end gap-3 pt-4">'
        + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
        + ((sessions || []).length ? '<button type="submit" style="min-height:44px;padding:0.5rem 1.1rem;font-size:0.85rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:4px;cursor:pointer">Send</button>' : '')
        + '</div></form></div>');
      document.getElementById('absence-form').addEventListener('submit', function(e) {
        e.preventDefault();
        _submit(studentId, new FormData(e.target), e.submitter);
      });
    }).catch(function() { /* App.Api already toasted */ });
  }

  function _submit(studentId, fd, btn) {
    var picked = fd.get('session');
    if (!picked) { App.Utils.showToast('Pick the class your child will miss', 'error'); return; }
    var parts = picked.split('|');
    App.Utils.withLoading(btn, function() {
      return App.Api.post('/api/absence-reports', { studentId: studentId, classId: parts[0], date: parts[1], reason: (fd.get('reason') || '').trim() })
        .then(function(rep) {
          App.Utils.hideModal(true);
          App.Utils.showToast(rep && rep.status === 'late' ? 'Noted. It was less than 3 hours before class, so no make-up credit.' : 'Thanks for telling us. You will see here whether a make-up credit is approved.', 'success');
          return App.Api.loadSnapshot().then(function() { App.Router.refresh(); });
        })
        .catch(function() { /* App.Api already toasted, e.g. already reported */ });
    });
  }

  // The parent's own reports, newest session first.
  // students are the parent's own children; the admin's parent preview holds every family's reports.
  function parentListHtml(reports, students, classes) {
    var own = {};
    students.forEach(function(st) { own[st.id] = true; });
    reports = reports.filter(function(r) { return own[r.studentId]; });
    if (!reports.length) return '';
    var rows = reports.slice().sort(function(a, b) { return b.sessionDate.localeCompare(a.sessionDate); }).map(function(r) {
      var st = STATUS[r.status] || { label: r.status, color: '#64748b' };
      return '<div style="padding:0.55rem 0;border-top:1px solid #f4f4f2;font-size:0.8rem">'
        + '<div style="font-weight:600;color:#111">' + App.Utils.esc(_nameOf(students, r.studentId, 'firstName')) + ' · ' + App.Utils.esc(_nameOf(classes, r.classId, 'name')) + ' · ' + App.Utils.esc(_sessionLabel(r.sessionDate, r.sessionTime)) + '</div>'
        + '<div style="color:' + st.color + '">' + App.Utils.esc(st.label) + '</div>'
        + (r.decisionNote ? '<div style="color:#64748b">' + App.Utils.esc(r.decisionNote) + '</div>' : '')
        + '</div>';
    }).join('');
    return '<div style="background:#fff;border:1px solid rgba(0,0,0,0.07);padding:1.25rem 1.5rem;margin-top:1rem">'
      + '<div style="font-size:0.72rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.5rem">Absences you reported</div>'
      + rows + '</div>';
  }

  // Staff queue: reports waiting for a decision. The server already scoped them to this person.
  function reviewHtml(reports, students, classes) {
    // The admin's teacher preview holds the whole queue; a teacher's own sessions are already scoped.
    var mine = App.currentRole === 'teacher' && App.currentTeacher ? classes.filter(function(c) { return (c.teacherIds || []).indexOf(App.currentTeacher) > -1; }).map(function(c) { return c.id; }) : null;
    var pending = reports.filter(function(r) { return r.status === 'pending' && (!mine || mine.indexOf(r.classId) > -1); })
      .sort(function(a, b) { return a.sessionDate.localeCompare(b.sessionDate); });
    if (!pending.length) return '';
    return '<section aria-label="Absence reports to review" style="background:#fff;border:1px solid #fde68a;border-left:3px solid #d97706;padding:1rem 1.25rem;margin-bottom:1rem">'
      + '<div style="font-size:0.72rem;font-weight:700;color:#92400e;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.5rem">Absence reports to review (' + pending.length + ')</div>'
      + pending.map(function(r) {
        return '<div style="display:flex;gap:0.75rem;align-items:center;flex-wrap:wrap;padding:0.5rem 0;border-top:1px solid #fef3c7;font-size:0.8rem">'
          + '<div style="flex:1 1 14rem;min-width:0"><div style="font-weight:600;color:#111">' + App.Utils.esc(_nameOf(students, r.studentId, 'firstName') + ' ' + _nameOf(students, r.studentId, 'lastName')) + ' · ' + App.Utils.esc(_nameOf(classes, r.classId, 'name')) + '</div>'
          + '<div style="color:#64748b">' + App.Utils.esc(_sessionLabel(r.sessionDate, r.sessionTime)) + (r.reason ? ' · ' + App.Utils.esc(r.reason) : '') + '</div></div>'
          + '<button type="button" onclick="App.Absence._approve(\'' + r.id + '\', this)" style="min-height:44px;padding:0.35rem 0.9rem;font-size:0.75rem;font-weight:700;background:#15803d;color:#fff;border:none;border-radius:4px;cursor:pointer">Approve credit</button>'
          + '<button type="button" onclick="App.Absence._declineModal(\'' + r.id + '\')" style="min-height:44px;padding:0.35rem 0.9rem;font-size:0.75rem;font-weight:700;background:#fff;color:#b91c1c;border:1px solid #fecaca;border-radius:4px;cursor:pointer">Decline</button>'
          + '</div>';
      }).join('')
      + '</section>';
  }

  function _decide(id, approve, note, btn) {
    return App.Utils.withLoading(btn, function() {
      return App.Api.post('/api/absence-reports/' + encodeURIComponent(id) + '/decision', { approve: approve, note: note || '' })
        .then(function(res) {
          App.Utils.hideModal(true);
          var msg = !approve ? 'Declined. The parent can see why.'
            : (res && res.credited ? 'Approved, make-up credit added' : 'Approved. This class already had a make-up credit, so none was added.');
          App.Utils.showToast(msg, 'success');
          return App.Api.loadSnapshot().then(function() { App.Router.refresh(); });
        })
        .catch(function() { /* App.Api already toasted */ });
    });
  }

  // Credits are money the centre owes, so approving asks first and says how many.
  async function _approve(id, btn) {
    var st = App.Store.get();
    var rep = (st.absenceReports || []).find(function(r) { return r.id === id; });
    var cls = rep && (st.classes || []).find(function(c) { return c.id === rep.classId; });
    var credits = App.Utils.creditsForClass(cls);
    var ok = await App.Utils.showConfirm({
      title: 'Approve the make-up credit?',
      message: 'Gives ' + credits + ' make-up credit' + (credits === 1 ? '' : 's') + ' (1 credit = 15 minutes). This cannot be undone here.',
      confirmLabel: 'Approve'
    });
    if (!ok) return;
    return _decide(id, true, '', btn);
  }

  function _declineModal(id) {
    App.Utils.showModal('<div class="p-6" style="width:min(420px,92vw)">'
      + '<h2 class="text-lg font-bold mb-2">Decline the make-up credit?</h2>'
      + '<label class="block text-sm text-slate-600 mb-1" for="absence-decline-note">Tell the parent why</label>'
      + '<textarea id="absence-decline-note" class="form-input" rows="3" maxlength="' + REASON_MAX + '"></textarea>'
      + '<div class="flex justify-end gap-3 pt-4">'
      + '<button type="button" onclick="App.Utils.hideModal()" class="px-4 py-2 text-sm border border-slate-200 rounded-lg hover:bg-slate-50">Cancel</button>'
      + '<button type="button" onclick="App.Absence._confirmDecline(\'' + id + '\', this)" style="padding:0.5rem 1.1rem;font-size:0.85rem;font-weight:700;background:#b91c1c;color:#fff;border:none;border-radius:4px;cursor:pointer">Decline</button>'
      + '</div></div>');
  }

  function _confirmDecline(id, btn) {
    var el = document.getElementById('absence-decline-note');
    var note = el ? el.value.trim() : '';
    if (!note) { App.Utils.showToast('Tell the parent why', 'error'); if (el) el.focus(); return; }
    return _decide(id, false, note, btn);
  }

  App.Absence = {
    reportModal: reportModal,
    parentListHtml: parentListHtml,
    reviewHtml: reviewHtml,
    _approve: _approve,
    _declineModal: _declineModal,
    _confirmDecline: _confirmDecline,
    _sessionOptionsHtml: _sessionOptionsHtml
  };
})();
