// Parents saw a Pay Online button even with no gateway (it answered with an error),
// and had to find the bank details somewhere else before telling us they had paid.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

async function parentBilling(details) {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/billing.js']);
  const students = [{ id: 'Z', firstName: 'Zayden', lastName: 'Tan', contact: 'tan@example.com' }];
  const invoices = [{ id: 'I1', invoiceNo: 'INV-2026-0001', studentId: 'Z', amount: 230, status: 'Unpaid', type: 'Monthly',
    period: '2026-10', dueDate: '2026-10-07', description: 'Oct', lineItems: [] }];
  let modal = '';
  sandbox.App.currentRole = 'client';
  sandbox.App.clientParent = 'tan@example.com';
  sandbox.App.Store = { get: () => ({ students, invoices }) };
  sandbox.App.Utils.showModal = (h) => { modal = h; };
  sandbox.App.Router = { refresh() {} };
  sandbox.App.Api = { get: () => Promise.resolve(details) };
  const container = { innerHTML: '' };
  sandbox.App.Billing.render(container);
  await new Promise((r) => setTimeout(r, 0));
  sandbox.App.Billing.render(container);
  return { page: container.innerHTML, B: sandbox.App.Billing, modal: () => modal };
}

test('Pay Online is hidden when no gateway is ready', async () => {
  const { page } = await parentBilling({ onlinePayment: false, bankAccountNo: '5647 2672 4699' });
  assert.doesNotMatch(page, /Pay Online/);
});

test('Pay Online shows when the gateway can take and confirm payment', async () => {
  const { page } = await parentBilling({ onlinePayment: true });
  assert.match(page, /Pay Online/);
});

test('the pay popup shows the amount, the account and the reference to use', async () => {
  const { B, modal } = await parentBilling({ onlinePayment: false, bankName: 'Maybank', bankAccountNo: '5647 2672 4699' });
  B._parentSubmitPaid('I1');
  assert.match(modal(), /Transfer RM ?230\.00 to/);
  assert.match(modal(), /5647 2672 4699/);
  assert.match(modal(), /data-copy="564726724699"/);
  assert.match(modal(), /INV-2026-0001<\/strong> in the transfer reference/);
});
