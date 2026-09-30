// A nav button whose page has no container is a dead click: the router finds no
// element and returns. Settings shipped that way, so every button is checked.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const FRONTEND = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const html = fs.readFileSync(path.join(FRONTEND, 'index.html'), 'utf8');
const router = fs.readFileSync(path.join(FRONTEND, 'js/router.js'), 'utf8');

// Commented-out markup (the archived Messages page) is not a live button.
const live = html.replace(/<!--[\s\S]*?-->/g, '');
const navPages = [...new Set([...live.matchAll(/data-page="([\w-]+)"/g)].map((m) => m[1]))];

test('every page a nav button opens has a container to render into', () => {
  const missing = navPages.filter((p) => !live.includes(`id="${p}-page"`));
  assert.deepEqual(missing, []);
});

test('every page a nav button opens has a title', () => {
  const missing = navPages.filter((p) => !new RegExp(`\\b${p}:\\s*'`).test(router));
  assert.deepEqual(missing, []);
});

test('a page the role hides cannot be opened by typing its address', async () => {
  const { loadSandbox } = await import('./_load.mjs');
  const sandbox = loadSandbox(['js/router.js']);
  const pages = {};
  for (const id of ['dashboard', 'settings']) pages[id + '-page'] = { classList: { add() { pages.opened = id; }, remove() {} } };
  sandbox.document.getElementById = (id) => pages[id] || null;
  sandbox.history = { replaceState() {} };
  sandbox.location = { hash: '' };
  sandbox.window.location = sandbox.location;

  sandbox.App.Router.setHidden({ settings: true });
  sandbox.App.Router.navigate('settings');
  assert.equal(pages.opened, 'dashboard');

  sandbox.App.Router.setHidden({});
  sandbox.App.Router.navigate('settings');
  assert.equal(pages.opened, 'settings');
});

test('every page the code navigates to exists', () => {
  const dir = path.join(FRONTEND, 'js');
  const files = fs.readdirSync(dir, { recursive: true }).filter((f) => f.endsWith('.js')).map((f) => path.join(dir, f));
  const targets = new Map();
  for (const file of files) {
    // Whole-line comments mention navigate('page') as an example, not a link.
    const src = fs.readFileSync(file, 'utf8').split('\n').filter((l) => !l.trim().startsWith('//')).join('\n');
    // navigate('x') in code, navigate(\'x\') inside built HTML, and quick-link page:'x' entries.
    for (const m of src.matchAll(/navigate\(\\?'([a-z][\w-]*)\\?'|\bpage:\s*'([a-z][\w-]*)'/g)) {
      const id = m[1] || m[2];
      if (!targets.has(id)) targets.set(id, path.relative(FRONTEND, file));
    }
  }
  const missing = [...targets].filter(([id]) => !live.includes(`id="${id}-page"`)).map(([id, file]) => `${id} (${file})`);
  assert.deepEqual(missing, []);
});
