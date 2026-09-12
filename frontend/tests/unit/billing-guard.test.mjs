// A monthly invoice with no tuition on it is almost certainly missing the month
// it bills for. One September invoice was RM250 registration plus RM260 deposit
// with no class line, which also took up the student's slot for the month, so
// their tuition could not be raised at all.
import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

const sandbox = loadSandbox(['js/utils.js', 'js/modules/billing.js']);
const B = sandbox.App.Billing;

const item = (name, unitPrice) => ({ kind: 'item', name, qty: 1, unitPrice, amount: unitPrice });
const discount = (name, amt) => ({ kind: 'discount', name, qty: 1, unitPrice: amt, amount: -amt });

describe('missing-tuition guard', () => {
  test('flags the exact invoice that caused this: registration plus deposit, no class', () => {
    assert.equal(B._looksLikeMissingTuition('Monthly', [
      item('Registration Fee', 250),
      item('Deposit (1 month) — Group Level 4-6', 260),
    ]), true);
  });

  test('does not flag a monthly invoice that has tuition on it', () => {
    assert.equal(B._looksLikeMissingTuition('Monthly', [
      item('Registration Fee', 250),
      item('Deposit (1 month) — Group Level 4-6', 260),
      item('Group', 260),
    ]), false);
  });

  test('tuition alone is fine', () => {
    assert.equal(B._looksLikeMissingTuition('Monthly', [item('Level 3 & 4', 260)]), false);
  });

  test('an Adhoc invoice of fees is exactly what Adhoc is for', () => {
    assert.equal(B._looksLikeMissingTuition('Adhoc', [item('Registration Fee', 250)]), false);
  });

  test('discount lines do not count as tuition', () => {
    assert.equal(B._looksLikeMissingTuition('Monthly', [
      item('Registration Fee', 250),
      discount('Early bird discount', 10),
    ]), true);
  });

  test('an empty invoice is rejected elsewhere, not here', () => {
    assert.equal(B._looksLikeMissingTuition('Monthly', []), false);
  });

  test('self-study and membership are not tuition either', () => {
    assert.equal(B._looksLikeMissingTuition('Monthly', [
      item('TSH Membership', 40),
      item('Self-study add-on', 10),
    ]), true);
  });
});
