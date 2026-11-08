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
  const form={inputs:{quota_validity_days:'30',multiplier:'1.2345',threshold:'123.456',reason:'Reviewed rates',confirm:'on'},
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
test('form preserves legacy threshold without offering consumption conversion',()=>{
 const h=harness();const p=h.view.payload(h.form);
 assert.equal(p.multiplier_bps,10000);assert.equal(p.ticket_threshold_milli,5000);
 h.view.current={id:7,ticket_threshold_milli:123456,rules:[{multiplier_bps:23456}]};assert.equal(h.view.payload(h.form).multiplier_bps,23456);assert.equal(h.view.payload(h.form).ticket_threshold_milli,123456);
 assert.equal(p.starts_at,undefined);assert.equal(p.continuous,true);assert.equal(p.quota_validity_days,30);assert.equal(p.experience_budget,undefined);assert.equal(p.experience_budget_unlimited,true);
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
 const h=harness();h.context.fetch=async()=>({ok:true,status:200,json:async()=>({periods:[],continuous_supported:true,current_reward_rule_supported:true,unlimited_quota_supported:true,unlimited_experience_supported:true})});
 const html=await h.view.page();assert.match(html,/name="quota_validity_days"[^>]*value="30"/);assert.doesNotMatch(html,/name="starts_at"/);assert.doesNotMatch(html,/name="threshold"|name="multiplier"|API 贡献倍率|未成券贡献|每张脉冲券所需贡献度/);assert.match(html,/社区签到和互动/);
});
test('experience-only pool is probability-only with no EXP budget and keeps per-prize limit',()=>{
 const h=harness();
 h.form.querySelectorAll=()=>[{querySelector:s=>({value:s.includes('prize_key')?'exp-500':s.includes('prize_amount')?'500':s.includes('prize_type')?'community_exp':'1'})}];
 const p=h.view.payload(h.form);
 assert.equal(p.experience_budget,undefined);assert.equal(p.experience_budget_unlimited,true);assert.equal(p.reward_budget,0);assert.equal(p.quota_budget_unlimited,undefined);
 assert.equal(p.rewards[0].amount,500);assert.equal(p.rewards[0].reward_type,'community_exp');
 h.form.querySelectorAll=()=>[{querySelector:s=>({value:s.includes('prize_key')?'exp-big':s.includes('prize_amount')?'1000001':s.includes('prize_type')?'community_exp':'1'})}];
 assert.throws(()=>h.view.payload(h.form));
});

test('quota and EXP pools always send zero caps and probability-only flags',()=>{
 const h=harness();
 h.form.querySelectorAll=()=>['community_exp','newapi_quota'].map(type=>({querySelector:s=>({value:s.includes('prize_type')?type:s.includes('prize_key')?type:s.includes('prize_amount')?'10':'1'})}));
 const p=h.view.payload(h.form);assert.equal(p.quota_budget_unlimited,true);assert.equal(p.experience_budget_unlimited,true);assert.equal(p.reward_budget,0);assert.equal(p.experience_budget,undefined);
});
test('quota expectation includes experience weight and avoids unsafe products',()=>{
 const {context}=harness();
 assert.equal(context.MetarPeriodAdmin.quotaExpectation([{type:'newapi_quota',amount:100,weight:1},{type:'community_exp',amount:10,weight:9}]),'100/10 ≈ 10.0000');
 assert.match(context.MetarPeriodAdmin.quotaExpectation([{type:'newapi_quota',amount:Number.MAX_SAFE_INTEGER,weight:Number.MAX_SAFE_INTEGER}]),/9007199254740991\.0000$/);
});

test('50% pool preserves the approved 0.25-unit expectation across quota conversion rates',()=>{
 const {context}=harness();
 const {recommendedRewards,rewardPreview}=context.MetarPeriodAdmin;
 for (const unit of [100000,500000,4]) {
  const rows=recommendedRewards(unit);
  assert.equal(rows.length,7);
  assert.equal(rows.map(r=>r.weight).join(','),'44000,5000,800,200,28000,12000,10000');
  assert.equal(rows.slice(0,4).map(r=>r.amount).join(','),[unit/4,unit,unit*5,unit*25].join(','));
  assert.equal(rows.slice(4).map(r=>r.amount).join(','),'1,4,20');
  const stats=context.MetarAdapters.pulseRewardStats(rows);
  assert.equal(stats.total,100000n);assert.equal(stats.quotaWeight,50000n);
  assert.equal(stats.quotaAmount*4n,BigInt(unit)*stats.total);
  assert.equal(stats.expAmount*100n,276n*stats.total);
  assert.equal(stats.expAmount*100n,552n*stats.expWeight);
  const html=rewardPreview(rows,unit);
  for(const v of ['44%','5%','0.8%','0.2%','28%','12%','10%','56%','24%','20%','0.25 ⚡️','2.76 EXP','5.52 EXP']) assert.ok(html.includes(v),v);
 }
 for(const unit of [0,-4,3,1.5,Number.MAX_SAFE_INTEGER-3]) assert.equal(recommendedRewards(unit),null);
 // Mutating a returned form must not change the next preset.
 recommendedRewards(4)[4].amount=999;
 assert.equal(recommendedRewards(4)[4].amount,1);
});
test('loading the pool changes only prizes and reason, preserving validity and pending requests',()=>{
 const h=harness();
 const original={quota_validity_days:'60'};
 for(const [key,value] of Object.entries(original))h.form.querySelector(`[name="${key}"]`).value=value;
 h.view.action('admin-period-preset');
 for(const [key,value] of Object.entries(original))assert.equal(h.form.querySelector(`[name="${key}"]`).value,value);
 assert.match(h.form.querySelector('[data-period-prizes]').innerHTML,/value="44000"/);
 assert.match(h.form.querySelector('[data-period-expectation]').innerHTML,/2.76 EXP/);
 assert.equal(h.view.pending,null);assert.equal(h.storage.size,0);
 const html=h.form.querySelector('[data-period-prizes]').innerHTML;
 h.view.pending={key:'original',body:{rewards:[]}};
 h.view.quotaPerUnit=4;h.view.action('admin-period-preset');
 assert.equal(h.form.querySelector('[data-period-prizes]').innerHTML,html);
});
test('existing finite rules show their prizes but the form offers no budget inputs',async()=>{
 const h=harness();
 h.context.fetch=async()=>({ok:true,status:200,json:async()=>({continuous_supported:true,current_reward_rule_supported:true,unlimited_quota_supported:true,unlimited_experience_supported:true,periods:[{id:1,key:'old-rule',rules:[],ticket_threshold_milli:5000,status:'active',continuous:true,starts_at:'2020-01-01',ends_at:'2099-01-01',quota_budget_unlimited:false,reward_budget:10000,experience_budget:20000,rewards:[{key:'old',amount:10,weight:1,reward_type:'community_exp'}]}]})});
 const html=await h.view.page();
 assert.doesNotMatch(html,/name="unlimited_quota"|name="reward_budget"|name="experience_budget"/);
 assert.match(html,/保存统一规则/);assert.match(html,/name="reason"/);assert.match(html,/name="confirm"/);
 assert.match(html,/value="old"/);assert.doesNotMatch(html,/value="api-participation"/);
});
test('approved preset payload preserves integer amounts and weights when submitted',()=>{
 const h=harness();
 const rows=h.context.MetarPeriodAdmin.recommendedRewards(100000);
 h.form.querySelectorAll=()=>rows.map(r=>({querySelector:s=>({value:String(s.includes('prize_type')?(r.reward_type||'newapi_quota'):s.includes('prize_key')?r.key:s.includes('prize_amount')?r.amount:r.weight)})}));
 const payload=h.view.payload(h.form);
 assert.deepEqual(JSON.parse(JSON.stringify(payload.rewards)),JSON.parse(JSON.stringify(rows)));
 assert.equal(payload.multiplier_bps,10000);assert.equal(payload.quota_validity_days,30);
});
