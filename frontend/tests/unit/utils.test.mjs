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

describe('creditsForClass', () => {
  // 1 credit = 15 minutes, agreed with the centre 02/04/2026. The backend
  // mirror is creditsForDuration in handlers_cancelled.go - keep them in sync.
  const U = () => loadSandbox().App.Utils;
  test('a 1-hour class earns 4 credits', () => {
    assert.equal(U().creditsForClass({ time: '10:00', endTime: '11:00' }), 4);
  });
  test('a 30-minute class earns 2 credits', () => {
    assert.equal(U().creditsForClass({ time: '15:00', endTime: '15:30' }), 2);
  });
  test('a 45-minute class earns 3 credits', () => {
    assert.equal(U().creditsForClass({ time: '09:15', endTime: '10:00' }), 3);
  });
  test('missing or unparsable times fall back to the standard 4', () => {
    assert.equal(U().creditsForClass(null), 4);
    assert.equal(U().creditsForClass({}), 4);
    assert.equal(U().creditsForClass({ time: 'abc', endTime: '11:00' }), 4);
    assert.equal(U().creditsForClass({ time: '11:00', endTime: '10:00' }), 4);
  });
});

describe('holidayCovers', () => {
  // THE holiday range predicate (F4). The backend mirror is core.HolidayCovers
  // in Go - keep them in sync. Malformed endDate (before date) means single-day:
  // the old backend SQL treated it as open-ended and suppressed reminders forever.
  const U = () => loadSandbox().App.Utils;
  test('a range holiday covers every day from date to endDate inclusive', () => {
    const h = { date: '2026-03-15', endDate: '2026-03-17' };
    assert.equal(U().holidayCovers(h, '2026-03-15'), true);
    assert.equal(U().holidayCovers(h, '2026-03-16'), true);
    assert.equal(U().holidayCovers(h, '2026-03-17'), true);
    assert.equal(U().holidayCovers(h, '2026-03-14'), false);
    assert.equal(U().holidayCovers(h, '2026-03-18'), false);
  });
  test('no endDate means single day, never open-ended', () => {
    const h = { date: '2026-03-15' };
    assert.equal(U().holidayCovers(h, '2026-03-15'), true);
    assert.equal(U().holidayCovers(h, '2026-03-16'), false);
    assert.equal(U().holidayCovers({ date: '2026-03-15', endDate: '' }, '2026-04-01'), false);
  });
  test('endDate before date degrades to single day, matching the backend', () => {
    const h = { date: '2026-03-15', endDate: '2026-03-10' };
    assert.equal(U().holidayCovers(h, '2026-03-15'), true);
    assert.equal(U().holidayCovers(h, '2026-03-12'), false);
  });
  test('null holiday or missing date never matches', () => {
    assert.equal(U().holidayCovers(null, '2026-03-15'), false);
    assert.equal(U().holidayCovers({}, '2026-03-15'), false);
  });
});

describe('scheduleOn — version resolution (mirrors store.ScheduleOn in Go)', () => {
  const cls = { id: 'CLS_1', day: 'Thursday', time: '16:00', endTime: '17:00' };
  // A version states the schedule that applies FROM effectiveFrom.
  const early = { classId: 'CLS_1', day: 'Friday', time: '15:00', endTime: '16:00', effectiveFrom: '0001-01-01' };
  const sept = { classId: 'CLS_1', day: 'Thursday', time: '16:00', endTime: '17:00', effectiveFrom: '2026-09-01' };

  test('a date before the change resolves to the earlier version', () => {
    const got = U.scheduleOn(cls, [early, sept], '2026-08-28');
    assert.equal(got.day, 'Friday');
    assert.equal(got.time, '15:00');
    assert.equal(got.endTime, '16:00');
  });

  test('the effective date itself uses the new version', () => {
    assert.equal(U.scheduleOn(cls, [early, sept], '2026-09-01').day, 'Thursday');
    assert.equal(U.scheduleOn(cls, [early, sept], '2026-10-15').day, 'Thursday');
  });

  test('the greatest effectiveFrom at or before the date wins, regardless of array order', () => {
    const nov = { classId: 'CLS_1', day: 'Monday', time: '09:00', endTime: '10:00', effectiveFrom: '2026-11-01' };
    assert.equal(U.scheduleOn(cls, [nov, sept, early], '2026-09-15').day, 'Thursday');
    assert.equal(U.scheduleOn(cls, [nov, sept, early], '2026-11-02').day, 'Monday');
    assert.equal(U.scheduleOn(cls, [nov, sept, early], '2026-08-01').day, 'Friday');
  });

  test('an out-of-order insert just fills its own span', () => {
    // Added after nov, but effective earlier — no guard, no rejection.
    const nov = { classId: 'CLS_1', day: 'Monday', time: '09:00', endTime: '10:00', effectiveFrom: '2026-11-01' };
    const oct = { classId: 'CLS_1', day: 'Tuesday', time: '11:00', endTime: '12:00', effectiveFrom: '2026-10-01' };
    assert.equal(U.scheduleOn(cls, [nov, oct, early], '2026-10-10').day, 'Tuesday');
    assert.equal(U.scheduleOn(cls, [nov, oct, early], '2026-11-10').day, 'Monday');
  });

  test('versions for another class are ignored', () => {
    const other = { classId: 'CLS_2', day: 'Monday', time: '10:00', endTime: '11:00', effectiveFrom: '0001-01-01' };
    assert.equal(U.scheduleOn(cls, [other], '2026-08-28').day, 'Thursday');
  });

  test('no versions falls back to the class row, tolerating null', () => {
    const got = U.scheduleOn(cls, null, '2026-08-28');
    assert.equal(got.day, 'Thursday');
    assert.equal(got.time, '16:00');
    assert.equal(got.endTime, '17:00');
  });
});

describe('enrolledOn — roster membership for a DATE, not for "now"', () => {
  const stu = { id: 'STU_1', enrolledClasses: [] };
  const stints = [
    { studentId: 'STU_1', classId: 'CLS_A', startedOn: '2026-03-01', endedOn: '2026-06-01' },
    { studentId: 'STU_1', classId: 'CLS_A', startedOn: '2026-09-01', endedOn: '' },
    { studentId: 'STU_2', classId: 'CLS_A', startedOn: '2020-01-01', endedOn: '' },
  ];

  test('a past date still shows a student who has since left', () => {
    // The bug this fixes: unenrolling erased them from every past roster, so
    // their attendance rows existed but nobody could see them.
    assert.equal(U.enrolledOn(stu, 'CLS_A', '2026-04-15', stints), true);
  });

  test('the gap between stints excludes them', () => {
    assert.equal(U.enrolledOn(stu, 'CLS_A', '2026-07-15', stints), false);
  });

  test('a later stint re-includes them', () => {
    assert.equal(U.enrolledOn(stu, 'CLS_A', '2026-09-15', stints), true);
  });

  test('endedOn is exclusive, matching how billing treats the same window', () => {
    assert.equal(U.enrolledOn(stu, 'CLS_A', '2026-05-31', stints), true);
    assert.equal(U.enrolledOn(stu, 'CLS_A', '2026-06-01', stints), false);
  });

  test('another student\'s stints are not borrowed', () => {
    assert.equal(U.enrolledOn({ id: 'STU_3', enrolledClasses: [] }, 'CLS_A', '2026-04-15', stints), false);
  });

  test('with no stints at all it falls back to the current class list', () => {
    // Only reachable for data predating the enrolments table.
    const legacy = { id: 'STU_9', enrolledClasses: ['CLS_A'] };
    assert.equal(U.enrolledOn(legacy, 'CLS_A', '2026-04-15', stints), true);
    assert.equal(U.enrolledOn(legacy, 'CLS_B', '2026-04-15', stints), false);
  });
});

describe('runsOnDate — the one client-side answer to "is this class on today"', () => {
  const cls = { id: 'CLS_A', day: 'Monday', time: '10:00', endTime: '11:00' };
  const versions = [{ classId: 'CLS_A', day: 'Monday', time: '10:00', endTime: '11:00', effectiveFrom: '0001-01-01' }];

  test('a normal week matches on the weekday', () => {
    // 2026-09-07 is a Monday.
    assert.equal(U.runsOnDate(cls, '2026-09-07', { scheduleVersions: versions, sessionMoves: [] }), true);
    assert.equal(U.runsOnDate(cls, '2026-09-08', { scheduleVersions: versions, sessionMoves: [] }), false);
  });

  test('a session moved AWAY no longer runs on its original date', () => {
    // The bug: the dashboard kept showing a rescheduled class on the old day.
    const moves = [{ classId: 'CLS_A', fromDate: '2026-09-07', toDate: '2026-09-09' }];
    assert.equal(U.runsOnDate(cls, '2026-09-07', { scheduleVersions: versions, sessionMoves: moves }), false);
  });

  test('and DOES run on the date it moved to, even on the wrong weekday', () => {
    const moves = [{ classId: 'CLS_A', fromDate: '2026-09-07', toDate: '2026-09-09' }];
    assert.equal(U.runsOnDate(cls, '2026-09-09', { scheduleVersions: versions, sessionMoves: moves }), true);
  });

  test('a dated schedule change still applies', () => {
    const changed = versions.concat([{ classId: 'CLS_A', day: 'Thursday', time: '16:00', endTime: '17:00', effectiveFrom: '2026-09-01' }]);
    assert.equal(U.runsOnDate(cls, '2026-09-07', { scheduleVersions: changed, sessionMoves: [] }), false);
    assert.equal(U.runsOnDate(cls, '2026-09-10', { scheduleVersions: changed, sessionMoves: [] }), true);
  });

  test('another class\'s move is not applied to this one', () => {
    const moves = [{ classId: 'CLS_B', fromDate: '2026-09-07', toDate: '2026-09-09' }];
    assert.equal(U.runsOnDate(cls, '2026-09-07', { scheduleVersions: versions, sessionMoves: moves }), true);
  });
});
