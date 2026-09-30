// The role switch previews other screens. It is a developer tool only: a parent
// who could flip it landed in an empty admin UI, and admins have no use for it.
import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

function appAs(actualRole, { hostname = 'studyhub.fit', developer = false } = {}) {
  const sandbox = loadSandbox(['js/store.js', 'js/utils.js', 'js/main.js']);
  sandbox.location.hostname = hostname;
  sandbox.App.actualRole = actualRole;
  sandbox.App.Api = { currentUser: () => ({ role: actualRole, developer }) };
  return sandbox.App;
}

describe('who may preview other roles', () => {
  test('the developer may, whatever their role', () => assert.equal(appAs('admin', { developer: true }).canPreviewRoles(), true));
  test('an admin who is not the developer may not: admins run the centre', () => assert.equal(appAs('admin').canPreviewRoles(), false));
  test('a superadmin works in the admin view', () => assert.equal(appAs('superadmin').uiRoleFor('superadmin'), 'admin'));
  test('a parent may not', () => assert.equal(appAs('parent').canPreviewRoles(), false));
  test('a teacher may not', () => assert.equal(appAs('teacher').canPreviewRoles(), false));
  test('anyone may on a developer machine', () => assert.equal(appAs('parent', { hostname: 'localhost' }).canPreviewRoles(), true));

  test('the switch does nothing for a parent', () => {
    const App = appAs('parent');
    App.currentRole = 'client';
    App.toggleRole();
    assert.equal(App.currentRole, 'client');
  });
});
