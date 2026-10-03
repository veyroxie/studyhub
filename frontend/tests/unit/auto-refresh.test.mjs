// A tab left open across a deploy refreshes itself at a safe moment, never mid-popup.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

function sandboxAt(version, popupOpen) {
  const sandbox = loadSandbox(['js/update.js']);
  let reloads = 0;
  sandbox.__appVersion = version;
  sandbox.location.reload = () => { reloads++; };
  sandbox.document.getElementById = (id) => (id === 'modal-overlay' ? { classList: { contains: (c) => c === 'hidden' ? !popupOpen : false } } : null);
  sandbox.document.createElement = () => ({ style: {}, setAttribute() {}, querySelector: () => ({ addEventListener() {} }) });
  sandbox.document.body.appendChild = () => {};
  return { U: sandbox.App.Update, reloads: () => reloads };
}

test('the same version changes nothing', () => {
  const { U } = sandboxAt('abc123', false);
  U.check('abc123');
  assert.equal(U.isPending(), false);
});

test('a new deploy is noticed and refreshes at the next safe moment', () => {
  const { U, reloads } = sandboxAt('abc123', false);
  U.check('def456');
  assert.equal(U.isPending(), true);
  assert.equal(U.refreshIfPending(), true);
  assert.equal(reloads(), 1);
});

test('an open popup holds the refresh, so typing is never lost', () => {
  const { U, reloads } = sandboxAt('abc123', true);
  U.check('def456');
  assert.equal(U.refreshIfPending(), false);
  assert.equal(reloads(), 0);
});

test('a page opened outside the server (no version filled in) never refreshes', () => {
  const { U } = sandboxAt('__APP_VERSION__', false);
  U.check('def456');
  assert.equal(U.isPending(), false);
});
