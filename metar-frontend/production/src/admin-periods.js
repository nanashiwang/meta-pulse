/* Period economics are immutable once created; this form creates the next pool. */
"use strict";
(() => {
  const t = (...args) => window.MetarI18n.t(...args);
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
      <label>${t("奖励类型")}<select name="prize_type"><option value="newapi_quota">${t("API 调用额度")}</option><option value="community_exp" ${prize.reward_type === 'community_exp' ? 'selected' : ''}>${t("社区经验 EXP")}</option></select></label><label>${t("奖励数量（整数）")}<input name="prize_amount" required inputmode="numeric" pattern="[1-9][0-9]*" value="${esc(prize.amount || '')}"></label>
      <label>${t("概率权重")}<input name="prize_weight" required inputmode="numeric" pattern="[1-9][0-9]*" value="${esc(prize.weight || '')}"></label>
      <button type="button" class="btn small" data-action="admin-period-remove">${t("移除奖项")}</button></div>`;
  }
  class View {
    constructor(api) {
      this.api = api;
      this.epoch = 0;
      this.busy = false;
      this.pending = null;
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
      if (list.continuous_supported !== true || list.unlimited_quota_supported !== true) return `<section class="card card-pad mt24"><h2>${t("贡献度与脉冲券")}</h2><p class="muted mt16">${t("奖励配置暂不可用，请确认服务已升级并完成管理通道配对。")}</p></section>`;
      const now = Date.now();
      this.current = list.periods.filter(p => p.status === 'active' && Date.parse(p.starts_at) <= now && Date.parse(p.ends_at) > now).sort((a,b) => Number(Boolean(b.continuous))-Number(Boolean(a.continuous)) || Date.parse(b.starts_at)-Date.parse(a.starts_at) || b.id-a.id)[0];
      return `<section class="card card-pad mt24"><div class="between wrap"><h2>${t("贡献度与脉冲券")}</h2><button type="button" class="btn primary" data-action="admin-period-toggle" aria-expanded="${Boolean(this.pending)}" aria-controls="admin-period-editor">${t("设置兑换比例")}</button></div>
        <p class="muted mt16">${t("保存后用于后续调用和新券，无需设置周期。已有券保留领取时的有效天数与概率；未成券的贡献度继续累计。")}</p>
        <div class="mt16 prod-period-list">${list.periods.length ? list.periods.map((p) => `<div class="prod-status mt8"><div><strong>${esc(p.key)} · ${esc(statusText(p.status))}</strong><p>${p.continuous ? `${esc(dateText(p.starts_at))} · ${t("长期有效")} · ${esc(p.quota_validity_days)} ${t("天内可抽额度")}` : `${esc(dateText(p.starts_at))} → ${esc(dateText(p.ends_at))} (UTC+8)`}</p><p>${t("贡献倍率")}：${p.rules.map((r) => `${esc(r.key === "default" ? t("通用") : r.key)} ${esc(format(r.multiplier_bps, 4))}×`).join("、")} · ${t("每张券所需贡献度")}：${p.ticket_threshold_milli ? esc(format(p.ticket_threshold_milli, 3)) : t("沿用服务器默认门槛")}</p><p>${t("额度上限")}：${p.quota_budget_unlimited ? t("不限总量") : esc(p.reward_budget ?? 0)}</p></div><span class="badge">${t("只读")}</span></div>`).join("") : `<p class="muted">${t("尚无规则，请设置贡献比例与奖项。")}</p>`}</div>
        <form id="admin-period-editor" data-form="admin-period" class="prod-admin-form mt24" ${this.pending ? "" : "hidden"}>
        <fieldset ${this.pending ? "disabled" : ""}><div class="prod-admin-grid">
          <label>${t("额度奖励有效天数")}<input name="quota_validity_days" type="number" min="1" max="3650" step="1" value="${this.current?.quota_validity_days || 30}" required><span class="prod-field-help">${t("从每张券获得时起计算，默认 30 天；到期后仅抽取经验。")}</span></label>
          <label>${t("API 贡献倍率")}<input name="multiplier" type="number" min="0.0001" max="100" step="0.0001" value="${this.current?.rules?.[0] ? esc(format(this.current.rules[0].multiplier_bps,4)) : 1}" required><span class="prod-field-help">${t("1 表示原始贡献的 1 倍；最多支持 4 位小数。")}</span></label>
          <label>${t("每张脉冲券所需贡献度")}<input name="threshold" type="number" min="0.001" step="0.001" required value="${this.current?.ticket_threshold_milli ? esc(format(this.current.ticket_threshold_milli,3)) : ""}" placeholder="1000"><span class="prod-field-help">${t("未成券贡献度持续累计，按产券时门槛转换；最多支持 3 位小数。")}</span></label>
        </div>
        <h3 class="mt24">${t("新券奖励规则")}</h3><p class="prod-field-help">${t("每次独立抽取，中奖不减少奖项权重。至少设置一个经验奖项。新规则可取消额度总上限；旧规则保持不变，保存不会开启抽奖。")}</p>
        <label class="prod-check mt16"><input name="unlimited_quota" type="checkbox" checked> ${t("不限制额度奖励总量")}</label><p class="prod-field-help">${t("通过奖项和权重控制平均成本；实际支出会波动，不保证固定总额。经验预算仍单独生效。")}</p><label class="prod-admin-field mt16">${t("额度奖池预算（整数 quota）")}<input name="reward_budget" value="0" inputmode="numeric" pattern="0|[1-9][0-9]*"><span class="prod-field-help">${t("不限制总量时忽略此项；取消勾选后填写额度上限。")}</span></label><label class="prod-admin-field mt16">${t("经验奖池预算（整数 EXP）")}<input name="experience_budget" required value="0" inputmode="numeric" pattern="0|[1-9][0-9]*"></label>
        <div data-period-prizes>${this.current?.rewards?.length ? this.current.rewards.map(rewardRow).join('') : rewardRow({reward_type:'community_exp'})}</div><button class="btn mt16" type="button" data-action="admin-period-add">${t("添加奖项")}</button>
        <p class="prod-field-help mt16">${t("奖项概率 = 该奖项权重 ÷ 所有奖项权重之和。到期券仅在经验奖项之间按权重抽取。")}</p>
        <button class="btn mt16" type="button" data-action="admin-period-expectation">${t("计算期望额度")}</button><p class="prod-field-help mt8" data-period-expectation role="status"></p><label class="prod-admin-field mt24">${t("修改原因")}<textarea name="reason" required minlength="3" maxlength="500" rows="2"></textarea></label>
        <label class="prod-check mt16"><input name="confirm" type="checkbox" required> ${t("我已核对有效天数、概率、额度上限模式和经验预算；已有券继续使用原规则。")}</label>
        <button type="submit" class="btn primary mt24">${t("保存新券规则")}</button></fieldset>
        <p class="prod-field-help mt16" data-period-message role="status" aria-live="polite">${this.pending ? esc(t("存在待确认的规则保存请求：{key}。请重试原请求。", { key: this.pending.body.key })) : ""}</p>
        <button class="btn primary mt16" type="button" data-action="admin-period-retry" ${this.pending ? "" : "hidden"}>${t("重试原请求")}</button>
        <button class="btn mt16" type="button" data-action="retry">${t("刷新规则列表")}</button>
        </form></section>`;
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
          "规则 {key} 已保存，对后续调用生效。请刷新规则列表查看。",
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
      if (action === "admin-period-expectation") {
        const node = form.querySelector("[data-period-expectation]");
        try {
          const rows = [...form.querySelectorAll(".prod-period-prize")].map(row => ({amount: integer(row.querySelector('[name="prize_amount"]').value), weight: integer(row.querySelector('[name="prize_weight"]').value), type: row.querySelector('[name="prize_type"]').value}));
          const value = quotaExpectation(rows);
          node.textContent = t("有效期内单次期望额度：{value} quota；这是平均值，并非单次或累计支出上限。", {value});
        } catch (_) { node.textContent = t("请先填写所有奖项的有效数量和权重。"); }
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
  window.MetarPeriodAdmin = Object.freeze({ View, fixed, format, quotaExpectation });
})();
