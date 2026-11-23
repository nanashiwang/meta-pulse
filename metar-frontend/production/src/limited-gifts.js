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
  function ruleHTML(prefix) {
    const rewards = points
      .map(
        ([
          k,
          title,
        ]) => `<fieldset class="lg-prize" data-prize="${k}"><legend>${title}</legend>
      <div class="lg-modes" role="group" aria-label="${title}发放方式">${[
        ["none", "不发放"],
        ["fixed", "固定数量"],
        ["random", "随机"],
      ]
        .map(
          ([value, label]) =>
            `<label><input type="radio" name="${prefix}${k}mode" value="${value}"${
              value === "none" ? " checked" : ""
            }>${label}</label>`,
        )
        .join("")}</div>
      <div class="lg-quantities" hidden><label data-low><span>数量</span><input name="${prefix}${k}lo" inputmode="decimal" disabled></label>
      <label data-high hidden>上限<input name="${prefix}${k}hi" inputmode="decimal" disabled></label></div></fieldset>`,
      )
      .join("");
    return `<section class="lg-rule" data-rule="${prefix}"><header class="lg-heading"><h3 data-rule-title>规则 1（最高优先级）</h3><div class="lg-rule-actions"><button type="button" class="btn" data-rule-action="up">上移</button><button type="button" class="btn" data-rule-action="down">下移</button><button type="button" class="btn" data-rule-action="remove">删除</button></div></header>
      <section class="lg-group lg-gates"><h3>领取条件 <small>点赞、评论和 API 消耗按昨日统计 · 留空不限</small></h3><div class="lg-criteria"><label>API 消耗大于<span class="lg-unit"><input name="${prefix}usage" inputmode="decimal" placeholder="不限"><select aria-label="用量单位" name="${prefix}unit"><option value="4">万</option><option value="8">亿</option></select></span></label>
      <label>点赞数大于等于<input type="number" min="0" max="1000000" name="${prefix}likes" placeholder="不限"></label><label>评论数大于等于<input type="number" min="0" max="1000000" name="${prefix}comments" placeholder="不限"></label><label>领取人数上限<input type="number" min="1" max="1000000" step="1" name="${prefix}max_claims" placeholder="留空不限"></label></div></section>
      <section class="lg-group lg-rewards"><h3>奖品配置 <small>可组合发放 · 抽奖券随机下限可填 0</small></h3><div class="lg-prizes">${rewards}</div></section></section>`;
  }
  function adminHTML() {
    return `<section class="card card-pad lg-admin" data-gifts><details data-gift-editor><summary>限时赠礼 · 创建活动</summary><p data-round-state>正在核对当前轮次…</p><form class="member-event-form lg-form">
      <section class="lg-group lg-basic"><h3>基本设置</h3><div class="lg-basics"><label>赠礼名称<input name="name" required maxlength="80" placeholder="例如：周末回馈"></label><label>持续时间（分钟）<input type="number" name="minutes" min="1" max="43200" value="60" required></label></div></section>
      <div class="lg-rule-list"><p>按顺序从上到下匹配，满员规则直接跳过；每人只领取第一组满足条件且有名额的奖励。</p><div data-rule-list>${ruleHTML(
        "r1-",
      )}</div><button type="button" class="btn" data-add-rule>＋ 添加条件与奖励</button></div>
      <div class="lg-submit"><span>仅限已绑定 API 的账号，每人每轮一次。</span><button class="btn primary" disabled>开启本轮赠礼</button></div><p role="status"></p></form>
      </details></section>`;
  }
  function historyHTML() {
    return '<section class="card card-pad lg-admin" data-gift-history><div class="lg-heading"><h3>赠礼发布记录</h3><button type="button" class="btn" data-list="1" data-refresh-history>刷新记录</button></div><div data-report aria-live="polite">正在读取发布记录…</div></section>';
  }
  function rewardFields(row) {
    const key = row.closest("[data-rule]").dataset.rule + row.dataset.prize,
      pick = (name) => row.querySelector('[name="' + key + name + '"]');
    const mode = row.querySelector('[name="' + key + 'mode"]:checked').value,
      low = pick("lo"),
      high = pick("hi");
    row.querySelector(".lg-quantities").hidden = mode === "none";
    low.disabled = mode === "none";
    high.disabled = mode !== "random";
    low.required = !low.disabled;
    high.required = !high.disabled;
    row.querySelector("[data-low] span").textContent =
      mode === "random" ? "下限" : "数量";
    row.querySelector("[data-high]").hidden = high.disabled;
    low.placeholder = mode === "random" ? "最小值" : "固定数量";
    high.placeholder = "最大值";
    if (mode === "random" && row.dataset.prize === "tickets")
      low.placeholder = "可填 0";
  }
  function read(form) {
    const f = new FormData(form),
      val = (k) => String(f.get(k) || "").trim();
    const groups = Array.from(
      form.querySelectorAll("[data-rule]"),
      (group, index) => {
        try {
          return readRule((key) => val(group.dataset.rule + key));
        } catch (e) {
          throw Error("规则 " + (index + 1) + "：" + e.message);
        }
      },
    );
    return { name: val("name"), minutes: Number(val("minutes")), groups };
  }
  function readRule(val) {
    const g = {
      usage_raw: val("usage")
        ? String(units(val("usage"), Number(val("unit"))))
        : "",
      likes: val("likes") ? Number(val("likes")) : null,
      comments: val("comments") ? Number(val("comments")) : null,
    };
    const cap = val("max_claims");
    if (cap) {
      const n = Number(cap);
      if (!/^\d+$/.test(cap) || !Number.isInteger(n) || n < 1 || n > 1000000)
        throw Error("领取人数上限必须是 1 到 1000000 的整数，留空不限。");
      g.max_claims = n;
    }
    let count = 0;
    for (const [k, , p] of points) {
      if (val(k + "mode") === "none") continue;
      const random = val(k + "mode") === "random";
      const min = units(val(k + "lo"), p),
        max = random ? units(val(k + "hi"), p) : min;
      const minimum = random && k === "tickets" ? 0n : 1n;
      if (
        min < minimum ||
        max < 1n ||
        max < min ||
        max > (k === "tickets" ? 100n : 1000000n)
      )
        throw Error("奖品数量或范围无效。");
      g[k] = { min: Number(min), max: Number(max) };
      count++;
    }
    if (!count) throw Error("至少选择一种奖品。");
    return g;
  }
  function historyGrid(rows, detail, nav, translate = (text) => text) {
    if (!rows.length)
      return (
        '<p class="lg-empty">' +
        esc(translate(detail ? "暂无领取记录。" : "暂无记录")) +
        "</p>"
      );
    const labels = detail
      ? ["领取人", "领取时间", "命中规则", "闪电", "经验值", "抽奖券"]
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
    const caption = esc(
      translate(detail ? "每人领取明细" : "各轮赠礼 · 奖品列为实际到账总量"),
    );
    const body = rows
      .map((v) => {
        const initial = detail
          ? [
              esc(v.Name) + " <small>#" + esc(v.User) + "</small>",
              esc(new Date(v.At * 1000).toLocaleString()),
              v.rule_number > 0 ? "#" + esc(v.rule_number) : esc(translate("未记录档位")),
            ]
          : [
              esc(v.Name),
              esc(v.Actor),
              v.End > (v.ServerNow ?? Date.now() / 1000) ? "进行中" : "已结束",
              esc(v.People),
            ];
        const rewards = points.map(([key, , precision]) => {
          const prop = { flash: "Flash", exp: "Exp", tickets: "Tickets" }[key];
          return (
            esc(amount(v[prop], precision)) +
            (detail && prop === "Tickets" && v.no_ticket_award
              ? "<small>" + esc(translate("本次未获得抽奖券")) + "</small>"
              : detail && v[prop] > 0
                ? "<small>" +
                  esc(translate(v[prop + "Paid"] ? "已到账" : "待到账")) +
                  "</small>"
                : "")
          );
        });
        return (
          "<tr>" +
          initial
            .concat(rewards, detail ? [] : ['<div class="lg-report-actions">' + nav(1, "档位配置", v.ID, "tiers") + nav(1, "每人明细", v.ID) + "</div>"])
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
    return reportTable(caption, labels.map(label => translate(label)), body);
  }
  function reportTable(caption, labels, body) {
    return '<div class="lg-table-wrap" tabindex="0" role="region" aria-label="' +
      caption + '"><table><caption>' + caption + '</caption><thead><tr>' +
      labels.map(label => '<th scope="col">' + esc(label) + '</th>').join('') +
      '</tr></thead><tbody>' + body + '</tbody></table></div>';
  }
  function usageAmount(raw) {
    const digits = BigInt(raw).toString();
    const places = digits.length > 8 ? 8 : 4;
    const padded = digits.padStart(places + 1, "0");
    const fraction = padded.slice(-places).replace(/0+$/, "");
    return padded.slice(0, -places) + (fraction ? "." + fraction : "") + (places === 8 ? " 亿" : " 万");
  }
  function tierGrid(round, id, nav) {
    const compare = round.social_inclusive ? "≥" : ">";
    const pillList = items => '<div class="lg-tier-values">' + items.map(value => '<span>' + esc(value) + '</span>').join('') + '</div>';
    const totals = v => pillList([amount(v.flash, 2) + " 闪", v.exp + " 经验值", v.tickets + " 张券"]);
    const body = round.tiers.map(tier => {
      const rule = tier.rule;
      const gates = [];
      if (rule.usage_raw) gates.push("API 消耗 > " + usageAmount(rule.usage_raw));
      if (rule.likes != null) gates.push("点赞 " + compare + " " + rule.likes);
      if (rule.comments != null) gates.push("评论 " + compare + " " + rule.comments);
      const prizes = points.flatMap(([key, label, precision]) => {
        const reward = rule[key];
        if (!reward) return [];
        const low = amount(reward.min, precision), high = amount(reward.max, precision);
        return [label + " · " + (low === high ? "固定 " + low : "随机 " + low + "–" + high)];
      });
      return '<tr><th scope="row">档位 ' + esc(tier.number) + (tier.number === 1 ? '<small>最高优先级</small>' : '') +
        '</th><td>' + (gates.length ? pillList(gates) : "无限制") + '</td><td>' + pillList(prizes) +
        '</td><td>' + esc(tier.people) + ' / ' + esc(rule.max_claims ?? "不限") + '</td><td>' + totals(tier) +
        '</td><td>' + nav(1, "本档领取明细", id, String(tier.number)) + '</td></tr>';
    }).join('');
    const legacy = round.unassigned;
    const unassigned = legacy?.people > 0
      ? '<tr><th scope="row">未记录档位</th><td colspan="2">这些领取记录未保存命中的档位，不能归入任一档。</td><td>' + esc(legacy.people) +
        '</td><td>' + totals(legacy) + '</td><td>' + nav(1, "查看未归档明细", id, "0") + '</td></tr>'
      : '';
    return '<h4 class="lg-report-title">' + esc(round.name) + ' · 档位配置</h4>' +
      '<p class="lg-report-note">按发布时的优先级排列；同档填写的条件须同时满足。领取人数包含尚未到账及抽中零张券的记录。</p>' +
      reportTable("各档配置及到账合计", ["档位", "昨日门槛", "奖品配置", "已领取 / 上限", "实际到账", "明细"], body + unassigned);
  }
  // The caller supplies the administrator adapter. The server still verifies
  // the administrator session before returning any per-person rows.
  function mountClaimDetails(panel, admin, round, isCurrent, translate) {
    const rowsNode = panel.querySelector("[data-claim-rows]");
    const status = panel.querySelector("[data-claim-status]");
    const previous = panel.querySelector("[data-claim-prev]");
    const next = panel.querySelector("[data-claim-next]");
    const refresh = panel.querySelector("[data-claim-refresh]");
    const pageLabel = panel.querySelector("[data-claim-page]");
    let page = 1,
      loading = false,
      more = false;
    async function load(target = page) {
      if (!isCurrent() || loading) return;
      loading = true;
      refresh.disabled = previous.disabled = next.disabled = true;
      status.textContent = translate("正在读取领取明细…");
      try {
        const rows = await admin.request(
          "/metar/events/gift-history?id=" +
            encodeURIComponent(round) +
            "&page=" +
            target,
        );
        if (!isCurrent()) return;
        if (!Array.isArray(rows)) throw Error("Invalid gift history");
        rowsNode.innerHTML = historyGrid(rows, true, null, translate);
        page = target;
        more = rows.length === 50 && target < 1000;
        pageLabel.textContent = translate("第 {page} 页", { page });
        status.textContent = "";
      } catch (err) {
        if (!isCurrent()) return;
        rowsNode.textContent = "";
        more = false;
        page = 1;
        pageLabel.textContent = "";
        status.textContent = translate(
          err?.status === 403
            ? "仅管理员可以查看领取明细。"
            : "暂时无法读取领取明细，请刷新重试。",
        );
      } finally {
        loading = false;
        if (isCurrent()) {
          refresh.disabled = false;
          previous.disabled = page <= 1;
          next.disabled = !more;
        }
      }
    }
    previous.onclick = () => {
      if (page > 1) void load(page - 1);
    };
    next.onclick = () => {
      if (more) void load(page + 1);
    };
    refresh.onclick = () => void load();
    void load();
    return load;
  }

  function adminMount(el, admin, command, history) {
    if (!el || !history) return;
    const form = el.querySelector("form"),
      msg = form.querySelector("[role=status]");
    const list = form.querySelector("[data-rule-list]"),
      add = form.querySelector("[data-add-rule]");
    if (list && add) {
      let nextRule = 2;
      const updateOrder = () => {
        const rows = Array.from(list.children);
        rows.forEach((row, i) => {
          row.querySelector("[data-rule-title]").textContent =
            "规则 " + (i + 1) + (i === 0 ? "（最高优先级）" : "");
          row.querySelector('[data-rule-action="up"]').disabled = i === 0;
          row.querySelector('[data-rule-action="down"]').disabled =
            i === rows.length - 1;
          row.querySelector('[data-rule-action="remove"]').disabled =
            rows.length === 1;
        });
        add.disabled = rows.length >= 8;
      };
      list.onchange = (e) => {
        if (e.target.matches("input[type=radio]"))
          rewardFields(e.target.closest("[data-prize]"));
      };
      list.onclick = (e) => {
        const action = e.target.closest("[data-rule-action]");
        if (!action || action.disabled) return;
        const row = action.closest("[data-rule]");
        if (action.dataset.ruleAction === "up")
          row.previousElementSibling.before(row);
        else if (action.dataset.ruleAction === "down")
          row.nextElementSibling.after(row);
        else row.remove();
        updateOrder();
      };
      add.onclick = () => {
        if (list.children.length >= 8) return;
        list.insertAdjacentHTML("beforeend", ruleHTML("r" + nextRule++ + "-"));
        updateOrder();
        list.lastElementChild.querySelector("input").focus();
      };
      updateOrder();
    }
    const button = form.querySelector(".lg-submit button"),
      status = el.querySelector("[data-round-state]"),
      report = history.querySelector("[data-report]");
    let state = "checking",
      current = null,
      until = 0,
      busy = false,
      disposed = false,
      ticket = 0,
      headVersion = 0,
      nextCheck = 0,
      activePage = 1,
      activeId = "",
      activeView = "",
      roundListPage = 1;
    const nav = (p, label, id, view = "") =>
      '<button type="button" class="btn" data-list="' +
      p +
      '" data-round="' +
      esc(id) +
      '" data-gift-view="' + esc(view) + '">' +
      label +
      "</button>";
    function paint() {
      button.disabled = busy || state !== "idle";
      button.textContent = busy
        ? "正在核对…"
        : state === "live"
          ? "本轮结束后可开启"
          : "开启本轮赠礼";
      status.textContent =
        state === "live"
          ? "进行中：" + current.Name + " · 剩余 " + clock(until)
          : state === "idle"
            ? "当前没有进行中的赠礼。"
            : state === "error"
              ? "暂时无法确认当前轮次，请刷新记录后再开启。"
              : "正在核对当前轮次…";
    }
    async function load(page = 1, id = "", show = true, view = "") {
      const head = page === 1 && !id;
      const hv = head ? ++headVersion : 0,
        seq = show ? ++ticket : 0;
      if (show) {
        activeId = id;
        activeView = view;
        activePage = page;
        if (!id) roundListPage = page;
      }
      if (head) nextCheck = Date.now() + 30000;
      try {
        const rows = await admin.request(
          "/metar/events/gift-history?page=" +
            page +
            (id ? "&id=" + encodeURIComponent(id) : "") +
            (view === "tiers" ? "&tiers=1" : view ? "&rule=" + encodeURIComponent(view) : ""),
        );
        if (disposed) return false;
        if (view === "tiers" ? !Array.isArray(rows?.tiers) : !Array.isArray(rows)) throw Error();
        if (head && hv === headVersion) {
          current = rows.find(
            (v) => v.End > (v.ServerNow ?? Date.now() / 1000),
          );
          until = current
            ? Date.now() / 1000 +
              current.End -
              (current.ServerNow ?? Date.now() / 1000)
            : 0;
          state = current ? "live" : "idle";
          paint();
        }
        if (show && seq === ticket) {
          const toolbar = id ? '<div class="lg-report-actions">' + nav(roundListPage, "返回赠礼记录", "") +
            (view === "tiers" ? nav(1, "全部领取明细", id) : nav(1, "档位配置", id, "tiers")) + '</div>' : '';
          const title = view && view !== "tiers" ? '<h4 class="lg-report-title">' +
            (view === "0" ? "未记录档位" : "档位 " + esc(view)) + ' · 每人领取明细</h4>' : '';
          report.innerHTML = toolbar + (view === "tiers" ? tierGrid(rows, id, nav) : title +
            historyGrid(rows, !!id, nav) + '<div class="lg-report-actions">' +
            (page > 1 ? nav(page - 1, "上一页", id, view) : "") +
            (rows.length === (id ? 50 : 20) && page < 1000 ? nav(page + 1, "下一页", id, view) : "") + '</div>');
        }
        return head && hv === headVersion && state === "idle";
      } catch {
        if (disposed) return false;
        if (head && hv === headVersion) {
          state = "error";
          paint();
        }
        if (show && seq === ticket)
          report.textContent = "发布记录读取失败，请点击「刷新记录」重试。";
        return false;
      }
    }
    history.onclick = (e) => {
      const b = e.target.closest("[data-list]");
      if (!b) return;
      if (b.dataset.refreshHistory !== undefined) void load(activePage, activeId, true, activeView);
      else void load(Number(b.dataset.list), b.dataset.round || "", true, b.dataset.giftView || "");
    };
    form.onsubmit = async (e) => {
      e.preventDefault();
      if (disposed || busy || state !== "idle") return;
      busy = true;
      paint();
      try {
        const gift = read(form);
        if (!(await load(1, "", false)) || disposed) return;
        if (
          !root.confirm(
            "立即开启「" + gift.name + "」？开启后本轮规则不能修改。",
          )
        )
          return;
        await command({ op: "gift_start", gift });
      } catch (err) {
        msg.textContent =
          err.status === 409
            ? "当前状态已变化，请核对发布记录。"
            : err.message || "未能确认开启结果，请刷新。";
        if (err.status === 409) await load();
      } finally {
        busy = false;
        if (!disposed) paint();
      }
    };
    void load();
    const tick = root.setInterval(() => {
      if (disposed) return;
      if (state === "live") {
        if (Date.now() >= until * 1000) {
          state = "checking";
          nextCheck = 0;
        }
        paint();
      }
      if (Date.now() >= nextCheck)
        void load(1, "", activePage === 1 && !activeId);
    }, 1000);
    return () => {
      disposed = true;
      root.clearInterval(tick);
    };
  }
  function clock(end) {
    const s = Math.max(0, Math.ceil((end * 1000 - Date.now()) / 1000));
    return [Math.floor(s / 3600), Math.floor((s % 3600) / 60), s % 60]
      .map((x) => String(x).padStart(2, "0"))
      .join(":");
  }
  root.MetarLimitedGift = {
    units,
    adminHTML,
    historyHTML,
    adminMount,
    mountClaimDetails,
    clock,
  };
})(window);
