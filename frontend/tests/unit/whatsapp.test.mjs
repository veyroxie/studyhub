import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadApp } from './_load.mjs';

const U = loadApp(['js/utils.js']).Utils;

test('a Malaysian number as typed becomes WhatsApp digits', () => {
  assert.equal(U.msisdn('012-345 6789'), '60123456789');
  assert.equal(U.msisdn('+60 12 345 6789'), '60123456789');
  assert.equal(U.msisdn('60123456789'), '60123456789');
  assert.equal(U.msisdn('011-2862 0038'), '601128620038');
});

test('an unusable number gives nothing rather than a wrong chat', () => {
  assert.equal(U.msisdn(''), '');
  assert.equal(U.msisdn(null), '');
  assert.equal(U.msisdn('12345'), '');
});

test('the link carries the message, and without a number lets WhatsApp ask', () => {
  assert.equal(U.whatsAppLink('0123456789', 'Hi & bye'), 'https://wa.me/60123456789?text=Hi%20%26%20bye');
  assert.equal(U.whatsAppLink('', 'Hi'), 'https://wa.me/?text=Hi');
});

import { loadSandbox } from './_load.mjs';

function billing(invoices, students) {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/billing.js']);
  sandbox.App.Store = { get: () => ({ students, invoices }) };
  sandbox.window.__brandName = 'The Study Hub';
  sandbox.window.location = { origin: 'https://studyhub.fit' };
  return sandbox.App.Billing;
}

const kids = [
  { id: 'Z', firstName: 'Zayden', parentName: 'Mrs Tan', phone: '012-345 6789', contact: 'tan@example.com' },
  { id: 'L', firstName: 'Lucy', parentName: 'Mrs Tan', phone: '012-345 6789', contact: 'tan@example.com' },
];
const inv = (id, studentId, amount, extra = {}) => ({ id, studentId, amount, status: 'Unpaid', type: 'Monthly', period: '2026-10',
  dueDate: '2026-10-07', description: 'October tuition', invoiceNo: 'INV-2026-00' + id, lineItems: [], ...extra });

test('a family bill message lists each child and one total, in plain text', () => {
  const B = billing([inv('1', 'Z', 230), inv('2', 'L', 250)], kids);
  const msg = B._whatsAppMessage('bill:1');
  assert.equal(msg.phone, '012-345 6789');
  assert.match(msg.text, /^Hi Mrs Tan,/);
  assert.match(msg.text, /- Zayden: RM ?230\.00/);
  assert.match(msg.text, /- Lucy: RM ?250\.00/);
  assert.match(msg.text, /Total: RM ?480\.00/);
  assert.match(msg.text, /https:\/\/studyhub\.fit\/#billing/);
  assert.doesNotMatch(msg.text, /[—–]/, 'no em or en dashes');
});

test('the early bird deadline is mentioned only when the invoice carries it', () => {
  const eb = { lineItems: [{ kind: 'discount', name: 'Early bird discount', amount: -10 }] };
  assert.match(billing([inv('1', 'Z', 230, eb)], kids)._whatsAppMessage('1').text, /Pay by the 7th/);
  assert.doesNotMatch(billing([inv('1', 'Z', 230)], kids)._whatsAppMessage('1').text, /Pay by/);
});

test('a single invoice names the month, never the em-dashed description the cron writes', () => {
  const B = billing([inv('1', 'Z', 230, { description: 'Monthly tuition — Oct 2026 — Zayden Tan' })], kids);
  const text = B._whatsAppMessage('1').text;
  assert.match(text, /October 2026 tuition/);
  assert.doesNotMatch(text, /[—–]/);
});
