// Editing a draft from Run the month dropped Nadine on the billing page, and she had
// to reopen the review and find her place.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

function load() {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/billing.js']);
  let modal = '';
  const gets = [];
  sandbox.App.Store = { get: () => ({ students: [], invoices: [{ id: 'D1', studentId: 'S', status: 'Draft', type: 'Monthly',
    description: 'Oct', amount: 240, dueDate: '2026-10-07', createdOn: '2026-10-01', lineItems: [] }], pricingPlans: [], pricingCategories: [] }) };
  sandbox.App.Utils.showModal = (h) => { modal = h; };
  sandbox.App.Router = { refresh() {} };
  sandbox.App.Api = { get: (path) => { gets.push(path); return Promise.resolve({ drafts: [], issued: [] }); } };
  sandbox.document.getElementById = (id) => (id === 'edit-invoice-form' ? { addEventListener() {} } : null);
  return { B: sandbox.App.Billing, modal: () => modal, gets };
}

test('an edit opened from the month review cancels back to the review', () => {
  const { B, modal, gets } = load();
  B._editModal('D1', 'monthRun');
  assert.match(modal(), /App\.Billing\._afterEdit\('monthRun'\)/);
  B._afterEdit('monthRun');
  assert.match(gets[0], /^\/api\/billing\/month\?month=/);
});

test('an edit opened from anywhere else closes as before', () => {
  const { B, modal } = load();
  B._editModal('D1');
  assert.doesNotMatch(modal(), /_afterEdit/);
  B._editModal('D1', 'not-a-list');
  assert.doesNotMatch(modal(), /_afterEdit/);
});
