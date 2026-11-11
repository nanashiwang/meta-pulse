/* Model leaderboard pages. Data comes from the anonymous, read-only Intel API;
   no cookies or tokens are sent and nothing here writes. Missing values are
   shown as "—", never as zero. */
(function(root) {
  'use strict';
  const API = '/metar/api/intel/v1';
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const en = () => root.MetarI18n?.getLanguage() === 'en_US';
  const t = (zh, enText) => en() ? enText : zh;
  const DASH = '—';
  const link = (path, label, cls = '') => `<a data-router${cls ? ` class="${cls}"` : ''} href="${esc(path)}">${label}</a>`;
  const safeURL = value => { try { const u = new URL(value); return u.protocol === 'https:' && !u.username && !u.password ? u.href : ''; } catch (_) { return ''; } };
  const source = (url, label) => { const href = safeURL(url); return href ? `<a class="intel-source" href="${esc(href)}" target="_blank" rel="noopener noreferrer nofollow">${label} ↗</a>` : ''; };
  const slugOK = slug => /^[a-z0-9]+(?:[.-][a-z0-9]+)*$/.test(slug) && slug.length <= 80;

  // Display-only formatting of integer API values.
  const fmt = {
    bps: v => v == null ? DASH : (v / 100).toFixed(1),
    percent: v => v == null ? DASH : (v / 100).toFixed(1) + '%',
    points: v => v == null ? DASH : (v / 1000).toLocaleString(en() ? 'en-US' : 'zh-CN', {maximumFractionDigits: 3}),
    usd: v => {
      if (v == null) return DASH;
      const dollars = v / 1e6;
      return '$' + (dollars >= 0.1 || dollars === 0 ? dollars.toFixed(2) : dollars.toPrecision(2));
    },
    tokens: v => {
      if (v == null || v <= 0) return DASH;
      if (v >= 1e6) return (v / 1e6).toFixed(v % 1e6 ? 1 : 0) + 'M';
      if (v >= 1e3) return Math.round(v / 1e3) + 'K';
      return String(v);
    },
    speed: v => v == null ? DASH : Math.round(v / 1000).toLocaleString() + ' tok/s',
    date: v => {
      if (!v) return DASH;
      const d = new Date(v);
      return Number.isNaN(d.getTime()) ? DASH : d.toLocaleDateString(en() ? 'en-US' : 'zh-CN', {year: 'numeric', month: 'short', day: 'numeric', timeZone: 'UTC'});
    },
    time: v => {
      const d = new Date(v);
      return Number.isNaN(d.getTime()) ? DASH : d.toLocaleString(en() ? 'en-US' : 'zh-CN', {timeZone: 'Asia/Shanghai', hour12: false});
    },
  };
  const value = (unit, v) => unit === 'percent' ? fmt.percent(v) : fmt.points(v);
  const license = r => r.open_weights ? (r.license && r.license !== 'proprietary' ? esc(r.license) : t('开放权重', 'Open weights')) : t('专有', 'Proprietary');
  const dims = {reasoning: ['推理', 'Reasoning'], coding: ['编程', 'Coding'], agent: ['Agent', 'Agent'], other: ['其他', 'Other']};
  const dimLabel = d => dims[d] ? t(...dims[d]) : esc(d);

  class IntelError extends Error {
    constructor(code, status) { super(code); this.code = code; this.status = status; }
  }

  async function defaultFetch(path) {
    let response;
    try {
      response = await root.fetch(API + path, {credentials: 'omit', headers: {Accept: 'application/json'}, referrerPolicy: 'no-referrer'});
    } catch (_) { throw new IntelError('unavailable', 0); }
    let body = null;
    try { body = await response.json(); } catch (_) { /* handled below */ }
    if (!response.ok || !body) throw new IntelError(body?.error || 'unavailable', response.status);
    return body;
  }

  // Leaderboard query, validated client-side to the same whitelist as the API.
  const SORTS = ['score', 'reasoning', 'coding', 'agent', 'context', 'speed', 'price', 'release'];
  function readQuery(params) {
    const get = k => params?.get?.(k) || '';
    const q = {window: get('window'), sort: get('sort'), order: get('order'), open: get('open'), page: get('page')};
    if (!['', 'all', '90d', '30d'].includes(q.window)) q.window = '';
    if (!SORTS.includes(q.sort)) q.sort = '';
    if (!['', 'asc', 'desc'].includes(q.order)) q.order = '';
    if (q.open !== '1') q.open = '';
    const page = Number(q.page);
    q.page = Number.isInteger(page) && page > 1 && page <= 1000 ? String(page) : '';
    return q;
  }
  const encode = q => new URLSearchParams(Object.entries(q).filter(([k, v]) => v && !(k === 'window' && v === 'all') && !(k === 'sort' && v === 'score') && !(k === 'order' && v === 'desc'))).toString();
  const boardURL = q => { const s = encode(q); return '/models' + (s ? '?' + s : ''); };

  function badges(r) {
    const out = [];
    if (r.status === 'unreleased') out.push(`<span class="badge">${t('未发布', 'Unreleased')}</span>`);
    else if (r.release_date && Date.now() - new Date(r.release_date).getTime() <= 15 * 86400000) out.push(`<span class="badge green">NEW</span>`);
    if (r.open_weights) out.push(`<span class="badge">${t('开放', 'Open')}</span>`);
    return out.join('');
  }

  function table(rows, q, sortable) {
    const head = (key, label, cls = '') => {
      if (!sortable) return `<th class="${cls}" scope="col">${label}</th>`;
      const active = (q.sort || 'score') === key;
      const nextOrder = active && q.order !== 'asc' ? 'asc' : '';
      // The API's default price order is cheapest first.
      const ascending = (q.order === 'asc') !== (key === 'price');
      const arrow = active ? (ascending ? ' ↑' : ' ↓') : ' ↕';
      return `<th class="${cls}" scope="col"${active ? ` aria-sort="${ascending ? 'ascending' : 'descending'}"` : ''}>${link(boardURL({...q, sort: key, order: nextOrder, page: ''}), label + arrow, 'intel-sort')}</th>`;
    };
    const body = rows.length ? rows.map(r => `<tr>
      <td class="intel-rank">${r.rank ?? DASH}</td>
      <th scope="row" class="intel-model-cell">${link('/model/' + encodeURIComponent(r.model), esc(r.name), 'intel-model-name')}<span class="intel-badges">${badges(r)}</span></th>
      <td class="intel-provider">${esc(r.provider_name || r.provider)}</td>
      <td class="intel-num intel-score">${r.composite_bps == null ? `<span class="muted" title="${t('来源未提供综合评分', 'Source has no overall score')}">${DASH}</span>` : fmt.bps(r.composite_bps)}</td>
      <td class="intel-num">${fmt.bps(r.reasoning_bps)}</td>
      <td class="intel-num">${fmt.bps(r.coding_bps)}</td>
      <td class="intel-num">${fmt.bps(r.agent_bps)}</td>
      <td class="intel-num">${fmt.tokens(r.context_tokens)}</td>
      <td class="intel-num">${fmt.speed(r.output_tokens_per_sec_milli)}</td>
      <td class="intel-num" title="${r.input_micro_usd == null ? '' : esc(t('输入 ', 'Input ') + fmt.usd(r.input_micro_usd) + ' · ' + t('输出 ', 'Output ') + fmt.usd(r.output_micro_usd))}">${fmt.usd(r.blended_micro_usd)}</td>
      <td class="intel-license">${license(r)}</td>
    </tr>`).join('') : `<tr><td colspan="11" class="intel-empty-row">${t('没有符合条件的模型', 'No models match these filters')}</td></tr>`;
    return `<div class="intel-table-wrap" tabindex="0" role="region" aria-label="${t('模型榜单', 'Model leaderboard')}"><table class="intel-table"><thead><tr>
      <th scope="col" class="intel-rank">#</th><th scope="col">${t('模型', 'Model')}</th><th scope="col">${t('厂商', 'Provider')}</th>
      ${head('score', t('综合分', 'Score'), 'intel-num')}${head('reasoning', t('推理', 'Reasoning'), 'intel-num')}${head('coding', t('编程', 'Coding'), 'intel-num')}${head('agent', 'Agent', 'intel-num')}
      ${head('context', t('上下文', 'Context'), 'intel-num')}${head('speed', t('速度', 'Speed'), 'intel-num')}${head('price', t('价格 $/1M', 'Price $/1M'), 'intel-num')}<th scope="col">${t('许可', 'License')}</th>
    </tr></thead><tbody>${body}</tbody></table></div>`;
  }

  function pagination(board, q) {
    const pages = Math.max(1, Math.ceil(board.total / board.page_size));
    if (pages <= 1) return '';
    const current = board.page;
    const go = (p, label, disabled) => disabled ? `<span class="btn small" aria-disabled="true">${label}</span>` : link(boardURL({...q, page: p > 1 ? String(p) : ''}), label, 'btn small');
    const first = (current - 1) * board.page_size + 1;
    return `<nav class="intel-pagination" aria-label="${t('分页', 'Pagination')}"><span class="muted">${first}–${Math.min(current * board.page_size, board.total)} / ${board.total}</span>${go(current - 1, t('上一页', 'Previous'), current <= 1)}<span>${current} / ${pages}</span>${go(current + 1, t('下一页', 'Next'), current >= pages)}</nav>`;
  }

  function subnav(active) {
    const item = (path, label) => link(path, label, 'intel-subnav-item' + (active === path ? ' active' : ''));
    return `<nav class="intel-subnav" aria-label="${t('榜单导航', 'Leaderboard navigation')}">${item('/models', t('模型评分榜', 'Model ratings'))}${item('/models?open=1', t('开放权重', 'Open weights'))}</nav>`;
  }

  const LEADERS = {
    reasoning: [['推理最强', 'Top reasoning'], l => fmt.bps(l.value), ['推理分', 'reasoning score']],
    coding: [['编程最强', 'Top coding'], l => fmt.bps(l.value), ['编程分', 'coding score']],
    value: [['前十中最便宜', 'Cheapest in the top 10'], l => fmt.usd(l.value), ['每百万 token 混合价', 'blended per 1M tokens']],
    speed: [['输出最快', 'Fastest output'], l => fmt.speed(l.value), ['中位输出速度', 'median output speed']],
    context: [['上下文最长', 'Longest context'], l => fmt.tokens(l.value), ['token 上下文', 'token context']],
    open: [['开放权重最强', 'Best open weights'], l => fmt.bps(l.value), ['综合分', 'score']],
  };

  function leaderCards(leaders) {
    if (!leaders.length) return '';
    return `<section class="intel-leaders" aria-label="${t('各维度领先', 'Category leaders')}">${leaders.filter(l => LEADERS[l.category]).map(l => {
      const [title, val, unit] = LEADERS[l.category];
      return `<a data-router class="card intel-leader" href="/model/${encodeURIComponent(l.row.model)}"><span class="intel-leader-title">${t(...title)}</span><strong class="intel-leader-model">${esc(l.row.name)}</strong><span class="intel-leader-provider muted">${esc(l.row.provider_name || l.row.provider)}</span><span class="intel-leader-value">${val(l)} <small class="muted">${t(...unit)}</small></span></a>`;
    }).join('')}</section>`;
  }

  // Answers are generated from the same snapshot as the table.
  function faq(home) {
    const find = c => home.leaders.find(l => l.category === c);
    const top = home.preview.rows.find(r => r.rank === 1);
    const items = [];
    // Every answer is stored as HTML: data-derived text is escaped here, and
    // FAQ answers are escaped before rendering.
    if (top) items.push([t('当前综合排名第一的模型是哪个？', 'Which model ranks first overall?'), esc(t(`${top.name}（${top.provider_name}），综合分 ${fmt.bps(top.composite_bps)}。`, `${top.name} by ${top.provider_name}, scoring ${fmt.bps(top.composite_bps)}.`))]);
    const pairs = [
      ['value', '前十名中最便宜的是哪个？', 'What is the cheapest model in the top 10?', l => `${l.row.name}：${fmt.usd(l.value)} / 1M tokens（3:1 输入输出混合价）。`, l => `${l.row.name} at ${fmt.usd(l.value)} per 1M tokens (3:1 input/output blend).`],
      ['context', '哪个模型上下文最长？', 'Which model has the longest context window?', l => `${l.row.name}，${fmt.tokens(l.value)} tokens。`, l => `${l.row.name}, with ${fmt.tokens(l.value)} tokens.`],
      ['speed', '哪个模型输出最快？', 'Which model has the fastest output?', l => `${l.row.name}，中位 ${fmt.speed(l.value)}。`, l => `${l.row.name}, at a median ${fmt.speed(l.value)}.`],
      ['open', '最强的开放权重模型是哪个？', 'What is the best open-weights model?', l => `${l.row.name}，综合分 ${fmt.bps(l.value)}。`, l => `${l.row.name}, scoring ${fmt.bps(l.value)}.`],
    ];
    for (const [c, qz, qe, az, ae] of pairs) { const l = find(c); if (l) items.push([t(qz, qe), esc(t(az(l), ae(l)))]); }
    items.push([t('综合分怎么计算？', 'How is the score calculated?'), esc(t('直接展示来源评分，不归一化、不加权、不推算。缺失分数显示「—」。', 'Display source scores directly, without normalisation, weighting or synthesis. Missing scores show “—”.'))]);
    return `<section class="card card-pad intel-faq"><h2>${t('常见问题', 'FAQ')}</h2>${items.map(([q, html]) => `<details><summary>${esc(q)}</summary><p class="muted">${html}</p></details>`).join('')}</section>`;
  }

  function stateCard(error) {
    if (error?.code === 'no_snapshot') return `<section class="card card-pad intel-state"><h2>${t('榜单数据准备中', 'Leaderboard data is being prepared')}</h2><p class="muted">${t('模型、价格与评测数据导入并完成首次计算后将在这里展示。', 'Models, prices and benchmarks will appear here after the first import and scoring run.')}</p></section>`;
    if (error?.code === 'not_found') return `<section class="card card-pad intel-state"><h2>${t('没有找到这个模型', 'Model not found')}</h2><p class="muted">${t('它可能尚未收录或已更名。', 'It may not be listed yet or may have been renamed.')}</p><div class="mt16">${link('/models', '← ' + t('返回榜单', 'Back to the leaderboard'), 'page-back')}</div></section>`;
    return `<section class="card card-pad intel-state" role="alert"><h2>${t('模型榜单暂时不可用', 'The model leaderboard is temporarily unavailable')}</h2><p class="muted">${t('社区其他功能不受影响，请稍后刷新。', 'The rest of the community is unaffected. Please refresh later.')}</p></section>`;
  }

  // Required credits for third-party data, keyed by import source slug.
  // LLM Stats data must be credited with a visible link wherever it shows.
  const SOURCE_CREDITS = {'llm-stats': ['Data by LLM Stats', 'https://llm-stats.com']};
  const attribution = sources => {
    const credits = (Array.isArray(sources) ? sources : []).filter(s => SOURCE_CREDITS[s]).map(s => `<a class="intel-credit" href="${SOURCE_CREDITS[s][1]}" target="_blank" rel="noopener">${esc(SOURCE_CREDITS[s][0])}</a>`);
    return credits.length ? `<span class="intel-attribution">${t('数据来源：', 'Source: ')}${credits.join(' · ')}</span>` : '';
  };

  class View {
    constructor(fetcher = defaultFetch) { this.fetch = fetcher; this.generation = 0; }
    dispose() { this.generation++; }

    async page(path, params) {
      this.dispose();
      try {
        if (['/models', '/models/methodology', '/models/leaderboard'].includes(path)) {
          const q = readQuery(params);
          return q.open ? await this.board(q) : await this.home(q);
        }
        if (path.startsWith('/model/')) {
          const slug = decodeURIComponent(path.slice('/model/'.length));
          if (!slugOK(slug)) throw new IntelError('not_found', 404);
          return await this.detail(slug);
        }
      } catch (error) {
        return `${subnav(path)}${stateCard(error)}`;
      }
      return `${subnav('')}${stateCard(new IntelError('not_found', 404))}`;
    }

    async home(q = {}) {
      const h = await this.fetch('/home');
      const board = await this.board(q, true);
      h.leaders = Array.isArray(h.leaders) ? h.leaders : [];

      return `${subnav('/models')}
        ${leaderCards(h.leaders)}
        <div class="intel-layout">
          <section class="intel-main"><div class="intel-section-head"><h2>${t('综合榜', 'Overall ranking')}</h2></div>${board}</section>
          <aside class="intel-side">
            <section class="card card-pad"><h2>${t('综合分说明', 'About the score')}</h2><p class="muted">${t('直接展示来源原始评分，价格与速度单独展示。', 'Original source scores. Price and speed are shown separately.')}</p></section>
            <section class="card card-pad"><h2>${t('一起讨论', 'Discuss')}</h2><p class="muted">${t('对排名或数据有疑问？在社区发帖讨论或指出错误。', 'Questions about a ranking or a number? Start a discussion or report an error.')}</p><div class="intel-side-actions mt16">${link('/publish', t('发起讨论', 'Start a discussion'), 'btn primary small')}${link('/latest', t('社区讨论', 'Community'), 'btn small')}</div></section>
          </aside>
        </div>
        ${faq(h)}`;
    }

    async board(q, embedded = false) {
      const params = encode(q);
      const b = await this.fetch('/leaderboard' + (params ? '?' + params : ''));
      const windowLink = (w, label) => link(boardURL({...q, window: w, page: ''}), label, 'intel-chip' + ((q.window || 'all') === w ? ' active' : ''));
      return `${embedded ? '' : subnav(q.open ? '/models?open=1' : '/models')}
        <div class="intel-filters" role="group" aria-label="${t('筛选', 'Filters')}">
          <span class="intel-filter-label muted">${t('发布时间', 'Released')}</span>${windowLink('all', t('全部', 'All'))}${windowLink('90d', t('近 90 天', '90 days'))}${windowLink('30d', t('近 30 天', '30 days'))}
          <span class="intel-filter-label muted">${t('许可', 'License')}</span>${link(boardURL({...q, open: '', page: ''}), t('全部', 'All'), 'intel-chip' + (!q.open ? ' active' : ''))}${link(boardURL({...q, open: '1', page: ''}), t('开放权重', 'Open weights'), 'intel-chip' + (q.open ? ' active' : ''))}
        </div>
        ${table(b.rows, q, true)}
        ${pagination(b, q)}
        <p class="intel-note muted">${t('排名为全榜综合排名，筛选不会改变名次。价格为模型厂商标准档（无则取最低报价）的 3:1 输入/输出混合价，单位美元/百万 token。', 'Ranks are overall and do not change with filters. Price is the 3:1 input/output blend of the maker\'s standard offering (or the cheapest listed), in USD per 1M tokens.')}</p>`;
    }

    async detail(slug) {
      const d = await this.fetch('/models/' + encodeURIComponent(slug));
      const b = d.board;
      const stat = (label, v) => `<div class="card intel-stat"><span class="muted">${label}</span><strong>${v}</strong></div>`;
      const offerings = d.offerings.length ? `<div class="intel-table-wrap"><table class="intel-table"><thead><tr><th scope="col">${t('服务商', 'Provider')}</th><th scope="col">${t('档位', 'Tier')}</th><th scope="col" class="intel-num">${t('输入', 'Input')}</th><th scope="col" class="intel-num">${t('输出', 'Output')}</th><th scope="col" class="intel-num">${t('缓存读', 'Cache read')}</th><th scope="col">${t('生效', 'Effective')}</th><th scope="col">${t('来源', 'Source')}</th></tr></thead><tbody>${d.offerings.map(o => `<tr><td>${esc(o.provider_name)}</td><td>${esc(o.tier)}</td><td class="intel-num">${fmt.usd(o.input_micro_usd)}</td><td class="intel-num">${fmt.usd(o.output_micro_usd)}</td><td class="intel-num">${fmt.usd(o.cache_read_micro_usd)}</td><td>${fmt.date(o.effective_at)}</td><td>${source(o.source_url, t('价格页', 'Pricing')) || DASH}</td></tr>`).join('')}</tbody></table></div><p class="intel-note muted">${t('单位：美元 / 百万 token', 'USD per 1M tokens')}</p>` : `<p class="muted">${t('暂无价格数据。', 'No pricing data yet.')}</p>`;
      const results = d.benchmark_results.length ? `<div class="intel-table-wrap"><table class="intel-table"><thead><tr><th scope="col">${t('评测', 'Benchmark')}</th><th scope="col">${t('维度', 'Dimension')}</th><th scope="col" class="intel-num">${t('结果', 'Result')}</th><th scope="col">${t('设置', 'Settings')}</th><th scope="col">${t('发布', 'Published')}</th><th scope="col">${t('来源', 'Source')}</th></tr></thead><tbody>${d.benchmark_results.map(r => `<tr><td>${esc(r.name)}${r.higher_is_better ? '' : ` <small class="muted">${t('越低越好', 'lower is better')}</small>`}</td><td>${dimLabel(r.dimension)}</td><td class="intel-num">${value(r.unit, r.value)}</td><td>${esc(r.settings) || DASH}</td><td>${fmt.date(r.published_at)}</td><td>${r.self_run ? `<span class="badge">${t('本站自测', 'Self-run')}</span>` : source(r.source_url, t('原始结果', 'Original'))}</td></tr>`).join('')}</tbody></table></div>` : `<p class="muted">${t('暂无评测结果。', 'No benchmark results yet.')}</p>`;
      return `${subnav('')}
        <header class="intel-hero intel-detail-head"><div><h1>${esc(d.name)}</h1><p class="muted">${esc(d.provider_name)}${d.family ? ' · ' + esc(d.family) : ''} · ${t('发布 ', 'Released ')}${fmt.date(d.release_date)}</p><div class="intel-badges">${badges({...d, release_date: d.release_date})}<span class="badge">${license(d)}</span></div></div><div class="intel-side-actions">${source(d.source_url, t('官方说明', 'Official page'))}${source(d.provider_website, t('厂商网站', 'Provider site'))}</div></header>
        <section class="intel-stats">
          ${stat(t('综合排名', 'Overall rank'), b?.rank != null ? '#' + b.rank : DASH)}
          ${stat(t('综合分', 'Score'), b?.composite_bps != null ? fmt.bps(b.composite_bps) : DASH)}
          ${stat(t('推理 / 编程 / Agent', 'Reasoning / Coding / Agent'), b ? `${fmt.bps(b.reasoning_bps)} / ${fmt.bps(b.coding_bps)} / ${fmt.bps(b.agent_bps)}` : DASH)}
          ${stat(t('上下文 / 最大输出', 'Context / max output'), `${fmt.tokens(d.context_tokens)} / ${fmt.tokens(d.max_output_tokens)}`)}
          ${stat(t('输出速度', 'Output speed'), fmt.speed(b?.output_tokens_per_sec_milli))}
          ${stat(t('混合价 $/1M', 'Blended $/1M'), fmt.usd(b?.blended_micro_usd))}
        </section>
        ${b && b.composite_bps == null ? `<p class="intel-note muted">${t('来源未提供综合评分，本站不自行合成。', 'The source has no overall score; we do not synthesize one.')}</p>` : ''}
        <section class="card card-pad mt16"><h2>${t('价格', 'Pricing')}</h2>${offerings}</section>
        <section class="card card-pad mt16"><h2>${t('评测结果', 'Benchmark results')}</h2>${results}</section>
        <section class="card card-pad mt16"><h2>${t('讨论这个模型', 'Discuss this model')}</h2><p class="muted">${t('分享使用体验，或指出数据错误。', 'Share your experience or report a data error.')}</p><div class="intel-side-actions mt16">${link('/publish', t('发起讨论', 'Start a discussion'), 'btn primary small')}${link('/search?q=' + encodeURIComponent(d.name), t('搜索相关讨论', 'Search discussions'), 'btn small')}</div></section>
        ${attribution(d.sources) ? `<p class="intel-note">${attribution(d.sources)}</p>` : ''}`;
    }

  }

  const api = {View, readQuery, encode, fmt, IntelError};
  if (typeof module === 'object' && module.exports) module.exports = api;
  else root.MetarModels = api;
})(typeof window === 'undefined' ? globalThis : window);
