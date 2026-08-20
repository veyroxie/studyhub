// Loads the real frontend IIFE modules into a sandbox so their pure helpers
// can be unit-tested. Deliberately does NOT modify the app: no ESM rewrite, no
// build step, no bundler — the modules are executed exactly as the browser
// runs them, against the smallest DOM stub they touch at load time.
//
// Uses node:test / node:assert, so the suite has zero dependencies and runs
// with `node --test` on any machine that can already run the project.
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const FRONTEND = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');

function domStub() {
  const el = () => ({
    style: {}, dataset: {}, classList: { add() {}, remove() {}, contains: () => false },
    appendChild() {}, removeChild() {}, setAttribute() {}, removeAttribute() {},
    addEventListener() {}, focus() {}, querySelectorAll: () => [], innerHTML: '', textContent: '',
  });
  return {
    createElement: el,
    getElementById: () => null,
    querySelector: () => null,
    querySelectorAll: () => [],
    addEventListener() {},
    head: el(),
    body: el(),
    documentElement: el(),
    cookie: '',
  };
}

// loadApp runs the given frontend js files in one sandbox and returns window.App.
export function loadApp(files = ['js/utils.js']) {
  return loadSandbox(files).App;
}

// loadSandbox returns the whole sandbox as well, for helpers that reach for
// document — filterSelect looks its target up by id, so a test has to be able
// to stub getElementById.
export function loadSandbox(files = ['js/utils.js']) {
  // The sandbox must BE its own `window`: the modules do `window.App = ...`
  // and then reference bare `App`, which only resolves because in a browser
  // window is the global object. A plain { window: {} } stub breaks that.
  const sandbox = {
    document: domStub(),
    navigator: { clipboard: null, userAgent: 'node' },
    localStorage: { getItem: () => null, setItem() {}, removeItem() {} },
    sessionStorage: { getItem: () => null, setItem() {}, removeItem() {} },
    location: { hostname: 'localhost', protocol: 'http:', host: 'localhost' },
    setTimeout, clearTimeout, setInterval, clearInterval, console,
    fetch: () => Promise.reject(new Error('network disabled in unit tests')),
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  const ctx = vm.createContext(sandbox);
  for (const f of files) {
    vm.runInContext(fs.readFileSync(path.join(FRONTEND, f), 'utf8'), ctx, { filename: f });
  }
  return sandbox;
}
