// The pricing catalogue is the only source for what a class or a student can be
// priced at. Before this the class form offered a hardcoded Group/Private type
// and a 1-3 / 4-6 band from the retired pricing_tiers table, and could not point
// a class at the catalogue at all -- so a class Nadine created was unpriceable,
// and adding a category to the catalogue reached nothing.
import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

// Deliberately includes a tier name that several categories share, and a
// category with no plans yet, because both are real states of her catalogue.
function withCatalogue(extraCategories = [], extraPlans = []) {
  const sandbox = loadSandbox(['js/utils.js', 'js/store.js', 'js/modules/calendar.js', 'js/modules/students.js']);
  sandbox.App.Store.set({
    pricingCategories: [
      { id: 'PC_group', name: 'Group', creditCovered: false, sortOrder: 1 },
      { id: 'PC_private', name: 'Private', creditCovered: false, sortOrder: 2 },
      { id: 'PC_mandarin', name: 'Mandarin', creditCovered: false, sortOrder: 3 },
      ...extraCategories,
    ],
    pricingPlans: [
      { id: 'p1', categoryId: 'PC_group', tierName: 'Level 1-2', sessionsPerWeek: 1, monthlyFee: 240 },
      { id: 'p2', categoryId: 'PC_group', tierName: 'Level 3-4', sessionsPerWeek: 1, monthlyFee: 260 },
      { id: 'p3', categoryId: 'PC_private', tierName: 'Level 0', sessionsPerWeek: 1, monthlyFee: 440 },
      { id: 'p4', categoryId: 'PC_private', tierName: 'Level 1-2', sessionsPerWeek: 1, monthlyFee: 480 },
      { id: 'p5', categoryId: 'PC_mandarin', tierName: 'Group', sessionsPerWeek: 1, monthlyFee: 240 },
      ...extraPlans,
    ],
    classes: [
      { id: 'CLS_g', name: 'Math Group', pricingCategoryId: 'PC_group', defaultTierName: 'Level 3-4' },
      { id: 'CLS_m', name: 'Mandarin', pricingCategoryId: 'PC_mandarin', defaultTierName: 'Group' },
      { id: 'CLS_none', name: 'Uncategorised', pricingCategoryId: '', defaultTierName: '' },
    ],
  });
  return sandbox.App;
}

describe('class form reads the catalogue', () => {
  test('a category added to the catalogue is offered with no code change', () => {
    const App = withCatalogue([{ id: 'PC_drama', name: 'Drama', creditCovered: false, sortOrder: 4 }]);
    assert.match(App.Calendar._categoryOptions(''), /value="PC_drama"[^>]*>Drama</);
  });

  test('categories are offered in the order the catalogue sorts them', () => {
    const App = withCatalogue();
    const html = App.Calendar._categoryOptions('');
    assert.ok(html.indexOf('PC_group') < html.indexOf('PC_private'), 'sortOrder ignored');
  });

  test('tiers are scoped to the chosen category, because tier names are not unique', () => {
    const App = withCatalogue();
    const group = App.Calendar._tierOptionsFor('PC_group', '');
    const priv = App.Calendar._tierOptionsFor('PC_private', '');
    assert.match(group, /Level 3-4/);
    assert.doesNotMatch(group, /Level 0/, "Private's Level 0 leaked into Group");
    assert.match(priv, /Level 0/);
    assert.doesNotMatch(priv, /Level 3-4/, "Group's Level 3-4 leaked into Private");
  });

  test("Mandarin's tier is named Group, and that must not appear as a Group tier", () => {
    const App = withCatalogue();
    assert.doesNotMatch(App.Calendar._tierOptionsFor('PC_group', ''), /">Group</);
  });

  test('a category priced by nothing yet still renders a usable control', () => {
    const App = withCatalogue([{ id: 'PC_drama', name: 'Drama', sortOrder: 4 }]);
    const html = App.Calendar._tierOptionsFor('PC_drama', '');
    assert.match(html, /needs a custom fee/);
    assert.equal(html.match(/<option/g).length, 1, 'expected only the no-tier option');
  });

  test('the current category stays selected, so a save round-trips it', () => {
    const App = withCatalogue();
    assert.match(App.Calendar._categoryOptions('PC_mandarin'), /value="PC_mandarin" selected/);
    assert.doesNotMatch(App.Calendar._categoryOptions('PC_mandarin'), /Select a category/);
  });
});

describe('a class says what actually prices it', () => {
  test('names the catalogue category and tier', () => {
    const App = withCatalogue();
    assert.equal(App.Calendar._pricedAsLabel({ pricingCategoryId: 'PC_group', defaultTierName: 'Level 3-4' }), 'Group Level 3-4');
  });

  test('a custom fee wins, because the engine never reaches the tier', () => {
    const App = withCatalogue();
    const label = App.Calendar._pricedAsLabel({ pricingCategoryId: 'PC_group', defaultTierName: 'Level 3-4', monthlyFeeOverride: 310 });
    assert.match(label, /Custom/);
    assert.match(label, /310/);
  });

  test('a category with no tier is called out, since that class bills nothing', () => {
    const App = withCatalogue();
    assert.match(App.Calendar._pricedAsLabel({ pricingCategoryId: 'PC_group', defaultTierName: '' }), /no tier set/);
  });

  test('a credit-covered category is not a missing price', () => {
    const App = withCatalogue([{ id: 'PC_ss', name: 'Self-Study', creditCovered: true, sortOrder: 9 }]);
    assert.match(App.Calendar._pricedAsLabel({ pricingCategoryId: 'PC_ss', defaultTierName: '' }), /covered by credits/);
  });
});

describe('student tier list follows the classes the student is in', () => {
  test('only tiers their own categories price are offered', () => {
    const App = withCatalogue();
    assert.deepEqual(Array.from(App.Students._tierNamesFor(['CLS_g'])), ['Level 1-2', 'Level 3-4']);
  });

  test('a student in two categories sees both, since one level spans them', () => {
    const App = withCatalogue();
    assert.deepEqual(Array.from(App.Students._tierNamesFor(['CLS_g', 'CLS_m'])), ['Group', 'Level 1-2', 'Level 3-4']);
  });

  test('a tier no class of theirs is priced by is not offered, because applying it does nothing', () => {
    const App = withCatalogue();
    assert.ok(!App.Students._tierNamesFor(['CLS_g']).includes('Level 0'), 'offered a Private-only tier');
  });

  test('no classes means no tier to choose', () => {
    const App = withCatalogue();
    assert.deepEqual(Array.from(App.Students._tierNamesFor([])), []);
  });

  test('a class with no category contributes no tiers rather than throwing', () => {
    const App = withCatalogue();
    assert.deepEqual(Array.from(App.Students._tierNamesFor(['CLS_none'])), []);
  });
});
