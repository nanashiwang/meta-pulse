/* Lightweight community tools. Results stay on this page; nothing is stored or uploaded. */
(function(root) {
  'use strict';
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const t = (zh, en) => root.MetarI18n?.getLanguage() === 'en_US' ? en : zh;
  const EGRESS_ENDPOINT = '/metar/api/tools/ip';
  const EGRESS_TIMEOUT_MS = 8000;
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
    for (const entry of entries || []) if (entry && !seen.has(entry.address)) seen.set(entry.address, entry);
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

  /* The failure reason is shown to the visitor, so a blocked request, a gateway
     without the endpoint and a slow network are distinguishable. */
  class EgressError extends Error {
    constructor(reason, status = 0) { super(reason); this.reason = reason; this.status = status; }
  }

  async function fetchEgress(env = root, timeout = EGRESS_TIMEOUT_MS) {
    const controller = typeof env.AbortController === 'function' ? new env.AbortController() : null;
    const timer = controller ? env.setTimeout(() => controller.abort(), timeout) : 0;
    let response;
    try {
      response = await env.fetch(EGRESS_ENDPOINT, { cache: 'no-store', credentials: 'omit', headers: { Accept: 'application/json' }, signal: controller?.signal });
    } catch (error) {
      throw new EgressError(error?.name === 'AbortError' ? 'timeout' : 'network');
    } finally {
      if (controller) env.clearTimeout(timer);
    }
    if (!response.ok) throw new EgressError('http', response.status);
    let body;
    try { body = await response.json(); } catch (_) { throw new EgressError('format'); }
    const ip = normalizeIP(body?.ip);
    if (!ipFamily(ip)) throw new EgressError('format');
    return ip;
  }

  function egressErrorText(error) {
    switch (error?.reason) {
      case 'timeout': return t('请求超时，网络较慢或被拦截。', 'The request timed out; the network is slow or blocking it.');
      case 'network': return t('请求被拦截或网络中断，可检查广告/隐私拦截插件后重试。', 'The request was blocked or the connection dropped. Check ad or privacy blockers and retry.');
      case 'http': return t(`服务器返回 ${error.status}，服务可能正在更新，请稍后重试。`, `The server returned ${error.status}. It may be updating; retry shortly.`);
      case 'format': return t('服务器返回的内容无法识别，服务可能正在更新，请稍后重试。', 'The server response was not recognised. It may be updating; retry shortly.');
      default: return t('暂时无法读取，请稍后重试。', 'Unavailable right now. Retry shortly.');
    }
  }

  const STATUS_TEXT = {
    pending: ['等待检测', 'Not run'],
    running: ['检测中', 'Checking'],
    ok: ['正常', 'OK'],
    clean: ['未发现泄露', 'No leak'],
    none: ['未获取到地址', 'No addresses'],
    notice: ['需留意', 'Review'],
    leak: ['发现差异', 'Mismatch'],
    error: ['读取失败', 'Failed'],
    unsupported: ['不可用', 'Unavailable'],
    info: ['已读取', 'Read'],
  };
  const TONE = { ok: 'ok', clean: 'ok', none: 'ok', info: 'info', notice: 'warn', leak: 'risk', error: 'risk', unsupported: 'warn', pending: 'idle', running: 'idle' };
  const pill = status => `<span class="tool-pill" data-tone="${TONE[status] || 'idle'}">${esc(t(...(STATUS_TEXT[status] || STATUS_TEXT.pending)))}</span>`;
  const code = value => value ? `<code>${esc(value)}</code>` : '<span class="muted">—</span>';
  const row = (label, value) => `<div class="tool-row"><dt>${esc(label)}</dt><dd>${value}</dd></div>`;

  function addressRows(assessment, egressIP) {
    const egress = normalizeIP(egressIP);
    const rows = [];
    for (const address of assessment.publicAddresses) {
      const status = assessment.mismatched.includes(address) ? 'leak' : assessment.otherFamily.includes(address) ? 'notice' : 'ok';
      const note = status === 'leak' ? t('与出口 IP 不同', 'Differs from egress IP') : status === 'notice' ? t('另一协议的公网地址', 'Public address on the other protocol') : address === egress ? t('与出口 IP 一致', 'Matches egress IP') : t('公网地址', 'Public address');
      rows.push({ address, status, note });
    }
    for (const address of assessment.local) rows.push({ address, status: 'notice', note: t('内网地址', 'Local address') });
    if (assessment.hidden) rows.push({ address: '*.local', status: 'ok', note: t('mDNS 已隐藏真实内网地址', 'mDNS hides the real local address') });
    return rows.length ? `<ul class="tool-address-list">${rows.map(r => `<li><code>${esc(r.address)}</code><span class="muted">${esc(r.note)}</span>${pill(r.status)}</li>`).join('')}</ul>` : '';
  }

  function webrtcResult(result, egressIP) {
    if (!result.supported) return { status: 'unsupported', html: `<p class="muted">${esc(t('浏览器不支持或已禁用 WebRTC，网页无法通过它获取你的地址。', 'WebRTC is unsupported or disabled, so pages cannot read addresses through it.'))}</p>` };
    const assessment = assessWebRTC(result.entries, egressIP);
    const summary = {
      leak: t('WebRTC 暴露的公网地址与出口 IP 不同，可能绕过了代理或 VPN。', 'WebRTC exposes a public address different from your egress IP and may bypass your proxy or VPN.'),
      notice: t('未发现与出口不同的同协议公网地址，但 WebRTC 暴露了内网或另一协议的地址。', 'No different public address on the same protocol, but WebRTC exposes a local or other-protocol address.'),
      clean: t('WebRTC 看到的公网地址与出口 IP 一致，或仅出现隐藏的 mDNS 地址。', 'WebRTC sees the same public address as your egress IP, or only hidden mDNS names.'),
      none: t('WebRTC 没有返回任何地址，常见于浏览器隐私设置或网络限制。', 'WebRTC returned no addresses, which is common with privacy settings or network restrictions.'),
    }[assessment.status];
    return { status: assessment.status, html: `<p>${esc(summary)}</p>${addressRows(assessment, egressIP)}` };
  }

  const ICON = {
    globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/>',
    signal: '<path d="M5 12a10 10 0 0 1 14 0M8.5 15.5a5 5 0 0 1 7 0"/><circle cx="12" cy="19" r="1"/>',
    browser: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9h18"/>',
  };
  const icon = name => `<svg class="tool-card-icon" viewBox="0 0 24 24" aria-hidden="true">${ICON[name]}</svg>`;

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
      const card = (key, name, title, body, status = 'pending') => `<section class="card tool-check" data-check="${key}"><header class="tool-check-head">${icon(name)}<h2>${esc(title)}</h2><span data-pill>${pill(status)}</span></header><div class="tool-check-body">${body}</div></section>`;
      const summaryItem = (key, label) => `<li data-summary="${key}"><span>${esc(label)}</span><span data-pill>${pill('pending')}</span></li>`;
      return `<section class="tools-page tool-network" data-tool="network">
        <header class="card tool-hero">
          <div class="tool-hero-text">
            <h1>${esc(t('网络自检', 'Network self-check'))}</h1>
            <p class="muted">${esc(t('查看当前出口 IP、浏览器公开的环境信息，以及 WebRTC 是否暴露其他地址。结果只在本页显示，不会保存或上传。', 'See your egress IP, the environment details your browser exposes, and whether WebRTC reveals other addresses. Results stay on this page and are never stored or uploaded.'))}</p>
          </div>
          <div class="tool-hero-actions">
            <button type="button" class="btn primary" data-tool-run>${esc(t('开始检测', 'Run checks'))}</button>
            <p class="muted small">${esc(t(`WebRTC 检测会连接公共 STUN 服务器 ${STUN_HOST}。`, `The WebRTC check contacts the public STUN server ${STUN_HOST}.`))}</p>
          </div>
          <ul class="tool-summary" aria-label="${esc(t('检测概览', 'Check overview'))}">
            ${summaryItem('egress', t('出口 IP', 'Egress IP'))}${summaryItem('webrtc', 'WebRTC')}${summaryItem('env', t('浏览器环境', 'Browser environment'))}
          </ul>
        </header>
        <div class="tool-check-grid">
          ${card('egress', 'globe', t('出口 IP', 'Egress IP'), `<div class="tool-ip"><span class="tool-ip-value" data-egress>${esc(t('正在读取…', 'Loading…'))}</span><span class="tool-ip-family" data-egress-family></span></div><p class="muted small">${esc(t('社区服务器看到的访问地址。', 'The address the community server sees.'))}</p><div class="tool-actions"><button type="button" class="btn" data-egress-copy hidden>${esc(t('复制', 'Copy'))}</button><button type="button" class="btn" data-egress-retry hidden>${esc(t('重试', 'Retry'))}</button></div>`, 'running')}
          ${card('webrtc', 'signal', t('WebRTC 泄露', 'WebRTC leak'), `<div data-webrtc-result aria-live="polite"><p class="muted">${esc(t('点击「开始检测」后，与出口 IP 对比 WebRTC 能看到的地址。', 'Select “Run checks” to compare the addresses WebRTC can see with your egress IP.'))}</p></div>`)}
          ${card('env', 'browser', t('浏览器环境', 'Browser environment'), `<dl class="tool-rows">
            ${row(t('时区', 'Time zone'), code(env.timeZone))}
            ${row(t('UTC 偏移', 'UTC offset'), code(env.offset))}
            ${row(t('语言偏好', 'Language preferences'), env.languages.length ? env.languages.map(code).join(' ') : code(''))}
            ${row(t('日期与数字格式', 'Date and number format'), code(env.locale))}
          </dl>`, 'info')}
        </div>
        <p class="muted small">${esc(t('DNS 泄露检测需要专用解析服务，暂未提供。', 'DNS leak checks need a dedicated resolver service and are not available yet.'))}</p>
      </section>`;
    }

    setStatus(page, key, status) {
      for (const node of page.querySelectorAll(`[data-check="${key}"] [data-pill], [data-summary="${key}"] [data-pill]`)) node.innerHTML = pill(status);
    }

    loadEgress(page) {
      const value = page.querySelector('[data-egress]');
      const family = page.querySelector('[data-egress-family]');
      const copy = page.querySelector('[data-egress-copy]');
      const retry = page.querySelector('[data-egress-retry]');
      value.textContent = t('正在读取…', 'Loading…');
      value.classList.remove('is-error');
      family.textContent = '';
      copy.hidden = true;
      retry.hidden = true;
      this.setStatus(page, 'egress', 'running');
      this.egress = fetchEgress(root).then(ip => {
        if (this.disposed) return ip;
        value.textContent = ip;
        family.textContent = ipFamily(ip) === 6 ? 'IPv6' : 'IPv4';
        copy.hidden = !root.navigator?.clipboard;
        this.setStatus(page, 'egress', 'ok');
        return ip;
      }).catch(error => {
        if (!this.disposed) {
          value.textContent = egressErrorText(error);
          value.classList.add('is-error');
          retry.hidden = false;
          this.setStatus(page, 'egress', 'error');
        }
        return '';
      });
      return this.egress;
    }

    mount(container) {
      const page = container.querySelector('[data-tool="network"]');
      if (!page) return;
      this.setStatus(page, 'env', 'info');
      this.loadEgress(page);
      page.querySelector('[data-egress-retry]').addEventListener('click', () => this.loadEgress(page));
      page.querySelector('[data-egress-copy]').addEventListener('click', async event => {
        const ip = page.querySelector('[data-egress]').textContent;
        try { await root.navigator.clipboard.writeText(ip); event.target.textContent = t('已复制', 'Copied'); } catch (_) {}
      });
      const run = page.querySelector('[data-tool-run]');
      const output = page.querySelector('[data-webrtc-result]');
      run.addEventListener('click', async () => {
        run.disabled = true;
        this.setStatus(page, 'webrtc', 'running');
        output.innerHTML = `<p class="muted">${esc(t('正在检测…', 'Checking…'))}</p>`;
        try {
          const failed = page.querySelector('[data-egress]').classList.contains('is-error');
          const egressIP = await (failed ? this.loadEgress(page) : this.egress);
          const result = await collectIceAddresses(root);
          if (this.disposed) return;
          const view = webrtcResult(result, egressIP);
          output.innerHTML = view.html;
          this.setStatus(page, 'webrtc', view.status);
        } finally {
          if (!this.disposed) { run.disabled = false; run.textContent = t('重新检测', 'Run again'); }
        }
      });
    }

    dispose() { this.disposed = true; }
  }

  const exports = { View, ipFamily, addressScope, parseCandidate, assessWebRTC, formatOffset, browserEnvironment, collectIceAddresses, fetchEgress, egressErrorText, webrtcResult, EGRESS_ENDPOINT, STUN_HOST };
  if (typeof module === 'object' && module.exports) module.exports = exports;
  else root.MetarTools = Object.freeze(exports);
})(typeof window === 'undefined' ? globalThis : window);
