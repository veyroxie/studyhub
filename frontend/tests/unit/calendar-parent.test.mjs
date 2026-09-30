import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadApp } from './_load.mjs';

const App = loadApp(['js/utils.js', 'js/modules/calendar.js']);
const full = { id: 'C1', enrolled: 6, capacity: 6 };
const open = { id: 'C2', enrolled: 3, capacity: 6 };

test('a parent always sees their own child\'s class, even when it is full', () => {
  assert.equal(App.Calendar.isOpenToParent(full, { C1: true }), true);
});

test('a full class a parent is only browsing stays hidden', () => {
  assert.equal(App.Calendar.isOpenToParent(full, { C9: true }), false);
  assert.equal(App.Calendar.isOpenToParent(full, null), false);
});

test('a class with room is always open to browse', () => {
  assert.equal(App.Calendar.isOpenToParent(open, null), true);
});
