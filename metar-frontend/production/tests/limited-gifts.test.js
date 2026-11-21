const test = require("node:test"),
  assert = require("node:assert/strict"),
  vm = require("node:vm"),
  fs = require("node:fs");
const w = {};
vm.runInNewContext(
  fs.readFileSync(require.resolve("../src/limited-gifts.js"), "utf8"),
  { window: w },
);
const { units, adminHTML } = w.MetarLimitedGift;
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
    "flashmode",
    "expmode",
    "ticketsmode",
    "unit",
    "likes",
    "comments",
  ])
    assert.ok(html.includes('name="' + n + '"'));
  assert.match(html, /随机范围/);
});
