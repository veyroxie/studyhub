// Unit tests for the pure helpers in js/utils.js.
//
// Run: npm test  (or: TZ=Asia/Kuala_Lumpur node --test frontend/tests/unit/)
//
// TZ matters. localDate/today/nowTime exist because toISOString() returns the
// UTC date, which in UTC+8 is still "yesterday" until 08:00 local — that bug
// mis-dated check-ins and self-study rows. CI runs in UTC, so without a pinned
// TZ these tests would pass locally and fail there (or worse, vice versa).
import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

const sandbox = loadSandbox(['js/utils.js']);
const U = sandbox.App.Utils;

describe('esc — the XSS primitive', () => {
  test('escapes every character that can break out of HTML or an attribute', () => {
    assert.equal(U.esc('<script>'), '&lt;script&gt;');
    assert.equal(U.esc('a"b'), 'a&quot;b');
    assert.equal(U.esc("a'b"), 'a&#39;b');
    assert.equal(U.esc('a&b'), 'a&amp;b');
  });

  test('escapes the ampersand first so entities are not double-decoded', () => {
    // If & were escaped last, '&lt;' would render as a literal '<'.
    assert.equal(U.esc('&lt;'), '&amp;lt;');
  });

  test('renders null and undefined as empty, never the string "null"', () => {
    assert.equal(U.esc(null), '');
    assert.equal(U.esc(undefined), '');
  });

  test('coerces non-strings instead of throwing', () => {
    assert.equal(U.esc(42), '42');
    assert.equal(U.esc(0), '0');
  });

  test('defeats a real attribute-breakout payload', () => {
    const out = U.esc('" autofocus onfocus=alert(1) x="');
    assert.ok(!out.includes('"'), 'no raw quote may survive');
  });
});

describe('formatCurrency', () => {
  test('formats ringgit to two decimals', () => {
    assert.equal(U.formatCurrency(260), 'RM 260.00');
    // No thousands separator today — pinned so a future shared formatter
    // (V2 plan B7) changes it deliberately rather than by accident.
    assert.equal(U.formatCurrency(1234.5), 'RM 1234.50');
  });

  test('does not print NaN for empty or malformed input', () => {
    for (const bad of [null, undefined, '', 'abc']) {
      assert.ok(!String(U.formatCurrency(bad)).includes('NaN'),
        `formatCurrency(${JSON.stringify(bad)}) must not surface NaN to a parent`);
    }
  });

  test('handles zero as a real amount', () => {
    assert.equal(U.formatCurrency(0), 'RM 0.00');
  });
});

describe('local date helpers (timezone-sensitive)', () => {
  test('localDate uses the local calendar date, not the UTC one', () => {
    // 00:30 local on the 2nd is still the 1st in UTC when TZ=UTC+8.
    const d = new Date(2026, 7, 2, 0, 30, 0);
    assert.equal(U.localDate(d), '2026-08-02');
  });

  test('localDate zero-pads so string comparison sorts chronologically', () => {
    // Ordering across the app is lexicographic on these strings.
    assert.equal(U.localDate(new Date(2026, 0, 5)), '2026-01-05');
    assert.ok('2026-01-05' < '2026-01-12');
  });

  test('today returns a YYYY-MM-DD string', () => {
    assert.match(U.today(), /^\d{4}-\d{2}-\d{2}$/);
  });

  test('nowTime returns zero-padded HH:MM consistent with today()', () => {
    assert.match(U.nowTime(), /^\d{2}:\d{2}$/);
  });
});

describe('formatTime', () => {
  test('handles the 12-hour boundaries that off-by-one bugs hide in', () => {
    assert.match(U.formatTime('00:00').toLowerCase(), /12:00\s*am/);
    assert.match(U.formatTime('12:00').toLowerCase(), /12:00\s*pm/);
    assert.match(U.formatTime('13:05').toLowerCase(), /1:05\s*pm/);
  });

  test('does not throw on malformed input', () => {
    assert.doesNotThrow(() => U.formatTime(''));
    assert.doesNotThrow(() => U.formatTime('nonsense'));
  });
});

describe('generateId', () => {
  test('produces the prefixed, restricted-charset ids the onclick handlers rely on', () => {
    // ~60 inline handlers interpolate ids into JS string literals, where esc()
    // cannot protect them. That is only safe while ids stay quote-free.
    const id = U.generateId('inv');
    assert.match(id, /^inv[a-z0-9_]*$/i);
    assert.ok(!/['"<>\\]/.test(id), 'ids must never contain quote or angle characters');
  });

  test('does not collide across rapid successive calls', () => {
    const ids = new Set(Array.from({ length: 200 }, () => U.generateId('x')));
    assert.equal(ids.size, 200);
  });
});

describe('badge / colorClasses fallbacks', () => {
  test('an unknown colour falls back instead of returning undefined', () => {
    const c = U.colorClasses('chartreuse');
    assert.ok(c && c.bg, 'must return a usable class set');
    assert.deepEqual(c, U.colorClasses('blue'));
  });

  test('statusBadge renders an unknown status without throwing', () => {
    assert.doesNotThrow(() => U.statusBadge('Some Future Status'));
  });

  test('badge escapes its label', () => {
    assert.ok(!U.badge('<img src=x>', 'blue').includes('<img'));
  });
});

describe('filterFor / filterTarget — the shared entity picker', () => {
  test('escapes the placeholder so it cannot break out of the attribute', () => {
    const html = U.filterFor('enr-class', '" onfocus=alert(1) x="');
    assert.ok(!html.includes('" onfocus'), 'placeholder must not escape its attribute');
  });

  test('wires the oninput handler to the select it filters', () => {
    assert.ok(U.filterFor('cred-class').includes("filterTarget('cred-class'"));
  });

  test('hides only the options that do not match, never the blank one', () => {
    const opts = [
      { value: '', text: '-- select a class --', hidden: false },
      { value: 'c1', text: 'Mandarin L1 · Monday · Ms Lee', hidden: false },
      { value: 'c2', text: 'Phonics · Tuesday · Ms Tan', hidden: false },
    ];
    sandbox.document.getElementById =() => ({ tagName: 'SELECT', options: opts });

    U.filterTarget('x', 'phon');
    assert.equal(opts[0].hidden, false, 'the placeholder must stay selectable');
    assert.equal(opts[1].hidden, true);
    assert.equal(opts[2].hidden, false);
  });

  test('matches case-insensitively and clears when the query empties', () => {
    const opts = [{ value: 'c1', text: 'Mandarin L1', hidden: true }];
    sandbox.document.getElementById =() => ({ tagName: 'SELECT', options: opts });

    U.filterTarget('x', 'MANDARIN');
    assert.equal(opts[0].hidden, false);
    U.filterTarget('x', '');
    assert.equal(opts[0].hidden, false, 'an empty query must reveal everything again');
  });

  test('clears the selection when the chosen option is filtered away', () => {
    // Otherwise FormData still submits the hidden choice and the admin saves a
    // class she can no longer see.
    const opts = [
      { value: '', text: '-- select a class --', hidden: false },
      { value: 'c1', text: 'Mandarin L1', hidden: false },
      { value: 'c2', text: 'Phonics A', hidden: false },
    ];
    const sel = { tagName: 'SELECT', options: opts, selectedIndex: 2 };
    sandbox.document.getElementById = () => sel;

    U.filterTarget('x', 'mandarin');
    assert.equal(sel.selectedIndex, 0, 'the filtered-away choice must not stay selected');
  });

  test('keeps the selection when it still matches the filter', () => {
    const opts = [
      { value: '', text: '-- select a class --', hidden: false },
      { value: 'c2', text: 'Phonics A', hidden: false },
    ];
    const sel = { tagName: 'SELECT', options: opts, selectedIndex: 1 };
    sandbox.document.getElementById = () => sel;

    U.filterTarget('x', 'phon');
    assert.equal(sel.selectedIndex, 1);
  });

  test('filters checkbox rows by their data-search text', () => {
    // The enrolment list is rows, not options — same search box, other branch.
    const rows = [
      { attrs: 'mandarin l1 monday ms lee', hidden: false },
      { attrs: 'phonics a thursday no teacher assigned', hidden: false },
    ].map(r => ({ hidden: r.hidden, getAttribute: () => r.attrs }));
    sandbox.document.getElementById = () => ({ tagName: 'DIV', querySelectorAll: () => rows });

    U.filterTarget('enr-list', 'thursday');
    assert.equal(rows[0].hidden, true);
    assert.equal(rows[1].hidden, false);
  });

  test('row filtering matches on teacher name, not just class name', () => {
    const rows = [{ hidden: false, getAttribute: () => 'mandarin l1 monday ms lee' }];
    sandbox.document.getElementById = () => ({ tagName: 'DIV', querySelectorAll: () => rows });

    U.filterTarget('enr-list', 'ms lee');
    assert.equal(rows[0].hidden, false, 'searching a teacher must keep their classes visible');
  });

  test('does nothing when the select is absent instead of throwing', () => {
    sandbox.document.getElementById =() => null;
    assert.doesNotThrow(() => U.filterTarget('missing', 'abc'));
  });
});

describe('emptyState / showConfirm escaping contract', () => {
  test('emptyState escapes title and subtitle', () => {
    const html = U.emptyState('<script>t</script>', '<script>s</script>');
    assert.ok(!html.includes('<script>'), 'empty-state text must be escaped');
  });
});
