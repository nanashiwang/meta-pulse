'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');

const source = path.resolve(__dirname, '../src/adapters.js');
global.window = global;
window.localStorage = {
  values: new Map([['_a_ltk_', 'answer-token']]),
  getItem(key) { return this.values.get(key) || null; },
};
require(source);

const jsonResponse = (data, status = 200) => new Response(JSON.stringify({ code: status, data }), {
  status,
  headers: { 'content-type': 'application/json' },
});

test('Answer Adapter 只请求同源 API 并携带 Answer token', async () => {
  let request;
  global.fetch = async (url, options) => {
    request = { url, options };
    return jsonResponse({ count: 0, list: [] });
  };
  const client = new window.MetarAdapters.AnswerAdapter({ answerApiBase: 'https://evil.example/api' });
  await client.listQuestions({ page: 2, pageSize: 5, order: 'unanswered', tag: 'agent' });
  assert.match(request.url, /^\/answer\/api\/v1\/question\/page\?/);
  assert.match(request.url, /page=2/);
  assert.match(request.url, /tag=agent/);
  assert.equal(request.options.credentials, 'same-origin');
  assert.equal(request.options.headers.get('Authorization'), 'Bearer answer-token');
});

test('绑定状态只读取 Pulse Connector，不接受浏览器 user_id', async () => {
  global.fetch = async () => jsonResponse([
    { name: 'GitHub', link: '/answer/api/v1/connector/login/github', binding: true, external_id: 'github-user' },
    { name: '元衡', link: '/answer/api/v1/connector/login/pulse_user_center', binding: true, external_id: '42' },
  ]);
  const client = new window.MetarAdapters.AnswerAdapter({ answerApiBase: '/answer/api/v1' });
  const result = await client.getBindingState();
  assert.equal(result.status, 'bound');
  assert.match(result.connector.link, /pulse_user_center/);
});

test('异常响应失败关闭，不构造空业务数据', async () => {
  global.fetch = async () => new Response('not-json', { status: 200 });
  const client = new window.MetarAdapters.AnswerAdapter({ answerApiBase: '/answer/api/v1' });
  await assert.rejects(() => client.getCurrentUser(), (error) => error.code === 'invalid_response');
});

test('身份读取失败与真实访客状态严格区分', async () => {
  const failure = new window.MetarAdapters.AdapterError('身份服务超时', { code: 'timeout' });
  const unavailable = await window.MetarAdapters.loadIdentitySnapshot(async () => { throw failure; });
  assert.equal(unavailable.state, 'unavailable');
  assert.equal(unavailable.user, null);
  assert.equal(unavailable.error, failure);

  const guest = await window.MetarAdapters.loadIdentitySnapshot(async () => null);
  assert.deepEqual(guest, { state: 'ready', user: null, error: null });

  const expired = await window.MetarAdapters.loadIdentitySnapshot(async () => {
    throw new window.MetarAdapters.AdapterError('登录已过期', { code: 'unauthorized' });
  });
  assert.deepEqual(expired, { state: 'ready', user: null, error: null });
});

test('导航状态能区分待回答筛选与话题页', () => {
  const active = window.MetarAdapters.routeMatchesNavigation;
  assert.equal(active('/questions', 'order=unanswered', '/questions?order=unanswered'), true);
  assert.equal(active('/questions', 'order=unanswered', '/questions'), false);
  assert.equal(active('/questions', 'order=active', '/questions'), true);
  assert.equal(active('/question/42', '', '/questions'), true);
  assert.equal(active('/topics', '', '/topics'), true);
  assert.equal(active('/topic/agent', '', '/topics'), true);
});


test('抽奖请求仅包含操作编号，超时后保留同一幂等键', async () => {
  window.sessionStorage = { values: new Map(), getItem(key) { return this.values.get(key) || null; }, setItem(key, value) { this.values.set(key, value); }, removeItem(key) { this.values.delete(key); } };
  const store = new window.MetarAdapters.PulseOperation('user-a');
  const first = store.begin();
  assert.deepEqual(store.begin(), first);
  let request;
  global.fetch = async (url, options) => { request = { url, options }; throw new Error('lost response'); };
  const pulse = new window.MetarAdapters.PulseAdapter(new window.MetarAdapters.AnswerAdapter({}));
  await assert.rejects(() => pulse.act(first), (error) => error.code === 'action_pending');
  assert.equal(request.url, '/metar/api/pulse/actions');
  assert.equal(request.options.headers.get('X-Metar-Request'), '1');
  assert.equal(request.options.headers.get('Idempotency-Key'), first.idempotencyKey);
  assert.deepEqual(JSON.parse(request.options.body), { action_id: first.actionId });
  assert.deepEqual(store.read(), first);
  assert.equal(new window.MetarAdapters.PulseOperation('user-b').read(), null);
  store.clear();
  assert.notEqual(store.begin().actionId, first.actionId);
});

test('站点存储失败时不能发起不可恢复抽奖', () => {
  window.sessionStorage = { getItem() { return null; }, setItem() { throw new Error('blocked'); } };
  assert.throws(() => new window.MetarAdapters.PulseOperation('user').begin(), (error) => error.code === 'storage_unavailable');
});

test('额度仅按配置的整数单位展示，不猜测币种', () => {
  const format = window.MetarAdapters.formatPulseQuota;
  assert.equal(format(50000, 500000), '0.1 API 额度');
  assert.equal(format(1, 500000), '0.000002 API 额度');
  assert.equal(format(10, 0), '10 quota');
  assert.equal(format(2 ** 53, 500000), '待核对');
});
