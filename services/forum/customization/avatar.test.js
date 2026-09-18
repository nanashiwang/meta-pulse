const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const html = fs.readFileSync(__dirname + '/avatar-head.html', 'utf8');
const context = { module: { exports: {} }, URL };
vm.runInNewContext(html.match(/<script[^>]*>([\s\S]*?)<\/script>/)[1], context);
const { initial, avatarURL, profileName, isDefault } = context.module.exports;
const origin = 'https://metar.uk';

test('initial handles whitespace, unicode and missing users', () => {
  assert.equal(initial(' admin '), 'A');
  assert.equal(initial('元衡'), '元');
  assert.equal(initial('😀user'), '😀');
  assert.equal(initial('👩‍💻user'), '👩‍💻');
  assert.equal(initial('e\u0301ve'), 'E\u0301');
  assert.equal(initial(''), 'M');
});

// Model browser image state and Answer's restore predicate without network or
// a copied implementation of the enhancement. The actual head script runs here.
function image(attrs = {}, options = {}) {
  const attributes = { width: '80', height: '80', alt: 'Nickname', ...attrs };
  return {
    style: { content: '' }, className: 'rounded-circle', complete: false, naturalWidth: 0,
    get alt() { return attributes.alt; },
    getAttribute: (key) => attributes[key] ?? null,
    setAttribute: (key, value) => { attributes[key] = value; },
    removeAttribute: (key) => { delete attributes[key]; },
    matches: () => true,
    closest(selector) {
      if (selector === 'a[href]' && options.username) return { getAttribute: () => '/users/' + options.username };
      if (selector.startsWith('.fmt') && options.content) return {};
      return null;
    },
  };
}
function browser(images) {
  let observer;
  const events = {};
  const queue = [];
  const window = {};
  const document = {
    documentElement: {}, querySelectorAll: () => images, querySelector: () => null,
    addEventListener: (name, handler) => { events[name] = handler; },
  };
  vm.runInNewContext(html.match(/<script[^>]*>([\s\S]*?)<\/script>/)[1], {
    document, window, URL, location: { origin, pathname: '/' },
    queueMicrotask: (callback) => queue.push(callback),
    MutationObserver: class { constructor(callback) { observer = callback; } observe() {} },
  });
  return {
    scan() { observer(); while (queue.length) queue.shift()(); },
    fail(img) { events.error({ target: img }); },
  };
}
test('blocked defaults, image errors, SPA insertion and uploaded replacements', () => {
  const blocked = image({ 'data-src': 'https://www.gravatar.com/avatar/hash' }, { username: 'admin' });
  const upload = image({ src: '/uploads/avatar/a.png' }, { username: 'bob' });
  const post = image({ 'data-src': 'https://example.test/photo.png' }, { content: true });
  const images = [blocked, upload, post];
  const app = browser(images);
  assert.match(decodeURIComponent(blocked.getAttribute('src')), />A<\/text>/);
  assert.equal(blocked.getAttribute('data-src'), null);
  assert.equal(upload.getAttribute('src'), '/uploads/avatar/a.png');
  assert.equal(post.getAttribute('src'), null);
  app.fail(upload);
  assert.match(decodeURIComponent(upload.getAttribute('src')), />B<\/text>/);
  upload.setAttribute('src', '/uploads/avatar/new.png');
  app.scan();
  assert.equal(upload.getAttribute('src'), '/uploads/avatar/new.png');
  const next = image({}, { username: 'charlie' });
  images.push(next);
  app.scan();
  assert.match(decodeURIComponent(next.getAttribute('src')), />C<\/text>/);
  const before = next.getAttribute('src');
  app.scan();
  assert.equal(next.getAttribute('src'), before);
});
test('blocked custom avatars remain restorable by Answer external-media action', () => {
  const remote = image({ 'data-src': 'https://cdn.example.test/upload.png' }, { username: 'dave' });
  const app = browser([remote]);
  assert.equal(remote.getAttribute('src'), null);
  assert.match(decodeURIComponent(remote.style.content), />D<\/text>/);
  // Answer restores only images whose src is empty, then removes data-src.
  if (!remote.getAttribute('src') && remote.getAttribute('data-src')) {
    remote.setAttribute('src', remote.getAttribute('data-src'));
    remote.removeAttribute('data-src');
  }
  app.scan();
  assert.equal(remote.getAttribute('src'), 'https://cdn.example.test/upload.png');
  assert.equal(remote.style.content, '');
  app.fail(remote);
  assert.match(decodeURIComponent(remote.getAttribute('src')), />D<\/text>/);
});
test('generated avatars are self contained and escape user text', () => {
  const svg = decodeURIComponent(avatarURL('<img').split(',')[1]);
  assert.match(svg, />&lt;<\/text>/);
  assert.doesNotMatch(svg, /<img|script|https?:\/\/(?!www.w3.org)/);
});
test('only same-origin actual profile links identify users', () => {
  assert.equal(profileName('/users/admin', origin), 'admin');
  assert.equal(profileName('/users/%E5%85%83%E8%A1%A1/', origin), '元衡');
  for (const path of ['/users/settings', '/users/settings/profile', '/users/login', 'https://other.test/users/admin', '/users/%']) {
    assert.equal(profileName(path, origin), '');
  }
});
test('defaults never replace normal uploads or lookalike hosts', () => {
  for (const src of ['', 'https://www.gravatar.com/avatar/hash?s=48', 'https://gravatar.cn/avatar/hash', '/static/media/default-avatar.123abc.svg', '/assets/default-avatar-Q9.svg']) {
    assert.equal(isDefault(src, origin), true, src);
  }
  for (const src of ['/uploads/avatar/admin.png?s=96', 'https://gravatar.com.evil.test/a', 'https://other.test/default-avatar.svg', 'data:image/png;base64,abc']) {
    assert.equal(isDefault(src, origin), false, src);
  }
});
