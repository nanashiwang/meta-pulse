'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
global.window = global;
require('../src/adapters.js');
const choice = {selection_version:3, draws:Array.from({length:5},(_,i)=>({selection:'server-choice-'+i}))};
const { PulseOperation, PulseDrawSession, pulseActions, pulseRewardTier, AdapterError } = window.MetarAdapters;
function storage() {
  const values = new Map();
  window.sessionStorage = {getItem: key => values.get(key) || null, setItem: (key,value) => values.set(key,value), removeItem: key => values.delete(key)};
  return values;
}
const reward = action => ({grant_id:'grant-'+action.actionId,action_id:action.actionId,reward_type:'newapi_quota',amount:500000,status:'pending'});

test('five draws persist five distinct identifiers before spending, isolate users and preserve legacy operations', () => {
  const values = storage(), store = new PulseOperation('a'), operation = store.begin(5, choice);
  assert.equal(pulseActions(operation).length, 5);
  assert.equal(new Set(operation.actions.map(a => a.actionId)).size, 5);
  assert.deepEqual(store.begin(1, choice), operation);
  assert.deepEqual(new PulseOperation('a').read(), operation);
  assert.equal(new PulseOperation('b').read(), null);
  assert.doesNotMatch(values.get(store.key), /amount|reward|balance|ticket/);
  store.clear();
  const single = store.begin(1, choice);
  assert.equal(pulseActions(single).length, 1);
  assert.deepEqual(store.begin(5, choice), single);
});

test('third response loss is recovered by querying original IDs; five draws spend exactly five times', async () => {
  storage();
  const store = new PulseOperation('a'), operation = store.begin(5, choice), issued = new Map(), calls = [];
  let lose = true;
  const client = {
    async act(action) {
      calls.push(action.actionId);
      assert.equal(store.read().actions.length, 5);
      const result = issued.get(action.actionId) || reward(action); issued.set(action.actionId, result);
      if (calls.length === 3 && lose) throw new AdapterError('lost response', {code:'action_pending'});
      return result;
    },
    async rewards(id) { return {rewards: issued.has(id) ? [issued.get(id)] : []}; },
  };
  let confirmed = [];
  await assert.rejects(new PulseDrawSession(client,store).run(operation,{onResult:results=>confirmed=results}), error=>error.code==='action_pending');
  assert.equal(confirmed.length, 2);
  assert.equal(calls.length, 3);
  lose = false;
  const reloadedStore = new PulseOperation('a'), restored = reloadedStore.read(), session = new PulseDrawSession(client,reloadedStore);
  const known = await session.recover(restored);
  assert.equal(known.length, 3);
  assert.equal(calls.length, 3, 'read-only recovery cannot spend tickets');
  const results = await session.run(restored,{known});
  assert.equal(results.length,5); assert.equal(calls.length,5); assert.equal(issued.size,5);
  assert.equal(reloadedStore.read(),null);
  assert.deepEqual(results.map(r=>r.action_id),operation.actions.map(a=>a.actionId));
});

test('unknown result retries the same action and never advances until confirmed', async () => {
  storage();
  const store = new PulseOperation('a'), operation = store.begin(5, choice), calls = [];
  const client = {async act(action) {calls.push(action);throw new AdapterError('offline',{code:'action_pending'});},async rewards(){return {rewards:[]};}};
  const session = new PulseDrawSession(client,store);
  for(let i=0;i<3;i++) await assert.rejects(session.run(store.read(),{known:await session.recover(operation)}));
  assert.equal(calls.length,3);
  assert.ok(calls.every(action=>action.actionId===operation.actions[0].actionId && action.idempotencyKey===operation.actions[0].idempotencyKey));
  assert.deepEqual(store.read(),operation);
});

test('navigation stops unsent actions and malformed responses retain the original journal', async () => {
  storage();
  const store = new PulseOperation('a'), operation = store.begin(5, choice); let active = true, calls = 0;
  const client = {async act(action) {calls++;active=false;return reward(action);}};
  let confirmed;
  await assert.rejects(new PulseDrawSession(client,store).run(operation,{canContinue:()=>active,onResult:r=>confirmed=r}),error=>error.code==='action_interrupted');
  assert.equal(calls,1);assert.equal(confirmed.length,1);assert.deepEqual(store.read(),operation);
  await assert.rejects(new PulseDrawSession({async act(){return {...reward(operation.actions[0]),action_id:'wrong'};}},store).run(operation),error=>error.code==='action_pending');
  assert.deepEqual(store.read(),operation);
});

test('corrupt or blocked recovery storage never silently replaces an existing operation', () => {
  const values=storage(), store=new PulseOperation('a');
  values.set(store.key,'{broken');
  assert.throws(()=>store.begin(5, choice),error=>error.code==='storage_unavailable');
  values.clear(); window.sessionStorage.setItem=()=>{throw Error('full')};
  assert.throws(()=>store.begin(5, choice),error=>error.code==='storage_unavailable');
});

test('five presentation tiers use exact boundaries without changing reward amounts', () => {
  const quota = amount => ({reward_type:'newapi_quota',amount});
  const unit=500000;
  assert.equal(pulseRewardTier({reward_type:'community_exp',amount:100},unit),'white');
  for(const [amount,tier] of [[1,'blue'],[249999,'blue'],[250000,'purple'],[999999,'purple'],[1000000,'gold'],[4999999,'gold'],[5000000,'red']]) assert.equal(pulseRewardTier(quota(amount),unit),tier);
  assert.equal(pulseRewardTier(quota(1),3),'blue');assert.equal(pulseRewardTier(quota(2),3),'purple');
  assert.equal(pulseRewardTier(quota(Number.MAX_SAFE_INTEGER),Number.MAX_SAFE_INTEGER),'purple');
  assert.equal(pulseRewardTier(quota(10),0),'white');
});

test('new sessions preserve each of five cross-batch selections after reload', async () => {
  storage();
  const store = new PulseOperation('selection-user');
  assert.throws(()=>store.begin(),e=>e.code==='selection_required');
  const operation=store.begin(5,choice);
  assert.ok(pulseActions(operation).every(a=>a.protocolVersion===3 && a.selection===choice.draws[pulseActions(operation).indexOf(a)].selection));
  assert.deepEqual(store.begin(5,{selection_version:2,selection:'another-group'}),operation);
  const restored=new PulseOperation('selection-user').read();
  const calls=[];
  await new PulseDrawSession({act:async a=>{calls.push(a);return {action_id:a.actionId,grant_id:a.actionId,amount:10,reward_type:'community_exp'};}},store).run(restored);
  assert.ok(calls.every((a,i)=>a.selection===choice.draws[i].selection));
});

test('historical saved requests stay v1; mixed protocol saved batches fail closed', () => {
  const values=storage(),store=new PulseOperation('legacy');
  const id=crypto.randomUUID();
  values.set(store.key,JSON.stringify({actionId:id,idempotencyKey:id}));
  assert.equal(store.read().selection,undefined);
  assert.equal(store.begin(1,choice).selection,undefined);
  store.clear();
  const operation=store.begin(5,choice);
  operation.actions[1].protocolVersion=2;
  values.set(store.key,JSON.stringify(operation));
  assert.throws(()=>store.read(),e=>e.code==='storage_unavailable');
});

test('probability display uses exact integer ratios and never rounds tiny nonzero odds to zero', () => {
 const {formatPulseProbability: pct, pulseRewardStats: stats}=window.MetarAdapters;
 assert.equal(pct(44000,100000),'44%');assert.equal(pct(800,100000),'0.8%');
 assert.equal(pct(28000,50000),'56%');assert.equal(pct(12000,50000),'24%');
 assert.equal(pct(1,3),'≈33.3333%');assert.equal(pct(1,Number.MAX_SAFE_INTEGER),'<0.0001%');
 for(const [a,b] of [[1,0],[-1,2],[3,2],['<b>',2],[1,NaN]]) assert.equal(pct(a,b),'—');
 const large=stats([{amount:Number.MAX_SAFE_INTEGER,weight:Number.MAX_SAFE_INTEGER}]);
 assert.equal(large.quotaAmount,BigInt(Number.MAX_SAFE_INTEGER)**2n);
 assert.throws(()=>stats([{amount:1,weight:Number.MAX_SAFE_INTEGER},{amount:1,weight:1}]));
 assert.throws(()=>stats([{amount:1,weight:1,reward_type:'unknown'}]));
});
