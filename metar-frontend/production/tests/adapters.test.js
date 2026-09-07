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
