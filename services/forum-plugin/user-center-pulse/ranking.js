export const rankingKinds = ['reputation','level','posts','answers'];
export function buildActivityRanking(payload, staffs = []) {
  if (!payload || !rankingKinds.includes(payload.kind) || payload.kind === 'reputation' || !Array.isArray(payload.users)) throw new Error('ranking');
  const excluded = new Set(staffs.map(user => user.username));
  const seen = new Set();
  const users = payload.users.filter(user => !excluded.has(user.username)).slice(0,100).map((user,index) => {
    if (!user || typeof user.username !== 'string' || !user.username || seen.has(user.username) || !/^[1-9][0-9]*$/.test(String(user.value)) || (payload.kind==='level' && (!Number.isInteger(user.level) || user.level<0 || user.level>8))) throw new Error('ranking');
    seen.add(user.username);
    return {...user, position:index+1};
  });
  return {users,staffs};
}
export function rankingValue(user, kind) {
  if (kind === 'reputation') return (Number(user.rank)>0?'+':'') + (Number(user.rank)||0);
  const amount = String(user.value);
  return kind === 'level' ? 'Lv.' + user.level + ' · ' + amount + ' EXP' : amount;
}
export function buildRanking(data = {}) {
  const staffs = data.staffs || [];
  const excluded = new Set(staffs.map(user => user.username));
  const users = (data.users_with_the_most_reputation || [])
    .filter(user => !excluded.has(user.username) && ![2, 3].includes(Number(user.role_id)))
    .slice(0, 100)
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

export function createRankingCache(host, onChange, now = () => Date.now()) {
  const records = new Map(rankingKinds.map(kind => [kind,{data:null,error:false,loading:false,at:0}]));
  let pending = null, epoch = 0, controller, attemptedAt = -Infinity;
  function clear() {
    epoch++; controller?.abort(); pending=null; attemptedAt=-Infinity;
    for(const record of records.values()) Object.assign(record,{data:null,error:false,loading:false,at:0});
  }
  function load(force = false) {
    if(pending) return pending;
    if(!force && now()-attemptedAt<60000) return Promise.resolve();
    attemptedAt=now();const current=++epoch;controller=new AbortController();const active=controller;
    const timeout=host.setTimeout(()=>active.abort(),10000);
    const read=async path=>{const response=await host.fetch(path,{credentials:'same-origin',cache:'no-store',signal:active.signal});if(!response.ok)throw new Error('ranking');return (await response.json()).data;};
    for(const [kind,record] of records){record.loading=true;record.error=false;if(!record.data)onChange(kind);}
    const staff=read('/answer/api/v1/user/ranking').then(data=>{if(!data||!Array.isArray(data.staffs)||!Array.isArray(data.users_with_the_most_reputation))throw new Error('ranking');return data;});
    const jobs=rankingKinds.map(async kind=>{
      const record=records.get(kind);
      try {
        let result;
        if(kind==='reputation')result=buildRanking(await staff);
        else {
          // Fetch every board immediately; staff lookup must not serialize the requests.
          const [data,base]=await Promise.all([read('/answer/api/v1/metar/ranking?kind='+kind),staff.catch(()=>null)]);
          if(data?.kind!==kind)throw new Error('ranking');
          result=buildActivityRanking(data,base?.staffs||records.get('reputation').data?.staffs||[]);
        }
        if(current===epoch){record.data=result;record.at=now();}
      }catch(_){if(current===epoch)record.error=true;}
      finally{if(current===epoch){record.loading=false;onChange(kind);}}
    });
    pending=Promise.all(jobs).finally(()=>{host.clearTimeout(timeout);if(current===epoch)pending=null;});
    return pending;
  }
  return {get:kind=>records.get(kind),load,clear};
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
  let root, data, error = false, loading = false, page = 1, size = 100, active = false, kind = 'reputation';
  const cache = createRankingCache(host, changed => { if (active && changed === kind) render(); });
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
    const record = cache.get(kind);
    data=record.data;loading=record.loading;error=record.error;
    root.replaceChildren();
    const staff = el('aside', undefined, 'metar-ranking-staff');
    staff.append(el('h2', t('我们的社区工作人员', 'Our community staff')));
    staff.append(el('p', t('工作人员不参与排名', 'Staff do not participate in rankings')));
    (data?.staffs || []).forEach(user => staff.append(person(user)));
    const main = el('section', undefined, 'metar-ranking-main');
    main.append(el('div', t('社区排行榜', 'Community leaderboard'), 'metar-ranking-eyebrow'));
    const titles = {reputation:t('近 7 天声望增长','Reputation gained in 7 days'),level:t('社区等级榜','Community levels'),posts:t('累计发帖榜','Total posts'),answers:t('累计回答榜','Total answers')};
    const labels = {reputation:t('声望榜','Reputation'),level:t('等级榜','Levels'),posts:t('发帖榜','Posts'),answers:t('回答榜','Answers')};
    main.append(el('h1', titles[kind]));
    const tabs = el('nav',undefined,'metar-ranking-tabs');tabs.setAttribute('aria-label',t('切换排行榜','Choose leaderboard'));
    rankingKinds.forEach(value=>{const button=el('button',labels[value]);button.type='button';button.setAttribute('aria-pressed',String(kind===value));button.onclick=()=>{if(kind===value)return;kind=value;page=1;const url=new URL(host.location.href);url.searchParams.set('board',kind);host.history.replaceState(host.history.state,'',url.pathname+url.search);render();};tabs.append(button);});main.append(tabs);
    const notes={reputation:t('近7天声望正增长前100名。','Top 100 positive reputation gains in 7 days.'),level:t('按累计社区经验排序，同等级按经验高低排列。','Ordered by community EXP, including within the same level.'),posts:t('累计公开有效帖子，不计删除、待审核和隐藏内容。','Published visible posts; deleted, pending and hidden content excluded.'),answers:t('累计公开有效回答，不含帖子评论或楼中楼评论。','Published answers on visible posts; comments and replies excluded.')};
    main.append(el('p',notes[kind]+' '+t('工作人员不参与排名，不足100人时显示实际人数。','Staff excluded; fewer shown when fewer qualify.'),'metar-ranking-note'));
    root.append(staff, main);
    if (!data) {
      const status = el('p', error ? t('排行榜加载失败，请重试。', 'Could not load the leaderboard. Please retry.') : t('正在更新排行榜…', 'Updating leaderboard…'));
      status.setAttribute('role', 'status'); main.append(status);
      if (error) { const retry = el('button', t('重试', 'Retry')); retry.type = 'button'; retry.onclick = () => refresh(true); main.append(retry); }
      return;
    }
    if(error){const notice=el('p',t('刷新失败，暂时显示上次加载的榜单。','Refresh failed. Showing the last loaded ranking.'),'metar-ranking-note');notice.setAttribute('role','status');const retry=el('button',t('重试','Retry'));retry.type='button';retry.onclick=()=>refresh(true);main.append(notice,retry);}
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
    [t('名次', 'Position'), t('用户', 'Member'), (kind==='level'?t('等级 / 经验','Level / EXP'):kind==='posts'?t('帖子数','Posts'):kind==='answers'?t('回答数','Answers'):t('声望增长','Reputation gained'))].forEach(text => { const th = el('th', text); th.scope = 'col'; tr.append(th); });
    head.append(tr); table.append(head);
    const body = el('tbody');
    result.items.forEach(user => {
      const row = el('tr'); const name = el('td'); name.append(person(user));
      row.append(el('td', String(user.position), 'metar-ranking-position'), name, el('td', rankingValue(user,kind), 'metar-ranking-growth'));
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
  function refresh(force = false) {
    if(!isRanking() || !active) return;
    return cache.load(force === true);
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
      if(active)cache.clear();active = false;loading=false;root?.remove(); root = null;
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
    const requested = new URL(host.location.href).searchParams.get('board') || 'reputation';
    const selected = rankingKinds.includes(requested)?requested:'reputation';
    if (!active) { active = true; kind=selected; page=1; render();void refresh(); }
    else if(selected!==kind){kind=selected;page=1;render();}
  }
  let pending = false;
  const schedule = () => {
    if (pending) return;
    pending = true; host.requestAnimationFrame(() => { pending = false; mount(); });
  };
  new host.MutationObserver(schedule).observe(doc.documentElement, { childList: true, subtree: true });
  host.addEventListener('popstate', schedule);
  host.addEventListener('focus', () => refresh());
  doc.addEventListener('visibilitychange', () => { if (doc.visibilityState === 'visible') refresh(); });
  let identity = options.getUser?.()?.username || '';
  options.subscribeUser?.(() => {const next=options.getUser?.()?.username || '';if(next!==identity){identity=next;cache.clear();if(active)void refresh();}render();});
  options.onLanguageChanged?.(() => { render(); schedule(); });
  mount();
}
