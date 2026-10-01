// The setup checklist lived inside the parent dashboard behind an admin check,
// so the admins it was written for never saw it.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

function renderAs(role, storage = {}) {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/absence.js', 'js/modules/dashboard.js']);
  sandbox.App.currentRole = role;
  sandbox.App.Store = { get: () => ({ students: [], classes: [], invoices: [], staff: [], attendance: [], announcements: [], registrations: [] }) };
  sandbox.localStorage = { getItem: (k) => storage[k] || null, setItem() {} };
  sandbox.window.localStorage = sandbox.localStorage;
  const container = { innerHTML: '' };
  sandbox.App.Dashboard.render(container);
  return container.innerHTML;
}

test('an admin sees the centre setup checklist', () => {
  assert.match(renderAs('admin'), /Set up your centre/);
});

test('a dismissed checklist stays dismissed', () => {
  assert.doesNotMatch(renderAs('admin', { sh_checklist_done: '1' }), /Set up your centre/);
});

test('a checklist survives storage that throws, as in a private window', () => {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/absence.js', 'js/modules/dashboard.js']);
  sandbox.App.currentRole = 'admin';
  sandbox.App.Store = { get: () => ({}) };
  sandbox.localStorage = { getItem() { throw new Error('denied'); }, setItem() { throw new Error('denied'); } };
  sandbox.window.localStorage = sandbox.localStorage;
  const container = { innerHTML: '' };
  sandbox.App.Dashboard.render(container);
  assert.match(container.innerHTML, /Set up your centre/);
});
