/* Period economics are immutable once created; this form creates the next pool. */
'use strict';
(() => {
  const t = (...args) => window.MetarI18n.t(...args);
  const esc = (v) => String(v ?? '').replace(/[&<>"']/g, (c) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const invalid = () => new window.MetarAdapters.AdapterError('invalid period', { code: 'invalid_period' });
  const storageKey = 'metar-period-create-pending-v1';
  function fixed(value, digits, max = 9007199254740991n) {
    const text = String(value ?? '').trim();
    if (!new RegExp(`^(0|[1-9][0-9]*)(\\.[0-9]{1,${digits}})?$`).test(text)) throw invalid();
    const [whole, fraction = ''] = text.split('.');
    const result = BigInt(whole) * 10n ** BigInt(digits) + BigInt(fraction.padEnd(digits, '0'));
    if (result <= 0n || result > max) throw invalid();
    return Number(result);
  }
  function integer(value) {
    if (!/^[1-9][0-9]*$/.test(String(value))) throw invalid();
    const n = BigInt(value);
    if (n > 9007199254740991n) throw invalid();
    return Number(n);
  }
  function format(value, digits) {
    if (!Number.isSafeInteger(value) || value < 0) return '—';
    const text = String(value).padStart(digits + 1, '0');
    return `${text.slice(0, -digits)}.${text.slice(-digits)}`.replace(/\.?0+$/, '');
  }
  const statusText = (status) => ({draft:t('草稿'),active:t('已启用'),settling:t('结算中'),closed:t('已结束')})[status] || status;
  const dateText = (value) => {
    const date = new Date(value);
    return Number.isFinite(date.getTime()) ? new Intl.DateTimeFormat(window.MetarI18n.locale(), {timeZone:'Asia/Shanghai',year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false}).format(date) : '—';
  };
  const errorText = (error) => {
    if (error.code === 'period_conflict' || error.status === 409) return t('周期编号或时间与已有周期冲突，或重复请求的内容发生变化。请刷新周期列表后核对。');
    if (error.code === 'settings_pending') return t('暂时无法确认创建结果。请重试原请求，不要另建周期。');
    if (error.code === 'invalid_period' || error.status === 400) return t('请检查周期编号、开始时间、正数倍率、每张券贡献度、预算和奖项。单个奖项不能超过总预算。');
    if (error.status === 401 || error.status === 403) return t('仅正常且已激活的社区管理员可管理 Pulse 配置。');
    return t('周期配置暂不可用，请确认服务已升级并完成管理通道配对。');
  };
  function rewardRow() {
    return `<div class="prod-admin-grid prod-period-prize mt16">
      <label>${t('奖项编号')}<input name="prize_key" required pattern="[a-z0-9](?:[a-z0-9_]|-){0,63}" placeholder="reward-1"></label>
      <label>${t('奖励额度（整数 quota）')}<input name="prize_amount" required inputmode="numeric" pattern="[1-9][0-9]*"></label>
      <label>${t('抽取权重')}<input name="prize_weight" required inputmode="numeric" pattern="[1-9][0-9]*"></label>
      <button type="button" class="btn small" data-action="admin-period-remove">${t('移除奖项')}</button></div>`;
  }
  class View {
    constructor(api) { this.api = api; this.epoch = 0; this.busy = false; this.pending = null; }
    dispose() { this.epoch++; this.busy = false; }
    form() { return document.querySelector('form[data-form="admin-period"]'); }
    persist() {
      try { if (this.pending) sessionStorage.setItem(storageKey, JSON.stringify(this.pending)); else sessionStorage.removeItem(storageKey); } catch (_) { /* In-memory retry still works. */ }
    }
    restore() {
      try { const saved = JSON.parse(sessionStorage.getItem(storageKey) || 'null'); if (saved?.key && saved?.body?.key && saved.body.starts_at) this.pending = saved; } catch (_) { /* Invalid storage does not authorize a request. */ }
    }
    async page() {
      this.dispose(); this.restore();
      const epoch = this.epoch;
      let list;
      try { list = await this.api.periods(); } catch (e) {
        return `<section class="card card-pad mt24"><h2>${t('贡献度与脉冲券')}</h2><p class="muted mt16">${esc(errorText(e))}</p><button class="btn mt16" data-action="retry">${t('重新加载')}</button></section>`;
      }
      if (epoch !== this.epoch) return '';
      return `<section class="card card-pad mt24"><div class="between wrap"><h2>${t('贡献度与脉冲券')}</h2><button type="button" class="btn primary" data-action="admin-period-toggle" aria-expanded="${Boolean(this.pending)}" aria-controls="admin-period-editor">${t('设置兑换比例')}</button></div>
        <p class="muted mt16">${t('已启用周期的比例保持不变。新比例与奖池一起保存，在新周期开始后生效；每期持续 10 天。')}</p>
        <div class="mt16 prod-period-list">${list.periods.length ? list.periods.map((p) => `<div class="prod-status mt8"><div><strong>${esc(p.key)} · ${esc(statusText(p.status))}</strong><p>${esc(dateText(p.starts_at))} → ${esc(dateText(p.ends_at))} (UTC+8)</p><p>${t('贡献倍率')}：${p.rules.map((r) => `${esc(r.key === 'default' ? t('通用') : r.key)} ${esc(format(r.multiplier_bps,4))}×`).join('、')} · ${t('每张券所需贡献度')}：${p.ticket_threshold_milli ? esc(format(p.ticket_threshold_milli,3)) : t('沿用服务器默认门槛')}</p></div><span class="badge">${t('只读')}</span></div>`).join('') : `<p class="muted">${t('尚无周期，请创建首个周期。')}</p>`}</div>
        <form id="admin-period-editor" data-form="admin-period" class="prod-admin-form mt24" ${this.pending ? '' : 'hidden'}>
        <fieldset ${this.pending ? 'disabled' : ''}><div class="prod-admin-grid">
          <label>${t('新周期编号')}<input name="key" required maxlength="64" pattern="[a-zA-Z0-9](?:[a-zA-Z0-9_]|-){0,63}" placeholder="rewards-2026-02"></label>
          <label>${t('开始时间（北京时间）')}<input name="starts_at" type="datetime-local" required></label>
          <label>${t('API 贡献倍率')}<input name="multiplier" type="number" min="0.0001" max="100" step="0.0001" value="1" required><span class="prod-field-help">${t('1 表示原始贡献的 1 倍；最多支持 4 位小数。')}</span></label>
          <label>${t('每张脉冲券所需贡献度')}<input name="threshold" type="number" min="0.001" step="0.001" required placeholder="1000"><span class="prod-field-help">${t('按本期累计净贡献计算，最多支持 3 位小数。')}</span></label>
        </div>
        <h3 class="mt24">${t('新周期奖池')}</h3><p class="prod-field-help">${t('预算与奖励使用 new-api 整数 quota。请根据实际成本填写；保存不会开启抽奖或自动发奖开关。')}</p>
        <label class="prod-admin-field mt16">${t('奖池总预算（整数 quota）')}<input name="reward_budget" required inputmode="numeric" pattern="[1-9][0-9]*"></label>
        <div data-period-prizes>${rewardRow()}</div><button class="btn mt16" type="button" data-action="admin-period-add">${t('添加奖项')}</button>
        <label class="prod-admin-field mt24">${t('修改原因')}<textarea name="reason" required minlength="3" maxlength="500" rows="2"></textarea></label>
        <label class="prod-check mt16"><input name="confirm" type="checkbox" required> ${t('我已核对比例、时间与奖池；保存后本周期规则将冻结。')}</label>
        <button type="submit" class="btn primary mt24">${t('保存并创建新周期')}</button></fieldset>
        <p class="prod-field-help mt16" data-period-message role="status" aria-live="polite">${this.pending ? esc(t('存在待确认的周期创建请求：{key}。请重试原请求。', {key:this.pending.body.key})) : ''}</p>
        <button class="btn primary mt16" type="button" data-action="admin-period-retry" ${this.pending ? '' : 'hidden'}>${t('重试原请求')}</button>
        <button class="btn mt16" type="button" data-action="retry">${t('刷新周期列表')}</button>
        </form></section>`;
    }
    payload(form) {
      const f = new FormData(form);
      const key = String(f.get('key') || '').trim();
      if (!/^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$/.test(key) || !f.has('confirm')) throw invalid();
      const starts = String(f.get('starts_at') || '');
      if (!/^\d{4}-\d\d-\d\dT\d\d:\d\d$/.test(starts) || !Number.isFinite(Date.parse(`${starts}:00+08:00`))) throw invalid();
      const reward_budget = integer(f.get('reward_budget'));
      const rewards = [...form.querySelectorAll('.prod-period-prize')].map((row) => ({
        key: row.querySelector('[name="prize_key"]').value.trim(), amount: integer(row.querySelector('[name="prize_amount"]').value), weight: integer(row.querySelector('[name="prize_weight"]').value),
      }));
      const reason = String(f.get('reason') || '').trim();
      if (!rewards.length || rewards.length > 50 || reason.length < 3 || reason.length > 500 || new Set(rewards.map((r) => r.key)).size !== rewards.length || rewards.some((r) => !/^[a-z0-9][a-z0-9_-]{0,63}$/.test(r.key) || r.amount > reward_budget) || rewards.reduce((sum,r) => sum + BigInt(r.weight),0n)>9007199254740991n) throw invalid();
      return { key, starts_at: `${starts}:00+08:00`, multiplier_bps: fixed(f.get('multiplier'),4,1000000n), ticket_threshold_milli:fixed(f.get('threshold'),3),reward_budget,rewards,reason };
    }
    async submit(form, retry = false) {
      if (this.busy || (this.pending && !retry)) return;
      const epoch = this.epoch;
      const message = form.querySelector('[data-period-message]');
      try {
        if (!retry) { this.pending = { body:this.payload(form),key:window.crypto.randomUUID() }; this.persist(); }
        if (!this.pending) return;
        this.busy = true; form.querySelector('fieldset').disabled = true;
        form.querySelector('[data-action="admin-period-retry"]').disabled = true;
        message.textContent = t('正在创建周期…');
        const result = await this.api.createPeriod(this.pending.body,this.pending.key);
        this.pending = null; this.persist();
        if (epoch !== this.epoch) return;
        message.textContent = t('周期 {key} 已创建，比例已保存。请刷新周期列表查看。', {key:result.period_key});
        form.reset();
      } catch (error) {
        if (error.code !== 'settings_pending') { this.pending = null; this.persist(); }
        if (epoch === this.epoch) message.textContent = errorText(error);
      } finally {
        if (epoch === this.epoch) {
          this.busy = false; form.querySelector('fieldset').disabled = Boolean(this.pending);
          const retryButton = form.querySelector('[data-action="admin-period-retry"]'); retryButton.hidden = !this.pending; retryButton.disabled = false;
        }
      }
    }
    action(action,target) {
      const form = this.form(); if (!form || this.busy) return;
      if (action === 'admin-period-toggle') { form.hidden = !form.hidden; target.setAttribute('aria-expanded',String(!form.hidden)); return; }
      if (action === 'admin-period-retry') return this.submit(form,true);
      if (this.pending) return;
      if (action === 'admin-period-add' && form.querySelectorAll('.prod-period-prize').length < 50) form.querySelector('[data-period-prizes]').insertAdjacentHTML('beforeend',rewardRow());
      if (action === 'admin-period-remove' && form.querySelectorAll('.prod-period-prize').length > 1) target.closest('.prod-period-prize')?.remove();
    }
  }
  window.MetarPeriodAdmin = Object.freeze({View,fixed,format});
})();
