'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
function harness() {
  const storage = new Map();
  const context = vm.createContext({ console, URL, Headers, AbortController, setTimeout, clearTimeout, crypto:require('node:crypto').webcrypto,
    sessionStorage:{getItem:k=>storage.get(k),setItem:(k,v)=>storage.set(k,v),removeItem:k=>storage.delete(k)},
    localStorage:{getItem:()=>null},
    FormData:class{constructor(form){this.data=form.inputs;} get(k){return this.data[k];}has(k){return k in this.data;}},
  });context.window=context;
  for (const name of ['i18n.js','adapters.js','admin-periods.js']) vm.runInContext(fs.readFileSync(path.join(__dirname,'../src',name),'utf8'),context);
  const nodes = new Map();
  const form={inputs:{quota_validity_days:'30',multiplier:'1.2345',threshold:'123.456',reward_budget:'0',experience_budget:'100',reason:'Reviewed rates',confirm:'on'},
    querySelector:s=>{if(!nodes.has(s))nodes.set(s,{});return nodes.get(s);},
    querySelectorAll:()=>[{querySelector:s=>({value:s.includes('prize_type')?'community_exp':s.includes('prize_key')?'reward':s.includes('prize_amount')?'10':'1'})}],reset(){}};
  context.document={querySelector:()=>form};
  const adapter=new context.MetarAdapters.PulseAdminAdapter(new context.MetarAdapters.AnswerAdapter({}));
  return {context,form,storage,adapter,view:new context.MetarPeriodAdmin.View(adapter)};
}
test('fixed-point input preserves decimal precision and rejects overflow or rounding',()=>{
 const {context}=harness(); const {fixed,format}=context.MetarPeriodAdmin;
 assert.equal(fixed('1.2345',4),12345);assert.equal(fixed('123.456',3),123456);
 assert.equal(fixed('9007199254740.991',3),Number.MAX_SAFE_INTEGER);
 for(const v of ['0','-1','0.0001','1e3','1.0000','9007199254740.992'])assert.throws(()=>fixed(v,3));
 assert.equal(format(1000000,3),'1000');assert.equal(format(12500,4),'1.25');
});
test('form sends frozen ticket rules without a manual period or date',()=>{
 const h=harness();const p=h.view.payload(h.form);
 assert.equal(p.multiplier_bps,12345);assert.equal(p.ticket_threshold_milli,123456);
 assert.equal(p.starts_at,undefined);assert.equal(p.continuous,true);assert.equal(p.quota_validity_days,30);assert.equal(p.experience_budget,100);
 assert.equal(p.expected_period_id,0);assert.match(p.key,/^rules-/);
 delete h.form.inputs.confirm;assert.throws(()=>h.view.payload(h.form));
});
test('lost response survives refresh and retries identical payload and key; no duplicate submission',async()=>{
 const h=harness();const requests=[];
 h.context.fetch=async(url,options)=>{
  requests.push({url,options});if(requests.length===1)throw new Error('lost response');
  return {ok:true,status:200,json:async()=>({period_id:1,period_key:JSON.parse(options.body).key,status:'active'})};
 };
 await h.view.submit(h.form);assert.ok(h.view.pending);assert.equal(h.storage.size,1);
 await h.view.submit(h.form);assert.equal(requests.length,1);
 const reloaded=new h.context.MetarPeriodAdmin.View(h.adapter);reloaded.restore();
 await reloaded.submit(h.form,true);
 assert.equal(requests[0].options.body,requests[1].options.body);
 assert.equal(requests[0].options.headers.get('Idempotency-Key'),requests[1].options.headers.get('Idempotency-Key'));
 assert.equal(reloaded.pending,null);assert.equal(h.storage.size,0);
});
test('conflict is shown without overwriting active periods or automatic retry',async()=>{
 const h=harness();let count=0;
 h.context.fetch=async()=>{count++;return {ok:false,status:409,json:async()=>({error:'period_conflict'})};};
 await h.view.submit(h.form);assert.equal(count,1);assert.equal(h.view.pending,null);
 assert.match(h.form.querySelector('[data-period-message]').textContent,/冲突/);
});
test('older backend disables the new editor while returning a clear unavailable state',async()=>{
 const h=harness();h.context.fetch=async()=>({ok:false,status:404,json:async()=>({error:'not_found'})});
 const html=await h.view.page();assert.match(html,/奖励配置暂不可用/);assert.doesNotMatch(html,/data-form="admin-period"/);
});
test('legacy list endpoint without continuous capability cannot enable the editor',async()=>{
 const h=harness();h.context.fetch=async()=>({ok:true,status:200,json:async()=>({periods:[]})});
 const html=await h.view.page();assert.match(html,/奖励配置暂不可用/);assert.doesNotMatch(html,/data-form="admin-period"/);
});
test('new backend displays continuous form with a thirty-day default',async()=>{
 const h=harness();h.context.fetch=async()=>({ok:true,status:200,json:async()=>({periods:[],continuous_supported:true})});
 const html=await h.view.page();assert.match(html,/name="quota_validity_days"[^>]*value="30"/);assert.doesNotMatch(html,/name="starts_at"/);
});
test('experience pool uses EXP budget independently and rejects accidental quota conversion',()=>{
 const h=harness();h.form.inputs.reward_budget='0';h.form.inputs.experience_budget='1000';
 h.form.querySelectorAll=()=>[{querySelector:s=>({value:s.includes('prize_key')?'exp-500':s.includes('prize_amount')?'500':s.includes('prize_type')?'community_exp':'1'})}];
 const p=h.view.payload(h.form);
 assert.equal(p.experience_budget,1000);assert.equal(p.reward_budget,0);
 assert.equal(p.rewards[0].amount,500);assert.equal(p.rewards[0].reward_type,'community_exp');
 h.form.inputs.experience_budget='499';assert.throws(()=>h.view.payload(h.form));
 h.form.inputs.experience_budget='1000';h.form.inputs.reward_budget='100';assert.throws(()=>h.view.payload(h.form));
});
