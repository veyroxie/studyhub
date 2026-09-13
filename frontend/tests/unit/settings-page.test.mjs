// Business & Invoice details moved out of the schedule page (#24).
//
// These assertions are deliberately structural rather than visual: the bug was
// that a whole screen was unreachable, and "is it registered and does it render
// the form" is exactly what nobody checked when it was buried in a sub-tab.
import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { loadSandbox } from './_load.mjs';

const FRONTEND = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const read = (p) => fs.readFileSync(path.join(FRONTEND, p), 'utf8');

describe('settings page', () => {
  test('the module exposes a render function', () => {
    const sandbox = loadSandbox(['js/utils.js', 'js/modules/settings.js']);
    assert.equal(typeof sandbox.App.Settings.render, 'function');
  });

  test('it renders the business form, not an empty page', async () => {
    const sandbox = loadSandbox(['js/utils.js', 'js/modules/settings.js']);
    sandbox.App.currentRole = 'admin';
    const container = { innerHTML: '' };
    await sandbox.App.Settings.render(container);
    assert.match(container.innerHTML, /business-settings-form/);
    assert.match(container.innerHTML, /bankAccountNo/);
    assert.match(container.innerHTML, /invoiceTerms/);
  });

  test('a non-admin gets nothing', async () => {
    const sandbox = loadSandbox(['js/utils.js', 'js/modules/settings.js']);
    sandbox.App.currentRole = 'teacher';
    const container = { innerHTML: '' };
    await sandbox.App.Settings.render(container);
    assert.doesNotMatch(container.innerHTML, /bankAccountNo/);
  });

  test('the page is reachable: dock button, script tag and route all exist', () => {
    const html = read('index.html');
    const main = read('js/main.js');
    // A button that is never created cannot be shown, and a module that is
    // never loaded registers as undefined. Both have bitten this app before.
    assert.match(html, /data-page="settings"[^>]*class="dock-btn dock-admin"/);
    assert.match(html, /js\/modules\/settings\.js/);
    assert.match(main, /App\.Router\.register\('settings'/);
    // Loaded before main.js, or App.Settings is undefined at registration.
    assert.ok(html.indexOf('js/modules/settings.js') < html.indexOf('js/main.js'),
      'settings.js must load before main.js');
  });

  test('business details are gone from the schedule module', () => {
    const calendar = read('js/modules/calendar.js');
    assert.doesNotMatch(calendar, /business-settings-form/);
    assert.doesNotMatch(calendar, /_renderBusinessSettingsCard/);
    // And the sub-tab no longer claims to be Settings, which would leave two.
    assert.doesNotMatch(calendar, />Settings<\/button>/);
  });
});
