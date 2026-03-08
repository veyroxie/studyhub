(function() {
  window.App = window.App || {};
  let _toastTimer = null;

  App.Utils = {
    showModal(html) {
      document.getElementById('modal-content').innerHTML = html;
      const overlay = document.getElementById('modal-overlay');
      overlay.classList.remove('hidden');
      overlay.classList.add('flex');
      document.body.style.overflow = 'hidden';
    },
    hideModal() {
      const overlay = document.getElementById('modal-overlay');
      overlay.classList.add('hidden');
      overlay.classList.remove('flex');
      document.body.style.overflow = '';
      document.getElementById('modal-content').innerHTML = '';
    },
    showToast(message, type) {
      type = type || 'success';
      const colors = { success:'bg-emerald-500', info:'bg-blue-500', warning:'bg-amber-500', error:'bg-red-500' };
      const icons = { success:'✓', info:'ℹ', warning:'⚠', error:'✕' };
      const toast = document.getElementById('toast');
      toast.innerHTML = '<div class="flex items-center gap-3 px-4 py-3 rounded-xl text-white shadow-2xl ' + (colors[type]||colors.info) + '">'
        + '<span class="text-base font-bold">' + (icons[type]||'ℹ') + '</span>'
        + '<span class="text-sm font-medium">' + message + '</span>'
        + '</div>';
      toast.classList.remove('hidden');
      if (_toastTimer) clearTimeout(_toastTimer);
      _toastTimer = setTimeout(function() { toast.classList.add('hidden'); }, 4000);
    },
    formatDate(dateStr) {
      if (!dateStr) return '—';
      try {
        return new Date(dateStr + 'T00:00:00').toLocaleDateString('en-MY', { day:'numeric', month:'short', year:'numeric' });
      } catch(e) { return dateStr; }
    },
    formatMonth(monthStr) {
      if (!monthStr) return '—';
      const [y, m] = monthStr.split('-');
      const names = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
      return (names[parseInt(m,10)-1] || m) + ' ' + y;
    },
    formatCurrency(amount) {
      return 'RM ' + parseFloat(amount || 0).toFixed(2);
    },
    formatTime(timeStr) {
      if (!timeStr) return '—';
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
        'Paid':'green','Unpaid':'yellow','Overdue':'red',
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
      return String(str || '').replace(/&/g,'&amp;').replace(/"/g,'&quot;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
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

  // Close modal on overlay click
  document.addEventListener('DOMContentLoaded', function() {
    document.getElementById('modal-overlay').addEventListener('click', function(e) {
      if (e.target === this) App.Utils.hideModal();
    });
  });
})();
