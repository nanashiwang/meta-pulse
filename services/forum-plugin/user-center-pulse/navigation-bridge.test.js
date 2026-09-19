import test from 'node:test';
import assert from 'node:assert/strict';
import { installNavigationBridge } from './navigation-bridge.js';
import { target } from './route-policy.js';

function browser(path = '/users/admin') {
  const tasks = [], listeners = new Map(), loads = [], entries = [];
  let url = new URL(path, 'https://metar.uk');
  const host = {
    location: {
      get href() { return url.href; }, get origin() { return url.origin; },
      replace(value) { loads.push(value); },
    },
    history: {},
    queueMicrotask(task) { tasks.push(task); },
    addEventListener(name, fn) { listeners.set(name, fn); },
    removeEventListener(name) { listeners.delete(name); },
    flush() { while (tasks.length) tasks.shift()(); },
    emit(name, path) { if (path) url = new URL(path, url); listeners.get(name)?.(); },
    loads, entries,
  };
  for (const method of ['pushState', 'replaceState']) host.history[method] = function (state, title, value) {
    assert.equal(this, host.history);
    const next = new URL(value || url, url);
    if (next.origin !== url.origin) throw new Error('SecurityError');
    entries.push({ method, state, title });
    url = next;
    return 'native-result';
  };
  return host;
}

test('资料页回首页完整加载 METAR，保留 Answer 已提交的历史和 state', () => {
  const host = browser();
  installNavigationBridge(host);
  host.flush();
  assert.deepEqual(host.loads, []);
  const state = { key: 'native', idx: 1 };
  assert.equal(host.history.pushState(state, '', '/?order=hot&page=2#main'), 'native-result');
  assert.deepEqual(host.loads, []);
  host.flush();
  assert.deepEqual(host.loads, ['/latest?order=hot&page=2#main']);
  assert.equal(host.entries[0].state, state);
  assert.equal(host.entries.length, 1);
});

test('登录回跳、返回前进及 bfcache 恢复均可离开旧壳层', () => {
  for (const path of ['/me', '/search?q=a%26b', '/admin/pulse', '/topic/AI%20tools']) {
    for (const event of ['popstate', 'pageshow']) {
      const host = browser(); installNavigationBridge(host); host.flush();
      host.emit(event, path); host.flush();
      assert.deepEqual(host.loads, [path]);
    }
  }
  const host = browser('/users/login'); installNavigationBridge(host); host.flush();
  host.history.replaceState(null, '', '/me/bookmarks'); host.flush();
  assert.deepEqual(host.loads, ['/me/bookmarks']);
});

test('写操作、敏感落地和未迁移的用户页保持 Answer；失败的导航没有副作用', () => {
  const host = browser(); installNavigationBridge(host); host.flush();
  for (const path of ['/questions/ask', '/questions/42/99?commentId=101', '/users/register', '/users/auth-landing?token=fixture', '/users/confirm-email?key=fixture', '/api/user-center/login/callback', '/users/someone', '/admin/pulse_user_center', '/blog/']) {
    host.history.pushState({}, '', path); host.flush();
  }
  assert.deepEqual(host.loads, []);
  assert.throws(() => host.history.pushState({}, '', 'https://evil.test/latest'));
  host.flush(); assert.deepEqual(host.loads, []);
  // A blocked editor navigation never commits history, so no click handler
  // can bypass the blocker or discard the draft.
  assert.equal(host.entries.length, 9);
});

test('拒绝跨域、凭据、编码路径穿越和分隔符，允许查询及锚点', () => {
  for (const value of ['https://evil.test/latest', 'https://user:pass@metar.uk/latest', '/topic/%2fadmin', '/topic/%5cadmin', '/topic/%00', '/topic/a%2f..%2fusers', '/topic/%', '/latest\n']) {
    assert.equal(target(value, 'https://metar.uk'), null, value);
  }
  assert.equal(target('/question/42?commentId=7#answer-9', 'https://metar.uk'), '/question/42?commentId=7#answer-9');
});

test('重复安装不会叠加历史包装器，同一轮只根据最终已提交路径切换', () => {
  const host = browser(); const dispose = installNavigationBridge(host);
  assert.equal(installNavigationBridge(host), dispose);
  host.history.pushState({}, '', '/');
  host.history.replaceState({}, '', '/users/admin');
  host.flush(); assert.deepEqual(host.loads, []);
  dispose(); host.history.pushState({}, '', '/latest'); host.flush();
  assert.deepEqual(host.loads, []);
});
