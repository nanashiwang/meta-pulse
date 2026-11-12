import test from 'node:test';
import assert from 'node:assert/strict';
import { backDestination } from './native-shell.js';

test('native administration pages return to me instead of the community feed', () => {
 for (const path of ['/admin', '/admin/', '/admin/dashboard', '/admin/users', '/admin/settings']) {
  assert.equal(backDestination(path), '/me', path);
 }
});
test('home placeholders and other return destinations remain intact', () => {
 for (const path of ['/', '/latest', '/questions', '/discover']) assert.equal(backDestination(path), null);
 for (const path of ['/badges', '/users/settings/profile', '/settings/binding']) assert.equal(backDestination(path), '/me');
 for (const path of ['/questions/123', '/users/example', '/administrator']) assert.equal(backDestination(path), '/latest');
});
