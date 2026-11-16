(function (root) {
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
  class View {
    constructor(api) {
      this.api = api;
      this.epoch = 0;
    }
    dispose() {
      this.epoch++;
      this.root = null;
    }
    async page(query) {
      this.pageNo = Math.max(1, Number(query.get("page") || 1));
      this.data = await this.api.request(
        "/metar/awards/honors?page=" + this.pageNo,
      );
      return this.html();
    }
    html() {
      const d = this.data;
      return (
        '<section class="card card-pad"><h1>我的头衔与勋章</h1><p>头衔与勋章在公布入选时获得，与手动发放闪分开处理。</p><form data-honor-form><label>帖子等级旁展示的头衔<select name="title"><option value="">不展示</option>' +
        d.titles
          .map(
            (v) =>
              '<option value="' +
              esc(v.key) +
              '" ' +
              (d.selection === v.key ? "selected" : "") +
              ">" +
              esc(v.name) +
              " · " +
              (v.style === "glow" ? "月度光效" : "每周普通") +
              " · " +
              esc(new Date(v.expires_at * 1000).toLocaleString()) +
              " 到期</option>",
          )
          .join("") +
        '</select></label><button class="btn primary mt8">保存展示选择</button><p data-honor-status role="status"></p></form><h2 class="mt24">永久金色勋章</h2>' +
        (d.badges.length
          ? d.badges
              .map(
                (b) =>
                  '<span class="award-medal">🏅 ' +
                  esc(b.name) +
                  " × " +
                  b.count +
                  "</span>",
              )
              .join("")
          : "<p>暂无评选勋章</p>") +
        '<h2 class="mt24">获得记录</h2>' +
        d.records
          .map(
            (r) =>
              '<article class="card card-pad mt8"><strong>' +
              esc(r.period.key) +
              " · " +
              (r.period.type === "month" ? "月度" : "每周") +
              (r.period.kind === "post" ? "优秀帖子奖" : "优秀回应奖") +
              "</strong><p>" +
              esc(r.reason) +
              '</p><a href="/questions/' +
              esc(r.question_id) +
              (r.answer_id !== "0" ? "/" + esc(r.answer_id) : "") +
              '" target="_blank" rel="noopener">查看获奖内容 ↗</a></article>',
          )
          .join("") +
        '<div class="flex mt16">' +
        (this.pageNo > 1
          ? '<a href="/awards?view=honors&page=' +
            (this.pageNo - 1) +
            '">上一页</a>'
          : "") +
        (d.more
          ? '<a href="/awards?view=honors&page=' +
            (this.pageNo + 1) +
            '">下一页</a>'
          : "") +
        '</div><a href="/awards">返回获奖名单</a></section>'
      );
    }
    mount(node) {
      this.root = node;
      const form = node.querySelector("[data-honor-form]");
      form.onsubmit = async (e) => {
        e.preventDefault();
        const button = form.querySelector("button"),
          status = form.querySelector("[data-honor-status]"),
          epoch = this.epoch;
        button.disabled = true;
        try {
          await this.api.request("/metar/awards/command", {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              "X-Metar-Request": "1",
              "Idempotency-Key": root.crypto.randomUUID(),
            },
            body: JSON.stringify({
              op: "title",
              title_key: form.elements.title.value,
            }),
          });
          if (epoch === this.epoch)
            status.textContent = "已保存，帖子中会展示所选有效头衔。";
        } catch (_) {
          if (epoch === this.epoch)
            status.textContent = "保存失败，头衔可能已过期，请刷新后重试。";
        } finally {
          button.disabled = false;
        }
      };
    }
  }
  root.MetarAwardHonors = { View };
})(window);
