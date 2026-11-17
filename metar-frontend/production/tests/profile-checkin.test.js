'use strict';
const test = require('node:test'), assert = require('node:assert/strict');
const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm');
const source = fs.readFileSync(path.join(__dirname, '../src/app.js'), 'utf8');
const mount = source.slice(source.indexOf('  async function mountProfileCheckin()'), source.indexOf('  async function bookmarksPage()'));
function harness() {
  const button = {}, status = {}, card = {}; let amount = 10, failRead = false, failPost = false, commit = true, pending;
  const calls = [];
  const root = {dataset:{}, isConnected:true, querySelector:s=>s==='button'?button:status};
  const context = vm.createContext({document:{querySelector:s=>s==='[data-profile-checkin]'?root:card}, currentUser:{id:'one'}, navigationSequence:1, identitySequence:1, t:s=>s,
    growthView:async()=>({compact:async()=>'<section>updated EXP</section>'}),
    answer:{request:async(url,options)=>{calls.push({url,options}); if(options) {if(commit)amount=0;if(pending)return pending;if(failPost)throw Error('lost response');return {};} if(failRead)throw Error('offline');return {checkin_amount:amount};}}});
  vm.runInContext(mount,context);
  return {button,status,card,root,context,calls,mount:()=>context.mountProfileCheckin(), amount:v=>amount=v, failRead:v=>failRead=v, failPost:v=>failPost=v, commit:v=>commit=v,pending:v=>pending=v};
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
