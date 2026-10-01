// The developer page reads technical history; every value comes from the server and is escaped.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

const dev = () => loadSandbox(['js/utils.js', 'js/modules/developer.js']).App.Developer;

test('the audit query carries only the filters that are set, encoded', () => {
  const D = dev();
  assert.equal(D._auditQuery({ q: '', action: '', from: '', to: '' }), '');
  assert.equal(D._auditQuery({ q: 'a&b c', action: 'invoice_paid', from: '', to: '2026-10-01' }),
    '?q=a%26b%20c&action=invoice_paid&to=2026-10-01');
});

test('audit rows escape what people typed', () => {
  const html = dev()._auditRowsHtml([{ createdAt: '2026-10-01T10:00:00+08:00', actorEmail: 'x@y.com', action: 'note', entityType: 'student', entityId: '1', detail: '<img src=x onerror=alert(1)>' }]);
  assert.doesNotMatch(html, /<img/);
  assert.match(html, /&lt;img/);
});

test('an empty audit says so', () => {
  assert.match(dev()._auditRowsHtml([]), /Nothing matches/);
});

test('health says plainly when email only reaches the allowlist', () => {
  const html = dev()._healthHtml({ version: 'v1', env: 'production', uptimeSec: 600, dbOK: true, jobsEnabled: true,
    migrations: { count: 70, latest: '0070_invoice_payment_note.sql' }, emailLive: true, emailOnlyTo: 1,
    emailQueue: { pending: 0, sent24h: 3, failed: 2 }, onlinePayments: false });
  assert.match(html, /1 allowed address\(es\) only/);
  assert.match(html, /not configured/);
  assert.match(html, /0070_invoice_payment_note\.sql/);
});

test('failures list both failed emails and stuck jobs, escaped', () => {
  const html = dev()._failuresHtml({ emails: [{ subject: '<b>Invoice</b>', to: 'p@x.com', attempts: 5, lastError: 'not in OUTBOUND_ALLOWLIST', createdAt: '' }], jobs: [] });
  assert.match(html, /&lt;b&gt;Invoice/);
  assert.match(html, /No stuck jobs/);
});

test('only the developer sees the page in the menu', () => {
  const sandbox = loadSandbox(['js/store.js', 'js/utils.js', 'js/main.js']);
  sandbox.location.hostname = 'studyhub.fit';
  sandbox.App.currentRole = 'admin';
  sandbox.App.Api = { currentUser: () => ({ role: 'admin', developer: false }) };
  assert.equal(sandbox.App.hiddenPages().developer, true);
  sandbox.App.Api = { currentUser: () => ({ role: 'admin', developer: true }) };
  assert.equal(sandbox.App.hiddenPages().developer, false);
});
