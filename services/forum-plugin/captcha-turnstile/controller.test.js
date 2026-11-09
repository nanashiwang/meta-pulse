import test from 'node:test';
import assert from 'node:assert/strict';
import { createCaptchaController } from './controller.js';
function fixture(overrides = {}) {
  const state = { errors: 0, removed: 0, handlers: undefined, shows: 0 };
  const controller = createCaptchaController({
    captchaKey: 'email',
    request: async (path) => path.includes('action/record')
      ? { data: { verify: true, captcha_id: 'native-id' } }
      : { data: { slug_name: 'metar_turnstile', config: { key: 'public', action: 'answer_captcha' } } },
    present: (_, handlers) => { state.handlers = handlers; state.shows++; return () => state.removed++; },
    report: () => state.errors++, ...overrides,
  });
  return { state, controller };
}
test('registration submits once with the native fields and never retains a token', async () => {
  const { state, controller } = fixture(); let submitted = 0, payload;
  await controller.check(() => { submitted++; payload = controller.getCaptcha(); });
  assert.equal(submitted, 0);
  state.handlers.verified('single-use'); state.handlers.verified('single-use');
  assert.equal(submitted, 1);
  assert.deepEqual(payload, { verify: true, captcha_id: 'native-id', captcha_code: 'single-use' });
  assert.equal(controller.getCaptcha().captcha_code, '');
  assert.equal(state.removed, 1);
});
test('captcha API rejection starts a fresh challenge without reusing token', async () => {
  const { state, controller } = fixture(); const payloads = [];
  await controller.check(() => payloads.push(controller.getCaptcha().captcha_code));
  const old = state.handlers; old.verified('first');
  controller.handleCaptchaError([{ error_field: 'captcha_code' }]);
  await new Promise(resolve => setImmediate(resolve));
  old.verified('replay'); state.handlers.verified('fresh');
  assert.deepEqual(payloads, ['first', 'fresh']); assert.equal(state.shows, 2);
});
test('strategy/network failure does not submit and can retry', async () => {
  let fail = true, submits = 0;
  const { state, controller } = fixture({ request: async () => { if (fail) throw Error('offline'); return { data: { verify: false } }; }, captchaKey: 'password' });
  await controller.check(() => submits++); assert.equal(submits, 0); assert.equal(state.errors, 1);
  fail = false; await controller.check(() => submits++); assert.equal(submits, 1);
});
test('email never downgrades to unverified even with a stale strategy', async () => {
  const { state, controller } = fixture({ request: async () => ({ data: { verify: false } }) });
  await controller.check(() => assert.fail('unverified')); assert.equal(state.errors, 1);
});
test('missing site key fails closed and concurrent clicks do not submit', async () => {
  const { state, controller } = fixture({ request: async (path) => ({ data: path.includes('action/record') ? { verify: true } : { config: {} } }) });
  await Promise.all([controller.check(() => assert.fail()), controller.check(() => assert.fail())]);
  assert.equal(state.errors, 1); assert.equal(state.shows, 0);
});
test('cancel, unmount and in-flight responses cannot submit', async () => {
  const { state, controller } = fixture(); await controller.check(() => assert.fail('cancelled'));
  state.handlers.cancelled(); state.handlers.verified('late');
  let finish;
  const other = fixture({ request: () => new Promise(resolve => { finish = resolve; }) });
  const pending = other.controller.check(() => assert.fail('unmounted')); other.controller.dispose();
  finish({ data: { verify: true } }); await pending; assert.equal(other.state.shows, 0);
});
test('expiry and script failure cannot provide a token; subsequent verified retry works', async () => {
  const { state, controller } = fixture(); let submitted = 0;
  await controller.check(() => submitted++); state.handlers.failed();
  assert.equal(controller.getCaptcha().captcha_code, ''); assert.equal(submitted, 0);
  state.handlers.verified('new'); assert.equal(submitted, 1);
});
