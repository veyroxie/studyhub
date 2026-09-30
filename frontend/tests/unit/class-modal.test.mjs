// The class popup had no Close and no actions, and the natural way to enrol (open
// the class, add the student) dead-ended.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

function load(role) {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/calendar.js']);
  const cls = { id: 'C1', name: 'Level 3 Maths', day: 'Monday', time: '16:00', endTime: '17:00', teacherIds: [], enrolled: 2, capacity: 6, color: 'blue', classroom: 'R1' };
  const students = [
    { id: 'S1', firstName: 'Aiden', lastName: 'Lim', status: 'Active', enrolledClasses: ['C1'] },
    { id: 'S2', firstName: 'Bella', lastName: 'Ng', status: 'Active', enrolledClasses: ['C9'] },
    { id: 'S3', firstName: 'Carl', lastName: 'Ho', status: 'Inactive', enrolledClasses: [] },
  ];
  let modal = '';
  let submit = null;
  const saves = [];
  sandbox.App.currentRole = role;
  sandbox.App.Store = { get: () => ({ classes: [cls], students, staff: [], feedback: [], scheduleVersions: [], sessionMoves: [], cancelledClasses: [], holidays: [] }) };
  sandbox.App.Utils.showModal = (h) => { modal = h; };
  sandbox.App.Utils.showToast = () => {};
  sandbox.App.Router = { refresh() {} };
  sandbox.App.Students = { saveEnrolment: (id, classes, from) => { saves.push({ id, classes: Array.from(classes), from }); return Promise.resolve(); } };
  sandbox.document.getElementById = (id) => (id === 'add-to-class-form' ? { addEventListener: (t, fn) => { submit = fn; } } : null);
  sandbox.FormData = class { get(k) { return { studentId: 'S2', enrolledFrom: '2026-11-01' }[k]; } };
  return { C: sandbox.App.Calendar, modal: () => modal, saves, submit: () => submit({ preventDefault() {}, target: { querySelector: () => null } }) };
}

test('every role can close the class popup', () => {
  for (const role of ['admin', 'teacher', 'client']) {
    const { C, modal } = load(role);
    C._classModal('C1');
    assert.match(modal(), />Close<\/button>/, role);
  }
});

test('only an admin can add a student from the class', () => {
  const admin = load('admin'); admin.C._classModal('C1');
  const parent = load('client'); parent.C._classModal('C1');
  assert.match(admin.modal(), /Add a student/);
  assert.doesNotMatch(parent.modal(), /Add a student/);
});

test('adding a student offers only active students not already in the class, and keeps their other classes', async () => {
  const { C, modal, saves, submit } = load('admin');
  C._addStudentModal('C1');
  assert.match(modal(), /Bella Ng/);
  assert.doesNotMatch(modal(), /Aiden Lim/, 'already in the class');
  assert.doesNotMatch(modal(), /Carl Ho/, 'inactive');
  await submit();
  assert.deepEqual(saves, [{ id: 'S2', classes: ['C9', 'C1'], from: '2026-11-01' }]);
});
