const test = require("node:test"),
  assert = require("node:assert/strict"),
  vm = require("node:vm"),
  fs = require("node:fs");
const w = {};
vm.runInNewContext(
  fs.readFileSync(require.resolve("../src/limited-gifts.js"), "utf8"),
  {
    window: w,
    FormData: class {
      constructor(form) {
        return form.values;
      }
    },
  },
);
const { units, adminHTML, historyHTML } = w.MetarLimitedGift;
test("gift thresholds retain the API integer through wan/yi and flash cents", () => {
  assert.equal(units("1.5", 4), 15000n);
  assert.equal(units("0.00000001", 8), 1n);
  assert.equal(units("92233720368.54775807", 8), 9223372036854775807n);
  assert.equal(units("0.01", 2), 1n);
  for (const v of ["1e5", "-1", "0.00001", ""])
    assert.throws(() => units(v, 4));
  assert.throws(() => units("92233720368.54775808", 8));
});
test("admin can choose each reward mode and numeric limits", () => {
  const html = adminHTML();
  for (const n of [
    "r1-flashmode",
    "r1-expmode",
    "r1-ticketsmode",
    "r1-unit",
    "r1-likes",
    "r1-comments",
    "r1-max_claims",
  ])
    assert.ok(html.includes('name="' + n + '"'));
  assert.match(html, /type="radio" name="r1-flashmode" value="none" checked/);
  assert.match(html, /type="radio" name="r1-flashmode" value="random"/);
  assert.match(html, /class="lg-quantities" hidden/);
  assert.match(html, /min="1" max="1000000" step="1" name="r1-max_claims" placeholder="留空不限"/);
});

test("gift history remains outside the collapsed editor and active or unreadable state blocks submit", async () => {
  const button = { disabled: true },
    status = {},
    report = {},
    form = {
      querySelector: (s) =>
        s === ".lg-submit button" ? button : s === "[role=status]" ? {} : null,
      querySelectorAll: () => [],
    };
  const el = {
    querySelector: (s) =>
      s === "form" ? form : s === "[data-round-state]" ? status : report,
  };
  const html = adminHTML();
  assert.doesNotMatch(html, /data-report/);
  assert.match(historyHTML(), /data-report/);
  const history = { querySelector: () => report };
  let mode = "live",
    posts = 0;
  w.setInterval = () => 1;
  w.clearInterval = () => {};
  const admin = {
    request: async () => {
      if (mode === "error") throw Error();
      return mode === "idle"
        ? []
        : [
            {
              End: 101,
              ServerNow: 100,
              ID: 1,
              Name: "test round",
              People: 0,
              Flash: 0,
              Exp: 0,
              Tickets: 0,
            },
          ];
    },
  };
  const stop = w.MetarLimitedGift.adminMount(el, admin, () => posts++, history);
  await new Promise(setImmediate);
  assert.ok(button.disabled);
  assert.match(status.textContent, /test round/);
  assert.match(report.innerHTML, /test round/);
  await form.onsubmit({ preventDefault() {} });
  assert.equal(posts, 0);
  const refresh = () =>
    history.onclick({
      target: { closest: () => ({ dataset: { list: "1" } }) },
    });
  mode = "error";
  refresh();
  await new Promise(setImmediate);
  assert.ok(button.disabled);
  mode = "idle";
  refresh();
  await new Promise(setImmediate);
  assert.equal(button.disabled, false);
  stop();
});

test("claim detail pages render actual rewards and clear stale rows after denied access", async () => {
  const elements = new Map();
  const panel = {
    querySelector(key) {
      if (!elements.has(key)) elements.set(key, {});
      return elements.get(key);
    },
  };
  const requests = [];
  let denied = false,
    current = true;
  const row = {
    Name: "<img src=x>",
    User: "95",
    At: 1700000000,
    Flash: 523,
    Exp: 8,
    Tickets: 2,
    ExpPaid: true,
    rule_number: 2,
  };
  const admin = {
    async request(path) {
      requests.push(path);
      if (denied) throw { status: 403 };
      return path.endsWith("page=1") ? Array(50).fill(row) : [row];
    },
  };
  const translate = (s, values) => s.replace("{page}", values?.page);
  const reload = w.MetarLimitedGift.mountClaimDetails(
    panel,
    admin,
    "7",
    () => current,
    translate,
  );
  const flush = () => new Promise(setImmediate);
  const el = (key) => elements.get("[data-claim-" + key + "]");
  await flush();
  assert.equal(requests[0], "/metar/events/gift-history?id=7&page=1");
  assert.match(el("rows").innerHTML, /&lt;img src=x&gt;.*#2.*5\.23.*已到账/);
  assert.doesNotMatch(el("rows").innerHTML, /<img/);
  assert.equal(el("next").disabled, false);
  el("next").onclick();
  await flush();
  assert.equal(el("page").textContent, "第 2 页");
  assert.equal(el("next").disabled, true);
  assert.equal(el("prev").disabled, false);
  denied = true;
  await reload();
  assert.equal(el("rows").textContent, "");
  assert.match(el("status").textContent, /仅管理员/);
  current = false;
  await reload();
  assert.equal(requests.length, 3);
});


test("each gift rule posts its own limit and omits empty limits", async () => {
  const button = {},
    msg = {},
    sent = [];
  const fields = { name: "Test", minutes: "5" };
  for (const prefix of ["r2-", "r1-"])
    Object.assign(fields, {
      [prefix + "flashmode"]: "none",
      [prefix + "ticketsmode"]: "none",
      [prefix + "expmode"]: "fixed",
      [prefix + "explo"]: "5",
    });
  const form = {
    values: new Map(Object.entries(fields)),
    querySelector: (s) =>
      s === ".lg-submit button" ? button : s === "[role=status]" ? msg : null,
    querySelectorAll: () => ["r2-", "r1-"].map((rule) => ({ dataset: { rule } })),
  };
  const el = { querySelector: (s) => s === "form" ? form : {} };
  w.confirm = () => true;
  const stop = w.MetarLimitedGift.adminMount(
    el,
    { request: async () => [] },
    (data) => sent.push(data),
    { querySelector: () => ({}) },
  );
  await new Promise(setImmediate);
  const submit = () => form.onsubmit({ preventDefault() {} });
  for (const value of ["0", "1.5", "1000001"]) {
    form.values.set("r2-max_claims", value);
    await submit();
    assert.match(msg.textContent, /规则 1：领取人数上限/);
  }
  assert.equal(sent.length, 0);
  form.values.set("r2-max_claims", "1");
  await submit();
  assert.equal(sent.length, 1);
  const [first, second] = sent[0].gift.groups;
  assert.equal(first.max_claims, 1);
  assert.ok(!Object.hasOwn(second, "max_claims"));
  stop();
});


test("admin history shows frozen tier settings and keeps filter on page and refresh", async () => {
  const status = {}, report = {}, button = {};
  const form = { querySelector: field => field === '.lg-submit button' ? button : null };
  const editor = { querySelector: field => field === 'form' ? form : status };
  const history = { querySelector: () => report };
  const paths = [];
  const person = { Name: '档位领取者', User: '8', At: 1700000000, Flash: 199, Exp: 0, Tickets: 0, rule_number: 1, no_ticket_award: true };
  const admin = { request: async path => {
    paths.push(path);
    const q = new URL(path, 'https://forum.test').searchParams;
    if (q.has('tiers')) return {
      name: '<旧轮次>', social_inclusive: true,
      tiers: [
        { number: 1, people: 51, flash: 199, exp: 0, tickets: 0, rule: { likes: 3, comments: 0, usage_raw: '9007199254740993', max_claims: 55, flash: { min: 100, max: 199 }, tickets: { min: 0, max: 2 } } },
        { number: 2, people: 0, flash: 0, exp: 0, tickets: 0, rule: { exp: { min: 1, max: 1 } } },
      ], unassigned: { people: 1, flash: 200, exp: 0, tickets: 0 },
    };
    if (q.get('rule') === '1') return q.get('page') === '1' ? Array(50).fill(person) : [person];
    if (q.get('rule') === '0') return [{...person,Name: '旧领取者', rule_number: 0}];
    return [{ID:'7',Name:'赠礼',Actor:'管理员',End:0,People:52,Flash:399,Exp:0,Tickets:0}];
  }};
  const stop = w.MetarLimitedGift.adminMount(editor, admin, () => {}, history);
  const flush = () => new Promise(setImmediate);
  const click = async (dataset) => {
    history.onclick({ target: { closest: () => ({dataset}) } });
    await flush();
  };
  await flush();
  assert.match(report.innerHTML, /档位配置.*每人明细/);
  await click({ list: '1', round: '7', giftView: 'tiers' });
  assert.match(report.innerHTML, /&lt;旧轮次&gt; · 档位配置/);
  for (const text of ['API 消耗 &gt; 90071992.54740993 亿', '点赞 ≥ 3', '评论 ≥ 0', '随机 1.00–1.99', '随机 0–2', '固定 1', '51 / 55', '未记录档位'])
    assert.ok(report.innerHTML.includes(text), text);
  await click({ list:'1', round:'7', giftView:'1' });
  assert.match(report.innerHTML, /档位 1 · 每人领取明细/);
  assert.match(report.innerHTML, /data-list="2" data-round="7" data-gift-view="1">下一页/);
  await click({ list:'2', round:'7', giftView:'1' });
  assert.match(paths.at(-1), /page=2&id=7&rule=1$/);
  assert.match(report.innerHTML, /档位领取者.*待到账.*本次未获得抽奖券/);
  await click({ list:'1', refreshHistory:'' });
  assert.match(paths.at(-1), /page=2&id=7&rule=1$/);
  await click({ list:'1', round:'7', giftView:'0' });
  assert.match(paths.at(-1), /rule=0$/);
  assert.match(report.innerHTML, /未记录档位 · 每人领取明细.*旧领取者/);
  stop();
});
