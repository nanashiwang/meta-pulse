export function buildRanking(data = {}) {
  const staffs = data.staffs || [];
  const excluded = new Set(staffs.map(user => user.username));
  const users = (data.users_with_the_most_reputation || []).slice(0, 20)
    .filter(user => !excluded.has(user.username) && ![2, 3].includes(Number(user.role_id)))
    .map((user, index) => ({ ...user, position: index + 1 }));
  return { users, staffs };
}

export function paginate(users, page = 1, size = 20) {
  size = [10, 20, 50, 100].includes(Number(size)) ? Number(size) : 20;
  const pages = Math.max(1, Math.ceil(users.length / size));
  page = Math.max(1, Math.min(pages, Number(page) || 1));
  return { items: users.slice((page - 1) * size, page * size), page, pages };
}

export function ownPosition(data, user) {
  if (!user?.username) return { state: 'guest' };
  if ([2, 3].includes(Number(user.role_id)) || data.staffs.some(item => item.username === user.username)) return { state: 'staff' };
  const found = data.users.find(item => item.username === user.username);
  return found ? { state: 'listed', position: found.position } : { state: 'absent' };
}

export function installRanking(options, host = window) {
  if (host.__metarRanking) return;
  host.__metarRanking = true;
  const doc = host.document;
  const el = (tag, text, className) => {
    const node = doc.createElement(tag);
    if (text !== undefined) node.textContent = text;
    if (className) node.className = className;
    return node;
  };
  const english = () => options.getLanguage?.() === 'en_US' || options.getLanguage?.() === 'en';
  const t = (zh, en) => english() ? en : zh;
  let root, data, error = false, loading = false, page = 1, size = 20, active = false;
  const isRanking = () => /^\/users\/?$/.test(host.location.pathname);
  function person(user) {
    const link = el('a', undefined, 'metar-ranking-person');
    link.href = `/users/${encodeURIComponent(user.username)}`;
    const avatar = user.avatar;
    const source = typeof avatar === 'string' ? avatar : avatar?.[avatar?.type];
    if (source && /^(https?:\/\/|\/)/.test(source)) {
      const image = el('img'); image.src = source; image.alt = ''; image.loading = 'lazy'; link.append(image);
    } else {
      link.append(el('span', (user.display_name || user.username || '?').slice(0, 1), 'metar-ranking-avatar'));
    }
    link.append(el('span', user.display_name || user.username));
    return link;
  }
  function render() {
    if (!root?.isConnected) return;
    root.replaceChildren();
    const staff = el('aside', undefined, 'metar-ranking-staff');
    staff.append(el('h2', t('我们的社区工作人员', 'Our community staff')));
    staff.append(el('p', t('工作人员不参与排名', 'Staff do not participate in rankings')));
    (data?.staffs || []).forEach(user => staff.append(person(user)));
    const main = el('section', undefined, 'metar-ranking-main');
    main.append(el('div', t('社区排行榜', 'Community leaderboard'), 'metar-ranking-eyebrow'));
    main.append(el('h1', t('近 7 天声望增长', 'Reputation gained in 7 days')));
    main.append(el('p', t('展示接口近7天声望增长前20名中的非工作人员。名次为本期展示名单内位置。', 'Non-staff members from the API’s top 20 for the past 7 days. Positions refer to this displayed list.'), 'metar-ranking-note'));
    root.append(staff, main);
    if (loading || error || !data) {
      const status = el('p', error ? t('排行榜加载失败，请重试。', 'Could not load the leaderboard. Please retry.') : t('正在更新排行榜…', 'Updating leaderboard…'));
      status.setAttribute('role', 'status'); main.append(status);
      if (error) { const retry = el('button', t('重试', 'Retry')); retry.type = 'button'; retry.onclick = refresh; main.append(retry); }
      return;
    }
    const own = ownPosition(data, options.getUser?.());
    const message = own.state === 'listed' ? t(`我的本期榜内位置：第 ${own.position} 名`, `My position this period: ${own.position}`)
      : own.state === 'staff' ? t('我的排名：工作人员不参与排名', 'My position: staff do not participate')
      : own.state === 'guest' ? t('登录后查看我的本期榜内位置', 'Sign in to see your position this period')
      : t('我的排名：未进入本期展示名单', 'My position: not in this period’s displayed list');
    const ownNode = el('div', undefined, 'metar-ranking-own'); ownNode.setAttribute('role', 'status');
    const ownText = el(own.state === 'guest' ? 'a' : 'span', message);
    if (own.state === 'guest') ownText.href = '/users/login';
    ownNode.append(ownText); main.append(ownNode);
    const result = paginate(data.users, page, size); page = result.page;
    const table = el('table', undefined, 'metar-ranking-table');
    const head = el('thead'); const tr = el('tr');
    [t('名次', 'Position'), t('用户', 'Member'), t('声望增长', 'Reputation gained')].forEach(text => { const th = el('th', text); th.scope = 'col'; tr.append(th); });
    head.append(tr); table.append(head);
    const body = el('tbody');
    result.items.forEach(user => {
      const row = el('tr'); const name = el('td'); name.append(person(user));
      row.append(el('td', String(user.position), 'metar-ranking-position'), name, el('td', `${Number(user.rank) > 0 ? '+' : ''}${Number(user.rank) || 0}`, 'metar-ranking-growth'));
      body.append(row);
    });
    table.append(body); main.append(table);
    if (!data.users.length) main.append(el('p', t('本期暂无可展示用户', 'No members to display this period')));
    const controls = el('div', undefined, 'metar-ranking-pagination');
    const label = el('label', t('每页 ', 'Per page ')); const select = el('select');
    [10, 20, 50, 100].forEach(value => { const option = el('option', String(value)); option.value = String(value); select.append(option); });
    select.value = String(size); select.onchange = () => { size = Number(select.value); page = 1; render(); }; label.append(select);
    controls.append(label, el('span', t(`共 ${data.users.length} 人 · ${page} / ${result.pages} 页`, `${data.users.length} members · ${page} / ${result.pages}`)));
    const prev = el('button', t('上一页', 'Previous')); prev.type = 'button'; prev.disabled = page === 1; prev.onclick = () => { page--; render(); };
    const next = el('button', t('下一页', 'Next')); next.type = 'button'; next.disabled = page === result.pages; next.onclick = () => { page++; render(); };
    controls.append(prev, next); main.append(controls);
  }
  async function refresh() {
    if (loading || !isRanking()) return;
    loading = true; error = false; render();
    try {
      const response = await host.fetch('/answer/api/v1/user/ranking', { credentials: 'same-origin', cache: 'no-store' });
      if (!response.ok) throw new Error('ranking');
      const payload = await response.json();
      if (!payload.data || !Array.isArray(payload.data.users_with_the_most_reputation) || !Array.isArray(payload.data.staffs)) throw new Error('ranking');
      data = buildRanking(payload.data);
    } catch { error = true; }
    finally { loading = false; render(); }
  }
  function mount() {
    const inbox = doc.querySelector('#header a[href="/users/notifications/inbox"]');
    const header = doc.querySelector('#header');
    if (header && !header.querySelector('.metar-ranking-link')) {
      const link = el('a', undefined, 'metar-ranking-link icon-link nav-link d-flex align-items-center justify-content-center p-0 me-2');
      link.href = '/users'; link.append(el('i', undefined, 'br bi-bar-chart-fill fs-4'));
      if (inbox) inbox.before(link); else header.querySelector('.navbar-brand')?.after(link);
    }
    const link = header?.querySelector('.metar-ranking-link');
    if (link) { link.title = t('排行榜', 'Leaderboard'); link.setAttribute('aria-label', link.title); }
    if (!isRanking()) {
      active = false; root?.remove(); root = null;
      doc.querySelector('.metar-ranking-container')?.classList.remove('metar-ranking-container');
      doc.querySelectorAll('[data-metar-ranking-original]').forEach(node => node.removeAttribute('data-metar-ranking-original'));
      return;
    }
    const container = doc.querySelector('.answer-container.main-mx-with');
    if (!container) return;
    container.classList.add('metar-ranking-container');
    if (!root?.isConnected) {
      root = el('div', undefined, 'metar-ranking'); root.id = 'metar-ranking';
      Array.from(container.children).forEach(node => node.setAttribute('data-metar-ranking-original', ''));
      container.append(root); render();
    }
    // 上游榜单可能在自定义页面挂载后才异步插入。
    Array.from(container.children).forEach(node => {
      if (node !== root) node.setAttribute('data-metar-ranking-original', '');
    });
    if (!active) { active = true; page = 1; refresh(); }
  }
  let pending = false;
  const schedule = () => {
    if (pending) return;
    pending = true; host.requestAnimationFrame(() => { pending = false; mount(); });
  };
  new host.MutationObserver(schedule).observe(doc.documentElement, { childList: true, subtree: true });
  host.addEventListener('popstate', schedule);
  host.addEventListener('focus', refresh);
  doc.addEventListener('visibilitychange', () => { if (doc.visibilityState === 'visible') refresh(); });
  options.subscribeUser?.(() => render());
  options.onLanguageChanged?.(() => { render(); schedule(); });
  mount();
}
