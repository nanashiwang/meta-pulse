const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const {webcrypto} = require('node:crypto');

function setup() {
  const records = new Map();
  const window = {
    MetarI18n: {getLanguage: () => 'zh_CN'}, crypto: webcrypto,
    sessionStorage: {getItem: k => records.get(k) ?? null, setItem: (k, v) => records.set(k, v), removeItem: k => records.delete(k)},
  };
  vm.runInNewContext(fs.readFileSync(require.resolve('../src/admin-models.js'), 'utf8'), {window, URL, TextEncoder, Date, JSON, Number, Object, String, Array, Error, setTimeout});
  return {api: window.MetarModelsAdmin, records, window};
}

const overview = {counts: {providers: 2, models: 3}, snapshot: {rows: 3, method_version: 'v1', computed_at: '2026-11-08T00:00:00Z'},
  imports: [{id: 1, source: '<b>evil</b>', actor_id: '', catalog_rows: 5, observations_added: 4, observations_skipped: 0, created_at: '2026-11-08T00:00:00Z'}]};

function fetcher(handlers) {
  const calls = [];
  const fn = async (op, opts = {}) => { calls.push({op, ...opts}); const h = handlers[op]; if (h instanceof Error) throw h; return typeof h === 'function' ? h(opts, calls) : h; };
  fn.calls = calls;
  return fn;
}

test('overview renders counts and escapes import sources', async () => {
  const {api} = setup();
  const html = await new api.View(null, fetcher({overview})).page({id: '7'});
  assert.match(html, /模型榜管理/);
  assert.match(html, /&lt;b&gt;evil&lt;\/b&gt;/);
  assert.doesNotMatch(html, /<b>evil<\/b>/);
  assert.match(html, /命令行/, 'CLI imports have no operator');
  assert.match(html, /data-intel-admin="import" disabled/, 'import disabled before validation');
});

test('unavailable service shows a clear state', async () => {
  const {api} = setup();
  const html = await new api.View(null, fetcher({overview: new api.AdminError('forbidden', 403)})).page({id: '7'});
  assert.match(html, /需要当前有效的管理员账号/);
});

test('validation gates import and binds the key to the exact text', async () => {
  const {api} = setup();
  const f = fetcher({overview, validate: {valid: true, summary: {models: 1}}, import: {stats: {catalog_rows: 1}}});
  const v = new api.View(null, f);
  await v.page({id: '7'});
  v.setText('{"source":{"slug":"a"}}');
  await assert.rejects(v.importValidated(), e => e.code === 'invalid_document');
  await v.validate();
  assert.match(v.render(), /校验通过/);
  assert.doesNotMatch(v.render(), /data-intel-admin="import" disabled/);
  v.setText('{"source":{"slug":"b"}}');
  assert.equal(v.validated, null, 'editing after validation requires validating again');
  await assert.rejects(v.importValidated());
  await v.validate();
  const key = v.validated.key;
  await v.importValidated();
  const imp = f.calls.find(c => c.op === 'import');
  assert.equal(imp.key, key);
  assert.equal(imp.body, '{"source":{"slug":"b"}}');
  assert.match(key, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
});

test('an unconfirmed import is retried with the same key and body after reload', async () => {
  const {api, records} = setup();
  let attempts = 0;
  const handlers = {overview, validate: {summary: {}}, import: () => { attempts++; if (attempts === 1) throw new api.AdminError('pending', 0); return {stats: {replayed: true}}; }};
  const first = new api.View(null, fetcher(handlers));
  await first.page({id: '7'});
  first.setText('{"source":{"slug":"a"}}');
  await first.validate();
  await assert.rejects(first.importValidated(), e => e.code === 'pending');
  assert.equal(records.size, 1);

  const f2 = fetcher(handlers);
  const reloaded = new api.View(null, f2);
  const html = await reloaded.page({id: '7'});
  assert.match(html, /重试原操作/);
  await reloaded.importValidated();
  const retry = f2.calls.find(c => c.op === 'import');
  assert.equal(retry.key, first.pending.key);
  assert.equal(retry.body, '{"source":{"slug":"a"}}');
  assert.equal(records.size, 0, 'confirmed result clears the pending record');

  const other = new api.View(null, fetcher(handlers));
  await other.page({id: '8'});
  assert.equal(other.pending, null, 'pending imports are per administrator');
});

test('client-side checks reject invalid JSON and oversized documents without a request', async () => {
  const {api} = setup();
  const f = fetcher({overview});
  const v = new api.View(null, f);
  await v.page({id: '7'});
  v.setText('{not json');
  await assert.rejects(v.validate(), e => e.code === 'invalid_json');
  v.setText('"' + 'x'.repeat(api.MAX_BYTES) + '"');
  await assert.rejects(v.validate(), e => e.code === 'document_too_large');
  assert.equal(f.calls.filter(c => c.op === 'validate').length, 0);
});

test('default fetch targets the admin BFF with session token and marker header', async () => {
  const {api, window} = setup();
  let seen;
  window.fetch = async (url, opts) => { seen = {url, opts}; return {ok: false, status: 422, json: async () => ({error: 'invalid_document', problems: ['models[0].name bad']})}; };
  const v = new api.View({token: () => 'abc'});
  await v.page({id: '7'});
  assert.equal(seen.url, '/answer/admin/api/metar/intel/overview');
  assert.equal(seen.opts.headers.Authorization, 'Bearer abc');
  assert.equal(seen.opts.headers['X-Metar-Request'], '1');
  assert.equal(seen.opts.credentials, 'same-origin');
  v.setText('{}');
  await assert.rejects(v.validate(), e => e.code === 'invalid_document' && e.problems[0] === 'models[0].name bad');
});
