'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');

global.window = global;
window.location = { origin: 'https://metar.uk' };
require('../src/avatars.js');
const avatars = window.MetarAvatars;

test('默认头像取用户名优先的 Unicode 首字符', () => {
  assert.equal(avatars.initial({ username: ' admin ', display_name: '别名' }), 'A');
  assert.equal(avatars.initial({ username: '元衡' }), '元');
  assert.equal(avatars.initial({ username: '𠮷野' }), '𠮷');
  assert.equal(avatars.initial({ username: 'e\u0301mile' }), 'E\u0301');
  assert.equal(avatars.initial({ username: '👩‍💻alice' }), '👩‍💻');
  assert.equal(avatars.initial({ username: 'ßeta' }), 'S');
  assert.equal(avatars.initial({ username: ' ', display_name: 'Alice' }), 'A');
  assert.equal(avatars.initial(null), 'M');
});

test('Answer 上传头像兼容对象与字符串并保持同源', () => {
  assert.equal(avatars.source({ type: 'custom', custom: '/uploads/avatar/alice.png' }), 'https://metar.uk/uploads/avatar/alice.png');
  assert.equal(avatars.source('/uploads/avatar/bob.webp?s=96'), 'https://metar.uk/uploads/avatar/bob.webp?s=96');
  assert.equal(avatars.source('https://metar.uk/uploads/avatar/carol.jpg'), 'https://metar.uk/uploads/avatar/carol.jpg');
  assert.equal(avatars.source({ type: 'gravatar', gravatar: '/avatar/hash', custom: '/uploads/avatar/old.png' }), '');
  assert.equal(avatars.source({ type: 'default', custom: '/uploads/avatar/old.png' }), '');
  assert.equal(avatars.source('/static/media/default-avatar.abcdef.svg'), '');
  assert.equal(avatars.source(''), '');
  assert.equal(avatars.source(null), '');
});

test('外部头像与不可用地址不会产生图片请求', () => {
  for (const value of [
    'https://www.gravatar.com/avatar/hash', 'https://gravatar.cn/avatar/hash',
    'https://images.example/avatar.png', '//evil.example/avatar.png',
    '/\\evil.example/avatar.png', 'https://metar.uk.evil.example/avatar.png',
    'http://metar.uk/uploads/avatar/a.png', 'https://user:password@metar.uk/a.png',
    'javascript:alert(1)', 'data:image/svg+xml,<svg></svg>', 'blob:https://metar.uk/id',
    'https://[invalid', {}, 42,
  ]) {
    assert.equal(avatars.source(value), '', String(value));
    assert.doesNotMatch(avatars.markup({ username: 'alice', avatar: value }), /<img\b/);
  }
});

test('头像文本和上传 URL 不能成为 HTML 或事件处理器', () => {
  const html = avatars.markup({
    username: '<img src=x onerror=alert(1)>',
    avatar: { type: 'custom', custom: '/uploads/avatar/a.png?name="&value=<test>' },
  });
  assert.match(html, /<span class="prod-avatar-initial" aria-hidden="true">&lt;<\/span>/);
  assert.match(html, /aria-label="&lt;img src=x onerror=alert\(1\)&gt; 的头像"/);
  assert.equal((html.match(/<img\b/g) || []).length, 1);
  assert.match(html, /&amp;value=/);
  assert.doesNotMatch(html, /\sonerror="/);
});

test('加载成功展示上传图，失败恢复同一头像的首字母', () => {
  const classes = new Set(['avatar', 'prod-avatar']);
  let removed = false;
  const image = {
    naturalWidth: 128,
    matches: (selector) => selector === 'img[data-metar-avatar-image]',
    parentElement: { classList: {
      contains: (name) => classes.has(name), add: (name) => classes.add(name), remove: (name) => classes.delete(name),
    } },
    remove: () => { removed = true; },
  };
  const html = avatars.markup({ username: 'alice', avatar: '/uploads/avatar/a.png' });
  assert.match(html, /prod-avatar-initial[^>]*>A<\/span>/);
  assert.doesNotMatch(html, /has-image/);
  avatars.handleImageEvent({ type: 'load', target: image });
  assert.equal(classes.has('has-image'), true);
  assert.equal(removed, false);
  avatars.handleImageEvent({ type: 'error', target: image });
  assert.equal(classes.has('has-image'), false);
  assert.equal(removed, true);

  const contentImage = { matches: () => false, remove: () => { throw new Error('正文图片不应被修改'); } };
  avatars.handleImageEvent({ type: 'error', target: contentImage });
});

test('捕获图片事件覆盖后续 SPA 页面且不使用内联事件', () => {
  const listeners = [];
  avatars.install({ addEventListener: (...args) => listeners.push(args) });
  assert.deepEqual(listeners.map(([type, , capture]) => [type, capture]), [['load', true], ['error', true]]);
  assert.ok(listeners.every(([, listener]) => listener === avatars.handleImageEvent));
});
