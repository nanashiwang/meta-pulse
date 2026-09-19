import test from 'node:test';
import assert from 'node:assert/strict';
import { installLocalRegistration } from './local-registration.js';
function fixture(agent) {
  let state = { agent, unrelated: 'preserved' };
  const listeners = [];
  return {
    listeners, updates: 0,
    getState: () => state,
    subscribe: (fn) => listeners.push(fn),
    setState(next) { this.updates++; state = { ...state, ...next }; listeners.forEach((fn) => fn(state)); },
  };
}
const meta = { enabled: true, agent_info: { name: 'Meta Pulse', enabled_original_user_system: true, sign_up_redirect_url: 'https://metar.uk/answer/api/v1/user-center/sign-up/redirect', login_redirect_url: '/connector', control_center_items: [] } };

test('本地注册在初始加载和重新获取插件信息时均恢复且不改变身份或登录配置', () => {
  const store = fixture();
  installLocalRegistration(store);
  installLocalRegistration(store);
  assert.equal(store.listeners.length, 1);
  for (let i = 0; i < 3; i++) {
    store.setState({ agent: meta });
    assert.deepEqual(store.getState().agent, { ...meta, agent_info: { ...meta.agent_info, sign_up_redirect_url: '/users/register' } });
    assert.equal(store.getState().unrelated, 'preserved');
  }
  assert.equal(store.updates, 6);
  const initial = fixture(meta);
  installLocalRegistration(initial);
  assert.equal(initial.getState().agent.agent_info.sign_up_redirect_url, '/users/register');
});

test('不接管其他插件、停用插件或关闭本地账号的模式', () => {
  for (const agent of [{ ...meta, enabled: false }, { ...meta, agent_info: { ...meta.agent_info, name: 'Other' } }, { ...meta, agent_info: { ...meta.agent_info, enabled_original_user_system: false } }, { enabled: true }]) {
    const store = fixture(agent);
    installLocalRegistration(store);
    assert.equal(store.updates, 0);
    assert.deepEqual(store.getState().agent, agent);
  }
});
