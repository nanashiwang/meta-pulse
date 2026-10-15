'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const source=fs.readFileSync(path.join(__dirname,'../src/pulse-feed.js'),'utf8');
const payload={quota_per_unit:500000,rewards:[{reward_type:'newapi_quota',amount:625000,won_at:'2026-10-15T01:15:49Z',user_id:999,username:'private',grant_id:'private'}]};
const displayReward=row=>({amount:String(row.amount),unit:'⚡️',tier:'purple'});
const opts={t:x=>x,displayReward,locale:()=> 'en-US'};
function harness(){
 const listeners=new Map(),timers=new Map(),pending=[];let id=0,current=true;
 const node=()=>({innerHTML:'',textContent:'',hidden:false,disabled:false,onclick:null,dataset:{},setAttribute(){}});
 const makeRoot=kind=>{const row=node(),message=node(),button=node();row.dataset.pulseFeedRows=kind;return {...node(),row,message,button,querySelectorAll:sel=>sel==='[data-pulse-feed-rows]'?[row]:sel==='[data-pulse-feed-message]'?[message]:[button],addEventListener(){},removeEventListener(){}}};
 const root=makeRoot('preview'),dialog=makeRoot('full');
 const document={hidden:false,addEventListener:(name,fn)=>listeners.set(name,fn),removeEventListener:name=>listeners.delete(name)};
 const context=vm.createContext({document,setTimeout:(fn,ms)=>{timers.set(++id,{fn,ms});return id;},clearTimeout:id=>timers.delete(id)});
 context.window=context;context.addEventListener=(name,fn)=>listeners.set(name,fn);context.removeEventListener=name=>listeners.delete(name);
 vm.runInContext(source,context);
 const client={winners:()=>new Promise((resolve,reject)=>pending.push({resolve,reject}))};
 const view=new context.MetarRewardFeed.View(root,dialog,{...opts,client,current:()=>current});
 return {api:context.MetarRewardFeed,root,dialog,view,document,listeners,timers,pending,setCurrent:value=>{current=value;}};
}
const flush=()=>new Promise(resolve=>setImmediate(resolve));
test('feed validation strips identity fields, rounds seconds and bounds records and numeric values',async()=>{
 const h=harness(),safe=h.api.validatedFeed(payload);
 assert.deepEqual(JSON.parse(JSON.stringify(safe)),{quota_per_unit:500000,rewards:[{reward_type:'newapi_quota',amount:625000,won_at:'2026-10-15T01:15:00.000Z'}]});
 const html=h.api.rows(safe,{...opts,displayReward:()=>({amount:'<script>',unit:'EXP',tier:'white'})});
 assert.doesNotMatch(html,/private|user_id|grant_id|<script>/);assert.match(html,/&lt;script&gt;/);
 for(const bad of [null,{...payload,rewards:Array(21).fill(payload.rewards[0])},{...payload,quota_per_unit:-1},...['cash',null].map(reward_type=>({...payload,rewards:[{...payload.rewards[0],reward_type}]})),...[-1,0,1.5,Number.MAX_SAFE_INTEGER+1].map(amount=>({...payload,rewards:[{...payload.rewards[0],amount}]}))]) assert.throws(()=>h.api.validatedFeed(bad));
 assert.match(h.api.rows({rewards:[],quota_per_unit:1},opts),/暂无社区中奖动态/);h.view.dispose();
});
test('feed failures retain confirmed rows; permission refusal clears both previews and stops polling',async()=>{
 const h=harness();h.pending[0].resolve(payload);await flush();
 const old=h.root.row.innerHTML;assert.match(old,/625000/);assert.equal(h.dialog.row.innerHTML,old);assert.equal([...h.timers.values()][0].ms,60000);
 const refresh=h.view.refresh();assert.equal(h.dialog.button.disabled,false,'refresh must not disable the focused control');h.pending[1].reject(new Error('offline'));await refresh;
 assert.equal(h.root.row.innerHTML,old);assert.match(h.root.message.textContent,/仍显示上次记录/);
 const denied=h.view.refresh();h.pending[2].reject({status:403});await denied;
 assert.equal(h.root.row.innerHTML,'');assert.equal(h.dialog.row.innerHTML,'');assert.equal(h.timers.size,0);assert.match(h.root.message.textContent,/重新登录/);h.view.dispose();
});
test('hidden pages do not poll and late responses cannot cross a disposed or changed identity',async()=>{
 const h=harness();h.pending[0].resolve(payload);await flush();h.document.hidden=true;h.listeners.get('visibilitychange')();
 assert.equal(h.timers.size,0);await h.view.refresh();assert.equal(h.pending.length,1);
 h.document.hidden=false;h.listeners.get('visibilitychange')();assert.equal(h.pending.length,2);const before=h.root.row.innerHTML;
 h.setCurrent(false);h.pending[1].resolve({...payload,rewards:[]});await flush();assert.equal(h.root.row.innerHTML,before);assert.equal(h.timers.size,0);h.view.dispose();
 const disposed=harness();disposed.view.dispose();disposed.pending[0].resolve(payload);await flush();assert.equal(disposed.root.row.innerHTML,'');assert.equal(disposed.timers.size,0);
});
test('one in-flight request is shared, previews show five rows and full dialog stays bounded',async()=>{
 const h=harness();await h.view.refresh();assert.equal(h.pending.length,1);
 h.pending[0].resolve({...payload,rewards:Array(20).fill(payload.rewards[0])});await flush();assert.equal((h.root.row.innerHTML.match(/<tr>/g)||[]).length,5);assert.equal((h.dialog.row.innerHTML.match(/<tr>/g)||[]).length,20);h.view.dispose();
});
