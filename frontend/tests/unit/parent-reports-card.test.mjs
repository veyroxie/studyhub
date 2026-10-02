// Parents read progress reports, not the class feed, so the dashboard card lists reports.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

const kids = [{ id: 'STU001', firstName: 'Eita' }, { id: 'STU002', firstName: '<b>Kaho</b>' }];
const reports = [
  { id: 'PR_old', studentId: 'STU001', subject: 'Japanese', term: 'Term 1', createdAt: '2026-03-01' },
  { id: 'PR_new', studentId: 'STU002', subject: 'Maths', term: 'Term 3', createdAt: '2026-09-20' },
  { id: 'PR_other', studentId: 'STU099', subject: 'Other family', term: 'Term 3', createdAt: '2026-09-25' },
];

function card(list) {
  return loadSandbox(['js/utils.js', 'js/modules/absence.js', 'js/modules/dashboard.js']).App.Dashboard._latestReportsHtml(list, kids);
}

test('lists the children\'s reports newest first, each opening the reader', () => {
  const html = card(reports);
  assert.ok(html.indexOf('PR_new') < html.indexOf('PR_old'));
  assert.match(html, /App\.Progress\._readModal\('PR_new'\)/);
});

test('leaves out a report that is not about one of their children', () => {
  assert.doesNotMatch(card(reports), /PR_other|Other family/);
});

test('escapes names', () => {
  assert.doesNotMatch(card(reports), /<b>Kaho/);
});

test('says so when there are no reports yet', () => {
  assert.match(card([]), /No progress reports yet/);
});

test('a parent who owes sees the reports are paused, not an empty reader', () => {
  const D = loadSandbox(['js/utils.js', 'js/modules/absence.js', 'js/modules/dashboard.js']).App.Dashboard;
  const html = D._latestReportsHtml(reports, kids, true);
  assert.match(html, /paused until this month/);
  assert.doesNotMatch(html, /_readModal/);
});
