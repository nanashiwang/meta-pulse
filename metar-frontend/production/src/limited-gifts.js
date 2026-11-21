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
    return (
      '<details class="card card-pad" data-gifts><summary>限时赠礼</summary><form class="member-event-form"><label>赠礼名称<input name="name" required maxlength="80"></label><label>持续时间（分钟）<input type="number" name="minutes" min="1" max="43200" value="60" required></label><p>仅绑定 API 账号可领，每人每轮一次。</p><label>昨日 API 消耗严格大于<input name="usage" inputmode="decimal" placeholder="不限"></label><select aria-label="用量单位" name="unit"><option value="4">万</option><option value="8">亿</option></select>' +
      ["likes", "comments"]
        .map(
          (k, i) =>
            "<label>昨日" +
            (i ? "评论" : "点赞") +
            '数大于<input type="number" min="0" max="1000000" name="' +
            k +
            '" placeholder="不限"></label>',
        )
        .join("") +
      points
        .map(
          ([k, label, p]) =>
            "<fieldset><legend>" +
            label +
            '</legend><select name="' +
            k +
            'mode"><option value="none">不发放</option><option value="fixed">固定数量</option><option value="random">随机范围</option></select> <input name="' +
            k +
            'lo" inputmode="decimal" placeholder="固定值或下限" aria-label="' +
            label +
            '固定值或下限"> <input name="' +
            k +
            'hi" inputmode="decimal" placeholder="随机上限" aria-label="' +
            label +
            '随机上限"></fieldset>',
        )
        .join("") +
      '<button class="btn primary">开启本轮赠礼</button><p role="status"></p></form><h3>发放记录</h3><button data-list="1" class="btn">查看记录</button><div data-report></div></details>'
    );
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
  function adminMount(el, admin, command) {
    if (!el) return;
    const form = el.querySelector("form"),
      msg = form.querySelector("[role=status]");
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
          rows
            .map(
              (v) =>
                '<article class="card card-pad"><strong>' +
                esc(id ? v.Name + " (#" + v.User + ")" : v.Name) +
                "</strong><p>" +
                (id
                  ? "领取时间：" + esc(new Date(v.At * 1000).toLocaleString())
                  : "发起人：" +
                    esc(v.Actor) +
                    " · 领取人数：" +
                    esc(v.People) +
                    " · " +
                    (v.End * 1000 > Date.now() ? "进行中" : "已结束")) +
                "</p>" +
                (id ? "本次领取：" : "实际已发：") +
                points
                  .map(([k, l, p]) => {
                    const key = {
                      flash: "Flash",
                      exp: "Exp",
                      tickets: "Tickets",
                    }[k];
                    return (
                      l +
                      " " +
                      amount(v[key], p) +
                      (id && v[key] > 0
                        ? v[key + "Paid"]
                          ? "（已到账）"
                          : "（待到账）"
                        : "")
                    );
                  })
                  .join(" · ") +
                (id ? "" : nav(1, "每人明细", v.ID)) +
                "</article>",
            )
            .join("") +
          (!rows.length ? "暂无记录" : "") +
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
