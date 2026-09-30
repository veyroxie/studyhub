// Admin's "Mark All Present" used enrolledClasses, so it checked in a student not due
// to start yet; the teacher's version showed failed saves as checked in.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

function load(failIds = []) {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/attendance.js']);
  const posted = [];
  const toasts = [];
  const today = sandbox.App.Utils.today();
  sandbox.App.Store = { get: () => ({
    students: [
      { id: 'A', enrolledClasses: ['C1'] },
      { id: 'B', enrolledClasses: ['C1'] },
      { id: 'LATER', enrolledClasses: ['C1'] },
      { id: 'DONE', enrolledClasses: ['C1'] },
    ],
    enrollments: [
      { studentId: 'A', classId: 'C1', startedOn: '2026-01-01' },
      { studentId: 'B', classId: 'C1', startedOn: '2026-01-01' },
      { studentId: 'LATER', classId: 'C1', startedOn: '2099-01-01' },
      { studentId: 'DONE', classId: 'C1', startedOn: '2026-01-01' },
    ],
    attendance: [{ personId: 'DONE', classId: 'C1', date: today, checkIn: '16:00', status: 'Present' }],
  }) };
  sandbox.App.Utils.showToast = (m) => toasts.push(m);
  sandbox.App.Router = { refresh() {} };
  sandbox.App.Api = {
    post: (path, body) => { posted.push(body.personId); return failIds.includes(body.personId) ? Promise.reject(new Error('x')) : Promise.resolve({}); },
    loadSnapshot: () => Promise.resolve(),
  };
  sandbox.App.Attendance.focusClass('C1');
  return { A: sandbox.App.Attendance, posted, toasts };
}

test('checks in the roster on screen: not a student who has not started, not one already in', async () => {
  const { A, posted } = load();
  await A._checkAllIn(null);
  assert.deepEqual(Array.from(posted).sort(), ['A', 'B']);
});

test('a save that fails is reported, not shown as checked in', async () => {
  const { A, toasts } = load(['B']);
  await A._checkAllIn(null);
  assert.match(toasts[0], /1 checked in, 1 could not be saved/);
});
