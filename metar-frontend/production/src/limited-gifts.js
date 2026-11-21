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
  const points = [
    ["flash", "闪电", 2],
    ["exp", "经验值", 0],
    ["tickets", "抽奖券", 0],
  ];
  function units(text, places) {
    if (!/^\d+(?:\.\d+)?$/.test(text) || text.length > 32)
      throw Error("请输入有效的非负数。");
    const [w, d = ""] = text.split(".");
    if (d.length > places) throw Error("小数位超过单位精度。");
    const n = BigInt(w + d.padEnd(places, "0"));
    if (n > 9223372036854775807n) throw Error("数值过大。");
    return n;
  }
  function amount(n, p) {
    return p === 2 ? (Number(n) / 100).toFixed(2) : String(n);
  }
  function adminHTML() {
    const rewards = points
      .map(
        ([
          k,
          title,
        ]) => `<fieldset class="lg-prize" data-prize="${k}"><legend>${title}</legend>
      <label>发放方式<select name="${k}mode"><option value="none">不发放</option><option value="fixed">固定数量</option><option value="random">随机范围</option></select></label>
      <div class="lg-quantities"><label data-low><span>数量</span><input name="${k}lo" inputmode="decimal" aria-label="${title}数量" placeholder="—" disabled></label>
      <label data-high hidden>上限<input name="${k}hi" inputmode="decimal" aria-label="${title}上限" disabled></label></div></fieldset>`,
      )
      .join("");
    return `<details class="card card-pad lg-admin" data-gifts><summary>限时赠礼</summary><form class="member-event-form lg-form">
      <section class="lg-group lg-basic"><h3>基本设置</h3><div class="lg-basics"><label>赠礼名称<input name="name" required maxlength="80" placeholder="例如：周末回馈"></label><label>持续时间（分钟）<input type="number" name="minutes" min="1" max="43200" value="60" required></label></div></section>
      <section class="lg-group lg-gates"><h3>昨日领取门槛 <small>留空不限 · 填写项须同时满足</small></h3><div class="lg-criteria"><label>API 消耗大于<span class="lg-unit"><input name="usage" inputmode="decimal" placeholder="不限"><select aria-label="用量单位" name="unit"><option value="4">万</option><option value="8">亿</option></select></span></label>
      <label>点赞数大于<input type="number" min="0" max="1000000" name="likes" placeholder="不限"></label><label>评论数大于<input type="number" min="0" max="1000000" name="comments" placeholder="不限"></label></div></section>
      <section class="lg-group lg-rewards"><h3>奖品配置 <small>可组合发放</small></h3><div class="lg-prizes">${rewards}</div></section>
      <div class="lg-submit"><span>仅限已绑定 API 的账号，每人每轮一次。</span><button class="btn primary">开启本轮赠礼</button></div><p role="status"></p></form>
      <section class="lg-history"><div class="lg-heading"><h3>发放记录</h3><button type="button" data-list="1" class="btn">查看记录</button></div><div data-report aria-live="polite"></div></section></details>`;
  }
  function rewardFields(row) {
    const key = row.dataset.prize,
      pick = (name) => row.querySelector('[name="' + key + name + '"]');
    const mode = pick("mode").value,
      low = pick("lo"),
      high = pick("hi");
    low.disabled = mode === "none";
    high.disabled = mode !== "random";
    low.required = !low.disabled;
    high.required = !high.disabled;
    row.querySelector("[data-low] span").textContent =
      mode === "random" ? "下限" : "数量";
    row.querySelector("[data-high]").hidden = high.disabled;
    low.placeholder = low.disabled
      ? "—"
      : mode === "fixed"
        ? "固定数量"
        : "最小值";
    high.placeholder = "最大值";
  }
  function read(form) {
    const f = new FormData(form),
      val = (k) => String(f.get(k) || "").trim();
    const g = {
      name: val("name"),
      minutes: Number(val("minutes")),
      usage_raw: val("usage")
        ? String(units(val("usage"), Number(val("unit"))))
        : "",
      likes: val("likes") ? Number(val("likes")) : null,
      comments: val("comments") ? Number(val("comments")) : null,
    };
    let count = 0;
    for (const [k, , p] of points) {
      if (val(k + "mode") === "none") continue;
      let min = units(val(k + "lo"), p),
        max = val(k + "mode") === "random" ? units(val(k + "hi"), p) : min;
      if (min < 1n || max < min || max > (k === "tickets" ? 100n : 1000000n))
        throw Error("奖品数量或范围无效。");
      g[k] = { min: Number(min), max: Number(max) };
      count++;
    }
    if (!count) throw Error("至少选择一种奖品。");
    return g;
  }
  function historyGrid(rows, detail, nav) {
    if (!rows.length) return '<p class="lg-empty">暂无记录</p>';
    const labels = detail
      ? ["领取人", "领取时间", "闪电", "经验值", "抽奖券"]
      : [
          "赠礼名称",
          "发起人",
          "状态",
          "领取人数",
          "闪电",
          "经验值",
          "抽奖券",
          "明细",
        ];
    const caption = detail ? "每人领取明细" : "各轮赠礼 · 奖品列为实际到账总量";
    const body = rows
      .map((v) => {
        const initial = detail
          ? [
              esc(v.Name) + " <small>#" + esc(v.User) + "</small>",
              esc(new Date(v.At * 1000).toLocaleString()),
            ]
          : [
              esc(v.Name),
              esc(v.Actor),
              v.End * 1000 > Date.now() ? "进行中" : "已结束",
              esc(v.People),
            ];
        const rewards = points.map(([key, , precision]) => {
          const prop = { flash: "Flash", exp: "Exp", tickets: "Tickets" }[key];
          return (
            esc(amount(v[prop], precision)) +
            (detail && v[prop] > 0
              ? "<small>" +
                (v[prop + "Paid"] ? "已到账" : "待到账") +
                "</small>"
              : "")
          );
        });
        return (
          "<tr>" +
          initial
            .concat(rewards, detail ? [] : [nav(1, "每人明细", v.ID)])
            .map((cell, i) =>
              i === 0
                ? '<th scope="row">' + cell + "</th>"
                : "<td>" + cell + "</td>",
            )
            .join("") +
          "</tr>"
        );
      })
      .join("");
    return (
      '<div class="lg-table-wrap" tabindex="0" role="region" aria-label="' +
      caption +
      '"><table><caption>' +
      caption +
      "</caption><thead><tr>" +
      labels.map((h) => '<th scope="col">' + h + "</th>").join("") +
      "</tr></thead><tbody>" +
      body +
      "</tbody></table></div>"
    );
  }
  function adminMount(el, admin, command) {
    if (!el) return;
    const form = el.querySelector("form"),
      msg = form.querySelector("[role=status]");
    for (const row of form.querySelectorAll("[data-prize]")) {
      rewardFields(row);
      row.querySelector("select").onchange = () => rewardFields(row);
    }
    form.onsubmit = async (e) => {
      e.preventDefault();
      try {
        const gift = read(form);
        if (
          !root.confirm(
            "立即开启「" + gift.name + "」？开启后本轮规则不能修改。",
          )
        )
          return;
        form.querySelector("button").disabled = true;
        await command({ op: "gift_start", gift });
      } catch (e) {
        msg.textContent = e.message || "未能确认开启结果，请刷新。";
      } finally {
        form.querySelector("button").disabled = false;
      }
    };
    let seq = 0;
    el.onclick = async (e) => {
      const b = e.target.closest("[data-list]");
      if (!b) return;
      const page = Number(b.dataset.list),
        id = b.dataset.round || "",
        report = el.querySelector("[data-report]"),
        req = ++seq;
      const nav = (p, label, i) =>
        '<button class="btn" data-list="' +
        p +
        '" data-round="' +
        esc(i) +
        '">' +
        label +
        "</button>";
      try {
        const rows = await admin.request(
          "/metar/events/gift-history?page=" +
            page +
            (id ? "&id=" + encodeURIComponent(id) : ""),
        );
        if (req !== seq) return;
        report.innerHTML =
          (id ? nav(1, "返回赠礼记录", "") : "") +
          historyGrid(rows, Boolean(id), nav) +
          (page > 1 ? nav(page - 1, "上一页", id) : "") +
          (rows.length === (id ? 50 : 20) ? nav(page + 1, "下一页", id) : "");
      } catch {
        if (req === seq) report.textContent = "读取失败，请重试。";
      }
    };
  }
  function clock(end) {
    const s = Math.max(0, Math.ceil((end * 1000 - Date.now()) / 1000));
    return [Math.floor(s / 3600), Math.floor((s % 3600) / 60), s % 60]
      .map((x) => String(x).padStart(2, "0"))
      .join(":");
  }
  root.MetarLimitedGift = { units, adminHTML, adminMount, clock };
})(window);
