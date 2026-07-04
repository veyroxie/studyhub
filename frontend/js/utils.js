(function() {
  window.App = window.App || {};
  let _modalDirty = false;
  let _modalDirtyListeners = [];
  let _previousFocus = null;
  let _trapFocusHandler = null;
  let _modalExitTimer = null; // pending hideModal fade-out cleanup; cancelled if a new modal opens first

  /* ── Toast Container (created once) ── */
  function _getToastContainer() {
    let c = document.getElementById('toast-container');
    if (!c) {
      c = document.createElement('div');
      c.id = 'toast-container';
      c.style.cssText = 'position:fixed;bottom:1.5rem;right:1.5rem;z-index:9999;display:flex;flex-direction:column-reverse;gap:0.5rem;pointer-events:none;max-width:380px;width:100%';
      document.body.appendChild(c);
    }
    return c;
  }

  /* ── Toast CSS (injected once) ── */
  (function _injectToastCSS() {
    if (document.getElementById('sh-toast-css')) return;
    var s = document.createElement('style');
    s.id = 'sh-toast-css';
    s.textContent =
      '@keyframes shToastIn{from{opacity:0;transform:translateX(40px)}to{opacity:1;transform:translateX(0)}}' +
      '@keyframes shToastOut{from{opacity:1;transform:translateX(0)}to{opacity:0;transform:translateX(40px)}}' +
      '.sh-toast{pointer-events:auto;animation:shToastIn .3s ease forwards}' +
      '.sh-toast.sh-toast-exit{animation:shToastOut .25s ease forwards}';
    document.head.appendChild(s);
  })();

  /* ── Modal dirty-tracking helpers ── */
  function _attachDirtyListeners() {
    _modalDirty = false;
    _detachDirtyListeners();
    var mc = document.getElementById('modal-content');
    if (!mc) return;
    var handler = function() { _modalDirty = true; };
    ['input', 'change'].forEach(function(evt) {
      mc.addEventListener(evt, handler);
      _modalDirtyListeners.push({ el: mc, evt: evt, fn: handler });
    });
  }

  function _detachDirtyListeners() {
    _modalDirtyListeners.forEach(function(l) {
      l.el.removeEventListener(l.evt, l.fn);
    });
    _modalDirtyListeners = [];
  }

  App.Utils = {
    showModal(html) {
      // Cancel a still-pending hideModal fade-out so its delayed cleanup can't
      // wipe the modal we're about to show (open-right-after-close race).
      if (_modalExitTimer) { clearTimeout(_modalExitTimer); _modalExitTimer = null; }
      _previousFocus = document.activeElement;
      document.getElementById('modal-content').innerHTML = html;
      const overlay = document.getElementById('modal-overlay');
      const content = document.getElementById('modal-content');
      overlay.classList.remove('hidden');
      overlay.classList.remove('sh-modal-exit');
      overlay.classList.add('flex');
      document.body.style.overflow = 'hidden';

      // ARIA attributes
      overlay.setAttribute('aria-hidden', 'true');
      content.setAttribute('role', 'dialog');
      content.setAttribute('aria-modal', 'true');
      var titleEl = content.querySelector('h2');
      if (titleEl) {
        titleEl.id = 'modal-title';
        content.setAttribute('aria-labelledby', 'modal-title');
      }

      // Focus first focusable element
      setTimeout(function() {
        var focusable = content.querySelectorAll('a[href],button:not([disabled]),textarea:not([disabled]),input:not([disabled]),select:not([disabled]),[tabindex]:not([tabindex="-1"])');
        if (focusable.length) focusable[0].focus();
      }, 0);

      // Focus trap + Escape
      _trapFocusHandler = function(e) {
        if (e.key === 'Escape') { App.Utils.hideModal(); return; }
        if (e.key !== 'Tab') return;
        var focusable = content.querySelectorAll('a[href],button:not([disabled]),textarea:not([disabled]),input:not([disabled]),select:not([disabled]),[tabindex]:not([tabindex="-1"])');
        if (focusable.length === 0) return;
        var first = focusable[0];
        var last = focusable[focusable.length - 1];
        if (e.shiftKey) {
          if (document.activeElement === first) { e.preventDefault(); last.focus(); }
        } else {
          if (document.activeElement === last) { e.preventDefault(); first.focus(); }
        }
      };
      document.addEventListener('keydown', _trapFocusHandler);

      // Start tracking form changes after a tick (so initial value-setting doesn't trigger dirty)
      setTimeout(_attachDirtyListeners, 0);
    },
    hideModal(force) {
      if (!force && _modalDirty) {
        if (!confirm('You have unsaved changes. Discard?')) return;
      }
      _modalDirty = false;
      _detachDirtyListeners();
      // Remove focus trap listener
      if (_trapFocusHandler) {
        document.removeEventListener('keydown', _trapFocusHandler);
        _trapFocusHandler = null;
      }
      const overlay = document.getElementById('modal-overlay');
      // Symmetrical exit: fade out over 160ms before clearing. The CSS
      // animation `sh-modal-exit` runs the overlay + content together.
      // Skip animation when the overlay is already hidden (defensive
      // against double-clicks).
      if (overlay.classList.contains('hidden')) return;
      overlay.classList.add('sh-modal-exit');
      document.body.style.overflow = '';
      var prev = _previousFocus;
      _previousFocus = null;
      _modalExitTimer = setTimeout(function() {
        _modalExitTimer = null;
        overlay.classList.remove('sh-modal-exit');
        overlay.classList.add('hidden');
        overlay.classList.remove('flex');
        document.getElementById('modal-content').innerHTML = '';
        if (prev && prev.focus) prev.focus();
      }, 160);
    },

    // withLoading wraps an async action behind a clicked button so the
    // user gets immediate visual feedback: spinner + disabled state for
    // the duration of the fn. Pass the button element OR a CSS selector;
    // returns whatever the async fn returns.
    //
    //   await App.Utils.withLoading(submitBtn, async () => {
    //     await App.Api.post('/api/students', payload);
    //   });
    //
    // Safe to call multiple times — the button's original HTML is
    // captured before the first call and restored after the last.
    async withLoading(btn, fn) {
      if (typeof btn === 'string') btn = document.querySelector(btn);
      if (!btn || !btn.tagName) return fn();
      var orig = btn._origHTML;
      if (orig == null) {
        orig = btn.innerHTML;
        btn._origHTML = orig;
      }
      btn.disabled = true;
      btn.innerHTML = '<span class="sh-spinner" aria-hidden="true"></span>'
        + '<span style="margin-left:0.5em">' + orig + '</span>';
      try {
        return await fn();
      } finally {
        btn.disabled = false;
        btn.innerHTML = orig;
        btn._origHTML = null;
      }
    },
    // showToast renders a stacked toast (top-right by default; theme B
    // moves them above the dock). When `opts.action` is provided, an
    // inline button appears next to the message — used by the undoable
    // delete pattern:
    //   App.Utils.showToast('Invoice deleted', 'info', 6000, {
    //     action: { label: 'Undo', onClick: () => restoreFn() }
    //   });
    showToast(message, type, duration, opts) {
      type = type || 'success';
      duration = duration || 4000;
      opts = opts || {};
      var colors = { success:'bg-emerald-500', info:'bg-blue-500', warning:'bg-amber-500', error:'bg-red-500' };
      var icons = { success:'&#10003;', info:'&#8505;', warning:'&#9888;', error:'&#10005;' };
      var container = _getToastContainer();

      // Cap at 5 visible toasts — remove oldest (first child = oldest due to column-reverse)
      while (container.children.length >= 5) {
        container.removeChild(container.firstElementChild);
      }

      var el = document.createElement('div');
      el.className = 'sh-toast flex items-center gap-3 px-4 py-3 rounded-xl text-white shadow-2xl ' + (colors[type] || colors.info);
      // Build with text nodes, not innerHTML — the message can come from a
      // server error string or a broadcast student name and must never be
      // parsed as HTML.
      var iconSpan = document.createElement('span');
      iconSpan.className = 'text-base font-bold';
      iconSpan.innerHTML = icons[type] || '&#8505;';
      var msgSpan = document.createElement('span');
      msgSpan.className = 'text-sm font-medium flex-1';
      msgSpan.textContent = String(message == null ? '' : message);
      var closeBtn = document.createElement('button');
      closeBtn.setAttribute('aria-label', 'Close');
      closeBtn.setAttribute('style', 'pointer-events:auto;background:none;border:none;color:rgba(255,255,255,0.7);cursor:pointer;font-size:14px;padding:2px 4px;line-height:1');
      closeBtn.innerHTML = '&times;';
      el.appendChild(iconSpan);
      el.appendChild(msgSpan);

      // Inline action (Undo / Retry / etc.). Clicking the action runs the
      // callback AND dismisses the toast — the typical undo flow doesn't
      // want both the toast and the row to keep flashing.
      if (opts.action && opts.action.label && typeof opts.action.onClick === 'function') {
        var actBtn = document.createElement('button');
        actBtn.textContent = opts.action.label;
        actBtn.setAttribute('style', 'background:rgba(255,255,255,0.18);border:1px solid rgba(255,255,255,0.35);color:#fff;font-weight:700;font-size:0.78rem;padding:0.25rem 0.65rem;border-radius:6px;cursor:pointer;margin-right:0.35rem');
        actBtn.addEventListener('click', function() {
          try { opts.action.onClick(); } catch(e) {}
          _removeToast(el);
        });
        el.appendChild(actBtn);
      }

      el.appendChild(closeBtn);

      // Announce to screen readers
      var announcer = document.getElementById('live-announcer');
      if (announcer) announcer.textContent = String(message == null ? '' : message);

      // Close button handler
      closeBtn.addEventListener('click', function() { _removeToast(el); });

      container.appendChild(el);

      // Auto-dismiss
      var timer = setTimeout(function() { _removeToast(el); }, duration);
      el._toastTimer = timer;
    },
    formatDate(dateStr) {
      if (!dateStr) return '\u2014';
      try {
        return new Date(dateStr + 'T00:00:00').toLocaleDateString('en-MY', { day:'numeric', month:'short', year:'numeric' });
      } catch(e) { return dateStr; }
    },
    formatMonth(monthStr) {
      if (!monthStr) return '\u2014';
      const [y, m] = monthStr.split('-');
      const names = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
      return (names[parseInt(m,10)-1] || m) + ' ' + y;
    },
    formatCurrency(amount) {
      return 'RM ' + parseFloat(amount || 0).toFixed(2);
    },
    formatTime(timeStr) {
      if (!timeStr) return '\u2014';
      const parts = timeStr.split(':');
      let h = parseInt(parts[0], 10);
      const m = parts[1] || '00';
      const period = h >= 12 ? 'pm' : 'am';
      if (h > 12) h -= 12;
      if (h === 0) h = 12;
      return h + ':' + m + ' ' + period;
    },
    generateId(prefix) {
      prefix = prefix || 'id';
      return prefix + '_' + Date.now() + '_' + Math.random().toString(36).slice(2, 7);
    },
    badge(text, color) {
      color = color || 'blue';
      const map = {
        green:  'bg-emerald-50 text-emerald-700 ring-emerald-500/30',
        red:    'bg-red-50 text-red-700 ring-red-500/30',
        yellow: 'bg-amber-50 text-amber-700 ring-amber-500/30',
        blue:   'bg-blue-50 text-blue-700 ring-blue-500/30',
        gray:   'bg-gray-100 text-gray-600 ring-gray-400/30',
        purple: 'bg-purple-50 text-purple-700 ring-purple-500/30',
        orange: 'bg-orange-50 text-orange-700 ring-orange-500/30',
        teal:   'bg-teal-50 text-teal-700 ring-teal-500/30'
      };
      return '<span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-semibold ring-1 ring-inset ' + (map[color]||map.blue) + '">' + text + '</span>';
    },
    statusBadge(status) {
      const map = {
        'Active':'green','Inactive':'red','New':'blue','Waitlisted':'yellow',
        'Paid':'green','Unpaid':'yellow','Overdue':'red','Pending Verification':'purple',
        'Present':'green','Absent':'red','Late':'yellow',
        'Notice':'blue','Reminder':'yellow','Urgent':'red',
        'Pending':'yellow'
      };
      return App.Utils.badge(status, map[status] || 'gray');
    },
    colorClasses(color) {
      const map = {
        green:  { bg:'bg-emerald-100', border:'border-emerald-400', text:'text-emerald-800', dot:'bg-emerald-500', pill:'bg-emerald-100 text-emerald-800' },
        teal:   { bg:'bg-teal-100',    border:'border-teal-400',    text:'text-teal-800',    dot:'bg-teal-500',    pill:'bg-teal-100 text-teal-800'    },
        orange: { bg:'bg-orange-100',  border:'border-orange-400',  text:'text-orange-800',  dot:'bg-orange-500',  pill:'bg-orange-100 text-orange-800'  },
        blue:   { bg:'bg-blue-100',    border:'border-blue-400',    text:'text-blue-800',    dot:'bg-blue-500',    pill:'bg-blue-100 text-blue-800'    },
        purple: { bg:'bg-purple-100',  border:'border-purple-400',  text:'text-purple-800',  dot:'bg-purple-500',  pill:'bg-purple-100 text-purple-800'  }
      };
      return map[color] || map.blue;
    },
    esc(str) {
      return String(str == null ? '' : str).replace(/&/g,'&amp;').replace(/"/g,'&quot;').replace(/'/g,'&#39;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
    },
    today() {
      return new Date().toISOString().slice(0, 10);
    },
    nowTime() {
      const d = new Date();
      return d.getHours().toString().padStart(2,'0') + ':' + d.getMinutes().toString().padStart(2,'0');
    },
    emptyState(title, subtitle, btnHtml) {
      return '<div style="display:flex;flex-direction:column;align-items:center;justify-content:center;padding:4rem 2rem;text-align:center">'
        + '<div style="width:64px;height:64px;background:#f1f5f9;border-radius:50%;display:flex;align-items:center;justify-content:center;margin-bottom:1.25rem">'
        + '<svg width="28" height="28" fill="none" stroke="#94a3b8" stroke-width="1.5" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>'
        + '</div>'
        + '<div style="font-size:1rem;font-weight:700;color:#334155;margin-bottom:0.4rem">' + title + '</div>'
        + '<div style="font-size:0.83rem;color:#94a3b8;max-width:300px;line-height:1.5;margin-bottom:' + (btnHtml ? '1.25rem' : '0') + '">' + subtitle + '</div>'
        + (btnHtml || '')
        + '</div>';
    }
  };

  function _removeToast(el) {
    if (!el || !el.parentNode) return;
    clearTimeout(el._toastTimer);
    el.classList.add('sh-toast-exit');
    setTimeout(function() { if (el.parentNode) el.parentNode.removeChild(el); }, 260);
  }

  // ── Confirm Dialog (replaces browser confirm()) ─────────────────────────────
  // Usage: App.Utils.showConfirm({ title, message, details, confirmLabel,
  //   cancelLabel, danger, icon }).then(ok => { if (ok) ... })
  //   - message accepts HTML (legacy: some callers pass <strong>/<br>)
  //   - details renders as a smaller boxed note under the message
  //   - icon: 'trash' | 'warning' | 'info' | 'question' (auto if omitted)
  function _confirmIconSvg(kind) {
    switch (kind) {
      case 'trash':
        return '<polyline points="3 6 5 6 21 6"/>'
          + '<path d="M19 6l-1.2 13.2A2 2 0 0 1 15.8 21H8.2A2 2 0 0 1 6.2 19.2L5 6"/>'
          + '<line x1="10" y1="11" x2="10" y2="17"/>'
          + '<line x1="14" y1="11" x2="14" y2="17"/>'
          + '<path d="M9 6V4a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v2"/>';
      case 'warning':
        return '<path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>'
          + '<line x1="12" y1="9" x2="12" y2="13"/>'
          + '<line x1="12" y1="17" x2="12.01" y2="17"/>';
      case 'info':
        return '<circle cx="12" cy="12" r="10"/>'
          + '<line x1="12" y1="16" x2="12" y2="12"/>'
          + '<line x1="12" y1="8" x2="12.01" y2="8"/>';
      case 'question':
      default:
        return '<circle cx="12" cy="12" r="10"/>'
          + '<path d="M9.5 9a2.5 2.5 0 1 1 4.5 1.5c-1 .8-2 1.2-2 2.5"/>'
          + '<line x1="12" y1="17" x2="12.01" y2="17"/>';
    }
  }

  App.Utils.showConfirm = function(opts) {
    opts = opts || {};
    var title = opts.title || 'Are you sure?';
    var message = opts.message || '';
    var details = opts.details || '';
    var confirmLabel = opts.confirmLabel || 'Confirm';
    var cancelLabel = opts.cancelLabel || 'Cancel';
    var danger = !!opts.danger;
    var iconKind = opts.icon || (function() {
      if (!danger) return 'question';
      // Delete/Remove/Reject → trash icon; other danger → warning triangle.
      var t = (title || '').toLowerCase();
      if (/^(delete|remove|reject)\b/.test(t)) return 'trash';
      return 'warning';
    })();
    var iconBg = danger ? '#fdecec' : '#fbf3dc';
    var iconStroke = danger ? '#c1272d' : '#8a6d12';
    var actionClass = danger ? 'sh-btn-danger' : 'sh-btn-primary';

    return new Promise(function(resolve) {
      var id = 'confirm-' + Date.now();
      var iconSvg = _confirmIconSvg(iconKind);

      var html = '<div class="sh-confirm">'
        +   '<div class="sh-confirm-head">'
        +     '<div class="sh-confirm-icon" style="background:' + iconBg + '">'
        +       '<svg width="22" height="22" viewBox="0 0 24 24" fill="none" '
        +         'stroke="' + iconStroke + '" stroke-width="1.7" '
        +         'stroke-linecap="round" stroke-linejoin="round">'
        +         iconSvg
        +       '</svg>'
        +     '</div>'
        +     '<div class="sh-confirm-text">'
        +       '<h3 class="sh-confirm-title">' + App.Utils.esc(title) + '</h3>'
        +       (message ? '<p class="sh-confirm-message">' + message + '</p>' : '')
        +       (details ? '<div class="sh-confirm-details">' + App.Utils.esc(details) + '</div>' : '')
        +     '</div>'
        +   '</div>'
        +   '<div class="sh-confirm-actions">'
        +     '<button id="' + id + '-cancel" type="button" class="sh-btn-secondary">' + App.Utils.esc(cancelLabel) + '</button>'
        +     '<button id="' + id + '-ok" type="button" class="' + actionClass + '">' + App.Utils.esc(confirmLabel) + '</button>'
        +   '</div>'
        + '</div>';

      App.Utils.showModal(html);
      _modalDirty = false;

      // Narrow + softer-cornered frame for confirm dialogs only.
      var mc = document.getElementById('modal-content');
      mc.classList.add('sh-confirm-frame');

      var okBtn = document.getElementById(id + '-ok');
      var cancelBtn = document.getElementById(id + '-cancel');

      function finish(result) {
        mc.classList.remove('sh-confirm-frame');
        App.Utils.hideModal(true);
        resolve(result);
      }

      cancelBtn.addEventListener('click', function() { finish(false); });
      okBtn.addEventListener('click', function() { finish(true); });

      // Danger defaults focus to Cancel (safer). Non-danger focuses Confirm.
      setTimeout(function() { (danger ? cancelBtn : okBtn).focus(); }, 30);
    });
  };

  // Close modal on overlay click
  document.addEventListener('DOMContentLoaded', function() {
    // Hide the old toast element if it exists
    var oldToast = document.getElementById('toast');
    if (oldToast) oldToast.style.display = 'none';

    document.getElementById('modal-overlay').addEventListener('click', function(e) {
      if (e.target === this) App.Utils.hideModal();
    });
  });
})();
