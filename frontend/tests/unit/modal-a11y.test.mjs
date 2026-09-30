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
