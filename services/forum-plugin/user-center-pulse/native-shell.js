import { decorateNativeHeader, headerIcon, headerAccess } from './header-presentation.js';
import './return-navigation.js';
import { mountNotificationNavigation } from './notification-navigation.js';
import { installTheme } from './shared-theme.js';
import { installHomeNavigation, canonicalizeHomeLinks } from './home-navigation.js';
import { mountComposerLayout, positionComposerMenus } from './composer-layout.js';

export function backDestination(path) {
  if (/^\/(?:latest|questions|discover)?\/?$/.test(path)) return null;
  if (/^\/(?:admin(?:\/|$)|me\/|badges(?:\/|$)|users\/settings(?:\/|$)|settings(?:\/|$))/.test(path)) return '/me';
  return '/latest';
}

export function installNativeShell(language, host = window) {
  if (host.__metarNativeShell) return;
  host.__metarNativeShell = true;
  const doc = host.document;
  // The avatar opens the personal space; the legacy dropdown is no longer shown.
  doc.addEventListener('click', (event) => {
    const trigger = event.target.closest?.('#header #dropdown-basic');
    if (!trigger) return;
    event.preventDefault();
    event.stopPropagation();
    if (host.location.pathname !== '/me') {
      host.__metarReturnNavigation?.remember(host.location.href, '/me');
      host.location.href = '/me';
    }
  }, true);
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
  ].map(([href, zh, en]) => {
    const link = doc.createElement('a');
    link.href = href;
    // Normal document navigation retains Answer's beforeunload draft guard.
    nav.append(link);
    return { link, zh, en };
  });
  links[0].link.setAttribute('aria-current', 'page');
  const tools=doc.createElement('details');tools.className='metar-header-tools';
  const toolsTitle=doc.createElement('summary');tools.append(toolsTitle);
  const toolsPanel=doc.createElement('div');tools.append(toolsPanel);
  const toolLinks=[['/tools','全部工具','All tools'],['/tools/network','Claude 访问环境检测','Claude environment check']].map(([href,zh,en])=>{const link=doc.createElement('a');link.href=href;toolsPanel.append(link);return {link,zh,en};});
  const gift=doc.createElement('a'),giftLabel=doc.createElement('span'),giftTime=doc.createElement('time');gift.href='/events?view=gift';gift.className='metar-gift-nav';gift.hidden=true;gift.append(giftLabel,giftTime);nav.append(gift,tools);
  let giftEnd=0,giftNext=0,giftAccount='',giftBusy=false;
  async function refreshGift(){const u=language.getUser?.(),account=headerAccess(u).chat?String(u.username):'';if(account!==giftAccount){giftAccount=account;giftEnd=giftNext=0;gift.hidden=true}if(!account||doc.hidden||!gift.isConnected)return;const now=Date.now();if(now>=giftNext&&!giftBusy){giftBusy=true;giftNext=now+15000;try{const token=host.localStorage.getItem('_a_ltk_');if(!token)return;const response=await host.fetch('/answer/api/v1/metar/events/gift',{headers:{Authorization:token.startsWith('Bearer ')?token:'Bearer '+token},credentials:'same-origin',redirect:'error',cache:'no-store',signal:AbortSignal.timeout(5000)});const v=response.ok?await response.json():null;if(account!==language.getUser?.()?.username)return;giftEnd=Number(v?.data?.offer?.end||0)}catch{giftEnd=0}finally{giftBusy=false}}const left=Math.max(0,Math.ceil(giftEnd-Date.now()/1000));gift.hidden=!left;if(left){const label=language.getLanguage()==='en_US'?'Limited gifts':'限时赠礼';if(giftLabel.textContent!==label)giftLabel.textContent=label;const text=[Math.floor(left/3600),Math.floor(left%3600/60),left%60].map(n=>String(n).padStart(2,'0')).join(':');if(giftTime.textContent!==text)giftTime.textContent=text}}
  host.setInterval(refreshGift,1000);
  doc.addEventListener('click',event=>{if(!tools.contains(event.target)||event.target.closest('a'))tools.open=false;});
  doc.addEventListener('keydown',event=>{if(event.key==='Escape')tools.open=false;if((event.ctrlKey||event.metaKey)&&event.key.toLowerCase()==='k'){const input=doc.querySelector('#header form input[type=search]');if(input){event.preventDefault();input.focus();}}});
  const back = doc.createElement('button');
  back.type = 'button';
  back.className = 'page-back metar-back';
  back.onclick = () => {
    const fallback = backDestination(host.location.pathname);
    if (fallback && host.__metarReturnNavigation?.back) { host.__metarReturnNavigation.back(fallback); return; }
    const destination = fallback;
    if (destination) host.location.href = destination;
  };
  const toggle = doc.createElement('button');
  toggle.type = 'button';
  toggle.className = 'metar-theme-toggle';
  toggle.addEventListener('click', () => theme.select(theme.current() === 'dark' ? 'light' : 'dark'));
  const label = () => {
    const english = language.getLanguage() === 'en_US';
    const backText = english ? '← Back' : '← 返回';
    if (back.textContent !== backText) back.textContent = backText;
    back.setAttribute('aria-label', english ? 'Back to home' : '返回主界面');
    mountNotificationNavigation(doc, host.location.pathname, english);
    nav.setAttribute('aria-label', english ? 'Main navigation' : '主导航');
    const toolsText=english?'Tools':'工具';if(toolsTitle.textContent!==toolsText)toolsTitle.textContent=toolsText;
    toolLinks.forEach(({link,zh,en})=>{const text=english?en:zh;if(link.textContent!==text)link.textContent=text;});
    links.forEach(({ link, zh, en }) => {
      const text = english ? en : zh;
      if (link.textContent !== text) link.textContent = text;
    });
    const dark = theme.current() === 'dark';
    const title = english ? (dark ? 'Switch to light theme' : 'Switch to dark theme') : (dark ? '切换到浅色主题' : '切换到深色主题');
    toggle.setAttribute('aria-label', title);
    toggle.title = title;
    decorateNativeHeader(doc,{english,dark,...headerAccess(language.getUser?.())});
    const icon = dark ? 'sun' : 'moon';
    if (toggle.dataset.icon !== icon) { toggle.innerHTML=headerIcon(icon);toggle.dataset.icon=icon; }
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
    root.classList.toggle('metar-admin-users', host.location.pathname === '/admin/users');
    const header = doc.querySelector('#header');
    const row = header?.querySelector(':scope > .w-100');
    const brand = row?.querySelector('.navbar-brand');
    if (!row || !brand) return;
    back.hidden = backDestination(host.location.pathname) === null;
    if (back.parentElement !== row) row.prepend(back);
    const avatar = header.querySelector('#dropdown-basic')?.closest('.dropdown');
    avatar?.parentElement.classList.add('metar-account-actions');
    if (nav.parentElement !== row) brand.after(nav);
    if (toggle.parentElement !== row) row.append(toggle);
    const user=language.getUser?.();
    decorateNativeHeader(doc,{english:language.getLanguage()==='en_US',dark:theme.current()==='dark',...headerAccess(user)});
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
