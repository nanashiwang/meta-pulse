const { test } = require("node:test");
const assert = require("node:assert/strict");
const growth = require("../src/growth.js");
test("progress uses the fivefold interval and clamps corrections", () => {
  const s = {
    experience: 300,
    level: { minimum: 150 },
    next_level: { minimum: 600 },
  };
  assert.ok(Math.abs(growth.progress(s) - 100 / 3) < 1e-10);
  assert.equal(growth.progress({ ...s, experience: 0 }), 0);
  assert.equal(growth.progress({ ...s, experience: 1000 }), 100);
  assert.equal(growth.progress({ ...s, next_level: null }), 100);
});
test("ledger rows escape audit text and do not invent answer URLs", () => {
  const row = growth.entry({
    delta: -20,
    kind: "accepted",
    reason: "<img onerror=evil()>",
    object_type: "answer",
    object_id: "10",
    created_at: 0,
  });
  assert.ok(!row.includes("<img"));
  assert.ok(row.includes("-20"));
  assert.ok(!row.includes("/answers/"));
  assert.ok(row.includes("&lt;img"));
});
test("unavailable experience does not reject the account page", async () => {
  const view = new growth.View({
    request: async () => {
      throw Error("offline");
    },
  });
  assert.match(await view.compact(), /暂不可用/);
});
test("check-in does not send any identity or reward amount", async () => {
  let call;
  const view = new growth.View({
    request: async (...args) => {
      call = args;
      return {};
    },
  });
  await view.request("checkin", {});
  assert.equal(call[0], "/metar/experience/checkin");
  assert.equal(call[1].body, "{}");
  assert.equal(call[1].headers["X-Metar-Request"], "1");
});
