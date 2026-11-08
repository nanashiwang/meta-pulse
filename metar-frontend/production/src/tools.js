/* Local environment signals; DNS observations live in isolated server memory for at most 90 seconds. */
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
    if (value.includes(':') && /^[0-9a-f:.]+$/i.test(value)) {
      try { new URL(`http://[${value}]/`); return 6; } catch (_) {}
    }
    return 0;
  }

  function addressScope(address) {
    const value = normalizeIP(address);
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
      if (value === '::' || /^ff/.test(value)) return 'reserved';
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
    if (value.startsWith('::ffff:') && ipFamily(value.slice(7)) === 4) return value.slice(7);
    if (ipFamily(value) === 6) {
      const normalized = new URL(`http://[${value}]/`).hostname.slice(1, -1);
      const mapped = /^::ffff:([0-9a-f]{1,4}):([0-9a-f]{1,4})$/.exec(normalized);
      if (mapped) { const a = parseInt(mapped[1], 16), b = parseInt(mapped[2], 16); return [a >> 8, a & 255, b >> 8, b & 255].join('.'); }
      return normalized;
    }
    return value;
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
    else if (local.length || otherFamily.length || (!egressFamily && publicAddresses.length)) status = 'notice';
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
    let offset = '';
    try { const minutes = new env.Date().getTimezoneOffset(); if (Number.isFinite(minutes)) offset = formatOffset(minutes); } catch (_) {}
    return { timeZone, offset, languages, locale };
  }

  function collectIceAddresses(env = root, timeout = WEBRTC_TIMEOUT_MS, signal) {
    const Peer = env.RTCPeerConnection;
    if (typeof Peer !== 'function') return Promise.resolve({ supported: false, entries: [] });
    return new Promise(resolve => {
      const entries = [];
      let peer, done = false, timer;
      const finish = (error = false) => {
        if (done) return;
        done = true;
        env.clearTimeout(timer);
        signal?.removeEventListener('abort', cancel);
        try { peer?.close(); } catch (_) {}
        resolve({ supported: true, entries, error: Boolean(error) });
      };
      const cancel = () => finish(true);
      if (signal?.aborted) return cancel();
      signal?.addEventListener('abort', cancel, { once: true });
      try {
        timer = env.setTimeout(() => finish(), timeout);
        peer = new Peer({ iceServers: [{ urls: STUN_URL }] });
        peer.onicecandidate = event => {
          if (!event.candidate) return finish();
          const parsed = parseCandidate(event.candidate.candidate);
          if (parsed) entries.push(parsed);
        };
        peer.onicegatheringstatechange = () => { if (peer.iceGatheringState === 'complete') finish(); };
        peer.createDataChannel('metar-check');
        peer.createOffer().then(offer => peer.setLocalDescription(offer)).catch(() => finish(true));
      } catch (_) { finish(true); }
    });
  }

  /* The failure reason is shown to the visitor, so a blocked request, a gateway
     without the endpoint and a slow network are distinguishable. */
  class EgressError extends Error {
    constructor(reason, status = 0) { super(reason); this.reason = reason; this.status = status; }
  }

  async function fetchEgress(env = root, timeout = EGRESS_TIMEOUT_MS, signal) {
    const controller = typeof env.AbortController === 'function' ? new env.AbortController() : null;
    const cancel = () => controller?.abort();
    if (signal?.aborted) cancel();
    signal?.addEventListener('abort', cancel, { once: true });
    const timer = controller ? env.setTimeout(() => controller.abort(), timeout) : 0;
    try {
      const response = await env.fetch(EGRESS_ENDPOINT, { cache: 'no-store', credentials: 'omit', headers: { Accept: 'application/json' }, signal: controller?.signal });
      if (!response.ok) throw new EgressError('http', response.status);
      let body;
      try { body = await response.json(); } catch (error) {
        if (error?.name === 'AbortError') throw error;
        throw new EgressError('format');
      }
      const ip = normalizeIP(body?.ip);
      if (!ipFamily(ip)) throw new EgressError('format');
      return ip;
    } catch (error) {
      if (error instanceof EgressError) throw error;
      throw new EgressError(error?.name === 'AbortError' ? 'timeout' : 'network');
    } finally {
      if (controller) env.clearTimeout(timer);
      signal?.removeEventListener('abort', cancel);
    }
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
  const TONE = { ok: 'ok', clean: 'ok', none: 'warn', info: 'info', notice: 'warn', leak: 'risk', error: 'risk', unsupported: 'warn', pending: 'idle', running: 'idle' };
  const pill = status => `<span class="tool-pill" data-tone="${TONE[status] || 'idle'}">${esc(t(...(STATUS_TEXT[status] || STATUS_TEXT.pending)))}</span>`;
  const code = value => value ? `<code>${esc(value)}</code>` : '<span class="muted">—</span>';
  const row = (label, value) => `<div class="tool-row"><dt>${esc(label)}</dt><dd>${value}</dd></div>`;

  function addressRows(assessment, egressIP) {
    const egress = normalizeIP(egressIP);
    const rows = [];
    for (const address of assessment.publicAddresses) {
      const status = assessment.mismatched.includes(address) ? 'leak' : !ipFamily(egress) || assessment.otherFamily.includes(address) ? 'notice' : 'ok';
      const note = status === 'leak' ? t('与出口 IP 不同', 'Differs from egress IP') : status === 'notice' ? (!ipFamily(egress) ? t('无出口基准', 'No egress baseline') : t('另一协议的公网地址', 'Public address on the other protocol')) : address === egress ? t('与出口 IP 一致', 'Matches egress IP') : t('公网地址', 'Public address');
      rows.push({ address, status, note });
    }
    for (const address of assessment.local) rows.push({ address, status: 'notice', note: t('内网地址', 'Local address') });
    if (assessment.hidden) rows.push({ address: '*.local', status: 'ok', note: t('mDNS 已隐藏真实内网地址', 'mDNS hides the real local address') });
    return rows.length ? `<ul class="tool-address-list">${rows.map(r => `<li><code>${esc(r.address)}</code><span class="muted">${esc(r.note)}</span>${pill(r.status)}</li>`).join('')}</ul>` : '';
  }

  function webrtcResult(result, egressIP) {
    if (!result.supported) return { status: 'unsupported', html: `<p class="muted">${esc(t('浏览器不支持或已禁用 WebRTC，网页无法通过它获取你的地址。', 'WebRTC is unsupported or disabled, so pages cannot read addresses through it.'))}</p>` };
    if (result.error) return { status: 'unsupported', html: `<p class="muted">${t('WebRTC 探测失败或已取消，无法完成比较。', 'WebRTC probing failed or was cancelled; comparison is unavailable.')}</p>` };
    const assessment = assessWebRTC(result.entries, egressIP);
    const summary = {
      leak: t('WebRTC 暴露的公网地址与出口 IP 不同，可能绕过了代理或 VPN。', 'WebRTC exposes a public address different from your egress IP and may bypass your proxy or VPN.'),
      notice: t('存在需要核对的地址：可能是内网、另一协议的公网地址，或缺少可比较的出口基准。', 'Addresses need review: local or other-protocol addresses, or a missing egress baseline.'),
      clean: t('WebRTC 看到的公网地址与出口 IP 一致，或仅出现隐藏的 mDNS 地址。', 'WebRTC sees the same public address as your egress IP, or only hidden mDNS names.'),
      none: t('WebRTC 没有返回任何地址，常见于浏览器隐私设置或网络限制。', 'WebRTC returned no addresses, which is common with privacy settings or network restrictions.'),
    }[assessment.status];
    return { status: assessment.status, html: `${!ipFamily(egressIP) ? `<p class="muted">${t('出口基准无法读取，不能判定公网地址是否一致。', 'No egress baseline: public address consistency is unknown.')}</p>` : ''}<p>${esc(summary)}</p>${addressRows(assessment, egressIP)}` };
  }

  // Model v1: the six weights requested for this panel, not an Anthropic model.
  const SIGNAL_WEIGHTS = Object.freeze([30, 24, 20, 10, 8, 8]);
  const FONT_NAMES = Object.freeze(['Microsoft YaHei', 'PingFang SC', 'Hiragino Sans GB', 'SimSun', 'SimHei', 'DengXian', 'FangSong', 'STHeiti', 'STSong', 'HarmonyOS Sans SC', 'Alibaba PuHuiTi']);
  const CLAUDE_HOSTS = Object.freeze(['claude.com', 'claude.ai']);

  function detectFonts(env = root) {
    try {
      const context = env.document?.createElement('canvas').getContext('2d');
      if (!context) return { available: false, fonts: [] };
      const sample = '汉字环境检测 AaBb 0123456789';
      const bases = ['monospace', 'serif', 'sans-serif'];
      const width = font => { context.font = `72px ${font}`; return context.measureText(sample).width; };
      const baseline = bases.map(width);
      if (baseline.some(w => !Number.isFinite(w) || w <= 0)) return { available: false, fonts: [] };
      const fonts = FONT_NAMES.filter(font => bases.some((base, i) => Math.abs(width(`"${font}", ${base}`) - baseline[i]) > 0.1));
      return { available: true, fonts };
    } catch (_) { return { available: false, fonts: [] }; }
  }

  function emojiStyle(nav = {}) {
    const ua = `${nav.userAgent || ''} ${nav.platform || ''}`;
    if (/Mac|iPhone|iPad|iPod/i.test(ua)) return { value: 'Apple style', ratio: 0.25 };
    if (/Windows/i.test(ua)) return { value: 'Windows style', ratio: 0.25 };
    if (/Android/i.test(ua)) return { value: 'Android style', ratio: 0.25 };
    if (/Linux/i.test(ua)) return { value: 'Linux style', ratio: 0 };
    return { value: '', ratio: null };
  }

  function environmentScore(env, fontResult, emoji) {
    const zh = value => /^zh(?:[-_]|$)/i.test(value || '');
    const simplified = value => /^zh(?:[-_](?:CN|Hans)(?:[-_]|$)|$)/i.test(value || '');
    const languages = env.languages || [];
    const ratios = [
      env.timeZone ? (['Asia/Shanghai', 'Asia/Urumqi', 'Asia/Chongqing', 'Asia/Chungking', 'Asia/Harbin'].includes(env.timeZone) ? 1 : 0) : null,
      languages.length ? (simplified(languages[0]) ? 1 : languages.some(zh) ? 0.5 : 0) : null,
      fontResult.available ? (fontResult.fonts.length ? 1 : 0) : null,
      env.locale ? (zh(env.locale) ? 1 : 0) : null,
      env.offset ? (env.offset === 'UTC+08:00' ? 1 : 0) : null,
      emoji.ratio,
    ];
    const values = [env.timeZone, languages.join(', '), fontResult.fonts.join(', '), env.locale, env.offset, emoji.value];
    const signals = ratios.map((ratio, i) => ({ id: i, weight: SIGNAL_WEIGHTS[i], ratio, value: values[i], points: ratio === null ? null : Math.round(ratio * SIGNAL_WEIGHTS[i]), hit: ratio !== null && ratio >= 0.25 }));
    const score = signals.reduce((sum, s) => sum + (s.points || 0), 0);
    const coverage = signals.filter(s => s.ratio !== null).reduce((sum, s) => sum + s.weight, 0);
    return { signals, score, coverage, complete: coverage === 100, band: score <= 30 ? 'low' : score <= 60 ? 'medium' : 'high' };
  }

  function inspectEnvironment(env = root) {
    return environmentScore(browserEnvironment(env), detectFonts(env), emojiStyle(env.navigator));
  }

  function parseTrace(text) {
    const fields = Object.fromEntries(String(text).trim().split(/\r?\n/).filter(line => line.includes('=')).map(line => { const at = line.indexOf('='); return [line.slice(0, at), line.slice(at + 1)]; }));
    const ip = normalizeIP(fields.ip);
    if (!ipFamily(ip)) throw new EgressError('format');
    return { ip, location: /^[A-Z]{2}$/.test(fields.loc || '') ? fields.loc : '', colo: /^[A-Z0-9]{3}$/.test(fields.colo || '') ? fields.colo : '' };
  }

  async function fetchClaudeTrace(host, env = root, timeout = EGRESS_TIMEOUT_MS, signal) {
    if (!CLAUDE_HOSTS.includes(host)) throw new Error('Unsupported probe host');
    const controller = new env.AbortController();
    const cancel = () => controller.abort();
    if (signal?.aborted) cancel();
    signal?.addEventListener('abort', cancel, { once: true });
    const timer = env.setTimeout(() => controller.abort(), timeout);
    try {
      const response = await env.fetch(`https://${host}/cdn-cgi/trace`, { credentials: 'omit', cache: 'no-store', mode: 'cors', referrerPolicy: 'no-referrer', signal: controller.signal });
      if (!response.ok) throw new EgressError('http', response.status);
      return parseTrace(await response.text());
    } catch (error) {
      if (error instanceof EgressError) throw error;
      throw new EgressError(error?.name === 'AbortError' ? 'timeout' : 'network');
    } finally { env.clearTimeout(timer); signal?.removeEventListener('abort', cancel); }
  }

  async function dnsRequest(action, payload, env = root, signal) {
    const controller = new env.AbortController();
    const cancel = () => controller.abort();
    signal?.addEventListener('abort', cancel, {once:true});
    if (signal?.aborted) cancel();
    const timer = env.setTimeout(cancel, 5000);
    try {
      const response = await env.fetch(`/metar/api/tools/dns/${action}`, {
        method:'POST', credentials:'omit', cache:'no-store', signal:controller.signal,
        headers:{'Content-Type':'application/json','X-Metar-DNS-Probe':'1'}, body:JSON.stringify(payload),
      });
      if (!response.ok) throw new EgressError('http', response.status);
      return await response.json();
    } finally {env.clearTimeout(timer);signal?.removeEventListener('abort',cancel);}
  }
  function dnsDelay(env, signal) {
    return new Promise((resolve,reject) => {
      const cancel=()=>{env.clearTimeout(timer);signal?.removeEventListener('abort',cancel);reject(new Error('cancelled'));};
      const timer=env.setTimeout(()=>{signal?.removeEventListener('abort',cancel);resolve();},1000);
      signal?.addEventListener('abort',cancel,{once:true});if(signal?.aborted)cancel();
    });
  }
  async function collectDNS(env = root, signal) {
    const session = await dnsRequest('start', {}, env, signal);
    if (!/^[0-9a-f]{32}$/.test(session?.token || '') || !new RegExp(`^[0-9a-f]{32}\\.dnsprobe\\.metarhub\\.com$`).test(session?.hostname || '')) throw new EgressError('format');
    const trigger = new env.AbortController();
    const cancel=()=>trigger.abort();signal?.addEventListener('abort',cancel,{once:true});
    if(signal?.aborted)cancel();
    try {
      // NXDOMAIN is intentional: the authoritative DNS observation is the result.
      // No credentials/referrer, no content download and no TLS server required.
      env.fetch(`https://${session.hostname}/`,{mode:'no-cors',credentials:'omit',cache:'no-store',referrerPolicy:'no-referrer',signal:trigger.signal}).catch(()=>{});
      let addresses=[];
      for(let i=0;i<12;i++) {
        await dnsDelay(env,signal);
        const result=await dnsRequest('result',{token:session.token},env,signal);
        if (!Array.isArray(result?.resolvers) || result.resolvers.length>32 || result.resolvers.some(ip=>typeof ip!=='string'||!ipFamily(ip))) throw new EgressError('format');
        addresses=[...new Set(result.resolvers.map(normalizeIP))];
        // Allow multiple upstreams to arrive, even after the first result.
        if(addresses.length && i>=3)break;
      }
      return addresses;
    } finally {cancel();signal?.removeEventListener('abort',cancel);}
  }
  function dnsResult(addresses) {
    if(!addresses.length)return {status:'none',html:`<p>${t('未观测到递归查询：可能是 DNS 委派、53 端口、浏览器安全 DNS、缓存或代理解析路径限制。不能据此判定没有泄露。','No recursive query observed. Check delegation, port 53, browser secure DNS, caching or proxy resolution. This does not prove there is no leak.')}</p>`};
    return {status:'info',html:`<p>${t('已观测到本次浏览器探测的 DNS 递归出口：','Observed recursive DNS egress for this browser probe:')}</p><ul class="tool-address-list">${addresses.map(ip=>`<li><code>${esc(ip)}</code><span class="muted">${t('地区未核实 · 不作安全判定','Region unverified · No safety verdict')}</span></li>`).join('')}</ul><p class="small muted">${t('这是权威端看到的最后一级递归出口，可能来自浏览器 DoH、系统 DNS 或代理。仅适用于本轮浏览器探测，不代表全部应用的 DNS 路径。','This is the final recursive hop seen by the authority. It may belong to browser DoH, system DNS or a proxy; it does not describe every application’s DNS path.')}</p>`};
  }

  const SIGNAL_COPY = [
    ['系统时区', 'System time zone', '读取 Intl.DateTimeFormat 的时区，与 Asia/Shanghai、Asia/Urumqi 等时区比对。浏览器覆盖设置可能与终端不同。', 'Reads the Intl time zone and compares it with Asia/Shanghai, Asia/Urumqi and aliases. Browser overrides can differ from the terminal.', '◷'],
    ['浏览器语言', 'Browser languages', '读取 navigator.languages；首选 zh-CN / zh-Hans / zh 得满分，其他位置的中文偏好得半分。', 'Reads navigator.languages. Primary zh-CN / zh-Hans / zh receives full weight; other Chinese preferences receive half.', '◎'],
    ['已安装中文字体', 'Chinese fonts', '通过 Canvas 文字宽度差异探测常见中文字体。字体可跨地区安装，命中不代表用户所在地。', 'Probes common Chinese fonts using canvas text widths. Fonts can be installed anywhere; a match does not identify location.', 'T'],
    ['Intl 区域设置', 'Intl locale', '浏览器用于日期、时间与数字格式化的区域设置；中文区域格式计入本页估分。', 'The locale used for date and number formatting. Chinese locales contribute to this estimate.', '≋'],
    ['时区偏移', 'UTC offset', 'getTimezoneOffset() 是否为 UTC+8。多个国家与地区共用该偏移，因此只是弱信号。', 'Checks whether getTimezoneOffset() is UTC+8. Many countries share this offset, so it is a weak signal.', '◴'],
    ['Emoji 渲染风格', 'Emoji style', '依据 UA / platform 估计系统字体风格，不做国旗屏蔽判定。Apple / Windows / Android 仅计 +2。', 'Estimates OS style from UA / platform, not flag censorship. Apple / Windows / Android contributes only +2.', '☺'],
  ];

  function scoreMarkup(result) {
    const label = result.complete ? t(...({ low: ['低特征分', 'Low signal score'], medium: ['中特征分', 'Medium signal score'], high: ['高特征分', 'High signal score'] }[result.band])) : t('检测不完整', 'Incomplete scan');
    const verdict = result.complete
      ? t(...({
        low: ['当前环境不像 Claude 中国用户环境', 'This environment does not resemble the Claude China-user profile'],
        medium: ['当前环境可能呈现 Claude 中国用户特征', 'This environment may show Claude China-user signals'],
        high: ['当前环境呈现明显 Claude 中国用户特征', 'This environment shows strong Claude China-user signals'],
      }[result.band]))
      : t('部分信号无法读取', 'Some signals are unavailable');
    const hits = result.signals.filter(s => s.hit);
    return `<div class="ce-score-layout" data-band="${result.complete ? result.band : 'unknown'}">
      <div class="ce-gauge" role="img" aria-label="${esc(t(`环境特征分 ${result.score}，已测权重 ${result.coverage}/100`, `Signal score ${result.score}, measured weight ${result.coverage}/100`))}">
        <svg viewBox="0 0 200 200" aria-hidden="true"><circle class="ce-gauge-track" cx="100" cy="100" r="86"/><circle class="ce-gauge-fill" cx="100" cy="100" r="86" pathLength="100" stroke-dasharray="${result.score} 100"/></svg>
        <div><strong>${result.complete ? result.score : '—'}</strong><span>${result.complete ? '/ 100' : t(`已知分 ${result.score}`, `Known score ${result.score}`)}</span></div>
      </div>
      <div class="ce-verdict"><span class="ce-band">${esc(label)}</span><h2>${esc(verdict)}</h2><p>${esc(t('这是中文环境特征的参考分，不是身份识别、封号概率，也不是 Claude 官方风控结果。', 'A reference score for Chinese environment signals—not identity, ban probability, or an official Claude decision.'))}</p><div class="ce-scale"><span>0–30 ${t('低', 'Low')}</span><span>31–60 ${t('中', 'Medium')}</span><span>61–100 ${t('高', 'High')}</span></div></div>
      <div class="ce-hits"><span class="ce-eyebrow">${t('命中的信号', 'MATCHED SIGNALS')}</span><div>${hits.length ? hits.map(s => `<span class="ce-hit">${esc(t(SIGNAL_COPY[s.id][0], SIGNAL_COPY[s.id][1]))} <b>+${s.points}</b></span>`).join('') : `<span class="muted">${t('已读取的项目中暂无命中', 'No matches among measured signals')}</span>`}</div><small>${esc(t(`已读取 ${result.signals.filter(s => s.ratio !== null).length}/6 项 · 已测权重 ${result.coverage}/100 · 单项系数 ≥ 0.25 计为命中`, `${result.signals.filter(s => s.ratio !== null).length}/6 read · measured weight ${result.coverage}/100 · match threshold ≥ 0.25`))}</small></div>
    </div>`;
  }

  function signalsMarkup(result) {
    return result.signals.map(s => {
      const [zh, en, desc, descEn, glyph] = SIGNAL_COPY[s.id];
      return `<li class="ce-signal" data-signal="${s.id}"><span class="ce-signal-icon" aria-hidden="true">${glyph}</span><div class="ce-signal-main"><div class="ce-signal-title"><h3>${esc(t(zh, en))}</h3><span>${t('权重', 'Weight')} ${s.weight}</span>${s.id === 0 ? `<em>${t('时区信号', 'TIME ZONE')}</em>` : ''}</div><p>${esc(t(desc, descEn))}</p>${s.id === 2 && s.value ? `<div class="ce-fonts">${s.value.split(', ').map(font => `<code>${esc(font)}</code>`).join('')}</div>` : ''}</div><div class="ce-signal-value">${s.ratio === null ? `<span class="muted">${t('无法读取', 'Unavailable')}</span>` : s.id === 2 ? t(s.value ? '检测到字体' : '未检测到', s.value ? 'Fonts detected' : 'None detected') : code(s.value)}</div><strong class="ce-points" data-hit="${s.hit ? 'yes' : 'no'}">${s.points === null ? '—' : `+${s.points}`}</strong></li>`;
    }).join('');
  }

  function environmentGuide() {
    const p = (zh, en) => `<p>${t(zh, en)}</p>`;
    const faq = (zh, en, answer, answerEn) => `<details class="ce-faq"><summary>${esc(t(zh, en))}</summary>${p(answer, answerEn)}</details>`;
    return `<section class="ce-section ce-method" id="ce-method"><div class="ce-section-heading"><span class="ce-section-number">03</span><div><h2>${t('检测原理', 'How detection works')}</h2><p>${t('把可观察的信号和不可确认的结论分开。', 'Separate observable signals from unverified conclusions.')}</p></div></div>
      ${p('当 Claude Code 通过 <code>ANTHROPIC_BASE_URL</code> 指向中转端点时，部分公开逆向分析认为，它可能读取系统时区与中转 hostname，再把标记编码进 system prompt 的日期分隔符与相似撇号。下面保留这项研究的解释，但本页未独立验证各版本客户端的行为，也无法确认它如何影响账户判定。', 'Some public reverse-engineering reports describe Claude Code reading the time zone and relay hostname when using <code>ANTHROPIC_BASE_URL</code>, then encoding markers in system-prompt dates and apostrophes. This page explains the claim but has not independently verified client versions or account enforcement.')}
      ${p('网页读不到 Claude Code 能读的全部信息，也读不到终端环境变量。本工具读取浏览器暴露的时区，叠加语言、中文字体、Intl locale、UTC+8 偏移与 Emoji 风格六项信号，按 <strong>30 / 24 / 20 / 10 / 8 / 8</strong> 加权。每项系数为 0–1，贡献分四舍五入后相加；系数 ≥0.25 计为命中。分档：<strong>低 0–30、中 31–60、高 61–100</strong>。未知项目保留为未知，不把缺失结果算成安全。', 'A web page cannot read every CLI signal or terminal environment variables. This local model weights time zone, languages, Chinese fonts, Intl locale, UTC+8 offset and emoji style at <strong>30 / 24 / 20 / 10 / 8 / 8</strong>. Each coefficient is 0–1; rounded contributions are summed. Coefficients ≥0.25 count as matches. Bands: <strong>low 0–30, medium 31–60, high 61–100</strong>. Missing results stay unknown, not safe.')}
      <div class="ce-info-note">${t('隐私边界：环境特征仅在本页计算，不会保存或上传。DNS 服务仅在内存保留随机探测标识与递归出口 IP，最长 90 秒。网络请求会向目标网站和 STUN 服务暴露请求源 IP；本工具不读取 Claude 账号、Cookie、API Key 或对话。', 'Privacy: environment signals are computed locally and never stored or uploaded. DNS probe identifiers and resolver IPs are retained in server memory for up to 90 seconds. Network probes reveal the source IP to their destination and STUN service. No Claude account, cookies, API keys or conversations are read.')}</div>
    </section>
    <section class="ce-section" id="ce-faq"><div class="ce-section-heading"><span class="ce-section-number">04</span><div><h2>${t('常见问题', 'Frequently asked questions')}</h2><p>${t('关于分数、时区和检测范围。', 'About the score, time zone and measurement limits.')}</p></div></div>
      ${faq('Claude 真的会检查我的时区吗？', 'Does Claude really check my time zone?', '公开逆向分析有相关说法，不能据此认定所有版本都如此。本页通过 Intl.DateTimeFormat 读取浏览器报告的时区；通常来自系统，但隐私设置或扩展可以覆盖它，所以不保证与 Claude Code 终端读到的完全一致。', 'Public reverse-engineering reports make this claim, but it is not verified for every version. Intl.DateTimeFormat reports the browser time zone, usually derived from the system. Extensions and privacy settings can override it; it may differ from a terminal.')}
      ${faq('这个分数就是 Claude 的真实判定吗？', 'Is this score Claude’s actual decision?', '不是。它是本页透明、可解释的特征加权，不是官方检测接口，也不是封号概率。中文语言、中文字体或 Apple 设备都不能证明用户的国籍或所在地；网络连通也不代表账号可正常使用。', 'No. It is a transparent local heuristic, not an official API or ban probability. Chinese languages, fonts or an Apple device do not establish nationality or location. Connectivity also does not prove account access.')}
      ${faq('怎么降低分数？', 'How can I lower the score?', '先展开信号看具体命中。调整语言偏好、系统时区或字体可能改变本页分数，但请按实际使用需要设置，不要为了低分删除必要字体。浏览器设置不一定影响终端，低分也不保证稳定或合规。网络隐私应检查代理分流、WebRTC 和 DNS 是否符合你的预期。', 'Review the matched signals first. Language, time-zone or font settings may change the score; configure them for actual needs, not to chase a low number. Browser changes may not affect a CLI, and a low score guarantees neither reliability nor compliance. Check routing, WebRTC and DNS against your intended setup.')}
      ${faq('为什么 Claude 出口显示无法读取？', 'Why is a Claude egress result unavailable?', '本页直接从你的浏览器请求 claude.com 和 claude.ai 的 trace。跨域策略、网络故障、隐私扩展或目标未提供接口都可能使读取失败；这不等于 Claude 不可用，更不能用社区服务器看到的 IP 代替。可以打开对应的原始结果自行查看。', 'The page requests each Claude trace directly from your browser. CORS, connectivity, privacy extensions or an unavailable endpoint can prevent reading. This does not prove Claude is inaccessible; the community egress IP cannot substitute for it. Open the raw trace to inspect manually.')}
      ${faq('没有 WebRTC 地址，或者 DNS 未检测，就是安全了吗？', 'Are no WebRTC addresses or untested DNS safe?', '不是。WebRTC 没有候选地址可能只是被浏览器拦截；没有公网基准时无法判断出口一致性。DNS 必须通过专用唯一域名和权威日志识别递归解析器；本页以一次性随机域名观测实际递归出口，结果只在内存保留最多 90 秒；未取得地址或地区时显示未知，不等于安全。', 'No. ICE gathering may be blocked. Without an egress baseline, consistency is unknown. DNS needs unique hostnames and authoritative observations to identify recursive resolvers. This page observes actual recursive egress using a one-time hostname. Results expire from memory within 90 seconds. Missing addresses or regions remain unknown, not safe.')}
    </section>
    <article class="ce-section ce-guide" id="ce-guide"><div class="ce-section-heading"><span class="ce-section-number">05</span><div><h2>${t('Claude 访问环境及设备特征检测', 'Claude access environment & device signals')}</h2><p>${t('功能模块 · 公开研究 · 防御性设置', 'Modules · public research · defensive settings')}</p></div></div>
      ${p('除了 <a href="https://ippure.com/" target="_blank" rel="noopener noreferrer">IP 风险信息</a>，设备环境也是隐私自检的重要部分。参考 IPPure 的环境面板，本页把网络出口检测与中文环境特征放在一起，辅助排查访问不稳定、出口分流与隐私暴露问题；不把这些特征直接等同于封禁原因或隐私被窃取的证据。', 'Beyond <a href="https://ippure.com/" target="_blank" rel="noopener noreferrer">IP risk information</a>, device settings matter for privacy. Inspired by IPPure’s panel, this page combines egress checks with Chinese environment signals to investigate access, routing and privacy—not to claim these signals prove bans or data theft.')}
      <h3>${t('一、功能模块', '1. Feature modules')}</h3><div class="ce-guide-columns"><section><h4>${t('Claude 连通性及网络出口检测', 'Claude connectivity & egress')}</h4><ul>
      <li>${t('<strong>访问 Claude 出口 IP：</strong>单独请求 claude.com 与 claude.ai 的 trace，展示可读的实时 IP、国家/地区代码与节点。仅表示该 trace 请求路径，不保证登录、对话或 API 的路由相同。', '<strong>Claude egress IP:</strong> request each domain’s trace independently; show readable live IP, country code and edge. This measures the trace path, not necessarily login, chat or API routing.')}</li>
      <li>${t('<strong>WebRTC 隐私泄露隐患：</strong>读取 ICE 候选并与社区访问出口对比。公网地址差异需检查分流；内网与 mDNS 单独标注。没有 IP 归属数据库，不能断言某地址属于中国运营商；探测不等于拦截。', '<strong>WebRTC privacy:</strong> compare ICE addresses with community egress. Public mismatches warrant routing review; local and mDNS addresses are labeled separately. Without geolocation data, no Chinese carrier attribution is made. Detection does not block traffic.')}</li>
      <li>${t('<strong>DNS 泄露检测：</strong>用于检查递归解析器的出口和地区。本页用自有权威 DNS 观测一次性随机域名的递归出口，展示实际 IP；IP 地区未经核实，不据此断言国内或国外安全。', '<strong>DNS leaks:</strong> inspect recursive-resolver egress and location. Self-hosted authoritative DNS observes recursive egress for a one-time hostname and reports IPs. Regions are unverified; no domestic/foreign safety claim is made.')}</li></ul></section><section><h4>${t('中文环境识别', 'Chinese environment signals')}</h4><ol>
      <li>${t('<strong>语言偏好：</strong>检索 zh-CN、zh-Hans、zh 等浏览器语言列表。', '<strong>Languages:</strong> inspect preferences such as zh-CN, zh-Hans and zh.')}</li>
      <li>${t('<strong>国际化时区：</strong>展示 Asia/Shanghai、Asia/Urumqi 等时区与分钟偏移对应的 UTC 值。', '<strong>Time zone:</strong> report the browser time zone and UTC offset.')}</li>
      <li>${t('<strong>Emoji 国旗渲染差异：</strong>参考资料讨论 Canvas 像素探测。本页采用较保守的 UA 系统风格估计，不把单色国旗当作地区或审查证明。', '<strong>Emoji flag rendering:</strong> reference materials discuss canvas pixels. This page uses a conservative UA-based style estimate, not a claim of censorship or location from monochrome flags.')}</li>
      <li>${t('<strong>中文字体指纹：</strong>探测微软雅黑、苹方、等线、仿宋、鸿蒙及阿里普惠体等字体宽度差异。字体不是地区独有资产，隐私保护也可能影响测量。', '<strong>Font fingerprint:</strong> compare widths for YaHei, PingFang, DengXian, FangSong, HarmonyOS and Alibaba fonts. These fonts are not region-exclusive, and privacy defenses may affect measurements.')}</li></ol></section></div>
      <h3>${t('二、Claude Code 与 Unicode 隐写：公开分析在讨论什么？', '2. Claude Code & Unicode: what the reports describe')}</h3>
      <div class="ce-info-note">${t('以下为参考文章的研究主张与示例，非本页对 Anthropic 当前实现的确认；应结合具体客户端版本和原始代码核验。', 'The following describes claims and examples from the reference material, not verification of Anthropic’s current implementation. Verify against a specific client version and original code.')}</div>
      <ol><li>${t('<strong>中转与关键词核验：</strong>分析声称客户端会检查 ANTHROPIC_BASE_URL 的 hostname、中转列表或 AI Lab 关键词。读取系统代理与匹配中转域名是不同机制，不能混为一谈。', '<strong>Relay matching:</strong> reports describe checks against ANTHROPIC_BASE_URL hostnames, relay lists or AI Lab keywords. Inspecting system proxies and matching relay domains are distinct mechanisms.')}</li>
      <li>${t('<strong>时区匹配：</strong>分析提到对 Asia/Shanghai 或 Asia/Urumqi 的检查；网页只能展示浏览器提供的时区，不能证明 CLI 此刻做了什么。', '<strong>Time-zone matching:</strong> reports mention Asia/Shanghai or Asia/Urumqi. A browser reading cannot prove what a CLI is doing.')}</li>
      <li>${t('<strong>高相似度 Unicode 编码：</strong>分析以 Today’s date 那一行的日期分隔符和四种撇号作为编码载体。以下日期只是文档示例，不是当前日期。', '<strong>Similar-looking Unicode:</strong> reports describe date separators and four apostrophe forms in the Today’s date line as carriers. The date below is a documentation example, not today’s date.')}</li></ol>
      <div class="ce-code-example"><span>${t('日期分隔符示例', 'Date separator example')}</span><code>Today's date is 2026-07-03</code><span aria-hidden="true">↓</span><code>Today's date is 2026/07/03</code></div>
      <div class="ce-unicode-grid">${[["'", 'U+0027', '英文单引号', 'Apostrophe'], ['’', 'U+2019', '右单引号', 'Right quote'], ['ʼ', 'U+02BC', '修饰符撇号', 'Modifier apostrophe'], ['ʹ', 'U+02B9', '修饰符 prime', 'Modifier prime']].map(([symbol, point, zh, en]) => `<div><b>${esc(symbol)}</b><code>${point}</code><span>${esc(t(zh, en))}</span></div>`).join('')}</div>
      ${p('这类字符变化可能不容易被肉眼察觉，但并不会让原始请求字节对抓包工具不可见。若要验证，应检查原始 Unicode 码点与客户端版本，而不是仅凭界面字形断言信息外传。', 'Such changes can be hard to notice visually, but do not make request bytes invisible to capture tools. Inspect raw code points and the client version rather than inferring exfiltration from appearance alone.')}
      <h3>${t('三、防御性微调及技术规避建议', '3. Defensive configuration & limitations')}</h3><ol>
      <li>${t('<strong>系统时区与浏览器覆盖：</strong>参考资料建议将 Asia/Shanghai 改为同属 UTC+8 的 Asia/Singapore。这样会改变时区名称信号，但 UTC+8 项仍会命中；只改浏览器不一定影响 Claude Code。请按实际需要配置，并留意日历、日志和服务地区要求，不能据此保证账户安全。', '<strong>System time zone vs browser override:</strong> the reference suggests Asia/Singapore instead of Asia/Shanghai. This changes the named-zone signal but retains UTC+8. A browser-only change may not affect Claude Code. Consider calendars, logs and service-region requirements; no account-safety guarantee follows.')}</li>
      <li>${t('<strong>TUN 模式与软路由：</strong>透明路由可能改变进程看到的系统代理配置，但不能保证隐藏中转 hostname、环境变量、DNS 路径或出口 IP。启用后应重新核对 WebRTC、DNS 和各域名分流，不要把“能出海”当作“不可检测”。', '<strong>TUN mode and router routing:</strong> transparent routing may change visible system-proxy settings, but cannot guarantee hiding relay hostnames, environment variables, DNS paths or egress IPs. Recheck WebRTC, DNS and per-domain routing; reachability does not mean invisibility.')}</li></ol>
      <footer class="ce-guide-footer">${t('参考与说明', 'References')} · <a href="https://ippure.com/claude.html" target="_blank" rel="noopener noreferrer">IPPure</a> · <a href="https://github.com/LinXiaoTao/FuckClaude" target="_blank" rel="noopener noreferrer">${t('公开检测项目', 'Public detection project')}</a><span>${t('本页独立实现 · 非 Anthropic 官方工具', 'Independent implementation · Not an Anthropic tool')}</span></footer>
    </article>`;
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
      this.egressGeneration = 0;
      this.controller = typeof root.AbortController === 'function' ? new root.AbortController() : null;
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
      const result = inspectEnvironment(root);
      const card = (key, title, body, status = 'pending') => `<section class="ce-network-card" data-check="${key}"><header><h3>${esc(title)}</h3><span data-pill>${pill(status)}</span></header>${body}</section>`;
      return `<section class="tools-page tool-network ce-page" data-tool="network">
        <header class="ce-heading"><div><span class="ce-eyebrow">METAR LAB / ENVIRONMENT CHECK</span><h1>${t('Claude 访问环境检测', 'Claude access environment check')}</h1><p>${t('看清网络出口，读懂设备信号。让每一个检测结果都有据可查。', 'See your network path. Understand your device signals. Inspect the evidence behind each result.')}</p></div><span class="ce-local-badge">${t('本地计算 · 无需登录', 'Local analysis · No sign-in')}</span></header>
        <nav class="ce-nav" aria-label="${t('本页导航', 'On this page')}"><a href="#ce-overview">${t('检测概览', 'Overview')}</a><a href="#ce-network">${t('网络与隐私', 'Network')}</a><a href="#ce-signals">${t('环境信号', 'Signals')}</a><a href="#ce-method">${t('检测原理', 'Method')}</a><a href="#ce-faq">FAQ</a><a href="#ce-guide">${t('完整说明', 'Guide')}</a></nav>
        <section class="ce-overview" id="ce-overview"><header class="ce-overview-head"><span>${t('Claude 中文用户环境检测', 'Chinese environment signal check')}</span><span class="ce-model-label">HEURISTIC v1</span></header><div data-score aria-live="polite">${scoreMarkup(result)}</div><footer class="ce-scan-footer"><div><button type="button" class="ce-scan-button" data-tool-run>${t('开始完整检测', 'Run full scan')} <span aria-hidden="true">↗</span></button><span data-scan-time role="status">${t('本地环境已读取 · 网络探测等待开始', 'Local signals read · Network probes not started')}</span></div><p>${t(`点击后将连接 claude.com、claude.ai 与 ${STUN_HOST}，并向 *.dnsprobe.metarhub.com 发起 DNS 探测；目标服务会看到请求源 IP。`, `Scanning contacts claude.com, claude.ai and ${STUN_HOST}, and triggers DNS for *.dnsprobe.metarhub.com; those services see the request source IP.`)}</p></footer></section>
        <section class="ce-section" id="ce-network"><div class="ce-section-heading"><span class="ce-section-number">01</span><div><h2>${t('Claude 连通性及网络出口', 'Claude connectivity & egress')}</h2><p>${t('实时请求 · 按域名独立检测 · 未知不等于安全', 'Live requests · Separate domain probes · Unknown is not safe')}</p></div></div><div class="ce-network-grid">
          ${CLAUDE_HOSTS.map((host, i) => card(`claude-${i}`, `${host} ${t('出口 IP', 'egress IP')}`, `<div data-claude-result="${i}"><p class="ce-probe-value">${t('等待检测', 'Not tested')}</p><p class="muted small">${t('读取浏览器直连该域名的 trace 结果', 'Read this domain’s trace directly from your browser')}</p></div><a class="ce-raw-link" href="https://${host}/cdn-cgi/trace" target="_blank" rel="noopener noreferrer">${t('打开原始结果', 'Open raw trace')} ↗</a>`)).join('')}
          ${card('egress', t('当前社区访问出口', 'Community egress'), `<div class="tool-ip"><span class="tool-ip-value" data-egress>${t('正在读取…', 'Loading…')}</span><span class="tool-ip-family" data-egress-family></span></div><p class="muted small">${t('社区服务器看到的地址，不代表 Claude 域名出口。', 'The address seen by this community, not the Claude domains.')}</p><div class="tool-actions"><button type="button" class="btn" data-egress-copy hidden>${t('复制', 'Copy')}</button><button type="button" class="btn" data-egress-retry hidden>${t('重试', 'Retry')}</button></div>`, 'running')}
          ${card('webrtc', t('WebRTC 隐私泄露隐患', 'WebRTC privacy exposure'), `<div data-webrtc-result aria-live="polite"><p class="muted">${t('点击上方按钮，通过 ICE 候选检查是否出现其他公网或内网地址。', 'Run a scan to inspect ICE addresses for additional public or local addresses.')}</p></div>`)}
        </div><section class="ce-dns" data-check="dns"><header><div><span class="ce-eyebrow">DNS LEAK</span><h3>${t('DNS 区域泄露隐患', 'DNS resolver exposure')}</h3></div><span data-pill>${pill('pending')}</span></header><div data-dns-result aria-live="polite"><p>${t('点击完整检测，通过一次性随机域名观测实际递归 DNS 出口。','Run a full scan to observe recursive DNS egress using a one-time hostname.')}</p></div><span class="ce-dns-caption">${t('自有权威 DNS · 结果保留最多 90 秒 · IP 地区未核实', 'Self-hosted authoritative DNS · Results expire within 90 seconds · IP regions unverified')}</span></section></section>
        <section class="ce-section" id="ce-signals" data-check="env"><div class="ce-section-heading"><span class="ce-section-number">02</span><div><h2>${t('检测哪些信号', 'Which signals are checked')}</h2><p>${t('六项环境指纹，加权得出 0–100 参考分。点击重新扫描可更新。', 'Six environment signals form a 0–100 reference score. Scan again to refresh.')}</p></div><span data-pill>${pill('info')}</span></div><ol class="ce-signal-list" data-signals>${signalsMarkup(result)}</ol></section>
        ${environmentGuide()}
      </section>`;
    }

    setStatus(page, key, status) {
      for (const node of page.querySelectorAll(`[data-check="${key}"] [data-pill], [data-summary="${key}"] [data-pill]`)) node.innerHTML = pill(status);
    }

    loadEgress(page) {
      const generation = ++this.egressGeneration;
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
      this.egress = fetchEgress(root, EGRESS_TIMEOUT_MS, this.controller?.signal).then(ip => {
        if (this.disposed || generation !== this.egressGeneration) return ip;
        value.textContent = ip;
        family.textContent = ipFamily(ip) === 6 ? 'IPv6' : 'IPv4';
        copy.hidden = !root.navigator?.clipboard;
        this.setStatus(page, 'egress', 'ok');
        return ip;
      }).catch(error => {
        if (!this.disposed && generation === this.egressGeneration) {
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
      this.setStatus(page, 'env', inspectEnvironment(root).complete ? 'info' : 'notice');
      this.loadEgress(page);
      page.querySelector('[data-egress-retry]').addEventListener('click', () => this.loadEgress(page));
      page.querySelector('[data-egress-copy]').addEventListener('click', async event => {
        const ip = page.querySelector('[data-egress]').textContent;
        try { await root.navigator.clipboard.writeText(ip); event.target.textContent = t('已复制', 'Copied'); } catch (_) {}
      });
      const run = page.querySelector('[data-tool-run]');
      const output = page.querySelector('[data-webrtc-result]');
      run.addEventListener('click', async () => {
        if (run.disabled || this.disposed) return;
        run.disabled = true;
        run.textContent = t('正在扫描…', 'Scanning…');
        const result = inspectEnvironment(root);
        page.querySelector('[data-score]').innerHTML = scoreMarkup(result);
        page.querySelector('[data-signals]').innerHTML = signalsMarkup(result);
        this.setStatus(page, 'env', result.complete ? 'info' : 'notice');
        this.setStatus(page, 'webrtc', 'running');
        output.innerHTML = `<p class="muted">${t('正在检测…', 'Checking…')}</p>`;
        page.querySelector('[data-scan-time]').textContent = t('网络探测进行中…', 'Network probes in progress…');
        try {
          const webRTC = Promise.all([this.loadEgress(page), collectIceAddresses(root, WEBRTC_TIMEOUT_MS, this.controller?.signal)]).then(([ip, ice]) => {
            if (this.disposed) return;
            const view = webrtcResult(ice, ip);
            output.innerHTML = view.html;
            this.setStatus(page, 'webrtc', view.status);
          });
          const probes = CLAUDE_HOSTS.map(async (host, i) => {
            const target = page.querySelector(`[data-claude-result="${i}"]`);
            this.setStatus(page, `claude-${i}`, 'running');
            target.innerHTML = `<p class="ce-probe-value">${t('检测中…', 'Checking…')}</p>`;
            try {
              const trace = await fetchClaudeTrace(host, root, EGRESS_TIMEOUT_MS, this.controller?.signal);
              if (this.disposed) return;
              target.innerHTML = `<p class="ce-probe-value">${esc(trace.ip)}</p><p class="muted small">${esc(t('国家/地区', 'Country/region'))}: ${esc(trace.location || '—')} · ${esc(t('节点', 'Edge'))}: ${esc(trace.colo || '—')}</p><p class="small">${t('trace 可读取，不代表账户或对话可用。', 'Readable trace, not proof of account or chat access.')}</p>`;
              this.setStatus(page, `claude-${i}`, 'info');
            } catch (error) {
              if (this.disposed) return;
              target.innerHTML = `<p class="ce-probe-value ce-probe-unknown">${t('无法读取', 'Unavailable')}</p><p class="muted small">${esc(error.reason === 'network' ? t('可能受跨域策略、隐私扩展或网络限制；不等于目标不可访问。', 'CORS, privacy extensions or connectivity may prevent reading; this does not prove the site is unreachable.') : egressErrorText(error))}</p>`;
              this.setStatus(page, `claude-${i}`, 'unsupported');
            }
          });
          this.setStatus(page, 'dns', 'running');
          const dnsOutput=page.querySelector('[data-dns-result]');
          dnsOutput.innerHTML=`<p>${t('正在等待随机域名的递归查询…','Waiting for recursive queries to the random hostname…')}</p>`;
          const dnsProbe=collectDNS(root,this.controller?.signal).then(addresses=>{
            if(this.disposed)return;
            const view=dnsResult(addresses);dnsOutput.innerHTML=view.html;this.setStatus(page,'dns',view.status);
          }).catch(error=>{
            if(this.disposed)return;
            dnsOutput.innerHTML=`<p>${esc(t('DNS 探测未完成。请稍后重试；若持续失败，管理员需检查探测服务、NS 委派和 UDP/TCP 53。','DNS probing did not complete. Retry later; persistent failures require checking the probe service, NS delegation and UDP/TCP 53.'))}</p><p class="small muted">${esc(egressErrorText(error))}</p>`;
            this.setStatus(page,'dns','unsupported');
          });
          await Promise.allSettled([webRTC, ...probes, dnsProbe]);
        } finally {
          if (!this.disposed) {
            run.disabled = false;
            run.textContent = t('重新扫描 ↗', 'Scan again ↗');
            page.querySelector('[data-scan-time]').textContent = t('本轮扫描结束 · ', 'Scan finished · ') + new root.Date().toLocaleTimeString();
          }
        }
      });
    }

    dispose() { this.disposed = true; this.controller?.abort(); }
  }

  const exports = { View, collectDNS, dnsRequest, dnsResult, environmentScore, detectFonts, emojiStyle, inspectEnvironment, parseTrace, fetchClaudeTrace, SIGNAL_WEIGHTS, FONT_NAMES, scoreMarkup, signalsMarkup, ipFamily, addressScope, parseCandidate, assessWebRTC, formatOffset, browserEnvironment, collectIceAddresses, fetchEgress, egressErrorText, webrtcResult, EGRESS_ENDPOINT, STUN_HOST };
  if (typeof module === 'object' && module.exports) module.exports = exports;
  else root.MetarTools = Object.freeze(exports);
})(typeof window === 'undefined' ? globalThis : window);
