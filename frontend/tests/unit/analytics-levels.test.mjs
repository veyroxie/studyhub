// Analytics grouped students by classes.level_band, the retired pricing_tiers
// banding. Nothing maintains that column now that the catalogue prices classes,
// so the By Level view drifted toward showing everyone as "Other" as classes
// were edited. It groups by the tier a student is priced at instead.
import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

function withCatalogue() {
  const sandbox = loadSandbox(['js/utils.js', 'js/store.js', 'js/modules/analytics.js']);
  sandbox.App.Store.set({
    pricingCategories: [
      { id: 'PC_group', name: 'Group', sortOrder: 1 },
      { id: 'PC_private', name: 'Private', sortOrder: 2 },
    ],
    pricingPlans: [
      { id: 'p1', categoryId: 'PC_group', tierName: 'Level 1-2', sessionsPerWeek: 1, monthlyFee: 240 },
      { id: 'p2', categoryId: 'PC_group', tierName: 'Level 3-4', sessionsPerWeek: 1, monthlyFee: 260 },
      { id: 'p3', categoryId: 'PC_private', tierName: 'Level 5-6', sessionsPerWeek: 1, monthlyFee: 520 },
    ],
  });
  return sandbox.App;
}

const CLASSES = [
  { id: 'CLS_a', defaultTierName: 'Level 1-2' },
  { id: 'CLS_b', defaultTierName: 'Level 3-4' },
  { id: 'CLS_none', defaultTierName: '' },
];

describe('levels come from the catalogue', () => {
  test('the level list is the catalogue tiers, not the two retired bands', () => {
    const App = withCatalogue();
    assert.deepEqual(Array.from(App.Analytics._levels()), ['Level 1-2', 'Level 3-4', 'Level 5-6']);
  });

  test('a tier added to the catalogue shows up as a level with no code change', () => {
    const App = withCatalogue();
    assert.ok(Array.from(App.Analytics._levels()).includes('Level 5-6'));
  });
});

describe('a student groups by the tier they are priced at', () => {
  test("the student's own tier wins over the class default", () => {
    const App = withCatalogue();
    const s = { id: 'S1', pricingTier: 'Level 5-6', enrolledClasses: ['CLS_a'] };
    assert.equal(App.Analytics._studentLevel(s, CLASSES), 'Level 5-6');
  });

  test('otherwise it falls back to the class default', () => {
    const App = withCatalogue();
    const s = { id: 'S2', pricingTier: '', enrolledClasses: ['CLS_b'] };
    assert.equal(App.Analytics._studentLevel(s, CLASSES), 'Level 3-4');
  });

  test('straddling two tiers takes the higher, as the band grouping did', () => {
    const App = withCatalogue();
    const s = { id: 'S3', enrolledClasses: ['CLS_a', 'CLS_b'] };
    assert.equal(App.Analytics._studentLevel(s, CLASSES), 'Level 3-4');
  });

  test('an untiered class leaves the student ungrouped rather than mislabelled', () => {
    const App = withCatalogue();
    const s = { id: 'S4', enrolledClasses: ['CLS_none'] };
    assert.equal(App.Analytics._studentLevel(s, CLASSES), null);
  });

  test('a student in no classes is ungrouped', () => {
    const App = withCatalogue();
    assert.equal(App.Analytics._studentLevel({ id: 'S5' }, CLASSES), null);
  });
});
