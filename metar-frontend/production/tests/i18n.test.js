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
  for (const name of ['app.js', 'admin-pulse.js', 'adapters.js', 'avatars.js']) {
    for (const match of script(name).matchAll(/\bt\(\s*(["'])([^"'\n]+)\1/g)) {
      const key = match[2];
      if (/[\u3400-\u9fff]/.test(key)) assert.notEqual(i18n.t(key), key, `${name}: ${key}`);
    }
  }
});

async function shell({ language = 'en_US', user = null, binding = 'unbound', content = false, failure = '', pulseRules = {}, pulseRewards = [], actionResult = 'settled', storageBlocked = false, adminError = 0 } = {}) {
  const { context, i18n, values } = languageContext(language);
  const node = () => ({ innerHTML: '', textContent: '', attributes: {}, setAttribute(key, value) { this.attributes[key] = value; }, focus() {} });
  const nodes = Object.fromEntries(['app', 'view', 'main', 'skip', 'description', 'theme', 'menu', 'language'].map((key) => [key, node()]));
  const documentEvents = new Map();
  const windowEvents = new Map();
  const requests = [];
  const operations = new Map();
  const document = {
    title: '', documentElement: { dataset: {}, lang: '', getAttribute(key) { return this.dataset[key.replace('data-', '')]; }, setAttribute(key, value) { this.dataset[key.replace('data-', '')] = value; } },
    body: { classList: { remove() {}, toggle() { return true; } } },
    getElementById: (id) => nodes[id] || null,
    querySelector: (selector) => ({
      '[data-action="skip"]': nodes.skip, 'meta[name="description"]': nodes.description,
      '[data-action="theme"]': nodes.theme, '[data-action="menu"]': nodes.menu,
      '[data-action="language"]': nodes.language,
    })[selector] || null,
    querySelectorAll: () => [],
    addEventListener: (event, callback) => { const list = documentEvents.get(event) || []; list.push(callback); documentEvents.set(event, list); },
  };
  const question = { id: '42', title: content ? '未读' : 'User question', description: content ? '社区规范' : 'User summary', content: content ? '我的收藏' : 'User content', user_info: user, created_at: Math.floor(Date.now() / 1000) - 3600 };
  Object.assign(context, {
    document, location: new URL('https://metar.uk/latest'),
    URL, URLSearchParams, Headers, AbortController, setTimeout, clearTimeout,
    crypto: require('node:crypto').webcrypto,
    sessionStorage: {
      getItem: (key) => operations.get(key) || null,
      setItem: (key, value) => { if (storageBlocked) throw new Error('Blocked'); operations.set(key, value); },
      removeItem: (key) => operations.delete(key),
    },
    console: { warn() {}, error() {} },
    matchMedia: () => ({ matches: false }), scrollTo() {},
    addEventListener: (event, callback) => windowEvents.set(event, callback),
    __METAR_RUNTIME_CONFIG__: JSON.parse(fs.readFileSync(path.join(SOURCE, '../config.production.json'), 'utf8')),
    fetch: async (url, options) => {
      requests.push({ url, options });
      const pathname = new URL(url, 'https://metar.uk').pathname;
      if (failure === 'all' || (failure === 'identity' && pathname.endsWith('/user/info'))) return { ok: false, status: 503, json: async () => ({ msg: '中文服务器错误', data: null }) };
      if (pathname.startsWith('/metar/api/admin/pulse/')) {
        if (adminError) return { ok: false, status: adminError, json: async () => ({ error: 'settings_unavailable' }) };
        return { ok: true, status: 200, json: async () => ({ revision: 1, config: { newapi_internal_base_url: 'http://new-api:3000', quota_per_unit: '500000', actions_enabled: false, reward_shadow_mode: true }, secrets: {}, worker_ready: true }) };
      }
      if (pathname.startsWith('/metar/api/pulse/')) {
        if (failure === 'pulse') return { ok: false, status: 503, json: async () => ({ error: 'pulse_unavailable' }) };
        let payload;
        if (pathname.endsWith('/summary')) payload = { available_tickets: 2, current_contribution_milli: 1200500, level: { name: 'Member' } };
        else if (pathname.endsWith('/rules')) payload = { enabled: true, quota_per_unit: 500000, total_weight: 100, period: { key: 'test-period', ends_at: '2026-10-01T00:00:00Z' }, rewards: [{ name: 'Daily reward', amount: 50000, weight: 100 }], ...pulseRules };
        else if (pathname.endsWith('/rewards')) payload = { rewards: new URL(url, 'https://metar.uk').searchParams.has('action_id') ? [] : pulseRewards };
        else if (pathname.endsWith('/actions')) {
          if (actionResult === 'timeout') throw new Error('Lost response');
          if (actionResult === 'action_rejected') return { ok: false, status: 409, json: async () => ({ error: 'action_rejected' }) };
          payload = { grant_id: 'grant-1', action_id: JSON.parse(options.body).action_id, status: actionResult };
        }
        return { ok: true, status: 200, json: async () => payload };
      }
      let data = { count: 0, list: [] };
      if (pathname === '/answer/api/v1/user/info') data = user;
      else if (pathname.endsWith('/personal/user/info')) data = { ...user, bio: content ? '元衡账号绑定' : '' };
      else if (pathname.endsWith('/question/info')) data = question;
      else if (pathname.endsWith('/connector/user/info')) data = binding === 'unavailable' ? [] : [{ link: '/answer/api/v1/connector/login/pulse_user_center', binding: binding === 'bound' }];
      else if (content && pathname.endsWith('/question/page')) data = { count: 1, list: [question] };
      return { ok: true, status: 200, json: async () => ({ data }) };
    },
  });
  context.history = {
    replaceState(_state, _title, url) { context.location = new URL(url, context.location.origin); },
    pushState(_state, _title, url) { context.location = new URL(url, context.location.origin); },
  };
  for (const name of ['theme.js', 'adapters.js', 'avatars.js', 'admin-pulse.js', 'route-policy.js', 'router.js', 'app.js']) vm.runInContext(script(name), context);
  await new Promise(setImmediate);
  return {
    i18n, values, document, nodes, requests, operations,
    async navigate(route) { context.location = new URL(route, context.location.origin); await windowEvents.get('popstate')(); },
    async changeLanguage(value) {
      for (const listener of documentEvents.get('change') || []) listener({ target: { value, matches: () => true } });
      await new Promise(setImmediate);
    },
    async click(action) {
      for (const listener of documentEvents.get('click') || []) listener({ target: { closest: () => ({ dataset: { action } }) } });
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
    for (const route of ['/latest', '/latest', '/question/42', '/topics', '/topic/agent', '/knowledge', '/search', '/search?q=hello', '/me', '/me/bookmarks', '/me/notifications', '/settings/binding', '/pulse', '/publish', '/login', '/register', '/forgot', '/support', '/status', '/guidelines', '/missing']) {
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
  await view.navigate('/latest');
  assert.match(view.html(), /最近活跃/);
  await view.changeLanguage('en_US');
  assert.match(view.html(), /Active/);
  assert.match(view.html(), /aria-label="Interface language"/);
  assert.equal(view.values.get('metar-language'), 'en_US');
  assert.equal(view.document.title, 'Latest topics · METAR');
  assert.match(view.html(), /<h3>未读<\/h3>/);
  assert.doesNotMatch(view.html(), /<p>社区规范<\/p>/); // Compact rows omit excerpts.
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

const pulseUser = { id: '7', username: 'alice', display_name: 'Alice', mail_status: 1 };

test('Pulse 奖池、状态与失败提示完整翻译，奖项名称和参数保持原文并转义', async () => {
  const view = await shell({ user: pulseUser, binding: 'bound', pulseRewards: [
    { grant_id: 'grant-1', amount: 50000, status: 'settled', created_at: '2026-09-19T00:00:00Z' },
    { grant_id: 'grant-2', amount: 50000, status: 'settlement_dead' },
  ] });
  await view.navigate('/pulse');
  assertEnglish(view, 'real Pulse payload');
  assert.match(view.html(), /Available Pulse tickets: 2/);
  assert.match(view.html(), /0\.1 API credits/);
  assert.match(view.html(), /Probability 100 \/ 100/);
  assert.match(view.html(), /Credited/);
  assert.match(view.html(), /Awaiting processing/);
  await view.changeLanguage('zh_CN');
  assert.match(view.html(), /可用脉冲券：2/);
  assert.match(view.html(), /0\.1 API 额度/);

  const original = await shell({ user: pulseUser, binding: 'bound', pulseRules: { rewards: [{ name: '未读<script>', amount: 50000, weight: '<b>5</b>' }] } });
  await original.navigate('/pulse');
  assert.match(original.html(), /<strong>未读&lt;script&gt;<\/strong>/);
  assert.match(original.html(), /Probability &lt;b&gt;5&lt;\/b&gt; \/ 100/);
  assert.doesNotMatch(original.html(), /<script>|<b>5<\/b>/);
  for (const unavailable_reason of ['budget_exhausted', 'activity_paused', 'no_active_period', 'funding_verification_required', 'reward_pool_unavailable', 'unknown']) {
    const paused = await shell({ user: pulseUser, binding: 'bound', pulseRules: { enabled: false, unavailable_reason, rewards: [] } });
    await paused.navigate('/pulse');
    assertEnglish(paused, unavailable_reason);
    assert.match(paused.html(), /No reward pool is currently available/);
  }
  const unavailable = await shell({ user: pulseUser, binding: 'bound', failure: 'pulse' });
  await unavailable.navigate('/pulse');
  assertEnglish(unavailable, 'Pulse unavailable');
  assert.match(unavailable.html(), /The rewards service is unavailable/);
});

test('Pulse 提交结果与原请求恢复提示随切换语言更新，超时不换操作编号', async () => {
  for (const [actionResult, english, chinese] of [
    ['settled', 'Your reward has been credited.', '奖励已到账。'],
    ['pending', 'Your draw is complete', '抽奖已完成'],
    ['action_rejected', 'No ticket was spent.', '本次未扣券'],
    ['timeout', 'The result cannot be confirmed yet.', '暂时无法确认本次结果'],
  ]) {
    const view = await shell({ user: pulseUser, binding: 'bound', actionResult });
    await view.navigate('/pulse');
    await view.click('pulse-draw');
    assertEnglish(view, actionResult);
    assert.ok(view.html().includes(english), actionResult);
    assert.equal(view.operations.size, actionResult === 'timeout' ? 1 : 0);
    if (actionResult === 'timeout') {
      assert.match(view.html(), /Check original draw/);
      await view.click('pulse-resume');
      const actions = view.requests.filter(({ url }) => url.endsWith('/actions'));
      assert.equal(actions.length, 2);
      assert.equal(actions[0].options.body, actions[1].options.body);
      assert.equal(actions[0].options.headers.get('Idempotency-Key'), actions[1].options.headers.get('Idempotency-Key'));
    }
    await view.changeLanguage('zh_CN');
    assert.ok(view.html().includes(chinese), actionResult + ' language switch');
    await view.changeLanguage('en_US');
    assertEnglish(view, actionResult + ' switched back');
  }
  const blocked = await shell({ user: pulseUser, binding: 'bound', storageBlocked: true });
  await blocked.navigate('/pulse');
  await blocked.click('pulse-draw');
  assertEnglish(blocked, 'blocked storage');
  assert.match(blocked.html(), /Allow site storage/);
  assert.equal(blocked.requests.some(({ url }) => url.endsWith('/actions')), false);
});


test('Pulse 管理入口仅对 Answer 正常激活管理员显示，版主和伪造 is_admin 无权展示', async () => {
  for (const role_id of [1, 3, 0, undefined]) {
    const view = await shell({ user: { id: '8', role_id, is_admin: true, status: 'normal', mail_status: 1 } });
    assert.doesNotMatch(view.nodes.app.innerHTML, /href="\/admin\/pulse"/);
    await view.navigate('/admin/pulse');
    assert.match(view.html(), /Administrator access required/);
    assert.equal(view.requests.some(({ url }) => url.startsWith('/metar/api/admin/')), false);
  }
  for (const user of [{ role_id: 2, status: 'suspended', mail_status: 1 }, { role_id: 2, status: 'normal', mail_status: 2 }]) {
    const view = await shell({ user });
    assert.doesNotMatch(view.nodes.app.innerHTML, /href="\/admin\/pulse"/);
  }
  const view = await shell({ user: { id: '8', role_id: 2, status: 'normal', mail_status: 1 } });
  assert.match(view.nodes.app.innerHTML, /href="\/admin\/pulse"/);
  await view.navigate('/admin/pulse');
  assert.match(view.html(), /Save Pulse settings/);
  assert.match(view.html(), /Grant key storage is ready/);
  assertEnglish(view, 'admin settings');
  assert.doesNotMatch(view.html(), /PULSE_REWARD_RANDOM_SECRET|database password/);
  await view.changeLanguage('zh_CN');
  assert.match(view.html(), /保存 Pulse 配置/);
  await view.changeLanguage('en_US');
  assertEnglish(view, 'admin language switch');
  for (const adminError of [403, 503]) {
    const blocked = await shell({ user: { role_id: 2, status: 'normal', mail_status: 1 }, adminError });
    await blocked.navigate('/admin/pulse');
    assertEnglish(blocked, `admin error ${adminError}`);
    assert.doesNotMatch(blocked.html(), /data-form="admin-pulse"/);
    if (adminError === 503) assert.match(blocked.html(), /admin_hmac_secret/);
    else assert.doesNotMatch(blocked.html(), /admin_hmac_secret/);
  }
});
