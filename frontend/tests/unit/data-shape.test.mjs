// Guardrail: frontend/js/data.js must never contain records.
//
// It is served unauthenticated at /js/data.js, so anything in it is public.
// It previously held twelve real children — names and dates of birth — four of
// them current students and eight whose names had already been erased from the
// database. It reached staff screens on every session expiry, because the
// store fell back to it when local storage was cleared.
//
// This test fails if any collection gains an entry, or if anything that looks
// like personal data appears in the file at all.
import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { loadSandbox } from './_load.mjs';

const sandbox = loadSandbox(['js/data.js']);
const source = readFileSync(new URL('../../js/data.js', import.meta.url), 'utf8');

describe('data.js is a shape declaration, not a dataset', () => {
  test('every collection is empty', () => {
    const data = sandbox.App.DATA;
    assert.ok(data && typeof data === 'object', 'App.DATA must exist');
    for (const [key, value] of Object.entries(data)) {
      assert.ok(Array.isArray(value), `${key} must be an array`);
      assert.equal(value.length, 0,
        `${key} has ${value.length} record(s). This file is public — real data belongs in the database, and fabricated data reaches staff screens on session expiry.`);
    }
  });

  test('no personal-data fields appear anywhere in the file', () => {
    // Catches a record pasted back in even under a different collection name.
    for (const field of ['firstName', 'lastName', 'dob', 'parentName', 'phone', 'nric', 'medicalInfo', 'allergies']) {
      assert.ok(!source.includes(field + ':'),
        `data.js contains a "${field}" field — it is served unauthenticated, so this would publish personal data.`);
    }
  });

  test('no email addresses', () => {
    const match = source.match(/[\w.+-]+@[\w.-]+\.\w+/);
    assert.equal(match, null, `data.js contains an email address (${match && match[0]})`);
  });
});
