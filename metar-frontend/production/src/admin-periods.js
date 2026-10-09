/* Long-lived economics are immutable once saved; this form creates the next rule snapshot. */
"use strict";
(() => {
  const t = (...args) => window.MetarI18n.t(...args);
  const { formatPulseQuota, formatPulseRatio, formatPulseProbability, pulseRewardStats } = window.MetarAdapters;
  const esc = (v) =>
    String(v ?? "").replace(
      /[&<>"']/g,
      (c) =>
        ({
          "&": "&amp;",
          "<": "&lt;",
          ">": "&gt;",
          '"': "&quot;",
          "'": "&#39;",
        })[c],
    );
  const invalid = () =>
    new window.MetarAdapters.AdapterError("invalid period", {
      code: "invalid_period",
    });
  const storageKey = "metar-continuous-rules-pending-v1";
  const recommendedExpRewards = [
    { key: "exp-basic", reward_type: "community_exp", amount: 1, weight: 28000 },
    { key: "exp-middle", reward_type: "community_exp", amount: 4, weight: 12000 },
    { key: "exp-high", reward_type: "community_exp", amount: 20, weight: 10000 },
  ];
  function recommendedRewards(quotaPerUnit) {
    const unit = Number(quotaPerUnit);
    if (!Number.isSafeInteger(unit) || unit <= 0 || unit % 4 !== 0) return null;
    if (BigInt(unit) * 25n > BigInt(Number.MAX_SAFE_INTEGER)) return null;
    const quarter = unit / 4;
    return [
      { key: "api-participation", amount: quarter, weight: 44000 },
      { key: "api-third", amount: unit, weight: 5000 },
      { key: "api-second", amount: unit * 5, weight: 800 },
      { key: "api-first", amount: unit * 25, weight: 200 },
      ...recommendedExpRewards.map(r => ({...r})),
    ];
  }
  function fixed(value, digits, max = 9007199254740991n) {
    const text = String(value ?? "").trim();
    if (!new RegExp(`^(0|[1-9][0-9]*)(\\.[0-9]{1,${digits}})?$`).test(text))
      throw invalid();
    const [whole, fraction = ""] = text.split(".");
    const result =
      BigInt(whole) * 10n ** BigInt(digits) +
      BigInt(fraction.padEnd(digits, "0"));
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
    if (!Number.isSafeInteger(value) || value < 0) return "—";
    const text = String(value).padStart(digits + 1, "0");
    return `${text.slice(0, -digits)}.${text.slice(-digits)}`.replace(
      /\.?0+$/,
      "",
    );
  }
  const statusText = (status) =>
    ({
      draft: t("草稿"),
      active: t("已启用"),
      settling: t("结算中"),
      closed: t("已结束"),
    })[status] || status;
  const dateText = (value) => {
    const date = new Date(value);
    return Number.isFinite(date.getTime())
      ? new Intl.DateTimeFormat(window.MetarI18n.locale(), {
          timeZone: "Asia/Shanghai",
          year: "numeric",
          month: "2-digit",
          day: "2-digit",
          hour: "2-digit",
          minute: "2-digit",
          hour12: false,
        }).format(date)
      : "—";
  };
  const errorText = (error) => {
    if (error.code === "period_conflict" || error.status === 409)
      return t(
        "配置已被更新，或重复请求内容发生冲突。请刷新规则列表后核对。",
      );
    if (error.code === "settings_pending")
      return t("暂时无法确认保存结果，请重试原请求。");
    if (error.code === "invalid_period" || error.status === 400)
      return t(
        "请检查有效天数、倍率、每券贡献度、预算与奖项，并至少配置一个经验奖项。",
      );
    if (error.status === 401 || error.status === 403)
      return t("仅正常且已激活的社区管理员可管理 Pulse 配置。");
    return t("奖励配置暂不可用，请确认服务已升级并完成管理通道配对。");
  };
  function rewardRow(prize = {}) {
    return `<div class="prod-admin-grid prod-period-prize mt16">
      <label>${t("奖项编号")}<input name="prize_key" required pattern="[a-z0-9](?:[a-z0-9_]|-){0,63}" placeholder="reward-1" value="${esc(prize.key || '')}"></label>
      <label>${t("奖励类型")}<select name="prize_type"><option value="newapi_quota">${t("⚡️ 调用额度")}</option><option value="community_exp" ${prize.reward_type === 'community_exp' ? 'selected' : ''}>${t("社区经验 EXP")}</option></select></label><label>${t("奖励数量（整数）")}<input name="prize_amount" required inputmode="numeric" pattern="[1-9][0-9]*" value="${esc(prize.amount || '')}"></label>
      <label>${t("概率权重")}<input name="prize_weight" required inputmode="numeric" pattern="[1-9][0-9]*" value="${esc(prize.weight || '')}"></label>
      <button type="button" class="btn small" data-action="admin-period-remove">${t("移除奖项")}</button></div>`;
  }
  class View {
    constructor(api) {
      this.api = api;
      this.epoch = 0;
      this.busy = false;
      this.pending = null;
      this.quotaPerUnit = 500000;
    }
    dispose() {
      this.epoch++;
      this.busy = false;
    }
    form() {
      return document.querySelector('form[data-form="admin-period"]');
    }
    persist() {
      try {
        if (this.pending)
          sessionStorage.setItem(storageKey, JSON.stringify(this.pending));
        else sessionStorage.removeItem(storageKey);
      } catch (_) {
        /* In-memory retry still works. */
      }
    }
    restore() {
      try {
        const saved = JSON.parse(sessionStorage.getItem(storageKey) || "null");
        if (saved?.key && saved?.body?.key && saved.body.continuous === true)
          this.pending = saved;
      } catch (_) {
        /* Invalid storage does not authorize a request. */
      }
    }
    async page() {
      this.dispose();
      this.restore();
      const epoch = this.epoch;
      let list;
      try {
        list = await this.api.periods();
      } catch (e) {
        return `<section class="card card-pad mt24"><h2>${t("贡献度与脉冲券")}</h2><p class="muted mt16">${esc(errorText(e))}</p><button class="btn mt16" data-action="retry">${t("重新加载")}</button></section>`;
      }
      if (epoch !== this.epoch) return "";
      if (list.current_reward_rule_supported !== true || list.continuous_supported !== true || list.unlimited_quota_supported !== true) return `<section class="card card-pad mt24"><h2>${t("贡献度与脉冲券")}</h2><p class="muted mt16">${t("奖励配置暂不可用，请确认服务已升级并完成管理通道配对。")}</p></section>`;
      const now = Date.now();
      this.current = list.periods.filter(p => p.status === 'active' && Date.parse(p.starts_at) <= now && Date.parse(p.ends_at) > now).sort((a,b) => Number(Boolean(b.continuous))-Number(Boolean(a.continuous)) || Date.parse(b.starts_at)-Date.parse(a.starts_at) || b.id-a.id)[0];
      const preset = recommendedRewards(this.quotaPerUnit);
      const initialRewards = this.current?.rewards?.length ? this.current.rewards : (preset || recommendedExpRewards);
      return `<section class="card card-pad mt24"><div class="between wrap"><h2>${t("贡献度与脉冲券规则")}</h2><button type="button" class="btn primary" data-action="admin-period-toggle" aria-expanded="${Boolean(this.pending)}" aria-controls="admin-period-editor">${t("设置兑换比例")}</button></div>
        <p class="muted mt16">${t("保存后，所有未用券统一使用最新奖项、概率和预算；已有券的额度有效期不变，未成券贡献继续累计。")}</p>
        <div class="mt16 prod-status"><div><strong>${this.current ? t("当前长期规则已启用") : t("尚无长期规则")}</strong><p>${this.current ? `${esc(this.current.quota_validity_days)} ${t("天额度资格")} · ${t("每张券")} ${this.current.ticket_threshold_milli ? esc(format(this.current.ticket_threshold_milli, 3)) : t("沿用默认门槛")} ${t("贡献度")}` : t("请设置贡献比例与奖项。")}</p></div><span class="badge">${t("持续生效")}</span></div>
        <form id="admin-period-editor" data-form="admin-period" class="prod-admin-form mt24" ${this.pending ? "" : "hidden"}>
        <fieldset ${this.pending ? "disabled" : ""}><div class="prod-admin-grid">
          <label>${t("额度奖励有效天数")}<input name="quota_validity_days" type="number" min="1" max="3650" step="1" value="${this.current?.quota_validity_days || 30}" required><span class="prod-field-help">${t("从每张券获得时起计算，默认 30 天；到期后仅抽取经验。")}</span></label>
          <label>${t("API 贡献倍率")}<input name="multiplier" type="number" min="0.0001" max="100" step="0.0001" value="${this.current?.rules?.[0] ? esc(format(this.current.rules[0].multiplier_bps,4)) : 1}" required><span class="prod-field-help">${t("1 表示原始贡献的 1 倍；最多支持 4 位小数。")}</span></label>
          <label>${t("每张脉冲券所需贡献度")}<input name="threshold" type="number" min="0.001" step="0.001" required value="${this.current?.ticket_threshold_milli ? esc(format(this.current.ticket_threshold_milli,3)) : "5"}" placeholder="5"><span class="prod-field-help">${t("未成券贡献度持续累计，按产券时门槛转换；最多支持 3 位小数。推荐方案为每 5 contribution 产 1 张券。")}</span></label>
        </div>
        <h3 class="mt24">${t("统一抽奖规则")}</h3><p class="prod-field-help">${t("每次独立抽取，中奖不减少奖项权重。至少设置一个经验奖项。所有未用券使用新预算，旧奖励继续由原预算结算；保存不会开启抽奖。")}</p>
        <label class="prod-check mt16"><input name="unlimited_quota" type="checkbox" ${(!this.current || this.current.quota_budget_unlimited) ? "checked" : ""}> ${t("不限制额度奖励总量")}</label><p class="prod-field-help">${t("通过奖项和权重控制平均成本；实际支出会波动，不保证固定总额。经验预算仍单独生效。")}</p><label class="prod-admin-field mt16">${t("额度奖池预算（整数 quota）")}<input name="reward_budget" value="${(!this.current || this.current.quota_budget_unlimited) ? "0" : ""}" inputmode="numeric" pattern="0|[1-9][0-9]*"><span class="prod-field-help">${t("不限制总量时忽略此项；取消勾选后填写额度上限。")}</span></label><label class="prod-admin-field mt16">${t("经验奖池预算（整数 EXP）")}<input name="experience_budget" required value="${this.current ? "" : 100000}" inputmode="numeric" pattern="0|[1-9][0-9]*"></label>
        <p class="prod-field-help">${t("已有规则的有限预算不自动复制。请核对本期已发放和已预留数量，再填写新预算；当前余额快照不代表预算已转移。")}</p><div data-period-prizes>${initialRewards.map(rewardRow).join('')}</div><button class="btn mt16" type="button" data-action="admin-period-add">${t("添加奖项")}</button><button class="btn mt16" type="button" data-action="admin-period-preset">${t("载入 50% 额度概率方案")}</button><p class="prod-field-help">${t("推荐方案：额度中奖率 50%，每券期望 0.25 ⚡️ 与 2.76 EXP；到期券平均 5.52 EXP。额度奖项按当前换算比例生成。")}</p>
        <p class="prod-field-help mt16">${t("奖项概率 = 该奖项权重 ÷ 所有奖项权重之和。到期券仅在经验奖项之间按权重抽取。")}</p>
        <button class="btn mt16" type="button" data-action="admin-period-expectation">${t("预览概率与期望")}</button><div class="prod-field-help mt8" data-period-expectation role="status"></div><label class="prod-admin-field mt24">${t("修改原因")}<textarea name="reason" required minlength="3" maxlength="500" rows="2"></textarea></label>
        <label class="prod-check mt16"><input name="confirm" type="checkbox" required> ${t("我已确认新奖项、概率和预算适用于所有未用券；已有券原有效期保持不变。")}</label>
        <button type="submit" class="btn primary mt24">${t("保存统一规则")}</button></fieldset>
        <p class="prod-field-help mt16" data-period-message role="status" aria-live="polite">${this.pending ? esc(t("存在待确认的规则保存请求：{key}。请重试原请求。", { key: this.pending.body.key })) : ""}</p>
        <button class="btn primary mt16" type="button" data-action="admin-period-retry" ${this.pending ? "" : "hidden"}>${t("重试原请求")}</button>
        <button class="btn mt16" type="button" data-action="retry">${t("刷新规则列表")}</button>
        </form><details class="mt24"><summary>${t("各规则预算（最近 20 组）")}</summary><p class="muted mt8">${t("新抽奖仅使用当前规则预算；历史预算保留已中奖奖励的预留与结算，不与新预算相加。API 以原始 quota 计，经验以 EXP 计。")}</p>${list.periods.map(p=>`<h3 class="mt16">${esc(p.key)}</h3><p class="muted">${esc(t("查询时间：{time}",{time:p.queried_at ? new Date(p.queried_at).toLocaleString() : '—'}))}</p><div class="prod-pulse-table"><table><thead><tr><th>${t("预算编号")}</th><th>${t("总额")}</th><th>${t("已预留")}</th><th>${t("已结算")}</th><th>${t("可用额")}</th></tr></thead><tbody>${(p.budgets||[]).map(b=>`<tr><td>${esc(b.id)}</td><td>${esc(b.unlimited?t("不限总量"):b.total)}</td><td>${esc(b.reserved)}</td><td>${esc(b.settled)}</td><td>${esc(b.unlimited?t("不限总量"):b.available)}</td></tr>`).join('')}</tbody></table></div>`).join('')}</details></section>`;
    }
    payload(form) {
      const f = new FormData(form);
      if (!f.has("confirm")) throw invalid();
      const key = 'rules-' + window.crypto.randomUUID();
      const quota_validity_days = integer(f.get("quota_validity_days"));
      if (quota_validity_days > 3650) throw invalid();
      const unlimited = f.has("unlimited_quota");
      const reward_budget =
        unlimited || String(f.get("reward_budget")) === "0"
          ? 0
          : integer(f.get("reward_budget"));
      const experience_budget =
        !f.get("experience_budget") ||
        String(f.get("experience_budget")) === "0"
          ? 0
          : integer(f.get("experience_budget"));
      const rewards = [...form.querySelectorAll(".prod-period-prize")].map(
        (row) => ({
          ...(row.querySelector('[name="prize_type"]')?.value ===
          "community_exp"
            ? { reward_type: "community_exp" }
            : {}),
          key: row.querySelector('[name="prize_key"]').value.trim(),
          amount: integer(row.querySelector('[name="prize_amount"]').value),
          weight: integer(row.querySelector('[name="prize_weight"]').value),
        }),
      );
      const reason = String(f.get("reason") || "").trim();
      if (
        !rewards.length ||
        rewards.length > 50 ||
        reason.length < 3 ||
        reason.length > 500 ||
        new Set(rewards.map((r) => r.key)).size !== rewards.length ||
        rewards.some(
          (r) =>
            !/^[a-z0-9][a-z0-9_-]{0,63}$/.test(r.key) ||
            r.amount >
              (r.reward_type === "community_exp"
                ? Math.min(1000000, experience_budget)
                : unlimited ? Number.MAX_SAFE_INTEGER : reward_budget),
        ) ||
        rewards.reduce((sum, r) => sum + BigInt(r.weight), 0n) >
          9007199254740991n
      )
        throw invalid();
      if (
        rewards.some((r) => r.reward_type === "community_exp") !==
          experience_budget > 0 ||
        (!unlimited && rewards.some((r) => !r.reward_type) !== (reward_budget > 0))
      )
        throw invalid();
      if (!rewards.some(r => r.reward_type === "community_exp")) throw invalid();
      return {
        ...(experience_budget ? { experience_budget } : {}),
        key,
        ...(unlimited && rewards.some(r => !r.reward_type) ? {quota_budget_unlimited: true} : {}),
        continuous: true,
        quota_validity_days,
        expected_period_id: this.current?.id || 0,
        multiplier_bps: fixed(f.get("multiplier"), 4, 1000000n),
        ticket_threshold_milli: fixed(f.get("threshold"), 3),
        reward_budget,
        rewards,
        reason,
      };
    }
    async submit(form, retry = false) {
      if (this.busy || (this.pending && !retry)) return;
      const epoch = this.epoch;
      const message = form.querySelector("[data-period-message]");
      try {
        if (!retry) {
          this.pending = {
            body: this.payload(form),
            key: window.crypto.randomUUID(),
          };
          this.persist();
        }
        if (!this.pending) return;
        this.busy = true;
        form.querySelector("fieldset").disabled = true;
        form.querySelector('[data-action="admin-period-retry"]').disabled =
          true;
        message.textContent = t("正在保存规则…");
        const result = await this.api.createPeriod(
          this.pending.body,
          this.pending.key,
        );
        this.pending = null;
        this.persist();
        if (epoch !== this.epoch) return;
        message.textContent = t(
          "规则 {key} 已保存，所有未用券使用新奖项与预算，原有效期不变。请刷新规则列表查看。",
          { key: result.period_key },
        );
        form.reset();
      } catch (error) {
        if (error.code !== "settings_pending") {
          this.pending = null;
          this.persist();
        }
        if (epoch === this.epoch) message.textContent = errorText(error);
      } finally {
        if (epoch === this.epoch) {
          this.busy = false;
          form.querySelector("fieldset").disabled = Boolean(this.pending);
          const retryButton = form.querySelector(
            '[data-action="admin-period-retry"]',
          );
          retryButton.hidden = !this.pending;
          retryButton.disabled = false;
        }
      }
    }
    action(action, target) {
      const form = this.form();
      if (!form || this.busy) return;
      if (action === "admin-period-toggle") {
        form.hidden = !form.hidden;
        form.oninput = () => { form.querySelector("[data-period-expectation]").textContent = ""; };
        target.setAttribute("aria-expanded", String(!form.hidden));
        return;
      }
      if (this.pending && action !== "admin-period-retry") return;
      if (action === "admin-period-expectation") {
        const node = form.querySelector("[data-period-expectation]");
        try {
          const rows = [...form.querySelectorAll(".prod-period-prize")].map(row => ({amount: integer(row.querySelector('[name="prize_amount"]').value), weight: integer(row.querySelector('[name="prize_weight"]').value), reward_type: row.querySelector('[name="prize_type"]').value}));
          node.innerHTML = rewardPreview(rows, this.quotaPerUnit);
        } catch (_) { node.textContent = t("请先填写所有奖项的有效数量和权重。"); }
        return;
      }
      if (action === "admin-period-preset") {
        const rewards = recommendedRewards(this.quotaPerUnit);
        if (!rewards) {
          form.querySelector("[data-period-expectation]").textContent = t("当前额度换算比例无法精确生成安全的整数奖项，请核对 quota_per_unit（须为 4 的倍数且不能过大）。");
          return;
        }
        form.querySelector('[data-period-prizes]').innerHTML = rewards.map(rewardRow).join('');
        form.querySelector('[name="reason"]').value = t("采用 50% 额度概率方案：每券期望 0.25 ⚡️，经验奖励为 1 / 4 / 20 EXP。");
        form.querySelector("[data-period-expectation]").innerHTML = `<p>${t("已载入奖项；发券门槛、倍率、有效期和预算设置保持原值。核对后保存才会生效。")}</p>${rewardPreview(rewards, this.quotaPerUnit)}`;
        return;
      }
      if (action === "admin-period-retry") return this.submit(form, true);
      if (this.pending) return;
      if (action === "admin-period-add" || action === "admin-period-remove") form.querySelector("[data-period-expectation]").textContent = "";
      if (
        action === "admin-period-add" &&
        form.querySelectorAll(".prod-period-prize").length < 50
      )
        form
          .querySelector("[data-period-prizes]")
          .insertAdjacentHTML("beforeend", rewardRow());
      if (
        action === "admin-period-remove" &&
        form.querySelectorAll(".prod-period-prize").length > 1
      )
        target.closest(".prod-period-prize")?.remove();
    }
  }
  function quotaExpectation(rows) {
    let numerator = 0n, denominator = 0n;
    if (!rows.length) throw invalid();
    for (const row of rows) {
      const weight = BigInt(integer(row.weight));
      const amount = BigInt(integer(row.amount));
      denominator += weight;
      if (row.type === 'newapi_quota') numerator += amount * weight;
    }
    const scaled = numerator * 10000n / denominator;
    return `${numerator}/${denominator} ≈ ${scaled / 10000n}.${String(scaled % 10000n).padStart(4, '0')}`;
  }
  function rewardPreview(rows, quotaPerUnit) {
    const s = pulseRewardStats(rows);
    const validUnit = Number.isSafeInteger(quotaPerUnit) && quotaPerUnit > 0;
    const quota = formatPulseRatio(s.quotaAmount, s.total * (validUnit ? BigInt(quotaPerUnit) : 1n)) + (validUnit ? ' ⚡️' : ' quota');
    const amount = r => r.reward_type === 'community_exp' ? r.amount + ' EXP' : formatPulseQuota(r.amount, quotaPerUnit, window.MetarI18n.locale());
    return `<p class="mt16">${esc(t('额度中奖率：{quota} · 经验中奖率：{exp}', {quota:formatPulseProbability(s.quotaWeight,s.total),exp:formatPulseProbability(s.expWeight,s.total)}))}</p>
      <div class="prod-pulse-table"><table><thead><tr><th>${t('奖励')}</th><th>${t('有效券概率')}</th><th>${t('到期券概率')}</th></tr></thead><tbody>${rows.map(r=>`<tr><td>${esc(amount(r))}</td><td>${esc(formatPulseProbability(r.weight,s.total))}</td><td>${r.reward_type === 'community_exp' ? esc(formatPulseProbability(r.weight,s.expWeight)) : '—'}</td></tr>`).join('')}</tbody></table></div>
      <p>${esc(t('有效券每抽期望：{quota}，{exp} EXP。到期券每抽期望：{expired} EXP。', {quota,exp:formatPulseRatio(s.expAmount,s.total),expired:formatPulseRatio(s.expAmount,s.expWeight)}))}</p>
      <p>${t('期望是大量抽奖的平均值，不是保底，也不保证每周总支出；总成本仍取决于券量与预算。')}</p>`;
  }
  window.MetarPeriodAdmin = Object.freeze({ View, fixed, format, quotaExpectation, recommendedRewards, rewardPreview });
})();
