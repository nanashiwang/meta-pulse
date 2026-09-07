/* METAR production community shell. All community data comes from Apache Answer. */
'use strict';
(() => {
  const { AdapterError, AnswerAdapter, KnowledgeAdapter, loadIdentitySnapshot, relativePath, routeMatchesNavigation } = window.MetarAdapters;
  const rawConfig = window.__METAR_RUNTIME_CONFIG__ || {};
  const config = Object.freeze({
    siteName: typeof rawConfig.siteName === 'string' ? rawConfig.siteName : 'METAR',
    siteTagline: typeof rawConfig.siteTagline === 'string' ? rawConfig.siteTagline : '与创造者一起，把 AI 用出价值',
    answerApiBase: relativePath(rawConfig.answerApiBase, '/answer/api/v1'),
    answerLoginPath: relativePath(rawConfig.answerLoginPath, '/users/login'),
    answerRegisterPath: relativePath(rawConfig.answerRegisterPath, '/users/register'),
    answerPasswordResetPath: relativePath(rawConfig.answerPasswordResetPath, '/users/account-recovery'),
    answerAskPath: relativePath(rawConfig.answerAskPath, '/questions/ask'),
    answerSettingsPath: relativePath(rawConfig.answerSettingsPath, '/users/settings/profile'),
    answerBindingPath: relativePath(rawConfig.answerBindingPath, '/answer/api/v1/connector/login/pulse_user_center'),
    blogBasePath: relativePath(rawConfig.blogBasePath, '/blog/'),
    consoleUrl: safeExternalUrl(rawConfig.consoleUrl),
    docsUrl: safeExternalUrl(rawConfig.docsUrl),
    statusUrl: safeExternalUrl(rawConfig.statusUrl),
  });

  const answer = new AnswerAdapter(config);
  const knowledge = new KnowledgeAdapter(config);
  const app = document.getElementById('app');
  let navigationSequence = 0;
  let currentUser = null;
  let currentUserState = 'loading';
  let identityError = null;

  const ICONS = {
    search: '<circle cx="10.8" cy="10.8" r="6.6"/><path d="m16 16 4.2 4.2"/>',
    compass: '<circle cx="12" cy="12" r="9"/><path d="m15.7 8.3-2.2 5.2-5.2 2.2 2.2-5.2z"/>',
    chat: '<path d="M20 11.5a8 8 0 0 1-8 8H4l1.6-4A8 8 0 1 1 20 11.5Z"/><path d="M8 10h8m-8 4h5"/>',
    book: '<path d="M12 5C8 2 5 3 3 4v15c3-1 6-1 9 1 3-2 6-2 9-1V4c-2-1-5-2-9 1Zm0 0v15"/>',
    pulse: '<path d="M2 12h5l3-8 4 16 3-8h5"/>',
    code: '<path d="m7 6-6 6 6 6m10-12 6 6-6 6m-3-15-4 18"/>',
    bell: '<path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4"/>',
    plus: '<path d="M12 4v16M4 12h16"/>',
    arrow: '<path d="M4 12h16m-5-5 5 5-5 5"/>',
    external: '<path d="M14 3h7v7m0-7L10 14M10 3H4v17h17v-6"/>',
    bookmark: '<path d="M6 3h12v19l-6-4-6 4Z"/>',
    user: '<circle cx="12" cy="8" r="4"/><path d="M4 21v-2a8 8 0 0 1 16 0v2"/>',
    check: '<path d="m5 12 4 4L20 5"/>',
    shield: '<path d="m12 2 9 4v6c0 5-9 10-9 10S3 17 3 12V6z"/><path d="m8 11 3 3 5-5"/>',
    settings: '<path d="M9 3h6l1 3 3 1 2 5-2 5-3 1-1 3H9l-1-3-3-1-2-5 2-5 3-1z"/><circle cx="12" cy="12" r="3"/>',
    help: '<circle cx="12" cy="12" r="9"/><path d="M9 8a3 3 0 0 1 6 0c0 2-3 2-3 4m0 4v.5"/>',
    link: '<path d="m10 14 4-4m-7 2-3 3a4 4 0 0 0 6 6l4-4m-4-10 4-4a4 4 0 0 1 6 6l-3 3"/>',
    clock: '<circle cx="12" cy="12" r="9"/><path d="M12 6v6l4 2"/>',
    sun: '<circle cx="12" cy="12" r="4"/><path d="M12 1v2m0 18v2M1 12h2m18 0h2M4 4l2 2m12 12 2 2M4 20l2-2M18 6l2-2"/>',
    moon: '<path d="M21 13A9 9 0 0 1 11 3a9 9 0 1 0 10 10Z"/>',
    menu: '<path d="M4 6h16M4 12h16M4 18h16"/>',
    close: '<path d="m6 6 12 12M6 18 18 6"/>',
    target: '<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="5"/><circle cx="12" cy="12" r="1"/>',
    inbox: '<path d="m6 3-4 11v7h20v-7L18 3ZM2 14h6l2 3h4l2-3h6"/>',
    refresh: '<path d="M20 7v5h-5M4 17v-5h5M6 5a9 9 0 0 1 14 7M4 12a9 9 0 0 0 14 7"/>',
    flag: '<path d="M5 22V3c5-3 9 3 14 0v11c-5 3-9-3-14 0"/>',
    server: '<rect x="3" y="3" width="18" height="7" rx="2"/><rect x="3" y="14" width="18" height="7" rx="2"/><path d="M7 6.5h.1M7 17.5h.1M13 6.5h4M13 17.5h4"/>',
  };

  const LOGO = '<svg viewBox="0 0 32 32" fill="none" aria-hidden="true"><path d="M3 25V7l8 9 5-10 5 10 8-9v18" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"/><path d="m8 25 8-12 8 12" stroke="currentColor" stroke-width="2.6" stroke-linecap="round"/></svg>';
  const I = (name, className = '') => `<svg class="ico ${className}" viewBox="0 0 24 24" aria-hidden="true">${ICONS[name] || ICONS.compass}</svg>`;
  const esc = (value) => String(value ?? '').replace(/[&<>"']/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[char]));
  const text = (value) => esc(String(value || '').replace(/\r\n/g, '\n'));

  function safeExternalUrl(value) {
    if (typeof value !== 'string' || !value) return '';
    try {
      const url = new URL(value);
      return url.protocol === 'https:' && !url.username && !url.password && !url.search && !url.hash ? url.href.replace(/\/$/, '') : '';
    } catch (_) { return ''; }
  }

  function safeSameOriginPath(value, fallback) {
    if (!value) return fallback;
    try {
      const url = new URL(value, location.origin);
      return url.origin === location.origin && url.pathname.startsWith('/') ? `${url.pathname}${url.search}` : fallback;
    } catch (_) { return fallback; }
  }

  function route() {
    const raw = location.hash.replace(/^#/, '') || '/discover';
    const index = raw.indexOf('?');
    const path = index < 0 ? raw : raw.slice(0, index);
    return { path: path.startsWith('/') ? path : `/${path}`, query: new URLSearchParams(index < 0 ? '' : raw.slice(index + 1)) };
  }

  const link = (path, label, className = '') => `<a href="#${esc(path)}" class="${esc(className)}">${label}</a>`;
  const external = (path, label, className = '') => `<a href="${esc(path)}" class="${esc(className)}">${label}</a>`;
  const outbound = (url, label, className = '') => url ? `<a href="${esc(url)}" class="${esc(className)}" target="_blank" rel="noopener noreferrer">${label}</a>` : '';
  const badge = (label, className = '') => `<span class="badge ${className}">${label}</span>`;
  const crumb = (parts = []) => `<nav class="breadcrumb" aria-label="面包屑">${link('/discover', '社区')}${parts.map(([label, path]) => `<span>/</span>${path ? link(path, esc(label)) : `<span aria-current="page">${esc(label)}</span>`}`).join('')}</nav>`;
  const heading = (title, description = '', action = '') => `<div class="page-heading"><div><h1>${esc(title)}</h1>${description ? `<p>${esc(description)}</p>` : ''}</div>${action}</div>`;
  const empty = (title, description, action = '', icon = 'inbox') => `<div class="prod-empty">${I(icon)}<h2>${esc(title)}</h2><p>${esc(description)}</p>${action}</div>`;
  const loading = () => '<div class="prod-loading"><div><div class="loader"></div><p>正在读取社区实时数据…</p></div></div>';
  const displayName = (user) => user?.display_name || user?.username || '社区成员';
  const initial = (user) => displayName(user).trim().slice(0, 1).toUpperCase() || 'M';
  const isActiveUser = (user) => Boolean(user) && Number(user.mail_status) === 1 && !['inactive', 'suspended', 'deleted'].includes(String(user.status || 'normal'));
  const number = (value) => new Intl.NumberFormat('zh-CN').format(Number(value) || 0);
  const time = (value) => {
    const timestamp = Number(value) || 0;
    if (!timestamp) return '时间未知';
    const date = new Date(timestamp > 1e12 ? timestamp : timestamp * 1000);
    if (Number.isNaN(date.getTime())) return '时间未知';
    const seconds = Math.round((date.getTime() - Date.now()) / 1000);
    const abs = Math.abs(seconds);
    const relative = new Intl.RelativeTimeFormat('zh-CN', { numeric: 'auto' });
    if (abs < 60) return relative.format(seconds, 'second');
    if (abs < 3600) return relative.format(Math.round(seconds / 60), 'minute');
    if (abs < 86400) return relative.format(Math.round(seconds / 3600), 'hour');
    if (abs < 2592000) return relative.format(Math.round(seconds / 86400), 'day');
    return date.toLocaleDateString('zh-CN');
  };

  function questionPath(question) {
    return `/question/${encodeURIComponent(question.id)}`;
  }

  function answerQuestionPath(question) {
    return `/questions/${encodeURIComponent(question.id)}`;
  }

  function notificationPath(item) {
    const object = item?.object_info || {};
    const mapping = object.object_map || {};
    const questionID = mapping.question || (object.object_type === 'question' ? object.object_id : '');
    if (!questionID) return '/notifications';
    if (object.object_type === 'answer' && object.object_id) return `/questions/${encodeURIComponent(questionID)}/${encodeURIComponent(object.object_id)}`;
    if (object.object_type === 'comment' && mapping.answer && mapping.comment) return `/questions/${encodeURIComponent(questionID)}/${encodeURIComponent(mapping.answer)}?commentId=${encodeURIComponent(mapping.comment)}`;
    return `/questions/${encodeURIComponent(questionID)}`;
  }

  function tagName(tag) { return tag?.display_name || tag?.slug_name || '话题'; }

  function questionRow(question) {
    const operator = question.operator || question.user_info || {};
    const tags = Array.isArray(question.tags) ? question.tags : [];
    return `<article class="post-row">
      <div class="vote-box"><strong>${number(question.vote_count)}</strong><span>赞同</span></div>
      <div class="post-main">
        <div class="flex wrap gap6">${question.pin === 2 ? badge('置顶', 'green') : ''}${question.accepted_answer_id ? badge('已解决', 'green') : ''}${tags.slice(0, 3).map((tag) => link(`/topic/${encodeURIComponent(tag.slug_name || tagName(tag))}`, esc(tagName(tag)), 'badge')).join('')}</div>
        ${link(questionPath(question), `<h3>${esc(question.title)}</h3>`, 'prod-question-link')}
        <p>${esc(question.description || '该问题暂未提供摘要。')}</p>
        <div class="post-meta"><span class="avatar a4">${esc(initial(operator))}</span><span>${esc(displayName(operator))}</span><span>·</span><span>${time(question.operated_at || question.created_at || question.create_time || question.update_time)}</span><span>·</span><span>${number(question.answer_count)} 回答</span><span>·</span><span>${number(question.view_count)} 浏览</span></div>
      </div>
    </article>`;
  }

  function footer() {
    return `<footer class="prod-footer"><span>© 2026 ${esc(config.siteName)} · 社区内容与身份由 Apache Answer 管理</span><nav>${external('/questions', 'Answer 原始列表')}${external('/users/register', '加入社区')}${link('/guidelines', '社区规范')}${link('/status', '服务状态')}</nav></footer>`;
  }

  function currentTitle() {
    const path = route().path;
    if (path.startsWith('/question/')) return '问题详情';
    if (path.startsWith('/topic/')) return '话题';
    return ({ '/discover': '发现', '/questions': '问答广场', '/topics': '全部话题', '/knowledge': '知识库', '/search': '搜索', '/me': '个人空间', '/bookmarks': '我的收藏', '/notifications': '通知中心', '/settings/binding': '账号绑定', '/pulse': 'Pulse 权益', '/publish': '发布内容', '/login': '登录', '/register': '注册', '/forgot': '找回密码', '/status': '服务状态', '/support': '帮助中心', '/guidelines': '社区规范' })[path] || 'METAR 社区';
  }

  function topbar() {
    const path = route().path;
    const logged = currentUserState === 'ready' && currentUser;
    const identityUnavailableState = currentUserState === 'unavailable';
    const activeUser = logged && isActiveUser(currentUser);
    const active = (prefix) => path === prefix || path.startsWith(`${prefix}/`);
    return `<header class="topbar">
      <button class="icon-btn mobile-menu" type="button" data-action="menu" aria-label="打开导航" aria-controls="community-sidebar" aria-expanded="false">${I('menu')}</button>
      ${link('/discover', LOGO + `<span class="brand-word">${esc(config.siteName.toLowerCase())}</span>`, 'brand')}
      <nav class="topnav" aria-label="主导航">${link('/discover', '社区', active('/discover') || active('/questions') || active('/topic') || active('/topics') ? 'active' : '')}${link('/knowledge', '知识库', active('/knowledge') ? 'active' : '')}${link('/pulse', 'Pulse', active('/pulse') ? 'active' : '')}${outbound(config.consoleUrl, '开发者')}</nav>
      <form class="searchbox" data-form="search" role="search">${I('search')}<input type="search" name="q" aria-label="搜索社区" placeholder="搜索真实问题与回答…" value="${path === '/search' ? esc(route().query.get('q') || '') : ''}" autocomplete="off"><kbd>⌘ K</kbd></form>
      <div class="header-actions"><button type="button" class="icon-btn theme-btn" data-action="theme" aria-label="${document.documentElement.dataset.theme === 'dark' ? '切换到浅色主题' : '切换到深色主题'}">${I(document.documentElement.dataset.theme === 'dark' ? 'sun' : 'moon')}</button>${identityUnavailableState ? link('/me', I('server', 'sm') + '身份服务暂不可用', 'btn ghost') : logged ? (activeUser ? link('/notifications', I('bell') + '<span class="visually-hidden">通知中心</span>', 'icon-btn') + external(config.answerAskPath, I('plus') + '<span class="publish-label">发布</span>', 'btn primary') : external('/users/login?status=inactive', '激活账号', 'btn primary')) + link('/me', `<span class="avatar a4">${esc(initial(currentUser))}</span><span class="visually-hidden">个人空间</span>`, 'icon-btn') : external(config.answerLoginPath, '登录', 'btn ghost') + external(config.answerRegisterPath, '加入社区', 'btn primary guest-register')}</div>
    </header>`;
  }

  function sidebar() {
    const { path, query } = route();
    const item = (url, label, icon) => link(url, I(icon) + `<span>${esc(label)}</span>`, `nav-item ${routeMatchesNavigation(path, query.toString(), url) ? 'active' : ''}`);
    return `<aside class="sidebar" id="community-sidebar" aria-label="社区导航">
      <div class="nav-group"><div class="nav-label">社区</div>${item('/discover', '发现', 'compass')}${item('/questions', '问答广场', 'chat')}${item('/questions?order=unanswered', '待回答', 'target')}${item('/topics', '全部话题', 'flag')}${item('/knowledge', '知识库', 'book')}</div>
      <div class="nav-group"><div class="nav-label">我的空间</div>${item('/me', '个人主页', 'user')}${item('/bookmarks', '我的收藏', 'bookmark')}${item('/notifications', '通知中心', 'bell')}${item('/settings/binding', '账号绑定', 'link')}</div>
      <div class="side-bottom">${item('/pulse', 'Pulse 权益', 'pulse')}${item('/support', '帮助中心', 'help')}${item('/status', '服务状态', 'server')}<div class="side-footer">${link('/guidelines', '社区规范')}${external('/sitemap.xml', '站点地图')}</div></div>
    </aside>`;
  }

  function mobileBottom() {
    const { path, query } = route();
    return `<nav class="mobile-bottom" aria-label="移动端主导航">${[['/discover', '发现', 'compass'], ['/questions', '问答', 'chat'], ['/knowledge', '知识库', 'book'], ['/pulse', 'Pulse', 'pulse'], ['/me', '我的', 'user']].map(([url, label, icon]) => link(url, I(icon) + label, routeMatchesNavigation(path, query.toString(), url) ? 'active' : '')).join('')}</nav>`;
  }

  function renderShell() {
    app.innerHTML = `${topbar()}${sidebar()}<main class="page" id="main" tabindex="-1"><div class="page-inner" id="view">${loading()}</div></main>${mobileBottom()}`;
    app.setAttribute('aria-busy', 'true');
    document.title = `${currentTitle()} · ${config.siteName}`;
  }

  function renderView(html) {
    const view = document.getElementById('view');
    if (view) view.innerHTML = html;
    app.setAttribute('aria-busy', 'false');
    document.getElementById('main')?.focus({ preventScroll: true });
  }

  function renderError(error) {
    const inactive = error instanceof AdapterError && error.code === 'inactive';
    const message = inactive ? '当前社区账号尚未激活，请先完成 Answer 邮箱验证。' : error?.message || '页面暂时无法加载。';
    renderView(`${crumb([['加载失败']])}<section class="card prod-error">${I(inactive ? 'shield' : 'server')}<h2>${inactive ? '账号尚未激活' : '暂时没有读到社区数据'}</h2><p>${esc(message)} 社区数据不会由前端猜测或使用缓存数字替代。</p><div class="flex wrap prod-center-actions"><button type="button" class="btn primary" data-action="retry">${I('refresh')}重新加载</button>${inactive ? external('/users/login?status=inactive', '重新发送激活邮件', 'btn') : external('/questions', '打开 Answer 原始页面', 'btn')}</div></section>${footer()}`);
  }

  async function discoverPage() {
    const [latest, hot, tags] = await Promise.all([
      answer.listQuestions({ pageSize: 10, order: 'active' }),
      answer.listQuestions({ pageSize: 5, order: 'hot' }),
      answer.listTags({ pageSize: 8, order: 'popular' }),
    ]);
    const latestList = Array.isArray(latest?.list) ? latest.list : [];
    const hotList = Array.isArray(hot?.list) ? hot.list : [];
    const tagList = Array.isArray(tags?.list) ? tags.list : [];
    return `${crumb([['发现']])}<section class="prod-hero"><div class="eyebrow">METAR COMMUNITY</div><h1>${esc(config.siteTagline)}</h1><p>浏览真实问答、沉淀接入经验；社区账号可独立注册，元衡 API 账号仅在你主动选择时进行一对一绑定。</p><div class="prod-stat-grid"><div class="prod-stat"><strong>${number(latest?.count)}</strong><span>公开问题</span></div><div class="prod-stat"><strong>${number(tags?.count)}</strong><span>社区话题</span></div><div class="prod-stat"><strong>${currentUser ? (isActiveUser(currentUser) ? '已登录' : '待激活') : '可独立加入'}</strong><span>社区身份</span></div></div></section>
      <div class="content-grid mt24"><section class="card prod-feed"><div class="section-heading"><h2>社区最新动态</h2>${link('/questions', '查看全部 ' + I('arrow', 'sm'), 'textlink')}</div>${latestList.length ? latestList.map(questionRow).join('') : empty('社区还没有公开问题', '成为第一个发起讨论的人。', external(config.answerAskPath, '发起问题', 'btn primary'), 'chat')}</section><aside class="stack"><section class="card card-pad"><div class="card-title"><h3>热门话题</h3>${I('flag')}</div><div class="flex wrap mt16">${tagList.length ? tagList.map((tag) => link(`/topic/${encodeURIComponent(tag.slug_name)}`, esc(tagName(tag)), 'badge')).join('') : '<span class="muted">暂无话题</span>'}</div>${link('/topics', '浏览全部话题 ' + I('arrow', 'sm'), 'textlink mt16')}</section><section class="card card-pad"><div class="card-title"><h3>近期热门</h3>${I('target')}</div><ul class="mini-list">${hotList.slice(0, 5).map((question) => `<li>${link(questionPath(question), `<span>${esc(question.title)}</span><small>${number(question.answer_count)} 回答</small>`)}</li>`).join('') || '<li><span class="muted">暂无热门问题</span></li>'}</ul></section></aside></div>${footer()}`;
  }

  const QUESTION_ORDERS = { active: '最近活跃', newest: '最新发布', hot: '热门', score: '高赞', unanswered: '待回答' };

  async function questionsPage(tag = '') {
    const { query } = route();
    const requested = query.get('order') || 'active';
    const order = Object.prototype.hasOwnProperty.call(QUESTION_ORDERS, requested) ? requested : 'active';
    const page = Math.max(1, Number.parseInt(query.get('page') || '1', 10) || 1);
    const result = await answer.listQuestions({ page, pageSize: 20, order, tag });
    const list = Array.isArray(result?.list) ? result.list : [];
    const total = Number(result?.count) || 0;
    const base = tag ? `/topic/${encodeURIComponent(tag)}` : '/questions';
    const title = tag ? `话题：${tag}` : '问答广场';
    const tabs = Object.entries(QUESTION_ORDERS).map(([value, label]) => link(`${base}?order=${value}`, esc(label), `tab ${order === value ? 'active' : ''}`)).join('');
    const totalPages = Math.max(1, Math.ceil(total / 20));
    const pager = totalPages > 1 ? `<div class="prod-pager">${page > 1 ? link(`${base}?order=${order}&page=${page - 1}`, '上一页', 'btn small') : ''}<span>第 ${page} / ${totalPages} 页</span>${page < totalPages ? link(`${base}?order=${order}&page=${page + 1}`, '下一页', 'btn small') : ''}</div>` : '';
    return `${crumb(tag ? [['全部话题', '/topics'], [tag]] : [['问答广场']])}${heading(title, tag ? '来自 Apache Answer 的实时话题内容。' : '在真实问题里交换经验，在有依据的回答里共同成长。', external(config.answerAskPath, I('plus') + '发起提问', 'btn primary'))}<section class="card prod-feed"><div class="tabs">${tabs}</div>${list.length ? list.map(questionRow).join('') : empty(order === 'unanswered' ? '暂时没有待回答问题' : '当前筛选没有内容', '可以调整筛选，或发起一个新问题。', external(config.answerAskPath, '发起提问', 'btn primary'), 'chat')}${pager}</section>${footer()}`;
  }

  async function questionPage(id) {
    const [question, answers] = await Promise.all([answer.getQuestion(id), answer.listAnswers(id)]);
    const answerList = Array.isArray(answers?.list) ? answers.list : [];
    const tags = Array.isArray(question?.tags) ? question.tags : [];
    const owner = question?.user_info || {};
    return `${crumb([['问答广场', '/questions'], [question.title || '问题详情']])}<div class="content-grid"><div class="stack"><article class="card card-pad"><div class="flex wrap">${question.accepted_answer_id ? badge('已解决', 'green') : ''}${tags.map((tag) => link(`/topic/${encodeURIComponent(tag.slug_name || tagName(tag))}`, esc(tagName(tag)), 'badge')).join('')}</div><h1 class="mt16">${esc(question.title)}</h1><div class="post-meta mt16"><span class="avatar a4">${esc(initial(owner))}</span><span>${esc(displayName(owner))}</span><span>·</span><span>${time(question.create_time)}</span><span>·</span><span>${number(question.view_count)} 浏览</span></div><div class="divider"></div><div class="prod-question-body">${text(question.content || question.description || '该问题没有可展示的正文。')}</div><div class="divider"></div><div class="flex wrap"><span class="badge">${number(question.vote_count)} 赞同</span><span class="badge">${number(question.collection_count)} 收藏</span><span class="badge">${number(question.follow_count)} 关注</span>${external(answerQuestionPath(question), '在 Answer 完整页面参与 ' + I('external', 'sm'), 'btn primary')}</div></article><section class="card"><div class="section-heading"><h2>${number(answers?.count)} 个回答</h2><span class="muted">实时读取</span></div>${answerList.length ? answerList.map((item) => `<article class="prod-answer"><div class="prod-answer-head"><span class="flex"><span class="avatar a4">${esc(initial(item.user_info))}</span><strong>${esc(displayName(item.user_info))}</strong>${item.accepted === 1 ? badge('已采纳', 'green') : ''}</span><span>${time(item.create_time)} · ${number(item.vote_count)} 赞同</span></div><div class="prod-answer-content">${text(item.content || '该回答没有可展示的正文。')}</div></article>`).join('') : empty('还没有回答', '如果你有相关经验，欢迎在完整问题页贡献一个具体答案。', external(answerQuestionPath(question), '写回答', 'btn primary'), 'chat')}</section></div><aside class="stack"><section class="card card-pad"><h3>参与讨论</h3><p class="muted mt8">编辑、投票、回答、收藏等写操作继续由 Answer 原生页面负责，避免在未完成权限适配前复制业务逻辑。</p>${external(answerQuestionPath(question), '打开完整问题页 ' + I('external', 'sm'), 'btn primary wfull mt16')}</section><section class="card card-pad"><h3>内容边界</h3><p class="muted mt8">论坛内容本身不产生贡献值或脉冲券；内容奖励需走独立资格与审核流程。</p></section></aside></div>${footer()}`;
  }

  async function topicsPage() {
    const result = await answer.listTags({ pageSize: 48, order: 'popular' });
    const tags = Array.isArray(result?.list) ? result.list : [];
    return `${crumb([['全部话题']])}${heading('全部话题', '从真实社区标签中找到你正在探索的方向。')}<div class="prod-topic-grid">${tags.length ? tags.map((tag) => `<article class="card prod-topic-card"><div><div class="topic-icon">${I('flag')}</div><h3>${esc(tagName(tag))}</h3><p>${esc(tag.excerpt || tag.description || '该话题暂未添加介绍。')}</p></div><div class="between"><span class="muted">${number(tag.question_count)} 个问题</span>${link(`/topic/${encodeURIComponent(tag.slug_name)}`, '进入话题 ' + I('arrow', 'sm'), 'textlink')}</div></article>`).join('') : `<section class="card">${empty('还没有公开话题', '话题将随社区内容逐步建立。', external(config.answerAskPath, '发起问题', 'btn primary'), 'flag')}</section>`}</div>${footer()}`;
  }

  async function searchPage() {
    const query = (route().query.get('q') || '').trim();
    const form = `<form class="prod-search-form" data-form="search"><input type="search" name="q" value="${esc(query)}" maxlength="60" required placeholder="输入问题、模型或接入关键词"><button type="submit" class="btn primary">${I('search')}搜索</button></form>`;
    if (!query) return `${crumb([['搜索']])}${heading('搜索社区', '结果来自 Apache Answer 的真实搜索索引。')}${form}<section class="card">${empty('输入一个关键词开始搜索', '例如：API、Agent、模型评测、错误处理。', '', 'search')}</section>${footer()}`;
    const result = await answer.search(query);
    const list = Array.isArray(result?.list) ? result.list : [];
    return `${crumb([['搜索']])}${heading(`“${query}” 的结果`, `找到 ${number(result?.count)} 条社区内容。`)}${form}<section class="card">${list.length ? list.map((item) => { const object = item.object || {}; const questionId = object.question_id || object.id; return `<article class="result-row">${badge(item.object_type === 'answer' ? '回答' : '问题', item.object_type === 'answer' ? '' : 'green')}${link(`/question/${encodeURIComponent(questionId)}`, `<h3>${esc(object.title || '社区内容')}</h3>`)}<p>${esc(object.excerpt || '该结果暂未提供摘要。')}</p><small class="muted">${esc(displayName(object.user_info))} · ${time(object.created_at)}</small></article>`; }).join('') : empty('没有找到相关内容', '换一个更具体的关键词，或向社区发起新问题。', external(config.answerAskPath, '发起提问', 'btn primary'), 'search')}</section>${footer()}`;
  }

  function knowledgePage() {
    const articles = knowledge.listArticles();
    return `${crumb([['知识库']])}${heading('知识库', '长文由 VitePress 管理，与社区问答保持清晰事实源。', external(config.blogBasePath, '打开完整知识库 ' + I('external', 'sm'), 'btn primary'))}<div class="prod-knowledge-grid">${articles.map((article) => `<a class="card prod-knowledge-card" href="${esc(article.href)}"><div>${badge(esc(article.category), 'green')}<h2 class="mt16">${esc(article.title)}</h2><p>${esc(article.description)}</p></div><span class="textlink">阅读文章 ${I('arrow', 'sm')}</span></a>`).join('')}</div><section class="card card-pad mt24"><div class="between wrap"><div><h3>找不到需要的接入说明？</h3><p class="muted mt8">可以在社区提问，问题和回答会继续保存在 Answer。</p></div>${external(config.answerAskPath, '向社区提问', 'btn')}</div></section>${footer()}`;
  }

  function loginRequired(title, description) {
    return `${crumb([[title]])}<section class="card prod-login-card">${I('shield', 'lg')}<h1 class="mt16">${esc(title)}</h1><p>${esc(description)} 社区账号可独立注册，不要求先绑定元衡 API 账号。</p><div class="flex wrap">${external(config.answerLoginPath, '登录社区账号', 'btn primary')}${external(config.answerRegisterPath, '独立注册', 'btn')}${link('/discover', '先浏览社区', 'btn ghost')}</div></section>${footer()}`;
  }

  function identityUnavailable(title) {
    const reason = identityError?.message || '暂时无法确认当前社区登录状态。';
    return `${crumb([[title]])}<section class="card prod-login-card">${I('server', 'lg')}<h1 class="mt16">身份服务暂不可用</h1><p>${esc(reason)} 这不代表账号已退出，请勿反复登录；仍可继续尝试浏览社区公开内容。</p><div class="flex wrap"><button type="button" class="btn primary" data-action="retry-identity">${I('refresh')}重新确认身份</button>${link('/discover', '继续浏览社区', 'btn ghost')}</div></section>${footer()}`;
  }

  function accountUnavailable(title) {
    const inactive = Number(currentUser?.mail_status) === 2 || currentUser?.status === 'inactive';
    const headingText = inactive ? '社区账号尚未激活' : '当前社区账号不可参与操作';
    const description = inactive ? 'Answer 要求先验证邮箱。请进入原生激活页面重新发送邮件；METAR 不会绕过该状态。' : '账号状态由 Answer 管理，请在原生账号页面查看封禁或停用原因。';
    const action = inactive ? external('/users/login?status=inactive', '重新发送激活邮件', 'btn primary') : external(config.answerSettingsPath, '查看账号状态', 'btn primary');
    return `${crumb([[title]])}<section class="card prod-login-card">${I('shield', 'lg')}<h1 class="mt16">${headingText}</h1><p>${description}</p><div class="flex wrap">${action}${link('/discover', '继续浏览社区', 'btn ghost')}</div></section>${footer()}`;
  }

  async function profilePage() {
    if (currentUserState === 'unavailable') return identityUnavailable('个人空间');
    if (!currentUser) return loginRequired('个人空间', '登录后可查看由 Answer 管理的个人资料与发布记录。');
    const username = currentUser.username;
    const [profile, questions] = await Promise.all([answer.getProfile(username), answer.listPersonalQuestions(username)]);
    const list = Array.isArray(questions?.list) ? questions.list : [];
    return `${crumb([['个人空间']])}<div class="content-grid"><div class="stack"><section class="card prod-user-card"><div class="between wrap"><span class="avatar a4 large">${esc(initial(profile))}</span>${external(config.answerSettingsPath, I('settings', 'sm') + '编辑 Answer 资料', 'btn')}</div><h1 class="mt16">${esc(displayName(profile))}</h1><p class="muted mt8">@${esc(profile.username || username)}${profile.location ? ` · ${esc(profile.location)}` : ''}</p><p class="mt16">${esc(profile.bio || '这位成员暂未填写个人简介。')}</p><div class="profile-numbers"><div><strong>${number(questions?.count)}</strong><span>发布问题</span></div><div><strong>${number(profile.answer_count)}</strong><span>参与回答</span></div><div><strong>${number(profile.rank)}</strong><span>社区声望</span></div></div>${currentUser.mail_status === 2 ? `<div class="prod-status error mt16">${I('shield')}<div><strong>邮箱尚未激活</strong><p>请进入 Answer 账号页面重新发送激活邮件；前端不会绕过激活要求。</p></div></div>` : ''}</section><section class="card prod-feed"><div class="section-heading"><h2>最近发布</h2><span class="muted">Answer 实时数据</span></div>${list.length ? list.map(questionRow).join('') : empty('还没有发布问题', '从一个具体、可复现的问题开始。', external(config.answerAskPath, '发起问题', 'btn primary'), 'chat')}</section></div><aside class="stack"><section class="card card-pad"><h3>账号快捷入口</h3><ul class="mini-list"><li>${link('/bookmarks', '<span>我的收藏</span><small>Answer</small>')}</li><li>${link('/notifications', '<span>通知中心</span><small>Answer</small>')}</li><li>${link('/settings/binding', '<span>元衡账号绑定</span><small>可选</small>')}</li><li>${link('/pulse', '<span>Pulse 权益</span><small>绑定后</small>')}</li></ul></section></aside></div>${footer()}`;
  }

  async function bookmarksPage() {
    if (currentUserState === 'unavailable') return identityUnavailable('我的收藏');
    if (!currentUser) return loginRequired('我的收藏', '收藏内容由 Answer 管理。');
    if (!isActiveUser(currentUser)) return accountUnavailable('我的收藏');
    const result = await answer.listBookmarks(currentUser.username);
    const list = Array.isArray(result?.list) ? result.list : [];
    return `${crumb([['我的收藏']])}${heading('我的收藏', '实时读取当前 Answer 账号的收藏记录。')}<section class="card prod-feed">${list.length ? list.map(questionRow).join('') : empty('还没有收藏内容', '在 Answer 完整问题页收藏后，这里会显示对应记录。', link('/questions', '浏览问题', 'btn primary'), 'bookmark')}</section>${footer()}`;
  }

  async function notificationsPage() {
    if (currentUserState === 'unavailable') return identityUnavailable('通知中心');
    if (!currentUser) return loginRequired('通知中心', '通知由 Answer 账号和权限系统管理。');
    if (!isActiveUser(currentUser)) return accountUnavailable('通知中心');
    const result = await answer.listNotifications();
    const list = Array.isArray(result?.list) ? result.list : [];
    return `${crumb([['通知中心']])}${heading('通知中心', '只展示 Answer 返回的本人通知，不在浏览器伪造已读状态。', external('/notifications', '进入完整通知页', 'btn'))}<section class="card">${list.length ? list.map((item) => { const object = item.object_info || {}; const title = object.title || item.notification_action || '社区通知'; return `<article class="result-row"><div class="between">${external(notificationPath(item), `<h3>${esc(title)}</h3>`)}${item.is_read ? badge('已读') : badge('未读', 'green')}</div><p>${esc(item.user_info ? `${displayName(item.user_info)} ${item.notification_action || ''}`.trim() : item.notification_action || '查看完整通知了解详情。')}</p><small class="muted">${time(item.update_time)}</small></article>`; }).join('') : empty('暂时没有新通知', '回答、提及和系统消息会由 Answer 写入这里。', link('/questions', '返回社区', 'btn'), 'bell')}</section>${footer()}`;
  }

  async function bindingPage() {
    if (currentUserState === 'unavailable') return identityUnavailable('账号绑定');
    if (!currentUser) return loginRequired('账号绑定', '先登录独立社区账号，再自主选择是否连接元衡 API 身份。');
    if (!isActiveUser(currentUser)) return accountUnavailable('账号绑定');
    const state = await answer.getBindingState();
    if (state.status === 'unavailable') return `${crumb([['账号绑定']])}${heading('账号绑定', '社区身份与 API 身份保持独立。')}<section class="card card-pad"><div class="prod-status error">${I('server')}<div><strong>绑定服务暂不可用</strong><p>服务器没有返回 Pulse UserCenter Connector。社区浏览、登录、发帖和回答仍然可用。</p></div></div></section>${footer()}`;
    const connectorPath = safeSameOriginPath(state.connector?.link, config.answerBindingPath);
    const bound = state.status === 'bound';
    return `${crumb([['账号绑定']])}${heading('连接元衡 API 账号', '绑定是可选的一对一关系，不会合并两个账号、密码或余额。')}<section class="card card-pad"><div class="between wrap"><h2>元衡 API 身份</h2>${badge(bound ? I('check', 'sm') + ' 已绑定' : '未绑定', bound ? 'green' : '')}</div>${bound ? `<div class="prod-binding-account"><div class="topic-icon">${I('link')}</div><div><strong>已通过可信回调完成绑定</strong><p class="muted">服务端已确认一对一关系；页面不会展示外部用户 ID 或任何 API 凭据。</p></div>${badge('受保护关系', 'green')}</div><dl class="info-pairs"><dt>社区账号</dt><dd>${esc(displayName(currentUser))}</dd><dt>社区身份事实源</dt><dd>Apache Answer</dd><dt>API / 资金身份事实源</dt><dd>new-api</dd><dt>普通解绑或换绑</dt><dd>不开放；纠错需要支持流程与审计</dd></dl><div class="flex wrap mt24">${link('/pulse', '查看 Pulse 状态 ' + I('arrow', 'sm'), 'btn primary')}${link('/support', '联系支持', 'btn')}</div>` : `<div class="prod-binding-steps"><div class="prod-binding-step"><span class="number">1</span><strong>确认社区身份</strong><p>当前登录：${esc(displayName(currentUser))}</p></div><div class="prod-binding-step"><span class="number">2</span><strong>前往元衡授权</strong><p>由 Connector、浏览器 flow 和固定 callback 校验 API 身份。</p></div><div class="prod-binding-step"><span class="number">3</span><strong>建立一对一关系</strong><p>不按同名邮箱静默合并，冲突时拒绝覆盖。</p></div></div><div class="prod-status">${I('shield')}<div><strong>开始前请确认</strong><p>绑定后不能普通自助解绑或换绑；不会读取 API Key，不会改变社区密码或治理角色。</p></div></div><div class="flex wrap mt24">${external(connectorPath, I('link') + '开始安全绑定', 'btn primary')}${link('/discover', '暂不绑定', 'btn ghost')}</div>`}</section><section class="card card-pad mt24"><h3>身份边界</h3><p class="muted mt8">浏览器不能提交可信 user_id。所有回调参数在服务端验签、校验 flow 与 nonce 前都视为不可信输入。</p></section>${footer()}`;
  }

  async function pulsePage() {
    if (currentUserState === 'unavailable') return identityUnavailable('Pulse 权益');
    if (!currentUser) return loginRequired('Pulse 权益', 'Pulse 只对主动绑定元衡 API 身份的社区成员展示本人权益。');
    if (!isActiveUser(currentUser)) return accountUnavailable('Pulse 权益');
    const binding = await answer.getBindingState();
    if (binding.status === 'unbound') return `${crumb([['Pulse 权益']])}${heading('Pulse 权益', '调用之后的增长与权益系统。')}<section class="pulse-hero"><div><div class="eyebrow">MARGIN-AWARE LOYALTY</div><h1>先完成可选账号绑定</h1><p>社区账号可以独立使用。只有当你希望查看基于真实付费调用产生的等级、券和回馈时，才需要连接元衡 API 身份。</p><div class="actions">${link('/settings/binding', '了解并开始绑定 ' + I('arrow', 'sm'), 'btn light')}${link('/questions', '继续浏览社区', 'btn outline-light')}</div></div>${I('pulse')}</section>${footer()}`;
    if (binding.status === 'unavailable') throw new AdapterError('绑定状态暂时不可查询，Pulse 页面不会据此猜测身份。', { code: 'binding_unavailable' });
    return `${crumb([['Pulse 权益']])}${heading('Pulse 权益', '社区已确认绑定关系，但用户权益接口仍必须经过安全 BFF。')}<section class="pulse-hero"><div><div class="eyebrow">SECURE INTEGRATION IN PROGRESS</div><h1>已绑定，权益数据暂未开放</h1><p>当前前端不会使用固定等级、余额、券或奖励占位真实数据。社区 BFF 完成身份派生、权限、幂等与降级验收后，这里才会读取本人 Pulse 投影。</p><div class="actions">${outbound(config.consoleUrl, '打开元衡控制台 ' + I('external', 'sm'), 'btn light')}${link('/settings/binding', '查看绑定状态', 'btn outline-light')}</div></div>${I('shield')}</section><section class="card card-pad mt24"><h3>为什么这里没有“模拟余额”</h3><p class="muted mt8">Ledger 是 Pulse 事实源，Account 只是派生快照。浏览器无权决定预算、概率、券消费或最终到账；服务状态不确定时必须查询原记录，不能重新随机或换 source_ref 发放。</p></section>${footer()}`;
  }

  function authPage(kind) {
    const map = {
      login: ['登录社区账号', '登录与密码继续由 Apache Answer 安全处理。', config.answerLoginPath, '前往登录'],
      register: ['独立注册 METAR', '注册社区不要求绑定元衡 API 账号。', config.answerRegisterPath, '前往注册'],
      forgot: ['找回密码', '邮件验证、密码重置与账号状态由 Apache Answer 管理。', config.answerPasswordResetPath, '前往找回'],
    };
    const [title, description, path, button] = map[kind];
    return `${crumb([[title]])}<section class="card prod-login-card">${I(kind === 'register' ? 'user' : 'shield', 'lg')}<h1 class="mt16">${title}</h1><p>${description} 该入口不会把社区密码交给 new-api 或 Pulse。</p><div class="flex wrap">${external(path, button + ' ' + I('arrow', 'sm'), 'btn primary')}${kind !== 'register' ? link('/register', '独立注册', 'btn') : link('/login', '已有账号，去登录', 'btn')}${link('/discover', '返回社区', 'btn ghost')}</div></section>${footer()}`;
  }

  function publishPage() {
    return `${crumb([['发布内容']])}${heading('发布到社区', '提问、编辑、草稿、审核和附件继续由 Apache Answer 处理。')}<section class="card card-pad"><div class="prod-status">${I('shield')}<div><strong>使用 Answer 原生编辑器</strong><p>当前阶段不复制发布接口，避免遗漏验证码、激活状态、内容审核、附件和权限规则。</p></div></div><div class="flex wrap mt24">${external(config.answerAskPath, I('plus') + '发起问题', 'btn primary')}${external('/questions', '浏览完整论坛', 'btn')}${link('/guidelines', '阅读社区规范', 'btn ghost')}</div></section>${footer()}`;
  }

  function supportPage() {
    return `${crumb([['帮助中心']])}${heading('帮助中心', '根据问题类型选择正确的事实源，避免重复提交敏感信息。')}<div class="prod-topic-grid"><section class="card prod-topic-card"><div><div class="topic-icon">${I('user')}</div><h3>社区账号与内容</h3><p>登录、注册、邮箱激活、封禁、资料、发帖和回答由 Answer 管理。</p></div>${external('/users/settings/profile', '打开账号设置 ' + I('arrow', 'sm'), 'textlink')}</section><section class="card prod-topic-card"><div><div class="topic-icon">${I('link')}</div><h3>账号绑定纠错</h3><p>绑定冲突、误绑核对不提供普通解绑；处理需要确认授权并保留审计。</p></div>${link('/settings/binding', '查看绑定状态 ' + I('arrow', 'sm'), 'textlink')}</section><section class="card prod-topic-card"><div><div class="topic-icon">${I('pulse')}</div><h3>Pulse 奖励状态</h3><p>请保留非敏感 Reward Grant ID。不要提交密码、Cookie、API Key 或完整回调 URL。</p></div>${link('/pulse', '查看权益入口 ' + I('arrow', 'sm'), 'textlink')}</section></div>${footer()}`;
  }

  async function statusPage() {
    await answer.listQuestions({ pageSize: 1, order: 'active' });
    return `${crumb([['服务状态']])}${heading('服务状态', '状态来自当前页面对 Answer 公共 API 的即时检查。')}<section class="card card-pad"><div class="prod-status">${I('check')}<div><strong>社区读取服务正常</strong><p>Apache Answer 公共问题接口已返回。此结果不代表 Pulse、new-api、邮件或奖励结算服务均正常。</p></div></div><div class="divider"></div><div class="between wrap"><div><h3>更完整的运行状态</h3><p class="prod-page-note">只有配置真实监控来源后才显示外部状态页，不使用前端假数据。</p></div>${config.statusUrl ? outbound(config.statusUrl, '打开状态页 ' + I('external', 'sm'), 'btn') : badge('状态页未配置')}</div></section>${footer()}`;
  }

  function guidelinesPage() {
    return `${crumb([['社区规范']])}${heading('社区规范', '让真实问题、可验证经验和安全边界成为默认。')}<section class="card card-pad stack"><div><h2>1. 描述可复现的问题</h2><p class="muted mt8">说明目标、环境、已尝试方法和实际结果；不要泄露 API Key、Cookie、支付信息、完整 Prompt/Response 或个人隐私。</p></div><div><h2>2. 内容不自动产生权益</h2><p class="muted mt8">论坛发帖、回答、点赞不得产生 contribution 或 ticket。内容奖励使用独立资格、预算与人工审核。</p></div><div><h2>3. 尊重身份边界</h2><p class="muted mt8">社区身份来自 Answer，API 与资金身份来自 new-api。绑定可选、一对一，禁止静默换绑和身份转移。</p></div><div><h2>4. 对不确定结果先查询</h2><p class="muted mt8">奖励发放中或服务超时时，查询原 Grant 和 source_ref，不重复开启或重新随机。</p></div></section>${footer()}`;
  }

  function notFoundPage() {
    return `${crumb([['页面不存在']])}<section class="card">${empty('没有找到这个页面', '返回发现页，或打开 Answer 原始问题列表。', link('/discover', '返回发现', 'btn primary') + external('/questions', 'Answer 问题列表', 'btn'), 'compass')}</section>${footer()}`;
  }

  async function resolveView() {
    const { path } = route();
    if (path === '/discover') return discoverPage();
    if (path === '/questions') return questionsPage();
    if (path === '/topics') return topicsPage();
    if (path === '/knowledge') return knowledgePage();
    if (path === '/search') return searchPage();
    if (path === '/me') return profilePage();
    if (path === '/bookmarks') return bookmarksPage();
    if (path === '/notifications') return notificationsPage();
    if (path === '/settings/binding') return bindingPage();
    if (path === '/pulse') return pulsePage();
    if (path === '/publish') return publishPage();
    if (path === '/login') return authPage('login');
    if (path === '/register') return authPage('register');
    if (path === '/forgot') return authPage('forgot');
    if (path === '/support') return supportPage();
    if (path === '/status') return statusPage();
    if (path === '/guidelines') return guidelinesPage();
    if (path.startsWith('/question/')) return questionPage(decodeURIComponent(path.slice('/question/'.length)));
    if (path.startsWith('/topic/')) return questionsPage(decodeURIComponent(path.slice('/topic/'.length)));
    return notFoundPage();
  }

  function closeMobileMenu() {
    document.body.classList.remove('menu-open');
    document.querySelector('[data-action="menu"]')?.setAttribute('aria-expanded', 'false');
  }

  async function navigate() {
    const sequence = ++navigationSequence;
    closeMobileMenu();
    renderShell();
    try {
      const html = await resolveView();
      if (sequence === navigationSequence) renderView(html);
    } catch (error) {
      console.error('METAR view load failed', error);
      if (sequence === navigationSequence) renderError(error);
    }
    window.scrollTo({ top: 0, behavior: 'auto' });
  }

  function syncThemeControl() {
    const dark = document.documentElement.dataset.theme === 'dark';
    const button = document.querySelector('[data-action="theme"]');
    if (!button) return;
    button.innerHTML = I(dark ? 'sun' : 'moon');
    button.setAttribute('aria-label', dark ? '切换到浅色主题' : '切换到深色主题');
  }

  function setTheme(theme) {
    document.documentElement.dataset.theme = theme === 'dark' ? 'dark' : 'light';
    try { window.localStorage.setItem('_metar_theme', document.documentElement.dataset.theme); } catch (_) { /* UI preference only */ }
    syncThemeControl();
  }

  function initializeTheme() {
    let saved = '';
    try { saved = window.localStorage.getItem('_metar_theme') || ''; } catch (_) { /* use system */ }
    setTheme(saved || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'));
  }

  async function initializeIdentity() {
    currentUserState = 'loading';
    currentUser = null;
    identityError = null;
    const snapshot = await loadIdentitySnapshot(() => answer.getCurrentUser());
    currentUser = snapshot.user;
    currentUserState = snapshot.state;
    identityError = snapshot.error;
    if (identityError) console.warn('METAR identity bootstrap failed', identityError);
  }

  document.addEventListener('submit', (event) => {
    const form = event.target.closest('form[data-form="search"]');
    if (!form) return;
    event.preventDefault();
    const query = new FormData(form).get('q')?.toString().trim() || '';
    if (query) location.hash = `/search?q=${encodeURIComponent(query)}`;
  });

  document.addEventListener('click', (event) => {
    const action = event.target.closest('[data-action]')?.dataset.action;
    if (!action) return;
    if (action === 'skip') {
      event.preventDefault();
      document.getElementById('main')?.focus({ preventScroll: false });
    }
    if (action === 'menu') {
      const opened = document.body.classList.toggle('menu-open');
      document.querySelector('[data-action="menu"]')?.setAttribute('aria-expanded', String(opened));
    }
    if (action === 'theme') setTheme(document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark');
    if (action === 'retry') navigate();
    if (action === 'retry-identity') initializeIdentity().then(navigate);
  });

  document.addEventListener('keydown', (event) => {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
      event.preventDefault();
      document.querySelector('.searchbox input')?.focus();
    }
    if (event.key === 'Escape') closeMobileMenu();
  });

  window.addEventListener('hashchange', navigate);

  initializeTheme();
  initializeIdentity().finally(navigate);
})();
