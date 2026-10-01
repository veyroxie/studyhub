// A parent with one child already enrolled had no way to request a second:
// the form only rendered for a parent with no children at all.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

test('a parent with a child can request enrolment for another, through the same form', async () => {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/absence.js', 'js/modules/dashboard.js']);
  let html = '';
  let listener = null;
  let hidden = false;
  const posts = [];
  sandbox.App.clientParent = 'parent@example.com';
  sandbox.App.Store = { get: () => ({ registrations: [
    { type: 'enrollment', status: 'pending', email: 'parent@example.com', studentFirstName: 'Mia', submittedOn: '2026-09-29' },
  ] }) };
  sandbox.App.Utils.showModal = (h) => { html = h; };
  sandbox.App.Utils.hideModal = () => { hidden = true; };
  sandbox.App.Utils.showToast = () => {};
  sandbox.App.Router = { refresh() {} };
  sandbox.App.Api = { post: (path, body) => { posts.push({ path, body }); return Promise.resolve({}); }, loadSnapshot: () => Promise.resolve() };
  const button = { disabled: false, textContent: '' };
  sandbox.document.getElementById = (id) => {
    if (id === 'enroll-child-form') return { addEventListener: (type, fn) => { listener = fn; } };
    if (id === 'enroll-submit-btn') return button;
    return null;
  };
  sandbox.FormData = class { constructor() {} forEach(fn) { fn('Kaho', 'studentFirstName'); fn(' sh-ab12 ', 'referralCode'); } };

  sandbox.App.Dashboard._enrollChildModal();
  assert.match(html, /Enrol another child/);
  assert.match(html, /id="enroll-child-form"/);
  assert.match(html, /Pending enrolments[\s\S]*Mia/, 'an earlier request is still shown');

  await listener({ preventDefault() {}, target: {} });
  assert.equal(posts.length, 1);
  assert.equal(posts[0].path, '/api/enrollment-requests');
  assert.equal(posts[0].body.studentFirstName, 'Kaho');
  assert.equal(posts[0].body.referralCode, 'SH-AB12');
  assert.equal(hidden, true, 'the popup closes once the request is in');
});
