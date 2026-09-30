// Nadine opened Create Invoice after using the Sibling tab once, saw Single
// selected, picked Zayden, and was told "Select a family". The tab on screen and
// the mode the submit handler used had come apart.
import { test, describe, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

const SIBLINGS = [
  { id: 'STU_Z', firstName: 'Zayden', lastName: 'A', contact: 'parent@example.com', parentName: 'P', status: 'Active' },
  { id: 'STU_L', firstName: 'Lucy', lastName: 'A', contact: 'parent@example.com', parentName: 'P', status: 'Active' },
];

// A form stand-in that reads its initial field values out of the modal HTML,
// so the test sees exactly what the rendered popup would submit.
function fakeForm(html, typed) {
  const values = {};
  for (const m of html.matchAll(/<input type="hidden" name="(\w+)" value="(\w*)">/g)) values[m[1]] = m[2];
  Object.assign(values, typed);
  const listeners = {};
  return {
    values,
    listeners,
    addEventListener(type, fn) { listeners[type] = fn; },
    querySelector(sel) {
      const name = (sel.match(/\[name="(\w+)"\]/) || [])[1];
      if (!name) return null;
      return { get value() { return values[name] || ''; }, set value(v) { values[name] = v; } };
    },
  };
}

function openCreateModal(sandbox, typed) {
  let html = '';
  let form = null;
  sandbox.App.Utils.showModal = (h) => { html = h; form = null; };
  sandbox.document.getElementById = (id) => {
    if (id === 'create-invoice-form') return form || (form = fakeForm(html, typed));
    return null;
  };
  sandbox.App.Billing._createModal();
  return sandbox.document.getElementById('create-invoice-form');
}

function submit(form) {
  form.listeners.submit({ preventDefault() {}, target: form });
}

describe('create invoice popup: the tab on screen is the tab that submits', () => {
  let sandbox;
  let toasts;
  let posts;

  beforeEach(() => {
    sandbox = loadSandbox(['js/utils.js', 'js/modules/billing.js']);
    toasts = [];
    posts = [];
    sandbox.FormData = class { constructor(form) { this.form = form; } get(k) { return this.form.values[k] ?? null; } };
    sandbox.App.Store = { get: () => ({ students: SIBLINGS, invoices: [], pricingPlans: [], pricingCategories: [] }) };
    sandbox.App.Utils.showToast = (msg) => toasts.push(msg);
    sandbox.App.Api = { post: (path, body) => { posts.push({ path, body }); return Promise.resolve({}); }, loadSnapshot: () => Promise.resolve() };
    sandbox.App.Router = { refresh() {} };
    sandbox.confirm = () => true;
  });

  test('a single invoice opened after the Sibling tab was used still submits as single', () => {
    openCreateModal(sandbox, {});
    sandbox.App.Billing._setInvMode('sibling');
    sandbox.App.Utils.hideModal = () => {};

    const form = openCreateModal(sandbox, { studentId: 'STU_Z', type: 'Adhoc' });
    sandbox.App.Billing._addLineItem('reg-fee');
    submit(form);

    assert.ok(!toasts.includes('Select a family'), 'toasts: ' + JSON.stringify(toasts));
    assert.equal(posts.length, 1);
    assert.equal(posts[0].body.studentId, 'STU_Z');
  });

  test('a single invoice opened after the Self-Study tab was used still submits as single', () => {
    openCreateModal(sandbox, {});
    sandbox.App.Billing._setInvMode('selfstudy');

    const form = openCreateModal(sandbox, { studentId: 'STU_L', type: 'Adhoc' });
    sandbox.App.Billing._addLineItem('reg-fee');
    submit(form);

    assert.ok(!toasts.includes('Select a student'), 'toasts: ' + JSON.stringify(toasts));
    assert.equal(posts.length, 1);
    assert.equal(posts[0].body.studentId, 'STU_L');
  });

  test('choosing the Sibling tab in the open popup does route to the sibling invoice', () => {
    const form = openCreateModal(sandbox, {});
    sandbox.App.Billing._setInvMode('sibling');
    submit(form);

    assert.ok(toasts.includes('Select a family'), 'toasts: ' + JSON.stringify(toasts));
    assert.equal(posts.length, 0);
  });
});
