// Shared by the Answer plugin and METAR shell. The browser never supplies a
// trusted identity: native admin auth and the switch ticket decide the target.
(function (host) {
  'use strict';
  const TOKEN = '_a_ltk_', RETURN = '_metar_admin_return_';
  let busy = false, installed = false;
  const english = () => (host.document.documentElement.lang || '').startsWith('en');
  const text = (zh, en) => english() ? en : zh;
  const token = () => { try { return host.localStorage.getItem(TOKEN) || ''; } catch (_) { return ''; } };
  const headers = (value) => ({ Authorization: value.startsWith('Bearer ') ? value : `Bearer ${value}`, 'X-Metar-Request': '1', 'Content-Type': 'application/json' });
  const saved = () => { try { return JSON.parse(host.sessionStorage.getItem(RETURN) || 'null'); } catch (_) { return null; } };
  async function api(operation, body, value) {
    const response = await host.fetch(`/answer/admin/api/metar/accounts/${operation}`, {
      method: 'POST', credentials: 'same-origin', cache: 'no-store', headers: headers(value), body: JSON.stringify(body),
    });
    const result = await response.json();
    if (!response.ok) {
      if (JSON.stringify(result).includes('switch_plugin_disabled')) throw new Error(text('请先在后台「插件」启用「管理员账号切换」，然后重试。', 'Enable “管理员账号切换” in Admin → Plugins, then retry.'));
      throw new Error(text('操作失败：请检查管理员登录状态及目标账号是否已激活、可用且不是管理员。', 'Operation failed. Check the administrator session and that the target is an active non-administrator.'));
    }
    return result.data;
  }
  function changed(expected) {
    if (token() !== expected) throw new Error(text('登录账号已在其他页面改变，请刷新后重试。', 'The account changed in another page. Reload and retry.'));
  }
  async function start(username) {
    if (busy) return;
    if (saved()) { host.alert(text('请先返回管理员账号。', 'Return to the administrator account first.')); return; }
    if (!host.confirm(text(`切换登录到 @${username}？之后的操作会以该账号身份执行。`, `Sign in as @${username}? Subsequent actions will use this account.`))) return;
    busy = true;
    const adminToken = token();
    try {
      // Check storage before issuing a login. The return credential stays in
      // this tab, never in URLs or a permanent account/password collection.
      host.sessionStorage.setItem(RETURN, JSON.stringify({ adminToken, pending: true }));
      const result = await api('start', { username }, adminToken);
      changed(adminToken);
      if (!/^[a-f0-9]{64}$/.test(result.ticket) || result.callback !== '/answer/api/v1/connector/redirect/metar_admin_switch') throw new Error('Invalid switch response');
      const state = { adminToken, ticket: result.ticket, username, targetToken: null };
      host.sessionStorage.setItem(RETURN, JSON.stringify(state));
      const response = await host.fetch(result.callback, {
        credentials: 'same-origin', cache: 'no-store', headers: { ...headers(adminToken), 'X-Metar-Switch': result.ticket },
        referrerPolicy: 'same-origin',
      });
      const landing = new URL(response.url);
      const targetToken = landing.searchParams.get('access_token');
      if (!response.ok || landing.origin !== host.location.origin || landing.pathname !== '/users/auth-landing' || !targetToken) throw new Error(text('切换未完成，请返回管理员后重试。', 'Switch did not complete. Return to the administrator and retry.'));
      changed(adminToken);
      state.targetToken = targetToken;
      host.sessionStorage.setItem(RETURN, JSON.stringify(state));
      host.localStorage.setItem(TOKEN, targetToken);
      host.location.assign('/latest');
    } catch (error) {
      if (!saved()?.ticket) host.sessionStorage.removeItem(RETURN);
      host.alert(error.message);
      mountBanner();
    } finally { busy = false; }
  }
  async function restore() {
    if (busy) return;
    const state = saved(); if (!state?.ticket || !state.adminToken) return;
    busy = true;
    try {
      const current = token();
      // Never replace an unrelated login, including another tab's switch.
      if (current !== state.targetToken && current !== state.adminToken && current !== '') throw new Error(text('当前已是其他登录账号，请重新登录管理员。', 'A different account is signed in. Sign in as administrator again.'));
      await api('return', { ticket: state.ticket }, state.adminToken);
      if (state.targetToken) {
        const response = await host.fetch('/answer/api/v1/user/logout', { credentials: 'same-origin', cache: 'no-store', headers: headers(state.targetToken) });
        if (!response.ok && response.status !== 401) throw new Error(text('退出切换账号失败，请重试。', 'Could not sign out of the switched account. Retry.'));
      }
      changed(current);
      host.localStorage.setItem(TOKEN, state.adminToken);
      host.sessionStorage.removeItem(RETURN);
      host.location.assign('/admin/users');
    } catch (error) { host.alert(`${error.message}\n${text('原管理员会话失效时，可通过正常登录页重新登录。', 'If the administrator session expired, use the normal sign-in page.')}`); }
    finally { busy = false; }
  }
  function mountBanner() {
    let state = saved();
    if (state?.ticket && token() && token() !== state.adminToken && token() !== state.targetToken) {
      host.sessionStorage.removeItem(RETURN); state = null;
    }
    let banner = host.document.getElementById('metar-account-switch-banner');
    if (!state?.ticket) { banner?.remove(); return; }
    if (banner) return;
    banner = host.document.createElement('aside'); banner.id = 'metar-account-switch-banner'; banner.setAttribute('aria-label', text('账号切换', 'Account switching'));
    banner.style.cssText = 'position:fixed;bottom:16px;left:50%;transform:translateX(-50%);z-index:2000;max-width:calc(100vw - 24px);width:max-content;padding:12px 16px;border:1px solid #b78103;border-radius:10px;background:#fff7df;color:#352600;display:flex;align-items:center;gap:12px;flex-wrap:wrap;box-shadow:0 3px 15px #0003;font:14px sans-serif';
    const label = host.document.createElement('span'); label.textContent = text(`切换账号：@${state.username}`, `Switched account: @${state.username}`);
    const button = host.document.createElement('button'); button.type = 'button'; button.textContent = text('返回管理员', 'Return to administrator'); button.className = 'btn btn-sm btn-warning'; button.onclick = restore;
    const login = host.document.createElement('a'); login.href = '/users/login'; login.textContent = text('重新登录', 'Sign in again');
    banner.append(label, button, login); host.document.body.append(banner);
  }
  function mountRows(getUser) {
    if (host.location.pathname !== '/admin/users' || getUser()?.role_id !== 2 || saved()) return;
    host.document.querySelectorAll('tbody tr').forEach((row) => {
      const link = row.querySelector('td:first-child a[href^="/users/"]');
      if (!link) return;
      const path = new URL(link.href, host.location.origin).pathname;
      if (!/^\/users\/[^/]+$/.test(path)) return;
      const username = decodeURIComponent(path.slice(7));
      if (username === getUser()?.username) return;
      let button = row.querySelector('[data-metar-switch]');
      if (!button) {
        button = host.document.createElement('button'); button.type = 'button'; button.className = 'btn btn-sm btn-outline-primary mt-1'; button.setAttribute('data-metar-switch', '');
        row.lastElementChild.append(button);
      }
      const label = text('切换登录', 'Sign in as'); if (button.textContent !== label) button.textContent = label;
      button.onclick = () => start(username);
    });
  }
  function install(getUser = () => null) {
    if (installed) return; installed = true;
    const run = () => { mountBanner(); mountRows(getUser);
      host.document.querySelectorAll('a[href$="/connector/login/metar_admin_switch"]').forEach((a) => { a.hidden = true; });
    };
    let pending = false;
    new host.MutationObserver(() => { if (!pending) { pending = true; host.requestAnimationFrame(() => { pending = false; run(); }); } }).observe(host.document.documentElement, { childList: true, subtree: true });
    // A full document reload discards in-flight requests and private views.
    const initialToken = token();
    host.addEventListener('storage', (e) => { if ((e.key === TOKEN || e.key === null) && token() !== initialToken) host.location.reload(); });
    host.addEventListener('pageshow', () => { if (token() !== initialToken) host.location.reload(); });
    run();
  }
  host.MetarAccountSwitch = { install, start, restore };
  if (!host.__metarNativeAccountSwitch) {
    if (host.document.readyState === 'loading') host.document.addEventListener('DOMContentLoaded', () => { if (!host.__metarNativeAccountSwitch) install(); }, { once: true });
    else host.setTimeout(() => { if (!host.__metarNativeAccountSwitch) install(); }, 0);
  }
})(window);
