// Moved-in sessions skipped the schedule's filters, so a parent could see another
// class's rescheduled session; and a day holding only a moved-in session read as empty.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

const DAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];

function renderParentWeek({ ownClassToday }) {
  const sandbox = loadSandbox(['js/utils.js', 'js/modules/calendar.js']);
  const today = sandbox.App.Utils.today();
  const todayName = DAYS[new Date(today + 'T00:00:00').getDay()];
  const otherDay = DAYS[(new Date(today + 'T00:00:00').getDay() + 3) % 7];
  const cls = (id, name, day) => ({ id, name, day, time: '16:00', endTime: '17:00', teacherIds: [], enrolled: 2, capacity: 6, type: 'Group' });
  const classes = [cls('MINE', 'Zayden Maths', otherDay), cls('OTHER', 'Someone Else Science', otherDay)];
  const own = ['MINE'];
  if (ownClassToday) { classes.push(cls('TODAY', 'Zayden Art', todayName)); own.push('TODAY'); }
  sandbox.App.currentRole = 'client';
  sandbox.App.clientParent = 'tan@example.com';
  sandbox.App.Store = { get: () => ({
    classes,
    students: [{ id: 'Z', firstName: 'Zayden', contact: 'tan@example.com', enrolledClasses: own }],
    staff: [], cancelledClasses: [], holidays: [], scheduleVersions: [],
    sessionMoves: [
      { classId: 'MINE', fromDate: '2000-01-01', toDate: today },
      { classId: 'OTHER', fromDate: '2000-01-01', toDate: today },
    ],
  }) };
  const container = { innerHTML: '' };
  sandbox.App.Calendar.render(container);
  return container.innerHTML;
}

const count = (html, s) => html.split(s).length - 1;

test('a session moved onto an otherwise empty day shows there, beside its usual day', () => {
  assert.equal(count(renderParentWeek({ ownClassToday: false }), 'Zayden Maths'), 2);
});

test('a parent never sees another class\'s moved session, even on a day with their own class', () => {
  assert.equal(count(renderParentWeek({ ownClassToday: true }), 'Someone Else Science'), 0);
});
