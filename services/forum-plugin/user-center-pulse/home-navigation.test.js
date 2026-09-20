import test from 'node:test';
import assert from 'node:assert/strict';
import { homeDestination, canonicalizeHomeLinks, installHomeNavigation } from './home-navigation.js';

function anchor(href, { home = true, download = false } = {}) {
  return {
    matches: () => home,
    hasAttribute: (name) => name === 'download' && download,
    getAttribute: () => href,
    setAttribute: (name, value) => { assert.equal(name, 'href'); href = value; },
  };
}
const origin = 'https://metar.uk';

test('后台返回网站和站名直接指向首页，保留筛选与锚点', () => {
  const link = anchor('/?order=hot#main');
  canonicalizeHomeLinks({ querySelectorAll: () => [link] }, origin);
  assert.equal(link.getAttribute('href'), '/latest?order=hot#main');
  assert.equal(homeDestination(link, origin), '/latest?order=hot#main');
});

test('不接管普通链接、通知、管理操作、下载或外部站点', () => {
  for (const link of [anchor('/', { home: false }), anchor('/', { download: true }),
    anchor('/users/notifications/inbox'), anchor('/admin/users'), anchor('https://other.test/'), anchor('//other.test/'), anchor('/latest\\evil')]) {
    assert.equal(homeDestination(link, origin), null);
  }
});

test('一次浏览器原生导航，保留默认行为与 beforeunload；取消的点击不跳转', () => {
  let handler;
  let editing = false;
  const doc = {
    querySelector: () => editing,
    addEventListener: (name, fn, capture) => { assert.equal(name, 'click'); assert.equal(capture, true); handler = fn; },
    removeEventListener: (name, fn, capture) => { assert.equal(fn, handler); assert.equal(capture, true); handler = null; },
  };
  const dispose = installHomeNavigation({ document: doc, location: { origin } });
  for (const modifiers of [{}, { metaKey: true }, { ctrlKey: true }, { shiftKey: true }]) {
    const link = anchor('/');
    let stopped = 0;
    handler({ button: 0, target: { closest: () => link }, ...modifiers,
      stopPropagation: () => stopped++, preventDefault: () => assert.fail('must retain default navigation and draft guard') });
    assert.equal(stopped, 1);
    assert.equal(link.getAttribute('href'), '/latest');
  }
  for (const event of [{ button: 0, defaultPrevented: true }, { button: 1 }, { button: 0, target: null }]) {
    handler({ ...event, stopPropagation: () => assert.fail('must not intercept') });
  }
  editing = true;
  handler({ button: 0, target: { closest: () => anchor('/') }, stopPropagation: () => assert.fail('editor must keep React blocker') });
  dispose(); assert.equal(handler, null);
});
