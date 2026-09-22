// Add community destinations to Answer's avatar menu. Native logout stays intact.
export function accountLinks(user, english = false, canReview = false) {
  const item = (href, zh, en, icon) => ({ href, label: english ? en : zh, icon });
  const groups = [{ label: english ? 'My space' : '我的空间', items: [
    item('/me', '个人空间', 'My space', 'person'),
    item('/me/growth', '社区成长', 'Community growth', 'stars'),
    item('/me/bookmarks', '我的收藏', 'Bookmarks', 'bookmark'),
    item('/settings/binding', '账号绑定', 'Account binding', 'link-45deg'),
    item('/users/settings/profile', '账号设置', 'Account settings', 'gear'),
  ] }];
  const management = [];
  if (user?.role_id === 2 && user.mail_status === 1 && user.status === 'normal') {
    management.push(item('/admin/pulse', 'Pulse 配置', 'Pulse settings', 'sliders'));
    management.push(item('/admin/growth', '成长管理', 'Growth settings', 'stars'));
    management.push(item('/admin/dashboard', '社区管理', 'Community admin', 'gear'));
    management.push(item('/admin/pulse_user_center', '社区连接配置', 'Community connections', 'link-45deg'));
  }
  if (canReview) management.push(item('/review', '审查', 'Review', 'shield-check'));
  if (management.length) groups.push({ label: english ? 'Management' : '管理', items: management });
  return groups;
}

export function installAccountMenu(state, host = window) {
  if (host.__metarAccountMenu) return;
  host.__metarAccountMenu = true;
  const doc = host.document;
  function mount() {
    const user = state.getUser();
    const trigger = doc.querySelector('#header #dropdown-basic');
    const menu = trigger?.parentElement.querySelector('.dropdown-menu');
    if (!user?.username || !trigger) return;
    const english = state.getLanguage() === 'en_US';
    trigger.tabIndex = 0;
    trigger.setAttribute('aria-label', english ? 'Account menu' : '账号菜单');
    if (!menu) return;
    const canReview = Boolean(doc.querySelector('#sideNav a[href="/review"]'));
    const groups = accountLinks(user, english, canReview);
    const signature = JSON.stringify([user.username, user.display_name, groups]);
    if (!menu.classList.contains('metar-account-menu')) menu.classList.add('metar-account-menu');
    let content = menu.querySelector(':scope > .metar-account-content');
    if (content?.dataset.signature === signature) return;
    content?.remove();
    content = doc.createElement('div');
    content.className = 'metar-account-content';
    content.dataset.signature = signature;
    const profile = doc.createElement('div');
    profile.className = 'metar-account-profile';
    const avatar = trigger.querySelector('img');
    if (avatar) {
      const copy = avatar.cloneNode(true);
      copy.removeAttribute('id');
      copy.alt = '';
      profile.append(copy);
    }
    const identity = doc.createElement('div');
    const name = doc.createElement('strong');
    name.textContent = user.display_name || user.username;
    const level = doc.createElement('small');
    identity.append(name, level);
    profile.append(identity);
    content.append(profile);
    for (const group of groups) {
      const section = doc.createElement('div');
      section.className = 'metar-account-group';
      const title = doc.createElement('div');
      title.className = 'metar-account-label';
      title.textContent = group.label;
      section.append(title);
      for (const item of group.items) {
        const link = doc.createElement('a');
        link.className = 'dropdown-item';
        link.setAttribute('data-rr-ui-dropdown-item', '');
        link.tabIndex = 0;
        link.href = item.href;
        const icon = doc.createElement('i');
        icon.className = `bi bi-${item.icon}`;
        icon.setAttribute('aria-hidden', 'true');
        const label = doc.createElement('span');
        label.textContent = item.label;
        link.append(icon, label);
        section.append(link);
      }
      content.append(section);
    }
    menu.prepend(content);
    host.fetch(`/answer/api/v1/metar/experience/profile?username=${encodeURIComponent(user.username)}`, { credentials: 'same-origin' })
      .then((response) => response.ok ? response.json() : null)
      .then((response) => {
        const number = response?.data?.level?.number;
        if (level.isConnected && Number.isInteger(number) && number >= 0 && number <= 8) level.textContent = `Lv.${number}`;
      }).catch(() => {});
  }
  // Answer's anchor toggle lacks a native button's keyboard activation. Keep
  // its click/close behavior, and navigate only visible items (not hidden originals).
  doc.addEventListener('keydown', (event) => {
    const trigger = doc.querySelector('#header #dropdown-basic');
    const menu = trigger?.parentElement.querySelector('.metar-account-menu');
    const onTrigger = event.target === trigger;
    if (!trigger || (!onTrigger && !menu?.contains(event.target))) return;
    const open = trigger.getAttribute('aria-expanded') === 'true';
    if (onTrigger && ['Enter', ' '].includes(event.key)) {
      event.preventDefault(); event.stopPropagation(); trigger.click(); return;
    }
    if (event.key === 'Escape' && open) {
      event.preventDefault(); event.stopPropagation(); trigger.click(); trigger.focus(); return;
    }
    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return;
    event.preventDefault(); event.stopPropagation();
    if (!open) trigger.click();
    host.requestAnimationFrame(() => {
      mount();
      const panel = trigger.parentElement.querySelector('.metar-account-menu');
      const links = [...(panel?.querySelectorAll('a[href]') || [])].filter((link) => link.getClientRects().length);
      if (!links.length) return;
      const index = links.indexOf(doc.activeElement);
      const next = event.key === 'Home' ? 0 : event.key === 'End' ? links.length - 1
        : event.key === 'ArrowDown' ? (index + 1) % links.length : (index < 0 ? links.length - 1 : (index - 1 + links.length) % links.length);
      links[next].focus();
    });
  }, true);
  let pending = false;
  function schedule() {
    if (pending) return;
    pending = true;
    host.requestAnimationFrame(() => { pending = false; mount(); });
  }
  new host.MutationObserver(schedule).observe(doc.documentElement, { childList: true, subtree: true, attributes: true, attributeFilter: ['class'] });
  state.onLanguageChanged(schedule);
  state.subscribeUser(schedule);
  mount();
}
