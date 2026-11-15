import test from "node:test";
import assert from "node:assert/strict";
import {
  validatePoll,
  resultPercent,
  installTopicPolls,
} from "./topic-polls.js";
const config = {
  title: "Most useful tool?",
  options: ["A", "B", "C"],
  max_choices: 2,
  days: 7,
};
test("poll validation trims text, limits choices and preserves safe plain text", () => {
  assert.deepEqual(
    validatePoll({
      ...config,
      title: " Title ",
      options: ["<img src=x>", "B"],
    }),
    { title: "Title", options: ["<img src=x>", "B"], max_choices: 2, days: 7 },
  );
  for (const patch of [
    { title: "" },
    { title: "x".repeat(161) },
    { options: ["a"] },
    { options: ["a", " A "] },
    { options: ["a", "b\n"], title: "line\nbreak" },
    { max_choices: 4 },
    { days: 2 },
  ])
    assert.throws(() => validatePoll({ ...config, ...patch }));
});
test("multiple-choice percentages use people, not the sum of selections", () => {
  assert.equal(resultPercent(2, 3), 200 / 3);
  assert.equal(resultPercent(0, 0), 0);
  assert.equal(resultPercent(20, 3), 100);
});
function setup({ fail = false, blocked = false } = {}) {
  const values = new Map(),
    requests = [];
  let account = "1";
  const host = {
    document: { documentElement: { lang: "zh-CN" }, addEventListener() {} },
    MutationObserver: class {
      observe() {}
    },
    requestAnimationFrame: () => 1,
    addEventListener() {},
    setTimeout,
    clearTimeout,
    localStorage: {
      getItem: (k) => values.get(k),
      setItem: (k, v) => {
        if (blocked) throw Error("storage");
        values.set(k, v);
      },
      removeItem: (k) => values.delete(k),
    },
    fetch: async (url, options) => {
      requests.push({ url, options });
      if (fail) throw Error("timeout");
      return { ok: true, json: async () => ({ data: { ok: true } }) };
    },
  };
  const hooks = installTopicPolls(
    { getUser: () => ({ id: account }), getToken: () => "session" },
    host,
  );
  return { hooks, values, requests, switchUser: () => (account = "2") };
}
test("post creation stores recoverable same-key poll before sending and clears it on success", async () => {
  const h = setup();
  await h.hooks.attach("100", { user: "1", key: "request-0000000001", config });
  assert.equal(h.requests.length, 1);
  assert.equal(
    h.requests[0].options.headers["Idempotency-Key"],
    "request-0000000001",
  );
  assert.equal(h.requests[0].options.headers.Authorization, "Bearer session");
  assert.equal(h.values.size, 0);
});
test("uncertain creation keeps its poll and never republishes the post", async () => {
  const h = setup({ fail: true });
  await h.hooks.attach("100", { user: "1", key: "request-0000000001", config });
  const pending = JSON.parse(h.values.get("metar-poll-v1:1:pending-create"));
  assert.equal(pending.question_id, "100");
  assert.equal(pending.key, "request-0000000001");
  assert.equal(h.requests[0].url, "/answer/api/v1/metar/polls/command");
});
test("account change and unavailable persistence do not send a poll write", async () => {
  const h = setup();
  h.switchUser();
  await h.hooks.attach("100", { user: "1", key: "request-0000000001", config });
  assert.equal(h.requests.length, 0);
  const blocked = setup({ blocked: true });
  await blocked.hooks.attach("100", {
    user: "1",
    key: "request-0000000001",
    config,
  });
  assert.equal(blocked.requests.length, 0);
});
