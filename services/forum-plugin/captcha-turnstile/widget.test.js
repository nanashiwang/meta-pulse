import test from 'node:test';
import assert from 'node:assert/strict';
function environment() {
  const scripts = [], timers = new Map(); let sequence = 0;
  const win = { setTimeout(fn) { timers.set(++sequence, fn); return sequence; }, clearTimeout(id) { timers.delete(id); } };
  const doc = { createElement() { return { remove() { this.removed = true; } }; }, head: { append(script) { scripts.push(script); } } };
  return { win, doc, scripts, timers };
}
test('script is loaded once, only from Cloudflare, and reused', async () => {
  const { loadTurnstile } = await import('./widget.js?loader-success');
  const env = environment();
  const first = loadTurnstile(env.win, env.doc), second = loadTurnstile(env.win, env.doc);
  assert.equal(first, second); assert.equal(env.scripts.length, 1);
  assert.equal(env.scripts[0].src, 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit');
  env.win.turnstile = { render() {} }; env.scripts[0].onload();
  assert.equal(await first, env.win.turnstile); assert.equal(env.timers.size, 0);
  await loadTurnstile(env.win, env.doc); assert.equal(env.scripts.length, 1);
});
test('script errors, missing API and timeout are retryable without leaving scripts behind', async () => {
  const { loadTurnstile } = await import('./widget.js?loader-failure');
  const env = environment();
  let pending = loadTurnstile(env.win, env.doc); env.scripts[0].onerror();
  await assert.rejects(pending); assert.equal(env.scripts[0].removed, true);
  pending = loadTurnstile(env.win, env.doc); env.scripts[1].onload();
  await assert.rejects(pending); assert.equal(env.scripts[1].removed, true);
  pending = loadTurnstile(env.win, env.doc); [...env.timers.values()][0]();
  await assert.rejects(pending); assert.equal(env.scripts[2].removed, true);
  pending = loadTurnstile(env.win, env.doc); env.win.turnstile = {}; env.scripts[3].onload();
  assert.equal(await pending, env.win.turnstile);
});
