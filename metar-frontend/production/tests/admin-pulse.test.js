'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const path = require('node:path');
const source = (name) => fs.readFileSync(path.join(__dirname, '../src', name), 'utf8');

function harness() {
  const writes = [];
  const context = vm.createContext({ URL, URLSearchParams, Headers, AbortController, setTimeout, clearTimeout,
    crypto: require('node:crypto').webcrypto,
    localStorage: { getItem: () => 'answer-token', setItem: (...args) => writes.push(args) },
    sessionStorage: { setItem: (...args) => writes.push(args) },
    navigator: { clipboard: { writeText: async () => {} } },
    FormData: class {
      constructor(form) { this.values = new Map(Object.entries(form.inputs).filter(([, v]) => v.type !== 'checkbox' || v.checked).map(([k, v]) => [k, v.value])); }
      get(key) { return this.values.get(key) || null; }
      has(key) { return this.values.has(key); }
    },
  });
  context.window = context;
  for (const name of ['i18n.js', 'adapters.js', 'admin-pulse.js']) vm.runInContext(source(name), context);
  const keys = context.MetarAdapters.PULSE_ADMIN_SECRET_KEYS;
  const snapshot = (revision = 1) => ({ revision, config: { newapi_internal_base_url: 'http://new-api:3000', quota_per_unit: '500000', actions_enabled: false, reward_shadow_mode: true }, secrets: Object.fromEntries(keys.map((key) => [key, { configured: true, source: 'environment' }])), worker_ready: true });
  const nodes = new Map();
  const node = () => ({ disabled: false, textContent: '', innerHTML: '', hidden: false, classList: { toggle() {} } });
  const inputs = Object.fromEntries(keys.flatMap((key) => [[key, { type: 'password', value: '' }], ...(key.endsWith('_PREVIOUS') ? [[`clear_${key}`, { type: 'checkbox', checked: false, value: 'on' }]] : [])]));
  Object.assign(inputs, {
    newapi_internal_base_url: { value: 'http://new-api:3000' }, quota_per_unit: { value: '500000' },
    actions_enabled: { type: 'checkbox', checked: false }, reward_shadow_mode: { type: 'checkbox', checked: true }, reason: { value: 'Test paired configuration' },
  });
  const form = { inputs, elements: { namedItem: (key) => inputs[key] }, querySelectorAll: () => [], querySelector: (selector) => {
    if (!nodes.has(selector)) nodes.set(selector, node());
    return nodes.get(selector);
  } };
  context.document = { querySelector: () => form };
  const answer = new context.MetarAdapters.AnswerAdapter({});
  const adapter = new context.MetarAdapters.PulseAdminAdapter(answer);
  const view = new context.MetarPulseAdmin.View(adapter);
  return { context, writes, snapshot, adapter, view, form, keys, nodes };
}
const response = (data, status = 200) => ({ ok: status >= 200 && status < 300, status, json: async () => data });

test('管理员请求只走同源 Answer 认证，不读回服务密钥且 quota 保持精确字符串', async () => {
  const h = harness(); let request;
  h.context.fetch = async (url, options) => {
    request = { url, options };
    const value = h.snapshot(); value.config.quota_per_unit = '9007199254740993';
    value.secrets.PULSE_ADMIN_HMAC_SECRET.value = 'DO_NOT_DISPLAY';
    value.PULSE_SERVICE_HMAC_SECRET = 'DO_NOT_DISPLAY';
    return response(value);
  };
  const result = await h.adapter.settings();
  assert.equal(request.url, '/metar/api/admin/pulse/settings');
  assert.equal(request.options.headers.get('Authorization'), 'Bearer answer-token');
  assert.equal(request.options.headers.get('X-Metar-Request'), '1');
  assert.equal(request.options.credentials, 'same-origin');
  assert.equal(request.options.cache, 'no-store');
  assert.equal(request.options.redirect, 'error');
  assert.equal(result.config.quota_per_unit, '9007199254740993');
  assert.doesNotMatch(JSON.stringify(result), /DO_NOT_DISPLAY/);
  assert.equal(h.writes.length, 0);
});

test('输入密钥只写；空白保留，上一槽清除显式提交且拒绝边清除边改写', () => {
  const h = harness(); h.view.snapshot = h.snapshot();
  h.form.inputs.PULSE_ADMIN_HMAC_SECRET.value = 'a'.repeat(64);
  h.form.inputs.clear_PULSE_ADMIN_HMAC_SECRET_PREVIOUS.checked = true;
  h.form.inputs.quota_per_unit.value = '9007199254740993';
  const body = h.view.payload(h.form);
  assert.deepEqual(Object.keys(body.secrets), ['PULSE_ADMIN_HMAC_SECRET']);
  assert.equal(body.quota_per_unit, undefined);
  assert.equal(body.config.quota_per_unit, '9007199254740993');
  assert.deepEqual(Array.from(body.clear_secrets), ['PULSE_ADMIN_HMAC_SECRET_PREVIOUS']);
  h.form.inputs.PULSE_ADMIN_HMAC_SECRET_PREVIOUS.value = 'b'.repeat(64);
  assert.throws(() => h.view.payload(h.form), (error) => error.code === 'invalid_request');
  h.form.inputs.PULSE_ADMIN_HMAC_SECRET_PREVIOUS.value = '';
  for (const value of ['0', '-1', '1.5', '1e6', '9223372036854775808']) {
    h.form.inputs.quota_per_unit.value = value;
    assert.throws(() => h.view.payload(h.form), (error) => error.code === 'invalid_request');
  }
  assert.equal(h.writes.length, 0);
});

test('保存响应丢失保留原 payload/key，仅确认成功后清除密钥输入', async () => {
  const h = harness(); h.view.snapshot = h.snapshot(); const requests = [];
  h.form.inputs.PULSE_SERVICE_HMAC_SECRET.value = 'a'.repeat(64);
  h.context.fetch = async (url, options) => { requests.push({ url, options }); if (requests.length === 1) throw new Error('lost response with secret'); return response(h.snapshot(2)); };
  await h.view.submit(h.form);
  assert.ok(h.view.pending);
  assert.equal(h.form.querySelector('fieldset').disabled, true);
  assert.equal(h.form.inputs.PULSE_SERVICE_HMAC_SECRET.value, 'a'.repeat(64));
  await h.view.submit(h.form); // No new request may be generated while the original save is uncertain.
  assert.equal(requests.length, 1);
  await h.view.action('admin-save-retry');
  assert.equal(requests.length, 2);
  assert.equal(requests[0].options.body, requests[1].options.body);
  assert.equal(requests[0].options.headers.get('Idempotency-Key'), requests[1].options.headers.get('Idempotency-Key'));
  assert.match(requests[0].options.headers.get('Idempotency-Key'), /^[a-f0-9-]{36}$/);
  assert.equal(h.form.inputs.PULSE_SERVICE_HMAC_SECRET.value, '');
  assert.equal(h.view.pending, null);
  assert.equal(h.view.snapshot.revision, 2);
  assert.equal(h.form.querySelector('fieldset').disabled, false);
  assert.equal(h.writes.length, 0);
});

test('版本冲突不自动重试，重新读取保留输入并要求再次明确保存', async () => {
  const h = harness(); h.view.snapshot = h.snapshot(); const requests = [];
  h.form.inputs.PULSE_USER_BFF_HMAC_SECRET.value = 'b'.repeat(64);
  h.context.fetch = async (url, options) => {
    requests.push({ url, options });
    if (options.method === 'GET') { const latest = h.snapshot(4); latest.config.quota_per_unit = '1000000'; return response(latest); }
    if (requests.length === 1) return response({ error: 'settings_conflict', message: 'Do not echo a secret' }, 409);
    return response(h.snapshot(5));
  };
  await h.view.submit(h.form);
  assert.equal(h.view.conflict, true);
  assert.equal(h.view.pending, null);
  await h.view.submit(h.form);
  assert.equal(requests.length, 1);
  await h.view.action('admin-config-refresh');
  assert.equal(h.view.snapshot.revision, 4);
  assert.equal(h.form.inputs.PULSE_USER_BFF_HMAC_SECRET.value, 'b'.repeat(64));
  assert.match(h.form.querySelector('[data-admin-message]').textContent, /1000000/);
  assert.doesNotMatch(h.form.querySelector('[data-admin-message]').textContent, /Do not echo/);
  await h.view.submit(h.form);
  assert.equal(JSON.parse(requests[2].options.body).revision, 4);
  assert.notEqual(requests[0].options.headers.get('Idempotency-Key'), requests[2].options.headers.get('Idempotency-Key'));
});

test('生成密钥不自动保存，不覆盖尚未保存的输入，离开页面清除内存请求', async () => {
  const h = harness(); const requests = [];
  h.context.fetch = async (url, options) => { requests.push({ url, options }); return response({ secret: 'c'.repeat(64) }); };
  const target = { dataset: { key: 'PULSE_ROLLBACK_HMAC_SECRET' } };
  await h.view.action('admin-secret-generate', target);
  assert.equal(h.form.inputs.PULSE_ROLLBACK_HMAC_SECRET.value, 'c'.repeat(64));
  assert.equal(requests[0].url, '/metar/api/admin/pulse/secret');
  await h.view.action('admin-secret-generate', target);
  assert.equal(requests.length, 1);
  h.view.pending = { body: { secrets: { a: 'secret' } }, key: 'request' };
  h.view.dispose();
  assert.equal(h.view.pending, null);
  assert.equal(h.writes.length, 0);
});

test('离开页面后的迟到保存响应不能恢复敏感状态或修改下一页面', async () => {
  const h = harness(); h.view.snapshot = h.snapshot();
  let complete;
  h.context.fetch = () => new Promise((resolve) => { complete = resolve; });
  const save = h.view.submit(h.form);
  h.view.dispose();
  const message = h.form.querySelector('[data-admin-message]').textContent;
  complete(response(h.snapshot(2))); await save;
  assert.equal(h.view.pending, null);
  assert.equal(h.view.snapshot, null);
  assert.equal(h.form.querySelector('[data-admin-message]').textContent, message);
});

test('冲突重新读取后目标已锁定时同步权威地址，其他输入仍保留且可继续保存', async () => {
  const h = harness(); h.view.snapshot = h.snapshot(); h.view.conflict = true;
  h.form.inputs.newapi_internal_base_url.value = 'http://stale-target:3000';
  h.form.inputs.quota_per_unit.value = '800000';
  h.form.inputs.PULSE_ADMIN_HMAC_SECRET.value = 'd'.repeat(64);
  const latest = h.snapshot(7);
  latest.newapi_target_locked = true;
  latest.config.newapi_internal_base_url = 'http://locked-target:3000';
  h.context.fetch = async () => response(latest);
  await h.view.action('admin-config-refresh');
  assert.equal(h.form.inputs.newapi_internal_base_url.readOnly, true);
  assert.equal(h.form.inputs.newapi_internal_base_url.value, 'http://locked-target:3000');
  assert.equal(h.form.inputs.quota_per_unit.value, '800000');
  assert.equal(h.form.inputs.PULSE_ADMIN_HMAC_SECRET.value, 'd'.repeat(64));
  const payload = h.view.payload(h.form);
  assert.equal(payload.revision, 7);
  assert.equal(payload.config.newapi_internal_base_url, 'http://locked-target:3000');
});
