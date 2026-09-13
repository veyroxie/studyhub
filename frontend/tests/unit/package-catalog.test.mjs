// The invoice builder's package list must come from the live catalogue.
//
// It used to be hardcoded off the retired pricing_tiers table, bucketed into
// levels 1-6, so Level 0, Mandarin, Phonics and twice-weekly tiers could not be
// chosen at all and the right figures were typed by hand. Removing that list
// then left no tuition entries whatsoever, so a class could only be added by
// Rebuild, which replaces every line -- which is how a deposit went missing.
import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

function withCatalogue() {
  const sandbox = loadSandbox(['js/utils.js', 'js/store.js', 'js/modules/billing.js']);
  sandbox.App.Store.set({
    pricingCategories: [
      { id: 'PC_group', name: 'Group', creditCovered: false, sortOrder: 1 },
      { id: 'PC_mandarin', name: 'Mandarin', creditCovered: false, sortOrder: 2 },
      { id: 'PC_selfstudy', name: 'Self-Study', creditCovered: true, sortOrder: 3 },
    ],
    pricingPlans: [
      { id: 'p1', categoryId: 'PC_group', tierName: 'Level 3-4', sessionsPerWeek: 1, monthlyFee: 260 },
      { id: 'p2', categoryId: 'PC_group', tierName: 'Level 3-4', sessionsPerWeek: 2, monthlyFee: 490 },
      { id: 'p3', categoryId: 'PC_mandarin', tierName: 'Group', sessionsPerWeek: 1, monthlyFee: 240 },
      { id: 'p4', categoryId: 'PC_selfstudy', tierName: 'Beyond included hours', sessionsPerWeek: 1, monthlyFee: 0 },
    ],
  });
  return sandbox.App.Billing;
}

describe('package catalogue', () => {
  test('offers a twice-weekly tier, which the old list could not express at all', () => {
    const entries = withCatalogue()._packageCatalog();
    const twice = entries.find((e) => e.unitPrice === 490);
    assert.ok(twice, 'no 2x-a-week entry');
    assert.match(twice.descriptor, /2x a week/);
  });

  test('offers Mandarin, which the old list had no concept of', () => {
    const entries = withCatalogue()._packageCatalog();
    assert.ok(entries.some((e) => e.group === 'Mandarin' && e.unitPrice === 240));
  });

  test('line names match what the engine emits, so hand-built and generated invoices read alike', () => {
    const entries = withCatalogue()._packageCatalog();
    const group = entries.find((e) => e.key === 'plan-p1');
    assert.equal(group.name, 'Group');
  });

  test('credit-covered categories are not offered: they are free by design', () => {
    const entries = withCatalogue()._packageCatalog();
    assert.ok(!entries.some((e) => e.group === 'Self-Study'));
  });

  test('one-offs are still there, so a joining invoice can be built', () => {
    const entries = withCatalogue()._packageCatalog();
    for (const name of ['Registration Fee', 'Deposit (1 month)', 'TSH Membership']) {
      assert.ok(entries.some((e) => e.name === name), `missing ${name}`);
    }
  });

  test('every group reaches the dropdown, including one Nadine adds later', () => {
    const html = withCatalogue()._packageCatalogOptions();
    // The previous version hardcoded the group list, so Mandarin was built and
    // then dropped on the way out.
    assert.match(html, /optgroup label="Group"/);
    assert.match(html, /optgroup label="Mandarin"/);
    assert.match(html, /optgroup label="One-off"/);
  });

  test('an empty catalogue still offers the one-offs rather than nothing', () => {
    const sandbox = loadSandbox(['js/utils.js', 'js/store.js', 'js/modules/billing.js']);
    sandbox.App.Store.set({ pricingCategories: [], pricingPlans: [] });
    const entries = sandbox.App.Billing._packageCatalog();
    assert.ok(entries.some((e) => e.name === 'Registration Fee'));
  });
});
