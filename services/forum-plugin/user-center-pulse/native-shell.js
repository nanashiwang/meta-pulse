import { mountNotificationNavigation } from './notification-navigation.js';
import { installTheme } from './shared-theme.js';
import { installHomeNavigation, canonicalizeHomeLinks } from './home-navigation.js';
import { mountComposerLayout, positionComposerMenus } from './composer-layout.js';

export function backDestination(path, historyLength) {
  if (/^\/(?:latest|questions)?\/?$/.test(path)) return null;
  if (/^\/users\/settings(?:\/|$)/.test(path) || /^\/settings\/binding\/?$/.test(path)) return '/me';
  return historyLength > 1 ? 'back' : '/latest';
}

export function installNativeShell(language, host = window) {
  if (host.__metarNativeShell) return;
  host.__metarNativeShell = true;
  const doc = host.document;
  installHomeNavigation(host);
  const root = doc.documentElement;
  root.classList.add('metar-native');
  // Runtime URL keeps Answer's bundler from resolving a gateway-owned asset.
  root.style.setProperty('--metar-brand-image', 'url("/metar-assets/brand-logo.jpg")');
  const theme = installTheme(host, { native: true });
  const nav = doc.createElement('nav');
  nav.className = 'metar-primary-nav';
  const links = [
    ['/latest', '社区', 'Community'],
    ['/marketplace', '集市', 'Marketplace'],
    ['/models', '模型榜', 'Models'],
    ['/pulse', 'Pulse', 'Pulse'],
    ['/tools', '工具', 'Tools'],
  ].map(([href, zh, en]) => {
    const link = doc.createElement('a');
    link.href = href;
    // Normal document navigation retains Answer's beforeunload draft guard.
    nav.append(link);
    return { link, zh, en };
  });
  links[0].link.setAttribute('aria-current', 'true');
  const back = doc.createElement('button');
  back.type = 'button';
  back.className = 'metar-back';
  back.onclick = () => {
    const destination = backDestination(host.location.pathname, host.history.length);
    if (destination === 'back') host.history.back();
    else if (destination) host.location.href = destination;
  };
  const toggle = doc.createElement('button');
  toggle.type = 'button';
  toggle.className = 'metar-theme-toggle';
  toggle.addEventListener('click', () => theme.select(theme.current() === 'dark' ? 'light' : 'dark'));
  const label = () => {
    const english = language.getLanguage() === 'en_US';
    const backText = english ? '← Back' : '← 返回';
    if (back.textContent !== backText) back.textContent = backText;
    back.setAttribute('aria-label', english ? 'Go back' : '返回上一页');
    mountNotificationNavigation(doc, host.location.pathname, english);
    nav.setAttribute('aria-label', english ? 'Main navigation' : '主导航');
    links.forEach(({ link, zh, en }) => {
      const text = english ? en : zh;
      if (link.textContent !== text) link.textContent = text;
    });
    const dark = theme.current() === 'dark';
    const title = english ? (dark ? 'Switch to light theme' : 'Switch to dark theme') : (dark ? '切换到浅色主题' : '切换到深色主题');
    toggle.setAttribute('aria-label', title);
    toggle.title = title;
    const icon = dark ? '☀' : '☾';
    if (toggle.textContent !== icon) toggle.textContent = icon;
  };
  theme.subscribe(label);
  language.onLanguageChanged(label);
  let observedHeader;
  const resize = host.ResizeObserver ? new host.ResizeObserver((entries) => {
    root.style.setProperty('--metar-header-height', `${Math.ceil(entries[0].target.getBoundingClientRect().height)}px`);
    positionComposerMenus(host);
  }) : null;
  function mount() {
    mountComposerLayout(doc);
    positionComposerMenus(host);
    canonicalizeHomeLinks(doc, host.location.origin);
    mountNotificationNavigation(doc, host.location.pathname, language.getLanguage() === 'en_US');
    const header = doc.querySelector('#header');
    const row = header?.querySelector(':scope > .w-100');
    const brand = row?.querySelector('.navbar-brand');
    if (!row || !brand) return;
    back.hidden = backDestination(host.location.pathname, host.history.length) === null;
    if (back.parentElement !== row) row.prepend(back);
    const avatar = header.querySelector('#dropdown-basic')?.closest('.dropdown');
    avatar?.parentElement.classList.add('metar-account-actions');
    if (nav.parentElement !== row) brand.after(nav);
    if (toggle.parentElement !== row) row.append(toggle);
    if (brand.getAttribute('aria-label') !== 'METAR') brand.setAttribute('aria-label', 'METAR');
    if (header !== observedHeader) {
      resize?.disconnect();
      resize?.observe(header);
      observedHeader = header;
    }
  }
  // Coalesce rich editor/profile updates and let the host finish rendering.
  let pending = false;
  const schedule = () => {
    if (pending) return;
    pending = true;
    host.requestAnimationFrame(() => { pending = false; mount(); });
  };
  new host.MutationObserver(schedule).observe(root, { childList: true, subtree: true });
  let menuPending = false;
  const scheduleMenus = () => {
    if (menuPending) return;
    menuPending = true;
    host.requestAnimationFrame(() => { menuPending = false; positionComposerMenus(host); });
  };
  doc.addEventListener('focusin', scheduleMenus);
  host.addEventListener('resize', scheduleMenus);
  host.addEventListener('scroll', scheduleMenus, { passive: true });
  host.visualViewport?.addEventListener('resize', scheduleMenus);
  host.visualViewport?.addEventListener('scroll', scheduleMenus);
  host.addEventListener('popstate', schedule);
  label();
  mount();
}
