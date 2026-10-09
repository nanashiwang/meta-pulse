'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {render, layout, canRetain} = require('../src/loading.js');
const template = fs.readFileSync(path.join(__dirname, '../src/loading-view.html'), 'utf8');

test('首屏与路由共用骨架，区分列表/网格/私有面板且转义动态标题', () => {
  for (const route of ['/', '/latest', '/discover']) assert.equal(layout(route), 'feed');
  for (const route of ['/topic/ai', '/search', '/me/bookmarks']) assert.equal(layout(route), 'list');
  for (const route of ['/topics', '/knowledge']) assert.equal(layout(route), 'grid');
  for (const route of ['/me', '/admin/pulse', '/settings/binding', '/pulse']) assert.equal(layout(route), 'panel');
  const result = render(template, '<img src=x onerror=alert(1)>$&', '/topics', 'Loading…', true);
  assert.match(result, /data-layout="grid"/);
  assert.match(result, /&lt;img src=x onerror=alert\(1\)&gt;\$&amp;/);
  assert.doesNotMatch(result, /__METAR_|[\u3400-\u9fff]/);
  assert.equal((result.match(/class="loading-item"/g) || []).length, 6);
});

test('只保留相同 URL、语言及会话的公开页面，私人和状态页面一律实时加载', () => {
  for (const route of ['/latest', '/topics', '/topic/ai', '/search', '/knowledge', '/support', '/guidelines']) {
    assert.equal(canRetain(route, route, route, 'zh_CN', 'zh_CN', true), true);
    assert.equal(canRetain(route, route + '?page=2', route, 'zh_CN', 'zh_CN', true), false);
    assert.equal(canRetain(route, route, route, 'en_US', 'zh_CN', true), false);
    assert.equal(canRetain(route, route, route, 'zh_CN', 'zh_CN', false), false);
  }
  for (const route of ['/me', '/me/bookmarks', '/settings/binding', '/pulse', '/admin/pulse', '/admin/growth', '/status', '/users/alice', '/topic/ai/edit']) {
    assert.equal(canRetain(route, route, route, 'zh_CN', 'zh_CN', true), false);
  }
});
