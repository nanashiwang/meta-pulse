'use strict';
const test = require('node:test'), assert = require('node:assert/strict');
const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm');
const source = fs.readFileSync(path.join(__dirname, '../src/app.js'), 'utf8');
const mount = source.slice(source.indexOf('  async function mountProfileCheckin()'), source.indexOf('  async function bookmarksPage()'));
const receiptSource = source.slice(source.indexOf('  function checkinReceiptText('),source.indexOf('  async function mountProfileCheckin()'));
function harness() {
  const button = {}, status = {}, card = {}; let amount = 10, failRead = false, failPost = false, commit = true, pending;
  let receipt={day:'2026-11-18',experience:10,flash_amount:'0.00',flash_status:'not_issued'};
  const calls = [], timers=new Map();let timerID=0;
  const root = {dataset:{}, isConnected:true, querySelector:s=>s==='button'?button:status};
  const context = vm.createContext({document:{querySelector:s=>s==='[data-profile-checkin]'?root:card}, currentUser:{id:'one'}, navigationSequence:1, identitySequence:1, t:(s,v)=>s.replace(/\{(\w+)\}/g,(_,k)=>v?.[k]??'{'+k+'}'),
    setTimeout:fn=>{timers.set(++timerID,fn);return timerID;},clearTimeout:id=>timers.delete(id),
    growthView:async()=>({compact:async()=>'<section>updated EXP</section>'}),
    answer:{request:async(url,options)=>{calls.push({url,options}); if(options) {if(commit)amount=0;if(pending)return pending;if(failPost)throw Error('lost response');return {checkin_amount:amount,checkin_receipt:amount===0?receipt:null};} if(failRead)throw Error('offline');return {checkin_amount:amount,checkin_receipt:amount===0?receipt:null};}}});
  vm.runInContext(receiptSource+mount,context);
  return {button,status,card,root,context,calls,timers,tick:async()=>{const [id,fn]=timers.entries().next().value;timers.delete(id);await fn();},mount:()=>context.mountProfileCheckin(), receipt:v=>receipt=v, amount:v=>amount=v, failRead:v=>failRead=v, failPost:v=>failPost=v, commit:v=>commit=v,pending:v=>pending=v};
}
test('profile shows daily state and posts no identity or amount; success updates EXP',async()=>{
 const h=harness();await h.mount();assert.equal(h.button.textContent,'每日签到');assert.equal(h.button.disabled,false);
 await h.button.onclick();assert.equal(h.button.textContent,'今日已签到');assert.equal(h.button.disabled,true);assert.match(h.card.innerHTML,/updated EXP/);
 const posts=h.calls.filter(c=>c.options);assert.equal(posts.length,1);assert.equal(posts[0].url,'/metar/experience/checkin');assert.equal(posts[0].options.body,'{}');assert.equal(posts[0].options.headers['X-Metar-Request'],'1');
 await h.button.onclick();assert.equal(h.calls.filter(c=>c.options).length,1);
});
test('already checked-in members cannot submit again',async()=>{
 const h=harness();h.amount(0);await h.mount();assert.equal(h.button.textContent,'今日已签到');await h.button.onclick();assert.equal(h.calls.filter(c=>c.options).length,0);
});
test('an unknown status stays retryable and never blindly posts',async()=>{
 const h=harness();h.failRead(true);await h.mount();assert.equal(h.button.textContent,'重试签到');assert.equal(h.button.disabled,false);await h.button.onclick();assert.equal(h.calls.filter(c=>c.options).length,0);
 h.failRead(false);await h.button.onclick();assert.equal(h.button.textContent,'今日已签到');
});
test('a lost successful POST recovers from summary without issuing a second POST',async()=>{
 const h=harness();await h.mount();h.failPost(true);await h.button.onclick();assert.equal(h.button.textContent,'今日已签到');assert.match(h.card.innerHTML,/updated EXP/);assert.equal(h.calls.filter(c=>c.options).length,1);
});
test('failed uncommitted check-in remains retryable',async()=>{
 const h=harness();await h.mount();h.commit(false);h.failPost(true);await h.button.onclick();assert.equal(h.button.disabled,false);assert.match(h.status.textContent,/未确认/);
 h.commit(true);h.failPost(false);await h.button.onclick();assert.equal(h.button.textContent,'今日已签到');
});
test('in-flight clicks cannot duplicate posts; old responses cannot change a new identity or page',async()=>{
 for(const change of [h=>h.context.identitySequence++,h=>h.context.navigationSequence++,h=>h.root.isConnected=false]) {
  const h=harness();await h.mount();let resolve;h.pending(new Promise(r=>resolve=r));const work=h.button.onclick();await new Promise(r=>setImmediate(r));await h.button.onclick();assert.equal(h.calls.filter(c=>c.options).length,1);change(h);resolve({});await work;assert.equal(h.card.innerHTML,undefined);assert.equal(h.status.textContent,'');
 }
});

test('profile renders credited EXP and exact flash text from the check-in receipt',async()=>{
 const h=harness();await h.mount();await h.button.onclick();assert.equal(h.status.textContent,'10exp · 0.00⚡️');assert.match(h.status.title,/0.00 闪（未发放）/);
});
test('signed-in revisit and lost response recover the original actual receipt',async()=>{
 const h=harness();h.amount(0);h.receipt({day:'2026-11-18',experience:15,flash_amount:'0.00',flash_status:'not_issued'});await h.mount();assert.equal(h.status.textContent,'15exp · 0.00⚡️');assert.equal(h.calls.filter(c=>c.options).length,0);
 const lost=harness();await lost.mount();lost.failPost(true);await lost.button.onclick();assert.equal(lost.status.textContent,'10exp · 0.00⚡️');assert.equal(lost.calls.filter(c=>c.options).length,1);
});
test('missing or malformed receipts never invent zero or substitute future claim amounts',async()=>{
 for(const receipt of [null,{day:'2026-11-18',experience:-1,flash_amount:'0.00',flash_status:'not_issued'},{day:'2026-11-18',experience:10,flash_amount:'1e3',flash_status:'settled'},{day:'2026-11-18',experience:10,flash_amount:'2.50',flash_status:'not_issued'}]){
  const h=harness();h.amount(0);h.receipt(receipt);await h.mount();assert.match(h.status.textContent,/明细暂不可用/);assert.doesNotMatch(h.status.textContent,/0.00|2.50/);
 }
});

test('server-created cents are shown as pending then settled, without another POST or reroll',async()=>{
 const h=harness();h.receipt({day:'2026-11-18',experience:2,flash_amount:'0.30',flash_status:'pending'});await h.mount();await h.button.onclick();assert.equal(h.status.textContent,'2exp · 0.30⚡️');assert.match(h.status.title,/0.30 闪（发放中）/);assert.equal(h.timers.size,1);
 h.receipt({day:'2026-11-18',experience:2,flash_amount:'0.30',flash_status:'settled'});await h.tick();assert.equal(h.status.textContent,'2exp · 0.30⚡️');assert.match(h.status.title,/0.30 闪（已到账）/);assert.equal(h.calls.filter(c=>c.options).length,1);assert.equal(h.timers.size,0);
});
test('pending revisit queries status without a new check-in; blocked is not credited',async()=>{
 const h=harness();h.amount(0);h.receipt({day:'2026-11-18',experience:2,flash_amount:'0.01',flash_status:'pending'});await h.mount();assert.equal(h.timers.size,1);
 h.receipt({day:'2026-11-18',experience:2,flash_amount:'0.01',flash_status:'blocked'});await h.tick();assert.equal(h.status.textContent,'2exp · 0.01⚡️');assert.match(h.status.title,/0.01 闪（待处理）/);assert.equal(h.calls.filter(c=>c.options).length,0);assert.equal(h.timers.size,0);
});

 test('preparing receipts use a dash instead of inventing a zero flash amount',async()=>{
  const h=harness();h.amount(0);h.receipt({day:'2026-11-18',experience:3,flash_status:'preparing'});await h.mount();
  assert.equal(h.status.textContent,'3exp · —⚡️');assert.match(h.status.title,/服务器生成/);
 });
