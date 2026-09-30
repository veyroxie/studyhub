import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

const state = {
  students: [
    { id: 'STU001', firstName: 'Zayden', lastName: 'Tan', parentName: 'Mrs Tan', contact: 'tan@example.com', phone: '012-345 6789' },
    { id: 'STU002', firstName: 'Lucy', lastName: '<b>Lim</b>', parentName: 'Mr Lim', contact: 'lim@example.com', phone: '' },
  ],
  invoices: [{ id: 'INV_1', invoiceNo: 'INV-2026-0034', studentId: 'STU001', amount: 230, status: 'Unpaid' }],
  classes: [{ id: 'C1', name: 'Level 3 Maths', day: 'Monday', time: '16:00' }],
};

function load() {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/search.js']);
  return sandbox.App;
}

test('finds a student by name, parent email or a few digits of the phone', () => {
  const { Search } = load();
  assert.equal(Search.find(state, 'zayd')[0].id, 'STU001');
  assert.equal(Search.find(state, 'lim@exa')[0].id, 'STU002');
  assert.equal(Search.find(state, '3456')[0].id, 'STU001');
});

test('finds an invoice by number and a class by name', () => {
  const { Search } = load();
  assert.deepEqual(Array.from(Search.find(state, '0034'), (r) => r.kind), ['invoice']);
  assert.deepEqual(Array.from(Search.find(state, 'maths'), (r) => r.kind), ['class']);
});

test('one letter searches nothing', () => {
  assert.equal(load().Search.find(state, 'z').length, 0);
});

test('results are escaped when drawn', () => {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/search.js']);
  const box = { innerHTML: '' };
  sandbox.App.Store = { get: () => state };
  sandbox.document.getElementById = (id) => (id === 'global-search-results' ? box : null);
  sandbox.App.Search._update('lucy');
  assert.match(box.innerHTML, /&lt;b&gt;Lim&lt;\/b&gt;/);
  assert.doesNotMatch(box.innerHTML, /<b>Lim<\/b>/);
});

// "/" from a button inside a half-filled popup replaced the form with the search box.
test('the shortcut does nothing while a popup is open', () => {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/search.js']);
  let opened = false;
  const shown = { classList: { contains: () => false } };
  sandbox.document.getElementById = (id) => (id === 'modal-overlay' || id === 'app' ? shown : null);
  sandbox.App.Utils.showModal = () => { opened = true; };
  sandbox.App.currentRole = 'admin';
  sandbox.App.Search._onShortcut({ key: '/', target: { tagName: 'BUTTON' }, preventDefault() {} });
  assert.equal(opened, false);
});

test('the shortcut opens search for an admin on a quiet page', () => {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/search.js']);
  let opened = false;
  const overlay = { classList: { contains: (c) => c === 'hidden' } };
  const app = { classList: { contains: () => false } };
  sandbox.document.getElementById = (id) => ({ 'modal-overlay': overlay, app }[id] || null);
  sandbox.App.Utils.showModal = () => { opened = true; };
  sandbox.App.currentRole = 'admin';
  sandbox.App.Search._onShortcut({ key: '/', target: { tagName: 'BODY' }, preventDefault() {} });
  assert.equal(opened, true);
});
