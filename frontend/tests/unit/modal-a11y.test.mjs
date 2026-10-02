// The overlay holding every dialog was aria-hidden, so screen readers skipped them.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

function el() {
  const attrs = {};
  return { attrs, inert: false, style: {}, innerHTML: '', classList: { add() {}, remove() {}, contains: () => false },
    setAttribute(k, v) { attrs[k] = v; }, removeAttribute(k) { delete attrs[k]; },
    querySelector: () => null, querySelectorAll: () => [], focus() {}, addEventListener() {}, removeEventListener() {} };
}

test('an open dialog is readable and the page behind it is not', () => {
  const sandbox = loadSandbox(['js/utils.js']);
  const nodes = { app: el(), 'modal-overlay': el(), 'modal-content': el() };
  sandbox.document.getElementById = (id) => nodes[id] || null;
  sandbox.document.removeEventListener = () => {};
  sandbox.App.Utils.showModal('<h2>Pay</h2>');
  assert.equal(nodes['modal-overlay'].attrs['aria-hidden'], undefined);
  assert.equal(nodes['modal-content'].attrs.role, 'dialog');
  assert.equal(nodes.app.inert, true);

  sandbox.App.Utils.hideModal(true);
  assert.equal(nodes.app.inert, false);
  assert.equal(nodes.app.attrs['aria-hidden'], undefined);
});

// Signing out, or a timed-out session, with a popup open left #app inert, so the next sign-in was unclickable.
test('the login screen always hands back a usable page', () => {
  const sandbox = loadSandbox(['js/store.js', 'js/utils.js', 'js/main.js']);
  const nodes = { app: el(), 'modal-overlay': el(), 'modal-content': el(), 'login-screen': el() };
  sandbox.document.getElementById = (id) => nodes[id] || null;
  sandbox.document.removeEventListener = () => {};
  sandbox.App.Utils.showModal('<h2>Create invoice</h2>');
  sandbox.App.Login.show('Signed out');
  assert.equal(nodes.app.inert, false);
});

// Escape or an outside click closed a confirm without answering it, and an action
// guarded until the answer (Issue every draft) stayed locked until a reload.
test('a confirm closed without an answer is a no', async () => {
  const sandbox = loadSandbox(['js/utils.js']);
  const nodes = { app: el(), 'modal-overlay': el(), 'modal-content': el(), };
  const buttons = {};
  sandbox.document.getElementById = (id) => nodes[id] || (buttons[id] = buttons[id] || el());
  sandbox.document.removeEventListener = () => {};
  nodes['modal-content'].classList = { add() {}, remove() {}, contains: () => false };
  const answer = sandbox.App.Utils.showConfirm({ title: 'Issue every draft?', confirmLabel: 'Issue' });
  sandbox.App.Utils.hideModal();
  assert.equal(await answer, false);
});
