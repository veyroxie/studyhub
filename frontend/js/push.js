// Web push subscription (parents only). Two entry points:
//   - App.Push.init()   — on login/restore: silently re-subscribe IF the parent
//     has already granted permission. Never prompts on load (browsers penalise
//     unsolicited prompts, and it's poor UX).
//   - App.Push.enable() — from the profile modal button: requests permission
//     then subscribes. This is the parent's explicit opt-in.
(function() {
  window.App = window.App || {};

  var _vapidKey = null;

  function _isSupported() {
    return ('serviceWorker' in navigator) && ('PushManager' in window) && ('Notification' in window);
  }

  // VAPID public key is base64url; PushManager wants a Uint8Array.
  function _urlBase64ToUint8Array(base64) {
    var padding = '='.repeat((4 - base64.length % 4) % 4);
    var normalised = (base64 + padding).replace(/-/g, '+').replace(/_/g, '/');
    var raw = atob(normalised);
    var arr = new Uint8Array(raw.length);
    for (var i = 0; i < raw.length; i++) arr[i] = raw.charCodeAt(i);
    return arr;
  }

  async function _ensureKey() {
    if (_vapidKey !== null) return _vapidKey;
    try {
      var resp = await App.Api.get('/api/push/vapid-key', { silent: true });
      _vapidKey = (resp && resp.publicKey) ? resp.publicKey : '';
    } catch (e) {
      _vapidKey = '';
    }
    return _vapidKey;
  }

  async function _subscribe() {
    var reg = await navigator.serviceWorker.ready;
    var sub = await reg.pushManager.getSubscription();
    if (!sub) {
      sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: _urlBase64ToUint8Array(_vapidKey)
      });
    }
    var json = sub.toJSON();
    await App.Api.post('/api/push/subscribe', {
      endpoint: sub.endpoint,
      p256dh: json.keys.p256dh,
      auth: json.keys.auth
    }, { silent: true });
  }

  // init: refresh the subscription on load when already permitted. Endpoints
  // rotate (browser reinstall, key change), so re-posting keeps the server row
  // current. Parents only — staff/admin don't get check-in alerts.
  async function init() {
    if (App.currentRole !== 'client') return;
    if (!_isSupported() || Notification.permission !== 'granted') return;
    if (!await _ensureKey()) return;
    try {
      await _subscribe();
    } catch (e) {
      console.warn('push re-subscribe failed', e);
    }
  }

  // enable: parent's explicit opt-in from the profile modal. Returns true on
  // success so the caller can update the button state.
  async function enable() {
    if (!_isSupported()) {
      App.Utils.showToast('Push not supported on this browser', 'error');
      return false;
    }
    if (!await _ensureKey()) {
      App.Utils.showToast('Push alerts are not available right now', 'error');
      return false;
    }
    var perm = Notification.permission;
    if (perm === 'default') perm = await Notification.requestPermission();
    if (perm !== 'granted') {
      App.Utils.showToast('Notifications blocked — allow them in your browser settings', 'error');
      return false;
    }
    try {
      await _subscribe();
      App.Utils.showToast('Check-in alerts enabled on this device', 'success');
      return true;
    } catch (e) {
      App.Utils.showToast('Could not enable push alerts', 'error');
      return false;
    }
  }

  function isGranted() {
    return _isSupported() && Notification.permission === 'granted';
  }

  App.Push = { init: init, enable: enable, isGranted: isGranted };
})();
