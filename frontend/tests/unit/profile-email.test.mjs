import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

function wiredProfile(fields) {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/profile.js']);
  const calls = { posts: [], toasts: [], signedOut: 0 };
  let html = '';
  let listener = null;
  const container = { set innerHTML(h) { html = h; }, get innerHTML() { return html; } };
  sandbox.App.currentRole = 'client';
  sandbox.App.Store = { get: () => ({ students: [] }) };
  sandbox.App.Utils.showToast = (msg) => calls.toasts.push(msg);
  sandbox.App.signOut = () => { calls.signedOut++; };
  sandbox.App.Api = {
    get: () => Promise.resolve({ email: 'old@example.com', name: 'P' }),
    post: (path, body) => { calls.posts.push({ path, body }); return Promise.resolve({}); },
  };
  sandbox.document.getElementById = (id) => (id === 'pf-email-form' ? { addEventListener: (t, fn) => { listener = fn; } } : null);
  sandbox.document.querySelectorAll = () => [];
  sandbox.FormData = class { get(k) { return fields[k]; } };
  return { sandbox, calls, container, submit: () => listener({ preventDefault() {}, target: {} }), html: () => html };
}

test('changing email sends it with the password, then signs the person out', async () => {
  const p = wiredProfile({ newEmail: 'new@example.com', confirmEmail: 'NEW@example.com ', currentPassword: 'pw' });
  await p.sandbox.App.Profile.render(p.container);
  assert.match(p.html(), /id="pf-email-form"/);
  await p.submit();
  assert.equal(p.calls.posts[0].path, '/api/auth/change-email');
  assert.equal(p.calls.posts[0].body.newEmail, 'new@example.com');
  assert.equal(p.calls.signedOut, 1);
});

test('two different emails are refused before anything is sent', async () => {
  const p = wiredProfile({ newEmail: 'new@example.com', confirmEmail: 'nwe@example.com', currentPassword: 'pw' });
  await p.sandbox.App.Profile.render(p.container);
  await p.submit();
  assert.equal(p.calls.posts.length, 0);
  assert.equal(p.calls.signedOut, 0);
});
