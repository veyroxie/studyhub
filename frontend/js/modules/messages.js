(function() {
  window.App = window.App || {};

  var _selectedThread = null; // parent email

  function render(container) {
    var s = App.Store.get();
    var isAdmin = App.currentRole === 'admin';
    var students = s.students || [];
    var messages = s.messages || [];

    // Build list of parent threads
    var parents = {};
    students.forEach(function(stu) {
      if (!parents[stu.contact]) {
        parents[stu.contact] = { name: stu.parentName, email: stu.contact, kids: [] };
      }
      parents[stu.contact].kids.push(stu.firstName);
    });

    // In parent mode, force thread to own email
    if (!isAdmin) {
      _selectedThread = App.clientParent;
    } else if (!_selectedThread && Object.keys(parents).length > 0) {
      _selectedThread = Object.keys(parents)[0];
    }

    var threadList = Object.values(parents);

    container.innerHTML = '<div style="display:flex;height:calc(100vh - 120px);gap:0;border-radius:16px;overflow:hidden;border:1px solid rgba(0,0,0,0.08);box-shadow:0 1px 4px rgba(0,0,0,0.06)">'
      + (isAdmin ? _renderThreadList(threadList, messages) : '')
      + _renderThread(_selectedThread, threadList, messages, isAdmin)
      + '</div>';

    // Auto-scroll to bottom
    var msgArea = document.getElementById('msg-scroll');
    if (msgArea) msgArea.scrollTop = msgArea.scrollHeight;

    // Focus input
    var inp = document.getElementById('msg-input');
    if (inp) inp.focus();
  }

  function _renderThreadList(threads, messages) {
    var items = threads.map(function(p) {
      var lastMsg = messages.filter(function(m) { return m.toParent === p.email; }).slice(-1)[0];
      var unread  = messages.filter(function(m) { return m.toParent === p.email && m.fromRole === 'parent' && !m.read; }).length;
      var active  = _selectedThread === p.email;
      return '<div onclick="App.Messages._selectThread(\'' + p.email + '\')" style="display:flex;align-items:center;gap:0.65rem;padding:0.85rem 1rem;cursor:pointer;border-bottom:1px solid #f4f4f2;transition:background 0.12s;background:' + (active ? '#fef9ec' : 'transparent') + '" onmouseover="if(this.style.background!==\'#fef9ec\')this.style.background=\'#fafaf8\'" onmouseout="if(\''+p.email+'\'!==App.Messages._currentThread())this.style.background=\'transparent\'">'
        + '<div style="width:2.2rem;height:2.2rem;border-radius:50%;background:' + (active ? 'var(--gold)' : 'var(--gold-dim)') + ';color:' + (active ? '#0a0a0a' : 'var(--gold)') + ';font-weight:800;font-size:0.82rem;display:flex;align-items:center;justify-content:center;flex-shrink:0;border:1px solid rgba(201,162,39,0.25)">' + p.name.charAt(0).toUpperCase() + '</div>'
        + '<div style="flex:1;min-width:0">'
        +   '<div style="font-size:0.83rem;font-weight:' + (unread > 0 ? '700' : '600') + ';color:#111;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + p.name + '</div>'
        +   '<div style="font-size:0.7rem;color:#94a3b8;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + (lastMsg ? lastMsg.text.slice(0,35) + (lastMsg.text.length > 35 ? '…' : '') : p.kids.join(', ')) + '</div>'
        + '</div>'
        + (unread > 0 ? '<span style="width:1.1rem;height:1.1rem;border-radius:50%;background:var(--gold);color:#0a0a0a;font-size:0.62rem;font-weight:800;display:flex;align-items:center;justify-content:center;flex-shrink:0">' + unread + '</span>' : '')
        + '</div>';
    }).join('');

    return '<div style="width:260px;flex-shrink:0;background:#fff;border-right:1px solid #f0ede8;display:flex;flex-direction:column">'
      + '<div style="padding:1rem;border-bottom:1px solid #f0ede8;background:#faf9f7">'
      +   '<p style="font-size:0.8rem;font-weight:700;color:#111;margin:0">Messages</p>'
      +   '<p style="font-size:0.7rem;color:#94a3b8;margin:2px 0 0">' + threads.length + ' conversations</p>'
      + '</div>'
      + '<div style="flex:1;overflow-y:auto">' + (items || App.Utils.emptyState('No conversations yet', 'Students need to be added before messages appear.', '')) + '</div>'
      + '</div>';
  }

  function _renderThread(parentEmail, threads, messages, isAdmin) {
    var parent = threads.find(function(p) { return p.email === parentEmail; });
    var threadMsgs = messages.filter(function(m) { return m.toParent === parentEmail; });

    var header = parent
      ? '<div style="display:flex;align-items:center;gap:0.65rem;padding:0.85rem 1.1rem;background:#faf9f7;border-bottom:1px solid #f0ede8">'
      +   '<div style="width:2rem;height:2rem;border-radius:50%;background:var(--gold-dim);color:var(--gold);font-weight:800;font-size:0.8rem;display:flex;align-items:center;justify-content:center;border:1px solid rgba(201,162,39,0.25)">' + parent.name.charAt(0) + '</div>'
      +   '<div><div style="font-size:0.85rem;font-weight:700;color:#111">' + parent.name + '</div>'
      +        '<div style="font-size:0.7rem;color:#94a3b8">' + parent.kids.join(', ') + '</div></div>'
      + '</div>'
      : '<div style="padding:0.85rem 1.1rem;background:#faf9f7;border-bottom:1px solid #f0ede8"><p style="font-size:0.85rem;font-weight:700;color:#111;margin:0">Select a conversation</p></div>';

    var bubbles = threadMsgs.length === 0
      ? '<div style="flex:1;display:flex;align-items:center;justify-content:center"><p style="font-size:0.85rem;color:#94a3b8">No messages yet. Say hello!</p></div>'
      : '<div id="msg-scroll" style="flex:1;overflow-y:auto;padding:1rem;display:flex;flex-direction:column;gap:0.6rem">'
      + threadMsgs.map(function(m) {
          var isMine = (isAdmin && m.fromRole === 'admin') || (!isAdmin && m.fromRole === 'parent');
          return '<div style="display:flex;flex-direction:column;align-items:' + (isMine ? 'flex-end' : 'flex-start') + '">'
            + '<div style="max-width:72%;padding:0.55rem 0.85rem;border-radius:' + (isMine ? '14px 14px 4px 14px' : '14px 14px 14px 4px') + ';background:' + (isMine ? 'var(--gold)' : '#f4f4f2') + ';color:' + (isMine ? '#0a0a0a' : '#111') + ';font-size:0.84rem;line-height:1.45">' + _esc(m.text) + '</div>'
            + '<div style="font-size:0.65rem;color:#94a3b8;margin-top:3px;padding:0 4px">' + _fmtTs(m.ts) + '</div>'
            + '</div>';
        }).join('')
      + '</div>';

    var compose = parent
      ? '<div style="padding:0.75rem 1rem;border-top:1px solid #f0ede8;background:#fff;display:flex;gap:0.5rem">'
      +   '<input id="msg-input" type="text" placeholder="Type a message…" onkeydown="if(event.key===\'Enter\')App.Messages._send()" style="flex:1;padding:0.5rem 0.85rem;font-size:0.84rem;border:1px solid #e2e8f0;border-radius:10px;outline:none;background:#faf9f7" onfocus="this.style.borderColor=\'var(--gold)\'" onblur="this.style.borderColor=\'#e2e8f0\'">'
      +   '<button onclick="App.Messages._send()" style="padding:0.5rem 1rem;font-size:0.82rem;font-weight:700;background:var(--gold);color:#0a0a0a;border:none;border-radius:10px;cursor:pointer;transition:opacity 0.15s" onmouseover="this.style.opacity=\'0.82\'" onmouseout="this.style.opacity=\'1\'">Send</button>'
      + '</div>'
      : '';

    return '<div style="flex:1;display:flex;flex-direction:column;background:#fff;min-width:0">'
      + header
      + (parent ? bubbles : '<div style="flex:1">' + App.Utils.emptyState('No conversation selected', 'Select a conversation from the list to get started.', '') + '</div>')
      + compose
      + '</div>';
  }

  function _selectThread(email) {
    _selectedThread = email;
    // Mark messages from this parent as read
    var s = App.Store.get();
    var updated = (s.messages || []).map(function(m) {
      return (m.toParent === email && m.fromRole === 'parent') ? Object.assign({}, m, { read: true }) : m;
    });
    App.Store.set({ messages: updated });
    var container = document.getElementById('messages-page');
    if (container) render(container);
    App.Notifs.refresh();
  }

  function _send() {
    var inp = document.getElementById('msg-input');
    if (!inp || !inp.value.trim() || !_selectedThread) return;
    var text = inp.value.trim();
    inp.value = '';
    var s = App.Store.get();
    var isAdmin = App.currentRole === 'admin';
    var newMsg = {
      id: App.Utils.generateId(),
      fromRole: isAdmin ? 'admin' : 'parent',
      fromLabel: isAdmin ? 'Study Hub' : (s.students.find(function(st) { return st.contact === App.clientParent; }) || {}).parentName || 'Parent',
      toParent: _selectedThread,
      text: text,
      ts: new Date().toISOString(),
      read: false
    };
    App.Store.set({ messages: (s.messages || []).concat(newMsg) });
    // re-render
    var container = document.getElementById('messages-page');
    if (container) render(container);
    App.Notifs.refresh();
  }

  function _esc(str) {
    return String(str).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
  }

  function _fmtTs(ts) {
    if (!ts) return '';
    var d = new Date(ts);
    var now = new Date();
    var sameDay = d.toDateString() === now.toDateString();
    if (sameDay) return d.toLocaleTimeString('en-MY', { hour:'2-digit', minute:'2-digit' });
    return d.toLocaleDateString('en-MY', { day:'numeric', month:'short' }) + ' ' + d.toLocaleTimeString('en-MY', { hour:'2-digit', minute:'2-digit' });
  }

  function _currentThread() { return _selectedThread; }

  App.Messages = { render: render, _selectThread: _selectThread, _send: _send, _currentThread: _currentThread };
})();
