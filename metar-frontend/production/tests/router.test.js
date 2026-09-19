'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { create, owns } = require('../src/router.js');

function browser(url) {
  const host = { location: new URL(url), pushes: 0, replacements: 0 };
  host.history = {
    replaceState(_state, _title, value) { host.replacements++; host.location = new URL(value, host.location); },
    pushState(_state, _title, value) { host.pushes++; host.location = new URL(value, host.location); },
  };
  return host;
}

test('旧分享链接只替换当前历史项，并保留筛选、分页和编码后的搜索词', () => {
  for (const [old, expected] of [
    ['/discover', '/latest'], ['/questions?order=unanswered&page=2', '/latest?order=unanswered&page=2'],
    ['/topics', '/topics'], ['/question/42', '/question/42'], ['/topic/AI%20tools', '/topic/AI%20tools'],
    ['/bookmarks', '/me/bookmarks'], ['/notifications', '/me/notifications'],
    ['/admin/pulse', '/admin/pulse'], ['/search?q=a%23b%26c', '/search?q=a%23b%26c'],
  ]) {
    const host = browser('https://metar.uk/#' + old);
    assert.equal(create(host).migrate(), true);
    assert.equal(host.location.pathname + host.location.search, expected);
    assert.equal(host.location.hash, '');
    assert.equal(host.pushes, 0);
    assert.equal(host.replacements, 1);
  }
});

test('普通路径可直接读取，尾斜杠规范化，锚点不作为页面路由', () => {
  const host = browser('https://metar.uk/latest/?order=hot#main');
  const router = create(host);
  router.migrate();
  assert.equal(router.route().path, '/latest');
  assert.equal(router.route().query.get('order'), 'hot');
  assert.equal(host.location.hash, '#main');
  assert.equal(create(browser('https://metar.uk/')).route().path, '/latest');
});

test('路由拒绝外站、回调、发帖、账号接口和伪造的旧 hash 路径', () => {
  for (const url of ['//evil.test/topics', '/\\evil.test/topics', 'https://evil.test/topics', '/users/login', '/questions', '/questions/ask', '/notifications', '/answer/api/v1/user/info', '/api/user-center/login/callback', '/topic/../../api/token', '/latest\n']) {
    const host = browser('https://metar.uk/latest');
    assert.equal(create(host).go(url), false, url);
    host.location.hash = '#' + url;
    // /questions and /notifications are legitimate old shell aliases only.
    if (!['/questions', '/notifications', '/latest\n'].includes(url)) assert.equal(create(host).migrate(), false, url);
  }
});

test('站内普通点击走 History，修饰键、新窗口、下载和 Answer 链接保持浏览器原行为', () => {
  const host = browser('https://metar.uk/latest');
  const router = create(host);
  const event = (changes = {}) => ({ button: 0, preventDefault() { this.prevented = true; }, target: { closest: () => ({ target: '', hasAttribute: () => false, getAttribute: () => '/topics' }) }, ...changes });
  const click = event();
  assert.equal(router.follow(click), true);
  assert.equal(click.prevented, true);
  assert.equal(host.location.pathname, '/topics');
  assert.equal(host.pushes, 1);
  for (const changes of [{ ctrlKey: true }, { metaKey: true }, { shiftKey: true }, { altKey: true }, { button: 1 }, { defaultPrevented: true }, { target: { closest: () => null } }]) {
    assert.equal(router.follow(event(changes)), false);
  }
  for (const anchor of [{ target: '_blank' }, { hasAttribute: () => true }, { getAttribute: () => '/users/login' }]) {
    assert.equal(router.follow(event({ target: { closest: () => ({ target: '', hasAttribute: () => false, getAttribute: () => '/topics', ...anchor }) } })), false);
  }
});

test('Nginx 深链接范围与浏览器一致，Answer 原生路径不被静态首页吞掉', () => {
  const config = fs.readFileSync(path.resolve(__dirname, '../../..', 'deploy/nginx/meta-pulse.conf'), 'utf8');
  const pattern = config.match(/location ~ (\^\/\(\?:latest[^\n]+) \{/)[1];
  const gateway = new RegExp(pattern);
  const shellPaths = ['/', '/latest', '/topics', '/topic/support', '/question/42', '/search', '/knowledge', '/me', '/me/bookmarks', '/me/notifications', '/settings/binding', '/admin/pulse', '/pulse', '/login', '/register', '/forgot', '/publish', '/support', '/status', '/guidelines'];
  for (const route of shellPaths) {
    assert.equal(owns(route), true, route);
    assert.equal(route === '/' || gateway.test(route), true, route);
  }
  for (const route of ['/questions', '/questions/ask', '/questions/42', '/notifications', '/users/login', '/users/auth-landing', '/users/confirm-email', '/answer/api/v1/question/page', '/metar/api/pulse/actions', '/api/user-center/login/callback', '/blog/', '/sitemap.xml']) {
    assert.equal(gateway.test(route), false, route);
    assert.equal(owns(route), false, route);
  }
});
