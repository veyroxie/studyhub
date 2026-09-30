// One parent's issued Monthly invoices for one month are paid as one family bill.
// The grouping here must match store.FamilyBillMembers, or the server refuses the bill.
import { test, describe, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

const PARENT = 'parent@example.com';
const students = [
  { id: 'STU_Z', firstName: 'Zayden', contact: PARENT },
  { id: 'STU_L', firstName: 'Lucy', contact: PARENT },
  { id: 'STU_O', firstName: 'Other', contact: 'other@example.com' },
];
const monthly = (id, studentId, amount, status = 'Unpaid', period = '2026-10') =>
  ({ id, studentId, amount, status, period, type: 'Monthly', dueDate: period + '-07', description: 'Oct' });

describe('family bill grouping', () => {
  const B = loadSandbox(['js/utils.js', 'js/modules/billing.js']).App.Billing;

  test('two children of one parent in one month make one bill', () => {
    const bills = B._familyBills([monthly('I1', 'STU_Z', 230), monthly('I2', 'STU_L', 250)], students);
    assert.equal(bills.length, 1);
    assert.deepEqual(Array.from(bills[0].members, (m) => m.id), ['I1', 'I2']);
    assert.equal(bills[0].contact, PARENT);
  });

  test('a single child is not a family bill', () => {
    assert.equal(B._familyBills([monthly('I1', 'STU_Z', 230), monthly('I3', 'STU_O', 230)], students).length, 0);
  });

  test('drafts and voids stay out, as they do on the server', () => {
    const bills = B._familyBills([monthly('I1', 'STU_Z', 230), monthly('I2', 'STU_L', 250, 'Draft'), monthly('I4', 'STU_L', 250, 'Void')], students);
    assert.equal(bills.length, 0);
  });

  test('different months are different bills, and non-monthly invoices never join', () => {
    const bills = B._familyBills([
      monthly('I1', 'STU_Z', 230), monthly('I2', 'STU_L', 250),
      monthly('I5', 'STU_Z', 230, 'Unpaid', '2026-11'), { ...monthly('I6', 'STU_L', 250, 'Unpaid', '2026-11'), type: 'Adhoc' },
    ], students);
    assert.equal(bills.length, 1);
    assert.equal(bills[0].period, '2026-10');
  });
});

describe('paying a family bill', () => {
  let sandbox;
  let posts;
  beforeEach(() => {
    sandbox = loadSandbox(['js/utils.js', 'js/modules/billing.js']);
    posts = [];
    sandbox.App.Store = { get: () => ({ students, invoices: [
      monthly('I1', 'STU_Z', 230), monthly('I2', 'STU_L', 250), monthly('I7', 'STU_L', 999, 'Paid', '2026-10'),
    ] }) };
    sandbox.App.Utils.hideModal = () => {};
    sandbox.App.Utils.showToast = () => {};
    sandbox.App.Router = { refresh() {} };
    sandbox.App.Api = {
      post: (path, body) => { posts.push({ path, body }); return Promise.resolve({}); },
      put: () => { throw new Error('a bill must not fall back to one-invoice PUTs'); },
      loadSnapshot: () => Promise.resolve(),
    };
  });

  test('the claim carries every outstanding child and the total shown, and skips what is already paid', () => {
    sandbox.App.Billing._parentConfirmSubmit('bill:I1', 'Bank Transfer', 'MBB1', 'uploads/proof_I1_1.png');
    assert.equal(posts.length, 1);
    const { path, body } = posts[0];
    assert.equal(path, '/api/family-bills/pay');
    assert.deepEqual(Array.from(body.invoiceIds), ['I1', 'I2']);
    assert.equal(body.expectedTotal, 480);
    assert.equal(body.period, '2026-10');
    assert.equal(body.status, 'Pending Verification');
    assert.equal(body.paymentProof, 'uploads/proof_I1_1.png');
  });
});

describe('admin actions on a family bill', () => {
  let sandbox;
  let posts;
  let toasts;
  let typed;
  const load = (invoices) => {
    sandbox = loadSandbox(['js/utils.js', 'js/modules/billing.js']);
    posts = [];
    toasts = [];
    sandbox.App.Store = { get: () => ({ students, invoices, referralRewards: [] }) };
    sandbox.App.Utils.hideModal = () => {};
    sandbox.App.Utils.showToast = (msg) => toasts.push(msg);
    sandbox.App.Notifs = { refresh() {} };
    sandbox.App.Router = { refresh() {} };
    sandbox.App.Api = {
      post: (path, body) => { posts.push({ path, body }); return Promise.resolve({}); },
      put: () => { throw new Error('a bill must not fall back to one-invoice PUTs'); },
      loadSnapshot: () => Promise.resolve(),
    };
    sandbox.document.getElementById = (id) => (id === 'cash-confirm-amount' ? { value: typed, focus() {} } : null);
  };

  test('cash for a family bill must match the bill total, not one child', () => {
    load([monthly('I1', 'STU_Z', 230), monthly('I2', 'STU_L', 250)]);
    typed = '230';
    sandbox.App.Billing._confirmCashSubmit('bill:I1');
    assert.equal(posts.length, 0);
    typed = '480';
    sandbox.App.Billing._confirmCashSubmit('bill:I1');
    assert.equal(posts.length, 1);
    assert.equal(posts[0].body.status, 'Paid');
    assert.equal(posts[0].body.expectedTotal, 480);
    assert.equal(posts[0].body.parentEmail, PARENT);
  });

  test('rejecting a family bill reopens only the claimed invoices', () => {
    load([monthly('I1', 'STU_Z', 230, 'Pending Verification'), monthly('I2', 'STU_L', 250, 'Unpaid')]);
    sandbox.App.Billing._markUnpaid('bill:I1');
    assert.equal(posts.length, 1);
    assert.deepEqual(Array.from(posts[0].body.invoiceIds), ['I1']);
    assert.equal(posts[0].body.expectedTotal, 230);
  });
});

describe('what admin is shown before acting on a family bill', () => {
  let sandbox;
  let html;
  const load = (invoices, extraStudents = []) => {
    sandbox = loadSandbox(['js/utils.js', 'js/modules/billing.js']);
    html = '';
    sandbox.App.Store = { get: () => ({ students: students.concat(extraStudents), invoices }) };
    sandbox.App.Utils.showModal = (h) => { html = h; };
    sandbox.App.Utils.hideModal = () => {};
  };

  test('verifying a bill names a sibling the parent never claimed', () => {
    load([monthly('I1', 'STU_Z', 230, 'Pending Verification'), monthly('I2', 'STU_L', 250, 'Unpaid')]);
    sandbox.App.Billing._verifyPaid('bill:I1');
    assert.match(html, /Not claimed by the parent: Lucy/);
    assert.match(html, /Confirm all, including unclaimed/);
  });

  test('a fully claimed bill verifies without the warning', () => {
    load([monthly('I1', 'STU_Z', 230, 'Pending Verification'), monthly('I2', 'STU_L', 250, 'Pending Verification')]);
    sandbox.App.Billing._verifyPaid('bill:I1');
    assert.doesNotMatch(html, /Not claimed/);
  });

  test('the family review names a child the monthly run did not draft', () => {
    const frozen = { id: 'STU_F', firstName: 'Mia', lastName: 'Tan', contact: PARENT };
    load([], [frozen]);
    sandbox.App.Billing._familyDraftReview(PARENT, '2026-10', [
      { invoiceId: 'D1', studentId: 'STU_Z', studentName: 'Zayden Tan', amount: 230 },
      { invoiceId: 'D2', studentId: 'STU_L', studentName: 'Lucy Tan', amount: 250 },
    ]);
    assert.match(html, /Mia Tan: not drafted/);
  });
});

describe('emailing a parent from the admin menu', () => {
  const load = (reply) => {
    const sandbox = loadSandbox(['js/utils.js', 'js/modules/billing.js']);
    const calls = { posts: [], toasts: [] };
    sandbox.App.Utils.showConfirm = () => Promise.resolve(true);
    sandbox.App.Utils.showToast = (msg) => calls.toasts.push(msg);
    sandbox.App.Api = { post: (path) => { calls.posts.push(path); return Promise.resolve(reply); } };
    return { B: sandbox.App.Billing, calls };
  };

  test('a family bill key sends the whole bill', async () => {
    const { B, calls } = load({ queued: true, to: PARENT });
    await B._emailParent('bill:I1');
    assert.deepEqual(Array.from(calls.posts), ['/api/family-bills/I1/email']);
    assert.match(calls.toasts[0], /Sending to/);
  });

  test('an allowlist drop is reported as not sent, never as sent', async () => {
    const { B, calls } = load({ queued: false, to: PARENT, reason: 'outbound email is restricted to the allowlist' });
    await B._emailParent('I1');
    assert.deepEqual(Array.from(calls.posts), ['/api/invoices/I1/email']);
    assert.match(calls.toasts[0], /^Not sent: outbound email is restricted/);
  });
});
