// Profile page (replaces the old cramped "My Profile" modal). Role-aware:
// parents get account, their children, notification controls (push + email),
// security and a Help/FAQ; staff/admin get account + email prefs + security
// (incl. MFA, reusing App.Dashboard's MFA helpers).
(function() {
  window.App = window.App || {};

  // Parent-facing FAQ. Static copy — rendered as native <details> so it needs
  // no JS to expand/collapse. Covers the things parents can't be expected to
  // discover on their own (esp. the iOS install-before-push requirement).
  var FAQ = [
    { q: 'How do I get notified when my child checks in or out?',
      a: 'Tap "Enable push alerts" above and allow the browser prompt. You\'ll then get a notification the moment your child checks in or out — even when StudyHub is closed.' },
    { q: 'I\'m on an iPhone or iPad and don\'t see the prompt.',
      a: 'Apple only allows notifications for installed web apps. Open studyhub.fit in Safari, tap the Share icon, choose "Add to Home Screen", then open StudyHub from your home screen and tap "Enable push alerts" here.' },
    { q: 'Do I also get an email?',
      a: 'Only if you turn on "Email me when my child checks in or out" above. Push is instant; email is an optional backup that also works if you haven\'t enabled push.' },
    { q: 'Will the pop-up alerts show when the app is closed?',
      a: 'In-app pop-ups only appear while StudyHub is open in a tab. Push notifications work even when it\'s fully closed — that\'s why we recommend enabling them.' },
    { q: 'I enabled alerts but nothing arrives.',
      a: 'Check that notifications aren\'t blocked for studyhub.fit in your browser or phone settings. Note: check-in alerts are paused while a monthly fee is unpaid.' },
    { q: 'How do I turn alerts off?',
      a: 'Untick the email option here, and switch off notifications for studyhub.fit in your device settings to stop push alerts.' }
  ];

  function _card(inner) {
    return '<div style="background:#fff;border:1px solid rgba(0,0,0,0.07);border-radius:14px;box-shadow:0 1px 3px rgba(0,0,0,0.04);padding:1.25rem 1.4rem">' + inner + '</div>';
  }

  function _heading(text, sub) {
    var subHtml = sub ? '<p style="font-size:0.78rem;color:#94a3b8;margin:0.15rem 0 0">' + App.Utils.esc(sub) + '</p>' : '';
    return '<h3 style="font-size:0.98rem;font-weight:700;color:#111;margin:0">' + App.Utils.esc(text) + '</h3>' + subHtml;
  }

  function _label(text) {
    return '<label style="display:block;font-size:0.68rem;font-weight:700;color:#64748b;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:0.3rem">' + text + '</label>';
  }

  function _toggle(field, label, checked) {
    return '<label style="display:flex;align-items:flex-start;gap:0.6rem;padding:0.5rem 0;font-size:0.85rem;color:#374151;cursor:pointer">'
      + '<input type="checkbox" class="pf-toggle" data-field="' + field + '"' + (checked ? ' checked' : '') + ' style="margin-top:0.15rem">'
      + '<span>' + label + '</span></label>';
  }

  function _accountCard(profile) {
    return _card(
      _heading('Account', profile.email)
      + '<form id="pf-account-form" style="display:flex;flex-direction:column;gap:0.85rem;margin-top:1rem">'
      + '<div>' + _label('Name') + '<input name="name" value="' + App.Utils.esc(profile.name || '') + '" class="form-input" required></div>'
      + '<div>' + _label('Phone') + '<input name="phone" value="' + App.Utils.esc(profile.phone || '') + '" class="form-input"></div>'
      + '<button type="submit" style="align-self:flex-start;padding:0.5rem 1.1rem;font-size:0.8rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:8px;cursor:pointer">Save changes</button>'
      + '</form>'
    );
  }

  function _childrenCard() {
    var students = (App.Store.get().students || []).filter(function(s) { return s.contact === App.clientParent; });
    var body;
    if (students.length === 0) {
      body = '<p style="font-size:0.85rem;color:#94a3b8;margin:1rem 0 0">No children linked to your account yet.</p>';
    } else {
      body = '<div style="display:flex;flex-direction:column;gap:0.6rem;margin-top:1rem">' + students.map(_childRow).join('') + '</div>';
    }
    return _card(_heading('My children', students.length + ' enrolled') + body);
  }

  function _childRow(s) {
    var classes = (s.enrolledClasses || s.enrolled_classes || []);
    var count = Array.isArray(classes) ? classes.length : 0;
    var status = App.Utils.esc(s.status || 'Active');
    return '<div style="display:flex;align-items:center;gap:0.75rem;padding:0.65rem 0.8rem;background:#fafaf8;border:1px solid #f0eee8;border-radius:10px">'
      + '<div style="width:34px;height:34px;border-radius:50%;background:var(--gold-dim);color:#8a6d12;display:flex;align-items:center;justify-content:center;font-weight:700;font-size:0.85rem">' + App.Utils.esc((s.firstName || '?').charAt(0)) + '</div>'
      + '<div style="flex:1;min-width:0"><div style="font-size:0.88rem;font-weight:600;color:#111">' + App.Utils.esc(s.firstName + ' ' + (s.lastName || '')) + '</div>'
      + '<div style="font-size:0.74rem;color:#94a3b8">' + count + ' class' + (count !== 1 ? 'es' : '') + ' · ' + status + '</div></div></div>';
  }

  function _notificationsCard(profile, isParent) {
    var pushBlock = isParent ? _pushBlock() : '';
    var checkinToggle = isParent ? _toggle('notifyCheckinEmail', 'Email me when my child checks in or out', profile.notifyCheckinEmail) : '';
    var note = isParent
      ? '<p style="font-size:0.72rem;color:#94a3b8;margin:0.85rem 0 0;line-height:1.55">Push works even when StudyHub is closed. In-app pop-ups only show while the app is open. On iPhone/iPad you must "Add to Home Screen" first — see Help below.</p>'
      : '';
    return _card(
      _heading('Notifications', 'Choose how we reach you')
      + pushBlock
      + '<div style="margin-top:0.5rem">'
      + checkinToggle
      + _toggle('notifyInvoiceReminders', 'Email me invoice reminders', profile.notifyInvoiceReminders)
      + _toggle('notifyAnnouncements', 'Email me new announcements', profile.notifyAnnouncements)
      + _toggle('notifyPaymentReceipts', 'Email me payment receipts', profile.notifyPaymentReceipts)
      + '</div>' + note
    );
  }

  function _pushBlock() {
    var granted = App.Push && App.Push.isGranted();
    var label = granted ? 'Push alerts enabled on this device' : 'Enable push alerts';
    var disabled = granted ? ' disabled' : '';
    return '<div style="margin:1rem 0 0.25rem">'
      + '<button type="button" id="pf-push-btn"' + disabled + ' style="padding:0.55rem 1.1rem;font-size:0.82rem;font-weight:700;background:#0a0a0a;color:#fff;border:none;border-radius:8px;cursor:pointer' + (granted ? ';opacity:0.6;cursor:default' : '') + '">' + label + '</button>'
      + '</div>';
  }

  function _securityCard(profile) {
    var isAdmin = App.currentRole === 'admin' || App.currentRole === 'superadmin';
    var mfa = isAdmin ? '<div id="mfa-section" style="margin-top:1.25rem;padding-top:1.25rem;border-top:1px solid #f1f5f9"></div>' : '';
    return _card(
      _heading('Security')
      + '<form id="pf-pw-form" style="display:flex;flex-direction:column;gap:0.75rem;margin-top:1rem">'
      + '<input name="currentPassword" type="password" placeholder="Current password" class="form-input" required>'
      + '<input name="newPassword" type="password" placeholder="New password (min 8 chars)" class="form-input" required minlength="8">'
      + '<button type="submit" style="align-self:flex-start;padding:0.5rem 1.1rem;font-size:0.8rem;font-weight:600;background:#fff;color:#374151;border:1px solid #e2e8f0;border-radius:8px;cursor:pointer">Update password</button>'
      + '</form>' + mfa
    );
  }

  function _helpCard() {
    var items = FAQ.map(function(f) {
      return '<details style="border-bottom:1px solid #f1f5f9;padding:0.7rem 0">'
        + '<summary style="font-size:0.86rem;font-weight:600;color:#1a1a1a;cursor:pointer;list-style:none">' + App.Utils.esc(f.q) + '</summary>'
        + '<p style="font-size:0.82rem;color:#64748b;line-height:1.6;margin:0.6rem 0 0">' + App.Utils.esc(f.a) + '</p></details>';
    }).join('');
    return _card(_heading('Help & FAQ', 'Notifications and account') + '<div style="margin-top:0.75rem">' + items + '</div>');
  }

  function _build(profile) {
    var isParent = App.currentRole === 'client';
    return '<div style="max-width:680px;margin:0 auto;display:flex;flex-direction:column;gap:1rem">'
      + _accountCard(profile)
      + (isParent ? _childrenCard() : '')
      + _notificationsCard(profile, isParent)
      + _securityCard(profile)
      + (isParent ? _helpCard() : '')
      + '<div style="text-align:center;padding:0.25rem 0 0.5rem">'
      +   '<button type="button" id="pf-replay-tour" style="font-size:0.78rem;color:#64748b;background:none;border:none;cursor:pointer;text-decoration:underline">Replay the product tour</button>'
      + '</div>'
      + '</div>';
  }

  function _wire(profile) {
    _wireAccount(profile);
    _wirePassword();
    _wireToggles(profile);
    _wirePush();
    var replay = document.getElementById('pf-replay-tour');
    if (replay) replay.addEventListener('click', function() { if (App.Tutorial) App.Tutorial.start(); });
    if (App.currentRole === 'admin' || App.currentRole === 'superadmin') {
      App.Dashboard._renderMFASection(profile.mfaEnabled);
    }
  }

  function _wireAccount(profile) {
    var form = document.getElementById('pf-account-form');
    if (!form) return;
    form.addEventListener('submit', async function(e) {
      e.preventDefault();
      var fd = new FormData(e.target);
      try {
        await App.Api.put('/api/auth/profile', { name: fd.get('name'), phone: fd.get('phone') });
        profile.name = fd.get('name');
        App.Utils.showToast('Profile updated', 'success');
        await App.Api.loadSnapshot();
      } catch (err) { /* App.Api already toasted the error */ }
    });
  }

  function _wirePassword() {
    var form = document.getElementById('pf-pw-form');
    if (!form) return;
    form.addEventListener('submit', async function(e) {
      e.preventDefault();
      var fd = new FormData(e.target);
      try {
        await App.Api.post('/api/auth/change-password', { currentPassword: fd.get('currentPassword'), newPassword: fd.get('newPassword') });
        App.Utils.showToast('Password changed', 'success');
        e.target.reset();
      } catch (err) { /* App.Api already toasted the error */ }
    });
  }

  function _wireToggles(profile) {
    document.querySelectorAll('.pf-toggle').forEach(function(box) {
      box.addEventListener('change', async function() {
        // name is required by the API; fall back to email for accounts that
        // were auto-created without a display name.
        var body = { name: profile.name || profile.email };
        body[box.dataset.field] = box.checked;
        try {
          await App.Api.put('/api/auth/profile', body);
          App.Utils.showToast('Notification settings saved', 'success');
        } catch (err) {
          box.checked = !box.checked; // revert; App.Api already toasted the error
        }
      });
    });
  }

  function _wirePush() {
    var btn = document.getElementById('pf-push-btn');
    if (!btn) return;
    btn.addEventListener('click', async function() {
      if (!App.Push) return;
      var ok = await App.Push.enable();
      if (ok) { btn.textContent = 'Push alerts enabled on this device'; btn.disabled = true; btn.style.opacity = '0.6'; btn.style.cursor = 'default'; }
    });
  }

  async function render(container) {
    container.innerHTML = '<div style="max-width:680px;margin:0 auto;color:#94a3b8;font-size:0.9rem;padding:2rem 0">Loading your profile…</div>';
    var profile;
    try {
      profile = await App.Api.get('/api/auth/profile');
    } catch (e) {
      container.innerHTML = '<div style="max-width:680px;margin:0 auto;color:#dc2626;font-size:0.9rem;padding:2rem 0">Could not load your profile. Please refresh and try again.</div>';
      return;
    }
    if (!profile) return;
    container.innerHTML = _build(profile);
    _wire(profile);
  }

  App.Profile = { render: render };
})();
