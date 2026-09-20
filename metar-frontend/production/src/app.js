/* METAR production community shell. All community data comes from Apache Answer. */
'use strict';
(() => {
  const { AdapterError, AnswerAdapter, PulseAdminAdapter, isCommunityAdministrator, PulseAdapter, PulseOperation, formatPulseQuota, KnowledgeAdapter, loadIdentitySnapshot, relativePath, routeMatchesNavigation } = window.MetarAdapters;
  const { markup: avatar, install: installAvatars } = window.MetarAvatars;
  const { t, locale, getLanguage, setLanguage, syncDocument, errorMessage, countLabel } = window.MetarI18n;
  const rawConfig = window.__METAR_RUNTIME_CONFIG__ || {};
  const config = Object.freeze({
    siteName: typeof rawConfig.siteName === 'string' ? rawConfig.siteName : 'METAR',
    siteTagline: typeof rawConfig.siteTagline === 'string' ? rawConfig.siteTagline : "与创造者一起，把 AI 用出价值",
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
  const pulse = new PulseAdapter(answer);
  const growth = new window.MetarGrowth.View(answer, new AnswerAdapter({answerApiBase: "/answer/admin/api"}));
  const pulseAdmin = new window.MetarPulseAdmin.View(new PulseAdminAdapter(answer));
  let pulseBusy = false;
  let pulseMessage = "";
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

  const router = window.MetarRouter.create(window);
  router.migrate();
  const route = router.route;
  const link = (path, label, className = '') => `<a href="${esc(window.MetarRouter.href(path))}" data-router class="${esc(className)}">${label}</a>`;
  const external = (path, label, className = '') => `<a href="${esc(path)}" class="${esc(className)}">${label}</a>`;
  const outbound = (url, label, className = '') => url ? `<a href="${esc(url)}" class="${esc(className)}" target="_blank" rel="noopener noreferrer">${label}</a>` : '';
  const badge = (label, className = '') => `<span class="badge ${className}">${label}</span>`;
  const crumb = (parts = []) => `<nav class="breadcrumb" aria-label="${t("面包屑")}">${link('/latest', t("社区"))}${parts.map(([label, path]) => `<span>/</span>${path ? link(path, esc(label)) : `<span aria-current="page">${esc(label)}</span>`}`).join('')}</nav>`;
  const heading = (title, description = '', action = '') => `<div class="page-heading"><div><h1>${esc(title)}</h1>${description ? `<p>${esc(description)}</p>` : ''}</div>${action}</div>`;
  const empty = (title, description, action = '', icon = 'inbox') => `<div class="prod-empty">${I(icon)}<h2>${esc(title)}</h2><p>${esc(description)}</p>${action}</div>`;
  const loading = () => `<div class="prod-loading"><div><div class="loader"></div><p>${t("正在读取社区实时数据…")}</p></div></div>`;
  const displayName = (user) => user?.display_name || user?.username || t("社区成员");
  const isActiveUser = (user) => Boolean(user) && Number(user.mail_status) === 1 && !['inactive', 'suspended', 'deleted'].includes(String(user.status || 'normal'));
  const number = (value) => new Intl.NumberFormat(locale()).format(Number(value) || 0);
  const time = (value) => {
    const timestamp = Number(value) || 0;
    if (!timestamp) return t("时间未知");
    const date = new Date(timestamp > 1e12 ? timestamp : timestamp * 1000);
    if (Number.isNaN(date.getTime())) return t("时间未知");
    const seconds = Math.round((date.getTime() - Date.now()) / 1000);
    const abs = Math.abs(seconds);
    const relative = new Intl.RelativeTimeFormat(locale(), { numeric: 'auto' });
    if (abs < 60) return relative.format(seconds, 'second');
    if (abs < 3600) return relative.format(Math.round(seconds / 60), 'minute');
    if (abs < 86400) return relative.format(Math.round(seconds / 3600), 'hour');
    if (abs < 2592000) return relative.format(Math.round(seconds / 86400), 'day');
    return date.toLocaleDateString(locale());
  };

  const { questionHref, profileHref, searchHref, nativeDestination } = window.MetarRouter;
  const publicAuthor = (user, label, className = '') => user?.username
    ? external(profileHref(user.username), label, className) : `<span class="${className}">${label}</span>`;

  function tagName(tag) { return tag?.display_name || tag?.slug_name || t("话题"); }

  function questionRow(question) {
    const author = question.user_info || question.operator || {};
    const operator = question.operator || author;
    const tags = Array.isArray(question.tags) ? question.tags : [];
    const stamp = question.operated_at || question.update_time || question.created_at || question.create_time;
    return `<article class="discussion-row">
      <div class="discussion-main">
        ${external(questionHref(question.id || question.question_id), `<h3>${question.pin === 2 ? I('flag', 'sm') + `<span class="visually-hidden">${t("置顶")} </span>` : ''}${esc(question.title)}</h3>`, 'discussion-title')}
        <div class="discussion-meta">${question.accepted_answer_id ? badge(I('check', 'sm') + t("已解决"), 'green') : ''}${tags.slice(0, 3).map((tag) => link(`/topic/${encodeURIComponent(tag.slug_name || tagName(tag))}`, esc(tagName(tag)), 'badge')).join('')}${publicAuthor(author, esc(displayName(author)), 'discussion-author')}</div>
      </div>
      <div class="discussion-people" aria-label="${t('最近参与者')}">${publicAuthor(operator, avatar(operator))}</div>
      <div class="discussion-count"><span class="visually-hidden">${t('回复')} </span>${number(question.answer_count)}</div>
      <div class="discussion-count discussion-views"><span class="visually-hidden">${t('浏览量')} </span>${number(question.view_count)}</div>
      <div class="discussion-activity"><span class="visually-hidden">${t('活动')} </span>${time(stamp)}</div>
    </article>`;
  }

  function discussionList(list) {
    return `<div class="discussion-list"><div class="discussion-columns" aria-hidden="true"><span>${t('话题')}</span><span>${t('参与者')}</span><span>${t('回复')}</span><span>${t('浏览量')}</span><span>${t('活动')}</span></div>${list.map(questionRow).join('')}</div>`;
  }

  function footer() {
    return `<footer class="prod-footer"><span>© 2026 ${esc(config.siteName)}</span><nav>${external('/tos', t("服务条款"))}${external('/privacy', t("隐私政策"))}${external('/users/register', t("加入社区"))}${link('/guidelines', t("社区规范"))}${link('/status', t("服务状态"))}</nav></footer>`;
  }

  function currentTitle() {
    const path = route().path;
    if (path.startsWith('/question/')) return t("问题详情");
    if (path.startsWith('/topic/')) return t("话题");
    return ({ '/latest': t("最新话题"), '/topics': t("全部标签"), '/knowledge': t("知识库"), '/search': t("搜索"), '/me': t("个人空间"), '/me/bookmarks': t("我的收藏"), '/me/growth': t('社区成长'), '/admin/growth': t('成长管理'), '/me/notifications': t("通知中心"), '/settings/binding': t("账号绑定"), '/pulse': t("Pulse 权益"), '/admin/pulse': t("Pulse 配置"), '/publish': t("发布内容"), '/login': t("登录"), '/register': t("注册"), '/forgot': t("找回密码"), '/status': t("服务状态"), '/support': t("帮助中心"), '/guidelines': t("社区规范") })[path] || t("METAR 社区");
  }

  function languageControl() {
    return `<select class="language-select" data-action="language" aria-label="${t('界面语言')}"><option value="zh_CN"${getLanguage() === 'zh_CN' ? ' selected' : ''}>中文</option><option value="en_US"${getLanguage() === 'en_US' ? ' selected' : ''}>English</option></select>`;
  }

  function topbar() {
    const path = route().path;
    const logged = currentUserState === 'ready' && currentUser;
    const identityUnavailableState = currentUserState === 'unavailable';
    const activeUser = logged && isActiveUser(currentUser);
    const active = (prefix) => path === prefix || path.startsWith(`${prefix}/`);
    return `<header class="topbar">
      <button class="icon-btn mobile-menu" type="button" data-action="menu" aria-label="${t("打开导航")}" aria-controls="community-sidebar" aria-expanded="false">${I('menu')}</button>
      ${link('/latest', LOGO + `<span class="brand-word">${esc(config.siteName.toLowerCase())}</span>`, 'brand')}
      <nav class="topnav" aria-label="${t("主导航")}">${link('/latest', t("社区"), active('/latest') || active('/question') || active('/topic') || active('/topics') ? 'active' : '')}${link('/knowledge', t("知识库"), active('/knowledge') ? 'active' : '')}${link('/pulse', 'Pulse', active('/pulse') ? 'active' : '')}${outbound(config.consoleUrl, t("开发者"))}</nav>
      <form class="searchbox" data-form="search" role="search">${I('search')}<input type="search" name="q" aria-label="${t("搜索社区")}" placeholder="${t("搜索真实问题与回答…")}" value="${path === '/search' ? esc(route().query.get('q') || '') : ''}" autocomplete="off"><kbd>⌘ K</kbd></form>
      <div class="header-actions">${languageControl()}<button type="button" class="icon-btn theme-btn" data-action="theme" aria-label="${document.documentElement.dataset.theme === 'dark' ? t("切换到浅色主题") : t("切换到深色主题")}">${I(document.documentElement.dataset.theme === 'dark' ? 'sun' : 'moon')}</button>${identityUnavailableState ? link('/me', I('server', 'sm') + t("身份服务暂不可用"), 'btn ghost') : logged ? (activeUser ? external('/users/notifications/inbox', I('bell') + `<span class="visually-hidden">${t("通知中心")}</span>`, 'icon-btn') + external(config.answerAskPath, I('plus') + `<span class="publish-label">${t("发布")}</span>`, 'btn primary') : external('/users/login?status=inactive', t("激活账号"), 'btn primary')) + link('/me', `${avatar(currentUser)}<span class="visually-hidden">${t("个人空间")}</span>`, 'icon-btn') : external(config.answerLoginPath, t("登录"), 'btn ghost') + external(config.answerRegisterPath, t("加入社区"), 'btn primary guest-register')}</div>
    </header>`;
  }

  function sidebar() {
    const { path, query } = route();
    const item = (url, label, icon) => link(url, I(icon) + `<span>${esc(label)}</span>`, `nav-item ${routeMatchesNavigation(path, query.toString(), url) ? 'active' : ''}`);
    return `<aside class="sidebar" id="community-sidebar" aria-label="${t("社区导航")}">
      <div class="nav-group"><div class="nav-label">${t("社区")}</div>${item('/latest', t("全部讨论"), 'chat')}${item('/latest?order=unanswered', t("待回答"), 'target')}${item('/topics', t("全部标签"), 'flag')}${item('/knowledge', t("知识库"), 'book')}</div>
      <div class="nav-group"><div class="nav-label">${t("我的空间")}</div>${item('/me', t("个人空间"), 'user')}${item('/me/growth', t('社区成长'), 'target')}${item('/me/bookmarks', t("我的收藏"), 'bookmark')}${external('/users/notifications/inbox', I('bell') + `<span>${t("通知中心")}</span>`, 'nav-item')}${item('/settings/binding', t("账号绑定"), 'link')}${external(config.answerSettingsPath, I('settings') + `<span>${t("账号设置")}</span>`, 'nav-item')}</div>
      ${currentUserState === 'ready' && isCommunityAdministrator(currentUser) ? `<div class="nav-group"><div class="nav-label">${t('管理')}</div>${item('/admin/pulse', t('Pulse 配置'), 'settings')}${item('/admin/growth', t('成长管理'), 'target')}${external('/admin/dashboard', I('shield') + `<span>${t('社区管理')}</span>`, 'nav-item')}${external('/admin/pulse_user_center', I('link') + `<span>${t('社区连接配置')}</span>`, 'nav-item')}</div>` : ''}
      <div class="side-bottom">${item('/pulse', t("Pulse 权益"), 'pulse')}${item('/support', t("帮助中心"), 'help')}${item('/status', t("服务状态"), 'server')}<div class="side-footer">${link('/guidelines', t("社区规范"))}${external('/sitemap.xml', t("站点地图"))}</div></div>
    </aside>`;
  }

  function mobileBottom() {
    const { path, query } = route();
    return `<nav class="mobile-bottom" aria-label="${t("移动端主导航")}">${[['/latest', t("话题"), 'chat'], ['/topics', t("标签"), 'flag'], ['/knowledge', t("知识库"), 'book'], ['/pulse', 'Pulse', 'pulse'], ['/me', t("我的"), 'user']].map(([url, label, icon]) => link(url, I(icon) + label, routeMatchesNavigation(path, query.toString(), url) ? 'active' : '')).join('')}</nav>`;
  }

  function renderShell() {
    syncDocument();
    app.innerHTML = `${topbar()}${sidebar()}<main class="page" id="main" tabindex="-1"><div class="page-inner" id="view">${loading()}</div></main>${mobileBottom()}`;
    app.setAttribute('aria-busy', 'true');
    document.title = `${currentTitle()} · ${config.siteName}`;
    window.MetarSEO?.update();
  }

  function renderView(html) {
    const view = document.getElementById('view');
    if (view) view.innerHTML = html;
    app.setAttribute('aria-busy', 'false');
    window.MetarSEO?.update();
    document.getElementById('main')?.focus({ preventScroll: true });
  }

  function renderError(error) {
    const inactive = error instanceof AdapterError && error.code === 'inactive';
    const message = inactive ? t("当前社区账号尚未激活，请先完成邮箱验证。") : errorMessage(error) || t("页面暂时无法加载。");
    renderView(`${crumb([[t("加载失败")]])}<section class="card prod-error">${I(inactive ? 'shield' : 'server')}<h2>${inactive ? t("账号尚未激活") : t("暂时没有读到社区数据")}</h2><p>${esc(message)}${t(" 社区数据不会由前端猜测或使用缓存数字替代。")}</p><div class="flex wrap prod-center-actions"><button type="button" class="btn primary" data-action="retry">${I('refresh')}${t("重新加载")}</button>${inactive ? external('/users/login?status=inactive', t("重新发送激活邮件"), 'btn') : external('/questions', t("浏览社区话题"), 'btn')}</div></section>${footer()}`);
    window.MetarSEO?.update(true);
  }

  const questionOrders = () => ({ active: t("最近活跃"), newest: t("最新发布"), hot: t("热门"), score: t("高赞"), unanswered: t("待回答") });

  async function questionsPage(tag = '') {
    const { query } = route();
    const requested = query.get('order') || 'active';
    const orders = questionOrders();
    const order = Object.prototype.hasOwnProperty.call(orders, requested) ? requested : 'active';
    const page = Math.max(1, Number.parseInt(query.get('page') || '1', 10) || 1);
    const result = await answer.listQuestions({ page, pageSize: 20, order, tag });
    const list = Array.isArray(result?.list) ? result.list : [];
    const total = Number(result?.count) || 0;
    const base = tag ? `/topic/${encodeURIComponent(tag)}` : '/latest';
    const title = tag || t("最新话题");
    const tabs = Object.entries(orders).map(([value, label]) => link(`${base}?order=${value}`, `<span${order === value ? ' aria-current="page"' : ''}>${esc(label)}</span>`, `discussion-tab ${order === value ? 'active' : ''}`)).join('');
    const totalPages = Math.max(1, Math.ceil(total / 20));
    const pager = totalPages > 1 ? `<div class="prod-pager">${page > 1 ? link(`${base}?order=${order}&page=${page - 1}`, t("上一页"), 'btn small') : ''}<span>${t("第 {page} / {total} 页", { page: number(page), total: number(totalPages) })}</span>${page < totalPages ? link(`${base}?order=${order}&page=${page + 1}`, t("下一页"), 'btn small') : ''}</div>` : '';
    return `<section class="community-discussions">
      <div class="community-heading"><div><span class="eyebrow">METAR COMMUNITY</span><h1>${esc(title)}</h1></div>${link('/topics', I('flag', 'sm') + t('全部标签'), 'btn small')}</div>
      <div class="community-notice">${t('分享经验，认真提问，一起把 AI 用好。')}${link('/guidelines', t('社区规范'), 'textlink')}</div>
      <div class="discussion-toolbar"><nav class="discussion-tabs" aria-label="${t('话题筛选')}">${tabs}</nav>${external(config.answerAskPath, I('plus', 'sm') + t('新建话题'), 'btn primary')}</div>
      ${list.length ? discussionList(list) : empty(order === 'unanswered' ? t("暂时没有待回答问题") : t("当前筛选没有内容"), t("可以调整筛选，或发起一个新问题。"), external(config.answerAskPath, t("发起提问"), 'btn primary'), 'chat')}${pager}
    </section>${footer()}`;
  }

  async function topicsPage() {
    const result = await answer.listTags({ pageSize: 48, order: 'popular' });
    const tags = Array.isArray(result?.list) ? result.list : [];
    return `${crumb([[t("全部标签")]])}${heading(t("全部标签"), t("从真实社区标签中找到你正在探索的方向。"))}<div class="prod-topic-grid">${tags.length ? tags.map((tag) => `<article class="card prod-topic-card"><div><div class="topic-icon">${I('flag')}</div><h3>${esc(tagName(tag))}</h3><p>${esc(tag.excerpt || tag.description || t("该话题暂未添加介绍。"))}</p></div><div class="between"><span class="muted">${countLabel(tag.question_count, 'questions')}</span>${link(`/topic/${encodeURIComponent(tag.slug_name)}`, t("进入话题 ") + I('arrow', 'sm'), 'textlink')}</div></article>`).join('') : `<section class="card">${empty(t("还没有公开话题"), t("话题将随社区内容逐步建立。"), external(config.answerAskPath, t("发起问题"), 'btn primary'), 'flag')}</section>`}</div>${footer()}`;
  }

  async function searchPage() {
    const query = (route().query.get('q') || '').trim();
    const form = `<form class="prod-search-form" data-form="search"><input type="search" name="q" value="${esc(query)}" maxlength="60" required placeholder="${t("输入问题、模型或接入关键词")}"><button type="submit" class="btn primary">${I('search')}${t("搜索")}</button></form>`;
    if (!query) return `${crumb([[t("搜索")]])}${heading(t("搜索社区"), t("搜索社区中的问题与回答。"))}${form}<section class="card">${empty(t("输入一个关键词开始搜索"), t("例如：API、Agent、模型评测、错误处理。"), '', 'search')}</section>${footer()}`;
    const result = await answer.search(query);
    const list = Array.isArray(result?.list) ? result.list : [];
    return `${crumb([[t("搜索")]])}${heading(t("“{query}” 的结果", { query }), t("找到 {count} 条社区内容。", { count: number(result?.count) }))}${form}<section class="card">${list.length ? list.map((item) => { const object = item.object || {}; return `<article class="result-row">${badge(item.object_type === 'answer' ? t("回答") : t("问题"), item.object_type === 'answer' ? '' : 'green')}${external(searchHref(item), `<h3>${esc(object.title || t("社区内容"))}</h3>`)}<p>${esc(object.excerpt || t("该结果暂未提供摘要。"))}</p><small class="muted">${publicAuthor(object.user_info, esc(displayName(object.user_info)))} · ${time(object.created_at)}</small></article>`; }).join('') : empty(t("没有找到相关内容"), t("换一个更具体的关键词，或向社区发起新问题。"), external(config.answerAskPath, t("发起提问"), 'btn primary'), 'search')}</section>${footer()}`;
  }

  function knowledgePage() {
    const articles = knowledge.listArticles();
    return `${crumb([[t("知识库")]])}${heading(t("知识库"), t("模型评测、接入教程与实践经验。"), external(config.blogBasePath, t("打开完整知识库 ") + I('external', 'sm'), 'btn primary'))}<div class="prod-knowledge-grid">${articles.map((article) => `<a class="card prod-knowledge-card" href="${esc(article.href)}"><div>${badge(esc(article.category), 'green')}<h2 class="mt16">${esc(article.title)}</h2><p>${esc(article.description)}</p></div><span class="textlink">${t("阅读文章 ")}${I('arrow', 'sm')}</span></a>`).join('')}</div><section class="card card-pad mt24"><div class="between wrap"><div><h3>${t("找不到需要的接入说明？")}</h3><p class="muted mt8">${t("可以在社区发起提问，与其他成员一起讨论。")}</p></div>${external(config.answerAskPath, t("向社区提问"), 'btn')}</div></section>${footer()}`;
  }

  function loginRequired(title, description) {
    return `${crumb([[title]])}<section class="card prod-login-card">${I('shield', 'lg')}<h1 class="mt16">${esc(title)}</h1><p>${esc(description)}${t(" 社区账号可独立注册，不要求先绑定元衡 API 账号。")}</p><div class="flex wrap">${external(config.answerLoginPath, t("登录社区账号"), 'btn primary')}${external(config.answerRegisterPath, t("独立注册"), 'btn')}${link('/latest', t("先浏览社区"), 'btn ghost')}</div></section>${footer()}`;
  }

  function identityUnavailable(title) {
    const reason = errorMessage(identityError) || t("暂时无法确认当前社区登录状态。");
    return `${crumb([[title]])}<section class="card prod-login-card">${I('server', 'lg')}<h1 class="mt16">${t("身份服务暂不可用")}</h1><p>${esc(reason)}${t(" 这不代表账号已退出，请勿反复登录；仍可继续尝试浏览社区公开内容。")}</p><div class="flex wrap"><button type="button" class="btn primary" data-action="retry-identity">${I('refresh')}${t("重新确认身份")}</button>${link('/latest', t("继续浏览社区"), 'btn ghost')}</div></section>${footer()}`;
  }

  function accountUnavailable(title) {
    const inactive = Number(currentUser?.mail_status) === 2 || currentUser?.status === 'inactive';
    const headingText = inactive ? t("社区账号尚未激活") : t("当前社区账号不可参与操作");
    const description = inactive ? t("请先验证邮箱。可以在登录页面重新发送激活邮件。") : t("请在账号页面查看当前状态，或联系社区管理员。");
    const action = inactive ? external('/users/login?status=inactive', t("重新发送激活邮件"), 'btn primary') : external(config.answerSettingsPath, t("查看账号状态"), 'btn primary');
    return `${crumb([[title]])}<section class="card prod-login-card">${I('shield', 'lg')}<h1 class="mt16">${headingText}</h1><p>${description}</p><div class="flex wrap">${action}${link('/latest', t("继续浏览社区"), 'btn ghost')}</div></section>${footer()}`;
  }

  async function profilePage() {
    if (currentUserState === 'unavailable') return identityUnavailable(t("个人空间"));
    if (!currentUser) return loginRequired(t("个人空间"), t("登录后查看个人资料与发布记录。"));
    const username = currentUser.username;
    const [profile, questions, experience] = await Promise.all([answer.getProfile(username), answer.listPersonalQuestions(username), isActiveUser(currentUser) ? growth.compact() : Promise.resolve('')]);
    const list = Array.isArray(questions?.list) ? questions.list : [];
    return `${crumb([[t("个人空间")]])}<div class="content-grid"><div class="stack"><section class="card prod-user-card"><div class="between wrap">${avatar(profile, 'large')}<div class="flex wrap">${external(profileHref(username), t("公开主页"), 'btn')}${external(config.answerSettingsPath, I('settings', 'sm') + t("编辑资料"), 'btn')}</div></div><h1 class="mt16">${esc(displayName(profile))}</h1><p class="muted mt8">@${esc(profile.username || username)}${profile.location ? ` · ${esc(profile.location)}` : ''}</p><p class="mt16">${esc(profile.bio || t("这位成员暂未填写个人简介。"))}</p><div class="profile-numbers"><div><strong>${number(questions?.count)}</strong><span>${t("发布问题")}</span></div><div><strong>${number(profile.answer_count)}</strong><span>${t("参与回答")}</span></div><div><strong>${number(profile.rank)}</strong><span>${t("社区声望")}</span></div></div>${currentUser.mail_status === 2 ? `<div class="prod-status error mt16">${I('shield')}<div><strong>${t("邮箱尚未激活")}</strong><p>${t("请先验证邮箱，或在登录页面重新发送激活邮件。")}</p></div></div>` : ''}</section>${experience}<section class="card prod-feed"><div class="section-heading"><h2>${t("最近发布")}</h2><span class="muted">${t("发布记录")}</span></div>${list.length ? list.map((item) => questionRow({ ...item, user_info: profile })).join('') : empty(t("还没有发布问题"), t("从一个具体、可复现的问题开始。"), external(config.answerAskPath, t("发起问题"), 'btn primary'), 'chat')}</section></div><aside class="stack"><section class="card card-pad"><h3>${t("账号快捷入口")}</h3><ul class="mini-list"><li>${link('/me/bookmarks', `<span>${t("我的收藏")}</span>`)}</li><li>${external('/users/notifications/inbox', `<span>${t("通知中心")}</span>`)}</li><li>${link('/settings/binding', `<span>${t("元衡账号绑定")}</span><small>${t("可选")}</small>`)}</li><li>${link('/pulse', `<span>${t("Pulse 权益")}</span><small>${t("绑定后")}</small>`)}</li><li>${external('/users/logout', `<span>${t("退出登录")}</span>`)}</li></ul></section></aside></div>${footer()}`;
  }

  async function bookmarksPage() {
    if (currentUserState === 'unavailable') return identityUnavailable(t("我的收藏"));
    if (!currentUser) return loginRequired(t("我的收藏"), t("登录后查看收藏的话题。"));
    if (!isActiveUser(currentUser)) return accountUnavailable(t("我的收藏"));
    const page = Math.max(1, Number.parseInt(route().query.get('page') || '1', 10) || 1);
    const result = await answer.listBookmarks(currentUser.username, { page, pageSize: 20 });
    const totalPages = Math.max(1, Math.ceil((Number(result?.count) || 0) / 20));
    const pager = totalPages > 1 ? `<div class="prod-pager">${page > 1 ? link(`/me/bookmarks?page=${page - 1}`, t("上一页"), 'btn small') : ''}<span>${t("第 {page} / {total} 页", { page: number(page), total: number(totalPages) })}</span>${page < totalPages ? link(`/me/bookmarks?page=${page + 1}`, t("下一页"), 'btn small') : ''}</div>` : '';
    const list = Array.isArray(result?.list) ? result.list : [];
    return `${crumb([[t("我的收藏")]])}${heading(t("我的收藏"), t("你收藏的话题都在这里。"))}<section class="card prod-feed">${list.length ? list.map(questionRow).join('') : empty(t("还没有收藏内容"), t("在讨论页收藏感兴趣的话题，方便以后继续阅读。"), link('/latest', t("浏览问题"), 'btn primary'), 'bookmark')}</section>${pager}${footer()}`;
  }

  async function bindingPage() {
    if (currentUserState === 'unavailable') return identityUnavailable(t("账号绑定"));
    if (!currentUser) return loginRequired(t("账号绑定"), t("先登录独立社区账号，再自主选择是否连接元衡 API 身份。"));
    if (!isActiveUser(currentUser)) return accountUnavailable(t("账号绑定"));
    const state = await answer.getBindingState();
    if (state.status === 'unavailable') return `${crumb([[t("账号绑定")]])}${heading(t("账号绑定"), t("社区身份与 API 身份保持独立。"))}<section class="card card-pad"><div class="prod-status error">${I('server')}<div><strong>${t("绑定服务暂不可用")}</strong><p>${t("服务器没有返回 Pulse UserCenter Connector。社区浏览、登录、发帖和回答仍然可用。")}</p></div></div></section>${footer()}`;
    const connectorPath = safeSameOriginPath(state.connector?.link, config.answerBindingPath);
    const bound = state.status === 'bound';
    return `${crumb([[t("账号绑定")]])}${heading(t("连接元衡 API 账号"), t("绑定是可选的一对一关系，不会合并两个账号、密码或余额。"))}<section class="card card-pad"><div class="between wrap"><h2>${t("元衡 API 身份")}</h2>${badge(bound ? I('check', 'sm') + t(" 已绑定") : t("未绑定"), bound ? 'green' : '')}</div>${bound ? `<div class="prod-binding-account"><div class="topic-icon">${I('link')}</div><div><strong>${t("已通过可信回调完成绑定")}</strong><p class="muted">${t("服务端已确认一对一关系；页面不会展示外部用户 ID 或任何 API 凭据。")}</p></div>${badge(t("受保护关系"), 'green')}</div><dl class="info-pairs"><dt>${t("社区账号")}</dt><dd>${esc(displayName(currentUser))}</dd><dt>${t("社区身份事实源")}</dt><dd>Apache Answer</dd><dt>${t("API / 资金身份事实源")}</dt><dd>new-api</dd><dt>${t("普通解绑或换绑")}</dt><dd>${t("不开放；纠错需要支持流程与审计")}</dd></dl><div class="flex wrap mt24">${link('/pulse', t("查看 Pulse 状态 ") + I('arrow', 'sm'), 'btn primary')}${link('/support', t("联系支持"), 'btn')}</div>` : `<div class="prod-binding-steps"><div class="prod-binding-step"><span class="number">1</span><strong>${t("确认社区身份")}</strong><p>${t("当前登录：")}${esc(displayName(currentUser))}</p></div><div class="prod-binding-step"><span class="number">2</span><strong>${t("前往元衡授权")}</strong><p>${t("由 Connector、浏览器 flow 和固定 callback 校验 API 身份。")}</p></div><div class="prod-binding-step"><span class="number">3</span><strong>${t("建立一对一关系")}</strong><p>${t("不按同名邮箱静默合并，冲突时拒绝覆盖。")}</p></div></div><div class="prod-status">${I('shield')}<div><strong>${t("开始前请确认")}</strong><p>${t("绑定后不能普通自助解绑或换绑；不会读取 API Key，不会改变社区密码或治理角色。")}</p></div></div><div class="flex wrap mt24">${external(connectorPath, I('link') + t("开始安全绑定"), 'btn primary')}${link('/latest', t("暂不绑定"), 'btn ghost')}</div>`}</section><section class="card card-pad mt24"><h3>${t("身份边界")}</h3><p class="muted mt8">${t("浏览器不能提交可信 user_id。所有回调参数在服务端验签、校验 flow 与 nonce 前都视为不可信输入。")}</p></section>${footer()}`;
  }

  async function pulsePage() {
    if (currentUserState === 'unavailable') return identityUnavailable(t("Pulse 权益"));
    if (!currentUser) return loginRequired(t("Pulse 权益"), t("Pulse 只对主动绑定元衡 API 身份的社区成员展示本人权益。"));
    if (!isActiveUser(currentUser)) return accountUnavailable(t("Pulse 权益"));
    const binding = await answer.getBindingState();
    if (binding.status === 'unbound') return `${crumb([[t("Pulse 权益")]])}${heading(t("Pulse 权益"), t("调用之后的增长与权益系统。"))}<section class="pulse-hero"><div><div class="eyebrow">${t("付费调用回馈计划")}</div><h1>${t("先完成可选账号绑定")}</h1><p>${t("社区账号可以独立使用。只有当你希望查看基于真实付费调用产生的等级、券和回馈时，才需要连接元衡 API 身份。")}</p><div class="actions">${link('/settings/binding', t("了解并开始绑定 ") + I('arrow', 'sm'), 'btn light')}${link('/latest', t("继续浏览社区"), 'btn outline-light')}</div></div>${I('pulse')}</section>${footer()}`;
    if (binding.status === 'unavailable') throw new AdapterError(t("绑定状态暂时不可查询，Pulse 页面不会据此猜测身份。"), { code: 'binding_unavailable' });
    const [summary, rules, history] = await Promise.all([pulse.summary(), pulse.rules(), pulse.rewards()]);
    const operationStore = new PulseOperation(currentUser.id);
    let pending = operationStore.read();
    if (pending) {
      const recovery = await pulse.rewards(pending.actionId);
      if (Array.isArray(recovery.rewards) && recovery.rewards.some((r) => r.action_id === pending.actionId)) {
        operationStore.clear(); pending = null; pulseMessage = '已找到本次抽奖记录，请查看奖励明细。';
      }
    }
    const states = { pending: '发放中', settling: '发放中', settled: '已到账', reversed: '已撤销', failed: '等待处理', settlement_dead: '等待处理' };
    const unavailable = { budget_exhausted: '本期可用奖励预算已用完', activity_paused: '活动暂未开放', no_active_period: '当前没有进行中的活动', funding_verification_required: '本期权益正在核验', reward_pool_unavailable: '奖池准备中' };
    const available = Number.isSafeInteger(summary.available_tickets) ? summary.available_tickets : 0;
    const rewards = Array.isArray(history.rewards) ? history.rewards : [];
    const prizes = Array.isArray(rules.rewards) ? rules.rewards : [];
    const canDraw = rules.enabled === true && available > 0 && !pending && !pulseBusy;
    const rewardRows = rewards.map((r) => `<tr><td>${esc(formatPulseQuota(r.amount, rules.quota_per_unit, locale()))}</td><td>${esc(t(states[r.status] || '等待核对'))}</td><td>${esc(r.created_at ? new Date(r.created_at).toLocaleString(locale()) : '—')}</td><td><code>${esc(r.grant_id)}</code></td></tr>`).join('');
    const prizeRows = prizes.map((r) => `<li><strong>${esc(r.name)}</strong><span>${esc(formatPulseQuota(r.amount, rules.quota_per_unit, locale()))}</span><span>${esc(t('概率 {weight} / {total}', { weight: r.weight, total: rules.total_weight }))}</span></li>`).join('');
    return `${crumb([[t('Pulse 权益')]])}${heading(t('开启脉冲，获得调用回馈'), t('经核验的付费调用积累脉冲券，奖励自动发往已绑定的元衡 API 账号。'))}
      <section class="pulse-hero"><div><div class="eyebrow">${esc(rules.period?.key || t('元衡脉冲'))}</div><h1>${esc(t('可用脉冲券：{count}', { count: number(available) }))}</h1><p>${t('每次消耗 1 张券。奖励仅供 API 调用使用，不可转赠。')}</p>
      ${!rules.enabled ? `<p role="status">${esc(t(unavailable[rules.unavailable_reason] || '活动暂不可用'))}</p>` : ''}
      <div class="actions"><button type="button" class="btn light" data-action="pulse-draw" ${canDraw ? '' : 'disabled'}>${t(pulseBusy ? '正在处理…' : '开启一次脉冲 · 1 券')}</button><button type="button" class="btn outline-light" data-action="retry">${t('刷新奖励状态')}</button></div></div>${I('pulse')}</section>
      ${pulseMessage ? `<div class="prod-status mt24" role="status"><p>${esc(t(pulseMessage))}</p></div>` : ''}
      ${pending ? `<section class="card card-pad mt24" role="status"><h3>${t('正在确认上一次抽奖')}</h3><p class="muted mt8">${t('请先查询原请求。继续处理会沿用同一次抽奖，不会重新扣券或更换结果。')}</p><div class="flex wrap mt16"><button class="btn" data-action="retry">${t('查询原抽奖')}</button><button class="btn primary" data-action="pulse-resume" ${pulseBusy ? 'disabled' : ''}>${t('继续处理原请求')}</button></div></section>` : ''}
      <div class="prod-pulse-stats mt24"><section class="card card-pad"><h3>${t('当前等级')}</h3><p>${esc(summary.level?.name || t('未定级'))}</p></section><section class="card card-pad"><h3>${t('本期贡献')}</h3><p>${esc(Number.isSafeInteger(summary.current_contribution_milli) ? number(summary.current_contribution_milli / 1000) : t('待核对'))}</p></section><section class="card card-pad"><h3>${t('活动结束')}</h3><p>${esc(rules.period?.ends_at ? new Date(rules.period.ends_at).toLocaleString(locale()) : '—')}</p></section></div>
      <section class="card card-pad mt24"><h2>${t('本期奖池与规则')}</h2>${prizeRows ? `<ul class="prod-pulse-prizes">${prizeRows}</ul>` : `<p class="muted mt16">${t('暂无可参与奖池。')}</p>`}<p class="muted mt16">${t('奖项概率与产券规则在本期固定。预算不足不扣券；发放延迟会保留中奖结果。赠送额度和无法核验资金来源的消费不产生脉冲券。')}</p><p class="muted mt8">${t('API 额度按元衡账户的额度单位展示，不代表人民币或可提现金额。')}</p></section>
      <section class="card card-pad mt24"><h2>${t('奖励记录')}</h2><div class="prod-pulse-table"><table><thead><tr><th>${t('奖励')}</th><th>${t('到账状态')}</th><th>${t('时间')}</th><th>${t('奖励编号')}</th></tr></thead><tbody>${rewardRows || `<tr><td colspan="4">${t('暂无奖励记录')}</td></tr>`}</tbody></table></div></section>${footer()}`;
  }

  function supportPage() {
    return `${crumb([[t("帮助中心")]])}${heading(t("帮助中心"), t("查找账号、绑定和权益相关帮助。"))}<div class="prod-topic-grid"><section class="card prod-topic-card"><div><div class="topic-icon">${I('user')}</div><h3>${t("社区账号与内容")}</h3><p>${t("管理个人资料、登录方式和通知偏好。")}</p></div>${external('/users/settings/profile', t("打开账号设置 ") + I('arrow', 'sm'), 'textlink')}</section><section class="card prod-topic-card"><div><div class="topic-icon">${I('link')}</div><h3>${t("账号绑定纠错")}</h3><p>${t("绑定冲突、误绑核对不提供普通解绑；处理需要确认授权并保留审计。")}</p></div>${link('/settings/binding', t("查看绑定状态 ") + I('arrow', 'sm'), 'textlink')}</section><section class="card prod-topic-card"><div><div class="topic-icon">${I('pulse')}</div><h3>${t("Pulse 奖励状态")}</h3><p>${t("请保留非敏感 Reward Grant ID。不要提交密码、Cookie、API Key 或完整回调 URL。")}</p></div>${link('/pulse', t("查看权益入口 ") + I('arrow', 'sm'), 'textlink')}</section></div>${footer()}`;
  }

  async function statusPage() {
    await answer.listQuestions({ pageSize: 1, order: 'active' });
    return `${crumb([[t("服务状态")]])}${heading(t("服务状态"), t("状态来自当前页面对 Answer 公共 API 的即时检查。"))}<section class="card card-pad"><div class="prod-status">${I('check')}<div><strong>${t("社区读取服务正常")}</strong><p>${t("Apache Answer 公共问题接口已返回。此结果不代表 Pulse、new-api、邮件或奖励结算服务均正常。")}</p></div></div><div class="divider"></div><div class="between wrap"><div><h3>${t("更完整的运行状态")}</h3><p class="prod-page-note">${t("只有配置真实监控来源后才显示外部状态页，不使用前端假数据。")}</p></div>${config.statusUrl ? outbound(config.statusUrl, t("打开状态页 ") + I('external', 'sm'), 'btn') : badge(t("状态页未配置"))}</div></section>${footer()}`;
  }

  function guidelinesPage() {
    return `${crumb([[t("社区规范")]])}${heading(t("社区规范"), t("让真实问题、可验证经验和安全边界成为默认。"))}<section class="card card-pad stack"><div><h2>${t("1. 描述可复现的问题")}</h2><p class="muted mt8">${t("说明目标、环境、已尝试方法和实际结果；不要泄露 API Key、Cookie、支付信息、完整 Prompt/Response 或个人隐私。")}</p></div><div><h2>${t("2. 内容不自动产生权益")}</h2><p class="muted mt8">${t("论坛发帖、回答、点赞不得产生 contribution 或 ticket。内容奖励使用独立资格、预算与人工审核。")}</p></div><div><h2>${t("3. 尊重身份边界")}</h2><p class="muted mt8">${t("社区身份来自 Answer，API 与资金身份来自 new-api。绑定可选、一对一，禁止静默换绑和身份转移。")}</p></div><div><h2>${t("4. 对不确定结果先查询")}</h2><p class="muted mt8">${t("奖励发放中或服务超时时，查询原 Grant 和 source_ref，不重复开启或重新随机。")}</p></div></section>${footer()}`;
  }

  function notFoundPage() {
    return `${crumb([[t("页面不存在")]])}<section class="card">${empty(t("没有找到这个页面"), t("请检查地址，或返回社区继续浏览。"), link('/latest', t("返回发现"), 'btn primary'), 'compass')}</section>${footer()}`;
  }

  async function resolveView() {
    const { path } = route();
    if (path === '/latest') return questionsPage();
    if (path === '/topics') return topicsPage();
    if (path === '/knowledge') return knowledgePage();
    if (path === '/search') return searchPage();
    if (path === '/me') return profilePage();
    if (path === '/me/growth' || path === '/admin/growth') {
      const title = t(path === '/admin/growth' ? '成长管理' : '社区成长');
      if (currentUserState === 'unavailable') return identityUnavailable(title);
      if (!currentUser) return loginRequired(title, t('登录后查看个人资料与发布记录。'));
      if (!isActiveUser(currentUser)) return accountUnavailable(title);
      if (path === '/admin/growth' && !isCommunityAdministrator(currentUser)) return `${heading(title)}<section class="card">${empty(t('需要管理员权限'), '', link('/latest', t('返回社区'), 'btn'), 'shield')}</section>${footer()}`;
      return `${crumb([[title]])}${heading(title)}${await (path === '/admin/growth' ? growth.adminPage() : growth.page())}${footer()}`;
    }
    if (path === '/me/bookmarks') return bookmarksPage();
    if (path === '/settings/binding') return bindingPage();
    if (path === '/pulse') return pulsePage();
    if (path === '/admin/pulse') {
      if (currentUserState === 'unavailable') return identityUnavailable(t('Pulse 配置'));
      if (!currentUser) return loginRequired(t('Pulse 配置'), t('请使用社区管理员账号登录。'));
      if (!isCommunityAdministrator(currentUser)) return `${crumb([[t('Pulse 配置')]])}<section class="card">${empty(t('需要管理员权限'), t('仅正常且已激活的社区管理员可管理 Pulse 配置。'), link('/latest', t('返回社区'), 'btn'), 'shield')}</section>${footer()}`;
      return `${crumb([[t('Pulse 配置')]])}${heading(t('Pulse 配置'), t('管理 METAR 与 new-api 的连接、密钥和抽奖开关。'))}${await pulseAdmin.page()}${footer()}`;
    }
    if (path === '/support') return supportPage();
    if (path === '/status') return statusPage();
    if (path === '/guidelines') return guidelinesPage();
    if (path.startsWith('/topic/')) return questionsPage(decodeURIComponent(path.slice('/topic/'.length)));
    return notFoundPage();
  }

  async function submitPulse(resume = false) {
    if (pulseBusy || !currentUser || !isActiveUser(currentUser)) return;
    pulseBusy = true;
    document.querySelectorAll('[data-action="pulse-draw"], [data-action="pulse-resume"]').forEach((button) => { button.disabled = true; });
    const store = new PulseOperation(currentUser.id);
    try {
      const operation = resume ? store.read() : store.begin();
      if (!operation) return;
      const result = await pulse.act(operation);
      if (!result.grant_id || result.action_id !== operation.actionId) throw new AdapterError(t('本次结果仍待确认'), { code: 'action_pending' });
      store.clear();
      pulseMessage = result.status === 'settled' ? '奖励已到账。' : '抽奖已完成，奖励正在发放，请查看奖励记录。';
    } catch (error) {
      if (error.code === 'action_rejected') { store.clear(); pulseMessage = '本次未扣券，可能是可用券或活动预算不足，请刷新后查看。'; }
      else if (error.code === 'storage_unavailable') pulseMessage = '无法保存本次请求，请允许站点存储后重试';
      else pulseMessage = '暂时无法确认本次结果，请查询原抽奖或继续处理原请求。';
    } finally {
      pulseBusy = false;
      if (route().path === '/pulse') await navigate();
    }
  }

  function closeMobileMenu() {
    document.body.classList.remove('menu-open');
    document.querySelector('[data-action="menu"]')?.setAttribute('aria-expanded', 'false');
  }

  async function navigate() {
    const destination = nativeDestination(location, config);
    if (destination) { location.replace(destination); return; }
    const sequence = ++navigationSequence;
    closeMobileMenu();
    if (route().path !== '/admin/pulse') pulseAdmin.dispose();
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
    button.setAttribute('aria-label', dark ? t("切换到浅色主题") : t("切换到深色主题"));
  }

  function setTheme(theme) {
    window.MetarTheme.installTheme(window).select(theme);
  }

  function initializeTheme() {
    window.MetarTheme.installTheme(window).subscribe(syncThemeControl);
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

  document.addEventListener('change', (event) => {
    if (!event.target.matches?.('[data-action="language"]')) return;
    if (!setLanguage(event.target.value)) return;
    navigate().then(() => document.querySelector('[data-action="language"]')?.focus());
  });

  document.addEventListener('submit', (event) => {
    const growthForm = event.target.closest('form[data-growth-form]');
    if (growthForm) { event.preventDefault(); growth.submit(growthForm, navigate); return; }
    const periodForm = event.target.closest('form[data-form="admin-period"]');
    if (periodForm) { event.preventDefault(); pulseAdmin.periods?.submit(periodForm); return; }
    const adminForm = event.target.closest('form[data-form="admin-pulse"]');
    if (adminForm) { event.preventDefault(); pulseAdmin.submit(adminForm); return; }
    const form = event.target.closest('form[data-form="search"]');
    if (!form) return;
    event.preventDefault();
    const query = new FormData(form).get('q')?.toString().trim() || '';
    if (query && router.go(`/search?q=${encodeURIComponent(query)}`)) navigate();
  });

  document.addEventListener('click', (event) => {
    if (router.follow(event)) { navigate(); return; }
    const growthButton = event.target.closest('[data-growth]');
    if (growthButton?.dataset.growth) { event.preventDefault(); growth.click(growthButton, navigate); return; }
    const target = event.target.closest('[data-action]');
    const action = target?.dataset.action;
    if (!action) return;
    if (action.startsWith('admin-')) { pulseAdmin.action(action, target); return; }
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
    if (action === 'pulse-draw') submitPulse();
    if (action === 'pulse-resume') submitPulse(true);
    if (action === 'retry-identity') initializeIdentity().then(navigate);
  });

  document.addEventListener('keydown', (event) => {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
      event.preventDefault();
      document.querySelector('.searchbox input')?.focus();
    }
    if (event.key === 'Escape') closeMobileMenu();
  });

  window.addEventListener('popstate', navigate);
  window.addEventListener('hashchange', () => { if (router.migrate()) navigate(); });

  installAvatars();
  initializeTheme();
  if (nativeDestination(location, config)) navigate();
  else initializeIdentity().finally(navigate);
})();
