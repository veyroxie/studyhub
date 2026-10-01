// Parents report an absence; staff approve or decline. The server decides timing and credit.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

const A = () => loadSandbox(['js/utils.js', 'js/modules/absence.js']).App.Absence;
const students = [{ id: 'STU001', firstName: '<b>Eita</b>', lastName: 'S' }];
const classes = [{ id: 'C1', name: 'Level 3' }];

test('a session already reported cannot be picked again, and a late one says it earns nothing', () => {
  const html = A()._sessionOptionsHtml([
    { classId: 'C1', className: 'Level 3', date: '2026-10-08', time: '16:00', inTime: true, reported: 'pending' },
    { classId: 'C1', className: 'Level 3', date: '2026-10-01', time: '16:00', inTime: false },
  ]);
  assert.match(html, /value="C1\|2026-10-08" disabled/);
  assert.match(html, /Waiting for approval/);
  assert.match(html, /no make-up credit/);
});

test('staff see only what is waiting for them, escaped', () => {
  const html = A().reviewHtml([
    { id: 'ABS1', studentId: 'STU001', classId: 'C1', sessionDate: '2026-10-08', sessionTime: '16:00', status: 'pending', reason: '<i>dentist</i>' },
    { id: 'ABS2', studentId: 'STU001', classId: 'C1', sessionDate: '2026-10-01', sessionTime: '16:00', status: 'approved' },
  ], students, classes);
  assert.match(html, /Absence reports to review \(1\)/);
  assert.match(html, /_approve\('ABS1'/);
  assert.doesNotMatch(html, /ABS2/);
  assert.doesNotMatch(html, /<b>Eita|<i>dentist/);
});

test('nothing waiting means no review card at all', () => {
  assert.equal(A().reviewHtml([], students, classes), '');
});

test('a parent sees each report and why a credit was declined', () => {
  const html = A().parentListHtml([
    { id: 'ABS3', studentId: 'STU001', classId: 'C1', sessionDate: '2026-10-08', sessionTime: '16:00', status: 'declined', decisionNote: 'Make-up already booked' },
  ], students, classes);
  assert.match(html, /No make-up credit/);
  assert.match(html, /Make-up already booked/);
});

test('declining asks for a reason before anything is sent', () => {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/absence.js']);
  const posts = [];
  sandbox.App.Api = { post: (p, b) => { posts.push(b); return Promise.resolve({}); } };
  sandbox.App.Utils.showToast = () => {};
  sandbox.document.getElementById = () => ({ value: '  ', focus() {} });
  sandbox.App.Absence._confirmDecline('ABS1', null);
  assert.equal(posts.length, 0);
});
