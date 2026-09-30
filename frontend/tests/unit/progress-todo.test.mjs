// Reports were written one student at a time with nothing showing who was left,
// and a teacher's new report defaulted to no teacher.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

function teacherView(reports) {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/progress.js']);
  let modal = '';
  sandbox.App.currentRole = 'teacher';
  sandbox.App.currentTeacher = 'T1';
  sandbox.App.Store = { get: () => ({
    students: [
      { id: 'A', firstName: 'Aiden', lastName: 'Lim', status: 'Active', enrolledClasses: ['C1'] },
      { id: 'B', firstName: 'Bella', lastName: 'Ng', status: 'Active', enrolledClasses: ['C1'] },
      { id: 'X', firstName: 'Xavier', lastName: 'Other', status: 'Active', enrolledClasses: ['C9'] },
    ],
    classes: [{ id: 'C1', teacherIds: ['T1'] }, { id: 'C9', teacherIds: ['T2'] }],
    staff: [{ id: 'T1', fullName: 'Chiying' }, { id: 'T2', fullName: 'Other' }],
    progressReports: reports,
  }) };
  sandbox.App.Utils.showModal = (h) => { modal = h; };
  sandbox.document.getElementById = () => ({ addEventListener() {} });
  const container = { innerHTML: '' };
  sandbox.App.Progress.render(container);
  return { html: container.innerHTML, P: sandbox.App.Progress, modal: () => modal, term: (r) => r };
}

test('the list shows this teacher\'s students still without a report this term, and no one else\'s', () => {
  const first = teacherView([]);
  const term = first.html.match(/Still to write for ([^(]+)\(/);
  assert.ok(term, 'list is shown');
  assert.match(first.html, /Aiden Lim/);
  assert.match(first.html, /Bella Ng/);
  assert.doesNotMatch(first.html, /Xavier Other/);
});

test('a new report from the list is for that student, by this teacher', () => {
  const { P, modal } = teacherView([]);
  P._newModal('B');
  assert.match(modal(), /<option value="B" selected>/);
  assert.match(modal(), /<option value="T1" selected>/);
});
