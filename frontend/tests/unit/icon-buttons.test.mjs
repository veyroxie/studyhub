// A button whose only content is a glyph (edit, delete, more, reschedule) is
// silent to a screen reader unless it carries aria-label; title is not enough.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const FRONTEND = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const GLYPH_ONLY = /<button\b([^>]*)>\s*&#(?:9998|10005|8942|8631|215|8943);\s*<\/button>/g;

test('every glyph-only button has an aria-label', () => {
  const dir = path.join(FRONTEND, 'js');
  const unlabelled = [];
  for (const f of fs.readdirSync(dir, { recursive: true }).filter((x) => x.endsWith('.js'))) {
    const src = fs.readFileSync(path.join(dir, f), 'utf8');
    for (const m of src.matchAll(GLYPH_ONLY)) {
      if (!/aria-label=/.test(m[1])) unlabelled.push(`${f}: ${m[0].slice(0, 90)}`);
    }
  }
  assert.deepEqual(unlabelled, []);
});
