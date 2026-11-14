const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function load(language = 'zh_CN', extra = {}) {
  const window = { MetarI18n: { getLanguage: () => language }, Intl, Date, setTimeout, clearTimeout, navigator: { languages: ['en-US', 'fr'] }, ...extra };
  vm.runInNewContext(fs.readFileSync(require.resolve('../src/tools.js'), 'utf8'), { window, URL });
  return window.MetarTools;
}

test('classifies address scopes and families', () => {
  const tools = load();
  assert.equal(tools.ipFamily('203.0.113.9'), 4);
  assert.equal(tools.ipFamily('2001:db8::1'), 6);
  assert.equal(tools.ipFamily('999.1.1.1'), 0);
  assert.equal(tools.addressScope('192.168.1.5'), 'private');
  assert.equal(tools.addressScope('100.64.0.1'), 'private');
  assert.equal(tools.addressScope('169.254.1.1'), 'link-local');
  assert.equal(tools.addressScope('fe80::1'), 'link-local');
  assert.equal(tools.addressScope('0b5e7b4a-1111.local'), 'mdns');
  assert.equal(tools.addressScope('8.8.8.8'), 'public');
});

test('parses ICE candidate lines and rejects malformed ones', () => {
  const tools = load();
  assert.deepEqual({...tools.parseCandidate('candidate:842163049 1 udp 1677729535 198.51.100.7 61665 typ srflx raddr 0.0.0.0 rport 0 generation 0')}, {address: '198.51.100.7', protocol: 'udp', type: 'srflx', scope: 'public'});
  assert.equal(tools.parseCandidate('candidate:1 1 udp 2122260223 abc.local 5000 typ host').scope, 'mdns');
  assert.equal(tools.parseCandidate('garbage'), null);
  assert.equal(tools.parseCandidate(''), null);
});

test('flags a same-family public address that differs from egress as a leak', () => {
  const tools = load();
  const c = (address) => tools.parseCandidate(`candidate:1 1 udp 1 ${address} 1 typ srflx`);
  assert.equal(tools.assessWebRTC([c('198.51.100.7')], '203.0.113.9').status, 'leak');
  assert.deepEqual([...tools.assessWebRTC([c('198.51.100.7')], '203.0.113.9').mismatched], ['198.51.100.7']);
  assert.equal(tools.assessWebRTC([c('203.0.113.9'), c('203.0.113.9')], '203.0.113.9').status, 'clean');
  assert.equal(tools.assessWebRTC([c('203.0.113.9')], '::ffff:203.0.113.9').status, 'clean');
  assert.equal(tools.assessWebRTC([c('2001:db8::5')], '203.0.113.9').status, 'notice');
  assert.equal(tools.assessWebRTC([c('192.168.1.2')], '203.0.113.9').status, 'notice');
  assert.equal(tools.assessWebRTC([c('abc.local')], '203.0.113.9').status, 'clean');
  assert.equal(tools.assessWebRTC([], '203.0.113.9').status, 'none');
  // Unknown egress never invents a mismatch.
  assert.equal(tools.assessWebRTC([c('198.51.100.7')], '').status, 'notice');
});

test('formats UTC offsets from getTimezoneOffset minutes', () => {
  const tools = load();
  assert.equal(tools.formatOffset(-480), 'UTC+08:00');
  assert.equal(tools.formatOffset(300), 'UTC-05:00');
  assert.equal(tools.formatOffset(-330), 'UTC+05:30');
  assert.equal(tools.formatOffset(0), 'UTC+00:00');
});

test('hub lists the shared tool inventory and escapes it', () => {
  const tools = load('en_US');
  const html = new tools.View([{path: '/tools/network', zh: '网络自检', en: 'Net <b>', descriptionZh: '', descriptionEn: 'desc'}]).page('/tools');
  assert.match(html, /href="\/tools\/network"/);
  assert.match(html, /Net &lt;b&gt;/);
});

test('complete Claude panel preserves privacy, honest unavailable states and navigation', () => {
  for (const lang of ['zh_CN', 'en_US']) {
    const html = new (load(lang).View)([]).page('/tools/network');
    assert.match(html, /stun\.cloudflare\.com/);
    assert.match(html, /claude\.com/);
    assert.match(html, /claude\.ai/);
    assert.match(html, /U\+0027/);
    assert.match(html, /U\+2019/);
    assert.match(html, /ANTHROPIC_BASE_URL/);
    assert.match(html, /data-egress-retry hidden/);
    assert.equal((html.match(/data-signal="/g) || []).length, 6);
    assert.equal((html.match(/class="ce-faq"/g) || []).length, 5);
    for (const id of ['overview', 'network', 'signals', 'method', 'faq', 'guide']) assert.match(html, new RegExp(`id="ce-${id}"`));
    for (const key of ['egress', 'webrtc', 'env', 'dns', 'claude-0', 'claude-1']) assert.match(html, new RegExp(`data-check="${key}"`));
    assert.doesNotMatch(html, /localStorage|sessionStorage|sendBeacon/);
    assert.match(html, lang === 'zh_CN' ? /不会保存或上传/ : /never stored or uploaded/);
    assert.match(html, lang === 'zh_CN' ? /一次性随机域名/ : /one-time hostname/);
  }
});

test('WebRTC result escapes addresses, labels each one and handles unsupported browsers', () => {
  const tools = load();
  const unsupported = tools.webrtcResult({supported: false, entries: []}, '203.0.113.9');
  assert.equal(unsupported.status, 'unsupported');
  assert.match(unsupported.html, /不支持或已禁用/);
  const leak = tools.webrtcResult({supported: true, entries: [{address: '198.51.100.7', scope: 'public'}, {address: '<b>.local', scope: 'mdns'}]}, '203.0.113.9');
  assert.equal(leak.status, 'leak');
  assert.match(leak.html, /<code>198\.51\.100\.7<\/code><span class="muted">与出口 IP 不同<\/span><span class="tool-pill" data-tone="risk">/);
  assert.match(leak.html, /mDNS 已隐藏/);
  assert.doesNotMatch(leak.html, /<b>/);
  const same = tools.webrtcResult({supported: true, entries: [{address: '203.0.113.9', scope: 'public'}]}, '203.0.113.9');
  assert.equal(same.status, 'clean');
  assert.match(same.html, /与出口 IP 一致/);
});

test('egress lookup omits credentials and reports why it failed', async () => {
  let seen;
  const ok = load('zh_CN', {AbortController, fetch: async (url, options) => { seen = {url, options}; return {ok: true, json: async () => ({ip: '203.0.113.9'})}; }});
  assert.equal(await ok.fetchEgress(), '203.0.113.9');
  assert.equal(seen.url, '/metar/api/tools/ip');
  assert.equal(seen.options.credentials, 'omit');
  assert.equal(seen.options.cache, 'no-store');
  assert.ok(seen.options.signal);
  const reason = async (env, timeout) => { try { await load('zh_CN', env).fetchEgress(undefined, timeout); } catch (error) { return [error.reason, error.status]; } return null; };
  assert.deepEqual(await reason({fetch: async () => ({ok: true, json: async () => ({ip: '<script>'})})}), ['format', 0]);
  assert.deepEqual(await reason({fetch: async () => ({ok: true, json: async () => { throw SyntaxError('html'); }})}), ['format', 0]);
  assert.deepEqual(await reason({fetch: async () => ({ok: false, status: 404})}), ['http', 404]);
  assert.deepEqual(await reason({fetch: async () => { throw TypeError('blocked'); }}), ['network', 0]);
  const hang = {AbortController, fetch: (url, options) => new Promise((_, reject) => options.signal.addEventListener('abort', () => reject(Object.assign(Error('aborted'), {name: 'AbortError'}))))};
  assert.deepEqual(await reason(hang, 10), ['timeout', 0]);
  const tools = load();
  assert.match(tools.egressErrorText({reason: 'http', status: 502}), /502/);
  assert.match(tools.egressErrorText({reason: 'network'}), /拦截/);
});

test('candidate collection closes the peer and reports unsupported WebRTC', async () => {
  const none = await load().collectIceAddresses();
  assert.equal(none.supported, false);
  assert.equal(none.entries.length, 0);
  let closed = false, config;
  class Peer {
    constructor(value) { config = value; }
    createDataChannel() {}
    async createOffer() { return {}; }
    async setLocalDescription() {
      this.onicecandidate({candidate: {candidate: 'candidate:1 1 udp 1 198.51.100.7 1 typ srflx'}});
      this.onicecandidate({candidate: null});
    }
    close() { closed = true; }
  }
  const tools = load('zh_CN', {RTCPeerConnection: Peer});
  const result = await tools.collectIceAddresses();
  assert.equal(result.supported, true);
  assert.equal(result.entries[0].address, '198.51.100.7');
  assert.equal(closed, true);
  assert.equal(config.iceServers[0].urls, 'stun:stun.cloudflare.com:3478');
});

test('six-signal model has exact weights, complete scoring and match threshold', () => {
  const tools = load();
  const env = {timeZone: 'Asia/Shanghai', languages: ['zh-CN'], locale: 'zh-CN', offset: 'UTC+08:00'};
  const full = tools.environmentScore(env, {available: true, fonts: ['PingFang SC']}, {ratio: 0.25, value: 'Apple style'});
  assert.equal(full.score, 94);
  assert.equal(full.coverage, 100);
  assert.equal(full.signals.filter(s => s.hit).length, 6);
  assert.equal(full.band, 'high');
  assert.equal(full.complete, true);
  assert.deepEqual(Array.from(full.signals, s => s.weight), [30, 24, 20, 10, 8, 8]);
  assert.match(tools.scoreMarkup(full), /当前环境呈现明显 Claude 中国用户特征/);
  const byBand = band => {
    const template = [
      {timeZone:'Asia/Shanghai',languages:['en'],locale:'en',offset:'UTC+00:00'},
      {timeZone:'Asia/Shanghai',languages:['en'],locale:'en',offset:'UTC+00:00'},
      {timeZone:'Asia/Shanghai',languages:['en'],locale:'zh',offset:'UTC+00:00'},
      {timeZone:'Asia/Shanghai',languages:['en'],locale:'zh',offset:'UTC+00:00'},
    ];
    const input = template[band];
    return tools.environmentScore(input, {available:true,fonts:band >= 2 ? ['Test'] : []}, {ratio:band >= 2 ? 0.125 : band % 2 ? 0.125 : 0,value:'Test'});
  };
  assert.match(tools.scoreMarkup(byBand(0)), /当前环境不像 Claude 中国用户环境/);
  assert.match(tools.scoreMarkup(byBand(1)), /当前环境可能呈现 Claude 中国用户特征/);
  assert.match(tools.scoreMarkup(byBand(2)), /当前环境呈现明显 Claude 中国用户特征/);
  const enHigh = load('en_US').scoreMarkup(full);
  assert.match(enHigh, /This environment shows strong Claude China-user signals/);
  const language = value => tools.environmentScore({...env, languages: value}, {available: true, fonts: []}, {ratio: 0, value: 'Linux'}).signals[1].points;
  assert.equal(language(['en-US', 'zh-CN']), 12);
  assert.equal(language(['zh-TW']), 12);
  assert.equal(language(['zh-Hans-CN']), 24);
  assert.equal(language(['zh']), 24);
  assert.equal(language(['en-US']), 0);
  const singapore = tools.environmentScore({...env, timeZone: 'Asia/Singapore'}, {available: true, fonts: []}, {ratio: 0, value: 'Linux'});
  assert.equal(singapore.signals[0].points, 0);
  assert.equal(singapore.signals[4].points, 8);
});

test('score boundaries, unknown inputs and partial readings never report safe', () => {
  const tools = load();
  const make = (timeZone, languages, fonts, locale, offset, ratio) => tools.environmentScore({timeZone, languages, locale, offset}, {available: true, fonts}, {value: 'Test', ratio});
  const empty = tools.environmentScore({languages: []}, {available: false, fonts: []}, {value: '', ratio: null});
  assert.equal(empty.complete, false);
  assert.equal(empty.coverage, 0);
  assert.equal(empty.signals.every(s => s.points === null), true);
  assert.match(tools.scoreMarkup(empty), /检测不完整/);
  assert.doesNotMatch(tools.scoreMarkup(empty), /Claude 中国用户环境|Claude 中国用户特征/);
  assert.doesNotMatch(tools.scoreMarkup(empty), /低特征分/);
  const low = make('Asia/Shanghai', ['en'], [], 'en', 'UTC+00:00', 0);
  assert.equal(low.score, 30); assert.equal(low.band, 'low');
  const mid = make('Asia/Shanghai', ['en'], [], 'en', 'UTC+00:00', 0.125);
  assert.equal(mid.score, 31); assert.equal(mid.band, 'medium');
  assert.equal(mid.signals[5].hit, false);
  const sixty = make('Asia/Shanghai', ['en'], ['Test'], 'zh', 'UTC+00:00', 0);
  assert.equal(sixty.score, 60); assert.equal(sixty.band, 'medium');
  const high = make('Asia/Shanghai', ['en'], ['Test'], 'zh', 'UTC+00:00', 0.125);
  assert.equal(high.score, 61); assert.equal(high.band, 'high');
  assert.equal(make('Asia/Shanghai', ['zh'], ['Test'], 'zh', 'UTC+08:00', 1).score, 100);
  const bad = tools.environmentScore({timeZone: '<img src=x onerror=x>', languages: ['<script>'], locale: '<b>', offset: ''}, {available: false, fonts: []}, {ratio: null, value: ''});
  assert.doesNotMatch(tools.signalsMarkup(bad), /<img|<script>|<b>/);
  assert.match(tools.signalsMarkup(bad), /&lt;img/);
  const unavailable = load('zh_CN', {Intl: {DateTimeFormat() {throw Error('blocked');}}, Date: class {getTimezoneOffset() {return NaN;}}, navigator: {}}).inspectEnvironment();
  assert.equal(unavailable.coverage, 0);
});

test('font probing uses width differences, handles unavailable canvas and never uploads pixels', () => {
  const tools = load();
  assert.equal(tools.detectFonts({}).available, false);
  assert.equal(tools.detectFonts({document: {createElement() {throw Error('blocked');}}}).available, false);
  const context = {font: '', measureText() {return {width: this.font.includes('"PingFang SC"') ? 112 : 100};}};
  const probe = tools.detectFonts({document: {createElement: () => ({getContext: () => context})}});
  assert.equal(probe.available, true);
  assert.deepEqual(Array.from(probe.fonts), ['PingFang SC']);
  assert.equal(tools.emojiStyle({userAgent:'Macintosh'}).ratio, 0.25);
  assert.equal(tools.emojiStyle({userAgent:'Windows NT'}).value, 'Windows style');
  assert.equal(tools.emojiStyle({userAgent:'Linux Android 15'}).value, 'Android style');
  assert.equal(tools.emojiStyle({userAgent:'Linux'}).ratio, 0);
  assert.equal(tools.emojiStyle({}).ratio, null);
});

test('Claude probes use separate fixed hosts, omit credentials, and reject invalid traces', async () => {
  const seen = [];
  const tools = load('zh_CN', {AbortController, fetch: async (url, options) => {seen.push({url, options}); return {ok: true, text: async () => 'ip=198.51.100.8\nloc=US\ncolo=LAX\n'};}});
  for (const host of ['claude.com','claude.ai']) {
    const result = await tools.fetchClaudeTrace(host);
    assert.equal(result.ip, '198.51.100.8');
    assert.equal(result.location, 'US');
  }
  assert.equal(seen[0].url, 'https://claude.com/cdn-cgi/trace');
  assert.equal(seen[1].url, 'https://claude.ai/cdn-cgi/trace');
  assert.equal(seen[0].options.credentials, 'omit');
  assert.equal(seen[0].options.mode, 'cors');
  assert.equal(seen[0].options.referrerPolicy, 'no-referrer');
  await assert.rejects(tools.fetchClaudeTrace('example.org'));
  assert.equal(seen.length, 2);
  for (const text of ['<html>blocked</html>', 'ip=:::', 'ip=999.1.1.1', 'loc=US']) assert.throws(() => tools.parseTrace(text));
  assert.equal(tools.parseTrace('ip=2001:db8::1\nloc=<script>\ncolo=<img>').location, '');
  assert.equal(tools.ipFamily('::::'), 0);
  assert.equal(tools.addressScope('::ffff:c0a8:101'), 'private');
  assert.equal(tools.addressScope('::'), 'reserved');
  assert.equal(tools.addressScope('ff02::1'), 'reserved');
});

test('Claude failure and response-body timeouts remain unknown instead of borrowing community IP', async () => {
  const fail = async fetch => {
    const tools = load('zh_CN', {AbortController, fetch});
    try { await tools.fetchClaudeTrace('claude.ai', undefined, 5); } catch(error) { return error.reason; }
  };
  assert.equal(await fail(async () => {throw TypeError('CORS');}), 'network');
  assert.equal(await fail(async () => ({ok:false, status:403})), 'http');
  assert.equal(await fail(async () => ({ok:true, text:async () => '<html>challenge</html>'})), 'format');
  const hangingBody = async (_, options) => ({ok:true, text:() => new Promise((resolve, reject) => options.signal.addEventListener('abort', () => reject(Object.assign(Error('timeout'), {name:'AbortError'}))))});
  assert.equal(await fail(hangingBody), 'timeout');
  const tools = load('zh_CN', {AbortController, fetch:async (_, options) => {
    const response = await hangingBody(_, options); return {...response, json:response.text};
  }});
  await assert.rejects(tools.fetchEgress(undefined, 5), e => e.reason === 'timeout');
});

test('WebRTC error and navigation cancellation close the peer and never report no leak', async () => {
  let closed = false;
  class Peer {
    createDataChannel() {}
    async createOffer() {throw Error('blocked');}
    close() {closed = true;}
  }
  const tools = load('zh_CN', {RTCPeerConnection:Peer});
  const result = await tools.collectIceAddresses();
  assert.equal(closed, true);
  assert.equal(result.error, true);
  assert.equal(tools.webrtcResult(result, '203.0.113.9').status, 'unsupported');
  closed = false;
  class WaitingPeer extends Peer {async createOffer() {return {};} async setLocalDescription() {}}
  const waiting = load('zh_CN', {RTCPeerConnection:WaitingPeer});
  const controller = new AbortController();
  const pending = waiting.collectIceAddresses(undefined, 1000, controller.signal);
  controller.abort();
  assert.equal((await pending).error, true);
  assert.equal(closed, true);
  const noBaseline = tools.webrtcResult({supported:true,entries:[{address:'198.51.100.7',scope:'public'}]}, '');
  assert.equal(noBaseline.status, 'notice');
  assert.match(noBaseline.html, /无出口基准/);
});

test('production CSP permits only fixed Claude trace paths on both shell entries', () => {
  const nginx = fs.readFileSync(require.resolve('../../../deploy/nginx/meta-pulse.conf'), 'utf8');
  const policies = [...nginx.matchAll(/add_header Content-Security-Policy "([^"]+)"/g)].map(m => m[1]);
  assert.equal(policies.filter(policy => policy === "frame-ancestors 'self'").length, 1);
  const shellPolicies = policies.filter(policy => policy.includes("default-src"));
  assert.equal(shellPolicies.length, 2);
  for (const policy of shellPolicies) {
    assert.match(policy, /connect-src 'self' https:\/\/claude\.com\/cdn-cgi\/trace https:\/\/claude\.ai\/cdn-cgi\/trace https:\/\/\*\.dnsprobe\.metarhub\.com;/);
    assert.match(policy, /script-src 'self'; style-src 'self';/);
    assert.doesNotMatch(policy, /unsafe-inline|connect-src[^;]*https:\/\/\*;/);
  }
});

test('view rescans all probes, prevents overlapping scans and disposes pending requests', async () => {
  const elements = new Map();
  const node = key => {
    if (!elements.has(key)) elements.set(key, {textContent:'', innerHTML:'', hidden:false, disabled:false,
      classList:{add(){},remove(){}}, listeners:{}, addEventListener(type, fn){this.listeners[type]=fn;}});
    return elements.get(key);
  };
  const page = {querySelector: node, querySelectorAll: () => []};
  let requests = [], pending = [], stalled = false;
  const tools = load('zh_CN', {AbortController, fetch: async (url, options) => {
    requests.push(url);
    if (stalled) return new Promise((resolve, reject) => {
      pending.push(options.signal);
      const abort = () => reject(Object.assign(Error('aborted'), {name:'AbortError'}));
      if (options.signal.aborted) abort(); else options.signal.addEventListener('abort', abort, {once:true});
    });
    return {ok:true,json:async()=>({ip:'203.0.113.9'}),text:async()=> 'ip=198.51.100.8\nloc=US\ncolo=LAX'};
  }});
  const view = new tools.View([]);
  view.mount({querySelector:()=>page});
  await view.egress;
  const run = node('[data-tool-run]');
  await run.listeners.click();
  assert.equal(requests.length, 5);
  assert.equal(run.disabled, false);
  assert.match(node('[data-claude-result="0"]').innerHTML, /198\.51\.100\.8/);
  assert.match(node('[data-webrtc-result]').innerHTML, /不支持或已禁用/);
  stalled = true;
  const scan = run.listeners.click();
  assert.equal(run.disabled, true);
  await run.listeners.click();
  assert.equal(requests.length, 9);
  const markup = node('[data-claude-result="0"]').innerHTML;
  view.dispose();
  await scan;
  assert.equal(pending.length, 4);
  assert.equal(pending.every(signal=>signal.aborted), true);
  assert.equal(node('[data-claude-result="0"]').innerHTML, markup);
});

test('DNS uses random authoritative name and independent private lookup token', async () => {
  const requests=[];
  const token='a'.repeat(32), hostname='b'.repeat(32)+'.dnsprobe.metarhub.com';
  const tools=load('zh_CN', {AbortController,setTimeout:(fn,ms)=>setTimeout(fn,ms===1000?0:ms),fetch:async(url,options)=>{
    requests.push({url,options});
    if(url.endsWith('/start'))return {ok:true,json:async()=>({token,hostname})};
    if(url.endsWith('/result'))return {ok:true,json:async()=>({resolvers:['1.1.1.1','1.1.1.1','2606:4700:4700::1111']})};
    throw new TypeError('Expected NXDOMAIN');
  }});
  const addresses=await tools.collectDNS();
  assert.equal(addresses.length,2);
  assert.equal(requests[1].url,'https://'+hostname+'/');
  assert.equal(requests[1].options.credentials,'omit');
  assert.equal(requests[1].options.referrerPolicy,'no-referrer');
  assert.equal(requests[1].url.includes(token),false);
  assert.equal(JSON.parse(requests[2].options.body).token,token);
  assert.equal(tools.dnsResult(addresses).status,'info');
  assert.match(tools.dnsResult(addresses).html,/地区未核实/);
  assert.equal(tools.dnsResult([]).status,'none');
  assert.match(tools.dnsResult([]).html,/不能据此判定没有泄露/);
});
test('DNS rejects foreign probe destinations and malformed resolver output',async()=>{
  for(const hostname of ['evil.example','a.dnsprobe.metarhub.com.evil.test','x'.repeat(32)+'.dnsprobe.metarhub.com']){
    let count=0;
    const tools=load('zh_CN',{AbortController,fetch:async()=>{count++;return {ok:true,json:async()=>({token:'a'.repeat(32),hostname})};}});
    await assert.rejects(tools.collectDNS(),e=>e.reason==='format');assert.equal(count,1);
  }
  const controller=new AbortController();controller.abort();
  const tools=load('zh_CN',{AbortController,fetch:async(_,options)=>{assert.equal(options.signal.aborted,true);throw Object.assign(Error('aborted'),{name:'AbortError'});}});
  await assert.rejects(tools.collectDNS(undefined,controller.signal));
});
