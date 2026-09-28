'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
global.window = global;
require('../src/adapters.js');
const { PulseOperation, PulseDrawSession, pulseActions, pulseRewardTier, AdapterError } = window.MetarAdapters;
function storage() {
  const values = new Map();
  window.sessionStorage = {getItem: key => values.get(key) || null, setItem: (key,value) => values.set(key,value), removeItem: key => values.delete(key)};
  return values;
}
const reward = action => ({grant_id:'grant-'+action.actionId,action_id:action.actionId,reward_type:'newapi_quota',amount:500000,status:'pending'});

test('five draws persist five distinct identifiers before spending, isolate users and preserve legacy operations', () => {
  const values = storage(), store = new PulseOperation('a'), operation = store.begin(5);
  assert.equal(pulseActions(operation).length, 5);
  assert.equal(new Set(operation.actions.map(a => a.actionId)).size, 5);
  assert.deepEqual(store.begin(1), operation);
  assert.deepEqual(new PulseOperation('a').read(), operation);
  assert.equal(new PulseOperation('b').read(), null);
  assert.doesNotMatch(values.get(store.key), /amount|reward|balance|ticket/);
  store.clear();
  const single = store.begin();
  assert.equal(pulseActions(single).length, 1);
  assert.deepEqual(store.begin(5), single);
});

test('third response loss is recovered by querying original IDs; five draws spend exactly five times', async () => {
  storage();
  const store = new PulseOperation('a'), operation = store.begin(5), issued = new Map(), calls = [];
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
  const store = new PulseOperation('a'), operation = store.begin(5), calls = [];
  const client = {async act(action) {calls.push(action);throw new AdapterError('offline',{code:'action_pending'});},async rewards(){return {rewards:[]};}};
  const session = new PulseDrawSession(client,store);
  for(let i=0;i<3;i++) await assert.rejects(session.run(store.read(),{known:await session.recover(operation)}));
  assert.equal(calls.length,3);
  assert.ok(calls.every(action=>action.actionId===operation.actions[0].actionId && action.idempotencyKey===operation.actions[0].idempotencyKey));
  assert.deepEqual(store.read(),operation);
});

test('navigation stops unsent actions and malformed responses retain the original journal', async () => {
  storage();
  const store = new PulseOperation('a'), operation = store.begin(5); let active = true, calls = 0;
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
  assert.throws(()=>store.begin(5),error=>error.code==='storage_unavailable');
  values.clear(); window.sessionStorage.setItem=()=>{throw Error('full')};
  assert.throws(()=>store.begin(5),error=>error.code==='storage_unavailable');
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
