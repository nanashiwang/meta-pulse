/* Community experience is independent of reputation and paid Pulse benefits. */
(function (root, factory) {
  if (typeof module === "object" && module.exports) module.exports = factory();
  else root.MetarGrowth = factory();
})(typeof window === "undefined" ? globalThis : window, () => {
  "use strict";
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
  const t = (zh, en) =>
    typeof window !== "undefined" && window.MetarI18n?.locale().startsWith("en")
      ? en
      : zh;
  const number = (v) => (Number.isSafeInteger(v) ? v.toLocaleString() : "—");
  const time = (v) => new Date(v * 1000).toLocaleString();
  const levelName = (l) =>
    t(
      l.name,
      [
        "New friend",
        "New arrival",
        "Active member",
        "Helpful member",
        "Familiar face",
        "Experienced member",
        "Community builder",
        "Long-term contributor",
        "Community companion",
      ][l.number] || l.name,
    );
  const kinds = {
    checkin: ["签到", "Check-in"],
    question: ["有效问题", "Question"],
    answer: ["有效回答", "Answer"],
    like: ["获得认可", "Upvote"],
    accepted: ["回答被采纳", "Accepted answer"],
    featured: ["精选内容", "Featured content"],
    pulse: ["脉冲抽奖", "Pulse reward"],
    adjustment: ["人工调整", "Adjustment"],
  };
  function progress(s) {
    const min = s.level.minimum,
      max = s.next_level?.minimum;
    return max
      ? Math.max(0, Math.min(100, ((s.experience - min) / (max - min)) * 100))
      : 100;
  }
  function card(s, compact = false) {
    return `<section class="card card-pad growth-card growth-${esc(s.appearance)}"><div class="between wrap"><div><div class="eyebrow">${t("社区成长", "COMMUNITY GROWTH")}</div><h2>Lv.${s.level.number} · ${esc(levelName(s.level))}</h2></div><button class="btn primary" data-growth="checkin" ${s.checkin_amount ? "" : "disabled"}>${s.checkin_amount ? t("签到", "Check in") + " +" + s.checkin_amount + " EXP" : t("今日已签到", "Checked in")}</button></div><div class="growth-progress" role="progressbar" aria-label="${t("升级进度", "Level progress")}" aria-valuenow="${Math.round(progress(s))}" aria-valuemin="0" aria-valuemax="100"><span style="width:${progress(s)}%"></span></div><div class="between wrap muted"><span>${number(s.experience)}${s.next_level ? " / " + number(s.next_level.minimum) : ""} EXP</span><span>${s.next_level ? t("距离下一级还差 ", "Next level: ") + number(s.next_level.minimum - s.experience) + " EXP" : t("已达到最高等级", "Maximum level")}</span></div><p class="muted mt12">${t("今日常规经验", "Daily experience")} ${number(s.today)} / ${number(s.daily_cap)} · ${t("连续签到", "Check-in streak")} ${s.streak} ${t("天", "days")}</p>${compact ? `<a class="textlink" href="/me/growth" data-router>${t("查看成长与经验明细", "View growth and experience history")} →</a>` : ""}</section>`;
  }
  function entry(v) {
    const [zh, en] = kinds[v.kind] || ["经验", "Experience"];
    return `<li class="growth-entry"><div><strong>${t(zh, en)}</strong><p class="muted">${esc(v.reason)}</p><small>${esc(time(v.created_at || v.eligible_at))}</small>${v.object_type === "question" && v.object_id !== "0" ? ` · <a href="/questions/${encodeURIComponent(v.object_id)}">${t("查看内容", "View content")}</a>` : ""}</div><strong class="${v.delta < 0 ? "growth-negative" : ""}">${v.delta === undefined ? t("待发放", "Pending") : (v.delta > 0 ? "+" : "") + number(v.delta)} EXP</strong></li>`;
  }
  class View {
    constructor(adapter, admin) {
      this.api = adapter;
      this.admin = admin;
      this.busy = false;
      this.user = null;
      this.version = 0;
      this.entries = [];
      this.before = 0;
      this.notice = "";
    }
    async request(op, body, admin = false, key) {
      return (admin ? this.admin : this.api).request(
        "/metar/experience/" + op,
        body === undefined
          ? {}
          : {
              method: op === "rules" ? "PUT" : "POST",
              headers: {
                "Content-Type": "application/json",
                "X-Metar-Request": "1",
                ...(key ? { "Idempotency-Key": key } : {}),
              },
              body: JSON.stringify(body),
            },
      );
    }
    async compact() {
      try {
        return card(await this.request("summary"), true);
      } catch (_) {
        return `<section class="card card-pad"><h3>${t("社区成长", "Community growth")}</h3><p class="muted">${t("经验服务暂不可用，论坛仍可正常使用。", "Experience is temporarily unavailable. The community remains available.")}</p></section>`;
      }
    }
    async page() {
      const s = await this.request("summary");
      const [entries, pending] = await Promise.all([
        this.request("history"),
        this.request("pending"),
      ]);
      this.entries = entries;
      this.before = entries.length === 30 ? entries.at(-1).id : 0;
      const names = [
        ["default", 0, "默认", "Default"],
        ["frame", 3, "成员头像框", "Member frame"],
        ["sage", 5, "个人主页主题", "Profile theme"],
        ["builder", 6, "共建者头像框", "Builder frame"],
        ["veteran", 7, "长期贡献徽章", "Contributor badge"],
        ["companion", 8, "社区同行徽章", "Companion badge"],
      ];
      return `<div class="stack">${this.notice ? `<p role="status" class="community-notice">${esc(this.notice)}</p>` : ""}${card(s)}${s.pulse_sync_pending ? `<p class="prod-status" role="status">${t("脉冲经验仍在同步，稍后刷新可继续领取；签到和社区经验不受影响。", "Pulse experience is still syncing. Refresh later to resume; community experience remains available.")}</p>` : ""}${s.notices.length ? `<section class="card card-pad"><h3>${t("升级通知", "Level updates")}</h3>${s.notices.map((n) => `<p>${t("已解锁社区等级", "Unlocked community level")} Lv.${n.level}</p>`).join("")}<button class="btn" data-growth="read">${t("标记已读", "Mark as read")}</button></section>` : ""}<section class="card card-pad"><h3>${t("等级装扮", "Level appearance")}</h3><div class="growth-choices">${names.map(([key, level, zh, en]) => `<button class="btn ${s.appearance === key ? "primary" : ""}" data-growth="appearance" data-value="${key}" ${s.level.number < level ? "disabled" : ""}>${t(zh, en)}${s.level.number < level ? " · Lv." + level : ""}</button>`).join("")}</div></section><section class="card card-pad"><h3>${t("如何获得经验", "How to earn experience")}</h3><p class="muted">${t("经验累计不清零，不兑换余额或脉冲券。脉冲抽奖经验和精选经验不占常规每日上限。", "Experience never expires and cannot be exchanged for balance or Pulse tickets. Pulse and featured awards are outside the daily cap.")}</p><ul class="mini-list">${[
        ["checkin", s.rules.checkin],
        ["question", s.rules.question.amount],
        ["answer", s.rules.answer.amount],
        ["like", s.rules.like.amount],
        ["accepted", s.rules.accepted.amount],
        ["featured", s.rules.featured],
      ]
        .map(([k, v]) => `<li>${t(...kinds[k])}<strong>+${v} EXP</strong></li>`)
        .join(
          "",
        )}</ul><p class="muted">${t("问题每天最多 ", "Questions per day: ")}${s.rules.question.daily_count} · ${t("回答每天最多 ", "Answers per day: ")}${s.rules.answer.daily_count} · ${t("点赞每天最多 ", "Upvote EXP per day: ")}${s.rules.like.daily_amount} EXP · ${t("采纳每天最多 ", "Accepted answers per day: ")}${s.rules.accepted.daily_count}</p><p class="muted">${t("点赞者须邮箱已验证、注册满 7 天并达到 Lv.1；同一人对同一作者每天最多奖励 3 次。精选每月最多 ", "Voters need a verified email, a 7-day-old account and Lv.1. The same voter can reward an author 3 times a day. Monthly featured limit: ")}${s.rules.featured_monthly_count}</p><p class="muted">${t("连续签到满 7 天起每日 +", "From a 7-day streak, check in for +")}${s.rules.streak_checkin} EXP. ${t("问题、回答及互动需经过 24 小时观察；撤赞、取消采纳、删帖会撤销对应经验。", "Posts and interactions have a 24-hour observation period. Removed content or interactions revoke their experience.")}</p></section><section class="card card-pad"><h3>${t("累计等级", "Lifetime levels")}</h3><ul class="mini-list">${s.levels.map((l) => `<li>Lv.${l.number} · ${esc(levelName(l))}<strong>${number(l.minimum)} EXP</strong></li>`).join("")}</ul><p class="muted">${t("等级表示参与积累；专家、导师等专业称号另行审核授予。", "Levels reflect participation. Professional titles are awarded through separate review.")}</p></section>${pending.length ? `<section class="card card-pad"><h3>${t("待发放", "Pending experience")}</h3><ul class="growth-list">${pending.map(entry).join("")}</ul></section>` : ""}<section class="card card-pad"><h3>${t("经验明细", "Experience history")}</h3><ul class="growth-list" id="growth-history">${entries.map(entry).join("") || `<li class="muted">${t("还没有经验记录，完成今日签到开始成长。", "No experience yet. Start with your daily check-in.")}</li>`}</ul><button class="btn" data-growth="more" ${this.before ? "" : "hidden"}>${t("加载更多", "Load more")}</button></section></div>`;
    }
    async adminPage() {
      const s = await this.request("settings", undefined, true);
      this.version = s.version;
      const fields = [
        ["daily_cap", t("常规每日总上限", "Daily total cap")],
        ["checkin", t("每日签到", "Daily check-in")],
        ["streak_checkin", t("连续签到奖励", "Streak reward")],
        ["featured", t("精选经验", "Featured experience")],
        [
          "featured_monthly_count",
          t("每月精选次数", "Featured awards per month"),
        ],
      ];
      for (const kind of ["question", "answer", "like", "accepted"])
        for (const [key, label] of [
          ["amount", t("单次经验", "Amount")],
          ["daily_count", t("每日次数", "Daily count")],
          ["daily_amount", t("每日经验上限", "Daily cap")],
        ])
          fields.push([kind + "." + key, t(...kinds[kind]) + " · " + label]);
      this.rules = s.rules;
      return `<div class="stack">${this.notice ? `<p role="status" class="prod-status">${esc(this.notice)}</p>` : ""}<p class="muted">${t("规则修改只影响新事件；已有经验和待发放记录保留原规则。", "Rule changes apply only to new events. Existing and pending experience keep their original rules.")}</p><form class="card card-pad" data-growth-form="rules"><h3>${t("经验规则", "Experience rules")} · v${s.version}</h3><div class="growth-fields">${fields.map(([key, label]) => `<label>${esc(label)}<input class="input" type="number" min="1" required name="${key}" value="${key.includes(".") ? s.rules[key.split(".")[0]][key.split(".")[1]] : s.rules[key]}"></label>`).join("")}</div><label>${t("修改原因", "Reason")}<input class="input" name="reason" required maxlength="500"></label><button class="btn primary">${t("保存新版本", "Save new version")}</button></form><form class="card card-pad" data-growth-form="featured"><h3>${t("评选精选内容", "Feature content")}</h3><label>${t("类型", "Type")}<select class="input" name="object_type"><option value="question">${t("问题", "Question")}</option><option value="answer">${t("回答", "Answer")}</option></select></label><label>${t("内容 ID", "Content ID")}<input class="input" name="object_id" pattern="[1-9][0-9]*" required></label><label>${t("评选理由", "Reason")}<input class="input" name="reason" required maxlength="500"></label><button class="btn primary">${t("评为精选并发放经验", "Feature and award experience")}</button></form><form class="card card-pad" data-growth-form="pulse/reverse"><h3>${t("撤销脉冲经验奖励", "Reverse a Pulse experience prize")}</h3><label>${t("奖励编号", "Grant ID")}<input class="input" name="grant_id" required maxlength="64"></label><label>${t("撤销原因", "Reason")}<input class="input" name="reason" required maxlength="255"></label><button class="btn">${t("提交撤销", "Reverse prize")}</button><p class="muted">${t("撤销后，用户下次打开成长页或 Pulse 页时同步扣回。", "The reversal syncs when the member next opens Growth or Pulse.")}</p></form><form class="card card-pad" data-growth-form="adjustment"><h3>${t("人工调整经验", "Adjust experience")}</h3><label>${t("Answer 用户 ID", "Answer user ID")}<input class="input" name="user_id" pattern="[1-9][0-9]*" required></label><label>${t("调整值（负数为扣减）", "Change (negative to deduct)")}<input class="input" name="delta" type="number" min="-100000" max="100000" required></label><label>${t("调整原因（将对用户可见）", "Reason (visible to the user)")}<input class="input" name="reason" required maxlength="500"></label><button class="btn primary">${t("记录调整", "Record adjustment")}</button></form></div>`;
    }
    async click(button, refresh) {
      if (this.busy) return;
      this.busy = true;
      button.disabled = true;
      try {
        const action = button.dataset.growth;
        if (action === "more") {
          const target = document.querySelector("#growth-history");
          const rows = await this.request("history?before=" + this.before);
          target?.insertAdjacentHTML("beforeend", rows.map(entry).join(""));
          this.before = rows.length === 30 ? rows.at(-1).id : 0;
          button.hidden = !this.before;
          return;
        }
        if (action === "checkin") await this.request("checkin", {});
        if (action === "read") await this.request("notices/read", {});
        if (action === "appearance")
          await this.request("appearance", {
            appearance: button.dataset.value,
          });
        this.notice = t("已更新", "Updated");
        await refresh();
      } catch (error) {
        this.notice = t(
          "操作未完成，请刷新核对后重试。",
          "Operation not confirmed. Refresh to check before retrying.",
        );
        alert(this.notice);
      } finally {
        this.busy = false;
        button.disabled = false;
      }
    }
    async submit(form, refresh) {
      if (this.busy) return;
      this.busy = true;
      const button = form.querySelector("button");
      button.disabled = true;
      try {
        const op = form.dataset.growthForm;
        const values = Object.fromEntries(new FormData(form));
        let body = values;
        if (op === "rules") {
          const rules = structuredClone(this.rules);
          for (const [key, value] of Object.entries(values)) {
            if (key === "reason") continue;
            const [a, b] = key.split(".");
            if (b) rules[a][b] = Number(value);
            else rules[a] = Number(value);
          }
          body = { version: this.version, rules, reason: values.reason };
        }
        if (op === "adjustment") body.delta = Number(values.delta);
        const serialized = JSON.stringify(body);
        if (form.dataset.payload !== serialized) {
          form.dataset.request = crypto.randomUUID();
          form.dataset.payload = serialized;
        }
        await this.request(op, body, true, form.dataset.request);
        this.notice = t("已保存", "Saved");
        await refresh();
      } catch (_) {
        alert(
          t(
            "保存未确认，请保留页面重试；规则冲突时刷新后重新修改。",
            "Save not confirmed. Retry on this page; refresh if the rule version changed.",
          ),
        );
      } finally {
        this.busy = false;
        button.disabled = false;
      }
    }
  }
  return { View, progress, card, entry };
});
