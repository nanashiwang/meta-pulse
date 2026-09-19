// Answer 1.7.1 advertises a UserCenter sign-up redirect even when the plugin
// enables local accounts. Its signup guard follows that URL instead of loading
// the native form. Correct only this plugin's presentation metadata; the native
// registration setting, captcha, email checks and backend endpoint stay intact.
const installed = new WeakSet();
export function installLocalRegistration(store) {
  if (installed.has(store)) return;
  installed.add(store);
  function sync() {
    const { agent } = store.getState();
    if (!agent?.enabled || agent.agent_info?.name !== 'Meta Pulse' || agent.agent_info.enabled_original_user_system !== true || agent.agent_info.sign_up_redirect_url === '/users/register') return;
    store.setState({ agent: { ...agent, agent_info: { ...agent.agent_info, sign_up_redirect_url: '/users/register' } } });
  }
  store.subscribe(sync);
  sync();
}
