/* Administrator settings. Secret drafts exist only in the active form and memory. */
'use strict';
(() => {
  const { AdapterError, PULSE_ADMIN_SECRET_KEYS } = window.MetarAdapters;
  const t = (...args) => window.MetarI18n.t(...args);
  const esc = (value) => String(value ?? '').replace(/[&<>"']/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[char]));
  const SECRETS = [
    ['PULSE_USER_BFF_HMAC_SECRET', 'Pulse 用户侧密钥', '对应 new-api 后台的 Pulse 用户侧密钥。'],
    ['PULSE_ADMIN_HMAC_SECRET', 'Pulse 运营侧密钥', '对应 new-api 的运营侧密钥及插件 admin_hmac_secret。轮换时先在本页同时保存新值与原值（上一密钥），再更新对端。'],
    ['PULSE_FORUM_HMAC_SECRET', '社区等级查询密钥', '对应社区插件的 pulse_hmac_secret，仅用于等级查询。'],
    ['PULSE_COMMUNITY_BFF_HMAC_SECRET', '社区抽奖密钥', '对应社区插件的 community_bff_hmac_secret。'],
    ['PULSE_SERVICE_HMAC_SECRET', 'Pulse 自动发奖密钥', '对应 new-api 后台的 Pulse 自动发奖密钥。'],
    ['PULSE_ROLLBACK_HMAC_SECRET', 'Pulse 奖励撤销密钥', '对应 new-api 后台的 Pulse 奖励撤销密钥。'],
  ];
  const errorText = (error) => {
    if (error.code === 'admin_required' || error.status === 403) return t('仅正常且已激活的社区管理员可管理 Pulse 配置。');
    if (error.code === 'authentication_required' || error.status === 401) return t('登录状态已过期，请重新登录。');
    if (error.code === 'settings_conflict') return t('配置已被其他管理员修改。你的输入已保留，请读取最新版本、核对差异后再保存。');
    if (error.code === 'settings_pending') return t('暂时无法确认保存结果。输入已保留，请重试原保存；不会产生新的保存请求。');
    if (error.status === 400 || error.code === 'invalid_request') return t('配置未通过校验。请检查地址、正整数额度、独立密钥及修改原因，再保存。');
    if (error.code === 'origin_rejected') return t('请求来源校验失败，请刷新页面后重试。');
    return t('配置服务暂不可用。请检查管理配对、服务状态后重试。');
  };

  class View {
    constructor(api) { this.api = api; this.periods = window.MetarPeriodAdmin ? new window.MetarPeriodAdmin.View(api) : null; this.dispose(); }
    dispose() { this.periods?.dispose(); this.epoch = (this.epoch || 0) + 1; this.snapshot = null; this.pending = null; this.busy = false; this.conflict = false; }
    async page() {
      this.dispose();
      const epoch = this.epoch;
      try { const snapshot = await this.api.settings(); if (epoch !== this.epoch) return ''; this.snapshot = snapshot; }
      catch (error) {
        if (epoch !== this.epoch) return '';
        return `<section class="card card-pad prod-admin-error"><h2>${t('暂时无法读取 Pulse 配置')}</h2><p class="muted mt16">${esc(errorText(error))}</p>${error.status !== 403 && error.status !== 401 ? `<p class="muted mt16">${t('首次使用请在社区插件中将 admin_hmac_secret 与现有 Pulse 运营侧密钥配对。')}</p><a class="btn mt16" href="/admin/pulse_user_center">${t('打开社区插件设置')}</a>` : ''}<button class="btn mt16" data-action="retry">${t('重新加载')}</button></section>`;
      }
      const periodsHTML = this.periods ? await this.periods.page() : '';
      if (epoch !== this.epoch) return '';
      const data = this.snapshot;
      const keyField = ([key, label, description], previous = false) => {
        const name = previous ? `${key}_PREVIOUS` : key;
        const state = data.secrets[name];
        return `<div class="prod-secret-field"><div class="between wrap"><label for="${name}">${esc(t(label))}${previous ? ` · ${t('上一密钥')}` : ''}</label><span class="badge ${state.configured ? 'green' : ''}" data-secret-status="${name}">${t(state.configured ? '已配置' : '未配置')}</span></div><p class="prod-field-help">${esc(t(description))}</p><code class="prod-field-code">${name}</code><div class="prod-secret-input"><input id="${name}" name="${name}" type="password" autocomplete="new-password" spellcheck="false" autocapitalize="off" maxlength="512" placeholder="${t('留空保留现有密钥')}" aria-label="${esc(t(label))}${previous ? ` · ${t('上一密钥')}` : ''}"><button type="button" class="btn small" data-action="admin-secret-show" data-key="${name}">${t('显示输入')}</button><button type="button" class="btn small" data-action="admin-secret-copy" data-key="${name}">${t('复制输入')}</button>${previous ? '' : `<button type="button" class="btn small" data-action="admin-secret-generate" data-key="${name}">${t('生成新密钥')}</button>`}</div>${previous ? `<label class="prod-check"><input type="checkbox" name="clear_${name}"> ${t('清除上一密钥')}</label>` : ''}</div>`;
      };
      return `${periodsHTML}<div class="prod-status mt24"><div><strong>${t('配置保存在服务端')}</strong><p>${t('已有密钥不会回显，留空保留。新密钥仅在本页输入区显示。首次配对时两端填写相同值；已有密钥轮换请按下方顺序操作。')}</p></div></div>
      <form data-form="admin-pulse" class="prod-admin-form" autocomplete="off">
        <fieldset><section class="card card-pad mt24"><h2>${t('连接与运行')}</h2><p class="muted mt8">${t('运行环境沿用服务器部署配置。')}</p><div class="prod-admin-grid mt24">
          <div class="prod-admin-field"><label for="newapi_internal_base_url">${t('new-api 私网地址')}</label><input id="newapi_internal_base_url" name="newapi_internal_base_url" type="url" required maxlength="2048" ${data.newapi_target_locked ? 'readonly aria-readonly="true"' : ''} value="${esc(data.config.newapi_internal_base_url)}" placeholder="http://new-api-private:3000"><p class="prod-field-help">${t('填写 Pulse 可访问的 HTTP(S) 根地址，不含用户名、密码、路径、查询参数。')}</p><p class="prod-field-help">${t(data.newapi_target_locked ? '结算目标已锁定，迁址需维护窗口处理。' : '已有奖励记录后不能切换 new-api 资金来源；迁移需单独审计处理。')}</p></div>
          <div class="prod-admin-field"><label for="quota_per_unit">${t('每 1 API 额度单位对应 quota')}</label><input id="quota_per_unit" name="quota_per_unit" type="text" inputmode="numeric" pattern="[1-9][0-9]*" required maxlength="19" value="${esc(data.config.quota_per_unit)}" placeholder="500000"><p class="prod-field-help">${t('必须与 new-api 的 quota_per_unit 一致；这里不是人民币汇率。')}</p></div>
        </div><div class="prod-admin-switches mt24"><label class="prod-check"><input type="checkbox" name="actions_enabled" ${data.config.actions_enabled ? 'checked' : ''}><span><strong>${t('允许新的抽奖操作')}</strong><small>${t('关闭后仍可查询和恢复已有奖励。')}</small></span></label><label class="prod-check"><input type="checkbox" name="reward_shadow_mode" ${data.config.reward_shadow_mode ? 'checked' : ''}><span><strong>${t('影子模式')}</strong><small>${t('开启时不开放真实抽奖。正式启用还需要有效奖池和 new-api 自动发奖开关。')}</small></span></label></div>
        <p class="prod-field-help mt16" data-worker-status>${t(data.worker_ready ? '发奖密钥存储已就绪。' : '发奖密钥存储尚未就绪；请检查 Worker 初始化状态。')}</p></section>
        <section class="card card-pad mt24"><h2>${t('密钥与系统配对')}</h2><p class="muted mt8">${t('不同用途使用不同密钥。已有配对正常时无需重新生成；密钥修改后，需同步对应的 new-api 或社区插件字段。')}</p><div class="prod-admin-grid mt24">${SECRETS.map((item) => keyField(item)).join('')}</div><details class="prod-admin-rotation mt24"><summary>${t('高级：密钥轮换兼容')}</summary><p class="muted mt16">${t('仅在主动轮换时填写上一密钥；留空保留。确认对端已完成轮换后，才能勾选清除。')}</p><p class="muted mt16">${t('轮换时先在接收端同时保存新密钥与原值（上一密钥），再更新发送端，验证后清除上一密钥。')}</p><p class="muted mt16">${t('运营侧密钥：先在本页同时保存新运营侧密钥及原值作为上一密钥，再更新插件 admin_hmac_secret 和 new-api；不要先改插件。')}</p><div class="prod-admin-grid mt24">${SECRETS.map((item) => keyField(item, true)).join('')}</div></details></section>
        <section class="card card-pad mt24"><h2>${t('社区绑定与奖池')}</h2><p class="muted mt8">${t('社区 SSO、公开 new-api 地址和插件配对仍由 Answer 插件管理。这里的运行开关不修改已启用周期的概率、门槛或预算。')}</p><a href="/admin/pulse_user_center" class="btn mt16">${t('打开社区插件设置')}</a></section>
        <section class="card card-pad mt24"><label for="admin-pulse-reason">${t('修改原因')}</label><textarea id="admin-pulse-reason" name="reason" rows="3" required minlength="3" maxlength="500" placeholder="${t('例如：完成两端密钥配对，准备小额到账验收')}"></textarea><p class="prod-field-help">${t('原因将进入操作审计，请勿填写密钥或其他敏感信息。')}</p></section></fieldset>
        <div class="prod-admin-save mt24"><button type="submit" class="btn primary" data-admin-save>${t('保存 Pulse 配置')}</button><span class="muted" data-admin-revision>${t('配置版本 {revision}', { revision: data.revision })}</span></div>
        <div class="prod-status mt16" role="status" aria-live="polite" data-admin-message hidden></div>
        <div class="prod-admin-recovery mt16" data-admin-recovery hidden></div>
      </form>`;
    }
    form() { return document.querySelector('form[data-form="admin-pulse"]'); }
    message(value, error = false) {
      const node = this.form()?.querySelector('[data-admin-message]');
      if (node) { node.hidden = false; node.textContent = value; node.classList.toggle('error', error); }
    }
    lock(value) {
      const form = this.form();
      if (!form) return;
      form.querySelector('fieldset').disabled = value;
      form.querySelector('[data-admin-save]').disabled = value || this.conflict;
    }
    recovery() {
      const node = this.form()?.querySelector('[data-admin-recovery]');
      if (!node) return;
      node.hidden = !this.pending && !this.conflict;
      node.innerHTML = this.pending ? `<button class="btn primary" type="button" data-action="admin-save-retry">${t('重试原保存')}</button><p class="prod-field-help mt8">${t('请保留此页面。刷新或离开页面会清除尚未保存的密钥输入。')}</p>` : this.conflict ? `<button class="btn" type="button" data-action="admin-config-refresh">${t('读取最新版本并保留输入')}</button>` : '';
    }
    updateStatus(form, saved) {
      for (const key of PULSE_ADMIN_SECRET_KEYS) {
        const badge = form.querySelector(`[data-secret-status="${key}"]`);
        if (badge) { badge.textContent = t(saved.secrets[key].configured ? '已配置' : '未配置'); badge.classList.toggle('green', saved.secrets[key].configured); }
      }
      form.querySelector('[data-admin-revision]').textContent = t('配置版本 {revision}', { revision: saved.revision });
      const target = form.elements.namedItem('newapi_internal_base_url');
      if (target) {
        target.readOnly = saved.newapi_target_locked;
        if (saved.newapi_target_locked) target.value = saved.config.newapi_internal_base_url;
      }
      const worker = form.querySelector('[data-worker-status]');
      if (worker) worker.textContent = t(saved.worker_ready ? '发奖密钥存储已就绪。' : '发奖密钥存储尚未就绪；请检查 Worker 初始化状态。');
    }
    payload(form) {
      const fields = new FormData(form);
      const config = {
        newapi_internal_base_url: String(fields.get('newapi_internal_base_url') || '').trim(),
        quota_per_unit: String(fields.get('quota_per_unit') || '').trim(),
        actions_enabled: fields.has('actions_enabled'), reward_shadow_mode: fields.has('reward_shadow_mode'),
      };
      const secrets = {}, clear = [];
      for (const key of PULSE_ADMIN_SECRET_KEYS) {
        const value = String(fields.get(key) || '');
        if (value) secrets[key] = value;
        if (key.endsWith('_PREVIOUS') && fields.has(`clear_${key}`)) clear.push(key);
      }
      const reason = String(fields.get('reason') || '').trim();
      if (!/^[1-9][0-9]*$/.test(config.quota_per_unit) || BigInt(config.quota_per_unit) > 9223372036854775807n || reason.length < 3 || clear.some((key) => secrets[key])) throw new AdapterError('配置未通过校验', { code: 'invalid_request' });
      return { revision: this.snapshot.revision, config, secrets, clear_secrets: clear, reason };
    }
    async submit(form, retry = false) {
      if (this.busy || !this.snapshot || this.conflict) return;
      const epoch = this.epoch;
      try {
        if (!retry) {
          if (this.pending) return;
          this.pending = { body: this.payload(form), key: window.crypto.randomUUID() };
        }
        if (!this.pending) return;
        this.busy = true; this.lock(true);
        this.message(t('正在保存配置…'));
        const saved = await this.api.save(this.pending.body, this.pending.key);
        if (epoch !== this.epoch) return;
        this.snapshot = saved; this.pending = null;
        for (const key of PULSE_ADMIN_SECRET_KEYS) {
          const input = form.elements.namedItem(key);
          if (input) { input.value = ''; input.type = 'password'; }
          const clear = form.elements.namedItem(`clear_${key}`);
          if (clear) clear.checked = false;
        }
        form.querySelectorAll('[data-action="admin-secret-show"]').forEach((button) => { button.textContent = t('显示输入'); });
        this.updateStatus(form, saved);
        this.message(t('配置已保存；新请求生效，后台任务下一轮采用。请确认对应系统使用相同密钥。'));
      } catch (error) {
        if (epoch !== this.epoch) return;
        if (error.code !== 'settings_pending') this.pending = null;
        this.conflict = error.code === 'settings_conflict';
        this.message(errorText(error), true);
      } finally { if (epoch === this.epoch) { this.busy = false; this.lock(Boolean(this.pending)); this.recovery(); } }
    }
    async action(action, target) {
      if (action.startsWith('admin-period-')) return this.periods?.action(action,target);
      const form = this.form();
      if (!form || this.busy) return;
      const epoch = this.epoch;
      if (action === 'admin-save-retry') return this.submit(form, true);
      if (action === 'admin-config-refresh') {
        this.busy = true; this.lock(true);
        try {
          const latest = await this.api.settings();
          if (epoch !== this.epoch) return;
          this.snapshot = latest; this.conflict = false;
          this.updateStatus(form, latest);
          const c = latest.config;
          this.message(t('已读取最新版本，输入仍保留。当前服务端：地址 {url}；换算 {quota}；抽奖 {actions}；影子模式 {shadow}。请核对后再保存。', { url: c.newapi_internal_base_url, quota: c.quota_per_unit, actions: t(c.actions_enabled ? '开启' : '关闭'), shadow: t(c.reward_shadow_mode ? '开启' : '关闭') }));
        } catch (error) { if (epoch === this.epoch) this.message(errorText(error), true); }
        finally { if (epoch === this.epoch) { this.busy = false; this.lock(false); this.recovery(); } }
        return;
      }
      if (this.pending) return;
      const key = target?.dataset.key;
      if (!PULSE_ADMIN_SECRET_KEYS.includes(key)) return;
      const input = form.elements.namedItem(key);
      if (action === 'admin-secret-show') {
        input.type = input.type === 'password' ? 'text' : 'password';
        target.textContent = t(input.type === 'password' ? '显示输入' : '隐藏输入');
      }
      if (action === 'admin-secret-copy') {
        if (!input.value) { this.message(t('当前没有新输入可复制；已有密钥不会回显。')); return; }
        try { await navigator.clipboard.writeText(input.value); this.message(t('已复制当前输入。请粘贴到对应系统，勿发到聊天或日志。')); }
        catch (_) { this.message(t('无法自动复制，请显示输入后手动复制。'), true); }
      }
      if (action === 'admin-secret-generate') {
        if (input.value) { this.message(t('当前已有新输入。若需要重新生成，请先清空该输入框。'), true); return; }
        this.busy = true; this.lock(true);
        try { const secret = await this.api.generateSecret(); if (epoch !== this.epoch) return; input.value = secret; this.message(t('新密钥已填入，尚未保存。请妥善保存新值；首次配对与已有密钥轮换的操作顺序不同。')); }
        catch (error) { if (epoch === this.epoch) this.message(errorText(error), true); }
        finally { if (epoch === this.epoch) { this.busy = false; this.lock(false); } }
      }
    }
  }
  window.MetarPulseAdmin = Object.freeze({ View, errorText });
})();
