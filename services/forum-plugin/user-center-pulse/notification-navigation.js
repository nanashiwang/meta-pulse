// Presentation adapter only. Notifications and their read/filter actions remain
// Answer components; management links are copied only when Answer renders them.
export function notificationGroups(nativeNav, english = false) {
  const item = (href, zh, en, icon) => ({ href, label: english ? en : zh, icon });
  const groups = [
    { label: english ? 'Community' : '社区', items: [
      item('/latest', '全部讨论', 'All discussions', 'chat-square-text'),
      item('/latest?order=unanswered', '待回答', 'Unanswered', 'question-circle'),
      item('/topics', '全部标签', 'All tags', 'tags'),
      item('/knowledge', '知识库', 'Knowledge', 'book'),
    ] },
    { label: english ? 'My space' : '我的空间', items: [
      item('/me', '个人空间', 'My space', 'person'),
      item('/me/growth', '社区成长', 'Community growth', 'stars'),
      item('/me/bookmarks', '我的收藏', 'Bookmarks', 'bookmark'),
      item('/users/notifications/inbox', '通知中心', 'Notifications', 'bell'),
      item('/settings/binding', '账号绑定', 'Account binding', 'link-45deg'),
      item('/users/settings/profile', '账号设置', 'Account settings', 'gear'),
    ] },
  ];
  const management = [];
  if (nativeNav.querySelector('a[href="/admin"]')) {
    management.push(item('/admin/growth', '成长管理', 'Growth settings', 'stars'));
    management.push(item('/admin/pulse', 'Pulse 配置', 'Pulse settings', 'sliders'));
    management.push(item('/admin/dashboard', '社区管理', 'Community admin', 'gear'));
    management.push(item('/admin/pulse_user_center', '社区连接配置', 'Community connections', 'link-45deg'));
  }
  if (nativeNav.querySelector('a[href="/review"]')) management.push(item('/review', '审查', 'Review', 'shield-check'));
  if (management.length) groups.push({ label: english ? 'Management' : '管理', items: management });
  groups.push({ label: '', items: [
    item('/pulse', 'Pulse 权益', 'Pulse benefits', 'activity'),
    item('/support', '帮助中心', 'Help center', 'question-circle'),
    item('/status', '服务状态', 'Service status', 'hdd-stack'),
    item('/guidelines', '社区规范', 'Community guidelines', 'shield-check'),
    item('/sitemap.xml', '站点地图', 'Sitemap', 'diagram-3'),
  ] });
  return groups;
}

export function mountNotificationNavigation(doc, pathname, english = false) {
  const active = /^\/users\/notifications\/(inbox|achievement)(?:\/|$)/.test(pathname);
  doc.documentElement.classList.toggle('metar-notifications', active);
  if (!active) {
    doc.querySelectorAll('.metar-community-navigation').forEach((nav) => nav.remove());
    return;
  }
  // Answer renders a separate SideNav inside its mobile drawer.
  for (const nativeNav of doc.querySelectorAll('[id="sideNav"]')) {
    let nav = nativeNav.parentElement.querySelector(':scope > .metar-community-navigation');
    if (!nav) {
      nav = doc.createElement('nav');
      nav.className = 'metar-community-navigation';
      nativeNav.after(nav);
    }
    const groups = notificationGroups(nativeNav, english);
    const signature = JSON.stringify(groups);
    if (nav.dataset.signature === signature) continue;
    nav.dataset.signature = signature;
    nav.setAttribute('aria-label', english ? 'Community navigation' : '社区导航');
    nav.replaceChildren();
    for (const group of groups) {
      const section = doc.createElement('div');
      section.className = 'metar-nav-group';
      if (group.label) {
        const label = doc.createElement('div');
        label.className = 'metar-nav-label';
        label.textContent = group.label;
        section.append(label);
      }
      for (const item of group.items) {
        const link = doc.createElement('a');
        link.href = item.href;
        link.className = 'metar-nav-item';
        if (item.href === '/users/notifications/inbox') link.setAttribute('aria-current', 'page');
        const icon = doc.createElement('i');
        icon.className = `bi bi-${item.icon}`;
        icon.setAttribute('aria-hidden', 'true');
        const text = doc.createElement('span');
        text.textContent = item.label;
        link.append(icon, text);
        section.append(link);
      }
      nav.append(section);
    }
  }
}
