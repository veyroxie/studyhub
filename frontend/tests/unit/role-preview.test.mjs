// The role switch previews other screens. It is for admins and developers: a
// parent who could flip it landed in an empty admin UI.
import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { loadSandbox } from './_load.mjs';

function appAs(actualRole, hostname = 'studyhub.fit') {
  const sandbox = loadSandbox(['js/store.js', 'js/utils.js', 'js/main.js']);
  sandbox.location.hostname = hostname;
  sandbox.App.actualRole = actualRole;
  sandbox.App.actualEmail = actualRole + '@example.com';
  return sandbox.App;
}

describe('who may preview other roles', () => {
  test('an admin may', () => assert.equal(appAs('admin').canPreviewRoles(), true));
  test('a superadmin may, and works in the admin view', () => {
    const App = appAs('superadmin');
    assert.equal(App.canPreviewRoles(), true);
    assert.equal(App.uiRoleFor('superadmin'), 'admin');
  });
  test('a parent may not', () => assert.equal(appAs('parent').canPreviewRoles(), false));
  test('a teacher may not', () => assert.equal(appAs('teacher').canPreviewRoles(), false));
  test('anyone may on a developer machine', () => assert.equal(appAs('parent', 'localhost').canPreviewRoles(), true));

  test('the switch does nothing for a parent', () => {
    const App = appAs('parent');
    App.currentRole = 'client';
    App.toggleRole();
    assert.equal(App.currentRole, 'client');
  });
});
