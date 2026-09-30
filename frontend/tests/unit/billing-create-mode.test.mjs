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

function openCreateModal(sandbox, typed, elements = {}) {
  let html = '';
  let form = null;
  sandbox.App.Utils.showModal = (h) => { html = h; form = null; };
  sandbox.document.getElementById = (id) => {
    if (id === 'create-invoice-form') return form || (form = fakeForm(html, typed));
    return elements[id] || null;
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
    sandbox.App.Utils.today = () => '2026-09-20'; // outside the early-bird window unless a test says otherwise
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

  test('Build from catalogue prices the month the invoice is dated in, not the current one', () => {
    const urls = [];
    sandbox.App.Api.get = (url) => { urls.push(url); return Promise.resolve(null); };
    openCreateModal(sandbox, { studentId: 'STU_Z', invoiceDate: '2026-08-15' });
    sandbox.App.Billing._buildFromCatalogueForForm('create-invoice-form');

    assert.equal(urls.length, 1);
    assert.match(urls[0], /month=2026-08/);
  });

  test('a sibling invoice takes RM10 early bird per child, on one line, dated by the invoice date', () => {
    sandbox.document.querySelectorAll = (sel) => sel.includes('sibling-children-checks')
      ? [{ value: 'STU_Z' }, { value: 'STU_L' }] : [];
    const earlyBirdBox = { checked: false };
    const form = openCreateModal(sandbox,
      { parentEmail: 'parent@example.com', amountPerChild: '240', siblingDiscount: '0', invoiceDate: '2026-10-02', dueDate: '2026-10-07' },
      { 'early-bird-cb': earlyBirdBox });
    sandbox.App.Billing._setInvMode('sibling');
    earlyBirdBox.checked = true;
    submit(form);

    assert.equal(posts.length, 1);
    const earlyBird = posts[0].body.lineItems.filter((li) => li.name.startsWith('Early bird'));
    assert.equal(earlyBird.length, 1);
    assert.equal(earlyBird[0].amount, -20);
    assert.equal(posts[0].body.createdOn, '2026-10-02');
  });

  test('inside the window the early bird starts on, and leaves when the type is not Monthly', () => {
    sandbox.App.Utils.today = () => '2026-10-01';
    const form = openCreateModal(sandbox, { studentId: 'STU_Z', type: 'Adhoc' });
    sandbox.App.Billing._addLineItem('reg-fee');
    sandbox.App.Billing._syncEarlyBirdToType('Adhoc');
    submit(form);

    assert.equal(posts.length, 1);
    assert.ok(!posts[0].body.lineItems.some((li) => li.name.startsWith('Early bird')));
  });

  test('switching back to Monthly inside the window puts exactly one early bird back', () => {
    sandbox.App.Utils.today = () => '2026-10-01';
    const form = openCreateModal(sandbox, { studentId: 'STU_Z', type: 'Monthly' });
    sandbox.App.Billing._addLineItem('reg-fee');
    sandbox.App.Billing._syncEarlyBirdToType('Adhoc');
    sandbox.App.Billing._syncEarlyBirdToType('Monthly');
    sandbox.App.Billing._syncEarlyBirdToType('Monthly');
    submit(form);

    const earlyBird = posts[0].body.lineItems.filter((li) => li.name.startsWith('Early bird'));
    assert.equal(earlyBird.length, 1);
  });
});
