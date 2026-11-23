(function (root) {
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
  const t = (zh, en) => (root.MetarI18n?.getLanguage() === "en_US" ? en : zh);
  function currentWeek(now = new Date()) {
    const local = new Date(now.getTime() + 8 * 3600000);
    local.setUTCHours(0, 0, 0, 0);
    local.setUTCDate(local.getUTCDate() - ((local.getUTCDay() + 6) % 7));
    return local.toISOString().slice(0, 10);
  }
  const label = (kind) =>
    kind === "post"
      ? t("优秀帖子奖", "Excellent posts")
      : t("优秀回应奖", "Helpful responses");
  const errors = {
    forbidden: [
      "需要管理员权限，且不能审核自己的候选。",
      "Administrator access required; self-review is not allowed.",
    ],
    conflict: [
      "记录已变化，请刷新后重新审核。",
      "Records changed. Refresh before reviewing.",
    ],
    review_not_ready: [
      "尚未到审核时间，或所属月份仍有周奖未公布。",
      "Review is not open yet, or a required weekly selection is unpublished.",
    ],
    period_closed: [
      "本期已公布，不能再改动候选。",
      "This period is already published.",
    ],
    nomination_limit: [
      "本奖项名额已满，或同一用户已有入选内容。",
      "The award limit has been reached.",
    ],
    not_found: [
      "内容不可见、采纳已取消或候选已失效，请刷新。",
      "Content or eligibility is no longer available.",
    ],
    invalid_request: ["操作信息有误，请检查后重试。", "Check the operation details and try again."],
    reason_required: ["请先填写审核理由。", "Enter a review reason first."],
    criteria_required: ["入选或保留奖项前，请确认两项审核条件。", "Confirm both criteria before selecting or retaining the award."],
    saved_refresh_failed: ["操作已保存，但名单刷新失败。请刷新页面查看结果。", "Saved, but the list could not reload. Refresh to see the result."],
    pending: ["还有一次操作未确认，请先重试原操作。", "Retry the previous unconfirmed operation first."],
  };
  class View {
    constructor(api, admin) {
      this.api = api;
      this.adminAPI = admin;
      this.generation = 0;
      this.busy = false;
    }
    dispose() {
      this.generation++;
      this.root = null;
    }
    async page(path, user, query) {
      this.dispose();
      this.path = path;
      this.user = user;
      this.admin = path === "/admin/awards";
      this.period = {
        type: query.get("type") === "month" ? "month" : "week",
        key: query.get("key") || currentWeek(),
        kind: query.get("kind") === "response" ? "response" : "post",
      };
      if (this.period.type === "month")
        this.period.key = this.period.key.slice(0, 7);
      this.pageNo = Math.max(1, Number(query.get("page") || 1));
      this.pendingKey = "metar-awards-review-v1:" + user?.id;
      this.pending = null;
      if (this.admin) {
        try {
          this.pending = JSON.parse(
            root.sessionStorage.getItem(this.pendingKey) || "null",
          );
        } catch (_) {}
      }
      this.mine = !this.admin && query.get('view') === 'submissions';
      if (this.mine) {
        this.data = null;
        try { this.data = await this.loadSubmissions(); }
        catch (error) { if (error?.status === 401) throw error; }
        return this.submissionHTML();
      }
      this.data = await this.client().request("/metar/awards?" + this.query());
      return this.html();
    }
    scout() {
      return '<strong>' + t('伯乐奖：','Talent Scout award: ') + '</strong>' + t("推荐的帖子获得周奖后，前 10 位有效推荐者平分 20 闪，每人至少 3 闪，并各获一次「伯乐」勋章。闪由管理员手动发放，同帖月奖不重复发。", "When a recommended post wins a weekly award, the first 10 recommenders share 20 credits (minimum 3 each) and each earn a Talent Scout medal. Credits are paid manually; the monthly award does not pay again.");
    }
    async loadSubmissions() {
      const rows = await this.api.request('/metar/awards/submissions?' + new URLSearchParams({type:this.period.type,key:this.period.key}));
      if (!Array.isArray(rows)) throw Error("invalid_response");
      return rows;
    }
    submissionHTML() {
      const groups = ['post','response'].map(kind => {
        const rows = this.data?.filter(row => row.kind === kind);
        return '<section class="card card-pad mt16"><h2>' + (kind==='post'?t('优秀帖子奖','Excellent posts'):t('优秀回答奖','Excellent answers')) + '</h2>' +
          (!rows ? '<p class="muted">'+t('记录未能加载','Records unavailable')+'</p>' : rows.length ? rows.map(row => '<div class="between wrap mt8"><a href="/questions/' + esc(row.question_id) + (kind==='post'?'':'/'+esc(row.subject_id)) + '">' + esc(row.title) + (row.excerpt?' · '+esc(row.excerpt):'') + '</a>' +
            (row.can_withdraw ? '<button class="btn" data-award="withdraw" data-week="'+esc(row.week)+'" data-kind="'+kind+'" data-id="'+esc(row.subject_id)+'">'+t('取消提交','Cancel submission')+'</button>' : '<small>'+t('已截止','Closed')+'</small>')+'</div>').join('') : '<p class="muted">'+t('本期暂无提交','No submissions this period')+'</p>') + '</section>';
      });
      return '<section class="card card-pad"><a data-router href="'+esc(this.link())+'">← '+t('返回评选','Back to awards')+'</a><h1>'+t('我的提交','My submissions')+'</h1><p>'+esc(this.period.key)+' · '+t('帖子按点击提交的时间列出，无需等待审核；审核开始前可取消提交。','Posts are listed by submission date, before review. Cancel before review opens.')+'</p><p>'+this.scout()+'</p><p>'+t('优秀回答来自你在自己帖子里采纳的回答。撤销推荐不影响采纳状态。','Answers you accepted on your posts qualify. Withdrawal does not undo acceptance.')+'</p></section><p data-award-error role="status"></p>'+(this.data === null ? '<p role="alert">'+t('暂时无法读取本期提交，请稍后重试。','Could not load submissions. Please try again.')+' <a class="btn" href="'+esc(this.link(1,{view:'submissions'}))+'">'+t('重新加载','Retry')+'</a></p>' : '')+groups.join('');
    }
    client() {
      return this.admin ? this.adminAPI : this.api;
    }
    query() {
      return new URLSearchParams({ ...this.period, page: String(this.pageNo) });
    }
    link(page = this.pageNo, patch = {}) {
      return (
        this.path +
        "?" +
        new URLSearchParams({ ...this.period, ...patch, page: String(page) })
      );
    }
    reviewOpen() {
      return this.data.status === "open" && Date.now() >= this.data.review_at * 1000;
    }
    html() {
      const d = this.data,
        reviewOpen = this.reviewOpen(),
        criteria =
          this.period.kind === "post"
            ? [
                t("内容有价值", "Valuable content"),
                t("产生有效讨论", "Meaningful discussion"),
              ]
            : [
                t("回答准确", "Accurate response"),
                t("提供实际帮助", "Practical help"),
              ];
      const states = {
        pending: t("待审核", "Pending"),
        selected: t("已入选，待公布", "Selected, unpublished"),
        rejected: t("未入选", "Not selected"),
        awarded: t("已获奖", "Awarded"),
        revoked: t("已撤销", "Revoked"),
        review_required: t(
          "需复核月奖资格",
          "Monthly eligibility needs review",
        ),
      };
      return (
        `<section class="card card-pad"><div class="between wrap"><h1>` +
        t("优秀内容评选", "Community content awards") +
        `</h1><nav class="award-header-actions">` +
        (this.admin ? '<a data-router href="/awards?'+esc(this.query())+'">'+t('查看获奖名单','Published awards')+'</a>' :
          '<a class="btn primary" data-router href="'+esc(this.link(1,{type:'week',key:currentWeek(),view:'submissions'}))+'">'+t('我的提交','My submissions')+'</a>' +
          (this.user?.role_id === 2 ? '<a class="btn" data-router href="/admin/awards?'+esc(this.query())+'">'+t('管理审核','Review awards')+'</a>' : '')) +
        '</nav></div><p class="muted">' +
        t(
          "周奖每类最多10人，月奖每类最多5人，不分排名、不强制补位。",
          "Up to 10 weekly and 5 monthly winners per category. No ranking or forced allocation.",
        ) +
        `</p><p>` +
        t(
          "周奖50闪、月奖100闪由管理员手动发放；头衔与勋章在公布入选时获得。",
          "Rewards are delivered manually; titles and medals are granted upon publication.",
        ) +
        `</p><p>` + this.scout() + `</p><p><a href="/awards?view=honors">我的头衔与勋章</a></p><details><summary>` +
        t("参与规则", "Participation rules") +
        `</summary><p>` +
        t(
          "在帖子中点击“优秀稿件”提名他人内容，每天最多2次、每周10次；同一帖不能重复提名。按发帖周归属，次周周二 00:00（北京时间）截止提名并开放审核。作者采纳回答后，以服务端首次采纳时间归属优秀回应候选。月度按评选周周一所属月份归类，需等待该月全部周奖公布。",
          "Nominate others’ posts: 2 daily, 10 weekly, once per post. Nominations close and review opens at 00:00 Tuesday of the following week (Beijing time). Responses use first author acceptance. Monthly eligibility follows the month of the week’s Monday and waits for all weekly selections.",
        ) +
        `</p></details></section>
 <form data-award-period class="card card-pad mt16"><div class="flex wrap"><label>` +
        t("周期", "Period") +
        `<select name="type"><option value="week" ` +
        (this.period.type === "week" ? "selected" : "") +
        `>` +
        t("每周", "Weekly") +
        `</option><option value="month" ` +
        (this.period.type === "month" ? "selected" : "") +
        `>` +
        t("每月", "Monthly") +
        `</option></select></label><label>` +
        t("周一日期／月份", "Monday date / month") +
        `<input name="key" type="` +
        (this.period.type === "month" ? "month" : "date") +
        `" value="` +
        esc(this.period.key) +
        `" required></label><label>` +
        t("奖项", "Award") +
        `<select name="kind"><option value="post" ` +
        (this.period.kind === "post" ? "selected" : "") +
        `>` +
        label("post") +
        `</option><option value="response" ` +
        (this.period.kind === "response" ? "selected" : "") +
        `>` +
        label("response") +
        `</option></select></label><button class="btn primary">` +
        t("查看", "View") +
        `</button></div></form>
 <section class="mt16"><div class="between wrap"><h2>` +
        esc(this.period.key) +
        " · " +
        label(this.period.kind) +
        `</h2><span>` +
        (d.status === "published"
          ? t("已公布", "Published")
          : t("尚未公布", "Unpublished")) +
        `</span></div><p class="muted">` +
        t("可开始审核：", "Review opens: ") +
        esc(
          new Date(d.review_at * 1000).toLocaleString("zh-CN", {
            timeZone: "Asia/Shanghai",
          }),
        ) +
        " (UTC+8)" +
        `</p>` +
        (this.admin && d.status === "open" && !reviewOpen
          ? '<p class="muted mt8">' + t("本期审核尚未开放。到上述时间后，刷新页面即可审核。", "Review has not opened. Refresh after the time shown above.") + '</p>'
          : '') +
        (this.admin
          ? `<div class="flex wrap"><button class="btn" data-award="sync" ` +
            (d.status === "published" ? "disabled" : "") +
            `>` +
            t("同步候选", "Sync nominees") +
            `</button><button class="btn primary" data-award="publish" ` +
            (!reviewOpen ? "disabled" : "") +
            `>` +
            t("公布本期名单", "Publish selection") +
            `</button><button class="btn" data-award="audit">` +
            t("审核记录", "Audit log") +
            `</button></div>`
          : "") +
        `
 ` +
        (this.pending
          ? `<p role="status">` +
            t(
              "有一次操作尚未确认，请重试原请求。",
              "An operation is unconfirmed. Retry the original request.",
            ) +
            ` <button class="btn" data-award="retry">` +
            t("重试原操作", "Retry original") +
            `</button></p>`
          : "") +
        `<p data-award-error role="status"></p><div class="stack mt16">` +
        (d.items.length
          ? d.items
              .map(
                (c) =>
                  `<article class="card card-pad" data-candidate="` +
                  esc(c.subject_id) +
                  `"><div class="between wrap"><h3>` +
                  esc(c.name || "ID " + c.user_id) +
                  `</h3><span class="badge">` +
                  esc(states[c.status] || c.status) +
                  `</span></div><p><a href="/questions/` +
                  esc(c.question_id) +
                  (c.answer_id !== "0" ? "/" + esc(c.answer_id) : "") +
                  `" target="_blank" rel="noopener">` +
                  esc(c.title) +
                  ` ↗</a></p>` +
                  (this.admin &&
                  this.period.kind === "post" &&
                  this.period.type === "week"
                    ? `<p class="muted">` +
                      t(
                        "提名次数仅供参考：",
                        "Nominations (reference only): ",
                      ) +
                      c.nominations +
                      `</p>`
                    : "") +
                  (this.admin ? this.recommendationList(c) : "") +
                  (c.sources?.length
                    ? "<p>" +
                      t("月度来源：", "Weekly sources: ") +
                      c.sources
                        .map(
                          (s) =>
                            '<a target="_blank" rel="noopener" href="/questions/' +
                            esc(s.question_id) +
                            (s.answer_id !== "0"
                              ? "/" + esc(s.answer_id)
                              : "") +
                            '">' +
                            esc(s.week) +
                            "</a>",
                        )
                        .join(" · ") +
                      "</p>"
                    : "") +
                  (c.reason ? "<p>" + esc(c.reason) + "</p>" : "") +
                  (c.amount
                    ? "<p>" +
                      c.amount +
                      " " +
                      t(
                        c.reward_status === "manual_paid"
                          ? "闪 · 已手动发放"
                          : "闪 · 待手动发放",
                        c.reward_status === "manual_paid"
                          ? "reward units · Manually delivered"
                          : "reward units · Pending manual delivery",
                      ) +
                      "</p>"
                    : "") +
                  (this.admin
                    ? d.status === "open"
                      ? `<label><input type="checkbox" data-criterion="one" ` +
                        (c.criterion_one ? "checked" : "") +
                        `> ` +
                        criteria[0] +
                        `</label> <label><input type="checkbox" data-criterion="two" ` +
                        (c.criterion_two ? "checked" : "") +
                        `> ` +
                        criteria[1] +
                        `</label><textarea maxlength="2000" data-reason placeholder="` +
                        t(
                          "必填：具体审核依据",
                          "Required: specific review evidence",
                        ) +
                        `">` +
                        esc(c.reason) +
                        `</textarea><div class="flex mt8"><button class="btn primary" data-award="select" ` +
                        (!reviewOpen || !c.eligible ? "disabled" : "") +
                        `>` +
                        t("入选", "Select") +
                        `</button><button type="button" class="btn" data-award="reject" ` +
                        (!reviewOpen ? "disabled" : "") +
                        `>` + t("不入选", "Reject") + `</button></div>` +
                        (!reviewOpen ? '<p class="muted mt8">' + t("尚未到审核时间", "Review not open yet") + '</p>' : "")
                      : ["awarded", "review_required"].includes(c.status)
                        ? `<textarea data-reason maxlength="2000" placeholder="` +
                          t(
                            "操作备注（手动发放需注明凭据／说明）",
                            "Operation note or delivery reference (required)",
                          ) +
                          `"></textarea><button class="btn danger" data-award="revoke">` +
                          t("撤销此奖项", "Revoke award") +
                          `</button>` +
                          (c.status === "awarded" &&
                          c.reward_status !== "manual_paid"
                            ? `<button class="btn primary" data-award="paid">确认已手动发放</button>`
                            : "") +
                          (c.payment_note
                            ? `<p>发放人 ID ${esc(c.paid_by)} · ${esc(new Date(c.paid_at * 1000).toLocaleString())} · ${esc(c.payment_note)}</p>`
                            : "") +
                          (c.status === "review_required"
                            ? `<label><input type="checkbox" data-criterion="one"> ` +
                              criteria[0] +
                              `</label><label><input type="checkbox" data-criterion="two"> ` +
                              criteria[1] +
                              `</label><button class="btn primary" data-award="reconfirm">` +
                              t(
                                "复核后保留月奖",
                                "Confirm monthly eligibility",
                              ) +
                              `</button>`
                            : "")
                        : ""
                    : "") +
                  (this.admin ? '<p class="award-review-feedback" data-award-feedback role="status"></p>' : "") +
                  `</article>`,
              )
              .join("")
          : '<p class="card card-pad">' +
            t(
              this.admin
                ? "暂无候选。回应奖及月奖请先同步候选；帖子奖等待用户提名。"
                : "本期尚无已公布的入选者。",
              this.admin
                ? "No nominees. Sync response/monthly nominees; posts require nominations."
                : "No published winners for this period.",
            ) +
            "</p>") +
        `</div><div class="flex mt16">` +
        (this.pageNo > 1
          ? '<a data-router class="btn" href="' +
            esc(this.link(this.pageNo - 1)) +
            '">' +
            t("上一页", "Previous") +
            "</a>"
          : "") +
        (d.more
          ? '<a data-router class="btn" href="' +
            esc(this.link(this.pageNo + 1)) +
            '">' +
            t("下一页", "Next") +
            "</a>"
          : "") +
        `</div><div data-award-audit></div></section>`
      );
    }
    mount(node) {
      const form = node.querySelector("[data-award-period]");
      if (this.mine) {
        this.root=node;
        const gen=this.generation;
        node.querySelectorAll('[data-award="withdraw"]').forEach(b => b.onclick=async()=>{
          if(this.busy || !root.confirm(t('确认撤销这条推荐？','Withdraw this nomination?')))return;
          this.busy=true;b.disabled=true;
          try {
            b.dataset.requestKey ||= root.crypto.randomUUID();
            await this.api.request('/metar/awards/command',{method:'POST',headers:{'Content-Type':'application/json','X-Metar-Request':'1','Idempotency-Key':b.dataset.requestKey},body:JSON.stringify({op:'withdraw',period:{type:'week',key:b.dataset.week,kind:b.dataset.kind},subject_id:b.dataset.id})});
            const fresh=await this.loadSubmissions();
            if(gen===this.generation && this.root===node){this.data=fresh;node.innerHTML=this.submissionHTML();this.mount(node);node.querySelector('[data-award-error]').textContent=t('推荐已撤销。','Nomination withdrawn.');}
          }catch(e){if(gen===this.generation && this.root===node)this.error(e,b)}finally{this.busy=false;b.disabled=false}
        });return;
      }
      if (!form) return;
      this.root = node;
      const gen = this.generation;
      form.onsubmit = (e) => {
        e.preventDefault();
        const data = new FormData(form);
        root.location.assign(
          this.path +
            "?" +
            new URLSearchParams({
              type: data.get("type"),
              key:
                data.get("type") === "week"
                  ? currentWeek(new Date(data.get("key") + "T00:00:00+08:00"))
                  : data.get("key"),
              kind: data.get("kind"),
            }),
        );
      };
      form.elements.type.onchange = () => {
        const month = form.elements.type.value === "month";
        form.elements.key.type = month ? "month" : "date";
        form.elements.key.value = month
          ? this.period.key.slice(0, 7)
          : currentWeek();
      };
      node
        .querySelectorAll("[data-award]")
        .forEach(
          (b) => (b.onclick = () => this.action(b).catch((e) => {
            if (gen === this.generation && node === this.root) this.error(e, b);
          })),
        );
    }
    recommendationList(c) {
      if (!c.recommendations?.length) return '';
      const published = this.data.status === 'published';
      return '<details class="mt8"><summary>' + esc(t('推荐名单', 'Recommenders')) + ' · ' + esc(c.nominations) + '</summary><ul>' + c.recommendations.map(row => {
        const hasAward = Number.isInteger(row.cents) && row.cents > 0;
        const value = hasAward ? ' · ' + (row.cents / 100).toFixed(2) + ' ⚡️' : '';
        const label = row.paid_at ? t('已手动发放', 'Manually delivered') : c.status === 'revoked' ? t('已撤销', 'Revoked') : t('待手动发放', 'Pending manual delivery');
        const action = published && hasAward && !row.paid_at && c.status === 'awarded'
          ? ' <button type="button" class="btn small" data-award="rec_paid" data-recommender="' + esc(row.user_id) + '">' + esc(t('登记发放', 'Record delivery')) + '</button>' : '';
        return '<li>' + esc(row.name) + ' <small>ID ' + esc(row.user_id) + '</small>' + value + (published && hasAward ? ' · ' + label : '') + action + '</li>';
      }).join('') + '</ul></details>';
    }
    feedbackTarget(button) {
      return button?.closest("[data-candidate]")?.querySelector("[data-award-feedback]") || this.root?.querySelector("[data-award-error]");
    }
    error(e, button) {
      const target = this.feedbackTarget(button);
      if (!target) return;
      const code = e?.details?.error || e.message;
      target.classList.remove("green");
      target.classList.add("danger");
      target.textContent = Object.hasOwn(errors, code)
        ? t(...errors[code])
        : t(
            "操作未确认，请重试原请求或刷新。",
            "Operation unconfirmed. Retry the original request or refresh.",
          );
      if (this.pending && !this.root.querySelector('[data-award="retry"]')) {
        const b = target.ownerDocument.createElement("button");
        b.type = "button";
        b.className = "btn";
        b.textContent = t("重试原操作", "Retry original");
        b.dataset.award = "retry";
        const gen = this.generation;
        b.onclick = () => {
          if (!this.pending) return;
          this.command(this.pending.command).catch((e) => {
            if (gen === this.generation) this.error(e, button);
          });
        };
        target.append(b);
      }
    }
    async action(b) {
      if (this.busy) return;
      const op = b.dataset.award;
      if (op === "retry") {
        if (this.pending) await this.command(this.pending.command);
        return;
      }
      if (op === "audit") {
        const gen = this.generation;
        const rows = await this.adminAPI.request(
          "/metar/awards/audit?" + this.query(),
        );
        if (gen !== this.generation || !this.root) return;
        this.root.querySelector("[data-award-audit]").innerHTML =
          '<section class="card card-pad mt16"><h3>' +
          t("最近100条操作记录", "Last 100 audit entries") +
          "</h3>" +
          rows
            .map(
              (r) =>
                "<p>" +
                esc(new Date(r.created_at * 1000).toLocaleString()) +
                " · " +
                esc(r.operation) +
                " · ID " +
                esc(r.actor_id) +
                " · " +
                esc(r.reason) +
                "</p>",
            )
            .join("") +
          "</section>";
        return;
      }
      if (["select", "reject", "publish"].includes(op) && !this.reviewOpen()) {
        throw Error("review_not_ready");
      }
      if (
        op === "publish" &&
        !root.confirm(
          t(
            "公布后不能修改本期名单。允许零人入选；头衔和勋章将生效，闪由管理员另行手动发放。确认公布？",
            "Publish and freeze this period? Zero winners is allowed. Rewards will not be credited.",
          ),
        )
      )
        return;
      const item = b.closest("[data-candidate]");
      const cmd = { op, period: this.period, version: this.data.version };
      if (op === 'rec_paid') {
        if (!item) return;
        const reason = root.prompt(t('请在实际发放后输入凭据或备注。此操作仅登记，不会转账。', 'After paying, enter a receipt or note. This records delivery and does not transfer credits.'));
        if (reason === null) return;
        if (!reason.trim() || Array.from(reason.trim()).length > 2000) throw Error('reason_required');
        cmd.subject_id = item.dataset.candidate;
        cmd.recommender_id = b.dataset.recommender;
        cmd.reason = reason.trim();
      } else if (item) {
        cmd.subject_id = item.dataset.candidate;
        const reason = item.querySelector("[data-reason]");
        cmd.reason = reason.value.trim();
        cmd.criterion_one = !!item.querySelector('[data-criterion="one"]')
          ?.checked;
        cmd.criterion_two = !!item.querySelector('[data-criterion="two"]')
          ?.checked;
        if (!cmd.reason) {
          reason.focus();
          throw Error("reason_required");
        }
        if (["select", "reconfirm"].includes(op) && (!cmd.criterion_one || !cmd.criterion_two)) {
          item.querySelector('[data-criterion="' + (!cmd.criterion_one ? 'one' : 'two') + '"]')?.focus();
          throw Error("criteria_required");
        }
      }
      if (
        op === "revoke" &&
        !root.confirm(
          t(
            "确认撤销？操作会保留审核记录，相关月奖可能需要复核。",
            "Revoke this award? The audit trail is retained; related monthly awards may need review.",
          ),
        )
      )
        return;
      if (
        op === "paid" &&
        !root.confirm("请确认已经完成手动发放。此操作只登记记录，不会转账。")
      )
        return;
      const feedback = this.feedbackTarget(b);
      if (feedback) feedback.textContent = "";
      b.disabled = true;
      try { await this.command(cmd); }
      finally { b.disabled = false; }
    }
    async command(command) {
      if (this.busy) return;
      if (this.pending && this.pending.command !== command)
        throw Error("pending");
      this.pending ||= { command, key: root.crypto.randomUUID() };
      root.sessionStorage.setItem(
        this.pendingKey,
        JSON.stringify(this.pending),
      );
      this.busy = true;
      const gen = this.generation;
      let confirmed = false;
      try {
        await this.adminAPI.request("/metar/awards/command", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Metar-Request": "1",
            "Idempotency-Key": this.pending.key,
          },
          body: JSON.stringify(this.pending.command),
        });
        confirmed = true;
        this.pending = null;
        root.sessionStorage.removeItem(this.pendingKey);
        const fresh = await this.client().request(
          "/metar/awards?" + this.query(),
        );
        if (gen === this.generation && this.root) {
          this.data = fresh;
          this.root.innerHTML = this.html();
          this.mount(this.root);
          if (command.op === "reject" || command.op === "select") {
            const candidate = [...this.root.querySelectorAll("[data-candidate]")].find(row => row.dataset.candidate === command.subject_id);
            const target = candidate?.querySelector("[data-award-feedback]") || this.feedbackTarget();
            if (target) {
              target.classList.add("green");
              target.textContent = command.op === "reject" ? t("已保存为不入选。", "Rejection saved.") : t("已保存为入选。", "Selection saved.");
            }
          }
        }
      } catch (e) {
        if (confirmed) throw Error("saved_refresh_failed");
        if (e?.status >= 400 && e.status < 500) {
          this.pending = null;
          root.sessionStorage.removeItem(this.pendingKey);
        }
        throw e;
      } finally {
        this.busy = false;
      }
    }
  }
  root.MetarAwards = { View, currentWeek, esc };
})(typeof window === "undefined" ? globalThis : window);
