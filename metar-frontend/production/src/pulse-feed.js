/* Anonymous reward projection. No account or grant identifiers belong here. */
'use strict';
(() => {
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  function validatedFeed(payload) {
    if (!payload || !Array.isArray(payload.rewards) || payload.rewards.length > 20 || !Number.isSafeInteger(payload.quota_per_unit) || payload.quota_per_unit < 0) throw new Error('invalid reward feed');
    const rewards = payload.rewards.map(row => {
      if (!row || !['community_exp','newapi_quota'].includes(row.reward_type) || !Number.isSafeInteger(row.amount) || row.amount <= 0 || typeof row.won_at !== 'string' || !Number.isFinite(Date.parse(row.won_at))) throw new Error('invalid reward item');
      return {reward_type:row.reward_type, amount:row.amount, won_at:new Date(Math.floor(Date.parse(row.won_at)/60000)*60000).toISOString()};
    });
    return {rewards, quota_per_unit:payload.quota_per_unit};
  }
  function rows(payload, {t, displayReward, locale}, limit = 20) {
    if (!payload.rewards.length) return `<tr><td colspan="3" class="prod-pulse-recent-empty"><strong>${esc(t('暂无社区中奖动态'))}</strong><span>${esc(t('近期已到账的抽奖奖励会显示在这里。'))}</span></td></tr>`;
    return payload.rewards.slice(0, limit).map(row => {
      const reward = displayReward(row, payload.quota_per_unit);
      const date = new Date(row.won_at);
      return `<tr><td><span class="prod-pulse-anonymous"><span aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="8" r="3"/><path d="M5 21v-2a7 7 0 0 1 14 0v2"/></svg></span>${esc(t('匿名用户'))}</span></td><td><span class="prod-pulse-recent-reward" data-tier="${esc(reward.tier)}"><span aria-hidden="true">${row.reward_type === 'community_exp' ? '☆' : '♢'}</span><strong>${esc(reward.amount)} <small>${esc(reward.unit)}</small></strong></span></td><td><time datetime="${esc(row.won_at)}" title="${esc(date.toLocaleString(locale(), {year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'}))}">${esc(date.toLocaleString(locale(), {month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'}))}</time></td></tr>`;
    }).join('');
  }
  class View {
    constructor(root, dialog, options) {
      this.root = root; this.dialog = dialog; this.options = options;
      this.dead = false; this.busy = false; this.denied = false; this.suspended = false; this.timer = 0; this.payload = null;
      this.onVisibility = () => { clearTimeout(this.timer); if (!document.hidden) void this.refresh(); };
      this.onHide = () => { this.suspended = true; clearTimeout(this.timer); };
      this.onShow = () => { this.suspended = false; void this.refresh(); };
      this.onRetry = event => { if (event.target.closest('[data-pulse-feed-retry]')) void this.refresh(); };
      this.targets('[data-pulse-feed-retry]').forEach(button => { button.onclick = null; });
      root.addEventListener('click', this.onRetry); dialog?.addEventListener('click', this.onRetry);
      document.addEventListener('visibilitychange', this.onVisibility);
      window.addEventListener('pagehide', this.onHide); window.addEventListener('pageshow', this.onShow);
      void this.refresh();
    }
    active() { return !this.dead && !this.suspended && this.options.current(); }
    targets(selector) { return [this.root, this.dialog].filter(Boolean).flatMap(root => [...root.querySelectorAll(selector)]); }
    message(text) { this.targets('[data-pulse-feed-message]').forEach(node => {node.textContent = text; node.hidden = !text;}); }
    paint(payload) {
      this.targets('[data-pulse-feed-rows]').forEach(node => {node.innerHTML = rows(payload, this.options, node.dataset.pulseFeedRows === 'preview' ? 5 : 20);});
    }
    async refresh() {
      if (!this.active() || this.busy || document.hidden) return;
      clearTimeout(this.timer); this.busy = true;
      this.targets('[data-pulse-feed-retry]').forEach(button => { button.disabled = false; button.setAttribute('aria-disabled', 'true'); });
      this.root.setAttribute('aria-busy','true');
      try {
        const payload = validatedFeed(await this.options.client.winners());
        if (!this.active()) return;
        this.payload = payload; this.denied = false; this.paint(payload); this.message('');
      } catch (error) {
        if (!this.active()) return;
        this.denied = [401,403].includes(error?.status);
        if (this.denied || !this.payload) {
          this.payload = null;
          this.targets('[data-pulse-feed-rows]').forEach(node => { node.innerHTML = ''; });
        }
        this.message(this.options.t(this.denied ? '请重新登录并确认绑定后查看动态。' : this.payload ? '动态暂时无法更新，仍显示上次记录。' : '暂时无法加载社区中奖动态。'));
      } finally {
        this.busy = false;
        if (this.active()) {
          this.root.setAttribute('aria-busy','false');
          this.targets('[data-pulse-feed-retry]').forEach(button => { button.disabled = false; button.setAttribute('aria-disabled', 'false'); });
          if (!document.hidden && !this.denied) this.timer = setTimeout(() => this.refresh(), 60000);
        }
      }
    }
    dispose() {
      this.dead = true; this.payload = null; clearTimeout(this.timer);
      this.root.removeEventListener('click',this.onRetry); this.dialog?.removeEventListener('click',this.onRetry);
      document.removeEventListener('visibilitychange',this.onVisibility);
      window.removeEventListener('pagehide',this.onHide); window.removeEventListener('pageshow',this.onShow);
    }
  }
  window.MetarRewardFeed = Object.freeze({validatedFeed, rows, View});
})();
