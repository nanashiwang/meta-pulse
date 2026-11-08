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
  assert.equal(tools.assessWebRTC([c('198.51.100.7')], '').status, 'clean');
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

test('network page names the STUN host, shows no score and never stores results', () => {
  const tools = load();
  const html = new tools.View([]).page('/tools/network');
  assert.match(html, /stun\.cloudflare\.com/);
  assert.match(html, /不会保存或上传/);
  assert.match(html, /<code>en-US<\/code> <code>fr<\/code>/);
  assert.doesNotMatch(html, /分数|score/i);
});

test('WebRTC result escapes addresses and handles unsupported browsers', () => {
  const tools = load();
  const unsupported = tools.webrtcResult({supported: false, entries: []}, '203.0.113.9');
  assert.match(unsupported, /不支持或已禁用/);
  const leak = tools.webrtcResult({supported: true, entries: [{address: '198.51.100.7', scope: 'public'}]}, '203.0.113.9');
  assert.match(leak, /data-status="leak"/);
  assert.match(leak, /<code>198\.51\.100\.7<\/code>/);
});

test('egress lookup omits credentials and rejects invalid bodies', async () => {
  let seen;
  const ok = load('zh_CN', {fetch: async (url, options) => { seen = {url, options}; return {ok: true, json: async () => ({ip: '203.0.113.9'})}; }});
  assert.equal(await ok.fetchEgress(), '203.0.113.9');
  assert.equal(seen.url, '/metar/api/tools/ip');
  assert.equal(seen.options.credentials, 'omit');
  assert.equal(seen.options.cache, 'no-store');
  const bad = load('zh_CN', {fetch: async () => ({ok: true, json: async () => ({ip: '<script>'})})});
  await assert.rejects(bad.fetchEgress());
  const down = load('zh_CN', {fetch: async () => ({ok: false})});
  await assert.rejects(down.fetchEgress());
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
