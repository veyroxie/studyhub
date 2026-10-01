// The parent preview picked the blank "parent" of students with no email, and the
// parent screens then skipped their filter and showed every child in the centre.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadApp } from './_load.mjs';

const students = [
  { id: 'S1', contact: 'tan@example.com', parentName: 'Mrs Tan' },
  { id: 'S2', contact: 'tan@example.com', parentName: 'Mrs Tan' },
  { id: 'S3', contact: '', parentName: '' },
  { id: 'S4', contact: '  ', parentName: 'Nobody' },
];

test('the parent list leaves out students with no parent email', () => {
  const parents = loadApp().Utils.parentsOf(students);
  assert.deepEqual(Object.keys(parents), ['tan@example.com']);
  assert.equal(parents['tan@example.com'], 'Mrs Tan');
});

test('nobody is the child of a blank parent', () => {
  assert.equal(loadApp().Utils.childrenOf(students, '').length, 0);
});
