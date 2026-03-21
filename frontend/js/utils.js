(function() {
  window.App = window.App || {};
  let _modalDirty = false;
  let _modalDirtyListeners = [];
  let _previousFocus = null;
  let _trapFocusHandler = null;

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
      _previousFocus = document.activeElement;
      document.getElementById('modal-content').innerHTML = html;
      const overlay = document.getElementById('modal-overlay');
      const content = document.getElementById('modal-content');
      overlay.classList.remove('hidden');
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
      overlay.classList.add('hidden');
      overlay.classList.remove('flex');
      document.body.style.overflow = '';
      document.getElementById('modal-content').innerHTML = '';
      // Restore focus to previously focused element
      if (_previousFocus && _previousFocus.focus) {
        _previousFocus.focus();
        _previousFocus = null;
      }
    },
    showToast(message, type, duration) {
      type = type || 'success';
      duration = duration || 4000;
      var colors = { success:'bg-emerald-500', info:'bg-blue-500', warning:'bg-amber-500', error:'bg-red-500' };
      var icons = { success:'&#10003;', info:'&#8505;', warning:'&#9888;', error:'&#10005;' };
      var container = _getToastContainer();

      // Cap at 5 visible toasts — remove oldest (first child = oldest due to column-reverse)
      while (container.children.length >= 5) {
        container.removeChild(container.firstElementChild);
      }

      var el = document.createElement('div');
      el.className = 'sh-toast flex items-center gap-3 px-4 py-3 rounded-xl text-white shadow-2xl ' + (colors[type] || colors.info);
      el.innerHTML =
        '<span class="text-base font-bold">' + (icons[type] || '&#8505;') + '</span>' +
        '<span class="text-sm font-medium flex-1">' + message + '</span>' +
        '<button style="pointer-events:auto;background:none;border:none;color:rgba(255,255,255,0.7);cursor:pointer;font-size:14px;padding:2px 4px;line-height:1" aria-label="Close">&times;</button>';

      // Announce to screen readers
      var announcer = document.getElementById('live-announcer');
      if (announcer) announcer.textContent = message;

      // Close button handler
      el.querySelector('button').addEventListener('click', function() { _removeToast(el); });

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
      return String(str || '').replace(/&/g,'&amp;').replace(/"/g,'&quot;').replace(/'/g,'&#39;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
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
