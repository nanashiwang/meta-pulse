const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require.resolve('../src/app.js'), 'utf8');
const start = source.indexOf('  async function mountCommunityTasks()');
const end = source.indexOf('\n  function pulseFeedTable', start);
function fixture({fail=false, leave=false, pending=false}={}) {
 const status={},checkin={},refresh={}, root={isConnected:true,dataset:{},querySelector:s=>s.includes('progress')?status:s.includes('checkin')?checkin:refresh};
 let requests=0,refreshes=0,reads=0,waits=0;
 const ctx={document:{querySelector:()=>root},currentUser:{id:'u'},navigationSequence:1,identitySequence:1,t:x=>x,
  pulse:{request:async()=>{reads++;return {likes:0,comments:0,received:0,earned:requests?1:0,delivered:requests&&!pending&&reads>=3?1:0,checked_in:requests>0}}},
  answer:{request:async()=>{requests++;if(fail)throw Error('offline');}},refreshPulse:async()=>{refreshes++;},
  setTimeout:fn=>{waits++;if(leave)root.isConnected=false;fn();}};
 const mount=vm.runInNewContext('('+source.slice(start,end)+')',ctx);
 return {mount,checkin,status,root,stats:()=>({requests,refreshes,reads,waits})};
}
test('successful check-in refreshes immediately and again when asynchronous ticket delivery completes',async()=>{
 const f=fixture();await f.mount();await f.checkin.onclick();
 assert.equal(f.stats().requests,1);assert.equal(f.stats().refreshes,2);assert.equal(f.stats().waits,1);
 assert.equal(f.checkin.disabled,true);assert.equal(f.checkin.textContent,'今日已签到');
 await f.checkin.onclick();assert.equal(f.stats().requests,1);
});
test('failed check-in does not refresh benefits and remains retryable',async()=>{
 const f=fixture({fail:true});await f.mount();await f.checkin.onclick();
 assert.equal(f.stats().refreshes,0);assert.equal(f.stats().waits,0);assert.equal(f.checkin.disabled,false);
});
test('leaving the page stops ticket polling and prevents stale benefit refresh',async()=>{
 const f=fixture({leave:true});await f.mount();await f.checkin.onclick();
 assert.equal(f.stats().refreshes,1);assert.equal(f.stats().reads,2);
});
test('delayed delivery has a bounded polling window without resubmitting check-in',async()=>{
 const f=fixture({pending:true});await f.mount();await f.checkin.onclick();
 assert.equal(f.stats().waits,20);assert.equal(f.stats().requests,1);assert.equal(f.checkin.disabled,true);
 assert.match(f.status.textContent,/无需重复签到/);
});
