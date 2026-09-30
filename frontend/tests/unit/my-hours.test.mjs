import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

test('a teacher sees this month\'s hours and last month\'s pay on their dashboard', async () => {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/dashboard.js']);
  const body = { innerHTML: '' };
  const asked = [];
  sandbox.App.currentRole = 'teacher';
  sandbox.App.currentTeacher = 'T1';
  sandbox.App.Store = { get: () => ({
    staff: [{ id: 'T1', fullName: 'Chiying' }],
    classes: [{ id: 'C1', name: 'Maths', day: 'Monday', time: '16:00', endTime: '17:00', teacherIds: ['T1'], enrolled: 1, capacity: 6 }],
    students: [], attendance: [], announcements: [], scheduleVersions: [], sessionMoves: [], cancelledClasses: [], holidays: [],
  }) };
  sandbox.App.Api = { get: (path) => { asked.push(path); return Promise.resolve(path.endsWith(sandbox.App.Utils.today().slice(0, 7))
    ? { hours: 12.25 } : { hours: 40, pay: { total: 1200, status: 'Paid' } }); } };
  sandbox.document.querySelector = (sel) => (sel === '#my-hours-card .dash-card-body' ? body : null);
  const container = { innerHTML: '' };
  sandbox.App.Dashboard.render(container);
  assert.match(container.innerHTML, /id="my-hours-card"/);
  await new Promise((r) => setTimeout(r, 0));
  assert.equal(asked.length, 2);
  assert.match(body.innerHTML, /12\.3 h/);
  assert.match(body.innerHTML, /RM ?1200\.00 \(Paid\)/);
});
