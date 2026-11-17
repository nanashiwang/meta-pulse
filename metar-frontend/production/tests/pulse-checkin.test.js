'use strict';
const test = require('node:test'), assert = require('node:assert/strict');
const fs = require('node:fs'), vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname, '../src/app.js'), 'utf8');
const mount = source.slice(source.indexOf('  async function mountCommunityTasks()'), source.indexOf('  function pulseFeedTable('));
const progress = (extra={}) => ({likes:0, comments:0, received:0, earned:0, delivered:0, checked_in:false, checkin_delivered:extra.checked_in === true && extra.delivered > 0, ...extra});
const flush = () => new Promise(resolve => setImmediate(resolve));
function harness({bound=true}={}) {
  const status={}, checkin={}, refresh={}, timers=new Map(); let id=0, reads=0, posts=0, updates=0;
  let task=progress(), summary={checkin_amount:10}, fail=false, update=true, postFail=false, pending;
  const root={dataset:{}, isConnected:true, querySelector:s=>s==='[data-task-progress]'?status:s==='[data-task-checkin]'?checkin:refresh};
  const context=vm.createContext({document:{querySelector:()=>root}, currentUser:{id:'one'}, navigationSequence:1, identitySequence:1, pulseCoreState:{unbound:!bound}, t:s=>s,
    pulse:{request:async()=>{reads++; if(pending)return pending; if(fail)throw Error('offline'); return task;}},
    answer:{request:async(path,opts)=>{if(opts){posts++;if(postFail)throw Error('lost');return {};} return summary;}},
    refreshPulse:async()=>{updates++;return update;},
    setTimeout:fn=>{timers.set(++id,fn);return id;}, clearTimeout:id=>timers.delete(id)});
  vm.runInContext(mount,context);
  return {root,status,checkin,refresh,context,timers, mount:()=>context.mountCommunityTasks(), tick:async()=>{const [id,fn]=timers.entries().next().value;timers.delete(id);fn();await flush();},
    setTask:v=>task=v, setFail:v=>fail=v, setUpdate:v=>update=v, setPostFail:v=>postFail=v, setPending:v=>pending=v, setSummary:v=>summary=v,
    counts:()=>({reads,posts,updates})};
}
test('delayed collection and delivery refresh authoritative tickets without duplicate check-ins',async()=>{
 const h=harness();await h.mount();await h.checkin.onclick();
 assert.equal(h.checkin.disabled,true);assert.equal(h.counts().updates,0);
 await h.checkin.onclick();assert.equal(h.counts().posts,1);
 h.setTask(progress({checked_in:true,earned:1}));await h.tick();assert.equal(h.counts().updates,0);
 h.setTask(progress({checked_in:true,earned:1,delivered:1}));await h.tick();
 assert.equal(h.counts().updates,1);assert.equal(h.timers.size,0);assert.equal(h.checkin.textContent,'今日已签到');
});
test('pending delivery on entry and failed eligibility refresh recover automatically',async()=>{
 const h=harness();h.setTask(progress({checked_in:true,earned:1}));await h.mount();
 h.setTask(progress({checked_in:true,earned:1,delivered:1}));h.setUpdate(false);await h.tick();assert.equal(h.timers.size,1);
 h.setUpdate(true);await h.tick();assert.equal(h.counts().updates,2);assert.equal(h.timers.size,0);
});
test('progress errors and malformed data cannot grant tickets; polling is bounded and manual refresh resumes',async()=>{
 const h=harness();await h.mount();await h.checkin.onclick();h.setFail(true);
 for(let i=0;i<12;i++)await h.tick();
 assert.equal(h.timers.size,0);assert.equal(h.counts().updates,0);assert.match(h.status.textContent,/无需刷新浏览器/);
 h.setFail(false);h.setTask(progress({checked_in:true,earned:1,delivered:2}));await h.refresh.onclick();assert.equal(h.counts().updates,0);
 h.setTask(progress({checked_in:true,earned:1,delivered:1}));await h.tick();assert.equal(h.counts().updates,1);
});
test('lost check-in response is recovered by reads only',async()=>{
 const h=harness();await h.mount();h.setPostFail(true);await h.checkin.onclick();
 h.setTask(progress({checked_in:true,earned:1,delivered:1}));await h.tick();
 assert.equal(h.counts().posts,1);assert.equal(h.counts().updates,1);assert.equal(h.checkin.disabled,true);
});
test('navigation, identity change and detached UI discard in-flight task responses',async()=>{
 for(const change of [h=>h.context.navigationSequence++,h=>h.context.identitySequence++,h=>h.context.currentUser={id:'two'},h=>h.root.isConnected=false]){
  const h=harness();await h.mount();await h.checkin.onclick();let resolve;
  h.setPending(new Promise(r=>resolve=r));const tick=h.tick();change(h);
  resolve(progress({checked_in:true,earned:1,delivered:1}));await tick;
  assert.equal(h.counts().updates,0);assert.equal(h.timers.size,0);
 }
});
test('unbound check-in keeps EXP available without any Pulse calls',async()=>{
 const h=harness({bound:false});await h.mount();assert.equal(h.checkin.disabled,false);
 h.setSummary({checkin_amount:0});await h.checkin.onclick();assert.equal(h.checkin.disabled,true);
 assert.deepEqual(h.counts(),{reads:0,posts:1,updates:0});assert.equal(h.timers.size,0);
});
test('growth summary cards keep experience information without check-in buttons',async()=>{
 const {View}=require('../src/growth.js');
 const card=await new View({request:async()=>({appearance:'default',level:{number:1,minimum:0,name:'新手'},experience:20,next_level:null,checkin_amount:10,today:0,daily_cap:100,streak:1})}).compact();
 assert.match(card,/20 EXP/);assert.doesNotMatch(card,/data-growth="checkin"/);assert.match(card,/签到抽奖/);
});

test('settled entry does not trigger a fallback remount loop; manual progress refresh updates benefits',async()=>{
 const h=harness();h.setTask(progress({checked_in:true,earned:1,delivered:1}));await h.mount();
 assert.equal(h.counts().updates,0);assert.equal(h.timers.size,0);
 await h.refresh.onclick();assert.equal(h.counts().updates,1);
});

test('partial delivery updates usable tickets while other community tickets are still pending',async()=>{
 const h=harness();await h.mount();await h.checkin.onclick();
 h.setTask(progress({checked_in:true,earned:2,delivered:1}));await h.tick();
 assert.equal(h.counts().updates,1);assert.equal(h.timers.size,1);
 h.setTask(progress({checked_in:true,earned:2,delivered:2}));await h.tick();
 assert.equal(h.counts().updates,2);assert.equal(h.timers.size,0);
});

test('other delivered task tickets do not confirm delivery of the daily check-in ticket',async()=>{
 const h=harness();h.setTask(progress({earned:1,delivered:1}));await h.mount();await h.checkin.onclick();
 h.setTask(progress({checked_in:true,earned:1,delivered:1,checkin_delivered:false}));await h.tick();
 assert.equal(h.counts().updates,0);assert.equal(h.timers.size,1);
 h.setTask(progress({checked_in:true,earned:2,delivered:2,checkin_delivered:true}));await h.tick();
 assert.equal(h.counts().updates,1);assert.equal(h.timers.size,0);
});
test('old servers lacking a check-in receipt get periodic eligibility refresh without false confirmation',async()=>{
 const h=harness();await h.mount();await h.checkin.onclick();
 h.setTask(progress({checked_in:true,earned:1,delivered:1,checkin_delivered:undefined}));
 for(let i=0;i<12;i++)await h.tick();
 assert.ok(h.counts().updates>1);assert.equal(h.counts().posts,1);assert.equal(h.timers.size,0);
});

test('check-in before linking is displayed as completed and ineligible without endless polling or another POST',async()=>{
 for(const reason of ['before_binding','before_start']) {
  const h=harness();h.setTask(progress({checked_in:true,checkin_ineligible_reason:reason}));await h.mount();
  assert.equal(h.checkin.disabled,true);assert.equal(h.checkin.textContent,'今日已签到');
  assert.match(h.status.textContent,/今天不补发签到券/);assert.equal(h.timers.size,0);
  await h.checkin.onclick();assert.equal(h.counts().posts,0);
  await h.refresh.onclick();assert.match(h.status.textContent,/今天不补发签到券/);assert.equal(h.timers.size,0);
 }
});
test('an eligible EXP check-in not yet collected starts polling on page entry',async()=>{
 const h=harness();h.setTask(progress({checked_in:true}));await h.mount();
 assert.equal(h.checkin.disabled,true);assert.equal(h.timers.size,1);
 h.setTask(progress({checked_in:true,earned:1,delivered:1}));await h.tick();
 assert.equal(h.counts().updates,1);assert.equal(h.timers.size,0);
});

test('non-ticket weekdays still check in for EXP without waiting for a ticket',async()=>{
 const h=harness();h.setTask(progress({checkin_ticket_target:0}));await h.mount();assert.equal(h.checkin.textContent,'每日签到 · 领经验');
 h.setTask(progress({checked_in:true,checkin_ticket_target:0,checkin_ineligible_reason:'not_scheduled'}));await h.checkin.onclick();assert.equal(h.checkin.textContent,'今日已签到');assert.equal(h.timers.size,0);assert.match(h.status.textContent,/今天仅领取经验/);
});
test('Monday and Friday three-ticket check-in waits for all three while exposing partial delivery',async()=>{
 const h=harness();h.setTask(progress({checkin_ticket_target:3}));await h.mount();assert.equal(h.checkin.textContent,'每日签到 · +3 券');
 h.setTask(progress({checked_in:true,checkin_ticket_target:3,earned:3,delivered:1,checkin_delivered:false}));await h.checkin.onclick();assert.equal(h.timers.size,1);assert.equal(h.counts().updates,1);
 h.setTask(progress({checked_in:true,checkin_ticket_target:3,earned:3,delivered:3,checkin_delivered:true}));await h.tick();assert.equal(h.timers.size,0);assert.equal(h.counts().updates,2);
});


test('ticket acquisition panel explains current check-in rules without old engagement tasks',()=>{
 const markup=source.slice(source.indexOf('  function communityTasksMarkup('),source.indexOf('  async function mountCommunityTasks()'));
 const context=vm.createContext({t:s=>s,link:()=>''});vm.runInContext(markup,context);
 for(const bound of [true,false]){
  const html=context.communityTasksMarkup(bound);
  assert.match(html,/周一、周五签到各送 3 张抽奖券，每周共 6 张/);
  assert.match(html,/其他日期不赠送签到券/);assert.match(html,/2.5 闪/);assert.match(html,/北京时间/);
  assert.doesNotMatch(html,/收到.*评论|给别人点赞|互动达标|有效互动|每天最多|每日签到 · \+1/);
 }
});
test('completed check-in displays no engagement counters or combined 3/6 ticket cap',async()=>{
 const h=harness();h.setTask(progress({received:140,checked_in:true,earned:3,delivered:3,checkin_delivered:true}));await h.mount();
 assert.equal(h.status.textContent,'今日已签到，签到赠券已到账。');
 assert.doesNotMatch(h.status.textContent,/140|点赞|评论|3\/6|3\/8/);
});
test('background engagement delivery cannot turn a completed check-in into a check-in timeout',async()=>{
 const h=harness();h.setTask(progress({checked_in:true,checkin_ticket_target:3,earned:5,delivered:3,checkin_delivered:true}));await h.mount();
 for(let i=0;i<12;i++)await h.tick();
 assert.equal(h.status.textContent,'今日已签到，签到赠券已到账。');assert.equal(h.timers.size,0);
});
test('a legacy one-ticket target never advertises the retired daily +1 check-in rule',async()=>{
 const h=harness();h.setTask(progress({checkin_ticket_target:1}));await h.mount();
 assert.equal(h.checkin.textContent,'每日签到');assert.match(h.status.textContent,/周一、周五签到各赠 3 张券/);
});
