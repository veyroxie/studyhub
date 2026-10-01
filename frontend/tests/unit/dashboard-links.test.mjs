// A dashboard link used to land on the page's default view: "3 payments awaiting
// verification" opened the Unpaid tab, where none of them are.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

function sandboxWith() {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/absence.js', 'js/modules/dashboard.js']);
  const calls = [];
  sandbox.App.Router = { navigate: (p) => calls.push(['navigate', p]) };
  sandbox.App.Billing = { focus: (f) => calls.push(['billing', f]) };
  sandbox.App.Attendance = { focusClass: (id) => calls.push(['attendance', id]) };
  return { D: sandbox.App.Dashboard, calls };
}

test('a billing link opens the tab it was about, before the page renders', () => {
  const { D, calls } = sandboxWith();
  D._open('billing', 'Pending');
  assert.deepEqual(calls.map((c) => c.join(':')), ['billing:Pending', 'navigate:billing']);
});

test('Mark on a class opens check-in on that class', () => {
  const { D, calls } = sandboxWith();
  D._open('attendance', 'CLS_1');
  assert.deepEqual(calls.map((c) => c.join(':')), ['attendance:CLS_1', 'navigate:attendance']);
});

test('a plain link just navigates', () => {
  const { D, calls } = sandboxWith();
  D._open('staff', '');
  assert.deepEqual(calls.map((c) => c.join(':')), ['navigate:staff']);
});
