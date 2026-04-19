(function() {
  window.App = window.App || {};

  var _KEY = 'sh_tutorial_done';
  var _step = 0;
  var _steps = [];
  var _overlay = null;
  var _tooltip = null;

  function _getSteps() {
    var isAdmin = App.currentRole === 'admin';
    var isTeacher = App.currentRole === 'teacher';

    var steps = [
      {
        title: 'Welcome to StudyHub',
        text: 'Let\'s take a quick tour of your dashboard. This will only take a minute.',
        target: null
      },
      {
        title: 'Dashboard',
        text: 'This is your home base. You\'ll see key stats, quick actions, and alerts here.',
        target: '[data-page="dashboard"]'
      },
      {
        title: 'Calendar',
        text: 'View all classes, workshops, and events. Switch between weekly and monthly views.',
        target: '[data-page="calendar"]'
      }
    ];

    if (isAdmin || isTeacher) {
      steps.push({
        title: 'Students',
        text: isAdmin
          ? 'Manage all students — view profiles, approve registrations, track enrolments.'
          : 'View your students\' profiles and attendance history.',
        target: '[data-page="students"]'
      });
    }

    if (isAdmin || !isTeacher) {
      steps.push({
        title: 'Billing',
        text: isAdmin
          ? 'Track invoices, mark payments, generate monthly bills, and view financial reports.'
          : 'View your child\'s invoices and submit payment confirmations.',
        target: '[data-page="billing"]'
      });
    }

    if (isAdmin || isTeacher) {
      steps.push({
        title: 'Attendance',
        text: 'Check students and staff in/out. Supports barcode scanner for quick kiosk mode.',
        target: '[data-page="attendance"]'
      });
    }

    steps.push({
      title: 'Announcements',
      text: isAdmin
        ? 'Post announcements to parents and staff. Set auto-archive dates for time-sensitive notices.'
        : isTeacher
          ? 'View announcements and submit new ones for admin approval.'
          : 'Stay updated with the latest announcements from the centre.',
      target: '[data-page="communication"]'
    });

    if (isAdmin || isTeacher) {
      steps.push({
        title: 'Classroom Feedback',
        text: 'Log session notes per class — topic, mood, and per-student feedback for parents to see.',
        target: '[data-page="feedback"]'
      });
    }

    if (isAdmin) {
      steps.push({
        title: 'Staff',
        text: 'Manage teachers and staff — payroll, performance reviews, and schedules.',
        target: '[data-page="staff"]'
      });
      steps.push({
        title: 'Analytics',
        text: 'Revenue charts, attendance trends, and enrolment insights — all in one place.',
        target: '[data-page="analytics"]'
      });
    }

    steps.push({
      title: 'Notifications',
      text: 'Click the bell to see alerts — overdue payments, attendance issues, pending approvals.',
      target: '#notif-bell-btn, #dock-notif-bell-wrap button, #top-notif-bell-wrap button'
    });

    steps.push({
      title: 'You\'re all set!',
      text: 'That\'s the tour. You can always replay it from the help menu. Enjoy using StudyHub!',
      target: null
    });

    return steps;
  }

  function _createOverlay() {
    if (_overlay) return;

    _overlay = document.createElement('div');
    _overlay.id = 'tutorial-overlay';
    _overlay.style.cssText = 'position:fixed;inset:0;z-index:10000;pointer-events:none';

    _tooltip = document.createElement('div');
    _tooltip.id = 'tutorial-tooltip';
    _tooltip.style.cssText = 'position:fixed;z-index:10001;background:#fff;border-radius:16px;padding:1.5rem;'
      + 'box-shadow:0 20px 60px rgba(0,0,0,0.25),0 2px 8px rgba(0,0,0,0.1);max-width:340px;width:90vw;pointer-events:auto;'
      + 'border:1px solid rgba(0,0,0,0.08)';

    document.body.appendChild(_overlay);
    document.body.appendChild(_tooltip);

    // Backdrop
    var backdrop = document.createElement('div');
    backdrop.style.cssText = 'position:fixed;inset:0;background:rgba(0,0,0,0.45);pointer-events:auto;z-index:9999';
    backdrop.id = 'tutorial-backdrop';
    backdrop.onclick = function() { _next(); };
    document.body.appendChild(backdrop);
  }

  function _cleanup() {
    var el;
    el = document.getElementById('tutorial-overlay'); if (el) el.remove();
    el = document.getElementById('tutorial-tooltip'); if (el) el.remove();
    el = document.getElementById('tutorial-backdrop'); if (el) el.remove();
    el = document.getElementById('tutorial-highlight'); if (el) el.remove();
    _overlay = null;
    _tooltip = null;
  }

  function _highlightTarget(step) {
    // Remove previous highlight
    var old = document.getElementById('tutorial-highlight');
    if (old) old.remove();

    if (!step.target) return null;

    // Try multiple selectors (comma-separated)
    var targets = step.target.split(',').map(function(s) { return s.trim(); });
    var el = null;
    for (var i = 0; i < targets.length; i++) {
      el = document.querySelector(targets[i]);
      if (el && el.offsetParent !== null) break;
      el = null;
    }
    if (!el) return null;

    var rect = el.getBoundingClientRect();
    var pad = 6;

    var highlight = document.createElement('div');
    highlight.id = 'tutorial-highlight';
    highlight.style.cssText = 'position:fixed;z-index:10000;border-radius:12px;pointer-events:none;'
      + 'box-shadow:0 0 0 4000px rgba(0,0,0,0.45);'
      + 'border:2px solid var(--gold,#C9A227);'
      + 'transition:all 0.3s ease;'
      + 'top:' + (rect.top - pad) + 'px;'
      + 'left:' + (rect.left - pad) + 'px;'
      + 'width:' + (rect.width + pad * 2) + 'px;'
      + 'height:' + (rect.height + pad * 2) + 'px';
    document.body.appendChild(highlight);

    // Hide the flat backdrop when we have a target highlight
    var backdrop = document.getElementById('tutorial-backdrop');
    if (backdrop) backdrop.style.background = 'transparent';

    return rect;
  }

  function _positionTooltip(targetRect) {
    if (!_tooltip) return;

    if (!targetRect) {
      // Center on screen
      _tooltip.style.top = '50%';
      _tooltip.style.left = '50%';
      _tooltip.style.transform = 'translate(-50%, -50%)';
      return;
    }

    _tooltip.style.transform = 'none';

    var tRect = _tooltip.getBoundingClientRect();
    var vw = window.innerWidth;
    var vh = window.innerHeight;

    // Try below target first
    var top = targetRect.bottom + 16;
    var left = targetRect.left + targetRect.width / 2 - tRect.width / 2;

    // If below won't fit, try above
    if (top + tRect.height > vh - 20) {
      top = targetRect.top - tRect.height - 16;
    }
    // If above won't fit either, position to the right
    if (top < 20) {
      top = targetRect.top;
      left = targetRect.right + 16;
    }
    // Clamp left
    if (left < 16) left = 16;
    if (left + tRect.width > vw - 16) left = vw - tRect.width - 16;

    _tooltip.style.top = top + 'px';
    _tooltip.style.left = left + 'px';
  }

  function _renderStep() {
    if (!_tooltip) return;
    var step = _steps[_step];
    var isFirst = _step === 0;
    var isLast = _step === _steps.length - 1;
    var progress = (_step + 1) + ' / ' + _steps.length;

    // Show backdrop for no-target steps
    var backdrop = document.getElementById('tutorial-backdrop');
    if (backdrop) backdrop.style.background = step.target ? 'transparent' : 'rgba(0,0,0,0.45)';

    var targetRect = _highlightTarget(step);

    _tooltip.innerHTML = '<div style="margin-bottom:0.75rem">'
      + '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.35rem">'
      + '<span style="font-size:1rem;font-weight:700;color:#1a1a1a">' + step.title + '</span>'
      + '<span style="font-size:0.7rem;color:#94a3b8;font-weight:600">' + progress + '</span>'
      + '</div>'
      + '<p style="font-size:0.85rem;color:#64748b;line-height:1.5;margin:0">' + step.text + '</p>'
      + '</div>'
      + '<div style="display:flex;justify-content:space-between;align-items:center;gap:0.75rem">'
      + '<button onclick="App.Tutorial.skip()" style="font-size:0.8rem;color:#94a3b8;background:none;border:none;cursor:pointer;padding:0.25rem 0">Skip tour</button>'
      + '<div style="display:flex;gap:0.5rem">'
      + (!isFirst ? '<button onclick="App.Tutorial._prev()" style="padding:0.4rem 0.9rem;font-size:0.8rem;font-weight:600;border:1px solid #e2e8f0;border-radius:8px;background:#fff;color:#334155;cursor:pointer">Back</button>' : '')
      + '<button onclick="App.Tutorial._next()" style="padding:0.4rem 0.9rem;font-size:0.8rem;font-weight:600;background:var(--gold,#C9A227);color:#fff;border:none;border-radius:8px;cursor:pointer">'
      + (isLast ? 'Finish' : 'Next') + '</button>'
      + '</div>'
      + '</div>';

    _positionTooltip(targetRect);
  }

  function _next() {
    _step++;
    if (_step >= _steps.length) {
      _finish();
      return;
    }
    _renderStep();
  }

  function _prev() {
    if (_step > 0) _step--;
    _renderStep();
  }

  function _finish() {
    _cleanup();
    try { localStorage.setItem(_KEY, '1'); } catch(e) {}
  }

  function start() {
    _step = 0;
    _steps = _getSteps();
    _createOverlay();
    _renderStep();
  }

  function skip() {
    _finish();
  }

  // Auto-start on first visit (checked after login)
  function autoStart() {
    try {
      if (localStorage.getItem(_KEY)) return;
    } catch(e) { return; }
    // Small delay so the dashboard renders first
    setTimeout(start, 800);
  }

  App.Tutorial = {
    start: start,
    skip: skip,
    autoStart: autoStart,
    _next: _next,
    _prev: _prev
  };
})();
