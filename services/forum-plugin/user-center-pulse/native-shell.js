import { installTheme } from './shared-theme.js';

export function installNativeShell(language, host = window) {
  if (host.__metarNativeShell) return;
  host.__metarNativeShell = true;
  const doc = host.document;
  const root = doc.documentElement;
  root.classList.add('metar-native');
  const theme = installTheme(host, { native: true });
  const nav = doc.createElement('nav');
  nav.className = 'metar-primary-nav';
  const links = [
    ['/latest', '社区', 'Community'],
    ['/knowledge', '知识库', 'Knowledge'],
    ['/pulse', 'Pulse', 'Pulse'],
  ].map(([href, zh, en]) => {
    const link = doc.createElement('a');
    link.href = href;
    // Normal document navigation retains Answer's beforeunload draft guard.
    nav.append(link);
    return { link, zh, en };
  });
  links[0].link.setAttribute('aria-current', 'true');
  const toggle = doc.createElement('button');
  toggle.type = 'button';
  toggle.className = 'metar-theme-toggle';
  toggle.addEventListener('click', () => theme.select(theme.current() === 'dark' ? 'light' : 'dark'));
  const label = () => {
    const english = language.getLanguage() === 'en_US';
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
  }) : null;
  function mount() {
    const header = doc.querySelector('#header');
    const row = header?.querySelector(':scope > .w-100');
    const brand = row?.querySelector('.navbar-brand');
    if (!row || !brand) return;
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
  new host.MutationObserver(() => {
    if (pending) return;
    pending = true;
    host.requestAnimationFrame(() => { pending = false; mount(); });
  }).observe(root, { childList: true, subtree: true });
  mount();
}
