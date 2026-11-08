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
  const boardURL = q => { const s = encode(q); return '/models/leaderboard' + (s ? '?' + s : ''); };

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
      const arrow = active ? (q.order === 'asc' ? ' ↑' : ' ↓') : '';
      return `<th class="${cls}" scope="col"${active ? ` aria-sort="${q.order === 'asc' ? 'ascending' : 'descending'}"` : ''}>${link(boardURL({...q, sort: key, order: nextOrder, page: ''}), label + arrow, 'intel-sort')}</th>`;
    };
    const body = rows.length ? rows.map(r => `<tr>
      <td class="intel-rank">${r.rank ?? DASH}</td>
      <th scope="row" class="intel-model-cell">${link('/model/' + encodeURIComponent(r.model), esc(r.name), 'intel-model-name')}<span class="intel-badges">${badges(r)}</span></th>
      <td class="intel-provider">${esc(r.provider_name || r.provider)}</td>
      <td class="intel-num intel-score">${r.composite_bps == null ? `<span class="muted" title="${t('评测覆盖不足，暂不计分', 'Not enough benchmark coverage to score')}">${DASH}</span>` : fmt.bps(r.composite_bps)}</td>
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
    return `<nav class="intel-subnav" aria-label="${t('榜单导航', 'Leaderboard navigation')}">${item('/models', t('榜单首页', 'Overview'))}${item('/models/leaderboard', t('完整榜单', 'Full leaderboard'))}${item('/models/leaderboard?open=1', t('开放权重', 'Open weights'))}${item('/models/methodology', t('计算方法', 'Methodology'))}</nav>`;
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
    // only the fixed methodology answer carries markup.
    if (top) items.push([t('当前综合排名第一的模型是哪个？', 'Which model ranks first overall?'), esc(t(`${top.name}（${top.provider_name}），综合分 ${fmt.bps(top.composite_bps)}。`, `${top.name} by ${top.provider_name}, scoring ${fmt.bps(top.composite_bps)}.`))]);
    const pairs = [
      ['value', '前十名中最便宜的是哪个？', 'What is the cheapest model in the top 10?', l => `${l.row.name}：${fmt.usd(l.value)} / 1M tokens（3:1 输入输出混合价）。`, l => `${l.row.name} at ${fmt.usd(l.value)} per 1M tokens (3:1 input/output blend).`],
      ['context', '哪个模型上下文最长？', 'Which model has the longest context window?', l => `${l.row.name}，${fmt.tokens(l.value)} tokens。`, l => `${l.row.name}, with ${fmt.tokens(l.value)} tokens.`],
      ['speed', '哪个模型输出最快？', 'Which model has the fastest output?', l => `${l.row.name}，中位 ${fmt.speed(l.value)}。`, l => `${l.row.name}, at a median ${fmt.speed(l.value)}.`],
      ['open', '最强的开放权重模型是哪个？', 'What is the best open-weights model?', l => `${l.row.name}，综合分 ${fmt.bps(l.value)}。`, l => `${l.row.name}, scoring ${fmt.bps(l.value)}.`],
    ];
    for (const [c, qz, qe, az, ae] of pairs) { const l = find(c); if (l) items.push([t(qz, qe), esc(t(az(l), ae(l)))]); }
    items.push([t('综合分怎么计算？', 'How is the score calculated?'), esc(t('对推理、编程、Agent 三个维度的公开评测做全榜归一化后加权平均；价格和速度单独展示，不计入综合分。评测覆盖不足的模型暂不计分。', 'Public benchmarks in reasoning, coding and agent work are normalised across the board and weighted. Price and speed are shown separately and do not affect the score. Models without enough coverage are not scored.')) + ' ' + link('/models/methodology', t('查看计算方法', 'Read the methodology'), 'textlink')]);
    return `<section class="card card-pad intel-faq"><h2>${t('常见问题', 'FAQ')}</h2>${items.map(([q, html]) => `<details><summary>${esc(q)}</summary><p class="muted">${html}</p></details>`).join('')}</section>`;
  }

  function stateCard(error) {
    if (error?.code === 'no_snapshot') return `<section class="card card-pad intel-state"><h2>${t('榜单数据准备中', 'Leaderboard data is being prepared')}</h2><p class="muted">${t('模型、价格与评测数据导入并完成首次计算后将在这里展示。', 'Models, prices and benchmarks will appear here after the first import and scoring run.')}</p></section>`;
    if (error?.code === 'not_found') return `<section class="card card-pad intel-state"><h2>${t('没有找到这个模型', 'Model not found')}</h2><p class="muted">${t('它可能尚未收录或已更名。', 'It may not be listed yet or may have been renamed.')}</p><div class="mt16">${link('/models/leaderboard', t('返回榜单', 'Back to the leaderboard'), 'btn')}</div></section>`;
    return `<section class="card card-pad intel-state" role="alert"><h2>${t('模型榜单暂时不可用', 'The model leaderboard is temporarily unavailable')}</h2><p class="muted">${t('社区其他功能不受影响，请稍后刷新。', 'The rest of the community is unaffected. Please refresh later.')}</p></section>`;
  }

  const updated = (snap) => `<span class="intel-updated muted">${t('数据更新于 ', 'Updated ')}${fmt.time(snap.computed_at)} · ${t('方法 ', 'Method ')}${esc(snap.method_version)}</span>`;

  class View {
    constructor(fetcher = defaultFetch) { this.fetch = fetcher; this.generation = 0; }
    dispose() { this.generation++; }

    async page(path, params) {
      this.dispose();
      try {
        if (path === '/models') return await this.home();
        if (path === '/models/leaderboard') return await this.board(readQuery(params));
        if (path === '/models/methodology') return await this.methodology();
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

    async home() {
      const h = await this.fetch('/home');
      const recent = h.recent.length ? `<ul class="intel-recent">${h.recent.map(r => `<li>${link('/model/' + encodeURIComponent(r.model), esc(r.name))}<span class="muted">${esc(r.provider_name || r.provider)} · ${fmt.date(r.release_date)}</span></li>`).join('')}</ul>` : `<p class="muted">${t('近 15 天没有新收录的模型。', 'No models released in the last 15 days.')}</p>`;
      return `${subnav('/models')}
        <header class="intel-hero"><h1>${t('AI 模型榜单', 'AI model leaderboard')}</h1><p class="muted">${t(`收录 ${h.model_count} 个语言模型，其中 ${h.ranked_count} 个有足够评测数据参与综合排名。每个数值都标注来源，缺失数据显示为「—」。`, `${h.model_count} language models listed, ${h.ranked_count} with enough benchmark coverage to be ranked. Every figure is sourced; missing data shows as "—".`)}</p>${updated(h)}</header>
        ${leaderCards(h.leaders)}
        <div class="intel-layout">
          <section class="intel-main"><div class="intel-section-head"><h2>${t('综合榜', 'Overall ranking')}</h2><div class="intel-window">${link('/models/leaderboard?window=90d', t('近 90 天', 'Last 90 days'), 'btn small')}${link('/models/leaderboard?window=30d', t('近 30 天', 'Last 30 days'), 'btn small')}${link('/models/leaderboard', t('完整榜单 →', 'Full leaderboard →'), 'btn small primary')}</div></div>${table(h.preview.rows, {}, false)}</section>
          <aside class="intel-side">
            <section class="card card-pad"><h2>${t('近期新模型', 'Recently released')}</h2>${recent}</section>
            <section class="card card-pad"><h2>${t('综合分说明', 'About the score')}</h2><p class="muted">${t('推理、编程、Agent 三维度公开评测的加权结果，价格与速度单独展示。', 'A weighted result of public reasoning, coding and agent benchmarks. Price and speed are shown separately.')}</p><div class="mt16">${link('/models/methodology', t('计算方法', 'Methodology'), 'textlink')}</div></section>
            <section class="card card-pad"><h2>${t('一起讨论', 'Discuss')}</h2><p class="muted">${t('对排名或数据有疑问？在社区发帖讨论或指出错误。', 'Questions about a ranking or a number? Start a discussion or report an error.')}</p><div class="intel-side-actions mt16">${link('/publish', t('发起讨论', 'Start a discussion'), 'btn primary small')}${link('/latest', t('社区讨论', 'Community'), 'btn small')}</div></section>
          </aside>
        </div>
        ${faq(h)}`;
    }

    async board(q) {
      const params = encode(q);
      const b = await this.fetch('/leaderboard' + (params ? '?' + params : ''));
      const windowLink = (w, label) => link(boardURL({...q, window: w, page: ''}), label, 'intel-chip' + ((q.window || 'all') === w ? ' active' : ''));
      return `${subnav(q.open ? '/models/leaderboard?open=1' : '/models/leaderboard')}
        <header class="intel-hero"><h1>${q.open ? t('开放权重模型榜', 'Open-weights leaderboard') : t('完整模型榜单', 'Full model leaderboard')}</h1>${updated(b)}</header>
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
        ${b && b.composite_bps == null ? `<p class="intel-note muted">${t(`评测覆盖 ${(b.coverage_bps / 100).toFixed(0)}%，未达到计分要求。`, `Benchmark coverage is ${(b.coverage_bps / 100).toFixed(0)}%, below the scoring threshold.`)}</p>` : ''}
        <section class="card card-pad mt16"><h2>${t('价格', 'Pricing')}</h2>${offerings}</section>
        <section class="card card-pad mt16"><h2>${t('评测结果', 'Benchmark results')}</h2>${results}</section>
        <section class="card card-pad mt16"><h2>${t('讨论这个模型', 'Discuss this model')}</h2><p class="muted">${t('分享使用体验，或指出数据错误。', 'Share your experience or report a data error.')}</p><div class="intel-side-actions mt16">${link('/publish', t('发起讨论', 'Start a discussion'), 'btn primary small')}${link('/search?q=' + encodeURIComponent(d.name), t('搜索相关讨论', 'Search discussions'), 'btn small')}</div></section>`;
    }

    async methodology() {
      const m = await this.fetch('/methodology');
      const total = m.weights.reduce((s, w) => s + w.weight, 0) || 1;
      const byDim = {};
      for (const b of m.benchmarks) (byDim[b.dimension] ||= []).push(b);
      const benchList = d => (byDim[d] || []).map(b => `<li>${esc(b.name)}${source(b.source_url, t('来源', 'Source'))}</li>`).join('') || `<li class="muted">${t('暂未配置评测', 'No benchmarks configured')}</li>`;
      return `${subnav('/models/methodology')}
        <header class="intel-hero"><h1>${t('综合分计算方法', 'Scoring methodology')}</h1><p class="muted">${t('当前版本 ', 'Current version ')}${esc(m.version)}${t('。方法调整只会发布新版本，不改写历史结果。', '. Changes are published as new versions; past results are never rewritten.')}</p></header>
        <section class="card card-pad"><h2>${t('维度与权重', 'Dimensions and weights')}</h2><div class="intel-weights">${m.weights.map(w => `<div class="intel-weight"><div class="intel-weight-head"><strong>${dimLabel(w.dimension)}</strong><span>${Math.round(w.weight * 100 / total)}%</span></div><div class="intel-weight-bar" aria-hidden="true"><span style="width:${Math.round(w.weight * 100 / total)}%"></span></div><ul class="intel-bench-list">${benchList(w.dimension)}</ul></div>`).join('')}</div></section>
        <section class="card card-pad mt16"><h2>${t('计算步骤', 'How it works')}</h2><ol class="intel-steps">
          <li>${t('每项评测取各模型最新的公开结果，在全榜内按最小值–最大值归一化到 0–100；越低越好的评测会反转。只有一个模型报告、或所有模型结果相同的评测不提供排名信息，跳过。', 'For each benchmark, take each model\'s latest public result and min-max normalise across the board to 0–100, inverting lower-is-better benchmarks. Benchmarks reported by a single model, or where all models tie, carry no ranking signal and are skipped.')}</li>
          <li>${t('同一维度内取已有评测的平均值，作为维度分。', 'Average the available benchmarks within each dimension.')}</li>
          <li>${t(`按权重对有数据的维度加权平均。覆盖权重不足 ${(m.min_coverage_bps / 100).toFixed(0)}% 的模型不计综合分，显示「—」并排在已计分模型之后，缺失维度不按 0 处理。`, `Weight the dimensions that have data. Models covering less than ${(m.min_coverage_bps / 100).toFixed(0)}% of the weight get no score, show "—" and sort after scored models; missing dimensions are never counted as zero.`)}</li>
          <li>${t('相同分数并列同名次。', 'Equal scores share a rank.')}</li>
        </ol></section>
        <section class="card card-pad mt16"><h2>${t('价格与速度', 'Price and speed')}</h2><p class="muted">${t(`价格与速度不计入综合分。混合价按输入:输出 = ${m.price_blend.input}:${m.price_blend.output} 计算，取模型厂商标准档，没有时取最低报价。速度为最近 ${m.speed_window_hours} 小时测量样本的中位数。`, `Price and speed do not affect the score. Blended price weights input:output ${m.price_blend.input}:${m.price_blend.output}, using the maker's standard offering or else the cheapest listed. Speed is the median of samples from the last ${m.speed_window_hours} hours.`)}</p></section>
        <section class="card card-pad mt16"><h2>${t('数据原则', 'Data principles')}</h2><ul class="intel-steps"><li>${t('数据来自厂商官方信息、评测原始发布方和本站自测，每个数值标注来源。', 'Data comes from official provider pages, original benchmark publishers and our own measurements. Every figure is sourced.')}</li><li>${t('历史数据只追加不覆盖；纠错以新记录发布。', 'History is append-only; corrections are published as new records.')}</li><li>${t('发现错误？欢迎在社区发帖指出。', 'Spotted an error? Tell us in the community.')} ${link('/publish', t('发起讨论', 'Start a discussion'), 'textlink')}</li></ul></section>`;
    }
  }

  const api = {View, readQuery, encode, fmt, IntelError};
  if (typeof module === 'object' && module.exports) module.exports = api;
  else root.MetarModels = api;
})(typeof window === 'undefined' ? globalThis : window);
