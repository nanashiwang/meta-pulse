const test = require('node:test');
const assert = require('node:assert/strict');
const { policy } = require('../src/seo.js');

test('private account, notification and reward routes never get indexed', () => {
  for (const path of ['/me', '/me/bookmarks', '/me/notifications', '/settings/binding', '/pulse', '/admin/pulse', '/search', '/publish', '/login', '/register', '/forgot', '/status']) {
    assert.equal(policy(path).robots, 'noindex, nofollow', path);
    assert.equal(policy(path).canonical, '', path);
  }
});
test('public question canonical uses the server rendered Answer URL', () => {
  assert.deepEqual(policy('/question/10010000000000002/', '?token=private&tracking=x'), { canonical: 'https://metar.uk/questions/10010000000000002', robots: 'index, follow' });
  assert.equal(policy('/topic/api', '?page=2&token=private').canonical, 'https://metar.uk/tags/api?page=2');
  assert.equal(policy('/latest', '?page=2&order=hot').canonical, 'https://metar.uk/latest?page=2');
  assert.equal(policy('/latest', '?page=garbage').canonical, 'https://metar.uk/latest');
  assert.equal(policy('/').canonical, 'https://metar.uk/');
});
