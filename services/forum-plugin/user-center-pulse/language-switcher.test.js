import { test } from 'node:test';
import assert from 'node:assert/strict';
import { LANGUAGE_KEY, createLanguageController } from './language-switcher.js';

function fixture(saved = null, initial = 'zh_CN') {
  let language = initial;
  let user = { username: '', language: 'Default', color_scheme: 'dark', access_token: '' };
  let stored = saved;
  const userListeners = [];
  const languageListeners = [];
  const queued = [];
  let dateLocale = '';
  let writes = 0;
  const host = {
    getLanguage: () => language,
    changeLanguage(value) { language = value; languageListeners.forEach((fn) => fn(value)); },
    onLanguageChanged(fn) { languageListeners.push(fn); },
    getUserLanguage: () => user.language,
    setUserLanguage(value) { writes++; user = { ...user, language: value }; userListeners.forEach((fn) => fn()); },
    subscribeUser(fn) { userListeners.push(fn); },
    setDateLocale(value) { dateLocale = value; },
  };
  const storage = {
    getItem(key) { assert.equal(key, LANGUAGE_KEY); return stored; },
    setItem(key, value) { assert.equal(key, LANGUAGE_KEY); stored = value; },
  };
  const controller = createLanguageController(host, storage, (fn) => queued.push(fn));
  return {
    controller, host,
    flush() { let n = 0; while (queued.length) { assert.ok(n++ < 10, 'no event loop'); queued.shift()(); } },
    user(value) { if (value) { user = value; userListeners.forEach((fn) => fn()); } return user; },
    stored: () => stored, writes: () => writes, dateLocale: () => dateLocale,
  };
}

test('guests can switch both ways and retain their browser preference', () => {
  const app = fixture();
  assert.equal(app.controller.language(), 'zh_CN');
  assert.equal(app.controller.select('en_US'), true);
  assert.equal(app.host.getLanguage(), 'en_US');
  assert.equal(app.dateLocale(), 'en_US');
  assert.equal(app.stored(), 'en_US');
  assert.equal(fixture(app.stored()).host.getLanguage(), 'en_US');
  app.controller.select('zh_CN');
  assert.equal(app.host.getLanguage(), 'zh_CN');
});
test('login refresh and logout retain language without changing identity or theme', () => {
  const app = fixture('en_US');
  const logged = { username: 'admin', language: 'zh_CN', color_scheme: 'dark', access_token: 'test-session', is_admin: true };
  app.user(logged);
  assert.deepEqual(app.user(), { ...logged, language: 'en_US' });
  assert.ok(app.writes() <= 2);
  app.user({ username: '', language: 'Default', access_token: '' });
  assert.deepEqual(app.user(), { username: '', language: 'en_US', access_token: '' });
});
test('late native language setup cannot undo the most recent explicit choice', () => {
  const app = fixture('en_US');
  app.host.changeLanguage('zh_CN');
  app.flush();
  assert.equal(app.host.getLanguage(), 'en_US');
  app.host.changeLanguage('zh_CN');
  app.controller.select('zh_CN');
  app.flush();
  assert.equal(app.host.getLanguage(), 'zh_CN');
});
test('invalid preferences are rejected, native default remains until an explicit choice', () => {
  const app = fixture('not-a-language', 'en_US');
  assert.equal(app.writes(), 0);
  assert.equal(app.controller.select('fr_FR'), false);
  assert.equal(app.controller.language(), 'en_US');
  app.host.changeLanguage('zh_CN');
  assert.equal(app.controller.language(), 'zh_CN');
  app.controller.fromStorage('en_US');
  assert.equal(app.host.getLanguage(), 'en_US');
});
test('blocked storage does not prevent in-page language switching', () => {
  const app = fixture();
  const denied = { getItem() { throw Error('disabled'); }, setItem() { throw Error('disabled'); } };
  const controller = createLanguageController(app.host, denied);
  controller.select('en_US');
  assert.equal(controller.language(), 'en_US');
  assert.equal(app.host.getLanguage(), 'en_US');
});
