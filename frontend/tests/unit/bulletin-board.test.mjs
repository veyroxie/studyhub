// The bulletin board is the centre's standing policies, on every dashboard, edited by admins only.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadApp, loadSandbox } from './_load.mjs';

const today = '2026-10-01';
const anns = [
  { id: 'A1', title: 'Late pickup', message: 'RM10 per 15 min', pinned: true, status: 'published', createdOn: '2026-01-05', updatedOn: '2026-09-20' },
  { id: 'A2', title: 'Make-up classes', message: 'Within 30 days', pinned: true, status: 'published', createdOn: '2026-03-01', updatedOn: '2026-03-01' },
  { id: 'A3', title: 'Holiday notice', message: 'Closed Monday', pinned: false, status: 'published', createdOn: '2026-09-29' },
  { id: 'A4', title: 'Draft rule', message: 'x', pinned: true, status: 'pending_approval', createdOn: '2026-09-30' },
  { id: 'A5', title: 'Old rule', message: 'x', pinned: true, status: 'published', createdOn: '2026-01-01', archiveOn: '2026-06-30' },
];

test('the board holds published, pinned, unexpired policies, latest change first', () => {
  const items = loadApp().Utils.boardItems(anns, today);
  assert.deepEqual(Array.from(items, (a) => a.id), ['A1', 'A2']);
});

test('the board shows when each policy last changed and escapes it', () => {
  const D = loadSandbox(['js/utils.js', 'js/modules/absence.js', 'js/modules/dashboard.js']).App.Dashboard;
  const html = D._bulletinBoardHtml([{ id: 'A1', title: '<b>Late</b>', message: 'x', updatedOn: '2026-09-20', createdOn: '2026-01-05' }], false);
  assert.match(html, /Bulletin board/);
  assert.match(html, /&lt;b&gt;Late/);
  assert.doesNotMatch(html, /_editModal/);
});

test('only an admin gets an Edit link, and an empty board is invisible to everyone else', () => {
  const D = loadSandbox(['js/utils.js', 'js/modules/absence.js', 'js/modules/dashboard.js']).App.Dashboard;
  assert.match(D._bulletinBoardHtml([{ id: 'A1', title: 't', message: 'm', createdOn: today }], true), /_editModal\('A1'\)/);
  assert.equal(D._bulletinBoardHtml([], false), '');
});

function composeAs(role, checked) {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/communication.js']);
  const posts = [];
  let submit = null;
  let html = '';
  sandbox.App.currentRole = role;
  sandbox.App.Store = { get: () => ({ staff: [], classes: [], announcements: [] }) };
  sandbox.App.Utils.showModal = (h) => { html = h; };
  sandbox.App.Api = { post: (p, body) => { posts.push(body); return new Promise(() => {}); } };
  sandbox.document.getElementById = (id) => (id === 'ann-form' ? { addEventListener: (t, fn) => { submit = fn; } } : null);
  const fields = { title: 'T', message: 'M', audience: 'parents', type: 'Notice', archiveOn: '', boardPolicy: checked ? 'on' : null };
  sandbox.FormData = class { get(k) { return fields[k]; } };
  sandbox.App.Communication._newModal();
  submit({ preventDefault() {}, target: {} });
  return { html, body: posts[0] };
}

test('an admin can put a new post on the board', () => {
  const { html, body } = composeAs('admin', true);
  assert.match(html, /Bulletin board policy/);
  assert.equal(body.pinned, true);
  assert.equal(body.category, 'policy');
});

test('a teacher is never offered the board and sends no pin', () => {
  const { html, body } = composeAs('teacher', true);
  assert.doesNotMatch(html, /Bulletin board policy/);
  assert.equal(body.pinned, undefined);
});
