'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const {batch, mount} = require('../src/discussion-stream.js');
const rows = (start, count) => Array.from({length:count}, (_,i) => ({id:String(start+i),title:'Question'}));
const tick = () => new Promise(setImmediate);
function setup(request, {observe=true, done=false, existing=rows(1,50),restorePage=6,onProgress=()=>{}}={}) {
  let trigger, click, valid=true, watches=0, disconnected=false;
  const message={}, button={addEventListener:(_,fn)=>click=fn,removeEventListener:()=>click=null};
  const tail={querySelector:s=>s==='button'?button:message};
  const list={querySelectorAll:()=>existing.map(item=>({dataset:{questionId:item.id}})),
    insertAdjacentHTML:(_,html)=>{ for(const id of html.split(',').filter(Boolean)) existing.push({id}); }};
  const node={isConnected:true,dataset:{nextPage:'6',done:String(done),order:'score',tag:'develop'},
    setAttribute(){},querySelector:s=>s==='.discussion-list'?list:tail};
  const host={};
  if(observe)host.IntersectionObserver=class {
    constructor(fn,options){ trigger=()=>fn([{isIntersecting:true}]);assert.equal(options.rootMargin,'0px 0px 400px 0px'); }
    observe(){watches++} unobserve(){} disconnect(){disconnected=true}
  };
  const denied=[];
  const options={host,request,restorePage,onProgress,row:item=>item.id+',',t:x=>x,current:()=>valid,onDenied:error=>denied.push(error)};
  const controller=mount(node,options);
  return {controller,node,options,existing,message,button,denied,trigger:()=>trigger(),click:()=>click(),
    invalidate:()=>valid=false,disconnected:()=>disconnected,watches:()=>watches};
}
test('50 initial records then page 6 and 7 append ten each, maintaining filter and order',async()=>{
  const calls=[];const h=setup(async q=>{calls.push(q);return {list:rows((q.page-1)*10+1,10),count:100}});
  h.trigger();await tick();h.trigger();await tick();
  assert.equal(h.existing.length,70);assert.equal(h.existing[50].id,'51');assert.equal(h.existing[69].id,'70');
  assert.deepEqual(calls,[{page:6,pageSize:10,order:'score',tag:'develop'},{page:7,pageSize:10,order:'score',tag:'develop'}]);
});
test('parallel intersections do not duplicate a request or append',async()=>{
  let resolve,calls=0;const h=setup(()=>{calls++;return new Promise(r=>resolve=r)});
  h.trigger();h.trigger();h.click();assert.equal(calls,1);
  resolve({list:rows(51,10),count:80});await tick();assert.equal(h.existing.length,60);
});
test('tail, empty and malformed results stop or reject appropriately',()=>{
  assert.equal(batch({list:rows(1,50),count:100},0,50).done,false);
  assert.equal(batch({list:rows(1,50),count:50},0,50).done,true);
  assert.equal(batch({list:rows(51,4),count:54},50,10).done,true);
  assert.equal(batch({list:[],count:0},0,50).done,true);
  for(const value of [{},{list:null,count:10},{list:[{}],count:10},{list:[],count:-1}])assert.throws(()=>batch(value,0,50));
});
test('duplicates are removed using exact string IDs without breaking page advancement',async()=>{
  const h=setup(async()=>({list:[{id:'50'},...rows(51,9)],count:100}));
  await h.controller.load();assert.equal(h.existing.length,59);assert.equal(h.node.dataset.nextPage,'7');
  assert.equal(batch({list:[{id:'9007199254740992'},{id:'9007199254740993'}],count:2},0,50).list.length,2);
});
test('failure retains rows and retries the same page only on explicit click',async()=>{
  let fail=true;const calls=[];const h=setup(async q=>{calls.push(q.page);if(fail)throw Error('offline');return {list:rows(51,3),count:53}});
  h.trigger();await tick();assert.equal(h.existing.length,50);assert.equal(h.button.hidden,false);assert.match(h.message.textContent,/失败/);
  h.trigger();await tick();assert.deepEqual(calls,[6]);fail=false;h.click();await tick();
  assert.deepEqual(calls,[6,6]);assert.equal(h.existing.length,53);assert.equal(h.node.dataset.done,'true');
  h.trigger();await tick();assert.deepEqual(calls,[6,6]);assert.equal(h.button.hidden,true);
});
test('late responses after disposal, route/session/language change or detach cannot append',async()=>{
  for(const invalidate of [h=>h.controller.dispose(),h=>h.invalidate(),h=>h.node.isConnected=false]){
    let resolve;const h=setup(()=>new Promise(r=>resolve=r));const work=h.controller.load();invalidate(h);
    resolve({list:rows(51,10),count:80});await work;assert.equal(h.existing.length,50);assert.equal(h.node.dataset.nextPage,'6');
  }
});
test('authorization denial stops observation and delegates clearing the retained view',async()=>{
  for(const status of [401,403]){const error={status};const h=setup(async()=>{throw error});await h.controller.load();assert.deepEqual(h.denied,[error]);assert.equal(h.disconnected(),true);}
});
test('no IntersectionObserver keeps an accessible manual loading action',async()=>{
  const h=setup(async()=>({list:rows(51,10),count:60}),{observe:false});assert.equal(h.button.hidden,false);
  h.click();await tick();assert.equal(h.existing.length,60);assert.equal(h.button.hidden,true);
});
test('restored DOM resumes from the next page and preserves prior deduplication',async()=>{
  const calls=[];const h=setup(async q=>{calls.push(q.page);return {list:rows((q.page-1)*10+1,10),count:100}});
  await h.controller.load();h.controller.dispose();const restored=mount(h.node,h.options);
  await restored.load();assert.deepEqual(calls,[6,7]);assert.equal(h.existing.length,70);
});

test('cold history return rebuilds saved depth in ten-record batches and reports restored progress',async()=>{
  const calls=[],progress=[];
  const h=setup(async q=>{calls.push(q.page);return {list:rows((q.page-1)*10+1,10),count:100}}, {restorePage:9,onProgress:(page,restoring)=>progress.push([page,restoring])});
  await tick();assert.equal(h.existing.length,80);assert.deepEqual(calls,[6,7,8]);assert.deepEqual(progress,[[7,true],[8,true],[9,true]]);
});
test('cold restore pauses on error, preserves progress, and continues after retry',async()=>{
  let failed=true;const calls=[];
  const h=setup(async q=>{calls.push(q.page);if(q.page===7&&failed)throw Error('offline');return {list:rows((q.page-1)*10+1,10),count:100}}, {restorePage:9});
  await tick();assert.equal(h.existing.length,60);assert.deepEqual(calls,[6,7]);failed=false;h.click();await tick();
  assert.equal(h.existing.length,80);assert.deepEqual(calls,[6,7,7,8]);
});
