/* Lightweight community tools. Results stay on this page; nothing is stored or uploaded. */
(function(root) {
  'use strict';
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const t = (zh, en) => root.MetarI18n?.getLanguage() === 'en_US' ? en : zh;
  const EGRESS_ENDPOINT = '/metar/api/tools/ip';
  const STUN_URL = 'stun:stun.cloudflare.com:3478';
  const STUN_HOST = 'stun.cloudflare.com';
  const WEBRTC_TIMEOUT_MS = 4000;

  function ipFamily(address) {
    const value = String(address || '');
    if (/^\d{1,3}(?:\.\d{1,3}){3}$/.test(value) && value.split('.').every(p => Number(p) <= 255)) return 4;
    if (value.includes(':') && /^[0-9a-f:.]+$/i.test(value)) return 6;
    return 0;
  }

  function addressScope(address) {
    const value = String(address || '').toLowerCase();
    if (value.endsWith('.local')) return 'mdns';
    const family = ipFamily(value);
    if (family === 4) {
      const [a, b] = value.split('.').map(Number);
      if (a === 127) return 'loopback';
      if (a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168) || (a === 100 && b >= 64 && b <= 127)) return 'private';
      if (a === 169 && b === 254) return 'link-local';
      if (a === 0 || a >= 224) return 'reserved';
      return 'public';
    }
    if (family === 6) {
      if (value === '::1') return 'loopback';
      if (/^fe[89ab]/.test(value)) return 'link-local';
      if (/^f[cd]/.test(value)) return 'private';
      return 'public';
    }
    return 'unknown';
  }

  // RFC 8839 candidate-attribute: foundation component transport priority address port "typ" type ...
  function parseCandidate(line) {
    const text = String(line || '').replace(/^a=/, '').replace(/^candidate:/, '');
    const parts = text.trim().split(/\s+/);
    const typ = parts.indexOf('typ');
    if (parts.length < 8 || typ !== 6) return null;
    const address = parts[4];
    return { address, protocol: parts[2].toLowerCase(), type: parts[7].toLowerCase(), scope: addressScope(address) };
  }

  function normalizeIP(address) {
    const value = String(address || '').toLowerCase();
    return value.startsWith('::ffff:') && ipFamily(value.slice(7)) === 4 ? value.slice(7) : value;
  }

  /* Compare WebRTC-visible addresses with the egress address the gateway saw.
     A public address of the same family that differs from the egress address
     means WebRTC bypasses the current network path (for example a proxy). */
  function assessWebRTC(entries, egressIP) {
    const seen = new Map();
    for (const candidate of entries || []) if (candidate && !seen.has(candidate.address)) seen.set(candidate.address, candidate);
    const list = [...seen.values()];
    const egress = normalizeIP(egressIP);
    const egressFamily = ipFamily(egress);
    const publicAddresses = list.filter(c => c.scope === 'public').map(c => normalizeIP(c.address));
    const mismatched = egressFamily ? publicAddresses.filter(a => ipFamily(a) === egressFamily && a !== egress) : [];
    const otherFamily = egressFamily ? publicAddresses.filter(a => ipFamily(a) !== egressFamily) : [];
    const local = list.filter(c => ['private', 'link-local', 'loopback'].includes(c.scope)).map(c => c.address);
    const hidden = list.some(c => c.scope === 'mdns');
    let status = 'clean';
    if (mismatched.length) status = 'leak';
    else if (local.length || otherFamily.length) status = 'notice';
    else if (!list.length) status = 'none';
    return { status, publicAddresses: [...new Set(publicAddresses)], mismatched: [...new Set(mismatched)], otherFamily: [...new Set(otherFamily)], local: [...new Set(local)], hidden };
  }

  function formatOffset(minutesWest) {
    const total = -Number(minutesWest || 0);
    const sign = total < 0 ? '-' : '+';
    const abs = Math.abs(total);
    return `UTC${sign}${String(Math.floor(abs / 60)).padStart(2, '0')}:${String(abs % 60).padStart(2, '0')}`;
  }

  function browserEnvironment(env = root) {
    let timeZone = '', locale = '';
    try { const options = env.Intl.DateTimeFormat().resolvedOptions(); timeZone = options.timeZone || ''; locale = options.locale || ''; } catch (_) {}
    const nav = env.navigator || {};
    const languages = Array.isArray(nav.languages) && nav.languages.length ? [...nav.languages] : (nav.language ? [nav.language] : []);
    return { timeZone, offset: formatOffset(new env.Date().getTimezoneOffset()), languages, locale };
  }

  function collectIceAddresses(env = root, timeout = WEBRTC_TIMEOUT_MS) {
    const Peer = env.RTCPeerConnection;
    if (typeof Peer !== 'function') return Promise.resolve({ supported: false, entries: [] });
    return new Promise(resolve => {
      const entries = [];
      let peer, done = false, timer;
      const finish = () => {
        if (done) return;
        done = true;
        env.clearTimeout(timer);
        try { peer?.close(); } catch (_) {}
        resolve({ supported: true, entries });
      };
      try {
        peer = new Peer({ iceServers: [{ urls: STUN_URL }] });
        peer.onicecandidate = event => {
          if (!event.candidate) return finish();
          const parsed = parseCandidate(event.candidate.candidate);
          if (parsed) entries.push(parsed);
        };
        peer.onicegatheringstatechange = () => { if (peer.iceGatheringState === 'complete') finish(); };
        peer.createDataChannel('metar-check');
        peer.createOffer().then(offer => peer.setLocalDescription(offer)).catch(finish);
        timer = env.setTimeout(finish, timeout);
      } catch (_) { finish(); }
    });
  }

  async function fetchEgress(env = root) {
    const response = await env.fetch(EGRESS_ENDPOINT, { cache: 'no-store', credentials: 'omit', headers: { Accept: 'application/json' } });
    if (!response.ok) throw new Error('egress unavailable');
    const body = await response.json();
    const ip = normalizeIP(body?.ip);
    if (!ipFamily(ip)) throw new Error('egress invalid');
    return ip;
  }

  const row = (label, value) => `<div class="tool-row"><dt>${esc(label)}</dt><dd>${value}</dd></div>`;
  const code = value => value ? `<code>${esc(value)}</code>` : '<span class="muted">—</span>';
  const badge = (status, text) => `<span class="tool-badge" data-status="${esc(status)}">${esc(text)}</span>`;

  function webrtcResult(result, egressIP) {
    if (!result.supported) return `<p>${badge('notice', t('不可用', 'Unavailable'))} ${esc(t('浏览器不支持或已禁用 WebRTC，网页无法通过它获取你的地址。', 'WebRTC is unsupported or disabled, so pages cannot read addresses through it.'))}</p>`;
    const assessment = assessWebRTC(result.entries, egressIP);
    const summary = {
      leak: [badge('leak', t('发现差异', 'Mismatch found')), t('WebRTC 暴露的公网地址与当前出口 IP 不同，可能绕过了代理或 VPN。', 'WebRTC exposes a public address different from your egress IP and may bypass your proxy or VPN.')],
      notice: [badge('notice', t('需留意', 'Review')), t('未发现与出口不同的同协议公网地址，但 WebRTC 暴露了内网或另一协议的地址。', 'No different public address of the same protocol, but WebRTC exposes a local or other-protocol address.')],
      clean: [badge('ok', t('未发现泄露', 'No leak found')), t('WebRTC 看到的公网地址与出口 IP 一致，或仅出现隐藏的 mDNS 地址。', 'WebRTC sees the same public address as your egress IP, or only hidden mDNS names.')],
      none: [badge('ok', t('未获取到地址', 'No addresses')), t('WebRTC 没有返回任何地址，常见于浏览器隐私设置或网络限制。', 'WebRTC returned no addresses, which is common with privacy settings or network restrictions.')],
    }[assessment.status];
    const list = values => values.length ? values.map(code).join(' ') : '<span class="muted">—</span>';
    return `<p>${summary[0]} ${esc(summary[1])}</p><dl class="tool-rows">
      ${row(t('WebRTC 公网地址', 'WebRTC public addresses'), list(assessment.publicAddresses))}
      ${assessment.otherFamily.length ? row(t('另一协议的公网地址', 'Other-protocol public addresses'), list(assessment.otherFamily)) : ''}
      ${row(t('内网地址', 'Local addresses'), list(assessment.local))}
      ${row(t('mDNS 隐藏地址', 'mDNS hidden addresses'), assessment.hidden ? esc(t('有（浏览器已隐藏真实内网地址）', 'Yes (the browser hides the real local address)')) : '<span class="muted">—</span>')}
    </dl>`;
  }

  class View {
    constructor(tools = []) {
      this.tools = tools;
      this.egress = null;
      this.disposed = false;
    }

    page(path) {
      if (path === '/tools/network') return this.networkPage();
      return this.hubPage();
    }

    hubPage() {
      const cards = this.tools.map(tool => `<a class="card card-pad tool-card" data-router href="${esc(tool.path)}"><strong>${esc(t(tool.zh, tool.en))}</strong><p class="muted mt8">${esc(t(tool.descriptionZh, tool.descriptionEn))}</p></a>`).join('');
      return `<section class="tools-page"><h1 class="visually-hidden">${esc(t('工具', 'Tools'))}</h1><div class="tool-grid">${cards}</div></section>`;
    }

    networkPage() {
      const env = browserEnvironment(root);
      return `<section class="tools-page stack" data-tool="network">
        <h1 class="visually-hidden">${esc(t('网络自检', 'Network self-check'))}</h1>
        <p class="muted">${esc(t('检查当前网络出口和浏览器公开的环境信息。结果只在本页显示，不会保存或上传。', 'Check your network egress and the environment details your browser exposes. Results are shown on this page only and are never stored or uploaded.'))}</p>
        <section class="card card-pad"><h2>${esc(t('出口 IP', 'Egress IP'))}</h2>
          <dl class="tool-rows">${row(t('社区服务器看到的地址', 'Address seen by the community server'), `<span data-egress>${esc(t('正在读取…', 'Loading…'))}</span>`)}${row(t('协议', 'Protocol'), '<span data-egress-family>—</span>')}</dl>
        </section>
        <section class="card card-pad"><h2>${esc(t('浏览器环境', 'Browser environment'))}</h2>
          <dl class="tool-rows">
            ${row(t('时区', 'Time zone'), code(env.timeZone))}
            ${row(t('UTC 偏移', 'UTC offset'), code(env.offset))}
            ${row(t('语言偏好', 'Language preferences'), env.languages.length ? env.languages.map(code).join(' ') : code(''))}
            ${row(t('日期与数字格式', 'Date and number format'), code(env.locale))}
          </dl>
        </section>
        <section class="card card-pad"><h2>${esc(t('WebRTC 泄露', 'WebRTC leak'))}</h2>
          <p class="muted">${esc(t(`点击后浏览器会连接公共 STUN 服务器 ${STUN_HOST}，以获取 WebRTC 能看到的地址，并与出口 IP 对比。`, `Your browser will contact the public STUN server ${STUN_HOST} to read the addresses WebRTC can see, then compare them with your egress IP.`))}</p>
          <div class="mt8"><button type="button" class="btn primary" data-tool-webrtc>${esc(t('开始检测', 'Run check'))}</button></div>
          <div class="mt8" data-webrtc-result aria-live="polite"></div>
        </section>
        <p class="muted small">${esc(t('DNS 泄露检测需要专用解析服务，暂未提供。', 'DNS leak checks need a dedicated resolver service and are not available yet.'))}</p>
      </section>`;
    }

    mount(container) {
      const page = container.querySelector('[data-tool="network"]');
      if (!page) return;
      const target = page.querySelector('[data-egress]');
      const family = page.querySelector('[data-egress-family]');
      this.egress = fetchEgress(root).then(ip => {
        if (this.disposed) return ip;
        target.innerHTML = code(ip);
        family.textContent = ipFamily(ip) === 6 ? 'IPv6' : 'IPv4';
        return ip;
      }).catch(() => {
        if (!this.disposed) target.textContent = t('暂时无法读取，请稍后刷新。', 'Unavailable. Refresh later.');
        return '';
      });
      const button = page.querySelector('[data-tool-webrtc]');
      const output = page.querySelector('[data-webrtc-result]');
      button.addEventListener('click', async () => {
        button.disabled = true;
        output.textContent = t('正在检测…', 'Checking…');
        try {
          const [egressIP, result] = await Promise.all([this.egress, collectIceAddresses(root)]);
          if (!this.disposed) output.innerHTML = webrtcResult(result, egressIP);
        } finally {
          if (!this.disposed) button.disabled = false;
        }
      });
    }

    dispose() { this.disposed = true; }
  }

  const exports = { View, ipFamily, addressScope, parseCandidate, assessWebRTC, formatOffset, browserEnvironment, collectIceAddresses, fetchEgress, webrtcResult, EGRESS_ENDPOINT, STUN_HOST };
  if (typeof module === 'object' && module.exports) module.exports = exports;
  else root.MetarTools = Object.freeze(exports);
})(typeof window === 'undefined' ? globalThis : window);
