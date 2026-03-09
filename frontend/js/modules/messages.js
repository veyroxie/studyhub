(function() {
  window.App = window.App || {};

  // Thread model: threadId = parentEmail + '::' + responderId
  // responderId = 'admin' | staffId
  // Old messages without threadId default to admin thread (backward compat)

  var _selectedThread = null;
  var _search = '';

  function _tid(parentEmail, responderId) {
    return parentEmail + '::' + (responderId || 'admin');
  }
  function _parseTid(threadId) {
    var parts = (threadId || '').split('::');
    return { parentEmail: parts[0] || '', responderId: parts[1] || 'admin' };
  }
  function _msgTid(m) {
    return m.threadId || _tid(m.toParent, 'admin');
  }

  // ── unread count for a specific thread ──────────────────────────────────────
  function unreadCount(messages, threadId, myRole) {
    return messages.filter(function(m) {
      if (_msgTid(m) !== threadId) return false;
      if (m.read) return false;
      if (myRole === 'parent') return m.fromRole !== 'parent';
      if (myRole === 'teacher') return m.fromRole === 'parent';
      return m.fromRole === 'parent'; // admin
    }).length;
  }

  // ── total unread across all my threads ──────────────────────────────────────
  function totalUnread() {
    if (!App.Store) return 0;
    var s = App.Store.get();
    var messages = s.messages || [];
    var isAdmin   = App.currentRole === 'admin';
    var isTeacher = App.currentRole === 'teacher';
    var isParent  = App.currentRole === 'client';
    if (isParent) {
      return messages.filter(function(m) {
        return m.toParent === App.clientParent && m.fromRole !== 'parent' && !m.read;
      }).length;
    }
    if (isTeacher) {
      return messages.filter(function(m) {
        var parsed = _parseTid(_msgTid(m));
        return parsed.responderId === App.currentTeacher && m.fromRole === 'parent' && !m.read;
      }).length;
    }
    // admin
    return messages.filter(function(m) {
      var parsed = _parseTid(_msgTid(m));
      return parsed.responderId === 'admin' && m.fromRole === 'parent' && !m.read;
    }).length;
  }

  function render(container) {
    var s = App.Store.get();
    var isAdmin   = App.currentRole === 'admin';
    var isTeacher = App.currentRole === 'teacher';
    var isParent  = App.currentRole === 'client';
    var students  = s.students || [];
    var staff     = s.staff    || [];
    var messages  = s.messages || [];

    // Build parent map
    var parentMap = {};
    students.forEach(function(stu) {
      if (!stu.contact) return;
      if (!parentMap[stu.contact]) {
        parentMap[stu.contact] = { name: stu.parentName || stu.contact, email: stu.contact, kids: [] };
      }
      parentMap[stu.contact].kids.push(stu.firstName);
    });

    var html;

    if (isParent) {
      var pe = App.clientParent || '';
      // Build contact list: admin + teachers who have taught their kids
      var myClassIds = {};
      students.filter(function(stu) { return stu.contact === pe; })
        .forEach(function(stu) { (stu.enrolledClasses || []).forEach(function(cid) { myClassIds[cid] = true; }); });
      var myTeachers = staff.filter(function(st) {
        return (s.classes || []).some(function(c) { return c.teacherIds.indexOf(st.id) > -1 && myClassIds[c.id]; });
      });

      var contacts = [{ id: 'admin', label: 'The Study Hub', sublabel: 'Admin & Support', avatar: 'SH' }];
      myTeachers.forEach(function(st) {
        contacts.push({ id: st.id, label: st.name || st.fullName || 'Teacher', sublabel: st.role || 'Teacher', avatar: (st.name || '?').charAt(0) });
      });

      if (!_selectedThread) _selectedThread = _tid(pe, 'admin');

      html = _wrap(
        _renderContactList(contacts, pe, messages),
        _renderThread(_selectedThread, parentMap, staff, messages, false, true)
      );

    } else if (isTeacher) {
      var tid2 = App.currentTeacher || '';
      var myClassIds2 = (s.classes || []).filter(function(c) { return c.teacherIds.indexOf(tid2) > -1; }).map(function(c) { return c.id; });
      var myParents = {};
      students.forEach(function(stu) {
        if ((stu.enrolledClasses || []).some(function(cid) { return myClassIds2.indexOf(cid) > -1; })) {
          if (stu.contact) myParents[stu.contact] = true;
        }
      });

      var threads = Object.keys(myParents).map(function(email) {
        return { parentEmail: email, parent: parentMap[email], threadId: _tid(email, tid2) };
      });

      var filtered = _filterThreads(threads);
      if (!_selectedThread || !filtered.find(function(t) { return t.threadId === _selectedThread; })) {
        _selectedThread = filtered.length > 0 ? filtered[0].threadId : null;
      }

      html = _wrap(
        _renderThreadList(filtered, messages, 'teacher'),
        _renderThread(_selectedThread, parentMap, staff, messages, false, false)
      );

    } else {
      // Admin: parent-admin threads only
      var adminThreads = Object.values(parentMap).map(function(p) {
        return { parentEmail: p.email, parent: p, threadId: _tid(p.email, 'admin') };
      });
      var filtered2 = _filterThreads(adminThreads);
      if (!_selectedThread || !filtered2.find(function(t) { return t.threadId === _selectedThread; })) {
        _selectedThread = filtered2.length > 0 ? filtered2[0].threadId : null;
      }

      html = _wrap(
        _renderThreadList(filtered2, messages, 'admin'),
        _renderThread(_selectedThread, parentMap, staff, messages, true, false)
      );
    }

    container.innerHTML = html;
    var msgArea = document.getElementById('msg-scroll');
    if (msgArea) msgArea.scrollTop = msgArea.scrollHeight;
    var inp = document.getElementById('msg-input');
    if (inp) inp.focus();

    _updateMsgBadge();
  }

  function _filterThreads(threads) {
    if (!_search) return threads;
    var q = _search.toLowerCase();
    return threads.filter(function(t) {
      var p = t.parent;
      return p && (p.name.toLowerCase().includes(q) || p.email.toLowerCase().includes(q));
    });
  }

  function _wrap(sidebar, main) {
    return '<div style="display:flex;height:calc(100vh - 120px);gap:0;border-radius:16px;overflow:hidden;border:1px solid rgba(0,0,0,0.08);box-shadow:0 1px 4px rgba(0,0,0,0.06)">'
      + sidebar + main + '</div>';
  }

  // Parent's contact list (who to talk to)
  function _renderContactList(contacts, parentEmail, messages) {
    var items = contacts.map(function(c) {
      var tid = _tid(parentEmail, c.id);
      var threadMsgs = messages.filter(function(m) { return _msgTid(m) === tid; });
      var lastMsg = threadMsgs.slice(-1)[0];
      var unread = unreadCount(messages, tid, 'parent');
      var active = _selectedThread === tid;
      return '<div onclick="App.Messages._selectThread(\'' + _esc(tid) + '\')" style="display:flex;align-items:center;gap:0.65rem;padding:0.85rem 1rem;cursor:pointer;border-bottom:1px solid #f4f4f2;transition:background 0.12s;background:' + (active ? '#fef9ec' : 'transparent') + '">'
        + '<div style="width:2.2rem;height:2.2rem;border-radius:50%;background:' + (active ? 'var(--gold)' : 'var(--gold-dim)') + ';color:' + (active ? '#0a0a0a' : 'var(--gold)') + ';font-weight:800;font-size:0.78rem;display:flex;align-items:center;justify-content:center;flex-shrink:0;border:1px solid rgba(201,162,39,0.25)">' + _esc(c.avatar) + '</div>'
        + '<div style="flex:1;min-width:0">'
        +   '<div style="font-size:0.83rem;font-weight:' + (unread > 0 ? '700' : '600') + ';color:#111;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + _esc(c.label) + '</div>'
        +   '<div style="font-size:0.7rem;color:#94a3b8;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + (lastMsg ? lastMsg.text.slice(0,35) + (lastMsg.text.length > 35 ? '…' : '') : _esc(c.sublabel)) + '</div>'
        + '</div>'
        + (unread > 0 ? '<span style="min-width:1.1rem;height:1.1rem;border-radius:50%;background:var(--gold);color:#0a0a0a;font-size:0.62rem;font-weight:800;display:flex;align-items:center;justify-content:center;padding:0 3px;flex-shrink:0">' + unread + '</span>' : '')
        + '</div>';
    }).join('');

    return '<div style="width:250px;flex-shrink:0;background:#fff;border-right:1px solid #f0ede8;display:flex;flex-direction:column">'
      + '<div style="padding:0.9rem 1rem;border-bottom:1px solid #f0ede8;background:#faf9f7">'
      +   '<p style="font-size:0.82rem;font-weight:700;color:#111;margin:0 0 2px">Messages</p>'
      +   '<p style="font-size:0.7rem;color:#94a3b8;margin:0">' + contacts.length + ' contact' + (contacts.length !== 1 ? 's' : '') + '</p>'
      + '</div>'
      + '<div style="flex:1;overflow-y:auto">' + (items || App.Utils.emptyState('No contacts yet', "You'll be able to message your teachers and admin here.", '')) + '</div>'
      + '</div>';
  }

  // Admin / teacher thread list
  function _renderThreadList(threads, messages, myRole) {
    var items = threads.map(function(t) {
      var threadMsgs = messages.filter(function(m) { return _msgTid(m) === t.threadId; });
      var lastMsg = threadMsgs.slice(-1)[0];
      var unread = unreadCount(messages, t.threadId, myRole);
      var active = _selectedThread === t.threadId;
      var p = t.parent || { name: t.parentEmail, kids: [] };
      return '<div onclick="App.Messages._selectThread(\'' + _esc(t.threadId) + '\')" style="display:flex;align-items:center;gap:0.65rem;padding:0.85rem 1rem;cursor:pointer;border-bottom:1px solid #f4f4f2;transition:background 0.12s;background:' + (active ? '#fef9ec' : 'transparent') + '">'
        + '<div style="width:2.2rem;height:2.2rem;border-radius:50%;background:' + (active ? 'var(--gold)' : 'var(--gold-dim)') + ';color:' + (active ? '#0a0a0a' : 'var(--gold)') + ';font-weight:800;font-size:0.82rem;display:flex;align-items:center;justify-content:center;flex-shrink:0;border:1px solid rgba(201,162,39,0.25)">' + (p.name || '?').charAt(0).toUpperCase() + '</div>'
        + '<div style="flex:1;min-width:0">'
        +   '<div style="font-size:0.83rem;font-weight:' + (unread > 0 ? '700' : '600') + ';color:#111;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + _esc(p.name) + '</div>'
        +   '<div style="font-size:0.7rem;color:#94a3b8;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + (lastMsg ? lastMsg.text.slice(0,35) + (lastMsg.text.length > 35 ? '…' : '') : (p.kids || []).join(', ')) + '</div>'
        + '</div>'
        + (unread > 0 ? '<span style="min-width:1.1rem;height:1.1rem;border-radius:50%;background:var(--gold);color:#0a0a0a;font-size:0.62rem;font-weight:800;display:flex;align-items:center;justify-content:center;padding:0 3px;flex-shrink:0">' + unread + '</span>' : '')
        + '</div>';
    }).join('');

    var searchBar = '<div style="padding:0.5rem 0.85rem;border-bottom:1px solid #f0ede8">'
      + '<input type="search" placeholder="Search…" value="' + _esc(_search) + '" oninput="App.Messages._setSearch(this.value)" style="width:100%;padding:0.35rem 0.65rem;font-size:0.8rem;border:1px solid #e2e8f0;border-radius:8px;outline:none;background:#faf9f7;box-sizing:border-box">'
      + '</div>';

    return '<div style="width:250px;flex-shrink:0;background:#fff;border-right:1px solid #f0ede8;display:flex;flex-direction:column">'
      + '<div style="padding:0.9rem 1rem;border-bottom:1px solid #f0ede8;background:#faf9f7">'
      +   '<p style="font-size:0.82rem;font-weight:700;color:#111;margin:0 0 2px">Messages</p>'
      +   '<p style="font-size:0.7rem;color:#94a3b8;margin:0">' + threads.length + ' conversation' + (threads.length !== 1 ? 's' : '') + '</p>'
      + '</div>'
      + searchBar
      + '<div style="flex:1;overflow-y:auto">' + (items || App.Utils.emptyState('No conversations', myRole === 'admin' ? 'Add students to start messaging parents.' : 'No parents in your classes yet.', '')) + '</div>'
      + '</div>';
  }

  function _renderThread(threadId, parentMap, staff, messages, isAdmin, isParent) {
    if (!threadId) {
      return '<div style="flex:1;display:flex;flex-direction:column;background:#fff;min-width:0">'
        + '<div style="padding:0.85rem 1.1rem;background:#faf9f7;border-bottom:1px solid #f0ede8"><p style="font-size:0.85rem;font-weight:700;color:#111;margin:0">Select a conversation</p></div>'
        + '<div style="flex:1">' + App.Utils.emptyState('No conversation selected', 'Pick a conversation from the list.', '') + '</div>'
        + '</div>';
    }

    var parsed  = _parseTid(threadId);
    var pe      = parsed.parentEmail;
    var respId  = parsed.responderId;
    var parent  = parentMap[pe];
    var threadMsgs = messages.filter(function(m) { return _msgTid(m) === threadId; });

    // Header: show who you're talking to
    var headerName, headerSub;
    if (isParent) {
      if (respId === 'admin') {
        headerName = 'The Study Hub';
        headerSub  = 'Admin & Support';
      } else {
        var teacher = staff.find(function(s) { return s.id === respId; });
        headerName  = teacher ? (teacher.name || teacher.fullName) : 'Teacher';
        headerSub   = teacher ? teacher.role : 'Teacher';
      }
    } else {
      headerName = parent ? parent.name : pe;
      headerSub  = parent ? (parent.kids || []).join(', ') : '';
    }

    var header = '<div style="display:flex;align-items:center;gap:0.65rem;padding:0.85rem 1.1rem;background:#faf9f7;border-bottom:1px solid #f0ede8">'
      + '<div style="width:2rem;height:2rem;border-radius:50%;background:var(--gold-dim);color:var(--gold);font-weight:800;font-size:0.8rem;display:flex;align-items:center;justify-content:center;border:1px solid rgba(201,162,39,0.25)">' + (headerName || '?').charAt(0).toUpperCase() + '</div>'
      + '<div><div style="font-size:0.85rem;font-weight:700;color:#111">' + _esc(headerName || '') + '</div>'
      +      '<div style="font-size:0.7rem;color:#94a3b8">' + _esc(headerSub || '') + '</div></div>'
      + '</div>';

    var isMyRole = App.currentRole;
    var bubbles = threadMsgs.length === 0
      ? '<div style="flex:1;display:flex;align-items:center;justify-content:center"><p style="font-size:0.85rem;color:#94a3b8">No messages yet. Say hello!</p></div>'
      : '<div id="msg-scroll" style="flex:1;overflow-y:auto;padding:1rem;display:flex;flex-direction:column;gap:0.6rem">'
      + threadMsgs.map(function(m) {
          var isMine = (isParent && m.fromRole === 'parent')
            || (isAdmin && m.fromRole === 'admin')
            || (isMyRole === 'teacher' && m.fromRole === 'teacher');
          var senderLabel = !isMine && m.fromLabel
            ? '<div style="font-size:0.65rem;color:#94a3b8;margin-bottom:2px;padding:0 4px">' + _esc(m.fromLabel) + '</div>'
            : '';
          return '<div style="display:flex;flex-direction:column;align-items:' + (isMine ? 'flex-end' : 'flex-start') + '">'
            + senderLabel
            + '<div style="max-width:72%;padding:0.55rem 0.85rem;border-radius:' + (isMine ? '14px 14px 4px 14px' : '14px 14px 14px 4px') + ';background:' + (isMine ? 'var(--gold)' : '#f4f4f2') + ';color:' + (isMine ? '#0a0a0a' : '#111') + ';font-size:0.84rem;line-height:1.45">' + _esc(m.text) + '</div>'
            + '<div style="font-size:0.65rem;color:#94a3b8;margin-top:3px;padding:0 4px">' + _fmtTs(m.ts) + '</div>'
            + '</div>';
        }).join('')
      + '</div>';

    var compose = '<div style="padding:0.75rem 1rem;border-top:1px solid #f0ede8;background:#fff;display:flex;gap:0.5rem">'
      + '<input id="msg-input" type="text" placeholder="Type a message…" onkeydown="if(event.key===\'Enter\')App.Messages._send()" style="flex:1;padding:0.5rem 0.85rem;font-size:0.84rem;border:1px solid #e2e8f0;border-radius:10px;outline:none;background:#faf9f7" onfocus="this.style.borderColor=\'var(--gold)\'" onblur="this.style.borderColor=\'#e2e8f0\'">'
      + '<button onclick="App.Messages._send()" style="padding:0.5rem 1rem;font-size:0.82rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:10px;cursor:pointer">Send</button>'
      + '</div>';

    return '<div style="flex:1;display:flex;flex-direction:column;background:#fff;min-width:0">'
      + header + bubbles + compose + '</div>';
  }

  function _selectThread(threadId) {
    _selectedThread = threadId;
    var parsed = _parseTid(threadId);
    var s = App.Store.get();
    var myRole = App.currentRole;
    var updated = (s.messages || []).map(function(m) {
      if (_msgTid(m) !== threadId) return m;
      // Mark as read: opposite side's messages
      var isTheirMsg = (myRole === 'parent' && m.fromRole !== 'parent')
        || (myRole === 'admin' && m.fromRole === 'parent')
        || (myRole === 'teacher' && m.fromRole === 'parent');
      return isTheirMsg ? Object.assign({}, m, { read: true }) : m;
    });
    App.Store.set({ messages: updated });
    var container = document.getElementById('messages-page');
    if (container) render(container);
    _updateMsgBadge();
  }

  function _send() {
    var inp = document.getElementById('msg-input');
    if (!inp || !inp.value.trim() || !_selectedThread) return;
    var text = inp.value.trim();
    inp.value = '';
    var s = App.Store.get();
    var isAdmin   = App.currentRole === 'admin';
    var isTeacher = App.currentRole === 'teacher';
    var isParent  = App.currentRole === 'client';
    var parsed = _parseTid(_selectedThread);

    var fromRole, fromLabel;
    if (isAdmin) {
      fromRole  = 'admin';
      fromLabel = 'Study Hub Admin';
    } else if (isTeacher) {
      fromRole  = 'teacher';
      var me = (s.staff || []).find(function(st) { return st.id === App.currentTeacher; });
      fromLabel = me ? (me.name || me.fullName) : 'Teacher';
    } else {
      fromRole  = 'parent';
      var myStu = (s.students || []).find(function(st) { return st.contact === App.clientParent; });
      fromLabel = myStu ? myStu.parentName : 'Parent';
    }

    var newMsg = {
      id: App.Utils.generateId('msg'),
      threadId:  _selectedThread,
      fromRole:  fromRole,
      fromLabel: fromLabel,
      toParent:  parsed.parentEmail,
      text:      text,
      ts:        new Date().toISOString(),
      read:      false
    };
    App.Store.set({ messages: (s.messages || []).concat(newMsg) });
    var container = document.getElementById('messages-page');
    if (container) render(container);
    _updateMsgBadge();
  }

  function _setSearch(val) {
    _search = val;
    var container = document.getElementById('messages-page');
    if (container) render(container);
  }

  function _updateMsgBadge() {
    var count = totalUnread();
    ['msg-badge','top-msg-badge'].forEach(function(id) {
      var el = document.getElementById(id);
      if (!el) return;
      if (count > 0) { el.textContent = count > 99 ? '99+' : String(count); el.classList.remove('hidden'); }
      else { el.classList.add('hidden'); }
    });
  }

  function _esc(str) {
    return String(str || '').replace(/&/g,'&amp;').replace(/"/g,'&quot;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
  }
  function _fmtTs(ts) {
    if (!ts) return '';
    var d = new Date(ts);
    var now = new Date();
    if (d.toDateString() === now.toDateString()) return d.toLocaleTimeString('en-MY', { hour:'2-digit', minute:'2-digit' });
    return d.toLocaleDateString('en-MY', { day:'numeric', month:'short' }) + ' ' + d.toLocaleTimeString('en-MY', { hour:'2-digit', minute:'2-digit' });
  }
  function _currentThread() { return _selectedThread; }

  App.Messages = {
    render: render,
    _selectThread: _selectThread,
    _send: _send,
    _setSearch: _setSearch,
    _currentThread: _currentThread,
    totalUnread: totalUnread,
    _updateMsgBadge: _updateMsgBadge
  };
})();
