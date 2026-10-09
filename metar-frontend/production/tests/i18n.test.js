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
  for (const name of ['app.js', 'admin-periods.js', 'admin-pulse.js', 'adapters.js', 'avatars.js', 'pulse-core.js']) {
    for (const match of script(name).matchAll(/\bt\(\s*(["'])([^"'\n]+)\1/g)) {
      const key = match[2];
      if (/[\u3400-\u9fff]/.test(key)) assert.notEqual(i18n.t(key), key, `${name}: ${key}`);
    }
  }
});

async function shell({ language = 'en_US', user = null, binding = 'unbound', content = false, failure = '', pulseRules = {}, pulseRewards = [], actionResult = 'settled', tickets = 2, rejectAt = 0, storageBlocked = false, adminError = 0, bookmarkCount = 0, identityGate = null, publicControl = {} } = {}) {
  const { context, i18n, values } = languageContext(language);
  const node = () => ({ innerHTML: '', textContent: '', attributes: {}, setAttribute(key, value) { this.attributes[key] = value; }, focus() {} });
  const nodes = Object.fromEntries(['app', 'view', 'main', 'skip', 'description', 'theme', 'menu', 'language', 'metar-loading-template'].map((key) => [key, node()]));
  nodes['metar-loading-template'].innerHTML = script('loading-view.html');
  nodes['toast-root'] = { append(message) { nodes[message.id] = message; } };
  const scrolls = [];
  const documentEvents = new Map();
  const windowEvents = new Map();
  const requests = [];
  const redirects = [];
  const localLocation = (url) => Object.assign(new URL(url, 'https://metar.uk'), { replace: (value) => redirects.push(value) });
  const operations = new Map();
  const document = {
    title: '', documentElement: { dataset: {}, lang: '', getAttribute(key) { return this.dataset[key.replace('data-', '')]; }, setAttribute(key, value) { this.dataset[key.replace('data-', '')] = value; } },
    body: { classList: { remove() {}, toggle() { return true; } } },
    getElementById: (id) => nodes[id] || null,
    createElement() { return {...node(), remove() { delete nodes[this.id]; }}; },
    querySelector: (selector) => ({
      'header.topbar': {set outerHTML(value) { nodes.app.innerHTML = nodes.app.innerHTML.replace(/<header class="topbar">[\s\S]*?<\/header>/, () => value); }},
      '[data-action="skip"]': nodes.skip, 'meta[name="description"]': nodes.description,
      '[data-action="theme"]': nodes.theme, '[data-action="menu"]': nodes.menu,
      '[data-action="language"]': nodes.language,
    })[selector] || null,
    querySelectorAll: () => [],
    addEventListener: (event, callback) => { const list = documentEvents.get(event) || []; list.push(callback); documentEvents.set(event, list); },
  };
  const question = { id: '42', title: content ? '未读' : 'User question', description: content ? '社区规范' : 'User summary', content: content ? '我的收藏' : 'User content', user_info: user, created_at: Math.floor(Date.now() / 1000) - 3600 };
  Object.assign(context, {
    document, location: localLocation('/latest'),
    URL, URLSearchParams, Headers, AbortController, setTimeout: (...args) => { const timer = setTimeout(...args); timer.unref(); return timer; }, clearTimeout,
    crypto: require('node:crypto').webcrypto,
    sessionStorage: {
      getItem: (key) => operations.get(key) || null,
      setItem: (key, value) => { if (storageBlocked) throw new Error('Blocked'); operations.set(key, value); },
      removeItem: (key) => operations.delete(key),
    },
    console: { warn() {}, error() {} },
    matchMedia: () => ({ matches: false }), scrollTo(options) { scrolls.push(options); },
    addEventListener: (event, callback) => windowEvents.set(event, callback),
    __METAR_RUNTIME_CONFIG__: JSON.parse(fs.readFileSync(path.join(SOURCE, '../config.production.json'), 'utf8')),
    fetch: async (url, options) => {
      requests.push({ url, options });
      const pathname = new URL(url, 'https://metar.uk').pathname;
      if (pathname === '/answer/api/v1/user/info' && identityGate) await identityGate;
      if (pathname.endsWith('/question/page') || pathname.endsWith('/tags/page')) {
        if (publicControl.gate) await publicControl.gate;
        if (publicControl.fail) return { ok:false, status:503, json:async () => ({data:null}) };
      }
      if (failure === 'all' || (failure === 'identity' && pathname.endsWith('/user/info'))) return { ok: false, status: 503, json: async () => ({ msg: '中文服务器错误', data: null }) };
      if (pathname.startsWith('/metar/api/admin/pulse/')) {
        if (adminError) return { ok: false, status: adminError, json: async () => ({ error: 'settings_unavailable' }) };
        return { ok: true, status: 200, json: async () => ({ revision: 1, config: { newapi_internal_base_url: 'http://new-api:3000', quota_per_unit: '500000', actions_enabled: false, reward_shadow_mode: true }, secrets: {}, worker_ready: true }) };
      }
      if (pathname.startsWith('/metar/api/pulse/')) {
        if (failure === 'pulse') return { ok: false, status: 503, json: async () => ({ error: 'pulse_unavailable' }) };
        let payload;
        if (pathname.endsWith('/summary')) payload = { available_tickets: tickets, current_contribution_milli: 1200500, level: { name: 'Member' } };
        else if (pathname.endsWith('/rules')) {
          payload = {selection_version:3,selection:'server-choice',draws:Array.from({length:Math.min(5,tickets)},(_,i)=>({selection:'server-choice-'+i,experience_only:false,quota_expires_at:'2026-11-01T00:00:00Z'})),ticket_count:tickets,can_draw_five:tickets>=5,queried_at:'2026-10-09T00:00:00Z',budgets:[],enabled:true,quota_per_unit:500000,total_weight:100,period:{id:1,config_version:'test-rule',key:'test-period',ends_at:'2026-11-01T00:00:00Z'},rewards:[{name:'Daily reward',amount:50000,weight:100}],...pulseRules};
        }
        else if (pathname.endsWith('/rewards')) payload = { rewards: new URL(url, 'https://metar.uk').searchParams.has('action_id') ? [] : pulseRewards };
        else if (pathname.endsWith('/actions')) {
          if (actionResult === 'timeout') throw new Error('Lost response');
          if (actionResult === 'action_rejected' || (rejectAt && requests.filter(r=>r.url.endsWith('/actions')).length === rejectAt)) return { ok: false, status: 409, json: async () => ({ error: 'action_rejected' }) };
          payload = { grant_id: 'grant-'+JSON.parse(options.body).action_id, action_id: JSON.parse(options.body).action_id, status: actionResult, reward_type: 'newapi_quota', amount: 50000 };
        }
        return { ok: true, status: 200, json: async () => payload };
      }
      let data = { count: 0, list: [] };
      if (pathname === '/answer/api/v1/user/info') data = user;
      else if (pathname.endsWith('/personal/user/info')) data = { ...user, bio: content ? '元衡账号绑定' : '' };
      else if (pathname.endsWith('/question/info')) data = question;
      else if (pathname.endsWith('/personal/collection/page')) data = { count: bookmarkCount, list: bookmarkCount ? [question] : [] };
      else if (pathname.endsWith('/connector/user/info')) data = binding === 'unavailable' ? [] : [{ link: '/answer/api/v1/connector/login/pulse_user_center', binding: binding === 'bound' }];
      else if (pathname.endsWith('/personal/question/page')) data = { count: 1, list: [{ question_id: question.id, title: question.title }] };
      else if (content && pathname.endsWith('/question/page')) data = { count: 1, list: [question] };
      return { ok: true, status: 200, json: async () => ({ data }) };
    },
  });
  context.history = {
    replaceState(_state, _title, url) { context.location = localLocation(url); },
    pushState(_state, _title, url) { context.location = localLocation(url); },
  };
  context.MetarLoading = require('../src/loading.js');
  for (const name of ['theme.js', 'adapters.js', 'avatars.js', 'growth.js', 'admin-pulse.js', 'route-policy.js', 'router.js', 'pulse-core.js', 'app.js']) vm.runInContext(script(name), context);
  await new Promise(setImmediate);
  return {
    i18n, values, document, nodes, requests, operations, redirects, scrolls,
    restore() { return windowEvents.get('pageshow')({persisted:true}); },
    async navigate(route) { context.location = localLocation(route); await windowEvents.get('popstate')(); },
    async changeLanguage(value) {
      for (const listener of documentEvents.get('change') || []) listener({ target: { value, matches: () => true } });
      await new Promise(setImmediate);
    },
    async click(action, groupId = "1:false") {
      for (const listener of documentEvents.get('click') || []) listener({ target: { closest: () => ({ dataset: { action, groupId } }) } });
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

test('历史恢复在请求期间保留公开内容与滚动，失败提示可重试且不清空列表', async () => {
  const publicControl = {};
  const view = await shell({content:true, language:'zh_CN', publicControl});
  const previous = view.nodes.view.innerHTML;
  const scrollCount = view.scrolls.length;
  let release;
  publicControl.gate = new Promise(resolve => { release = resolve; });
  view.restore();
  assert.equal(view.nodes.view.innerHTML, previous);
  assert.equal(view.scrolls.length, scrollCount);
  assert.match(view.nodes['view-refresh-message'].innerHTML, /正在更新内容/);
  publicControl.fail = true;
  release();
  await new Promise(setImmediate);
  assert.equal(view.nodes.view.innerHTML, previous);
  assert.match(view.nodes['view-refresh-message'].innerHTML, /正在显示返回前的内容/);
  publicControl.fail = false;
  await view.click('retry');
  assert.equal(view.nodes['view-refresh-message'], undefined);
  assert.equal(view.scrolls.length, scrollCount);
});

test('身份变化、私有页面不保留旧内容，恢复中的迟到响应不覆盖新路由', async () => {
  const publicControl = {};
  const view = await shell({content:true, publicControl, user:{id:'7',username:'alice',display_name:'Alice',mail_status:1}});
  let release;
  publicControl.gate = new Promise(resolve => { release = resolve; });
  view.values.set('_a_ltk_', 'new-test-session');
  view.restore();
  assert.match(view.nodes.view.innerHTML, /class="page-loading"/);
  assert.doesNotMatch(view.nodes.view.innerHTML, /discussion-title/);
  await view.navigate('/knowledge');
  const newer = view.nodes.view.innerHTML;
  release();
  await new Promise(setImmediate);
  assert.equal(view.nodes.view.innerHTML, newer);
  assert.equal(view.nodes['view-refresh-message'], undefined);
  await view.navigate('/me');
  assert.match(view.nodes.view.innerHTML, /Alice/);
  view.restore();
  assert.match(view.nodes.view.innerHTML, /class="page-loading"/);
  assert.doesNotMatch(view.nodes.view.innerHTML, /Alice/);
  await new Promise(setImmediate);
});

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
  assert.equal(view.redirects.at(-1), '/questions/42');
  await view.navigate('/me');
  assert.match(view.html(), /元衡账号绑定/);
  await view.changeLanguage('zh_CN');
  assert.equal(view.document.documentElement.lang, 'zh-CN');
  assert.match(view.html(), /编辑资料/);
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
  assert.match(view.html(), /0\.1 ⚡️/);
  assert.match(view.html(), /Probability 100 \/ 100/);
  assert.match(view.html(), /Credited/);
  assert.match(view.html(), /Awaiting processing/);
  await view.changeLanguage('zh_CN');
  assert.match(view.html(), /可用脉冲券：2/);
  assert.match(view.html(), /0\.1 ⚡️/);

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
    ['pending', 'Your reward is being delivered in the background', '奖励将在后台发放'],
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


test('five-draw fallback submits five unique actions, refuses insufficient tickets and stops on definite rejection', async () => {
  const view=await shell({user:pulseUser,binding:'bound',tickets:5});
  await view.navigate('/pulse');await view.click('pulse-draw-five');
  const actions=view.requests.filter(r=>r.url.endsWith('/actions'));
  assert.equal(actions.length,5);
  assert.equal(new Set(actions.map(r=>r.options.headers.get('Idempotency-Key'))).size,5);
  assert.equal(view.operations.size,0);assertEnglish(view,'five draws');
  const insufficient=await shell({user:pulseUser,binding:'bound',tickets:4});
  await insufficient.navigate('/pulse');await insufficient.click('pulse-draw-five');
  assert.equal(insufficient.requests.filter(r=>r.url.endsWith('/actions')).length,0);
  const partial=await shell({user:pulseUser,binding:'bound',tickets:5,rejectAt:3});
  await partial.navigate('/pulse');await partial.click('pulse-draw-five');
  assert.equal(partial.requests.filter(r=>r.url.endsWith('/actions')).length,3);
  assert.equal(partial.operations.size,0);
  assert.match(partial.html(),/Confirmed rewards are kept/);assertEnglish(partial,'partial draw');
});

test('Pulse 管理入口仅对 Answer 正常激活管理员显示，版主和伪造 is_admin 无权展示', async () => {
  for (const role_id of [1, 3, 0, undefined]) {
    const view = await shell({ user: { id: '8', role_id, is_admin: true, status: 'normal', mail_status: 1 } });
    assert.doesNotMatch(view.nodes.app.innerHTML, /href="\/admin\/(?:pulse|dashboard|pulse_user_center)"/);
    await view.navigate('/admin/pulse');
    assert.match(view.html(), /Administrator access required/);
    assert.equal(view.requests.some(({ url }) => url.startsWith('/metar/api/admin/')), false);
  }
  for (const user of [{ role_id: 2, status: 'suspended', mail_status: 1 }, { role_id: 2, status: 'normal', mail_status: 2 }]) {
    const view = await shell({ user });
    assert.doesNotMatch(view.nodes.app.innerHTML, /href="\/admin\/(?:pulse|dashboard|pulse_user_center)"/);
  }
  const view = await shell({ user: { id: '8', role_id: 2, status: 'normal', mail_status: 1 } });
  assert.match(view.nodes.app.innerHTML, /href="\/admin\/pulse"/);
  assert.match(view.nodes.app.innerHTML, /href="\/admin\/dashboard"/);
  assert.match(view.nodes.app.innerHTML, /href="\/admin\/pulse_user_center"/);
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

test('统一入口打开原生互动页，旧详情不再读取只读副本，身份服务故障不阻断交接', async () => {
  const view = await shell({ user: { username: 'alice', display_name: 'Alice', mail_status: 1 }, content: true });
  assert.match(view.html(), /href="\/questions\/42" class="discussion-title"/);
  assert.match(view.html(), /href="\/users\/alice" class="discussion-author"/);
  assert.match(view.html(), /href="\/users\/notifications\/inbox"/);
  await view.navigate('/question/42?commentId=9#answer-7');
  assert.equal(view.redirects.at(-1), '/questions/42?commentId=9#answer-7');
  assert.ok(!view.requests.some(({ url }) => url.includes('/question/info') || url.includes('/answer/page')));
  const outage = await shell({ failure: 'identity' });
  await outage.navigate('/me/notifications');
  assert.equal(outage.redirects.at(-1), '/users/notifications/inbox');
});

test('账号页保留公开和私人入口，故障时旧登录发布路由仍交给原生处理', async () => {
  const view = await shell({ user: { username: 'alice', display_name: 'Alice', mail_status: 1 } });
  await view.navigate('/me');
  assert.match(view.html(), /href="\/users\/alice"[^>]*>Public profile/);
  assert.match(view.html(), /href="\/users\/settings\/profile"/);
  assert.doesNotMatch(view.html(), /Answer question list|Edit Answer profile|questions\/undefined/);
  assert.match(view.html(), /href="\/questions\/42" class="discussion-title"/);
  const outage = await shell({ failure: 'identity' });
  for (const [entry, target] of [['/login', '/users/login'], ['/register', '/users/register'], ['/forgot', '/users/account-recovery'], ['/publish', '/questions/ask']]) {
    await outage.navigate(entry);
    assert.equal(outage.redirects.at(-1), target);
  }
});


test('收藏分页使用当前会话用户名和真实页数，后续页仍可访问完整详情', async () => {
  const view = await shell({ user: { username: 'alice', mail_status: 1 }, bookmarkCount: 42 });
  await view.navigate('/me/bookmarks?page=2');
  assert.match(view.html(), /Page 2 of 3/);
  assert.match(view.html(), /href="\/me\/bookmarks\?page=1"/);
  assert.match(view.html(), /href="\/me\/bookmarks\?page=3"/);
  assert.match(view.html(), /href="\/questions\/42"/);
  const request = view.requests.find(({ url }) => url.includes('/personal/collection/page'));
  const url = new URL(request.url, 'https://metar.uk');
  assert.equal(url.searchParams.get('username'), 'alice');
  assert.equal(url.searchParams.get('page'), '2');
});

test('社区经验奖项与到账记录保留 EXP 单位，不按 quota 汇率转换', async()=>{
 const view=await shell({user:pulseUser,binding:'bound',pulseRules:{rewards:[{name:'EXP prize',reward_type:'community_exp',amount:500,weight:100}]},pulseRewards:[{grant_id:'exp1',reward_type:'community_exp',amount:500,status:'settled'}]});
 await view.navigate('/pulse');
 assert.equal((view.html().match(/500 EXP/g)||[]).length,2);
 assert.doesNotMatch(view.html(),/0\.001 API/);
 await view.changeLanguage('zh_CN');
 assert.equal((view.html().match(/500 EXP/g)||[]).length,2);
});


test('公开列表与身份查询并行，身份完成只更新导航，不重载已显示内容', async () => {
  let release;
  const identityGate = new Promise(resolve => { release = resolve; });
  const view = await shell({identityGate, user:pulseUser, content:true});
  assert.ok(view.requests.some(r => r.url.includes('/question/page?')));
  assert.match(view.nodes.app.innerHTML, /Checking account/);
  assert.match(view.nodes.view.innerHTML, /<h3>未读<\/h3>/);
  const content = view.nodes.view.innerHTML;
  release(); await new Promise(setImmediate);
  assert.equal(view.nodes.view.innerHTML, content);
  assert.doesNotMatch(view.nodes.app.innerHTML, /Checking account/);
  assert.equal(view.requests.filter(r => r.url.includes('/question/page?')).length, 1);
});

test('身份仍在读取时，权益页等待认证，不提前请求绑定或财务接口', async () => {
  let release;
  const identityGate = new Promise(resolve => { release = resolve; });
  const view = await shell({identityGate, user:pulseUser, binding:'bound'});
  const navigation = view.navigate('/pulse');
  await new Promise(setImmediate);
  assert.equal(view.requests.filter(r => /\/metar\/api\/pulse|connector\/user/.test(r.url)).length, 0);
  release(); await navigation;
  assert.ok(view.requests.some(r => r.url.includes('/metar/api/pulse/summary')));
});

test('unified tickets draw across batches without a group selector', async () => {
  const view=await shell({user:pulseUser,binding:'bound',tickets:5});
  await view.navigate('/pulse');
  assert.doesNotMatch(view.html(),/pulse-select-group/);
  await view.click('pulse-draw-five');
  const calls=view.requests.filter(r=>r.url.endsWith('/actions'));
  assert.equal(calls.length,5);
  assert.deepEqual(calls.map(r=>JSON.parse(r.options.body).selection),Array.from({length:5},(_,i)=>'server-choice-'+i));
  assert.ok(calls.every(r=>JSON.parse(r.options.body).protocol_version===3));
  assertEnglish(view,'unified draw');
});
