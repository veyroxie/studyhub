// "Absent + Replacement" granted credits in one tap, and the rule it depends on
// (parent told us 3 hours before) lived only in a hover tooltip.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

function load(answer) {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/attendance.js']);
  const posts = [];
  let asked = null;
  sandbox.App.currentRole = 'teacher';
  sandbox.App.Store = { get: () => ({
    students: [{ id: 'S1', firstName: 'Aiden', lastName: 'Lim', enrolledClasses: ['C1'] }],
    classes: [{ id: 'C1', name: 'Maths', time: '16:00', endTime: '17:00' }],
    attendance: [],
  }), set() {} };
  sandbox.App.Utils.showConfirm = (opts) => { asked = opts; return Promise.resolve(answer); };
  sandbox.App.Utils.showToast = () => {};
  sandbox.App.Router = { refresh() {} };
  sandbox.App.Api = { post: (path, body) => { posts.push({ path, body }); return Promise.resolve({}); } };
  sandbox.App.Attendance.focusClass('C1');
  return { A: sandbox.App.Attendance, posts, asked: () => asked };
}

test('declining the question grants nothing and marks nothing', async () => {
  const { A, posts, asked } = load(false);
  await A._markAbsentCredit('S1');
  assert.match(asked().message, /Aiden gets 4 replacement credits/);
  assert.match(asked().message, /3 hours before/);
  assert.equal(posts.length, 0);
});

test('confirming records the absence and the credits', async () => {
  const { A, posts } = load(true);
  await A._markAbsentCredit('S1');
  assert.deepEqual(posts.map((p) => p.path), ['/api/attendance', '/api/replacement-credits']);
  assert.equal(posts[1].body.minutes, 4);
});
