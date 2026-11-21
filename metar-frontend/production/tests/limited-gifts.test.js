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

test('gift history remains outside the collapsed editor and active or unreadable state blocks submit',async()=>{
 const button={disabled:true},status={},report={},form={querySelector:s=>s==='button'?button:{},querySelectorAll:()=>[]};const el={querySelector:s=>s==='form'?form:s==='[data-round-state]'?status:report};const html=adminHTML();assert.ok(html.indexOf('</details>')<html.indexOf('data-report'));let mode='live',posts=0;w.setInterval=()=>1;w.clearInterval=()=>{};const admin={request:async()=>{if(mode==='error')throw Error();return mode==='idle'?[]:[{End:101,ServerNow:100,ID:1,Name:'test round',People:0,Flash:0,Exp:0,Tickets:0}]}};
 const stop=w.MetarLimitedGift.adminMount(el,admin,()=>posts++);await new Promise(setImmediate);assert.ok(button.disabled);assert.match(status.textContent,/test round/);assert.match(report.innerHTML,/test round/);await form.onsubmit({preventDefault(){}});assert.equal(posts,0);const refresh=()=>el.onclick({target:{closest:()=>({dataset:{list:'1'}})}});mode='error';refresh();await new Promise(setImmediate);assert.ok(button.disabled);mode='idle';refresh();await new Promise(setImmediate);assert.equal(button.disabled,false);stop();});
