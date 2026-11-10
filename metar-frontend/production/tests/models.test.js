const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function setup(language = 'zh_CN') {
  const calls = [];
  const window = {MetarI18n: {getLanguage: () => language}};
  window.fetch = async (url, options) => { calls.push({url, options}); throw Error('use injected fetcher'); };
  vm.runInNewContext(fs.readFileSync(require.resolve('../src/models.js'), 'utf8'), {window, URL, URLSearchParams, Date, Math, Number, String, Object, Error});
  return {api: window.MetarModels, calls, window};
}

const row = (o = {}) => ({rank: 1, model: 'alpha', name: 'Alpha', provider: 'lab', provider_name: 'Lab', status: 'available', license: 'proprietary', open_weights: false,
  release_date: '2026-01-01T00:00:00Z', context_tokens: 1000000, composite_bps: 8123, reasoning_bps: 9000, coding_bps: 7000, agent_bps: null, coverage_bps: 7500,
  input_micro_usd: 2000000, output_micro_usd: 10000000, blended_micro_usd: 4000000, output_tokens_per_sec_milli: null, ...o});
const home = (rows = [row()]) => ({computed_at: '2026-11-08T00:00:00Z', method_version: 'v1', model_count: rows.length, ranked_count: rows.length,
  leaders: [{category: 'reasoning', row: rows[0], value: 9000, unit: 'bps'}], preview: {rows, total: rows.length, page: 1, page_size: 15}, recent: []});

function fetcher(responses) {
  const seen = [];
  const fn = async path => { seen.push(path); const r = responses[path.split('?')[0]] ?? (path.startsWith('/leaderboard') ? responses['/home']?.preview : undefined); if (r instanceof Error) throw r; return typeof r === 'function' ? r(path) : r; };
  fn.seen = seen;
  return fn;
}

test('home renders leaders, table and FAQ with escaping and dashes for missing data', async () => {
  const {api} = setup();
  const evil = row({name: '<img src=x onerror=alert(1)>', provider_name: '<b>Lab</b>'});
  const html = await new api.View(fetcher({'/home': home([evil])})).page('/models', new URLSearchParams());
  assert.doesNotMatch(html, /<img src=x/);
  assert.doesNotMatch(html, /<b>Lab<\/b>/);
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/);
  assert.match(html, /81\.2/);
  assert.match(html, /\$4\.00/);
  assert.match(html, /1M/);
  assert.match(html, /<td class="intel-num">—<\/td>/, 'missing agent and speed show a dash');
  assert.match(html, /推理最强/);
  assert.match(html, /常见问题/);
});

test('empty, missing and unavailable states are distinct', async () => {
  const {api} = setup();
  const noSnap = new api.IntelError('no_snapshot', 404);
  assert.match(await new api.View(fetcher({'/home': noSnap})).page('/models'), /榜单数据准备中/);
  assert.match(await new api.View(fetcher({'/models/missing': new api.IntelError('not_found', 404)})).page('/model/missing'), /没有找到这个模型/);
  assert.match(await new api.View(fetcher({'/home': new api.IntelError('unavailable', 503)})).page('/models'), /暂时不可用/);
});

test('invalid model slugs never reach the API', async () => {
  const {api} = setup();
  const f = fetcher({});
  const html = await new api.View(f).page('/model/' + encodeURIComponent('../admin'), new URLSearchParams());
  assert.equal(f.seen.length, 0);
  assert.match(html, /没有找到这个模型/);
});

test('leaderboard query is whitelisted and canonical', async () => {
  const {api} = setup();
  const q = api.readQuery(new URLSearchParams('window=7d&sort=drop&order=sideways&open=yes&page=-3&utm=x'));
  assert.equal(api.encode(q), '');
  const good = api.readQuery(new URLSearchParams('window=30d&sort=price&order=asc&open=1&page=2'));
  assert.equal(api.encode(good), 'window=30d&sort=price&order=asc&open=1&page=2');
  assert.equal(api.encode(api.readQuery(new URLSearchParams('window=all&sort=score&order=desc'))), '', 'defaults are omitted');
});

test('leaderboard requests the canonical query and renders sort links and pagination', async () => {
  const {api} = setup();
  const f = fetcher({'/home': home(), '/leaderboard': {computed_at: '2026-11-08T00:00:00Z', method_version: 'v1', rows: [row()], total: 31, page: 2, page_size: 15}});
  const html = await new api.View(f).page('/models/leaderboard', new URLSearchParams('sort=price&page=2&junk=1'));
  assert.equal(f.seen[1], '/leaderboard?sort=price&page=2');
  assert.match(html, /aria-sort="ascending"/);
  assert.match(html, /href="\/models\?sort=price&amp;order=asc"/, 'clicking the active column flips order and resets page');
  assert.match(html, /16–30 \/ 31/);
  assert.match(html, /href="\/models\?sort=price&amp;page=3"/);
});

test('model detail shows sourced prices and results, and only https sources become links', async () => {
  const {api} = setup('en_US');
  const detail = {slug: 'alpha', name: 'Alpha', provider: 'lab', provider_name: 'Lab', provider_website: 'javascript:alert(1)', family: '', modality: 'llm', status: 'unreleased',
    license: 'apache-2.0', open_weights: true, release_date: null, context_tokens: 128000, max_output_tokens: 0, source_url: 'https://lab.example/alpha',
    offerings: [{offering: 'lab-alpha', provider: 'lab', provider_name: 'Lab', tier: 'standard', input_micro_usd: 50000, output_micro_usd: null, cache_read_micro_usd: null, effective_at: '2026-10-01T00:00:00Z', source_url: 'https://lab.example/pricing'}],
    benchmark_results: [{benchmark: 'b', name: 'Bench', dimension: 'reasoning', unit: 'percent', higher_is_better: false, value: 8730, settings: '', self_run: true, published_at: '2026-10-02T00:00:00Z', source_url: ''}],
    board: row({rank: null, composite_bps: null, coverage_bps: 4000})};
  const html = await new api.View(fetcher({'/models/alpha': detail})).page('/model/alpha');
  assert.match(html, /Unreleased/);
  assert.match(html, /apache-2\.0/);
  assert.match(html, /\$0\.050/);
  assert.match(html, /87\.3%/);
  assert.match(html, /lower is better/);
  assert.match(html, /Self-run/);
  assert.match(html, /source has no overall score/);
  assert.match(html, /href="https:\/\/lab\.example\/pricing"[^>]*rel="noopener noreferrer nofollow"/);
  assert.doesNotMatch(html, /javascript:/);
});

test('number formatting', () => {
  const {api} = setup();
  const f = api.fmt;
  assert.equal(f.bps(10000), '100.0');
  assert.equal(f.usd(0), '$0.00');
  assert.equal(f.usd(150000), '$0.15');
  assert.equal(f.usd(25000), '$0.025');
  assert.equal(f.tokens(2000000), '2M');
  assert.equal(f.tokens(1048576), '1.0M');
  assert.equal(f.tokens(128000), '128K');
  assert.equal(f.tokens(0), '—');
  assert.equal(f.speed(240000), '240 tok/s');
  assert.equal(f.bps(null), '—');
});

test('default fetch sends no credentials or referrer and maps errors', async () => {
  const {api, window} = setup();
  let seen;
  window.fetch = async (url, options) => { seen = {url, options}; return {ok: false, status: 404, json: async () => ({error: 'no_snapshot'})}; };
  const html = await new api.View().page('/models');
  assert.equal(seen.url, '/metar/api/intel/v1/home');
  assert.equal(seen.options.credentials, 'omit');
  assert.equal(seen.options.referrerPolicy, 'no-referrer');
  assert.match(html, /榜单数据准备中/);
  window.fetch = async () => { throw Error('offline'); };
  assert.match(await new api.View().page('/models'), /暂时不可用/);
});

test('home tolerates null leaders and recent from older API responses', async () => {
  const {api} = setup();
  const h = home([row()]);
  h.leaders = null; h.recent = null;
  const html = await new api.View(fetcher({'/home': h})).page('/models', new URLSearchParams());
  assert.match(html, /综合榜/);
  assert.doesNotMatch(html, /暂时不可用/);
});

test('model details retain source attribution', async () => {
  const {api} = setup();
  const detail = {slug: 'alpha', name: 'Alpha', provider: 'lab', provider_name: 'Lab', provider_website: '', family: '', modality: 'llm', status: 'available', license: 'proprietary', open_weights: false, release_date: null, context_tokens: 0, max_output_tokens: 0, source_url: '', offerings: [], benchmark_results: [], board: row(), sources: ['llm-stats']};
  const v = new api.View(fetcher({'/models/alpha': detail}));
  for (const path of ['/model/alpha']) {
    const html = await v.page(path, new URLSearchParams());
    assert.match(html, /<a class="intel-credit" href="https:\/\/llm-stats\.com" target="_blank" rel="noopener">Data by LLM Stats<\/a>/, path);
  }
  const plain = await new api.View(fetcher({'/home': home([row()])})).page('/models', new URLSearchParams());
  assert.doesNotMatch(plain, /Data by LLM Stats/, 'no credit without llm-stats data');
});


test('ranking pages omit intros and methodology links in both languages', async () => {
  for (const lang of ['zh_CN', 'en_US']) {
    const {api} = setup(lang);
    const h = home([row()]); h.sources = ['llm-stats'];
    const board = {...h, rows: [row()], total: 1, page: 1, page_size: 15};
    const v = new api.View(fetcher({'/home': h, '/leaderboard': board, '/leaderboard?open=1': board}));
    for (const [path, query] of [['/models', ''], ['/models/leaderboard', ''], ['/models/leaderboard', 'open=1'], ['/models/methodology', '']]) {
      const html = await v.page(path, new URLSearchParams(query));
      assert.doesNotMatch(html, /intel-hero|intel-updated|intel-attribution|models\/methodology/);
      assert.match(html, /Alpha/);
    }
  }
});

 test('home uses full board data, keeps filters while paging, and marks every sortable column', async () => {
  const {api} = setup();
  const f = fetcher({'/home': home(), '/leaderboard': {rows: [row({name: 'Second page model'})], total: 40, page: 2, page_size: 15}});
  const html = await new api.View(f).page('/models', new URLSearchParams('window=30d&sort=reasoning&order=asc&page=2'));
  assert.equal(f.seen[1], '/leaderboard?window=30d&sort=reasoning&order=asc&page=2');
  assert.match(html, /Second page model/);
  assert.match(html, /模型评分榜/);
  assert.doesNotMatch(html, /完整榜单|榜单首页/);
  assert.match(html, /推理 ↑/);
  for (const label of ['综合分', '编程', 'Agent', '上下文', '速度']) assert.ok(html.includes(label + ' ↕'));
  assert.match(html, /href="\/models\?window=30d&amp;sort=reasoning"/);
  assert.match(html, /href="\/models\?window=30d&amp;sort=reasoning&amp;order=asc&amp;page=3"/);
  assert.match(html, /16–30 \/ 40/);
});

test('open weights retain sorting and paging on the canonical home route', async () => {
  const {api} = setup();
  const f = fetcher({'/leaderboard': {rows: [row()], total: 31, page: 1, page_size: 15}});
  const html = await new api.View(f).page('/models', new URLSearchParams('open=1&sort=agent'));
  assert.equal(f.seen[0], '/leaderboard?sort=agent&open=1');
  assert.match(html, /Agent ↓/);
  assert.match(html, /sort=agent&amp;order=asc&amp;open=1/);
  assert.doesNotMatch(html, /完整榜单|榜单首页/);
});

 test('recent releases render as a separate module before the ranking, in both languages', async () => {
  for (const lang of ['zh_CN', 'en_US']) {
    const {api} = setup(lang);
    const h = home();
    h.recent = [row({model: 'new-model', name: '<New model>', provider_name: 'Example Lab', release_date: '2026-11-09T00:00:00Z'})];
    const html = await new api.View(fetcher({'/home': h})).page('/models');
    assert.ok(html.indexOf('intel-recent-module') < html.indexOf('intel-main'));
    assert.equal((html.match(/intel-recent-module/g) || []).length, 1);
    assert.match(html, /href="\/model\/new-model"/);
    assert.match(html, /&lt;New model&gt;/);
    assert.match(html, /Example Lab/);
    assert.match(html, /aria-hidden="true">EL/);
    assert.match(html, /2026/);
    assert.match(html, /15/);
    assert.match(html, /href="https:\/\/llm-stats.com\/llm-updates" target="_blank" rel="noopener noreferrer"/);
  }
});

test('recent module shows an honest empty state and keeps the updates link', async () => {
  const {api} = setup();
  const html = await new api.View(fetcher({'/home': home()})).page('/models');
  assert.match(html, /近 15 天没有新发布的模型/);
  assert.match(html, /全部更新/);
  assert.doesNotMatch(html, /intel-recent-card/);
});
