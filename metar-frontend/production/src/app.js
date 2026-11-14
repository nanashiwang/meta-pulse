/* METAR production community shell. All community data comes from Apache Answer. */
'use strict';
(() => {
  const { AdapterError, AnswerAdapter, PulseAdminAdapter, isCommunityAdministrator, PulseAdapter, PulseOperation, PulseDrawSession, pulseActions, validPulseResult, pulseRewardTier, formatPulseQuota, KnowledgeAdapter, loadIdentitySnapshot, relativePath, routeMatchesNavigation } = window.MetarAdapters;
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
  const taxonomy = window.MetarTaxonomy;
  let taxonomyBusy = false;
  const pulse = new PulseAdapter(answer);
  let growth = null, pulseAdmin = null, chatView = null, chatModeration = null, memberView = null, operationsView = null, marketplaceView = null, modelsView = null, modelsAdminView = null, publishingView = null, toolsView = null;
  // Single tool inventory for the top-bar menu and the /tools hub.
  const TOOLS = Object.freeze([
    {path: '/tools/network', zh: 'Claude 访问环境检测', en: 'Claude environment check', descriptionZh: '六项设备信号评分、Claude 域名出口、WebRTC 与 DNS 状态，以及完整检测说明。', descriptionEn: 'Six local signals, Claude egress probes, WebRTC and DNS status, with a detailed guide.'},
  ]);
  let chatUnreadTimer = 0, chatUnreadBusy = false;
  const featureScripts = new Map();
  function loadFeatureScript(name, globalName) {
    if (window[globalName]) return Promise.resolve();
    if (!featureScripts.has(name)) {
      const promise = new Promise((resolve, reject) => {
        const script = document.createElement('script');
        const finish = error => {
          clearTimeout(timer); script.onload = script.onerror = null;
          if (error) { script.remove(); reject(new AdapterError(t('页面暂时无法加载。'), {code:'network'})); }
          else resolve();
        };
        const timer = setTimeout(() => finish(true), 5000);
        script.onload = () => finish(!window[globalName]);
        script.onerror = () => finish(true);
        script.src = `/metar-assets/${name}.js`;
        document.head.appendChild(script);
      }).catch(error => { featureScripts.delete(name); throw error; });
      featureScripts.set(name, promise);
    }
    return featureScripts.get(name);
  }
  async function growthView() {
    if (!growth) {
      if (!window.MetarGrowth) {
        await loadFeatureScript('growth-presentation', 'MetarGrowthPresentation');
        await loadFeatureScript('growth', 'MetarGrowth');
      }
      growth ||= new window.MetarGrowth.View(answer, new AnswerAdapter({answerApiBase:'/answer/admin/api'}));
    }
    return growth;
  }
  async function adminView() {
    if (!pulseAdmin) {
      if (!window.MetarPulseAdmin) {
        await loadFeatureScript('admin-periods', 'MetarPeriodAdmin');
        await loadFeatureScript('admin-pulse', 'MetarPulseAdmin');
      }
      pulseAdmin ||= new window.MetarPulseAdmin.View(new PulseAdminAdapter(answer));
    }
    return pulseAdmin;
  }
  let pulseBusy = false;
  let pulseMessage = "";
  let pulseView = null;
  let pulseFeedView = null;
  let pulseCoreState = null;
  let pulseLastResult = null;
  let pulseAssets = null;
  let pulseCoreReady = false;
  let pulseRefreshing = false;
  let pulseStatusTimer = 0, pulseStatusAttempts = 0, pulseStatusEpoch = 0, pulseHasPendingRewards = false;
  const pulseRewardStates = { pending: '发放中', settling: '发放中', settled: '已到账', reversed: '已撤销', failed: '等待处理', settlement_dead: '等待处理' };
  const pulseRewardPending = reward => ['pending', 'settling'].includes(reward?.status);
  const knowledge = new KnowledgeAdapter(config);
  const app = document.getElementById('app');
  let navigationSequence = 0;
  let currentUser = null;
  let currentUserState = 'loading';
  let identityError = null;
  let identityReady = Promise.resolve(), identitySequence = 0;
  let shellReady = false, shellLanguage = '';
  let renderedKey = null, renderedLanguage = '', renderedSession = '';
  let pendingListTransition = null, navigationNoticeTimer = null;
  function presentedRoute() {
    if (!pendingListTransition) return route();
    const url = new URL(pendingListTransition.source, location.origin);
    return {path:url.pathname === '/' ? '/latest' : url.pathname, query:url.searchParams};
  }
  let extrasUpdatedAt = 0;

  const ICONS = {
    trophy:'<path d="M8 3h8v6a4 4 0 0 1-8 0V3Zm0 2H4v2a4 4 0 0 0 4 4m8-6h4v2a4 4 0 0 1-4 4m-4 2v5m-4 3h8m-6-3h4l2 3H8Z"/>',
    calendar: '<rect x="3" y="5" width="18" height="16" rx="2"/><path d="M16 3v4M8 3v4M3 11h18"/>',
    edit: '<path d="m16 3 5 5-12 12-6 1 1-6Z M14 5l5 5"/>',
    language: '<path d="m5 8 6 6m-7 0 6-6 2-5M2 5h12M7 2v3m7 17 4-10 4 10m-6.8-3h5.6"/>',
    globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a17 17 0 0 1 0 18 17 17 0 0 1 0-18Z"/>',
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
    like: '<path d="M7 21H4a2 2 0 0 1-2-2v-7a2 2 0 0 1 2-2h3m0 11V10l5-8a2 2 0 0 1 3 2l-1 5h6a2 2 0 0 1 2 2l-2 8a2 2 0 0 1-2 2Z"/>',
    eye: '<path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8S1 12 1 12Z"/><circle cx="12" cy="12" r="3"/>',
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

  const LOGO = '<img class="brand-logo" src="/metar-assets/brand-logo.jpg" width="36" height="36" alt="METAR">';
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
  const external = (path, label, className = '', attributes = '') => `<a href="${esc(path)}" class="${esc(className)}"${attributes}>${label}</a>`;
  const outbound = (url, label, className = '') => url ? `<a href="${esc(url)}" class="${esc(className)}" target="_blank" rel="noopener noreferrer">${label}</a>` : '';
  const badge = (label, className = '') => `<span class="badge ${className}">${label}</span>`;
  const pageTitle = title => `<h1 class="visually-hidden">${esc(title)}</h1>`;
  const empty = (title, description, action = '', icon = 'inbox') => `<div class="prod-empty">${I(icon)}<h2>${esc(title)}</h2><p>${esc(description)}</p>${action}</div>`;
  const loading = () => window.MetarLoading.render(document.getElementById('metar-loading-template').innerHTML, currentTitle(), route().path, t("正在读取社区实时数据…"), getLanguage() === 'en_US');
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
    // Answer uses the string "0" when no answer has been accepted.
    // Keep decimal IDs as strings: real IDs can exceed JavaScript's safe integer range.
    const acceptedID = question.accepted_answer_id;
    const hasAcceptedAnswer = typeof acceptedID === 'string'
      ? /^[1-9]\d*$/.test(acceptedID)
      : Number.isSafeInteger(acceptedID) && acceptedID > 0;
    // Older Answer lists omit user_info. Only an "asked" operator is
    // guaranteed to be the original author; never use an editor or responder.
    const author = question.user_info || (question.operation_type === 'asked' ? question.operator : null) || {};
    const tags = Array.isArray(question.tags) ? question.tags : [];
    const description = typeof question.description === 'string' ? question.description.replace(/\s+/g, ' ').trim() : '';
    const characters = Array.from(description);
    const excerpt = characters.length > 28 ? characters.slice(0, 28).join('') + '…' : description;
    const reply = question.last_reply;
    const replyCount = Math.max(0, Number(question.answer_count) || 0);
    const replySummary = replyCount === 0 ? esc(t('暂无回答 · 等你分享经验'))
      : reply?.user_info && Number(reply.created_at) > 0
        ? esc(t('最后回复：')) + publicAuthor(reply.user_info, esc(displayName(reply.user_info))) + ' · ' + esc(time(reply.created_at))
        : esc(t('已有回答'));
    return `<article class="discussion-row">
      ${publicAuthor(author, avatar(author), 'discussion-avatar')}
      <div class="discussion-main">
        ${external(questionHref(question.id || question.question_id), `<h3>${question.pin === 2 ? I('flag', 'sm') + `<span class="visually-hidden">${t("置顶")} </span>` : ''}${esc(question.title)}</h3>`, 'discussion-title')}
        ${excerpt ? `<p class="discussion-excerpt">${esc(excerpt)}</p>` : ''}
        <div class="discussion-meta">
          ${publicAuthor(author, `<span>${esc(displayName(author))}</span>`, 'discussion-author')}
          ${Number(question.created_at) > 0 ? `<span class="discussion-created">${esc(t('发布于'))} ${esc(time(question.created_at))}</span>` : ''}
          ${[...tags.filter(tag=>tag.recommend), ...tags.filter(tag=>!tag.recommend)].slice(0,1).map((tag) => link(`/topic/${encodeURIComponent(tag.slug_name || tagName(tag))}`, esc(tagName(tag)), tag.recommend ? 'badge taxonomy-category-badge' : 'badge')).join('')}
          ${hasAcceptedAnswer ? badge(I('check', 'sm') + t("已采纳"), 'green') : ''}

        </div>
      </div>
      ${external(questionHref(question.id || question.question_id), `<strong>${number(replyCount)}</strong><span>${t('回答')}</span>`, 'discussion-reply-count')}
      <div class="discussion-last-reply">${replySummary}</div>
    </article>`;
  }

  function discussionList(list) {
    return `<div class="discussion-list">${list.map(questionRow).join('')}</div>`;
  }

  function footer() {
    return `<footer class="prod-footer"><span>© 2026 ${esc(config.siteName)}</span><nav>${link('/tos', t("服务条款"))}${link('/privacy', t("隐私政策"))}${outbound(config.consoleUrl, t('元衡API'))}${external('/users/register', t("加入社区"))}${link('/guidelines', t("社区规范"))}</nav></footer>`;
  }

  function currentTitle() {
    const path = route().path;
    if (path.startsWith('/marketplace/item/')) return t('Vibe Coding 集市');
    if (path.startsWith('/model/')) return t('模型详情');
    if (path.startsWith('/question/')) return t("问题详情");
    if (path.startsWith('/topic/')) return t("话题");
    return ({ '/me/drafts': t('我的草稿'), '/events': t('近期活动'), '/admin/publishing': '内容运营', '/admin/operations': t('社区经营'), '/admin/events': t('活动管理'), '/chat': t('聊天'), '/admin/chat': t('聊天举报'), '/latest': t("最新话题"), '/topics': t("类别与标签"), '/admin/taxonomy': t('类别管理'), '/knowledge': t("知识库"), '/search': t("搜索"), '/models': t('模型评分榜'), '/models/leaderboard': t('模型评分榜'), '/admin/models': t('模型榜管理'), '/tools': t('工具'), '/tools/network': t('Claude 访问环境检测'), '/marketplace': t('Vibe Coding 集市'), '/marketplace/mine': t('我的集市发布'), '/marketplace/publish': t('发布到集市'), '/admin/marketplace': t('集市审核'), '/me/settings': t('账号设置'), '/me/badges': t('勋章'), '/me': t("个人空间"), '/me/bookmarks': t("我的收藏"), '/me/growth': t('社区成长'), '/me/notifications': t("通知中心"), '/settings/binding': t("账号绑定"), '/pulse': t("Pulse 权益"), '/admin/pulse': t("Pulse 配置"), '/publish': t("发布内容"), '/login': t("登录"), '/register': t("注册"), '/forgot': t("找回密码"), '/status': t("服务状态"), '/support': t("帮助中心"), '/guidelines': t("社区规范"), '/tos': t("服务条款"), '/privacy': t("隐私政策") })[path] || t("METAR 社区");
  }

  function languageControl() {
    return `<span class="icon-btn language-control" title="${t('界面语言')}">${I('language')}<select class="language-select" data-action="language" aria-label="${t('界面语言')}"><option value="zh_CN"${getLanguage() === 'zh_CN' ? ' selected' : ''}>中文</option><option value="en_US"${getLanguage() === 'en_US' ? ' selected' : ''}>English</option></select></span>`;
  }

  function accountManagement(item) {
    if (currentUserState !== 'ready' || !isCommunityAdministrator(currentUser)) return '';
    return `<div class="account-menu-group"><div class="account-menu-label">${t('管理')}</div>${item('/admin/pulse', t('Pulse 配置'), 'settings')}${item('/admin/growth', t('成长管理'), 'target')}${item('/admin/taxonomy', t('类别管理'), 'flag')}${item('/admin/marketplace', t('集市审核'), 'code')}${item('/admin/models', t('模型榜管理'), 'target')}${item('/admin/chat', t('聊天举报'), 'chat')}${item('/admin/events', t('活动管理'), 'calendar')}${item('/admin/operations', t('社区经营'), 'target')}${item('/admin/publishing', '内容运营', 'edit')}${external('/admin/dashboard', I('shield') + `<span>${t('社区管理')}</span>`, 'account-menu-link')}${external('/admin/pulse_user_center', I('link') + `<span>${t('社区连接配置')}</span>`, 'account-menu-link')}</div>`;
  }

  const isPersonalPath = path => /^\/me(?:\/|$)/.test(path) || ['/settings/binding','/chat','/pulse'].includes(path);
  let personalNativeView = null, personalSidebarKey = '', personalNativeStep = 0, personalLeaveApproved = false, restoringPersonalHistory = false;
  function personalSidebar() {
    if (!isPersonalPath(route().path) || currentUserState !== 'ready' || !currentUser) return '';
    const { path, query } = route();
    const item = (url, label, icon) => link(url, I(icon) + `<span>${esc(label)}</span>`, `account-menu-link ${routeMatchesNavigation(path, query.toString(), url) ? 'active' : ''}`);
    const consoleBase = config.consoleUrl?.replace(/\/$/, '').replace(/\/console$/, '');
    return `<aside class="personal-sidebar" aria-label="${t('我的空间')}"><div class="account-menu-group"><div class="account-menu-label">${t('我的空间')}</div>${item('/chat', t('聊天'), 'chat')}${item('/me', t('个人空间'), 'user')}${item('/me/growth', t('社区成长'), 'target')}${item('/me/bookmarks', t('我的收藏'), 'bookmark')}${item('/settings/binding', t('账号绑定'), 'link')}${item('/me/badges', t('勋章'), 'trophy')}${item('/me/settings', t('账号设置'), 'settings')}${item('/me/notifications', t('通知中心'), 'bell')}${item('/me/drafts', t('我的草稿'), 'edit')}</div>${accountManagement(item)}<div class="account-menu-group personal-console-group"><div class="account-menu-label">${t('元衡服务')}</div>${item('/pulse', getLanguage() === 'en_US' ? 'Check-in & draw' : '签到抽奖', 'target')}${consoleBase ? outbound(consoleBase + '/console', t('元衡API'), 'account-menu-link') : ''}</div><div class="account-menu-group personal-logout">${external('/users/logout', I('external') + `<span>${t('退出登录')}</span>`, 'account-menu-link')}</div></aside>`;
  }

  const isHomePath = (path = route().path) => /^\/(?:latest|questions|discover)?\/?$/.test(path);

  function syncPersonalNavigation() {
    const sidebar = personalSidebar();
    const existing = document.querySelector('.personal-sidebar');
    const key = `${currentUser?.id || ''}:${getLanguage()}:${isCommunityAdministrator(currentUser)}:${config.consoleUrl}`;
    if (!sidebar || key !== personalSidebarKey) existing?.remove();
    personalSidebarKey = key;
    document.documentElement.classList.toggle('has-personal-sidebar', !!sidebar);
    document.documentElement.classList.toggle('community-wide', isHomePath() || route().path.startsWith('/topic/'));
    if (sidebar && (!existing || key !== existing.dataset.personalKey)) {
      if (existing?.isConnected) existing.remove();
      document.getElementById('main')?.insertAdjacentHTML('beforebegin', sidebar);
      const mounted = document.querySelector('.personal-sidebar');
      if (mounted) mounted.dataset.personalKey = key;
    }
    const back = document.querySelector('[data-action="back"]');
    if (back) back.hidden = isHomePath();
  }

  function goBack() {
    if (personalNativeView && !personalNativeView.confirmLeave()) return;
    personalLeaveApproved = true;
    const path = route().path;
    const fallback = isPersonalPath(path) ? '/latest' : /^\/(?:admin(?:\/|$)|me\/|users\/settings(?:\/|$)|settings(?:\/|$)|badges(?:\/|$))/.test(path) ? '/me' : '/latest';
    if (window.__metarReturnNavigation?.back) { window.__metarReturnNavigation.back(fallback); return; }
    const destination = fallback;
    if (!window.MetarRouter.owns(new URL(destination, location.origin).pathname)) { location.href = destination; return; }
    router.go(destination);
    navigate();
  }

  function accountMenu() {
    return `<a href="${esc(window.MetarRouter.href('/me'))}" data-router class="account-avatar" aria-label="${t('个人空间')}" title="${t('个人空间')}">${avatar(currentUser)}</a>`;
  }

  function topbar() {
    const path = route().path;
    const logged = currentUserState === 'ready' && currentUser;
    const identityUnavailableState = currentUserState === 'unavailable';
    const activeUser = logged && isActiveUser(currentUser);
    const active = (prefix) => path === prefix || path.startsWith(`${prefix}/`);
    return `<header class="topbar">
      <button type="button" class="page-back" data-action="back" aria-label="${getLanguage() === 'en_US' ? 'Back to home' : '返回主界面'}"${isHomePath() ? ' hidden' : ''}>← ${getLanguage() === 'en_US' ? 'Back' : '返回'}</button>
      ${link('/latest', LOGO + `<span class="brand-word" aria-hidden="true">${esc(config.siteName.toLowerCase())}</span>`, 'brand')}
      <nav class="topnav" aria-label="${t("主导航")}">${link('/latest', t("社区"), active('/latest') || active('/question') || active('/topic') || active('/topics') ? 'active' : '')}${link('/marketplace', t('集市'), active('/marketplace') ? 'active' : '')}${link('/models', t('模型榜'), active('/models') || active('/model') ? 'active' : '')}${link('/pulse', 'Pulse', active('/pulse') ? 'active' : '')}${toolsMenu(active('/tools'))}</nav>
      <form class="searchbox" data-form="search" role="search">${I('search')}<input type="search" name="q" aria-label="${t("搜索社区")}" placeholder="${t("搜索真实问题与回答…")}" value="${path === '/search' ? esc(route().query.get('q') || '') : ''}" autocomplete="off"><kbd>⌘ K</kbd></form>
      <div class="header-actions">${activeUser ? external('/users', I(ICONS.barChart ? 'barChart' : 'target') + `<span class="visually-hidden">${t('排行榜')}</span>`, 'icon-btn header-ranking', ` aria-label="${t('排行榜')}" title="${t('排行榜')}"`) : ''}${languageControl()}<button type="button" class="icon-btn theme-btn" data-action="theme" aria-label="${document.documentElement.dataset.theme === 'dark' ? t("切换到浅色主题") : t("切换到深色主题")}">${I(document.documentElement.dataset.theme === 'dark' ? 'sun' : 'moon')}</button>${currentUserState === 'loading' ? `<span class="identity-loading" role="status">${t("正在确认身份…")}</span>` : identityUnavailableState ? link('/me', I('server', 'sm') + `<span>${t("身份服务暂不可用")}</span>`, 'btn ghost identity-status') : logged ? (activeUser ? link('/me/notifications', I('bell') + `<span class="visually-hidden">${t("通知中心")}</span><span class="account-unread-dot" data-account-unread hidden></span>`, 'icon-btn account-notifications') + link('/me/notifications?view=achievement', I('trophy') + `<span class="visually-hidden">${getLanguage() === 'en_US' ? 'Achievements' : '成就'}</span>`, 'icon-btn account-achievements') + (isCommunityAdministrator(currentUser) ? external('/review', I('shield') + `<span class="visually-hidden">${getLanguage() === 'en_US' ? 'Review' : '审查'}</span>`, 'icon-btn account-review') : '') + `<a href="${esc(window.MetarRouter.href('/chat'))}" data-router class="icon-btn header-chat" title="${t('聊天')}">${I('chat')}<span class="visually-hidden">${t('聊天')}</span><span class="chat-unread" data-chat-unread hidden></span></a>` : external('/users/login?status=inactive', t("激活账号"), 'btn primary')) + accountMenu() : external(config.answerLoginPath, t("登录"), 'btn ghost') + external(config.answerRegisterPath, t("加入社区"), 'btn primary guest-register')}</div>
    </header>`;
  }

  let taxonomyDrawer = null;
  let navigationCatalog = null, navigationCatalogScope = null;
  const taxonomyInlineMarkup = new WeakMap();
  function updateTaxonomyInline(node, html) {
    if (taxonomyInlineMarkup.get(node) === html) return;
    node.innerHTML = html;
    taxonomyInlineMarkup.set(node, html);
  }
  const taxonomyCollapsed = Object.fromEntries([...document.querySelectorAll('.initial-chrome [data-taxonomy-group]')].map(node => [node.dataset.taxonomyGroup, !node.open]));
  document.addEventListener('toggle', event => {
    const node = event.target;
    if (node.matches?.('[data-taxonomy-group]') && node.isConnected) taxonomyCollapsed[node.dataset.taxonomyGroup] = !node.open;
  }, true);

  function toolsMenu(isActive) {
    const items = TOOLS.map(tool => link(tool.path, esc(getLanguage() === 'en_US' ? tool.en : tool.zh), 'topnav-menu-link')).join('');
    return `<details class="topnav-menu${isActive ? ' active' : ''}"><summary>${t('工具')}</summary><div class="topnav-menu-panel" role="group" aria-label="${t('工具')}">${link('/tools', t('全部工具'), 'topnav-menu-link')}${items}</div></details>`;
  }

  function mobileBottom() {
    const { path, query } = route();
    return `<nav class="mobile-bottom" aria-label="${t("移动端主导航")}">${[['/latest', t("话题"), 'chat'], ['/topics', t("类别"), 'flag'], ['/pulse', 'Pulse', 'pulse'], ['/me', t("我的"), 'user']].map(([url, label, icon]) => link(url, I(icon) + label, routeMatchesNavigation(path, query.toString(), url) ? 'active' : '')).join('')}</nav>`;
  }

  function syncNavigation() {
    syncPersonalNavigation();
    window.MetarSidebar?.sync();
    const {path, query} = presentedRoute();
    for (const node of document.querySelectorAll('.sidebar a[data-router], .personal-sidebar a[data-router], .mobile-bottom a[data-router], .account-menu a[data-router], .topnav a[data-router]')) {
      const href = node.getAttribute('href');
      const active = node.dataset.taxonomyDirectory
        ? path === '/topics' && (query.get('view') === 'tags' ? 'tags' : 'categories') === node.dataset.taxonomyDirectory
        : node.closest('.topnav') && href === '/latest'
        ? /^\/(?:latest|topics?|question)(?:\/|$)/.test(path)
        : routeMatchesNavigation(path, query.toString(), href);
      node.classList.toggle('active', active);
      if (active) node.setAttribute('aria-current', 'page');
      else node.removeAttribute('aria-current');
    }
    const tag = path.startsWith('/topic/') ? decodeURIComponent(path.slice(7)) : '';
    const selectedKind = path === '/topics' ? (query.get('view') === 'tags' ? 'tags' : 'categories') : tag ? (navigationCatalog?.categories.some(item=>item.slug_name===tag) ? 'categories' : 'tags') : '';
    for (const node of document.querySelectorAll('[data-action="taxonomy-open"]')) node.classList.toggle('active',node.dataset.kind===selectedKind);
    if (path === '/search') {
      const input = document.querySelector('.searchbox input');
      if (input) input.value = query.get('q') || '';
    }
  }

  function refreshChrome() {
    if (!shellReady) return;
    const input = document.querySelector('.searchbox input');
    const draft = input?.value, focused = document.activeElement === input;
    const header = document.querySelector('header.topbar');
    if (header) header.outerHTML = topbar();
    const next = document.querySelector('.searchbox input');
    if (next && draft !== undefined) { next.value = draft; if (focused) next.focus({preventScroll:true}); }
    extrasUpdatedAt = 0;
    refreshAccountExtras();
    refreshChatUnread();
    syncNavigation();
    enhanceTaxonomy();
  }

  function renderShell({retain = false} = {}) {
    syncDocument();
    if (!shellReady || shellLanguage !== getLanguage()) {
      app.innerHTML = `${topbar()}<main class="page" id="main" tabindex="-1"><div class="page-inner" id="view">${loading()}</div></main>${mobileBottom()}`;
      shellReady = true; shellLanguage = getLanguage();
      extrasUpdatedAt = 0;
      refreshAccountExtras();
    } else if (!retain) {
      const view = document.getElementById('view');
      if (view) view.innerHTML = loading();
    }
    syncNavigation();
    app.setAttribute('aria-busy', 'true');
    if (!pendingListTransition) {
      document.title = `${currentTitle()} · ${config.siteName}`;
      window.MetarSEO?.update();
    }
  }

  let footerResizeObserver = null;
  function fitFooter() {
    footerResizeObserver?.disconnect();
    const footer = document.querySelector('.prod-footer');
    const update = () => {
      const flowing = document.documentElement.classList.contains('community-wide');
      const height = footer?.isConnected && !flowing ? Math.ceil(footer.getBoundingClientRect().height) : 0;
      document.documentElement.style.setProperty('--community-footer-height', height + 'px');
    };
    update();
    if (footer && typeof window.ResizeObserver === 'function') {
      footerResizeObserver = new window.ResizeObserver(update);
      footerResizeObserver.observe(footer);
    }
  }

  function renderView(html, {focus = true} = {}) {
    const view = document.getElementById('view');
    if (view) view.innerHTML = html;
    fitFooter();
    document.title = `${currentTitle()} · ${config.siteName}`;
    syncNavigation();
    enhanceTaxonomy();
    void mountDiscussionRail();
    void mountPersonalNative();
    if (['/me/drafts', '/events', '/admin/events'].includes(route().path)) memberView?.mount(view);
    if (route().path === '/admin/operations') operationsView?.mount(view);
    if (route().path.startsWith('/marketplace') || route().path === '/admin/marketplace') marketplaceView?.mount(view);
    if (route().path === '/admin/models') modelsAdminView?.mount(view);
    if (route().path === '/admin/publishing') publishingView?.mount(view);
    if (route().path === '/tools' || route().path.startsWith('/tools/')) toolsView?.mount(view);
    const chatRoot = document.querySelector('[data-chat-root]');
    if (chatRoot && chatView) chatView.mount(chatRoot, route().query.get('room') || '');
    const core = document.querySelector('[data-pulse-core]');
    if (core && window.MetarPulseCore && pulseCoreState) {
      pulseView = new window.MetarPulseCore.View(core, { t });
      pulseView.update(pulseCoreState);
    }
    void mountPulseFeed();
    void mountCommunityTasks();
    schedulePulseStatus();
    app.setAttribute('aria-busy', 'false');
    window.MetarSEO?.update();
    const selectedTab = document.querySelector('.discussion-tab.active');
    if (selectedTab) {
      const tabs = selectedTab.parentElement;
      const selected = selectedTab.getBoundingClientRect(), bounds = tabs.getBoundingClientRect();
      if (selected.right > bounds.right) tabs.scrollLeft += selected.right - bounds.right;
      else if (selected.left < bounds.left) tabs.scrollLeft -= bounds.left - selected.left;
    }
    if (focus && !document.activeElement?.matches?.('input, textarea, select, [contenteditable="true"]')) {
      document.getElementById('main')?.focus({ preventScroll: true });
    }
    // Public author badges enhance an already usable page, outside its critical path.
    if (!window.MetarGrowthPresentation && document.querySelector('#view a[href^="/users/"]')) {
      const sequence = navigationSequence;
      const enhance = () => {
        if (sequence === navigationSequence) loadFeatureScript('growth-presentation', 'MetarGrowthPresentation').catch(() => {});
      };
      if (window.requestIdleCallback) window.requestIdleCallback(enhance, {timeout:1500});
      else setTimeout(enhance, 200);
    }
  }

  function renderError(error) {
    const inactive = error instanceof AdapterError && error.code === 'inactive';
    const message = inactive ? t("当前社区账号尚未激活，请先完成邮箱验证。") : errorMessage(error) || t("页面暂时无法加载。");
    renderView(`<section class="card prod-error">${I(inactive ? 'shield' : 'server')}<h2>${inactive ? t("账号尚未激活") : t("暂时没有读到社区数据")}</h2><p>${esc(message)}${t(" 社区数据不会由前端猜测或使用缓存数字替代。")}</p><div class="flex wrap prod-center-actions"><button type="button" class="btn primary" data-action="retry">${I('refresh')}${t("重新加载")}</button>${inactive ? external('/users/login?status=inactive', t("重新发送激活邮件"), 'btn') : external('/latest', t("浏览社区话题"), 'btn')}</div></section>${footer()}`);
    window.MetarSEO?.update(true);
  }

  const questionOrders = () => ({ active: t("最近活跃"), newest: t("最新发布"), hot: t("热门"), score: t("高赞"), unanswered: t("待回答") });

  function enhanceTaxonomy() {
    const node = document.querySelector('[data-taxonomy-filter]');
    const navigation = document.querySelector('[data-taxonomy-navigation]');
    if (!node && !navigation) return;
    const sequence = navigationSequence, key = taxonomy.scope(answer), {path} = presentedRoute();
    const tag = path.startsWith('/topic/') ? decodeURIComponent(path.slice(7)) : '';
    taxonomy.catalog(answer).then(data => {
      if (key !== taxonomy.scope(answer)) return;
      navigationCatalog = data;
      navigationCatalogScope = key;
      if (navigation?.isConnected) {
        const currentPath = presentedRoute().path;
        const current = currentPath.startsWith('/marketplace') ? currentPath : currentPath.startsWith('/topic/') ? decodeURIComponent(currentPath.slice(7)) : '';
        for (const area of navigation.querySelectorAll('[data-taxonomy-inline]')) {
          const html = taxonomy.inlineItems(data, area.dataset.taxonomyInline, current);
          updateTaxonomyInline(area, html);
        }
      }
      syncNavigation();
      if (node?.isConnected && sequence === navigationSequence) node.innerHTML = taxonomy.filters(data, tag);
    });
  }

  function closeTaxonomy(restore = true) {
    if (!taxonomyDrawer) return;
    taxonomyDrawer.restore = restore;
    taxonomyDrawer.node.close();
  }

  function openTaxonomy(kind, trigger) {
    closeTaxonomy(false);
    if (kind !== 'categories' && kind !== 'tags') return;
    const node = document.createElement('dialog');
    node.id = 'taxonomy-drawer'; node.className = 'taxonomy-drawer';
    node.setAttribute('aria-labelledby','taxonomy-drawer-title');
    node.innerHTML = taxonomy.drawer(kind);
    document.body.append(node);
    const state = {node,trigger,kind,restore:true,data:null,key:taxonomy.scope(answer)};
    taxonomyDrawer = state;
    trigger.setAttribute('aria-expanded','true');
    node.addEventListener('close', () => {
      if (taxonomyDrawer === state) taxonomyDrawer = null;
      if (taxonomyDrawer?.trigger !== trigger) trigger.setAttribute('aria-expanded','false');
      node.remove();
      if (state.restore && !taxonomyDrawer && trigger.isConnected) trigger.focus({preventScroll:true});
    });
    node.addEventListener('click', event => {
      if (event.target !== node) return;
      const rect = node.getBoundingClientRect();
      if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) closeTaxonomy();
    });
    const input = node.querySelector('[data-taxonomy-search]');
    const render = () => {
      if (taxonomyDrawer !== state || !node.open || state.key !== taxonomy.scope(answer) || !state.data) return;
      const path = presentedRoute().path;
      node.querySelector('[data-taxonomy-options]').innerHTML = taxonomy.drawerItems(state.data,kind,input.value,path.startsWith('/topic/')?decodeURIComponent(path.slice(7)):'');
    };
    input.addEventListener('input',render);
    node.showModal();
    taxonomy.catalog(answer).then(data => {
      if (taxonomyDrawer !== state || !node.open || state.key !== taxonomy.scope(answer)) return;
      state.data = data;
      navigationCatalog = data;
      navigationCatalogScope = state.key;
      node.querySelector('[data-taxonomy-footer]').innerHTML = taxonomy.drawerFooter(data,kind,config.answerAskPath,getLanguage()==='en_US');
      render();
    });
  }

  let tagDirectory = null, tagDirectoryScope = '', tagPanelOpen = false, tagFollowBusy = false, tagFollowError = '';

  async function loadTagDirectory() {
    const scope = taxonomy.scope(answer);
    if (tagDirectory && tagDirectoryScope === scope) return tagDirectory;
    const [catalogResult, followingResult] = await Promise.allSettled([
      taxonomy.catalog(answer),
      currentUser ? answer.request('/tags/following') : Promise.resolve([]),
    ]);
    const catalog = catalogResult.status === 'fulfilled' ? catalogResult.value : {};
    const following = followingResult.status === 'fulfilled' && Array.isArray(followingResult.value) ? followingResult.value : [];
    tagDirectory = {
      categories: catalog.categories || [],
      tags: catalog.tags || [],
      followed: new Set(following.map(tag => tag?.slug_name).filter(Boolean)),
    };
    tagDirectoryScope = scope;
    return tagDirectory;
  }

  function tagRow(tag) {
    const followed = tagDirectory.followed.has(tag.slug_name);
    return `<div class="tag-row">${link(taxonomy.topicURL(tag.slug_name), esc(tagName(tag)), 'tag-row-name')}<button type="button" class="tag-follow${followed ? ' active' : ''}" data-tag-follow="${esc(tag.slug_name)}" aria-pressed="${followed}">${followed ? t('取消关注') : t('关注')}</button></div>`;
  }

  function tagGroupsMarkup(filter = '') {
    if (!tagDirectory) return `<p class="muted">${t('正在加载…')}</p>`;
    const needle = filter.trim().toLocaleLowerCase();
    const matches = list => list.filter(tag => !needle || `${tagName(tag)} ${tag.slug_name}`.toLocaleLowerCase().includes(needle));
    const groups = [['类别', matches(tagDirectory.categories)], ['标签', matches(tagDirectory.tags)]].filter(([, list]) => list.length);
    if (!groups.length) return `<p class="muted">${t('没有匹配的标签')}</p>`;
    return groups.map(([label, list]) => `<section class="tag-group"><h3>${t(label)}</h3>${list.map(tagRow).join('')}</section>`).join('');
  }

  function tagPanelMarkup() {
    return `<div class="tag-panel-head"><strong>${t('标签')}</strong><button type="button" class="icon-btn" data-action="toggle-tags" aria-label="${t('关闭标签面板')}">${I('close')}</button></div><label class="tag-panel-search">${I('search')}<input type="search" data-tag-filter placeholder="${t('输入名称筛选…')}" aria-label="${t('筛选标签')}" autocomplete="off"></label><div data-tag-groups>${tagGroupsMarkup()}</div>${tagFollowError ? `<p class="tag-panel-status" role="alert">${esc(tagFollowError)}</p>` : ''}`;
  }

  function followedRailMarkup() {
    if (!currentUser) return `<p class="rail-empty">${external(config.answerLoginPath, t('登录后关注标签'), 'textlink')}</p>`;
    if (!tagDirectory) return `<p class="rail-empty">${t('正在加载…')}</p>`;
    if (!tagDirectory.followed.size) return `<p class="rail-empty">${t('暂未关注标签')}</p>`;
    const names = new Map([...tagDirectory.categories, ...tagDirectory.tags].map(tag => [tag.slug_name, tagName(tag)]));
    return `<div class="rail-tags">${[...tagDirectory.followed].map(slug => link(taxonomy.topicURL(slug), esc(names.get(slug) || slug), 'rail-tag')).join('')}</div>`;
  }

  function toggleTagPanel(open = !tagPanelOpen) {
    tagPanelOpen = open;
    const panel = document.querySelector('[data-tag-panel]');
    if (panel) { panel.hidden = !open; if (open) panel.innerHTML = tagPanelMarkup(); }
    document.querySelectorAll('[data-action="toggle-tags"]').forEach(button => {
      if (button.closest('[data-tag-panel]')) return;
      button.textContent = open ? t('完成') : t('编辑');
      button.setAttribute('aria-expanded', String(open));
    });
  }

  function refreshTagPanel() {
    document.querySelectorAll('[data-rail-tags]').forEach(node => { node.innerHTML = followedRailMarkup(); });
    const followed = document.querySelector('[data-followed-card]');
    if (followed) followed.hidden = !(currentUser && tagDirectory?.followed?.size);
    const panel = document.querySelector('[data-tag-panel]');
    if (panel && tagPanelOpen) panel.innerHTML = tagPanelMarkup();
  }

  async function toggleTagFollow(button) {
    const slug = button.dataset.tagFollow;
    if (!slug || !tagDirectory || tagFollowBusy) return;
    if (!currentUser) { location.assign(config.answerLoginPath); return; }
    const next = new Set(tagDirectory.followed);
    if (next.has(slug)) next.delete(slug); else next.add(slug);
    tagFollowBusy = true;
    button.disabled = true;
    try {
      await answer.request('/follow/tags', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ slug_name_list: [...next] }) });
      tagDirectory.followed = next;
      tagFollowError = '';
    } catch (error) {
      tagFollowError = errorMessage(error);
    } finally {
      tagFollowBusy = false;
      refreshTagPanel();
    }
  }

  function discussionRailItems(result, unanswered = false) {
    const rows = (Array.isArray(result?.list) ? result.list : []).filter(item =>
      item && (item.id || item.question_id) && typeof item.title === 'string' &&
      (!unanswered || Number(item.answer_count) === 0)).slice(0, unanswered ? 3 : 5);
    return rows.length ? '<ul class="rail-post-list">' + rows.map(item =>
      '<li>' + external(questionHref(item.id || item.question_id), esc(item.title)) + '</li>').join('') + '</ul>'
      : '<p class="rail-content-state">' + esc(t(unanswered ? '暂时没有待回答问题' : '暂无精选内容')) + '</p>';
  }

  let railMediaCleanup = null;
  async function mountDiscussionRail() {
    railMediaCleanup?.();
    railMediaCleanup = null;
    const root = document.querySelector('[data-discussion-rail]');
    if (!root) return;
    const disclosure = root.querySelector('[data-rail-disclosure]');
    if (disclosure && window.matchMedia) {
      const media = window.matchMedia('(max-width:900px)');
      const sync = () => { disclosure.open = !media.matches; };
      sync(); media.addEventListener?.('change',sync);
      railMediaCleanup = () => media.removeEventListener?.('change',sync);
    }
    const sequence = navigationSequence, scope = taxonomy.scope(answer);
    await Promise.all(['unanswered', 'featured'].map(async kind => {
      const node = root.querySelector('[data-rail-content="' + kind + '"]');
      if (!node) return;
      const current = () => root.isConnected && sequence === navigationSequence && scope === taxonomy.scope(answer);
      try {
        const result = await answer.listQuestions(kind === 'unanswered'
          ? {page:1, pageSize:3, order:'unanswered'}
          : {page:1, pageSize:5, order:'score', tag:'feeds'});
        if (current()) node.innerHTML = discussionRailItems(result, kind === 'unanswered');
      } catch (_) {
        if (current()) node.innerHTML = '<p class="rail-content-state">' + esc(t('内容暂时无法加载')) + '</p>';
      }
    }));
  }

  async function questionsPage(tag = '') {
    await loadTagDirectory();
    const { query } = route();
    const requested = query.get('order') || 'active';
    const orders = questionOrders();
    const order = Object.prototype.hasOwnProperty.call(orders, requested) ? requested : 'active';
    const page = Math.max(1, Number.parseInt(query.get('page') || '1', 10) || 1);
    const result = await answer.listQuestions({ page, pageSize: 20, order, tag });
    const catalog = navigationCatalogScope === taxonomy.scope(answer) ? navigationCatalog : null;
    const filter = `<div class="taxonomy-filter" data-taxonomy-filter>${catalog ? taxonomy.filters(catalog, tag) : link('/topics', t('类别'), 'btn small') + link('/topics?view=tags', t('标签'), 'btn small') + (tag ? `<span class="badge prod-current-tag">${esc(tag)}</span>` : '')}</div>`;
    const list = Array.isArray(result?.list) ? result.list : [];
    const total = Number(result?.count) || 0;
    const base = tag ? `/topic/${encodeURIComponent(tag)}` : '/latest';
    const title = tag || t("最新话题");
    const askPath = tag ? taxonomy.askURL(config.answerAskPath, tag) : config.answerAskPath;
    const tab = value => link(`${base}?order=${value}`, `<span${order === value ? ' aria-current="page"' : ''}>${esc(orders[value])}</span>`, `discussion-tab ${order === value ? 'active' : ''}`);
    const tabs = ['active','newest','unanswered'].map(tab).join('') + `<details class="discussion-more"><summary>${esc(['hot','score'].includes(order) ? orders[order] : t('更多'))}</summary><div>${['hot','score'].map(tab).join('')}${link('/topics?view=tags',t('标签'))}</div></details>`;
    const totalPages = Math.max(1, Math.ceil(total / 20));
    const pager = totalPages > 1 ? `<div class="prod-pager">${page > 1 ? link(`${base}?order=${order}&page=${page - 1}`, t("上一页"), 'btn small') : ''}<span>${t("第 {page} / {total} 页", { page: number(page), total: number(totalPages) })}</span>${page < totalPages ? link(`${base}?order=${order}&page=${page + 1}`, t("下一页"), 'btn small') : ''}</div>` : '';
    return `<section class="community-discussions">
      ${pageTitle(title)}
      <div class="community-layout">
        <div class="community-feed">
          <div class="discussion-toolbar">${filter}<nav class="discussion-tabs" aria-label="${t('话题筛选')}">${tabs}</nav>${external(askPath, I('plus', 'sm') + t('新建话题'), 'btn primary')}</div>
          ${list.length ? discussionList(list) : empty(order === 'unanswered' ? t("暂时没有待回答问题") : t("当前筛选没有内容"), t("可以调整筛选，或发起一个新问题。"), external(askPath, t("发起提问"), 'btn primary'), 'chat')}${pager}
        </div>
        <aside class="community-rail" data-discussion-rail aria-label="${t('社区侧栏')}"><details data-rail-disclosure open><summary class="rail-mobile-summary">${t('社区推荐')}</summary><div class="rail-disclosure-content">
          <section class="rail-card"><div class="rail-head"><h2>${t('等你来回答')}</h2></div><div data-rail-content="unanswered" aria-live="polite"><p class="rail-content-state">${t('正在加载…')}</p></div></section>
          <details class="rail-card rail-curated"><summary>${t('精选阅读')}</summary><div data-rail-content="featured" aria-live="polite"><p class="rail-content-state">${t('正在加载…')}</p></div></details>
          <section class="rail-card" data-followed-card${currentUser && tagDirectory?.followed?.size ? '' : ' hidden'}>
            <div class="rail-head"><h2>${t('已关注的标签')}</h2><button type="button" class="rail-edit" data-action="toggle-tags" aria-expanded="${tagPanelOpen}" aria-controls="tag-panel">${t(tagPanelOpen ? '完成' : '编辑')}</button></div>
            <div data-rail-tags>${followedRailMarkup()}</div>
          </section>
          <p class="rail-guidelines">${link('/guidelines', t('《社区准则》'))} · ${link('/topics?view=tags',t('标签'))}</p>
          <div class="tag-panel" id="tag-panel" data-tag-panel${tagPanelOpen ? '' : ' hidden'}>${tagPanelMarkup()}</div>
        </div></details></aside>
      </div>
    </section>${footer()}`;
  }

  function personalNativePage() {
    const title = currentTitle();
    if (currentUserState === 'unavailable') return identityUnavailable(title);
    if (!currentUser) return loginRequired(title, t('请先登录社区账号。'));
    if (!isActiveUser(currentUser)) return accountUnavailable(title);
    return pageTitle(title) + '<section class="personal-native" data-personal-native><p data-native-status role="status">' + t('正在加载…') + '</p><iframe title="' + esc(title) + '" hidden></iframe><p class="prod-page-note"><a data-native-original class="textlink" target="_blank" rel="noopener">' + t('在独立页面打开') + '</a></p></section>' + footer();
  }
  async function mountPersonalNative() {
    const root = document.querySelector('[data-personal-native]');
    if (!root) return;
    const sequence = navigationSequence;
    try {
      await loadFeatureScript('personal-workspace','MetarPersonalWorkspace');
      if (sequence !== navigationSequence || !root.isConnected) return;
      const target = window.MetarPersonalWorkspace.nativeTarget(route().path,route().query);
      if (!target) throw new Error('invalid personal route');
      root.querySelector('[data-native-original]').href = target;
      personalNativeStep = history.state?.metarReturnV2?.step || 0;
      personalNativeView = new window.MetarPersonalWorkspace.View(root, {
        target, t,
        onRoute: (path, replace = false) => {
          if (sequence !== navigationSequence || path === location.pathname + location.search) return;
          const details = route().path === '/me/badges';
          if (replace || !details) history.replaceState({...history.state,metarScroll:null},'',path);
          else router.go(path);
          personalNativeStep = history.state?.metarReturnV2?.step || 0;
          renderedKey = location.pathname + location.search;
          document.title = currentTitle() + ' · ' + config.siteName;
          syncNavigation();
        },
        onNavigate: path => { if (sequence === navigationSequence && router.go(path)) navigate(); },
        onExit: value => {
          const next = new URL(value,location.origin);
          if (!['http:','https:'].includes(next.protocol)) return;
          if (next.origin === location.origin && router.go(next.pathname+next.search)) navigate();
          else location.assign(next.href);
        },
        onSaved: () => { answer.getCurrentUser().then(user=>{if(sequence===navigationSequence&&user?.id===currentUser?.id){currentUser=user;refreshChrome();}}).catch(()=>{}); },
      });
    } catch (_) {
      if (root.isConnected) root.querySelector('[data-native-status]').textContent = t('页面暂时无法加载。');
    }
  }

  async function topicsPage() {
    return `${pageTitle(t('类别与标签'))}${await taxonomy.directory(answer, route().query, config.answerAskPath, getLanguage() === 'en_US')}${footer()}`;
  }

  async function searchPage() {
    const query = (route().query.get('q') || '').trim();
    const form = `<form class="prod-search-form" data-form="search"><input type="search" name="q" value="${esc(query)}" maxlength="60" required placeholder="${t("输入问题、模型或接入关键词")}"><button type="submit" class="btn primary">${I('search')}${t("搜索")}</button></form>`;
    if (!query) return `${pageTitle(t("搜索社区"))}${form}<section class="card">${empty(t("输入一个关键词开始搜索"), t("例如：API、Agent、模型评测、错误处理。"), '', 'search')}</section>${footer()}`;
    const result = await answer.search(query);
    const list = Array.isArray(result?.list) ? result.list : [];
    return `${pageTitle(t("“{query}” 的结果", { query }))}${form}<p class="prod-search-count muted">${esc(t("找到 {count} 条社区内容。", { count: number(result?.count) }))}</p><section class="card">${list.length ? list.map((item) => { const object = item.object || {}; return `<article class="result-row">${badge(item.object_type === 'answer' ? t("回答") : t("问题"), item.object_type === 'answer' ? '' : 'green')}${external(searchHref(item), `<h3>${esc(object.title || t("社区内容"))}</h3>`)}<p>${esc(object.excerpt || t("该结果暂未提供摘要。"))}</p><small class="muted">${publicAuthor(object.user_info, esc(displayName(object.user_info)))} · ${time(object.created_at)}</small></article>`; }).join('') : empty(t("没有找到相关内容"), t("换一个更具体的关键词，或向社区发起新问题。"), external(config.answerAskPath, t("发起提问"), 'btn primary'), 'search')}</section>${footer()}`;
  }

  function knowledgePage() {
    const articles = knowledge.listArticles();
    return `${pageTitle(t("知识库"))}<div class="prod-knowledge-grid">${articles.map((article) => `<a class="card prod-knowledge-card" href="${esc(article.href)}"><div>${badge(esc(article.category), 'green')}<h2 class="mt16">${esc(article.title)}</h2><p>${esc(article.description)}</p></div><span class="textlink">${t("阅读文章 ")}${I('arrow', 'sm')}</span></a>`).join('')}</div><section class="card card-pad mt24"><div class="between wrap"><div><h3>${t("找不到需要的接入说明？")}</h3><p class="muted mt8">${t("可以在社区发起提问，与其他成员一起讨论。")}</p></div><div class="flex wrap">${external(config.blogBasePath, t("打开完整知识库 ") + I('external', 'sm'), 'btn primary')}${external(config.answerAskPath, t("向社区提问"), 'btn')}</div></div></section>${footer()}`;
  }

  function loginRequired(title, description) {
    return `<section class="card prod-login-card">${I('shield', 'lg')}<h1 class="mt16">${esc(title)}</h1><p>${esc(description)}${t(" 社区账号可独立注册，不要求先绑定元衡 API 账号。")}</p><div class="flex wrap">${external(config.answerLoginPath, t("登录社区账号"), 'btn primary')}${external(config.answerRegisterPath, t("独立注册"), 'btn')}${link('/latest', t("先浏览社区"), 'btn ghost')}</div></section>${footer()}`;
  }

  function identityUnavailable(title) {
    const reason = errorMessage(identityError) || t("暂时无法确认当前社区登录状态。");
    return `<section class="card prod-login-card">${I('server', 'lg')}<h1 class="mt16">${t("身份服务暂不可用")}</h1><p>${esc(reason)}${t(" 这不代表账号已退出，请勿反复登录；仍可继续尝试浏览社区公开内容。")}</p><div class="flex wrap"><button type="button" class="btn primary" data-action="retry-identity">${I('refresh')}${t("重新确认身份")}</button>${link('/latest', t("继续浏览社区"), 'btn ghost')}</div></section>${footer()}`;
  }

  function accountUnavailable(title) {
    const inactive = Number(currentUser?.mail_status) === 2 || currentUser?.status === 'inactive';
    const headingText = inactive ? t("社区账号尚未激活") : t("当前社区账号不可参与操作");
    const description = inactive ? t("请先验证邮箱。可以在登录页面重新发送激活邮件。") : t("请在账号页面查看当前状态，或联系社区管理员。");
    const action = inactive ? external('/users/login?status=inactive', t("重新发送激活邮件"), 'btn primary') : external(config.answerSettingsPath, t("查看账号状态"), 'btn primary');
    return `<section class="card prod-login-card">${I('shield', 'lg')}<h1 class="mt16">${headingText}</h1><p>${description}</p><div class="flex wrap">${action}${link('/latest', t("继续浏览社区"), 'btn ghost')}</div></section>${footer()}`;
  }

  async function profilePage() {
    if (currentUserState === 'unavailable') return identityUnavailable(t("个人空间"));
    if (!currentUser) return loginRequired(t("个人空间"), t("登录后查看个人资料与发布记录。"));
    const username = currentUser.username;
    const [profile, questions, experience] = await Promise.all([answer.getProfile(username), answer.listPersonalQuestions(username), isActiveUser(currentUser) ? growthView().then(view => view.compact()).catch(() => '') : Promise.resolve('')]);
    const list = Array.isArray(questions?.list) ? questions.list : [];
    return `<div class="content-grid"><div class="stack"><section class="card prod-user-card"><div class="between wrap">${avatar(profile, 'large')}<div class="flex wrap">${external(profileHref(username), t("公开主页"), 'btn')}</div></div><h1 class="mt16">${esc(displayName(profile))}</h1><p class="muted mt8">@${esc(profile.username || username)}${profile.location ? ` · ${esc(profile.location)}` : ''}</p><p class="mt16">${esc(profile.bio || t("这位成员暂未填写个人简介。"))}</p><div class="profile-numbers"><div><strong>${number(questions?.count)}</strong><span>${t("发布问题")}</span></div><div><strong>${number(profile.answer_count)}</strong><span>${t("参与回答")}</span></div><div><strong>${number(profile.rank)}</strong><span>${t("社区声望")}</span></div></div>${currentUser.mail_status === 2 ? `<div class="prod-status error mt16">${I('shield')}<div><strong>${t("邮箱尚未激活")}</strong><p>${t("请先验证邮箱，或在登录页面重新发送激活邮件。")}</p></div></div>` : ''}</section>${experience}<section class="card prod-feed"><div class="section-heading"><h2>${t("最近发布")}</h2><span class="muted">${t("发布记录")}</span></div>${list.length ? list.map((item) => questionRow({ ...item, user_info: profile })).join('') : empty(t("还没有发布问题"), t("从一个具体、可复现的问题开始。"), external(config.answerAskPath, t("发起问题"), 'btn primary'), 'chat')}</section></div><aside class="stack"><section class="card card-pad"><h3>${t("账号快捷入口")}</h3><ul class="mini-list"><li>${link('/me/bookmarks', `<span>${t("我的收藏")}</span>`)}</li><li>${external('/users/notifications/inbox', `<span>${t("通知中心")}</span>`)}</li><li>${link('/settings/binding', `<span>${t("元衡账号绑定")}</span><small>${t("可选")}</small>`)}</li><li>${link('/pulse', `<span>${t("Pulse 权益")}</span><small>${t("绑定后")}</small>`)}</li><li>${external('/users/logout', `<span>${t("退出登录")}</span>`)}</li></ul></section></aside></div>${footer()}`;
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
    return `${pageTitle(t("我的收藏"))}<section class="card prod-feed">${list.length ? list.map(questionRow).join('') : empty(t("还没有收藏内容"), t("在讨论页收藏感兴趣的话题，方便以后继续阅读。"), link('/latest', t("浏览问题"), 'btn primary'), 'bookmark')}</section>${pager}${footer()}`;
  }

  async function bindingPage() {
    if (currentUserState === 'unavailable') return identityUnavailable(t("账号绑定"));
    if (!currentUser) return loginRequired(t("账号绑定"), t("先登录独立社区账号，再自主选择是否连接元衡 API 身份。"));
    if (!isActiveUser(currentUser)) return accountUnavailable(t("账号绑定"));
    const state = await answer.getBindingState();
    if (state.status === 'unavailable') return `${pageTitle(t("账号绑定"))}<section class="card card-pad"><div class="prod-status error">${I('server')}<div><strong>${t("绑定服务暂不可用")}</strong><p>${t("服务器没有返回 Pulse UserCenter Connector。社区浏览、登录、发帖和回答仍然可用。")}</p></div></div></section>${footer()}`;
    const connectorPath = safeSameOriginPath(state.connector?.link, config.answerBindingPath);
    const bound = state.status === 'bound';
    return `${pageTitle(t("连接元衡 API 账号"))}<section class="card card-pad"><div class="between wrap"><h2>${t("元衡 API 身份")}</h2>${badge(bound ? I('check', 'sm') + t(" 已绑定") : t("未绑定"), bound ? 'green' : '')}</div>${bound ? `<div class="prod-binding-account"><div class="topic-icon">${I('link')}</div><div><strong>${t("已通过可信回调完成绑定")}</strong><p class="muted">${t("服务端已确认一对一关系；页面不会展示外部用户 ID 或任何 API 凭据。")}</p></div>${badge(t("受保护关系"), 'green')}</div><dl class="info-pairs"><dt>${t("社区账号")}</dt><dd>${esc(displayName(currentUser))}</dd><dt>${t("社区身份事实源")}</dt><dd>Apache Answer</dd><dt>${t("API / 资金身份事实源")}</dt><dd>new-api</dd><dt>${t("普通解绑或换绑")}</dt><dd>${t("不开放；纠错需要支持流程与审计")}</dd></dl><div class="flex wrap mt24">${link('/pulse', t("查看 Pulse 状态 ") + I('arrow', 'sm'), 'btn primary')}${link('/support', t("联系支持"), 'btn')}</div>` : `<div class="prod-binding-steps"><div class="prod-binding-step"><span class="number">1</span><strong>${t("确认社区身份")}</strong><p>${t("当前登录：")}${esc(displayName(currentUser))}</p></div><div class="prod-binding-step"><span class="number">2</span><strong>${t("前往元衡授权")}</strong><p>${t("由 Connector、浏览器 flow 和固定 callback 校验 API 身份。")}</p></div><div class="prod-binding-step"><span class="number">3</span><strong>${t("建立一对一关系")}</strong><p>${t("不按同名邮箱静默合并，冲突时拒绝覆盖。")}</p></div></div><div class="prod-status">${I('shield')}<div><strong>${t("开始前请确认")}</strong><p>${t("绑定是可选的一对一关系，不会合并两个账号、密码或余额。")}</p><p>${t("绑定后不能普通自助解绑或换绑；不会读取 API Key，不会改变社区密码或治理角色。")}</p></div></div><div class="flex wrap mt24">${external(connectorPath, I('link') + t("开始安全绑定"), 'btn primary')}${link('/me', t("暂不绑定"), 'btn ghost')}</div>`}</section><section class="card card-pad mt24"><h3>${t("身份边界")}</h3><p class="muted mt8">${t("浏览器不能提交可信 user_id。所有回调参数在服务端验签、校验 flow 与 nonce 前都视为不可信输入。")}</p></section>${footer()}`;
  }

  function loadPulseCore() {
    if (pulseCoreReady) return Promise.resolve(true);
    if (!pulseAssets) {
      const nodes = [];
      const cleanups = [];
      pulseAssets = Promise.all(['css', 'js'].map((kind) => new Promise((resolve, reject) => {
        if (kind === 'js' && window.MetarPulseCore) { resolve(); return; }
        const asset = document.createElement(kind === 'css' ? 'link' : 'script');
        nodes.push(asset);
        if (kind === 'css') { asset.rel = 'stylesheet'; asset.href = '/metar-assets/pulse-core.css'; }
        else asset.src = '/metar-assets/pulse-core.js';
        const timer = setTimeout(() => reject(new Error('Pulse presentation timed out')), 5000);
        cleanups.push(() => { clearTimeout(timer); asset.onload = asset.onerror = null; });
        asset.onload = () => { clearTimeout(timer); resolve(); };
        asset.onerror = () => { clearTimeout(timer); reject(new Error('Pulse presentation unavailable')); };
        document.head.appendChild(asset);
      }))).then(() => {
        if (!window.MetarPulseCore) throw new Error('Pulse presentation unavailable');
        pulseCoreReady = true; return true;
      }).catch(() => {
        nodes.forEach(node => node.remove()); pulseAssets = null; return false;
      }).finally(() => cleanups.forEach(cleanup => cleanup()));
    }
    return pulseAssets;
  }

  function pulseResultDisplay(result, quotaPerUnit) {
    const exp = result.reward_type === 'community_exp';
    const formatted = formatPulseQuota(result.amount, quotaPerUnit, locale());
    return { type: result.reward_type, amount: exp ? number(result.amount) : formatted.replace(/ (⚡️|quota)$/, ''),
      unit: exp ? 'EXP' : Number.isSafeInteger(quotaPerUnit) && quotaPerUnit > 0 ? '⚡️' : 'quota',
      tier: pulseRewardTier(result, quotaPerUnit) };
  }

  function pulseSessionDisplay(last) {
    return last?.rewards.length ? { rewards: last.rewards.map(reward => pulseResultDisplay(reward, last.quotaPerUnit)), total: last.total } : null;
  }


  function pulseHistoryRows(rewards, perUnit) {
    const amount = r => r.reward_type === 'community_exp' ? number(r.amount)+' EXP' : formatPulseQuota(r.amount, perUnit, locale());
    return rewards.map(r => `<tr><td>${esc(amount(r))}</td><td data-pulse-reward-status="${esc(r.grant_id)}">${esc(t(pulseRewardStates[r.status] || '等待核对'))}</td><td>${esc(r.created_at ? new Date(r.created_at).toLocaleString(locale()) : '—')}</td><td><code>${esc(r.grant_id)}</code></td></tr>`).join('') || `<tr><td colspan="4">${t('暂无奖励记录')}</td></tr>`;
  }

  function pulseRecentRows(rewards, perUnit) {
    if (!rewards.length) return `<tr><td colspan="3" class="prod-pulse-recent-empty"><strong>${t('尚无抽奖记录')}</strong><span>${t('开启脉冲后，你的奖励会显示在这里。')}</span></td></tr>`;
    return rewards.slice(0, 5).map(reward => {
      const display = pulseResultDisplay(reward, perUnit);
      return `<tr><td><time datetime="${esc(reward.created_at)}" title="${esc(new Date(reward.created_at).toLocaleString(locale()))}">${esc(new Date(reward.created_at).toLocaleString(locale(), {month:'2-digit', day:'2-digit', hour:'2-digit', minute:'2-digit'}))}</time></td><td><span class="prod-pulse-recent-reward" data-tier="${esc(display.tier)}"><span aria-hidden="true">${display.type === 'community_exp' ? '☆' : '♢'}</span><strong>${esc(display.amount)} <small>${esc(display.unit)}</small></strong></span></td><td><span data-pulse-reward-status="${esc(reward.grant_id)}">${esc(t(pulseRewardStates[reward.status] || '等待核对'))}</span></td></tr>`;
    }).join('');
  }

  async function pulsePage({ deferHistory = false } = {}) {
    const sequence = navigationSequence;
    if (currentUserState === 'unavailable') return identityUnavailable(t("Pulse 权益"));
    if (!currentUser) return loginRequired(t("Pulse 权益"), t("Pulse 只对主动绑定元衡 API 身份的社区成员展示本人权益。"));
    if (!isActiveUser(currentUser)) return accountUnavailable(t("Pulse 权益"));
    const userId = currentUser.id;
    const binding = await answer.getBindingState();
    if (binding.status === 'unbound') return pulseUnboundPage(sequence, userId);
    if (binding.status === 'unavailable') throw new AdapterError(t("绑定状态暂时不可查询，Pulse 页面不会据此猜测身份。"), { code: 'binding_unavailable' });
    const [summary, catalog, history, coreReady] = await Promise.all([pulse.summary(), pulse.rules(), deferHistory ? {rewards:[]} : pulse.rewards(), loadPulseCore()]);
    if (sequence !== navigationSequence || currentUser?.id !== userId) return '';
    const rules = catalog.selection_version === 3 ? catalog : {...catalog, enabled:false, unavailable_reason:'selection_required',draws:[]};
    const operationStore = new PulseOperation(currentUser.id);
    let pending = operationStore.read();
    if (pending && !pulseBusy) {
      const recovered = await new PulseDrawSession(pulse, operationStore).recover(pending);
      if (sequence !== navigationSequence || currentUser?.id !== userId) return '';
      if (recovered.length) {
        pulseLastResult = { userId: currentUser.id, rewards: recovered, total: pulseActions(pending).length, quotaPerUnit: rules.quota_per_unit };
        if (recovered.length === pulseActions(pending).length) {
          operationStore.clear(); pending = null; pulseMessage = '已找到本次抽奖记录，请查看奖励明细。';
        }
      }
    }
    const unavailable = { selection_required: '请刷新并确认最新抽奖规则', insufficient_tickets:'积累脉冲券后，即可开启下一次回馈', rule_inactive:'当前规则已暂停或结束', budget_exhausted: '当前可用奖励预算已用完', activity_paused: '活动暂未开放', no_active_period: '当前规则尚未准备好', funding_verification_required: '当前权益正在核验', reward_pool_unavailable: '奖池准备中' };
    const available = Number.isSafeInteger(rules.ticket_count) ? rules.ticket_count : 0;
    const canDrawFive = rules.can_draw_five === true && rules.draws?.length === 5;
    const rewards = Array.isArray(history.rewards) ? history.rewards : [];
    const canDraw = rules.enabled === true && available > 0 && !pending && !pulseBusy;
    const last = pulseLastResult?.userId === currentUser.id ? pulseLastResult : null;
    if (last) last.rewards = last.rewards.map(reward => rewards.find(r => r.grant_id === reward.grant_id && validPulseResult(r, reward.action_id)) || reward);
    if (sequence === navigationSequence) {
      pulseHasPendingRewards = (deferHistory && pulseHasPendingRewards) || rewards.some(pulseRewardPending) || Boolean(last?.rewards.some(pulseRewardPending));
      pulseCoreState = {
        tickets: number(available), canDraw, canDrawFive: canDraw && canDrawFive, selection: rules, pending: Boolean(pending), busy: pulseBusy, quotaPerUnit: rules.quota_per_unit,
        result: pulseSessionDisplay(last),
        message: pulseMessage ? t(pulseMessage) : '',
        reason: pending ? t('正在确认本轮抽奖结果') : !rules.enabled ? t(unavailable[rules.unavailable_reason] || '活动暂不可用') : available === 0 ? t('积累脉冲券后，即可开启下一次回馈') : '',
      };
    }
    const benefits = `<dl class="prod-pulse-benefits" data-pulse-benefits-content><div><dt>${t('额度奖励资格')}</dt><dd>${esc(rules.period?.continuous ? (rules.experience_only ? t('仅经验') : rules.quota_expires_at ? new Date(rules.quota_expires_at).toLocaleString(locale()) : t('从获得券时计算')) : rules.period?.ends_at ? new Date(rules.period.ends_at).toLocaleString(locale()) : '—')}</dd></div></dl>
      ${rules.period?.continuous ? `<div class="prod-page-note" data-pulse-benefits-note><p>${t('已有券保留原额度有效期；新设置的有效天数只适用于以后获得的券。')}</p><p>${esc(rules.experience_only ? t('下一张券已超过额度有效期，仅抽取经验。') : t('系统优先使用额度资格有效的券。'))}</p></div>` : ''}`;
    const dialogHeader = (id, title) => `<div class="prod-pulse-dialog-header"><h2 id="${id}-title">${t(title)}</h2><form method="dialog"><button type="submit" class="prod-pulse-dialog-close" aria-label="${t('关闭窗口')}" autofocus>×</button></form></div>`;
    return `${coreReady ? window.MetarPulseCore.markup({ t }) : `<section class="card card-pad"><div class="prod-pulse-tools"><button type="button" class="prod-pulse-link" data-action="pulse-benefits" aria-haspopup="dialog" aria-controls="pulse-benefits">${t('我的权益')}</button><button type="button" class="prod-pulse-link" data-action="pulse-history" aria-haspopup="dialog" aria-controls="pulse-history">${t('奖励记录')}</button></div><h1>${esc(t('可用脉冲券：{count}', { count: number(available) }))}</h1><details class="prod-pulse-fallback-help"><summary aria-label="${t('抽取规则')}">?</summary><div><p>${t('每次消耗 1 张券。额度用于 API 调用，经验用于社区升级，均不可转赠。')}</p><p>${t('五连抽可合并不同时间获得的券；每次使用最新规则，中断时保留已获得的奖励。')}</p><p>${t('白光为经验奖励；额度奖励依次为蓝光（小于 0.5 ⚡️）、紫光（0.5 起）、金光（2 起）、红光（10 起）。光效不影响中奖概率。')}</p></div></details>
      ${!rules.enabled ? `<p role="status">${esc(t(unavailable[rules.unavailable_reason] || '活动暂不可用'))}</p>` : ''}
      <div class="actions"><button type="button" class="btn primary" data-action="pulse-draw" ${canDraw ? '' : 'disabled'}>${t(pulseBusy ? '正在处理…' : '开启一次脉冲 · 1 券')}</button><button type="button" class="btn primary" data-action="pulse-draw-five" ${canDraw && canDrawFive ? '' : 'disabled'}>${t('五连抽 · 5 券')}</button><button type="button" class="btn" data-action="retry">${t('刷新权益')}</button></div></section>`}
      <div data-pulse-details>
      ${coreReady && !rules.enabled ? `<p class="prod-page-note" role="status">${esc(t(unavailable[rules.unavailable_reason] || '活动暂不可用'))}</p>` : ''}
      ${pulseMessage && (!coreReady || pulseMessage !== '本次抽奖已完成。') ? `<div class="prod-status mt24" role="status"><p data-pulse-message>${esc(t(pulseMessage))}</p></div>` : ''}
      ${pending ? `<section class="card card-pad mt24" role="status"><h3>${t('正在确认本轮抽奖结果')}</h3><p class="muted mt8">${t('本轮尚有结果未确认。继续后会恢复原抽奖；已完成的次数不重复扣券，尚未执行的次数每次消耗 1 张券。')}</p><div class="flex wrap mt16"><button class="btn" data-action="retry">${t('查询原抽奖')}</button><button class="btn primary" data-action="pulse-resume" ${pulseBusy ? 'disabled' : ''}>${t('继续完成本轮抽奖')}</button></div></section>` : ''}
      </div>
      ${communityTasksMarkup()}
      <div class="prod-pulse-overview">
        <section class="prod-pulse-panel prod-pulse-recent" aria-labelledby="pulse-recent-title"><header><h2 id="pulse-recent-title">${I('clock')} ${t('我的抽奖记录')}</h2><button type="button" class="prod-pulse-link" data-action="pulse-history" aria-haspopup="dialog" aria-controls="pulse-history">${t('查看全部记录')} <span aria-hidden="true">→</span></button></header><p data-pulse-history-error role="status" class="prod-page-note" hidden></p><div class="prod-pulse-recent-table"><table><thead><tr><th>${t('时间')}</th><th>${t('获得奖励')}</th><th>${t('到账状态')}</th></tr></thead><tbody data-pulse-recent-rows>${pulseRecentRows(rewards, rules.quota_per_unit)}</tbody></table></div></section>
        <section class="prod-pulse-panel prod-pulse-community" data-pulse-feed aria-labelledby="pulse-community-title"><header><h2 id="pulse-community-title">${I('trophy')} ${t('社区中奖动态')}</h2><button type="button" class="prod-pulse-link" data-action="pulse-winners" aria-haspopup="dialog" aria-controls="pulse-winners">${t('查看更多')} <span aria-hidden="true">→</span></button></header><p class="prod-pulse-feed-note">${t('普通用户匿名展示，管理员可查看中奖用户名。')}</p>${pulseFeedTable('preview')}</section>
      </div>
      <dialog id="pulse-winners" class="prod-pulse-dialog" aria-labelledby="pulse-winners-title">${dialogHeader('pulse-winners', '社区中奖动态')}<p class="prod-page-note">${t('仅显示最近 100 条已到账的抽奖奖励。')}</p>${pulseFeedTable('full')}</dialog>
      <dialog id="pulse-benefits" class="prod-pulse-dialog prod-pulse-benefits-dialog" aria-labelledby="pulse-benefits-title">${dialogHeader('pulse-benefits', '我的权益')}<div data-pulse-benefits-body>${benefits}</div></dialog>
      <dialog id="pulse-history" class="prod-pulse-dialog" data-pulse-history aria-labelledby="pulse-history-title">${dialogHeader('pulse-history', '奖励记录')}<p data-pulse-history-error role="status" class="prod-page-note" hidden></p><div class="prod-pulse-table" tabindex="0" role="region" aria-label="${t('奖励记录')}"><table><thead><tr><th>${t('奖励')}</th><th>${t('到账状态')}</th><th>${t('时间')}</th><th>${t('奖励编号')}</th></tr></thead><tbody data-pulse-history-rows>${pulseHistoryRows(rewards, rules.quota_per_unit)}</tbody></table></div></dialog>${footer()}`;
  }

  // Unbound members see the same draw stage with zero tickets and a bind prompt.
  // No Pulse/BFF request is made: tickets and rewards belong to the API account.
  async function pulseUnboundPage(sequence, userId) {
    const coreReady = await loadPulseCore();
    if (sequence !== navigationSequence || currentUser?.id !== userId) return '';
    const reason = t('绑定元衡 API 账号后即可获得抽奖券并抽奖');
    pulseHasPendingRewards = false;
    pulseCoreState = { unbound: true, tickets: number(0), canDraw: false, canDrawFive: false, selection: null, pending: false, busy: false, quotaPerUnit: undefined, result: null, message: '', reason };
    const dialogHeader = (id, title) => `<div class="prod-pulse-dialog-header"><h2 id="${id}-title">${t(title)}</h2><form method="dialog"><button type="submit" class="prod-pulse-dialog-close" aria-label="${t('关闭窗口')}" autofocus>×</button></form></div>`;
    const bind = link('/settings/binding', t('去绑定元衡 API 账号 ') + I('arrow', 'sm'), 'btn primary');
    return `${coreReady ? window.MetarPulseCore.markup({ t }) : `<section class="card card-pad"><h1>${esc(t('可用脉冲券：{count}', { count: number(0) }))}</h1><div class="actions"><button type="button" class="btn primary" disabled>${t('开启一次脉冲 · 1 券')}</button><button type="button" class="btn primary" disabled>${t('五连抽 · 5 券')}</button></div></section>`}
      <div data-pulse-details><section class="card card-pad mt24 prod-pulse-bind" data-pulse-bind role="status"><div class="between wrap"><h2>${I('link')} ${t('尚未绑定元衡 API 账号')}</h2>${badge(t('未绑定'))}</div><p class="muted mt8">${t('抽奖券、额度奖励和抽奖记录都归属你的元衡 API 账号。完成一次绑定后，每日签到和社区互动即可获得抽奖券，额度奖励直接到账 API 账号。社区账号仍可独立使用。')}</p><div class="actions mt16">${bind}${link('/latest', t('继续浏览社区'), 'btn')}</div></section></div>
      ${communityTasksMarkup(false)}
      <dialog id="pulse-benefits" class="prod-pulse-dialog prod-pulse-benefits-dialog" aria-labelledby="pulse-benefits-title">${dialogHeader('pulse-benefits', '我的权益')}<div data-pulse-benefits-body><p>${t('尚未绑定元衡 API 账号，暂无抽奖券和权益。')}</p><div class="actions mt16">${bind}</div></div></dialog>${footer()}`;
  }

  function communityTasksMarkup(bound = true) {
    return `<section class="card card-pad mt24" data-community-tasks><div class="between wrap"><h2>${t('获取抽奖券')}</h2><button class="btn" data-task-checkin>${t(bound ? '每日签到 · +1 券' : '每日签到 · 领经验')}</button></div>
      <ul class="mini-list"><li>${t('帖子每收到 10 次评论 +1 券，每天最多 2 券。')}</li><li>${t('给别人点赞每 40 次或评论每 20 次 +1 券，分别累计，合计每天最多 3 券。')}</li><li>${t('每日签到 +1 券；社区任务每天最多 6 券。')}</li></ul>
      ${bound ? '' : `<p class="prod-page-note">${t('未绑定账号签到可领经验；绑定元衡 API 后签到另得 1 张抽奖券，绑定前不补发。')}</p>`}
      <p class="muted mt12">${t('按北京时间每日重置，仅统计绑定后及功能上线后的有效互动。评论包含回答和评论；自评自赞不计，撤赞重赞不重复计数。')}</p>
      <p class="muted">${t('互动达标后自动发券，通常需等待片刻。Meta API 消费不再获得抽奖券。')}</p>
      <p role="status" data-task-progress>${t('正在读取今日进度…')}</p><div class="actions"><button class="btn" data-task-refresh>${t('刷新任务进度')}</button><button class="btn" data-action="pulse-refresh">${t('刷新权益')}</button>${link('/latest',t('参与讨论'),'btn')}</div></section>`;
  }
  async function mountCommunityTasks() {
    const root = document.querySelector('[data-community-tasks]');
    if (!root || root.dataset.mounted) return;
    root.dataset.mounted = '1';
    const bound = !pulseCoreState?.unbound;
    const user = currentUser?.id, sequence = navigationSequence, identity = identitySequence;
    const current = () => root.isConnected && user === currentUser?.id && identity === identitySequence && sequence === navigationSequence;
    const status = root.querySelector('[data-task-progress]'), checkin = root.querySelector('[data-task-checkin]'), refresh = root.querySelector('[data-task-refresh]');
    let busy = false, signing = false, confirmed = false, awaitingCheckin = false;
    let syncing = false, attempts = 0, timer, lastDelivered = null;
    const schedule = () => {
      clearTimeout(timer);
      if (!current() || !syncing) return;
      if (attempts >= 12) {
        status.textContent = t(bound ? '赠券仍在同步，稍后点击“刷新任务进度”即可继续查询，无需刷新浏览器。' : '签到状态暂未确认，请稍后刷新任务进度或重试签到。');
        return;
      }
      timer = setTimeout(() => { attempts++; void load(); }, 5000);
    };
    const load = async ({refreshBenefits = false} = {}) => {
      if (busy || signing || !current()) return;
      clearTimeout(timer);
      busy = true; refresh.disabled = true; checkin.disabled = true;
      try {
        if (!bound) {
          const summary = await answer.request('/metar/experience/summary');
          if (!current()) return;
          if (!Number.isSafeInteger(summary.checkin_amount) || summary.checkin_amount < 0) throw new Error('invalid check-in status');
          confirmed ||= summary.checkin_amount === 0;
          syncing = awaitingCheckin && !confirmed;
          if (confirmed) awaitingCheckin = false;
          checkin.textContent = t(confirmed ? '今日已签到' : '每日签到 · 领经验');
          status.textContent = t(confirmed ? '今日已签到' : '未绑定账号签到可领经验；绑定元衡 API 后签到另得 1 张抽奖券，绑定前不补发。');
          return;
        }
        const progress = await pulse.request('tasks');
        if (!current()) return;
        if (!['likes','comments','received','earned','delivered'].every(k=>Number.isSafeInteger(progress[k]) && progress[k]>=0) || progress.earned>6 || progress.delivered>progress.earned || typeof progress.checked_in !== 'boolean') throw new Error('invalid task progress');
        const ineligible = progress.checkin_ineligible_reason || '';
        if (!['', 'before_binding', 'before_start'].includes(ineligible) || (ineligible && !progress.checked_in)) throw new Error('invalid check-in eligibility');
        confirmed ||= progress.checked_in;
        if (ineligible) awaitingCheckin = false;
        checkin.disabled = signing || confirmed;
        checkin.textContent = t(confirmed ? '今日已签到' : '每日签到 · +1 券');
        if (lastDelivered === null) lastDelivered = progress.delivered;
        const deliveryChanged = lastDelivered !== progress.delivered;
        const wasSyncing = syncing;
        syncing = (!ineligible && (awaitingCheckin || progress.checked_in) && progress.checkin_delivered !== true) || progress.delivered < progress.earned;
        status.textContent = ineligible ? t(ineligible === 'before_binding' ? '今日已签到；签到发生在账号绑定前，今天不补发签到券。明天签到可获券。' : '今日已签到；签到发生在赠券功能启用前，今天不补发签到券。明天签到可获券。') : syncing ? t('签到与赠券正在同步，到账后将自动更新抽奖券。') : t('今日点赞 {likes} 次 · 评论 {comments} 次 · 收到评论 {received} 次；已获得 {earned}/6 券，已入账 {delivered} 券。',progress);
        // Refresh the authoritative selection as well as the count: a newly
        // delivered ticket may enable single or five draws. Never invent +1.
        if (deliveryChanged || (!ineligible && (awaitingCheckin || progress.checked_in) && progress.checkin_delivered === undefined && attempts % 3 === 2) || (!syncing && (awaitingCheckin || wasSyncing || refreshBenefits))) {
          const refreshed = await refreshPulse();
          if (refreshed) lastDelivered = progress.delivered;
          else syncing = true;
          if (!current()) return;
          if (!syncing) awaitingCheckin = false;
        }
      } catch (error) {
        if (current()) status.textContent=t('任务进度暂不可用，请稍后刷新。已确认的券不会重复发放。');
      } finally {
        busy=false;
        if(current()) { refresh.disabled=false; checkin.disabled=signing || confirmed; schedule(); }
      }
    };
    refresh.onclick=() => { attempts=0; return load({refreshBenefits:true}); };
    checkin.onclick=async()=>{
      if(checkin.disabled || busy || signing || !current())return;
      signing=true; checkin.disabled=true;
      try {
        await answer.request('/metar/experience/checkin',{method:'POST',headers:{'Content-Type':'application/json','X-Metar-Request':'1'},body:'{}'});
        if (!current()) return;
        confirmed=true;
        checkin.textContent=t('今日已签到');
        status.textContent=t('签到与赠券正在同步，到账后将自动更新抽奖券。');
      }catch(_){
        if(current())status.textContent=t('暂时无法确认签到，请重试；同一天不会重复签到或发券。');
      }finally{
        signing=false;
        if(current()) {
          checkin.disabled=confirmed;
          // Even a lost response may have committed. Recover with reads only.
          awaitingCheckin=true; syncing=true; attempts=0;
          await load();
        }
      }
    };
    await load();
  }

  function pulseFeedTable(kind) {
    return `<div class="prod-pulse-recent-table"><table><thead><tr><th>${t('用户')}</th><th>${t('获得奖励')}</th><th>${t('时间')}</th></tr></thead><tbody data-pulse-feed-rows="${kind}"></tbody></table></div><p data-pulse-feed-message role="status" class="prod-page-note">${t('正在加载社区中奖动态…')}</p><div class="prod-pulse-feed-tools"><button type="button" class="prod-pulse-link" data-pulse-feed-retry disabled>${t('刷新动态')}</button></div>`;
  }

  async function mountPulseFeed() {
    const root = document.querySelector('[data-pulse-feed]');
    if (!root) return;
    const sequence = navigationSequence, userId = currentUser?.id, token = answer.token();
    const administrator = isCommunityAdministrator(currentUser);
    const current = () => administrator === isCommunityAdministrator(currentUser) && sequence === navigationSequence && route().path === '/pulse' && currentUserState === 'ready' && currentUser?.id === userId && isActiveUser(currentUser) && answer.token() === token;
    try {
      await loadFeatureScript('pulse-feed', 'MetarRewardFeed');
      if (!current()) return;
      pulseFeedView?.dispose();
      pulseFeedView = new window.MetarRewardFeed.View(root, document.getElementById('pulse-winners'), {client:administrator ? {winners:() => new PulseAdminAdapter(answer).request('winners')} : pulse, administrator, t, displayReward:pulseResultDisplay, locale, current});
    } catch (_) {
      if (!current()) return;
      [root, document.getElementById('pulse-winners')].filter(Boolean).forEach(container => {
        container.querySelector('[data-pulse-feed-message]').textContent = t('暂时无法加载社区中奖动态。');
        const retry = container.querySelector('[data-pulse-feed-retry]');
        retry.disabled = false;
        retry.onclick = () => { retry.onclick = null; retry.disabled = true; void mountPulseFeed(); };
      });
    }
  }

  function supportPage() {
    return `${pageTitle(t("帮助中心"))}<div class="prod-topic-grid"><section class="card prod-topic-card"><div><div class="topic-icon">${I('user')}</div><h3>${t("社区账号与内容")}</h3><p>${t("管理个人资料、登录方式和通知偏好。")}</p></div>${external('/users/settings/profile', t("打开账号设置 ") + I('arrow', 'sm'), 'textlink')}</section><section class="card prod-topic-card"><div><div class="topic-icon">${I('link')}</div><h3>${t("账号绑定纠错")}</h3><p>${t("绑定冲突、误绑核对不提供普通解绑；处理需要确认授权并保留审计。")}</p></div>${link('/settings/binding', t("查看绑定状态 ") + I('arrow', 'sm'), 'textlink')}</section><section class="card prod-topic-card"><div><div class="topic-icon">${I('pulse')}</div><h3>${t("Pulse 奖励状态")}</h3><p>${t("请保留非敏感 Reward Grant ID。不要提交密码、Cookie、API Key 或完整回调 URL。")}</p></div>${link('/pulse', t("查看权益入口 ") + I('arrow', 'sm'), 'textlink')}</section></div>${footer()}`;
  }

  async function statusPage() {
    await answer.request('/question/page?page=1&page_size=1&order=active');
    return `${pageTitle(t("服务状态"))}<section class="card card-pad"><div class="prod-status">${I('check')}<div><strong>${t("社区读取服务正常")}</strong><p>${t("Apache Answer 公共问题接口已返回。此结果不代表 Pulse、new-api、邮件或奖励结算服务均正常。")}</p></div></div><div class="divider"></div><div class="between wrap"><div><h3>${t("更完整的运行状态")}</h3><p class="prod-page-note">${t("只有配置真实监控来源后才显示外部状态页，不使用前端假数据。")}</p></div>${config.statusUrl ? outbound(config.statusUrl, t("打开状态页 ") + I('external', 'sm'), 'btn') : badge(t("状态页未配置"))}</div></section>${footer()}`;
  }

  function guidelinesPage() {
    return `${pageTitle(t("社区规范"))}<section class="card card-pad stack"><div><h2>${t("1. 描述可复现的问题")}</h2><p class="muted mt8">${t("说明目标、环境、已尝试方法和实际结果；不要泄露 API Key、Cookie、支付信息、完整 Prompt/Response 或个人隐私。")}</p></div><div><h2>${t("2. 内容不自动产生权益")}</h2><p class="muted mt8">${t("论坛发帖、回答、点赞不得产生 contribution 或 ticket。内容奖励使用独立资格、预算与人工审核。")}</p></div><div><h2>${t("3. 尊重身份边界")}</h2><p class="muted mt8">${t("社区身份来自 Answer，API 与资金身份来自 new-api。绑定可选、一对一，禁止静默换绑和身份转移。")}</p></div><div><h2>${t("4. 对不确定结果先查询")}</h2><p class="muted mt8">${t("奖励发放中或服务超时时，查询原 Grant 和 source_ref，不重复开启或重新随机。")}</p></div></section>${footer()}`;
  }

  async function legalPage(path) {
    // Static, trusted build-time HTML from seo-pages.json; no user data involved.
    await loadFeatureScript('legal', 'MetarLegal');
    const page = window.MetarLegal[path];
    const note = getLanguage() !== 'en_US' ? '' : `<p class="legal-language-note">${t('本文件仅提供中文版本，以中文版本为准。')}</p>`;
    return `<article class="card card-pad legal-doc">${note}${page.body}</article>${footer()}`;
  }

  function notFoundPage() {
    return `<section class="card">${empty(t("没有找到这个页面"), t("请检查地址，或返回社区继续浏览。"), link('/latest', '← ' + t("返回发现"), 'page-back'), 'compass')}</section>${footer()}`;
  }

  async function resolveView() {
    const { path } = route();
    if (/^\/(?:me|settings|admin|pulse|chat)(?:\/|$)/.test(path)) await identityReady;
    if (path === '/admin/publishing') {
      if (currentUserState === 'unavailable') return identityUnavailable('内容运营');
      if (!currentUser) return loginRequired('内容运营', t('请先登录社区账号。'));
      if (!isCommunityAdministrator(currentUser)) return empty(t('需要管理员权限'), '', link('/me', '← ' + t('返回'), 'page-back'), 'shield');
      await loadFeatureScript('admin-publishing', 'MetarPublishing');
      publishingView = new window.MetarPublishing.View(new AnswerAdapter({answerApiBase:'/answer/admin/api'}));
      return await publishingView.page();
    }
    if (path === '/admin/models') {
      const title = currentTitle();
      if (currentUserState === 'unavailable') return identityUnavailable(title);
      if (!currentUser) return loginRequired(title, t('请先登录社区账号。'));
      if (!isCommunityAdministrator(currentUser)) return empty(t('需要管理员权限'), '', link('/models', '← ' + t('返回榜单'), 'page-back'), 'shield');
      await loadFeatureScript('admin-models', 'MetarModelsAdmin');
      modelsAdminView = new window.MetarModelsAdmin.View(answer);
      return await modelsAdminView.page(currentUser);
    }
    if (path === '/models' || path.startsWith('/models/') || path.startsWith('/model/')) {
      // Anonymous public data: no identity wait, no Answer adapter.
      await loadFeatureScript('models', 'MetarModels');
      modelsView = new window.MetarModels.View();
      return await modelsView.page(path, route().query);
    }
    if (path === '/tools' || path.startsWith('/tools/')) {
      // Public client-side tools: no identity wait and no Answer, Pulse or Intel dependency.
      await loadFeatureScript('tools', 'MetarTools');
      toolsView = new window.MetarTools.View(TOOLS);
      return toolsView.page(path);
    }
    if (path === '/marketplace' || path.startsWith('/marketplace/') || path === '/admin/marketplace') {
      await identityReady;
      if (['/marketplace/mine', '/marketplace/publish', '/admin/marketplace'].includes(path)) {
        const title = currentTitle();
        if (currentUserState === 'unavailable') return identityUnavailable(title);
        if (!currentUser) return loginRequired(title, t('请先登录社区账号。'));
        if (!isActiveUser(currentUser)) return accountUnavailable(title);
        if (path === '/admin/marketplace' && !isCommunityAdministrator(currentUser)) return empty(t('需要管理员权限'), '', link('/marketplace', '← ' + t('返回集市'), 'page-back'), 'shield');
      }
      await loadFeatureScript('marketplace', 'MetarMarketplace');
      marketplaceView = new window.MetarMarketplace.View(answer, new AnswerAdapter({answerApiBase:'/answer/admin/api'}));
      return await marketplaceView.page(path, currentUser, route().query);
    }
    if (path === '/admin/operations') {
      if (currentUserState === 'unavailable') return identityUnavailable(currentTitle());
      if (!currentUser) return loginRequired(currentTitle(), t('请先登录社区账号。'));
      if (!isCommunityAdministrator(currentUser)) return empty(t('需要管理员权限'), '', link('/latest', '← ' + t('返回社区'), 'page-back'), 'shield');
      await loadFeatureScript('analytics', 'MetarOperations');
      operationsView = new window.MetarOperations.View(new AnswerAdapter({answerApiBase:'/answer/admin/api'}));
      return await operationsView.page();
    }
    if (['/me/drafts', '/events', '/admin/events'].includes(path)) {
      await identityReady;
      const title = currentTitle();
      if (path !== '/events') {
        if (currentUserState === 'unavailable') return identityUnavailable(title);
        if (!currentUser) return loginRequired(title, t('请先登录社区账号。'));
        if (!isActiveUser(currentUser)) return accountUnavailable(title);
        if (path === '/admin/events' && !isCommunityAdministrator(currentUser)) return empty(t('需要管理员权限'), '', link('/latest', '← ' + t('返回社区'), 'page-back'), 'shield');
      }
      await loadFeatureScript('drafts-store', 'MetarDrafts');
      await loadFeatureScript('member-pages', 'MetarMemberPages');
      memberView = new window.MetarMemberPages.View(answer, new AnswerAdapter({answerApiBase:'/answer/admin/api'}));
      return await memberView.page(path, isActiveUser(currentUser) ? currentUser : null, route().query);
    }
    if (path === '/chat' || path === '/admin/chat') {
      const title = t(path === '/chat' ? '聊天' : '聊天举报');
      if (currentUserState === 'unavailable') return identityUnavailable(title);
      if (!currentUser) return loginRequired(title, t('登录社区账号后开始聊天，无需绑定 API 账号。'));
      if (!isActiveUser(currentUser)) return accountUnavailable(title);
      if (path === '/admin/chat' && !isCommunityAdministrator(currentUser)) return empty(t('需要管理员权限'), '', link('/latest', '← ' + t('返回社区'), 'page-back'), 'shield');
      await loadFeatureScript('chat', 'MetarChat');
      if (path === '/admin/chat') {
        chatModeration = new window.MetarChat.Moderation(answer, new AnswerAdapter({answerApiBase:'/answer/admin/api'}));
        return await chatModeration.page(route().query.get('before') || '');
      }
      if (!chatView || chatView.user !== String(currentUser.id)) chatView = new window.MetarChat.View(answer, new AnswerAdapter({answerApiBase:'/answer/admin/api'}), currentUser);
      return chatView.shell();
    }
    if (path === '/latest') return questionsPage();
    if (path === '/topics') return topicsPage();
    if (path === '/admin/taxonomy') {
      const title = t('类别管理');
      if (currentUserState === 'unavailable') return identityUnavailable(title);
      if (!currentUser) return loginRequired(title, t('请使用社区管理员账号登录。'));
      if (!isCommunityAdministrator(currentUser)) return `<section class="card">${empty(t('需要管理员权限'), '', link('/latest', '← ' + t('返回社区'), 'page-back'), 'shield')}</section>${footer()}`;
      return `${pageTitle(title)}${await taxonomy.orderPage(answer)}${taxonomy.adminPage()}${footer()}`;
    }
    if (path === '/knowledge') return knowledgePage();
    if (path === '/search') return searchPage();
    if (['/me/settings','/me/badges','/me/notifications'].includes(path)) return personalNativePage();
    if (path === '/me') return profilePage();
    if (path === '/me/growth' || path === '/admin/growth') {
      const title = t(path === '/admin/growth' ? '成长管理' : '社区成长');
      if (currentUserState === 'unavailable') return identityUnavailable(title);
      if (!currentUser) return loginRequired(title, t('登录后查看个人资料与发布记录。'));
      if (!isActiveUser(currentUser)) return accountUnavailable(title);
      if (path === '/admin/growth' && !isCommunityAdministrator(currentUser)) return `${pageTitle(title)}<section class="card">${empty(t('需要管理员权限'), '', link('/latest', '← ' + t('返回社区'), 'page-back'), 'shield')}</section>${footer()}`;
      return `${pageTitle(title)}${await growthView().then(view => path === '/admin/growth' ? view.adminPage() : view.page())}${footer()}`;
    }
    if (path === '/me/bookmarks') return bookmarksPage();
    if (path === '/settings/binding') return bindingPage();
    if (path === '/pulse') return pulsePage();
    if (path === '/admin/pulse') {
      if (currentUserState === 'unavailable') return identityUnavailable(t('Pulse 配置'));
      if (!currentUser) return loginRequired(t('Pulse 配置'), t('请使用社区管理员账号登录。'));
      if (!isCommunityAdministrator(currentUser)) return `<section class="card">${empty(t('需要管理员权限'), t('仅正常且已激活的社区管理员可管理 Pulse 配置。'), link('/latest', '← ' + t('返回社区'), 'page-back'), 'shield')}</section>${footer()}`;
      return `${pageTitle(t('Pulse 配置'))}${await adminView().then(view => view.page())}${footer()}`;
    }
    if (path === '/support') return supportPage();
    if (path === '/status') return statusPage();
    if (path === '/guidelines') return guidelinesPage();
    if (path === '/tos' || path === '/privacy') return legalPage(path);
    if (path === '/topic/vibe-coding') { location.replace('/marketplace' + location.search); return ''; }
    if (path.startsWith('/topic/')) return questionsPage(decodeURIComponent(path.slice('/topic/'.length)));
    return notFoundPage();
  }

  async function submitPulse(resume = false, count = 1) {
    if (!resume && (count === 5 ? !pulseCoreState?.canDrawFive : !pulseCoreState?.canDraw)) return;
    if (pulseBusy || pulseRefreshing || !currentUser || !isActiveUser(currentUser)) return;
    pulseBusy = true;
    clearTimeout(pulseStatusTimer);
    pulseStatusEpoch++;
    pulseStatusAttempts = 0;
    pulseMessage = '';
    const presentation = pulseView;
    const quotaPerUnit = pulseCoreState?.quotaPerUnit;
    const userId = currentUser.id, identity = identitySequence, sequence = navigationSequence;
    const sameUser = () => currentUser?.id === userId && identitySequence === identity;
    document.querySelectorAll('[data-action="pulse-draw"], [data-action="pulse-draw-five"], [data-action="pulse-resume"]').forEach((button) => { button.disabled = true; });
    const store = new PulseOperation(userId);
    try {
      const operation = resume ? store.read() : store.begin(count, pulseCoreState?.selection);
      if (!operation) return;
      const session = new PulseDrawSession(pulse, store);
      const total = pulseActions(operation).length;
      pulseLastResult = { userId, rewards: [], total, quotaPerUnit };
      presentation?.begin(total);
      const known = resume ? await session.recover(operation) : [];
      await session.run(operation, {
        known,
        canContinue: () => sameUser() && sequence === navigationSequence && route().path === '/pulse',
        onResult: rewards => {
          if (!sameUser()) return;
          pulseLastResult = { userId, rewards, total, quotaPerUnit };
          presentation?.progress(rewards.length, total);
        },
      });
      if (!sameUser()) return;
      pulseMessage = '本次抽奖已完成。';
      await presentation?.reveal(pulseSessionDisplay(pulseLastResult));
    } catch (error) {
      if (!sameUser()) return;
      const partial = pulseLastResult?.userId === userId && pulseLastResult.rewards.length > 0;
      if (['action_rejected', 'selection_changed', 'selection_required'].includes(error.code)) {
        try {
          store.clear();
          pulseMessage = partial ? '连抽已停止，已获得的奖励保留；未完成的次数不再继续。' : error.code.startsWith('selection_') ? '本次未扣券，规则或券状态已变化，请刷新并确认最新概率。' : '本次未扣券，可能是可用券或活动预算不足，请刷新后查看。';
        } catch (_) { pulseMessage = '无法保存本次请求，请允许站点存储后重试'; }
      } else if (error.code === 'storage_unavailable') pulseMessage = '无法保存本次请求，请允许站点存储后重试';
      else pulseMessage = '本轮还有结果未确认，已确认的奖励会保留。请查询结果或继续完成本轮抽奖。';
      if (partial) await presentation?.reveal(pulseSessionDisplay(pulseLastResult));
      else presentation?.fail(t(pulseMessage));
    } finally {
      pulseBusy = false;
      if (currentUserState === 'ready' && route().path === '/pulse') await refreshPulse();
    }
  }

  async function refreshPulse() {
    if (pulseBusy || pulseRefreshing || route().path !== '/pulse') return false;
    if (!pulseView) { await navigate(); return true; }
    const presentation = pulseView;
    const sequence = navigationSequence;
    pulseRefreshing = true;
    clearTimeout(pulseStatusTimer);
    pulseStatusEpoch++;
    presentation.button.disabled = presentation.fiveButton.disabled = presentation.refresh.disabled = true;
    try {
      const html = await pulsePage({deferHistory:true});
      if (sequence !== navigationSequence || presentation !== pulseView) return;
      const template = document.createElement('template');
      template.innerHTML = html;
      const next = template.content.querySelector('[data-pulse-details]');
      const previous = document.querySelector('[data-pulse-details]');
      if (!next || !previous) { await navigate(); return; }

      // Keep the native dialogs mounted so background refresh preserves their
      // open state, focus and scroll position. History updates independently.
      const benefits = template.content.querySelector('[data-pulse-benefits-body]');
      const previousBenefits = document.querySelector('[data-pulse-benefits-body]');
      if (benefits && previousBenefits) previousBenefits.replaceChildren(...benefits.childNodes);
      previous.replaceWith(next);
      presentation.update(pulseCoreState);
      return true;
    } catch (_) {
      if (presentation === pulseView) {
        pulseCoreState = {...pulseCoreState, canDraw:false, canDrawFive:false};
        presentation.status.textContent = t('权益数据刷新失败，已显示的结果会保留，请刷新权益。');
        presentation.refresh.disabled = false;
        // Re-read authoritative eligibility before allowing another ticket spend.
      }
    } finally {
      pulseRefreshing = false;
      // Eligibility is ready independently of the history/settlement service.
      // This read may remain slow or fail without holding either draw button.
      if (sequence === navigationSequence && presentation === pulseView && !pulseCoreState?.unbound) void refreshPulseStatus();
    }
  }

  // Status reads never hold the draw button or wait for settlement. They stop
  // after a bounded window, on navigation and while the document is hidden.
  function schedulePulseStatus() {
    clearTimeout(pulseStatusTimer);
    if (route().path !== '/pulse' || currentUserState !== 'ready' || !isActiveUser(currentUser) || !pulseHasPendingRewards || document.hidden || pulseStatusAttempts >= 12) return;
    pulseStatusTimer = setTimeout(refreshPulseStatus, pulseStatusAttempts < 5 ? 2000 : 5000);
  }

  async function refreshPulseStatus() {
    if (pulseBusy || pulseRefreshing) { schedulePulseStatus(); return; }
    const sequence = navigationSequence, epoch = pulseStatusEpoch, userId = currentUser?.id;
    pulseStatusAttempts++;
    try {
      const history = await pulse.rewards();
      if (sequence !== navigationSequence || epoch !== pulseStatusEpoch || userId !== currentUser?.id || pulseBusy || pulseRefreshing) return;
      if (!Array.isArray(history.rewards)) throw new AdapterError('unconfirmed history', {code:'pulse_unavailable'});
      const rewards = history.rewards;
      const rows = document.querySelector('[data-pulse-history-rows]');
      if (rows) rows.innerHTML = pulseHistoryRows(rewards, pulseCoreState?.quotaPerUnit);
      const recent = document.querySelector('[data-pulse-recent-rows]');
      if (recent) recent.innerHTML = pulseRecentRows(rewards, pulseCoreState?.quotaPerUnit);
      document.querySelectorAll('[data-pulse-history-error]').forEach(historyError => { historyError.hidden = true; historyError.textContent = ''; });
      const last = pulseLastResult?.userId === userId ? pulseLastResult : null;
      if (last) {
        last.rewards = last.rewards.map(reward => rewards.find(item => item.grant_id === reward.grant_id && validPulseResult(item, reward.action_id)) || reward);
        // Delivery updates belong to history; leave the draw presentation untouched.
      }
      const byGrant = new Map(rewards.map(reward => [reward.grant_id, reward]));
      document.querySelectorAll('[data-pulse-reward-status]').forEach(node => {
        const reward = byGrant.get(node.dataset.pulseRewardStatus);
        if (reward) node.textContent = t(pulseRewardStates[reward.status] || '等待核对');
      });
      pulseHasPendingRewards = rewards.some(pulseRewardPending) || Boolean(last?.rewards.some(pulseRewardPending));
    } catch (_) {
      // Retain the last confirmed rows; do not turn a failed read into an empty
      // history or block another draw whose rules have already been checked.
      if (sequence === navigationSequence && epoch === pulseStatusEpoch && userId === currentUser?.id) {
        document.querySelectorAll('[data-pulse-history-error]').forEach(historyError => { historyError.hidden = false; historyError.textContent = t('到账状态暂时无法更新，已确认的奖励保留，不影响继续抽奖。'); });
      }
    }
    finally { if (sequence === navigationSequence && epoch === pulseStatusEpoch) schedulePulseStatus(); }
  }

  document.addEventListener('visibilitychange', () => {
    if (!document.hidden) pulseStatusAttempts = 0;
    schedulePulseStatus();
  });

  function closeAccountMenu(restoreFocus = false) {
    const menu = document.querySelector('.account-menu[open]');
    if (!menu) return;
    menu.open = false;
    if (restoreFocus) menu.querySelector('summary')?.focus();
  }

  function closeToolsMenu(restoreFocus = false) {
    const menu = document.querySelector('.topnav-menu[open]');
    if (!menu) return;
    menu.open = false;
    if (restoreFocus) menu.querySelector('summary')?.focus();
  }

  async function refreshAccountExtras() {
    const user = currentUser;
    if (!user || currentUserState !== 'ready' || Date.now() - extrasUpdatedAt < 30000) return;
    extrasUpdatedAt = Date.now();
    const levelNode = document.querySelector('[data-account-level]');
    if (levelNode && user.username) {
      answer.request(`/metar/experience/profile?username=${encodeURIComponent(user.username)}`).then((profile) => {
        const level = profile?.level?.number;
        if (levelNode.isConnected && currentUser === user && Number.isInteger(level) && level >= 0 && level <= 8) levelNode.textContent = `Lv.${level}`;
      }).catch(() => {});
    }
    const unreadNode = document.querySelector('[data-account-unread]');
    if (unreadNode && isActiveUser(user)) {
      answer.request('/notification/status').then((status) => {
        if (!unreadNode.isConnected || currentUser !== user) return;
        unreadNode.hidden = !(Number(status?.inbox) > 0);
        unreadNode.parentElement.setAttribute('aria-label', Number(status?.inbox) > 0 ? t('通知中心（有未读消息）') : t('通知中心'));
      }).catch(() => {});
    }
  }

  async function refreshChatUnread() {
    clearTimeout(chatUnreadTimer);
    if (chatUnreadBusy || document.hidden || currentUserState !== 'ready' || !isActiveUser(currentUser)) return;
    const user = currentUser, token = answer.token();
    chatUnreadBusy = true;
    try {
      const data = await answer.request('/metar/chat/summary');
      if (currentUser !== user || answer.token() !== token) return;
      document.querySelectorAll('[data-chat-unread]').forEach(node => {
        const count = Number(data.unread) + Number(data.invites);
        node.hidden = !count; node.textContent = count > 99 ? '99+' : String(count);
      });
    } catch (_) { /* Chat availability never blocks community navigation. */ }
    finally { chatUnreadBusy = false; if (!document.hidden) chatUnreadTimer = setTimeout(refreshChatUnread, 30000); }
  }
  document.addEventListener('visibilitychange', () => { clearTimeout(chatUnreadTimer); if (!document.hidden) refreshChatUnread(); });
  window.addEventListener('pagehide', () => { clearTimeout(chatUnreadTimer); chatView?.dispose(); });

  function closeMobileMenu() {
    window.MetarSidebar?.closeMobile();
  }

  function refreshMessage(failed = false, switching = false) {
    document.getElementById('view-refresh-message')?.remove();
    const message = document.createElement('div');
    message.id = 'view-refresh-message';
    message.className = 'view-refresh-status';
    message.setAttribute('role', 'status');
    message.innerHTML = failed
      ? `<span>${switching ? t('切换失败，仍显示原来的列表。') : t('暂时无法更新，正在显示返回前的内容。')}</span> <button class="btn small" type="button" data-action="retry">${switching ? t('重试切换') : t('重新加载')}</button>`
      : switching ? t('正在加载新列表…') : t('正在更新内容…');
    document.getElementById('toast-root').append(message);
  }

  async function navigate({restore = null, preservePublicView = false} = {}) {
    const destination = nativeDestination(location, config);
    if (destination) { location.replace(destination); return; }
    if (!isHomePath(location.pathname) && !location.pathname.startsWith('/topic/')) tagPanelOpen = false;
    personalNativeView?.dispose();
    personalNativeView = null;
    personalLeaveApproved = false;
    chatView?.dispose();
    memberView?.dispose();
    marketplaceView?.dispose();
    modelsView?.dispose();
    modelsAdminView?.dispose();
    publishingView?.dispose();
    toolsView?.dispose();
    const sequence = ++navigationSequence;
    const key = location.pathname + location.search;
    const session = answer.token();
    const restorePublic = preservePublicView && shellReady && window.MetarLoading.canRetain(route().path, key, renderedKey, getLanguage(), renderedLanguage, session === renderedSession);
    const switchingLists = !restorePublic && shellReady && window.MetarLoading.canSwitchList(key, renderedKey, getLanguage(), renderedLanguage, session === renderedSession);
    const retain = restorePublic || switchingLists;
    clearTimeout(navigationNoticeTimer);
    pendingListTransition = switchingLists ? {sequence, source:renderedKey, target:key} : null;
    document.getElementById('view-refresh-message')?.remove();
    if (!retain) renderedKey = null;
    clearTimeout(pulseStatusTimer);
    pulseStatusAttempts = 0;
    pulseHasPendingRewards = false;
    closeTaxonomy(false);
    closeMobileMenu();
    pulseView?.dispose();
    pulseView = null;
    pulseFeedView?.dispose();
    pulseFeedView = null;
    if (route().path !== '/admin/pulse') pulseAdmin?.dispose();
    renderShell({retain});
    if (switchingLists) {
      navigationNoticeTimer = setTimeout(() => { if (sequence === navigationSequence) refreshMessage(false, true); }, 180);
    } else if (retain) refreshMessage();
    else window.scrollTo({top:0, left:0, behavior:'auto'});
    try {
      const html = await resolveView();
      if (sequence !== navigationSequence) return;
      clearTimeout(navigationNoticeTimer);
      pendingListTransition = null;
      renderView(html, {focus:!retain || switchingLists});
      if (switchingLists) window.scrollTo({top:restore?.y || 0, left:restore?.x || 0, behavior:'auto'});
      renderedKey = key;
      renderedLanguage = getLanguage();
      renderedSession = session;
      document.getElementById('view-refresh-message')?.remove();
    } catch (error) {
      console.error('METAR view load failed', error);
      if (sequence !== navigationSequence) return;
      clearTimeout(navigationNoticeTimer);
      if (retain && ![401, 403].includes(error?.status)) {
        app.setAttribute('aria-busy', 'false');
        refreshMessage(true, switchingLists);
      } else {
        pendingListTransition = null;
        renderedKey = null;
        renderError(error);
      }
    }
    if (restore && !retain) window.scrollTo({top:restore.y, left:restore.x, behavior:'auto'});
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

  function initializeIdentity() {
    closeTaxonomy(false);
    navigationCatalog = null; navigationCatalogScope = null;
    document.querySelectorAll('[data-taxonomy-inline]').forEach(node => updateTaxonomyInline(node, taxonomy.inlineItems(null, node.dataset.taxonomyInline)));
    const sequence = ++identitySequence;
    currentUserState = 'loading'; currentUser = null; identityError = null;
    document.querySelectorAll('[data-chat-unread]').forEach(node => { node.hidden = true; node.textContent = ''; });
    answer.clearContentCache();
    identityReady = loadIdentitySnapshot(() => answer.getCurrentUser()).then(snapshot => {
      if (sequence !== identitySequence) return;
      currentUser = snapshot.user; currentUserState = snapshot.state; identityError = snapshot.error;
      if (identityError) console.warn('METAR identity bootstrap failed', identityError);
      refreshChrome();
    });
    return identityReady;
  }

  document.addEventListener('change', (event) => {
    if (event.target.matches?.('[data-action="taxonomy-filter"]')) {
      const {query} = route();
      if (event.target.value === '@directory') {
        if (router.go(event.target.dataset.kind === 'tags' ? '/topics?view=tags' : '/topics')) navigate();
        return;
      }
      const base = event.target.value ? taxonomy.topicURL(event.target.value) : '/latest';
      const order = query.get('order') || 'active';
      if (router.go(`${base}?${new URLSearchParams({order})}`)) navigate();
      return;
    }
    if (!event.target.matches?.('[data-action="language"]')) return;
    if (!setLanguage(event.target.value)) return;
    navigate().then(() => document.querySelector('[data-action="language"]')?.focus());
  });

  document.addEventListener('submit', (event) => {
    const orderForm = event.target.closest('[data-taxonomy-order]');
    if (orderForm) {
      event.preventDefault();
      if (isCommunityAdministrator(currentUser)) taxonomy.saveOrder(answer,new AnswerAdapter({answerApiBase:'/answer/admin/api'}),orderForm).then(()=>enhanceTaxonomy());
      return;
    }

    const taxonomyForm = event.target.closest('form[data-form="taxonomy-init"]');
    if (taxonomyForm) {
      event.preventDefault();
      if (taxonomyBusy || !isCommunityAdministrator(currentUser) || !taxonomyForm.reportValidity()) return;
      taxonomyBusy = true;
      const button = taxonomyForm.querySelector('button'), status = taxonomyForm.querySelector('[data-taxonomy-status]');
      button.disabled = true;
      taxonomy.initialize(answer, new AnswerAdapter({answerApiBase:'/answer/admin/api'}), name => {
        status.textContent = t('正在核对：{name}', {name});
      }).then(() => { status.textContent = t('类别与标签已保存并重新读取核实。'); }).catch(error => {
        status.textContent = `${errorMessage(error)} ${t('已完成的条目会保留；重新初始化会先查询现状，不会覆盖已有介绍。')}`;
      }).finally(() => { taxonomyBusy = false; button.disabled = false; });
      return;
    }
    const chatReview = event.target.closest('[data-chat-review]');
    if (chatReview) {
      event.preventDefault();
      if (chatReview.dataset.busy === '1') return;
      chatReview.dataset.busy = '1';
      chatReview.querySelectorAll('button').forEach(b => b.disabled = true);
      chatModeration?.submit(chatReview, event.submitter?.value).catch(error => {
        chatReview.querySelector('[role="status"]').textContent = window.MetarChat.errorText(error);
      }).finally(() => { chatReview.dataset.busy = ''; chatReview.querySelectorAll('button').forEach(b => b.disabled = false); });
      return;
    }
    const growthForm = event.target.closest('form[data-growth-form]');
    if (growthForm) { event.preventDefault(); growth?.submit(growthForm, navigate); return; }
    const periodForm = event.target.closest('form[data-form="admin-period"]');
    if (periodForm) { event.preventDefault(); pulseAdmin?.periods?.submit(periodForm); return; }
    const adminForm = event.target.closest('form[data-form="admin-pulse"]');
    if (adminForm) { event.preventDefault(); pulseAdmin?.submit(adminForm); return; }
    const form = event.target.closest('form[data-form="search"]');
    if (!form) return;
    event.preventDefault();
    const query = new FormData(form).get('q')?.toString().trim() || '';
    if (query) { answer.clearContentCache(); const destination = `/search?q=${encodeURIComponent(query)}`; if (router.go(destination) || location.pathname + location.search === destination) navigate(); }
  });

  document.addEventListener('click', (event) => {
    if (!event.target.closest('.account-menu')) closeAccountMenu();
    else if (event.target.closest('a')) closeAccountMenu();
    if (!event.target.closest('.topnav-menu') || event.target.closest('.topnav-menu a')) closeToolsMenu();
    const leaving = event.target.closest('a[href]');
    if (personalNativeView && leaving && event.button === 0 && !event.ctrlKey && !event.metaKey && !event.shiftKey && !event.altKey && !leaving.target && !leaving.hasAttribute('download') && !leaving.getAttribute('href').startsWith('#') && !personalNativeView.confirmLeave()) { event.preventDefault(); return; }
    if (router.follow(event)) { navigate(); return; }
    if (event.defaultPrevented) { closeMobileMenu(); return; }
    const growthButton = event.target.closest('[data-growth]');
    if (growthButton?.dataset.growth) { event.preventDefault(); growth?.click(growthButton, navigate); return; }
    const followButton = event.target.closest('[data-tag-follow]');
    if (followButton) { event.preventDefault(); toggleTagFollow(followButton); return; }
    const target = event.target.closest('[data-action]');
    const action = target?.dataset.action;
    if (!action) return;
    if (action.startsWith('admin-')) { pulseAdmin?.action(action, target); return; }
    if (action === 'toggle-tags') { event.preventDefault(); toggleTagPanel(); return; }
    if (action === 'skip') {
      event.preventDefault();
      document.getElementById('main')?.focus({ preventScroll: false });
    }
    if (action === 'taxonomy-inline-retry') { answer.clearContentCache(); enhanceTaxonomy(); }
    if (action === 'taxonomy-open') openTaxonomy(target.dataset.kind, target);
    if (action === 'taxonomy-close') closeTaxonomy();
    if (action === 'taxonomy-retry' && taxonomyDrawer) {
      const {kind,trigger} = taxonomyDrawer;
      answer.clearContentCache();
      openTaxonomy(kind,trigger);
    }
    if (action === 'menu') {
      closeTaxonomy(false);
    }
    if (action === 'back') { goBack(); return; }
    if (action === 'theme') setTheme(document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark');
    if (action === 'retry') { answer.clearContentCache(); navigate({preservePublicView:true}); }
    if (action === 'pulse-draw') submitPulse();
    if (action === 'pulse-draw-five') submitPulse(false, 5);
    if (action === 'pulse-resume') submitPulse(true);
    if (action === 'pulse-refresh') refreshPulse();
    if (action === 'pulse-benefits' || action === 'pulse-history' || action === 'pulse-winners') {
      document.getElementById(action)?.showModal();
    }
    if (action === 'retry-identity') { initializeIdentity(); refreshChrome(); navigate(); }
  });

  document.addEventListener('input', (event) => {
    const input = event.target.closest('[data-tag-filter]');
    if (!input) return;
    const groups = document.querySelector('[data-tag-groups]');
    if (groups) groups.innerHTML = tagGroupsMarkup(input.value);
  });

  document.addEventListener('keydown', (event) => {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
      event.preventDefault();
      const search = [...document.querySelectorAll('form[role="search"] input')].find(input => input.getClientRects().length);
      search?.focus();
    }
    if (event.key === 'Escape' && taxonomyDrawer) return;
    if (event.key === 'Escape') { closeMobileMenu(); closeAccountMenu(true); closeToolsMenu(true); }
  });

  document.addEventListener('focusin', (event) => {
    if (!event.target.closest('.account-menu')) closeAccountMenu();
    if (!event.target.closest('.topnav-menu')) closeToolsMenu();
  });

  window.addEventListener('popstate', () => {
    if (restoringPersonalHistory) { restoringPersonalHistory = false; return; }
    if (personalNativeView && !personalLeaveApproved && !personalNativeView.confirmLeave()) {
      const delta = personalNativeStep - (history.state?.metarReturnV2?.step || 0);
      if (delta) { restoringPersonalHistory = true; history.go(delta); }
      return;
    }
    return navigate({restore:router.position(), preservePublicView:true});
  });
  window.addEventListener('pagehide', () => router.checkpoint());
  window.addEventListener('focus', refreshAccountExtras);
  window.addEventListener('storage', event => {
    if (event.key === '_a_ltk_' || event.key === null) { initializeIdentity(); refreshChrome(); navigate(); }
  });
  window.addEventListener('pageshow', event => {
    if (event.persisted) { const restore = router.position(); initializeIdentity(); refreshChrome(); navigate({restore, preservePublicView:true}); }
  });
  window.addEventListener('hashchange', () => { if (router.migrate()) navigate(); });

  installAvatars();
  initializeTheme();
  if (nativeDestination(location, config)) navigate();
  else { initializeIdentity(); navigate({restore:router.position()}); }
})();
