'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const SOURCE = path.resolve(__dirname, '../src');
const script = (name) => fs.readFileSync(path.join(SOURCE, name), 'utf8');

function languageContext(saved, unavailable = false) {
  const values = new Map(saved === undefined ? [] : [['metar-language', saved]]);
  const context = vm.createContext({
    localStorage: {
      getItem: (key) => { if (unavailable) throw new Error('Storage blocked'); return values.get(key) || null; },
      setItem: (key, value) => { if (unavailable) throw new Error('Storage blocked'); values.set(key, value); },
    },
  });
  context.window = context;
  vm.runInContext(script('i18n.js'), context);
  return { context, values, i18n: context.MetarI18n };
}

test('默认中文、只接受约定语言，并与 Answer 共享持久偏好', () => {
  const { values, i18n } = languageContext();
  assert.equal(i18n.getLanguage(), 'zh_CN');
  assert.equal(i18n.locale(), 'zh-CN');
  assert.equal(i18n.t('问答广场'), '问答广场');
  assert.equal(i18n.setLanguage('en_US'), true);
  assert.equal(values.get('metar-language'), 'en_US');
  assert.equal(i18n.locale(), 'en-US');
  assert.equal(i18n.t('问答广场'), 'Questions');
  assert.equal(languageContext(values.get('metar-language')).i18n.getLanguage(), 'en_US');
  assert.equal(i18n.setLanguage('fr_FR'), false);
  assert.equal(i18n.getLanguage(), 'en_US');
  assert.equal(languageContext('unexpected').i18n.getLanguage(), 'zh_CN');
  const blocked = languageContext(undefined, true).i18n;
  assert.equal(blocked.setLanguage('en_US'), true);
  assert.equal(blocked.t('问答广场'), 'Questions');
});

test('参数文案和数量在两种语言下保持完整语序', () => {
  const { i18n } = languageContext('en_US');
  assert.equal(i18n.t('“{query}” 的结果', { query: '未读' }), 'Results for “未读”');
  assert.equal(i18n.t('第 {page} / {total} 页', { page: 2, total: 3 }), 'Page 2 of 3');
  assert.equal(i18n.countLabel(1, 'answers'), '1 answer');
  assert.equal(i18n.countLabel(1200, 'answers'), '1,200 answers');
  i18n.setLanguage('zh_CN');
  assert.equal(i18n.countLabel(1200, 'answers'), '1,200 回答');
  assert.equal(i18n.t('第 {page} / {total} 页', { page: 2, total: 3 }), '第 2 / 3 页');
});

test('app、适配器、头像中的显式中文界面词条都有英文翻译', () => {
  const { i18n } = languageContext('en_US');
  for (const name of ['app.js', 'adapters.js', 'avatars.js']) {
    for (const match of script(name).matchAll(/\bt\(\s*(["'])([^"'\n]+)\1/g)) {
      const key = match[2];
      if (/[\u3400-\u9fff]/.test(key)) assert.notEqual(i18n.t(key), key, `${name}: ${key}`);
    }
  }
});

async function shell({ language = 'en_US', user = null, binding = 'unbound', content = false, failure = '' } = {}) {
  const { context, i18n, values } = languageContext(language);
  const node = () => ({ innerHTML: '', textContent: '', attributes: {}, setAttribute(key, value) { this.attributes[key] = value; }, focus() {} });
  const nodes = Object.fromEntries(['app', 'view', 'main', 'skip', 'description', 'theme', 'menu', 'language'].map((key) => [key, node()]));
  const documentEvents = new Map();
  const windowEvents = new Map();
  const requests = [];
  const document = {
    title: '', documentElement: { dataset: {}, lang: '' },
    body: { classList: { remove() {}, toggle() { return true; } } },
    getElementById: (id) => nodes[id] || null,
    querySelector: (selector) => ({
      '[data-action="skip"]': nodes.skip, 'meta[name="description"]': nodes.description,
      '[data-action="theme"]': nodes.theme, '[data-action="menu"]': nodes.menu,
      '[data-action="language"]': nodes.language,
    })[selector] || null,
    addEventListener: (event, callback) => { const list = documentEvents.get(event) || []; list.push(callback); documentEvents.set(event, list); },
  };
  const question = { id: '42', title: content ? '未读' : 'User question', description: content ? '社区规范' : 'User summary', content: content ? '我的收藏' : 'User content', user_info: user, created_at: Math.floor(Date.now() / 1000) - 3600 };
  Object.assign(context, {
    document, location: { origin: 'https://metar.uk', hash: '#/discover' },
    URL, URLSearchParams, Headers, AbortController, setTimeout, clearTimeout,
    console: { warn() {}, error() {} },
    matchMedia: () => ({ matches: false }), scrollTo() {},
    addEventListener: (event, callback) => windowEvents.set(event, callback),
    __METAR_RUNTIME_CONFIG__: JSON.parse(fs.readFileSync(path.join(SOURCE, '../config.production.json'), 'utf8')),
    fetch: async (url, options) => {
      requests.push({ url, options });
      const pathname = new URL(url, 'https://metar.uk').pathname;
      if (failure === 'all' || (failure === 'identity' && pathname.endsWith('/user/info'))) return { ok: false, status: 503, json: async () => ({ msg: '中文服务器错误', data: null }) };
      let data = { count: 0, list: [] };
      if (pathname === '/answer/api/v1/user/info') data = user;
      else if (pathname.endsWith('/personal/user/info')) data = { ...user, bio: content ? '元衡账号绑定' : '' };
      else if (pathname.endsWith('/question/info')) data = question;
      else if (pathname.endsWith('/connector/user/info')) data = binding === 'unavailable' ? [] : [{ link: '/answer/api/v1/connector/login/pulse_user_center', binding: binding === 'bound' }];
      else if (content && pathname.endsWith('/question/page')) data = { count: 1, list: [question] };
      return { ok: true, status: 200, json: async () => ({ data }) };
    },
  });
  for (const name of ['adapters.js', 'avatars.js', 'app.js']) vm.runInContext(script(name), context);
  await new Promise(setImmediate);
  return {
    i18n, values, document, nodes, requests,
    async navigate(route) { context.location.hash = '#' + route; await windowEvents.get('hashchange')(); },
    async changeLanguage(value) {
      for (const listener of documentEvents.get('change') || []) listener({ target: { value, matches: () => true } });
      await new Promise(setImmediate);
    },
    html: () => nodes.app.innerHTML + nodes.view.innerHTML,
  };
}
const assertEnglish = (view, route) => {
  const rendered = view.html().replace(/中文/g, ''); // Native language name remains recognizable in either locale.
  assert.doesNotMatch(rendered, /[\u3400-\u9fff]/, route);
  assert.doesNotMatch(view.document.title, /[\u3400-\u9fff]/, route + ' title');
};

test('所有英文页面与访客/已登录/绑定/失败状态不会遗留静态中文', async () => {
  for (const scenario of [
    {}, { user: { username: 'alice', display_name: 'Alice', mail_status: 1 } },
    { user: { username: 'alice', display_name: 'Alice', mail_status: 1 }, binding: 'bound' },
    { user: { username: 'alice', display_name: 'Alice', mail_status: 2 } },
    { user: { username: 'alice', display_name: 'Alice', mail_status: 1 }, binding: 'unavailable' },
    { failure: 'all' },
  ]) {
    const view = await shell(scenario);
    for (const route of ['/discover', '/questions', '/question/42', '/topics', '/topic/agent', '/knowledge', '/search', '/search?q=hello', '/me', '/bookmarks', '/notifications', '/settings/binding', '/pulse', '/publish', '/login', '/register', '/forgot', '/support', '/status', '/guidelines', '/missing']) {
      await view.navigate(route);
      assertEnglish(view, route);
    }
    assert.equal(view.document.documentElement.lang, 'en-US');
    assert.equal(view.nodes.skip.textContent, 'Skip to main content');
    assert.ok(view.requests.every(({ options }) => options.headers.get('Accept-Language') === 'en-US'));
  }
});

test('语言切换重新渲染标题、筛选、ARIA、错误和数值，用户原文保持不变', async () => {
  const view = await shell({ language: 'zh_CN', user: { username: 'alice', display_name: 'Alice', mail_status: 1 }, content: true });
  await view.navigate('/questions');
  assert.match(view.html(), /最近活跃/);
  await view.changeLanguage('en_US');
  assert.match(view.html(), /Active/);
  assert.match(view.html(), /aria-label="Interface language"/);
  assert.equal(view.values.get('metar-language'), 'en_US');
  assert.equal(view.document.title, 'Questions · METAR');
  assert.match(view.html(), /<h3>未读<\/h3>/);
  assert.match(view.html(), /<p>社区规范<\/p>/);
  assert.match(view.html(), /1 hour ago/);
  await view.navigate('/question/42');
  assert.match(view.html(), /prod-question-body">我的收藏/);
  await view.navigate('/me');
  assert.match(view.html(), /元衡账号绑定/);
  await view.changeLanguage('zh_CN');
  assert.equal(view.document.documentElement.lang, 'zh-CN');
  assert.match(view.html(), /编辑 Answer 资料/);
  assert.equal(view.i18n.errorMessage({ code: 'network', message: 'old message' }), '暂时无法连接社区服务，请稍后重试');
  view.i18n.setLanguage('en_US');
  assert.match(view.i18n.errorMessage({ code: 'network', message: '中文错误' }), /Unable to connect/);
});
